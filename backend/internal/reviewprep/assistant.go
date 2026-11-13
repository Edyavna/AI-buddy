package reviewprep

import (
	"context"
	"fmt"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
)

// DemoAssistant — детерминированный помощник без внешних вызовов: собирает
// резюме из сохранённых ответов и показывает пропущенные обязательные пункты.
// Живая LLM-модель подключается за этим же интерфейсом отдельной поставкой.
type DemoAssistant struct{}

func (DemoAssistant) Help(ctx context.Context, prep Preparation, template Template) (HelpResult, error) {
	if _, err := identity.Require(ctx); err != nil {
		return HelpResult{}, err
	}
	var missing []string
	for _, item := range template.Items {
		answer, ok := prep.Answers.Items[item.Key]
		if item.Required && (!ok || !answer.Done) {
			missing = append(missing, item.Label)
		}
	}
	if prep.Answers.Summary == "" {
		missing = append(missing, "Резюме: что сделано")
	}
	if prep.Answers.Verification == "" {
		missing = append(missing, "Как проверено")
	}
	if len(prep.Checks) == 0 {
		missing = append(missing, "Результаты проверок с источником")
	}

	var summary strings.Builder
	fmt.Fprintf(&summary, "## %s", safeTitle(prep))
	if prep.PR.Repo != "" {
		fmt.Fprintf(&summary, "\n\nРепозиторий `%s`, PR #%d.", prep.PR.Repo, prep.PR.Number)
	}
	if text := strings.TrimSpace(prep.Answers.Summary); text != "" {
		fmt.Fprintf(&summary, "\n\n### Что сделано\n%s", text)
	}
	if text := strings.TrimSpace(prep.Answers.Verification); text != "" {
		fmt.Fprintf(&summary, "\n\n### Как проверено\n%s", text)
	}
	passed, failed := 0, 0
	for _, check := range prep.Checks {
		if check.Status == "passed" {
			passed++
		}
		if check.Status == "failed" {
			failed++
		}
	}
	if len(prep.Checks) > 0 {
		fmt.Fprintf(&summary, "\n\n### Проверки\nУспешно: %d, с ошибками: %d.", passed, failed)
	}
	if text := strings.TrimSpace(prep.Answers.Limitations); text != "" {
		fmt.Fprintf(&summary, "\n\n### Ограничения\n%s", text)
	}
	if text := strings.TrimSpace(prep.Answers.Questions); text != "" {
		fmt.Fprintf(&summary, "\n\n### Вопросы ревьюеру\n%s", text)
	}
	if len(missing) > 0 {
		summary.WriteString("\n\n### Стоит дополнить\n")
		for _, item := range missing {
			fmt.Fprintf(&summary, "- %s\n", item)
		}
	}
	return HelpResult{Summary: strings.TrimSpace(summary.String()), Missing: missing}, nil
}

func safeTitle(prep Preparation) string {
	if title := strings.TrimSpace(prep.PR.Title); title != "" {
		return title
	}
	if prep.PR.Repo != "" {
		return fmt.Sprintf("PR #%d", prep.PR.Number)
	}
	return "Подготовка к ревью"
}

var _ Assistant = DemoAssistant{}
