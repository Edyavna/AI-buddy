package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/chat"
	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/onboarding"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresDemoChecklistRefresh(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := t.Context()
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	h := requestHandler(db, Dependencies{})
	intern := loginCookie(t, h, "intern_demo_1", nil)
	free := loginCookie(t, h, "intern_demo_unassigned", nil)
	freePlan := apiRequest(t, h, "GET", planURL, "", free, 200)
	var fixture onboarding.Plan
	data, err := os.ReadFile("../../../contracts/examples/full-demo-plan.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if got := decodePlan(t, freePlan); !reflect.DeepEqual(got, fixture) || len(got.Steps) != 16 {
		t.Fatalf("fixture differs from the seeded free intern plan: %s", freePlan.Body.String())
	}

	read := func() onboarding.Plan {
		return decodePlan(t, apiRequest(t, h, "GET", planURL, "", intern, 200))
	}
	const stepID = "plan_demo_1:team-setup"
	original := checkpointStep(t, read(), stepID)
	confirmed := decodePlan(t, apiRequest(t, h, "PUT", internStepsURL+stepID+"/confirmation",
		checkpointBody(true, original.Revision), intern, 200))
	before := checkpointStep(t, confirmed, stepID)
	if before.Status != "in_progress" || len(before.Confirmations) != 1 {
		t.Fatal(before)
	}
	w := apiRequest(t, h, "POST", "/api/v1/conversations",
		`{"client_conversation_id":"99999999-1111-4444-8888-000000000043","step_id":"`+stepID+`"}`, intern, 201)
	var conv chat.Conversation
	if err := json.Unmarshal(w.Body.Bytes(), &conv); err != nil {
		t.Fatal(err)
	}
	// Simulate content from an earlier catalog version with the same key.
	if _, err := db.ExecContext(ctx, `
UPDATE onboarding_steps SET title='Старый заголовок', instructions='Старые инструкции' WHERE id=$1;
`, stepID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO onboarding_step_requests(step_id,request_id,resource_id,permission,status)
VALUES($1,'catalog-request','repo_demo_backend','read','draft')`, stepID); err != nil {
		t.Fatal(err)
	}
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	afterPlan := read()
	after := checkpointStep(t, afterPlan, stepID)
	if len(afterPlan.Steps) != 17 || after.Title != original.Title || after.Instructions != original.Instructions ||
		after.Status != before.Status || after.Revision != before.Revision+1 ||
		!reflect.DeepEqual(before.Confirmations, after.Confirmations) || !reflect.DeepEqual(after.RequestIDs, []string{"catalog-request"}) {
		t.Fatal(after)
	}
	var linked string
	if err := db.QueryRowContext(ctx, "SELECT step_id FROM conversations WHERE id=$1", conv.ID).Scan(&linked); err != nil || linked != stepID {
		t.Fatal(linked, err)
	}
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	if repeated := read(); !reflect.DeepEqual(afterPlan, repeated) {
		t.Fatal("repeated import changed the plan")
	}
}

type checklistModel func(context.Context, []agent.Message) (agent.Message, error)

func (m checklistModel) Complete(ctx context.Context, messages []agent.Message) (agent.Message, error) {
	return m(ctx, messages)
}

func TestPostgresAgentUsesSelectedChecklistStep(t *testing.T) {
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
INSERT INTO onboarding_step_dependencies(plan_id, step_id, prerequisite_id)
VALUES ('plan_demo_1', 'plan_demo_1:meet-team', 'step_local_setup')`); err != nil {
		t.Fatal(err)
	}
	deps := demoDependencies(db, integrations.New(db), &config.AIConfig{})
	a := deps.Agent.(*agent.Agent)
	h := requestHandler(db, deps)
	intern := loginCookie(t, h, testkit.UserID, nil)
	foreign := `{"client_conversation_id":"99999999-1111-4444-8888-000000000049","step_id":"plan_demo_2:meet-team"}`
	apiRequest(t, h, "POST", "/api/v1/conversations", foreign, intern, 404)
	_, err := a.Run(testkit.Context(t.Context()), ports.AgentInput{
		ConversationID: "foreign", AssistantMessageID: "test", Text: "Чужой шаг",
		StepID: new("plan_demo_2:meet-team"),
	}, func(context.Context, ports.AgentEvent) error { return nil })
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	// The managed repository access flow is covered by TestPostgresAgentThroughHTTPProviders.
	for i, tc := range []struct {
		key, title, instructions, question, answer, policy string
		missing                                            []string
		closedChat                                         bool
	}{
		{"meet-team", "Познакомься с командой", "с которой будешь работать", "С чего начать знакомство?", "Познакомьтесь с командой на первой неделе.", "intern", []string{"intern"}, false},
		{"team-setup", "Организационные моменты", "Добавь стажёра", "Как попасть в закрытый чат?", "Попросите наставника добавить вас. Ссылки нет.", "both", []string{"intern", "mentor"}, true},
		{"midpoint-feedback", "На 1:1 обсуди", "промежуточные итоги", "Кто подтверждает этот шаг?", "Подтверждают стажёр и наставник.", "both", []string{"intern", "mentor"}, false},
	} {
		t.Run(tc.key, func(t *testing.T) {
			a.Model = checklistModel(func(_ context.Context, messages []agent.Message) (agent.Message, error) {
				last := messages[len(messages)-1]
				if last.Role == "user" {
					if !strings.Contains(messages[1].Content, tc.title) {
						return agent.Message{}, errors.New("selected checkpoint missing from model context")
					}
					return agent.Message{Calls: []agent.Call{{ID: "checkpoint", Type: "function",
						Function: agent.Function{Name: "get_my_tasks", Arguments: "{}"},
					}}}, nil
				}
				if tc.closedChat && last.Role == "tool" && last.CallID == "access" {
					if !strings.Contains(last.Content, "unavailable_reason") {
						return agent.Message{}, errors.New("unsupported access tool must explain why it cannot create a draft")
					}
					return agent.Message{Content: fmt.Sprintf("{\"answer\":%q,\"outcome\":\"answered\"}", tc.answer)}, nil
				}
				var c struct {
					Title, Instructions string
					Period              *string
					Resources           []onboarding.Resource
					Status              string
					BlockedReason       *string  `json:"blocked_reason"`
					Prerequisites       []string `json:"unfinished_prerequisites"`
					Policy              string   `json:"completion_policy"`
					Missing             []string `json:"missing_confirmations"`
				}
				if err := json.Unmarshal([]byte(last.Content), &c); err != nil || last.Role != "tool" ||
					!strings.HasPrefix(c.Title, tc.title) || !strings.Contains(c.Instructions, tc.instructions) ||
					c.Policy != tc.policy || c.Period == nil || !reflect.DeepEqual(c.Missing, tc.missing) {
					return agent.Message{}, fmt.Errorf("unexpected checkpoint tool data: %s", last.Content)
				}
				if tc.key == "meet-team" && (c.Status != "blocked" || c.BlockedReason == nil ||
					!reflect.DeepEqual(c.Prerequisites, []string{"Запустить проект"})) {
					return agent.Message{}, fmt.Errorf("checkpoint context missed an unfinished prerequisite: %s", last.Content)
				}
				if tc.closedChat && (len(c.Resources) < 1 || c.Resources[0].URL != nil ||
					c.Resources[0].Note == nil || !strings.Contains(*c.Resources[0].Note, "администратора чата")) {
					return agent.Message{}, errors.New("closed chat must have a contact and no invented URL")
				}
				if tc.closedChat {
					return agent.Message{Calls: []agent.Call{{ID: "access", Type: "function",
						Function: agent.Function{Name: "prepare_access_request", Arguments: "{\"resource_id\":\"repo_demo_backend\",\"permission\":\"read\",\"reason\":\"Закрытый чат\"}"},
					}}}, nil
				}
				return agent.Message{Content: fmt.Sprintf(`{"answer":%q,"outcome":"answered"}`, tc.answer)}, nil
			})
			body := fmt.Sprintf(`{"client_conversation_id":"99999999-1111-4444-8888-%012d","step_id":"plan_demo_1:%s"}`, 100+i, tc.key)
			w := apiRequest(t, h, "POST", "/api/v1/conversations", body, intern, 201)
			var conv chat.Conversation
			if err := json.Unmarshal(w.Body.Bytes(), &conv); err != nil {
				t.Fatal(err)
			}
			body = fmt.Sprintf(`{"client_message_id":"99999999-1111-4444-8888-%012d","text":%q}`, 200+i, tc.question)
			request := httptest.NewRequest("POST", "/api/v1/conversations/"+conv.ID+"/messages", strings.NewReader(body))
			request.Header.Set("Origin", "http://localhost:5173")
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(intern)
			w = httptest.NewRecorder()
			h.ServeHTTP(w, request)
			stream := w.Body.String()
			if w.Code != 200 || !strings.Contains(stream, "event: message.completed") || !strings.Contains(stream, tc.answer) || strings.Contains(stream, "action.proposed") {
				t.Fatal(stream)
			}
		})
	}
}
