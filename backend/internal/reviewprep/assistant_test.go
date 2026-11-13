package reviewprep_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/reviewprep"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestDemoAssistantHelp(t *testing.T) {
	ctx := testkit.Context(context.Background())
	prep := reviewprep.Preparation{
		PR: reviewprep.PRSnapshot{Repo: "o/r", Number: 9, Title: "Фича"},
		Answers: reviewprep.Answers{
			Summary:      "Сделал форму",
			Verification: "go test ./...",
			Questions:    "Норм ли имена?",
			Items: map[string]reviewprep.ItemAnswer{
				"task_done": {Done: true},
			},
		},
		Checks: []reviewprep.CheckResult{{Name: "go test", Status: "passed", Source: "intern"}},
	}
	result, err := reviewprep.DemoAssistant{}.Help(ctx, prep, reviewprep.Template{Items: reviewprep.DefaultTemplate})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "Фича") || !strings.Contains(result.Summary, "Сделал форму") {
		t.Fatalf("summary misses data: %q", result.Summary)
	}
	for _, want := range []string{"Добавлены или обновлены тесты", "Проверки CI проходят на head-коммите", "Нет секретов, отладочного кода и лишних файлов"} {
		found := false
		for _, missing := range result.Missing {
			if missing == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing item %q not reported: %v", want, result.Missing)
		}
	}
}

func TestDemoAssistantFullChecklistHasNoMissing(t *testing.T) {
	ctx := testkit.Context(context.Background())
	items := map[string]reviewprep.ItemAnswer{}
	for _, item := range reviewprep.DefaultTemplate {
		items[item.Key] = reviewprep.ItemAnswer{Done: true}
	}
	prep := reviewprep.Preparation{
		Answers: reviewprep.Answers{Summary: "s", Verification: "v", Items: items},
		Checks:  []reviewprep.CheckResult{{Name: "ci", Status: "passed", Source: "ci"}},
	}
	result, err := reviewprep.DemoAssistant{}.Help(ctx, prep, reviewprep.Template{Items: reviewprep.DefaultTemplate})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Missing) != 0 {
		t.Fatalf("expected no missing items, got %v", result.Missing)
	}
}

func TestDemoAssistantRequiresPrincipal(t *testing.T) {
	_, err := reviewprep.DemoAssistant{}.Help(context.Background(), reviewprep.Preparation{}, reviewprep.Template{})
	if err == nil {
		t.Fatal("expected unauthenticated error")
	}
	mentorCtx := identity.WithPrincipal(context.Background(), identity.Principal{UserID: "m", TeamID: "t", Role: identity.Mentor})
	if _, err := (reviewprep.DemoAssistant{}).Help(mentorCtx, reviewprep.Preparation{}, reviewprep.Template{}); err != nil {
		t.Fatal(err) // помощник читает данные стажёра и доступен любому аутентифицированному пользователю
	}
}
