package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type outcomeKnowledge struct {
	hits               []ports.Source
	searchErr, readErr error
}

func (k outcomeKnowledge) Search(context.Context, string) ([]ports.Source, error) {
	return k.hits, k.searchErr
}
func (k outcomeKnowledge) Read(_ context.Context, id string) (ports.Document, error) {
	return ports.Document{ID: id, Title: "Локальный запуск", Content: "Выполните docker compose up."}, k.readErr
}

func TestOutcomeFixtures(t *testing.T) {
	hits := []ports.Source{{DocumentID: "setup", Title: "Локальный запуск"}}
	search := "search_knowledge"
	read := "read_document"
	gap := ports.OutcomeKnowledgeGap
	answered := ports.OutcomeAnswered
	clarify := ports.OutcomeNeedsClarification
	for _, tc := range []struct {
		question           string
		calls              []string
		knowledge          outcomeKnowledge
		modelOutcome, want ports.Outcome
	}{
		{"Как запустить сервис локально?", []string{search, read}, outcomeKnowledge{hits: hits}, answered, answered},
		{"Как поднять бэкенд на ноутбуке?", []string{search, read}, outcomeKnowledge{hits: hits}, answered, answered},
		{"Какая первая задача?", []string{"get_my_tasks"}, outcomeKnowledge{}, answered, answered},
		{"Привет!", nil, outcomeKnowledge{}, answered, answered},
		{"Дайте доступ", nil, outcomeKnowledge{}, clarify, clarify},
		{"Где это посмотреть?", nil, outcomeKnowledge{}, clarify, clarify},
		{"Где правила дежурств?", []string{search}, outcomeKnowledge{}, gap, gap},
		{"Как настроить TLS у локального сервиса?", []string{search, read}, outcomeKnowledge{hits: hits}, gap, gap},
		{"Где инструкция закрытой команды?", []string{search}, outcomeKnowledge{}, gap, gap},
		{"Где инструкция при сбое эмбеддингов?", []string{search}, outcomeKnowledge{searchErr: ports.ErrUnavailable}, gap, ""},
		{"Где инструкция при сбое чтения?", []string{search, read}, outcomeKnowledge{hits: hits, readErr: ports.ErrUnavailable}, gap, ""},
		{"Где инструкция при сбое модели?", []string{search}, outcomeKnowledge{}, "", ""},
		{"Где инструкция без обращения к поиску?", nil, outcomeKnowledge{}, gap, ""},
		{"Что сказано в непрочитанном результате?", []string{search}, outcomeKnowledge{hits: hits}, gap, ""},
		{"Что вернёт агент при неизвестном outcome?", nil, outcomeKnowledge{}, ports.OutcomeUnknown, ""},
	} {
		t.Run(tc.question, func(t *testing.T) {
			a, in := fixture()
			a.Knowledge, in.Text = tc.knowledge, tc.question
			var replies []Message
			query, _ := json.Marshal(map[string]string{"query": tc.question})
			for _, name := range tc.calls {
				args := `{}`
				switch name {
				case search:
					args = string(query)
				case read:
					args = `{"document_id":"setup"}`
				}
				replies = append(replies, call(name, args))
			}
			if tc.modelOutcome != "" {
				data, _ := json.Marshal(map[string]string{"answer": "Ответ для стажёра", "outcome": string(tc.modelOutcome)})
				replies = append(replies, Message{Content: string(data)})
			}
			a.Model = &Script{Replies: replies}
			var answer string
			out, err := a.Run(testkit.Context(context.Background()), in, func(_ context.Context, e ports.AgentEvent) error {
				if e.Kind == "answer.delta" {
					answer += e.Text
				}
				return nil
			})
			if out.Outcome != tc.want || (err != nil) != (tc.want == "") || (answer == "") != (tc.want == "") {
				t.Fatalf("outcome=%q answer=%q err=%v", out.Outcome, answer, err)
			}
		})
	}
}
