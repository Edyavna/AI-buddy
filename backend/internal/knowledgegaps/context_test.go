package knowledgegaps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type contextFixture struct {
	s               Store
	student, mentor context.Context
	q               Question
	input           *ContextInput
}

func newContextFixture(t *testing.T) contextFixture {
	t.Helper()
	db := testkit.Postgres(t)
	ctx := t.Context()
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO conversations(id,user_id,team_id,client_conversation_id,step_id,title) VALUES('ctx-conv','intern_demo_1','team_demo_backend','11111111-1111-4111-8111-111111111111','step_local_setup','Тест контекста')`,
		`INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,status,text,trace_id) VALUES('ctx-user','ctx-conv',1,'22222222-2222-4222-8222-222222222222','user','completed','Проверил настройки','test')`,
		`INSERT INTO knowledge_documents(id,source,source_id,title,content,updated_at,team_id,users,hash,direction,version) VALUES('ctx-doc','demo','ctx-doc','Исходная инструкция','Доступный текст',now(),'team_demo_backend','[]','test','mentor',1)`,
		`INSERT INTO chat_messages(id,conversation_id,sequence,role,status,text,in_reply_to_message_id,sources,trace_id) VALUES('ctx-assistant','ctx-conv',2,'assistant','completed','Проверьте доступ по инструкции','ctx-user','[{"document_id":"ctx-doc","version":1}]','test')`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	student := identity.WithPrincipal(ctx, identity.Principal{UserID: "intern_demo_1", TeamID: "team_demo_backend", Role: identity.Intern})
	mentor := identity.WithPrincipal(ctx, identity.Principal{UserID: "mentor_demo_1", TeamID: "team_demo_backend", Role: identity.Mentor})
	var revision int64
	if err := db.QueryRow(`SELECT revision FROM onboarding_plans WHERE user_id='intern_demo_1'`).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	in := &ContextInput{Reviewed: true, IncludeStep: true, StepRevision: &revision, Attempts: "Мои действия", Fragments: []ContextFragmentInput{{MessageID: "ctx-user", Text: "Проверил настройки"}, {MessageID: "ctx-assistant", Text: "Проверьте доступ по инструкции"}}, Documents: []ContextDocumentInput{{ID: "ctx-doc", Version: 1}}}
	s := Store{DB: db}
	q, err := s.Create(student, "ctx-assistant", Operation{ID: "33333333-3333-4333-8333-333333333333", Draft: Draft{Direction: "mentor", Question: "Уточните порядок", ContextSnapshot: in}})
	if err != nil {
		t.Fatal(err)
	}
	return contextFixture{s, student, mentor, q, in}
}
func execContext(t *testing.T, f contextFixture, query string, args ...any) {
	t.Helper()
	if _, err := f.s.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func requireUnavailable(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ports.ErrUnavailable) {
		t.Fatalf("dependency error must not become an empty or unauthorized result: %v", err)
	}
}

func TestPostgresContextReferencesAndCorruptMetadata(t *testing.T) {
	f := newContextFixture(t)
	q := f.q
	q.Revision--
	if err := f.s.filterContext(f.mentor, identity.Principal{UserID: "mentor_demo_1", TeamID: "team_demo_backend", Role: identity.Mentor}, &q); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
	q = f.q
	q.ID = "absent"
	if err := f.s.filterContext(f.mentor, identity.Principal{UserID: "mentor_demo_1", TeamID: "team_demo_backend", Role: identity.Mentor}, &q); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(f.q.ContextSnapshot)
	execContext(t, f, `UPDATE knowledge_gaps SET context_snapshot=NULL WHERE id=$1`, f.q.ID)
	q = f.q
	if err := f.s.filterContext(f.mentor, identity.Principal{UserID: "mentor_demo_1", TeamID: "team_demo_backend", Role: identity.Mentor}, &q); err != nil || q.ContextSnapshot != nil {
		t.Fatal(q, err)
	}
	if _, err := f.s.ContextDocument(f.mentor, f.q.ID, "ctx-doc"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	execContext(t, f, `UPDATE knowledge_gaps SET context_snapshot=$2 WHERE id=$1`, f.q.ID, string(raw))
	execContext(t, f, `UPDATE conversations SET step_id=NULL WHERE id='ctx-conv'`)
	q, err := f.s.Get(f.mentor, f.q.ID)
	if err != nil || q.ContextSnapshot.Step.Title != nil {
		t.Fatal(q, err)
	}
	execContext(t, f, `UPDATE conversations SET step_id='step_local_setup' WHERE id='ctx-conv'`)
	execContext(t, f, `UPDATE conversations SET user_id='intern_demo_2' WHERE id='ctx-conv'`)
	_, err = f.s.Get(f.mentor, f.q.ID)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	execContext(t, f, `UPDATE conversations SET user_id='intern_demo_1' WHERE id='ctx-conv'`)
	execContext(t, f, `ALTER TABLE onboarding_steps ALTER COLUMN title DROP NOT NULL; UPDATE onboarding_steps SET title=NULL WHERE id='step_local_setup'`)
	_, err = f.s.ContextPreview(f.student, "ctx-assistant", "mentor")
	requireUnavailable(t, err)
	_, err = f.s.Get(f.mentor, f.q.ID)
	requireUnavailable(t, err)
	_, err = f.s.SaveDraft(f.student, "ctx-assistant", Draft{Direction: "mentor", Question: "Проверка недоступного шага", ContextSnapshot: f.input})
	requireUnavailable(t, err)
	execContext(t, f, `UPDATE onboarding_steps SET title='Восстановленный шаг' WHERE id='step_local_setup'; ALTER TABLE onboarding_steps ALTER COLUMN title SET NOT NULL`)
	execContext(t, f, `UPDATE chat_messages SET sources='["invalid-source-shape"]' WHERE id='ctx-assistant'`)
	_, err = f.s.ContextPreview(f.student, "ctx-assistant", "mentor")
	requireUnavailable(t, err)
	_, err = f.s.Get(f.mentor, f.q.ID)
	requireUnavailable(t, err)
	_, err = f.s.SaveDraft(f.student, "ctx-assistant", Draft{Direction: "mentor", Question: "Проверка", ContextSnapshot: f.input})
	requireUnavailable(t, err)
	execContext(t, f, `UPDATE chat_messages SET sources='[{"document_id":"ctx-doc","version":1}]' WHERE id='ctx-assistant'`)
	_, err = f.s.SaveDraft(f.student, "ctx-assistant", Draft{Direction: "mentor", Question: "Сохраняемый черновик", ContextSnapshot: f.input})
	if err != nil {
		t.Fatal(err)
	}
	execContext(t, f, `UPDATE question_drafts SET context_snapshot='{"attempts":123}'`)
	_, err = f.s.MessageQuestions(f.student, "ctx-assistant")
	requireUnavailable(t, err)
	execContext(t, f, `DELETE FROM question_drafts; UPDATE knowledge_gaps SET context_snapshot='{"attempts":123}'`)
	_, err = f.s.Get(f.mentor, f.q.ID)
	requireUnavailable(t, err)
	_, err = f.s.List(f.mentor, "")
	requireUnavailable(t, err)
	_, err = f.s.ContextDocument(f.mentor, f.q.ID, "ctx-doc")
	requireUnavailable(t, err)
	execContext(t, f, `UPDATE knowledge_gaps SET context_snapshot=$2 WHERE id=$1`, f.q.ID, string(raw))
	// Missing storage must propagate, never silently label an unconfirmed opening.
	execContext(t, f, `ALTER TABLE question_document_opens RENAME TO temporarily_unavailable_opens`)
	_, err = f.s.ContextPreview(f.student, "ctx-assistant", "mentor")
	requireUnavailable(t, err)
	err = f.s.RecordContextOpen(f.student, "ctx-assistant", "ctx-doc", 1)
	requireUnavailable(t, err)
	execContext(t, f, `ALTER TABLE temporarily_unavailable_opens RENAME TO question_document_opens`)
	execContext(t, f, `ALTER TABLE hr_documents RENAME TO temporarily_unavailable_hr_documents`)
	_, err = f.s.OpenContextDocument(f.student, "ctx-assistant", "ctx-doc")
	requireUnavailable(t, err)
	execContext(t, f, `ALTER TABLE temporarily_unavailable_hr_documents RENAME TO hr_documents`)
	execContext(t, f, `ALTER TABLE knowledge_documents RENAME TO temporarily_unavailable_documents`)
	execContext(t, f, `UPDATE knowledge_gaps SET context_snapshot=jsonb_set(context_snapshot,'{fragments}',jsonb_build_array(context_snapshot->'fragments'->0))`)
	_, err = f.s.Get(f.mentor, f.q.ID)
	requireUnavailable(t, err)
	userOnly := *f.input
	userOnly.Fragments = userOnly.Fragments[:1]
	_, err = f.s.SaveDraft(f.student, "ctx-assistant", Draft{Direction: "mentor", Question: "Контекст при недоступном индексе", ContextSnapshot: &userOnly})
	requireUnavailable(t, err)
	_, err = f.s.OpenContextDocument(f.student, "ctx-assistant", "ctx-doc")
	requireUnavailable(t, err)
	_, err = f.s.ContextDocument(f.mentor, f.q.ID, "ctx-doc")
	requireUnavailable(t, err)
	execContext(t, f, `UPDATE chat_messages SET status='failed',error='{"error":{"code":"MODEL_ERROR"}}' WHERE id='ctx-assistant'`)
	_, err = f.s.ContextPreview(f.student, "ctx-assistant", "mentor")
	requireUnavailable(t, err)
	execContext(t, f, `ALTER TABLE temporarily_unavailable_documents RENAME TO knowledge_documents`)
	execContext(t, f, `UPDATE chat_messages SET status='completed',error=NULL WHERE id='ctx-assistant'`)
	execContext(t, f, `UPDATE knowledge_gaps SET context_snapshot=$2 WHERE id=$1`, f.q.ID, string(raw))
	q, err = f.s.Get(f.mentor, f.q.ID)
	if err != nil || q.ContextSnapshot.Fragments[1].Text == nil {
		t.Fatal(q, err)
	}
}

func TestContextDocumentsRejectUnsupportedRolesWithoutTouchingStorage(t *testing.T) {
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "unknown", Role: identity.Role("other")})
	if _, err := (Store{}).ContextDocument(ctx, "x", "y"); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
}

func TestPostgresContextCanceledScopeChecksFailClosed(t *testing.T) {
	f := newContextFixture(t)
	for name, call := range map[string]func(context.Context) error{
		"preview": func(ctx context.Context) error { _, e := f.s.ContextPreview(ctx, "ctx-assistant", "mentor"); return e },
		"draft": func(ctx context.Context) error {
			_, e := f.s.SaveDraft(ctx, "ctx-assistant", Draft{Direction: "mentor", Question: "Контекст", ContextSnapshot: f.input})
			return e
		},
		"create": func(ctx context.Context) error {
			_, e := f.s.Create(ctx, "ctx-assistant", Operation{ID: "44444444-4444-4444-8444-444444444444", Draft: Draft{Direction: "mentor", Question: "Контекст", ContextSnapshot: f.input}})
			return e
		},
		"reroute": func(ctx context.Context) error {
			_, e := f.s.Reroute(ctx, f.q.ID, Operation{ID: "55555555-5555-4555-8555-555555555555", Revision: f.q.Revision, Draft: Draft{Direction: "hr", Question: "Новая часть"}})
			return e
		},
		"read":     func(ctx context.Context) error { _, e := f.s.History(ctx, f.q.ID); return e },
		"document": func(ctx context.Context) error { _, e := f.s.ContextDocument(ctx, f.q.ID, "ctx-doc"); return e },
		"opening":  func(ctx context.Context) error { return f.s.RecordContextOpen(ctx, "ctx-assistant", "ctx-doc", 1) },
	} {
		t.Run(name, func(t *testing.T) {
			hold, err := f.s.DB.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Rollback()
			if err = hrscope.LockTeam(t.Context(), hold, "team_demo_backend"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(f.student)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- call(ctx) }()
			deadline := time.Now().Add(3 * time.Second)
			for {
				var count int
				if err = f.s.DB.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory'`).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("operation did not reach the scope lock")
				}
				time.Sleep(time.Millisecond)
			}
			if name == "create" {
				// A sender waiting for the team lock must not already own a message
				// row: draft saves take team -> message, and would deadlock otherwise.
				var found string
				if err = hold.QueryRowContext(t.Context(), `SELECT id FROM chat_messages WHERE id='ctx-assistant' FOR UPDATE NOWAIT`).Scan(&found); err != nil {
					t.Fatalf("send locked message before the team scope: %v", err)
				}
			}
			cancel()
			select {
			case err = <-done:
				requireUnavailable(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("canceled context check did not terminate")
			}
		})
	}
	q, err := f.s.Get(f.mentor, f.q.ID)
	if err != nil || q.Direction != "mentor" || q.Revision != f.q.Revision {
		t.Fatal(q, err)
	}
}

func TestPostgresContextWindowLimitsAndInvalidSelections(t *testing.T) {
	f := newContextFixture(t)
	execContext(t, f, `UPDATE chat_messages SET sequence=1000 WHERE id='ctx-assistant';
 INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,status,text,trace_id) SELECT 'ctx-user-'||n,'ctx-conv',n*2,gen_random_uuid(),'user','completed','Вопрос из реального сохранённого диалога','test' FROM generate_series(2,12)n;
 INSERT INTO chat_messages(id,conversation_id,sequence,role,status,text,in_reply_to_message_id,trace_id) SELECT 'ctx-assistant-'||n,'ctx-conv',n*2+1,'assistant','completed','Ответ из диалога','ctx-user-'||n,'test' FROM generate_series(2,12)n;
 INSERT INTO knowledge_documents(id,source,source_id,title,content,updated_at,team_id,users,hash,direction,version) SELECT 'ctx-extra-'||n,'demo','ctx-extra-'||n,'Инструкция '||n,'Текст',now(),'team_demo_backend','[]','test','mentor',1 FROM generate_series(1,11)n;
 UPDATE chat_messages SET sources=(SELECT jsonb_agg(jsonb_build_object('document_id','ctx-extra-'||n,'version',1)) FROM generate_series(1,11)n) WHERE id='ctx-assistant'`)
	view, err := f.s.ContextPreview(f.student, "ctx-assistant", "mentor")
	if err != nil || !view.Limited || len(view.Fragments) != 12 || len(view.Documents) != 10 {
		t.Fatal(view, err)
	}
	for _, change := range []func(*ContextInput){func(c *ContextInput) { c.Fragments = nil }, func(c *ContextInput) { c.Documents = nil }, func(c *ContextInput) { c.StepRevision = nil }, func(c *ContextInput) { c.Attempts = "bad\x00text" }, func(c *ContextInput) { c.Fragments = []ContextFragmentInput{{MessageID: "", Text: "x"}} }, func(c *ContextInput) { c.Fragments = make([]ContextFragmentInput, 13) }, func(c *ContextInput) { c.Documents = make([]ContextDocumentInput, 11) }, func(c *ContextInput) { c.Documents = []ContextDocumentInput{{ID: "", Version: 1}} }} {
		raw, _ := json.Marshal(f.input)
		var in ContextInput
		_ = json.Unmarshal(raw, &in)
		change(&in)
		_, err = f.s.SaveDraft(f.student, "ctx-assistant", Draft{Direction: "mentor", Question: "Проверка", ContextSnapshot: &in})
		if !errors.Is(err, ports.ErrInvalid) {
			t.Fatal(err)
		}
	}
	tx, err := f.s.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	student, _ := identity.Require(f.student)
	if _, _, err = fragmentSource(t.Context(), tx, contextRoot{conversation: "ctx-conv", sequence: 1000}, student, student, "mentor", "ctx-user"); !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(err)
	}
	if _, err = buildContext(t.Context(), tx, student, student, "ctx-assistant", "mentor", f.input); !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(err)
	}
	if err = sanitizeContextDraft(t.Context(), tx, student, "ctx-assistant", &Draft{Direction: "mentor", ContextSnapshot: f.input}); !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(err)
	}
}
