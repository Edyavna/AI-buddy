package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPlaybookMarkdown(t *testing.T) {
	commit := strings.Repeat("a", 40)
	markdown := "# Про C#\n\n## Раздел\n\n### Подраздел\n" +
		strings.Repeat("Кириллический текст.\n", 1800) +
		"## Финал\n\n[Путь](../docs/a.md#раздел) [Внешняя](https://example.org) [Якорь](#финал)\n" +
		"[ref]: <../notes/A B.md> \"Заметка\"\n\n`[код](../keep.md)`\n```md\n# Не заголовок\n[пример](keep.md)\n```\n"
	rewritten := rewritePlaybookLinks(markdown, commit, "nested/guide.md")
	for _, want := range []string{
		"(" + "<" + playbookURL(commit, "docs/a.md", "", "раздел") + ">)",
		"[Внешняя](https://example.org)", playbookURL(commit, "nested/guide.md", "", "финал"),
		"[ref]: <" + playbookURL(commit, "notes/A B.md", "", "") + "> \"Заметка\"",
		"`[код](../keep.md)`", "# Не заголовок\n[пример](keep.md)",
	} {
		if !strings.Contains(rewritten, want) {
			t.Errorf("missing %q", want)
		}
	}
	title, parts := splitPlaybookMarkdown(rewritten, "nested/guide.md")
	if title != "Про C#" || len(parts) < 3 {
		t.Fatal(title, len(parts))
	}
	for _, part := range parts {
		if len(part) == 0 || utf8.RuneCountInString(part) > MaxMarkdownRunes || !utf8.ValidString(part) || !strings.Contains(part, title) {
			t.Fatalf("invalid part of %d runes", utf8.RuneCountInString(part))
		}
		if strings.Contains(part, "Кириллический текст.") && !strings.Contains(part, "### Подраздел") {
			t.Fatal("section context lost")
		}
	}
	if strings.Count(strings.Join(parts, ""), "Кириллический текст.") != 1800 {
		t.Fatal("original text was lost or duplicated")
	}
}

func TestLoadPlaybookFromCommit(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=Test", "-c", "user.email=test@example.org"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet")
	git("remote", "add", "origin", playbookGitHub+".git")
	if err := os.Mkdir(filepath.Join(repo, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "nested", "guide.MD")
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(file, "Original title\n==============\n\nCommitted text")
	git("add", "nested/guide.MD")
	git("commit", "--quiet", "-m", "Add a guide")
	sha := git("rev-parse", "HEAD")
	write(file, "Edited in worktree")
	write(filepath.Join(repo, "untracked.md"), "Untracked")
	s, err := LoadPlaybook(t.Context(), repo, sha)
	if err != nil || s.Commit != sha || s.Files != 1 || s.Parts() != 1 {
		t.Fatal(s, err)
	}
	p := s.parts[0]
	if p.path != "nested/guide.MD" || p.url != playbookURL(sha, p.path, "", "") ||
		!strings.Contains(p.doc.Title, "Original title") || !strings.Contains(p.doc.Content, "Committed text") {
		t.Fatal(p)
	}
	for _, invalid := range []string{"HEAD", strings.Repeat("0", 40), p.blob} {
		if _, err := LoadPlaybook(t.Context(), repo, invalid); err == nil {
			t.Fatal("accepted invalid commit", invalid)
		}
	}
}

type playbookTestEmbedder struct {
	passages int
	failAt   int
}

func (e *playbookTestEmbedder) Embed(_ context.Context, task, _ string) ([]float64, error) {
	if task == "retrieval.passage" {
		e.passages++
		if e.passages == e.failAt {
			return nil, ports.ErrUnavailable
		}
	}
	result := make([]float64, 1024)
	result[0] = 1
	return result, nil
}

func TestPostgresPlaybookSyncAndACL(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(t.Context())
	other := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "another", TeamID: "team_added_later", Role: identity.Intern})
	embed := &playbookTestEmbedder{}
	s := Service{DB: db, Embedder: embed}
	doc := Document{ID: "internal", Source: "demo", SourceID: "internal.md", Title: "Internal", TeamID: testkit.TeamID, Content: "Private", UpdatedAt: time.Now()}
	if err := s.Import(ctx, doc); err != nil {
		t.Fatal(err)
	}
	first, second := strings.Repeat("a", 40), strings.Repeat("b", 40)
	part := func(commit, file, blob, body string) playbookPart {
		key := commit + ":1:" + file
		return playbookPart{
			doc: Document{ID: playbookIDPrefix + key, Source: playbookSource, SourceID: key, Title: file,
				TeamID: playbookSource, Content: body, UpdatedAt: doc.UpdatedAt},
			path: file, blob: blob, number: 1, url: playbookURL(commit, file, "", ""),
		}
	}
	before := PlaybookSnapshot{Commit: first, parts: []playbookPart{
		part(first, "changed.md", "1", strings.Repeat("old body ", 600)), part(first, "deleted.md", "2", "removed body"), part(first, "stable.md", "3", "stable"),
	}}
	after := PlaybookSnapshot{Commit: second, parts: []playbookPart{
		part(second, "changed.md", "4", "new body"), part(second, "added.md", "5", "new file"), part(second, "stable.md", "3", "stable"),
	}}
	sync := func(snapshot PlaybookSnapshot, want PlaybookSyncResult) {
		t.Helper()
		if got, err := s.SyncPlaybook(ctx, snapshot); err != nil || got != want {
			t.Fatal(got, want, err)
		}
	}
	search := func(ctx context.Context, direction string, want ...string) {
		t.Helper()
		hits, err := s.SearchDirection(ctx, "Playbook", direction)
		if err != nil || len(hits) != len(want) {
			t.Fatal(direction, hits, err)
		}
		got := map[string]bool{}
		for _, hit := range hits {
			got[hit.DocumentID] = true
		}
		for _, id := range want {
			if !got[id] {
				t.Fatal("missing", id, hits)
			}
		}
	}
	sync(before, PlaybookSyncResult{Indexed: 3})
	passages := embed.passages
	sync(before, PlaybookSyncResult{Reused: 3})
	if embed.passages != passages {
		t.Fatal("unchanged documents were embedded again")
	}
	old, deleted, stable := before.parts[0], before.parts[1], before.parts[2]
	search(ctx, "mentor", doc.ID, old.doc.ID, deleted.doc.ID, stable.doc.ID)
	search(ctx, "hr", old.doc.ID, deleted.doc.ID, stable.doc.ID)
	search(other, "mentor", old.doc.ID, deleted.doc.ID, stable.doc.ID)
	if _, err := s.Read(other, doc.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("cross-team access to internal document", err)
	}
	if _, err := s.Read(t.Context(), old.doc.ID); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("unauthenticated access", err)
	}

	embed.failAt = embed.passages + 2
	if _, err := s.SyncPlaybook(ctx, after); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal(err)
	}
	search(other, "hr", old.doc.ID, deleted.doc.ID, stable.doc.ID)
	if _, err := s.Read(other, after.parts[0].doc.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("partially published failed import", err)
	}
	sync(after, PlaybookSyncResult{Indexed: 2, Reused: 1, Archived: 2})
	search(other, "hr", after.parts[0].doc.ID, after.parts[1].doc.ID, stable.doc.ID)
	sync(after, PlaybookSyncResult{Reused: 3})
	for _, p := range []playbookPart{old, deleted, stable, after.parts[0]} {
		req := httptest.NewRequest("GET", "/api/v1/documents/"+p.doc.ID, nil).WithContext(other)
		req.SetPathValue("document_id", p.doc.ID)
		w := httptest.NewRecorder()
		Handler(&s)(w, req)
		var response struct {
			Content     string `json:"content"`
			OriginalURL string `json:"original_url"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || response.Content != p.doc.Content || response.OriginalURL != p.url {
			t.Fatal(w.Code, w.Body, err)
		}
	}
	sync(before, PlaybookSyncResult{Reused: 3, Archived: 2})
	search(other, "hr", old.doc.ID, deleted.doc.ID, stable.doc.ID)
}
