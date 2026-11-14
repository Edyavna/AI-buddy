package knowledge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const playbookGitHub = "https://github.com/avito-tech/playbook"

var gitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

type PlaybookSnapshot struct {
	Commit string
	Files  int
	parts  []playbookPart
}

func (s PlaybookSnapshot) Parts() int { return len(s.parts) }

type playbookPart struct {
	doc             Document
	path, blob, url string
	number          int
}

// LoadPlaybook reads tracked blobs, so the checkout and local edits cannot
// change the text behind a commit-pinned citation.
func LoadPlaybook(ctx context.Context, repo, commit string) (PlaybookSnapshot, error) {
	if repo == "" || !gitSHA.MatchString(commit) {
		return PlaybookSnapshot{}, fmt.Errorf("provide a local Playbook repository and its full Git commit SHA")
	}
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"--no-replace-objects", "-C", repo}, args...)...)
		out, err := cmd.Output()
		if err != nil {
			// Do not print Git's remote URLs or configuration in operator logs.
			return "", fmt.Errorf("read Git %s: %w", args[0], err)
		}
		return string(out), nil
	}
	origin, err := git("remote", "get-url", "origin")
	if err != nil {
		return PlaybookSnapshot{}, err
	}
	switch strings.TrimSuffix(strings.TrimSpace(origin), ".git") {
	case playbookGitHub, "git@github.com:avito-tech/playbook", "ssh://git@github.com/avito-tech/playbook":
	default:
		return PlaybookSnapshot{}, fmt.Errorf("Git origin must identify avito-tech/playbook on GitHub")
	}
	metadata, err := git("show", "--no-patch", "--format=%H%n%cI", commit, "--")
	if err != nil {
		return PlaybookSnapshot{}, err
	}
	sha, timestamp, ok := strings.Cut(strings.TrimSpace(metadata), "\n")
	if !ok || strings.TrimSpace(sha) != commit {
		return PlaybookSnapshot{}, fmt.Errorf("Git object is not the requested commit")
	}
	date, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return PlaybookSnapshot{}, fmt.Errorf("read Playbook commit date: %w", err)
	}
	tree, err := git("ls-tree", "-r", "-z", commit)
	if err != nil {
		return PlaybookSnapshot{}, err
	}
	snapshot := PlaybookSnapshot{Commit: commit}
	for entry := range strings.SplitSeq(strings.TrimSuffix(tree, "\x00"), "\x00") {
		meta, name, ok := strings.Cut(entry, "\t")
		if !ok || !strings.EqualFold(path.Ext(name), ".md") {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || !gitSHA.MatchString(fields[2]) {
			return PlaybookSnapshot{}, fmt.Errorf("Playbook Markdown is not a regular Git blob: %q", name)
		}
		blob, err := git("cat-file", "blob", fields[2])
		if err != nil {
			return PlaybookSnapshot{}, fmt.Errorf("read Playbook %q: %w", name, err)
		}
		if !utf8.ValidString(name) || !utf8.ValidString(blob) || strings.ContainsRune(blob, 0) {
			return PlaybookSnapshot{}, fmt.Errorf("Playbook Markdown must be UTF-8 text: %q", name)
		}
		original := playbookURL(commit, name, "", "")
		title, parts := splitPlaybookMarkdown(rewritePlaybookLinks(blob, commit, name), name)
		for i, content := range parts {
			number := i + 1
			key := commit + ":" + strconv.Itoa(number) + ":" + name
			id := fmt.Sprintf("%s%x", playbookIDPrefix, sha256.Sum256([]byte(key)))
			partTitle := "AvitoTech Playbook · " + title
			if len(parts) > 1 {
				partTitle += fmt.Sprintf(" · %d/%d", number, len(parts))
			}
			doc := Document{ID: id, Source: playbookSource, SourceID: key, Title: partTitle, Content: content, TeamID: playbookSource, UpdatedAt: date.UTC()}
			snapshot.parts = append(snapshot.parts, playbookPart{doc: doc, path: name, blob: fields[2], url: original, number: number})
		}
		snapshot.Files++
	}
	if snapshot.Files == 0 {
		return PlaybookSnapshot{}, fmt.Errorf("the Playbook commit contains no Markdown files")
	}
	return snapshot, nil
}

func playbookURL(commit, file, query, fragment string) string {
	view := "blob"
	if file == "." || strings.HasSuffix(file, "/") {
		view = "tree"
	}
	return (&url.URL{Scheme: "https", Host: "github.com", Path: "/avito-tech/playbook/" + view + "/" + commit + "/" + strings.TrimPrefix(file, "."+"/"), RawQuery: query, Fragment: fragment}).String()
}
