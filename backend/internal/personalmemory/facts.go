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
	Category  string `json:"category"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Quote     string `json:"source_quote"`
	Uncertain bool   `json:"uncertain"`
}
type factOption struct {
	Option
	keyword       *regexp.Regexp
	demoStatement *regexp.Regexp
}

func option(category, key, value, text, keywords, demoPrefix string) factOption {
	term := `(?:` + keywords + `)`
	return factOption{
		Option{category, key, value, text},
		regexp.MustCompile(`(?i)(?:^|[^\pL\pN_])` + term + `(?:$|[^\pL\pN_])`),
		regexp.MustCompile(`(?i)(?:^|[.!?;\n,]\s*|\sи\s)((?:я\s+)?(?:` + demoPrefix + `)\s*` + term + `)(?:$|[^\pL\pN_])`),
	}
}

// Keywords identify a memory topic. The selector needs the whole turn to infer
// whose preference it is and whether it is current.
func eligibleSource(text string) bool {
	if !safeSource(text) {
		return false
	}
	for _, item := range catalog {
		if item.keyword.MatchString(text) {
			return true
		}
	}
	return false
}

const demoOSPrefix = `(?:работаю|сижу)\s+(?:на|в)\s+|у\s+меня\s+(?:на\s+(?:компьютере|ноутбуке)\s+)?(?:ос\s+|система\s+|стоит\s+|установлена\s+)?`
const demoShellPrefix = `(?:использую|работаю\s+в|мой\s+терминал)\s+`
const demoFormatPrefix = `предпочитаю\s+|объясняй\s+|мне\s+удобнее\s+`

var catalog = []factOption{
	option("work_environment", "os", "windows", "Работаю на Windows", `windows|виндоус|виндовс|винд[ауыое]`, demoOSPrefix),
	option("work_environment", "os", "linux", "Работаю на Linux", `linux|линукс[аеу]?|ubuntu|убунту`, demoOSPrefix),
	option("work_environment", "os", "macos", "Работаю на macOS", `mac\s?os|мак[аеу]?|макбук[аеу]?`, demoOSPrefix),
	option("work_environment", "shell", "powershell", "Использую PowerShell", `power\s?shell|пауэршелл?[ае]?|повершелл?[ае]?`, demoShellPrefix),
	option("work_environment", "shell", "bash", "Использую Bash", `bash|баш[ае]?`, demoShellPrefix),
	option("work_environment", "shell", "zsh", "Использую Zsh", `zsh`, demoShellPrefix),
	option("work_environment", "shell", "cmd", "Использую CMD", `cmd`, demoShellPrefix),
	option("instruction_format", "format", "steps", "Предпочитаю пошаговые инструкции", `пошагов[а-яё]+|по\s+шагам|шаг\s+за\s+шагом|step.by.step`, demoFormatPrefix),
	option("instruction_format", "format", "concise", "Предпочитаю краткие инструкции", `кратк[а-яё]+|коротк[а-яё]+|без\s+воды|brief|concise`, demoFormatPrefix),
	option("instruction_format", "format", "detailed", "Предпочитаю подробные инструкции", `подробн[а-яё]+|детальн[а-яё]+|detailed`, demoFormatPrefix),
	option("instruction_format", "format", "examples", "Предпочитаю инструкции с примерами", `(?:инструкции\s+)?с\s+пример(?:ами|ом)|на\s+примерах|with\s+examples`, demoFormatPrefix),
}

func init() {
	for _, tech := range []struct{ value, label, aliases string }{
		{"go", "Go", `golang|голанг[ае]?`},
		{"typescript", "TypeScript", `тайпскрипт[ае]?`},
		{"javascript", "JavaScript", `js|джаваскрипт[ае]?`},
		{"react", "React", `реакт[ае]?`},
		{"python", "Python", `п[аи]тон[ае]?`},
		{"java", "Java", `джава|джаве`},
		{"docker", "Docker", `докер[ае]?`},
		{"postgresql", "PostgreSQL", `postgres|постгрес[ае]?`},
		{"git", "Git", `гит[ае]?`},
		{"nodejs", "Node.js", `nodejs|нода|ноде`},
		{"kubernetes", "Kubernetes", `k8s|кубернетес[ае]?`},
	} {
		catalog = append(catalog, option("learning", "technology", tech.value, "Изучаю "+tech.label, regexp.QuoteMeta(tech.label)+`|`+tech.aliases, `(?:изучаю|учу|осваиваю|разбираюсь\s+с)\s+`))
	}
}

// Reject the whole source before model selection. Only canonical catalog values
// are ever persisted: neither model prose nor source/document text is a memory value.
var unsafeSource = regexp.MustCompile(`(?i)(парол|password|passwd|токен|token|secret|api[_ -]?key|bearer|ssh-|BEGIN .*KEY|паспорт|снилс|инн\b|зарплат|оклад|болезн|диагноз|беремен|отпуск|кадров|документ|инструкци[яию].*(?:цитат|закрыт)|цитат|согласно|игнорир|ignore|system|assistant|[\x00-\x08\x0b\x0c\x0e-\x1f<>` + "`" + `]|https?://|\S+@\S+|[0-9]{6})`)

var memoryOptOut = regexp.MustCompile(`(?i)(не\s+(?:сохраняй|сохранять|сохрани|запоминай|запоминать|запомни|записывай|записывать)|не\s+надо\s+(?:сохранять|запоминать|записывать)|до\s+закрытия\s+(?:вкладки|чата|диалога)|без\s+карточек|не\s+делай\s+вывод[а-я]*\s+о\s+моих\s+предпочтениях|передумал[аи]?|без\s+(?:сохранения|запоминания)|только\s+(?:для\s+(?:этого\s+(?:разговора|чата|диалога)|этой\s+беседы)|в\s+(?:этом\s+(?:диалоге|чате|разговоре)|этой\s+беседе))|в\s+других\s+(?:чатах|диалогах)\s+не\s+учитывай|(?:забудь|не\s+вспоминай)|останется\s+между\s+нами|do\s+not\s+(?:save|remember|store)|don.t\s+(?:save|remember|store))`)

var rememberPrevious = regexp.MustCompile(`(?i)^\s*(?:пожалуйста[, ]+)?(?:запомни(?:\s+это)?|сохрани\s+это\s+в\s+память)(?:[, ]+пожалуйста)?[.!]?\s*$`)

func safeSource(text string) bool {
	return utf8.RuneCountInString(text) <= 1000 && !memoryOptOut.MatchString(text) && !unsafeSource.MatchString(text)
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
			return item.keyword.MatchString(c.Quote) && item.keyword.MatchString(source)
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
			return item.keyword.MatchString(source)
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

func selectionText(text string) string {
	if !safeSource(text) {
		return ""
	}
	fragments := []string{}
	for _, item := range catalog {
		if match := item.demoStatement.FindStringSubmatch(text); match != nil {
			fragments = append(fragments, match[1])
			if len(fragments) == 4 {
				break
			}
		}
	}
	if len(fragments) == 0 {
		if match := regexp.MustCompile(`(?i)у коллеги Windows, мне тоже удобно`).FindString(text); match != "" {
			fragments = append(fragments, match)
		}
	}
	return strings.Join(fragments, "\n")
}
