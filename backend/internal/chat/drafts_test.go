package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/drafts"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// This table/store exists ONLY in the test schema. #14 supplies the real request
// repository; these tests exercise real SQL commit/rollback across the seam.
type draftFixture struct {
	db       *sql.DB
	failLink bool
}
type draftTx struct {
	tx       *sql.Tx
	failLink bool
}

func (d draftFixture) WithinTx(ctx context.Context, fn func(drafts.Tx) error) error {
	return database.WithinTx(ctx, d.db, func(tx *sql.Tx) error { return fn(draftTx{tx, d.failLink}) })
}
func (d draftTx) UpsertDraft(ctx context.Context, p identity.Principal, in ports.DraftInput) (ports.Draft, error) {
	var valid bool
	if err := d.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM chat_messages m JOIN conversations c ON c.id=m.conversation_id
 WHERE m.id=$1 AND c.id=$2 AND c.user_id=$3 AND c.team_id=$4 AND c.step_id IS NOT DISTINCT FROM $5::text AND m.role='assistant' AND m.status='generating')`, in.AssistantMessageID, in.ConversationID, p.UserID, p.TeamID, in.StepID).Scan(&valid); err != nil {
		return ports.Draft{}, err
	}
	if !valid {
		return ports.Draft{}, ports.ErrNotFound
	}
	now := time.Now().UTC()
	draft := ports.Draft{DraftInput: in, Version: 1, Revision: 1, CreatedAt: now, UpdatedAt: now}
	state := ports.RequestState{Draft: draft, Status: "draft"}
	encoded, err := json.Marshal(state)
	if err != nil {
		return draft, err
	}
	_, err = d.tx.ExecContext(ctx, `INSERT INTO fixture_drafts(id,user_id,team_id,data) VALUES($1,$2,$3,$4)`, in.RequestID, p.UserID, p.TeamID, string(encoded))
	return draft, err
}
func (d draftTx) LinkAssistant(ctx context.Context, p identity.Principal, assistantID, requestID string) error {
	if err := LinkRequestTx(ctx, d.tx, p, assistantID, requestID); err != nil {
		return err
	}
	if d.failLink {
		return errors.New("forced error after link")
	}
	return nil
}
func (d draftFixture) Get(ctx context.Context, id string) (ports.RequestState, error) {
	p, err := principal(ctx)
	if err != nil {
		return ports.RequestState{}, err
	}
	var encoded []byte
	err = d.db.QueryRowContext(ctx, `SELECT data FROM fixture_drafts WHERE id=$1 AND user_id=$2 AND team_id=$3`, id, p.UserID, p.TeamID).Scan(&encoded)
	if err != nil {
		return ports.RequestState{}, sqlError(err)
	}
	var state ports.RequestState
	err = json.Unmarshal(encoded, &state)
	return state, err
}
func (d draftFixture) FindActive(context.Context, ports.AccessCheck) (ports.RequestState, error) {
	return ports.RequestState{}, ports.ErrNotFound
}
func setupDrafts(t *testing.T, f *fixture, failLink bool) draftFixture {
	t.Helper()
	if _, err := f.db.Exec(`CREATE TABLE fixture_drafts(id text PRIMARY KEY,user_id text NOT NULL,team_id text NOT NULL,data jsonb NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return draftFixture{f.db, failLink}
}
func draftInput(p Prepared, id string) ports.DraftInput {
	return ports.DraftInput{RequestID: id, ConversationID: p.Conversation.ID, AssistantMessageID: p.Assistant.ID, StepID: p.Conversation.StepID, Resource: ports.Resource{ID: "repo_demo_backend", Name: "Тестовый репозиторий", Permission: "read"}, Reason: "Настроить проект"}
}
func TestPostgresDraftLinkCommitRollbackAndAgentFailure(t *testing.T) {
	f := newFixture(t, fastAgent(), nil, StreamOptions{})
	c := f.create(t, 1)
	ctx := testContext()
	fixture := setupDrafts(t, f, true)
	coordinator := drafts.New(fixture)
	p, err := f.service.Prepare(ctx, c.ID, uid(2), "Нужен доступ")
	if err != nil {
		t.Fatal(err)
	}
	in := draftInput(p, "request-test")
	if _, err := coordinator.SaveAndLink(ctx, in); err == nil {
		t.Fatal("forced failure accepted")
	}
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM fixture_drafts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("draft escaped rollback", count, err)
	}
	messages, err := f.store.Messages(ctx, testOwner(), c.ID)
	if err != nil || len(messages[1].RequestIDs) != 0 {
		t.Fatal("link escaped rollback", messages, err)
	}
	fixture.failLink = false
	coordinator = drafts.New(fixture)
	svc := f.service
	svc.Requests = fixture
	svc.Agent = agentFunc(func(ctx context.Context, input ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
		saved, err := coordinator.SaveAndLink(ctx, in)
		if err != nil {
			return ports.AgentResult{}, err
		}
		// The callback must reload the committed version rather than trusting this.
		saved.Version = 999
		if err := emit(ctx, ports.AgentEvent{Kind: "draft.saved", Draft: &saved}); err != nil {
			return ports.AgentResult{}, err
		}
		if err := emit(ctx, ports.AgentEvent{Kind: "answer.delta", Text: "Подготовил черновик"}); err != nil {
			return ports.AgentResult{}, err
		}
		return ports.AgentResult{RequestIDs: []string{in.RequestID}, Sources: []ports.Source{{ID: "source", DocumentID: "doc", Title: "Инструкция", Excerpt: "Доступ"}}}, ports.ErrUnavailable
	})
	sink := newSink()
	if err := svc.Stream(ctx, p, sink, StreamOptions{}); err != nil {
		t.Fatal(err)
	}
	if !sink.contains("action.proposed") || !sink.contains("message.failed") {
		t.Fatal(sink.kinds)
	}
	for i, kind := range sink.kinds {
		if kind == "action.proposed" {
			proposed := sink.data[i].(map[string]any)["request"].(*accessRequest)
			if proposed.Version != 1 || proposed.Status != "draft" {
				t.Fatal("trusted emitted payload", proposed)
			}
		}
	}
	messages, err = f.store.Messages(ctx, testOwner(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	m := messages[1]
	if m.Status != "failed" || m.Error.Error.Code != "LLM_UNAVAILABLE" || len(m.RequestIDs) != 1 || len(m.Sources) != 1 || m.Text != "Подготовил черновик" {
		t.Fatal(m)
	}
	// A subsequent question receives authoritative snapshots even after failure.
	next, err := svc.Prepare(ctx, c.ID, uid(3), "Уточнение")
	if err != nil || len(next.Input.Requests) != 1 || next.Input.Requests[0].Version != 1 {
		t.Fatal(next, err)
	}
}
func TestPostgresProposalCannotInventLinkOrReuseSubmittedDraft(t *testing.T) {
	for _, status := range []string{"unlinked", "pending"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, fastAgent(), nil, StreamOptions{})
			c := f.create(t, 1)
			ctx := testContext()
			fixture := setupDrafts(t, f, false)
			p, err := f.service.Prepare(ctx, c.ID, uid(2), "Доступ")
			if err != nil {
				t.Fatal(err)
			}
			saved, err := drafts.New(fixture).SaveAndLink(ctx, draftInput(p, "r"))
			if err != nil {
				t.Fatal(err)
			}
			if status == "unlinked" {
				if _, err := f.db.Exec(`DELETE FROM chat_message_requests`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.db.Exec(`UPDATE fixture_drafts SET data=jsonb_set(data,'{Status}','"pending"'::jsonb)`); err != nil {
					t.Fatal(err)
				}
			}
			svc := f.service
			svc.Requests = fixture
			svc.Agent = agentFunc(func(ctx context.Context, _ ports.AgentInput, emit ports.Emit) (ports.AgentResult, error) {
				return ports.AgentResult{}, emit(ctx, ports.AgentEvent{Kind: "draft.saved", Draft: &saved})
			})
			sink := newSink()
			if err := svc.Stream(ctx, p, sink, StreamOptions{}); err != nil {
				t.Fatal(err)
			}
			if sink.contains("action.proposed") {
				t.Fatal("invalid proposal exposed")
			}
			if status == "unlinked" && !sink.contains("message.failed") {
				t.Fatal("missing link accepted")
			}
			if status == "pending" && !sink.contains("message.completed") {
				t.Fatal("submitted draft failed instead of skipping proposal")
			}
		})
	}
}
