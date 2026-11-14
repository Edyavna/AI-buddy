package knowledge

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	playbookHeading   = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$`)
	playbookUnderline = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)
	playbookFence     = regexp.MustCompile("^(?: {0,3})(`{3,}|~{3,})(.*)$")
	// Include reference definitions and inline code so that the latter is left intact.
	playbookLink = regexp.MustCompile("(`+)[^`]*`+|\\]\\([ \\t]*(<[^<>\\n]*>|(?:[^()<>\\s]+|\\([^()\\r\\n]*\\))+)|(?m)^ {0,3}\\[[^\\]\\n]+\\]:[ \\t]*(<[^<>\\n]*>|[^<>\\s]+)")
)

func rewritePlaybookLinks(markdown, commit, file string) string {
	var fence string
	var result strings.Builder
	for line := range strings.SplitAfterSeq(markdown, "\n") {
		inside := fence != ""
		fence = nextPlaybookFence(fence, line)
		if inside || fence != "" || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			result.WriteString(line)
			continue
		}
		last := 0
		for _, match := range playbookLink.FindAllStringSubmatchIndex(line, -1) {
			start, end := match[4], match[5]
			if start < 0 {
				start, end = match[6], match[7]
			}
			if start < 0 {
				continue
			}
			dest := strings.Trim(line[start:end], "<>")
			parsed, err := url.Parse(dest)
			if err != nil || parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(dest, "//") || strings.Contains(dest, "\\") {
				continue
			}
			target := file
			if parsed.Path != "" {
				base := path.Dir(file)
				if strings.HasPrefix(parsed.Path, "/") {
					base = "/"
				}
				target = strings.TrimPrefix(path.Join("/", base, parsed.Path), "/")
				if target == "" {
					target = "."
				} else if strings.HasSuffix(parsed.Path, "/") {
					target += "/"
				}
			}
			result.WriteString(line[last:start])
			result.WriteString("<" + playbookURL(commit, target, parsed.RawQuery, parsed.Fragment) + ">")
			last = end
		}
		result.WriteString(line[last:])
	}
	return result.String()
}

func nextPlaybookFence(open, line string) string {
	match := playbookFence.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
	if len(match) == 0 {
		return open
	}
	marker, rest := match[1], match[2]
	if open == "" {
		return marker
	}
	if strings.HasPrefix(marker, open) && strings.TrimSpace(rest) == "" {
		return ""
	}
	return open
}

type playbookSection struct {
	heading string
	level   int
	body    strings.Builder
}

func splitPlaybookMarkdown(markdown, file string) (string, []string) {
	markdown = strings.TrimPrefix(strings.ReplaceAll(markdown, "\r\n", "\n"), "\ufeff")
	lines := strings.SplitAfter(markdown, "\n")
	sections := []*playbookSection{{}}
	title := ""
	var fence string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		inside := fence != ""
		fence = nextPlaybookFence(fence, line)
		if !inside && fence == "" {
			level, heading, setext := playbookHeadingAt(lines, i)
			if level > 0 {
				if title == "" {
					title = heading
				}
				sections = append(sections, &playbookSection{heading: heading, level: level})
				if setext {
					i++
					line += lines[i]
				}
			}
		}
		sections[len(sections)-1].body.WriteString(line)
	}
	if title == "" {
		title = strings.TrimSuffix(path.Base(file), path.Ext(file))
	}
	title = limitRunes(title, 240)
	if strings.TrimSpace(markdown) == "" {
		markdown = "# " + title
	}
	if utf8.RuneCountInString(markdown) <= MaxMarkdownRunes {
		return title, []string{markdown}
	}
	var ancestors []*playbookSection
	var parts []string
	part := ""
	for _, section := range sections {
		if strings.TrimSpace(section.body.String()) == "" {
			continue
		}
		for len(ancestors) > 0 && ancestors[len(ancestors)-1].level >= section.level {
			ancestors = ancestors[:len(ancestors)-1]
		}
		heading := playbookContext(title, ancestors)
		body := section.body.String()
		if part != "" && utf8.RuneCountInString(part+body) <= MaxMarkdownRunes {
			part += body
		} else {
			if part != "" {
				parts = append(parts, part)
			}
			for utf8.RuneCountInString(heading+body) > MaxMarkdownRunes {
				first, rest := cutPlaybookText(body, MaxMarkdownRunes-utf8.RuneCountInString(heading))
				parts = append(parts, heading+first)
				body = rest
				heading = playbookContext(title, append(ancestors, section))
			}
			part = heading + body
		}
		if section.level > 0 {
			ancestors = append(ancestors, section)
		}
	}
	return title, append(parts, part)
}

func playbookHeadingAt(lines []string, i int) (int, string, bool) {
	line := strings.TrimRight(lines[i], "\n")
	if match := playbookHeading.FindStringSubmatch(line); len(match) > 0 {
		return len(match[1]), strings.TrimSpace(match[2]), false
	}
	if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") || i+1 >= len(lines) {
		return 0, "", false
	}
	if match := playbookUnderline.FindStringSubmatch(strings.TrimRight(lines[i+1], "\n")); len(match) > 0 {
		if match[1][0] == '=' {
			return 1, strings.TrimSpace(line), true
		}
		return 2, strings.TrimSpace(line), true
	}
	return 0, "", false
}

func playbookContext(title string, ancestors []*playbookSection) string {
	prefix := "# " + title + "\n\n"
	for _, section := range ancestors {
		if section.heading != "" && section.heading != title {
			prefix += strings.Repeat("#", section.level) + " " + limitRunes(section.heading, 240) + "\n\n"
		}
	}
	return prefix
}

func cutPlaybookText(text string, limit int) (string, string) {
	runes := []rune(text)
	cut := limit
	for i := limit - 1; i > limit/2; i-- {
		if runes[i] == '\n' {
			cut = i + 1
			break
		}
	}
	if cut == limit {
		for i := limit - 1; i > limit*3/4; i-- {
			if unicode.IsSpace(runes[i]) {
				cut = i + 1
				break
			}
		}
	}
	return string(runes[:cut]), string(runes[cut:])
}

func limitRunes(value string, maxRunes int) string {
	for i := range value {
		if maxRunes == 0 {
			return value[:i]
		}
		maxRunes--
	}
	return value
}
