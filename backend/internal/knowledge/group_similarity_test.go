package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"os"
	"strings"
	"testing"
)

func TestGroupSimilarityRules(t *testing.T) {
	for _, test := range []struct {
		a, b             string
		checkpoint, want bool
	}{
		{"как где это", "что или почему", false, false}, {"настроить сервер", "настроить VPN", false, false}, {"настроить сервер", "настроить VPN", true, true}, {"доступ сервис команда пароль инструкция", "доступ сервис команда пароль инструкция", false, true}, {"ежегодный отпуск", "локальная сборка", true, false},
		{"Ёлочный отпуск", "елочный отпуск", false, true},
	} {
		score, reason := SimilarQuestions(test.a, test.b, test.checkpoint)
		if score < 0 || score > 1 || (reason != "") != test.want {
			t.Fatal(test, score, reason)
		}
	}
	if _, reason := SimilarQuestions("настроить сервер", "настроить VPN", true); !strings.Contains(reason, "чекпоинт") {
		t.Fatal(reason)
	}
}
func TestGroupSimilarityEvaluation(t *testing.T) {
	raw, err := os.ReadFile("../../../demo/question-similarity-eval.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		A          string `json:"a"`
		B          string `json:"b"`
		Checkpoint bool   `json:"same_checkpoint"`
		Similar    bool   `json:"similar"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 30 {
		t.Fatal(len(cases))
	}
	tp, fp, fn, tn := 0, 0, 0, 0
	for _, item := range cases {
		score, reason := SimilarQuestions(item.A, item.B, item.Checkpoint)
		again, explanation := SimilarQuestions(item.A, item.B, item.Checkpoint)
		if score != again || reason != explanation {
			t.Fatal("not deterministic")
		}
		pred := reason != ""
		switch {
		case pred && item.Similar:
			tp++
		case pred:
			fp++
		case item.Similar:
			fn++
		default:
			tn++
		}
	}
	t.Logf("N=%d TP=%d FP=%d FN=%d TN=%d precision=%.2f%% recall=%.2f%%", len(cases), tp, fp, fn, tn, 100*float64(tp)/float64(tp+fp), 100*float64(tp)/float64(tp+fn))
}
func TestQuestionGroupIdentity(t *testing.T) {
	s := Uploads{}
	ctx := context.Background()
	if _, e := s.GroupWorkspace(ctx); !errors.Is(e, identity.ErrUnauthenticated) {
		t.Fatal(e)
	}
	if _, e := s.GetGroup(ctx, "x"); !errors.Is(e, identity.ErrUnauthenticated) {
		t.Fatal(e)
	}
	if _, e := s.SaveGroup(ctx, "x", GroupInput{}); !errors.Is(e, identity.ErrUnauthenticated) {
		t.Fatal(e)
	}
	if _, e := s.DeliverGroup(ctx, "x", 1, false); !errors.Is(e, identity.ErrUnauthenticated) {
		t.Fatal(e)
	}
}
