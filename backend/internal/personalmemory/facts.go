// Package personalmemory stores only explicitly approved, source-grounded work preferences.
package personalmemory

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type Option struct {
	Category string `json:"category"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	Text     string `json:"text"`
}
type Candidate struct {
	Category string `json:"category"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	Quote    string `json:"source_quote"`
}
type factOption struct {
	Option
	expression *regexp.Regexp
}

func option(category, key, value, text, expression string) factOption {
	return factOption{Option{category, key, value, text}, regexp.MustCompile(`(?i)(?:^|[.!?;\n,]\s*|\sи\s)(?:я\s+)?(` + expression + `)(?:$|[\s.,!;?])`)}
}

// Only the catalog phrase itself crosses the selection boundary, without the
// source's surrounding prose, separators or arbitrary adjacent values.
func selectionText(text string) string {
	fragments := []string{}
	for _, item := range catalog {
		if loc := item.expression.FindStringSubmatchIndex(text); loc != nil {
			fragments = append(fragments, text[loc[2]:loc[3]])
			if len(fragments) == 4 {
				break
			}
		}
	}
	return strings.Join(fragments, "\n")
}

var catalog = []factOption{
	option("work_environment", "os", "windows", "Работаю на Windows", `(?:(?:работаю|сижу)\s+(?:на|в)|у\s+меня\s+(?:на\s+(?:компьютере|ноутбуке)\s+)?(?:система\s+|стоит\s+|установлена\s+)?)\s*(?:windows|виндоус|виндовс)`),
	option("work_environment", "os", "linux", "Работаю на Linux", `работаю\s+(?:на|в)\s+linux`),
	option("work_environment", "os", "macos", "Работаю на macOS", `работаю\s+(?:на|в)\s+(?:macos|маке)`),
	option("work_environment", "shell", "powershell", "Использую PowerShell", `(?:использую|работаю\s+в)\s+powershell`),
	option("work_environment", "shell", "bash", "Использую Bash", `(?:использую|работаю\s+в)\s+bash`),
	option("work_environment", "shell", "zsh", "Использую Zsh", `(?:использую|работаю\s+в)\s+zsh`),
	option("work_environment", "shell", "cmd", "Использую CMD", `(?:использую|работаю\s+в)\s+cmd`),
	option("instruction_format", "format", "steps", "Предпочитаю пошаговые инструкции", `предпочитаю\s+пошаговые\s+инструкции`),
	option("instruction_format", "format", "concise", "Предпочитаю краткие инструкции", `предпочитаю\s+краткие\s+инструкции`),
	option("instruction_format", "format", "detailed", "Предпочитаю подробные инструкции", `предпочитаю\s+подробные\s+инструкции`),
	option("instruction_format", "format", "examples", "Предпочитаю инструкции с примерами", `предпочитаю\s+инструкции\s+с\s+примерами`),
}

func init() {
	for _, tech := range []struct{ value, label string }{{"go", "Go"}, {"typescript", "TypeScript"}, {"javascript", "JavaScript"}, {"react", "React"}, {"python", "Python"}, {"java", "Java"}, {"docker", "Docker"}, {"postgresql", "PostgreSQL"}, {"git", "Git"}, {"nodejs", "Node.js"}, {"kubernetes", "Kubernetes"}} {
		catalog = append(catalog, option("learning", "technology", tech.value, "Изучаю "+tech.label, `изучаю\s+`+regexp.QuoteMeta(tech.label)))
	}
}

// Reject the whole source before model selection. Only canonical catalog values
// are ever persisted: neither model prose nor source/document text is a memory value.
var unsafeSource = regexp.MustCompile(`(?i)(парол|password|passwd|токен|token|secret|api[_ -]?key|bearer|ssh-|BEGIN .*KEY|паспорт|снилс|инн\b|зарплат|оклад|болезн|диагноз|беремен|отпуск|кадров|документ|инструкци[яию].*(?:цитат|закрыт)|цитат|согласно|коллег|не\s+(?:использую|работаю|изучаю|предпочитаю)|не\s+я|игнорир|ignore|system|assistant|[\x00-\x1f<>` + "`" + `]|https?://|\S+@\S+|[0-9]{6})`)

func safeSource(text string) bool {
	return utf8.RuneCountInString(text) <= 1000 && !unsafeSource.MatchString(strings.TrimSpace(text))
}
func findOption(key, value string) (Option, bool) {
	for _, item := range catalog {
		if item.Key == key && item.Value == value {
			return item.Option, true
		}
	}
	return Option{}, false
}
func validCandidate(c Candidate, source string) bool {
	if !safeSource(source) || c.Quote == "" || utf8.RuneCountInString(c.Quote) > 200 || !strings.Contains(source, c.Quote) {
		return false
	}
	for _, item := range catalog {
		if item.Key == c.Key && item.Value == c.Value && item.Category == c.Category {
			return item.expression.MatchString(c.Quote) && item.expression.MatchString(source)
		}
	}
	return false
}
func storedFactAllowed(category, key, value, source string) bool {
	if !safeSource(source) {
		return false
	}
	for _, item := range catalog {
		if item.Category == category && item.Key == key && item.Value == value {
			return item.expression.MatchString(source)
		}
	}
	return false
}
func slot(key, value string) string {
	if key == "technology" {
		return key + ":" + value
	}
	return key
}
func Options() []Option {
	out := make([]Option, 0, len(catalog))
	for _, item := range catalog {
		out = append(out, item.Option)
	}
	return out
}
