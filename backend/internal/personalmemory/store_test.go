package personalmemory

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type selectFunc func(context.Context, string) ([]Candidate, error)

func (f selectFunc) Select(ctx context.Context, text string) ([]Candidate, error) {
	return f(ctx, text)
}
func memoryFixture(t *testing.T) (Store, context.Context) {
	t.Helper()
	db := testkit.Postgres(t)
	if err := demo.Seed(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO conversations(id,user_id,team_id,client_conversation_id,title) VALUES('memory-conversation','intern_demo_1','team_demo_backend','79111111-1111-4111-8111-111111111111','Память');INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,status,text,trace_id) VALUES('memory-user','memory-conversation',1,'79222222-2222-4222-8222-222222222222','user','completed','Я работаю на Windows и использую PowerShell.','memory')`)
	if err != nil {
		t.Fatal(err)
	}
	return Store{DB: db, Selector: Model{Demo: true}}, identity.WithPrincipal(t.Context(), identity.Principal{UserID: "intern_demo_1", TeamID: "team_demo_backend", Role: identity.Intern})
}

func TestPostgresSelectionSendsWholeSourceAndSkipsUnmatchedMessages(t *testing.T) {
	s, ctx := memoryFixture(t)
	const original = "rTqLmNpVxYzAwBcDeFhJk. Использую PowerShell. Приватный фрагмент рядом."
	mustSQL(t, s, `UPDATE chat_messages SET text=$1 WHERE id='memory-user'`, original)
	calls := 0
	s.Selector = selectFunc(func(_ context.Context, input string) ([]Candidate, error) {
		calls++
		if input != original {
			t.Error("selector did not receive the whole turn", input)
		}
		return []Candidate{{Category: "work_environment", Key: "shell", Value: "powershell", Quote: "Использую PowerShell"}}, nil
	})
	v, err := s.Suggest(ctx, "memory-user")
	if err != nil || v.State != "ready" || len(v.Items) != 1 {
		t.Fatal(v, err)
	}
	var saved string
	if err := s.DB.QueryRow(`SELECT text FROM chat_messages WHERE id='memory-user'`).Scan(&saved); err != nil || saved != original {
		t.Fatal("source was rewritten", saved, err)
	}
	w, err := s.Workspace(ctx)
	if err != nil || len(w.Entries) != 0 {
		t.Fatal("selection confirmed memory automatically", w, err)
	}
	mustSQL(t, s, `DELETE FROM memory_proposals; DELETE FROM memory_extractions; UPDATE chat_messages SET text='rTqLmNpVxYzAwBcDeFhJk' WHERE id='memory-user'`)
	v, err = s.Suggest(ctx, "memory-user")
	if err != nil || v.State != "skipped" || len(v.Items) != 0 || calls != 1 {
		t.Fatal("unmatched source reached selector", v, calls, err)
	}
}

func TestPostgresRememberFollowUpUsesPreviousUserTurn(t *testing.T) {
	s, ctx := memoryFixture(t)
	mustSQL(t, s, `UPDATE chat_messages SET text='У коллеги Windows, мне тоже удобно' WHERE id='memory-user';
		INSERT INTO chat_messages(id,conversation_id,sequence,role,status,in_reply_to_message_id,text,trace_id)
		VALUES('memory-answer','memory-conversation',2,'assistant','completed','memory-user','Уточните предпочтение','memory');
		INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,status,text,trace_id)
		VALUES ('memory-follow-up','memory-conversation',3,gen_random_uuid(),'user','completed','Запомни это.','memory'),
		('memory-opt-out','memory-conversation',5,gen_random_uuid(),'user','completed','Я использую Bash. Не сохраняй.','memory'),
		('memory-after-opt-out','memory-conversation',7,gen_random_uuid(),'user','completed','Запомни это','memory');
		INSERT INTO memory_extractions(message_id,user_id,team_id,state) VALUES('memory-user','intern_demo_1','team_demo_backend','skipped')`)
	p := mustSuggest(t, s, ctx, "memory-follow-up", "os")
	if p.SourceMessageID != "memory-user" || p.ConversationID != "memory-conversation" || !p.Uncertain || p.State != "pending" {
		t.Fatal(p)
	}
	s.Selector = selectFunc(func(context.Context, string) ([]Candidate, error) {
		t.Fatal("selector called again or on an opted-out turn")
		return nil, nil
	})
	for _, id := range []string{"memory-follow-up", "memory-user"} {
		if got := mustSuggest(t, s, ctx, id, "os"); got.ID != p.ID || !got.Uncertain {
			t.Fatal(got)
		}
	}
	if out, err := s.Suggest(ctx, "memory-after-opt-out"); err != nil || out.State != "skipped" || len(out.Items) != 0 {
		t.Fatal(out, err)
	}
	workspace, err := s.Workspace(ctx)
	if err != nil || len(workspace.Entries) != 0 || len(workspace.Pending) != 1 {
		t.Fatal(workspace, err)
	}
	if e := accept(t, s, ctx, p); e.SourceMessageID != "memory-user" || e.Value != "windows" {
		t.Fatal(e)
	}
	if got := mustSuggest(t, s, ctx, "memory-follow-up", "os"); got.ID != p.ID || got.State != "accepted" {
		t.Fatal(got)
	}
}

func TestPostgresMemoryCannotUseMentorOnlyMarkdownSources(t *testing.T) {
	s, ctx := memoryFixture(t)
	p := mustSuggest(t, s, ctx, "memory-user", "shell")
	e := accept(t, s, ctx, p)
	// Archiving does not revoke access. Mentor-only mode does, even for the author.
	mustSQL(t, s, `UPDATE conversations SET archived=true WHERE id='memory-conversation'`)
	if _, err := s.GetEntry(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	mustSQL(t, s, `UPDATE conversations SET markdown=true WHERE id='memory-conversation'`)
	for name, check := range map[string]func() error{
		"source":       func() error { _, err := s.Proposals(ctx, "memory-user"); return err },
		"selection":    func() error { _, err := s.Suggest(ctx, "memory-user"); return err },
		"confirmation": func() error { _, err := s.Decide(ctx, p.ID, Decision{Decision: "accept"}); return err },
		"entry":        func() error { _, err := s.GetEntry(ctx, e.ID); return err },
		"edit":         func() error { _, err := s.Edit(ctx, e.ID, "bash", e.Revision); return err },
		"delete":       func() error { return s.Delete(ctx, e.ID, e.Revision) },
	} {
		if err := check(); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("%s: mentor-only source was accessible: %v", name, err)
		}
	}
	w, err := s.Workspace(ctx)
	if err != nil || len(w.Entries) != 0 || len(w.Pending) != 0 || strings.Contains(w.Markdown, "PowerShell") {
		t.Fatal("inaccessible source leaked into workspace or model profile", w, err)
	}
	if profile, err := s.Profile(ctx); err != nil || strings.Contains(profile, "PowerShell") {
		t.Fatal("inaccessible source leaked into next model request", profile, err)
	}
}
func mustSQL(t *testing.T, s Store, query string, args ...any) {
	t.Helper()
	if _, err := s.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func mustSuggest(t *testing.T, s Store, ctx context.Context, id, key string) Proposal {
	t.Helper()
	v, e := s.Suggest(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range v.Items {
		if p.Key == key {
			return p
		}
	}
	t.Fatal(v)
	return Proposal{}
}
func accept(t *testing.T, s Store, ctx context.Context, p Proposal) Entry {
	t.Helper()
	in := Decision{Decision: "accept"}
	if p.Replaces != nil {
		in.ExpectedEntryID = &p.Replaces.ID
		in.ExpectedRevision = p.Replaces.Revision
	}
	r, e := s.Decide(ctx, p.ID, in)
	if e != nil || r.Entry == nil {
		t.Fatal(r, e)
	}
	return *r.Entry
}

func TestPostgresMemoryApprovalEditDeleteAndReplay(t *testing.T) {
	s, ctx := memoryFixture(t)
	p := mustSuggest(t, s, ctx, "memory-user", "shell")
	w, e := s.Workspace(ctx)
	if e != nil || len(w.Entries) != 0 || len(w.Pending) != 2 || strings.Contains(w.Markdown, "PowerShell") {
		t.Fatal(w, e)
	}
	s.Selector = selectFunc(func(context.Context, string) ([]Candidate, error) {
		t.Fatal("ready extraction must not be regenerated")
		return nil, nil
	})
	if v, e := s.Suggest(ctx, "memory-user"); e != nil || len(v.Items) != 2 {
		t.Fatal(v, e)
	}
	var wg sync.WaitGroup
	results := make(chan Entry, 2)
	for range 2 {
		wg.Go(func() { results <- accept(t, s, ctx, p) })
	}
	wg.Wait()
	close(results)
	var stored Entry
	for v := range results {
		if stored.ID != "" && stored.ID != v.ID {
			t.Fatal("duplicate confirmation created another entry")
		}
		stored = v
	}
	w, e = s.Workspace(ctx)
	if e != nil || len(w.Entries) != 1 || len(w.Pending) != 1 || !strings.Contains(w.Markdown, "PowerShell") {
		t.Fatal(w, e)
	}
	other := w.Pending[0]
	r, e := s.Decide(ctx, other.ID, Decision{Decision: "dismiss"})
	if e != nil || r.Proposal.State != "dismissed" || r.Entry != nil {
		t.Fatal(r, e)
	}
	if _, e = s.Decide(ctx, other.ID, Decision{Decision: "dismiss"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Decide(ctx, other.ID, Decision{Decision: "accept"}); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
	if _, e = s.Decide(ctx, p.ID, Decision{Decision: "dismiss"}); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
	if _, e = s.Edit(ctx, stored.ID, "token_secret", 1); !errors.Is(e, ErrRejected) {
		t.Fatal(e)
	}
	unchanged, e := s.Edit(ctx, stored.ID, "powershell", 1)
	if e != nil || unchanged.Revision != 1 {
		t.Fatal(unchanged, e)
	}
	stored, e = s.Edit(ctx, stored.ID, "bash", 1)
	if e != nil || stored.Revision != 2 || !stored.Edited {
		t.Fatal(stored, e)
	}
	if _, e = s.Edit(ctx, stored.ID, "powershell", 1); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
	if _, e = s.GetEntry(ctx, stored.ID); e != nil {
		t.Fatal(e)
	}
	profile, e := s.Profile(ctx)
	if e != nil || strings.Contains(profile, "PowerShell") || !strings.Contains(profile, "Bash") {
		t.Fatal(profile, e)
	}
	if e = s.Delete(ctx, stored.ID, 1); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, stored.ID, stored.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e = s.GetEntry(ctx, stored.ID); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = s.Decide(ctx, p.ID, Decision{Decision: "accept"}); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal("deleted proposal was replayed", e)
	}
	if _, e = s.GetEntry(ctx, stored.ID); !errors.Is(e, ErrDeleted) {
		t.Fatal("deletion has no owner receipt", e)
	}
	if e = s.Delete(ctx, stored.ID, stored.Revision); e != nil {
		t.Fatal("deletion replay failed", e)
	}
	if e = s.Delete(ctx, stored.ID, stored.Revision-1); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
	profile, e = s.Profile(ctx)
	if e != nil || strings.Contains(profile, "Bash") || strings.Contains(profile, "Windows") {
		t.Fatal(profile, e)
	}
	if _, e = s.Edit(ctx, "missing", "bash", 1); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, "missing", 1); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = s.Edit(ctx, "missing", "bash", 0); !errors.Is(e, ports.ErrInvalid) {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, "missing", 0); !errors.Is(e, ports.ErrInvalid) {
		t.Fatal(e)
	}
	if _, e = s.Decide(ctx, p.ID, Decision{Decision: "other"}); !errors.Is(e, ports.ErrInvalid) {
		t.Fatal(e)
	}
	if _, e = s.Decide(ctx, "missing", Decision{Decision: "accept"}); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPostgresMemoryReplacementAndSourceIsolation(t *testing.T) {
	s, ctx := memoryFixture(t)
	first := mustSuggest(t, s, ctx, "memory-user", "shell")
	mustSQL(t, s, `INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,status,text,trace_id) VALUES('memory-user-2','memory-conversation',2,gen_random_uuid(),'user','completed','Я использую Bash.','memory')`)
	second := mustSuggest(t, s, ctx, "memory-user-2", "shell")
	old := accept(t, s, ctx, first)
	if _, e := s.Decide(ctx, second.ID, Decision{Decision: "accept"}); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
	state, e := s.Proposals(ctx, "memory-user-2")
	if e != nil || state.Items[0].Replaces == nil {
		t.Fatal(state, e)
	}
	current := accept(t, s, ctx, state.Items[0])
	if current.ID != old.ID || current.Revision != 2 {
		t.Fatal(current, old)
	}
	peer := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "intern_demo_2", TeamID: "team_demo_backend", Role: identity.Intern})
	foreign := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "intern_demo_other", TeamID: "team_demo_frontend", Role: identity.Intern})
	for _, actor := range []context.Context{peer, foreign} {
		if _, e := s.GetEntry(actor, current.ID); !errors.Is(e, ports.ErrNotFound) {
			t.Fatal(e)
		}
		if _, e := s.Edit(actor, current.ID, "powershell", 2); !errors.Is(e, ports.ErrNotFound) {
			t.Fatal(e)
		}
		if e := s.Delete(actor, current.ID, 2); !errors.Is(e, ports.ErrNotFound) {
			t.Fatal(e)
		}
		if _, e := s.Decide(actor, first.ID, Decision{Decision: "accept"}); !errors.Is(e, ports.ErrNotFound) {
			t.Fatal(e)
		}
		if _, e := s.Proposals(actor, "memory-user"); !errors.Is(e, ports.ErrNotFound) {
			t.Fatal(e)
		}
		if _, e := s.Suggest(actor, "memory-user"); !errors.Is(e, ports.ErrNotFound) {
			t.Fatal(e)
		}
		profile, e := s.Profile(actor)
		if e != nil || strings.Contains(profile, "Bash") {
			t.Fatal(profile, e)
		}
	}
	mustSQL(t, s, `UPDATE conversations SET user_id='intern_demo_2' WHERE id='memory-conversation'`)
	workspace, e := s.Workspace(ctx)
	if e != nil || len(workspace.Entries) != 0 || len(workspace.Pending) != 0 {
		t.Fatal(workspace, e)
	}
	if _, e = s.GetEntry(ctx, current.ID); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = s.Proposals(ctx, "memory-user-2"); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPostgresMemoryModelFailuresAndChangedSource(t *testing.T) {
	s, ctx := memoryFixture(t)
	for _, selector := range []Selector{nil, selectFunc(func(context.Context, string) ([]Candidate, error) { return nil, ports.ErrUnavailable }), selectFunc(func(context.Context, string) ([]Candidate, error) {
		return []Candidate{{Category: "hr", Key: "shell", Value: "powershell", Quote: "использую PowerShell"}}, nil
	}), selectFunc(func(context.Context, string) ([]Candidate, error) { return make([]Candidate, 5), nil })} {
		s.Selector = selector
		v, e := s.Suggest(ctx, "memory-user")
		if e != nil || v.State != "failed" || len(v.Items) != 0 {
			t.Fatal(v, e)
		}
	}
	s.Selector = Model{Demo: true}
	if v, e := s.Suggest(ctx, "memory-user"); e != nil || v.State != "ready" || len(v.Items) != 2 {
		t.Fatal(v, e)
	}
	mustSQL(t, s, `UPDATE chat_messages SET text='Мой пароль supersecret. Я использую PowerShell.' WHERE id='memory-user'; DELETE FROM memory_proposals; DELETE FROM memory_extractions`)
	s.Selector = selectFunc(func(context.Context, string) ([]Candidate, error) {
		t.Fatal("sensitive source reached the selector")
		return nil, nil
	})
	if v, e := s.Suggest(ctx, "memory-user"); e != nil || v.State != "skipped" {
		t.Fatal(v, e)
	}
	if v, e := s.Suggest(ctx, "memory-user"); e != nil || v.State != "skipped" {
		t.Fatal(v, e)
	}
	mustSQL(t, s, `UPDATE chat_messages SET text='Я использую PowerShell.' WHERE id='memory-user'; DELETE FROM memory_extractions`)
	s.Selector = selectFunc(func(context.Context, string) ([]Candidate, error) {
		mustSQL(t, s, `UPDATE chat_messages SET text='Я использую Bash.' WHERE id='memory-user'`)
		return []Candidate{{Category: "work_environment", Key: "shell", Value: "powershell", Quote: "использую PowerShell"}}, nil
	})
	if _, e := s.Suggest(ctx, "memory-user"); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
	s.Selector = selectFunc(func(context.Context, string) ([]Candidate, error) {
		mustSQL(t, s, `UPDATE conversations SET user_id='intern_demo_2' WHERE id='memory-conversation'`)
		return []Candidate{}, nil
	})
	if _, e := s.Suggest(ctx, "memory-user"); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPostgresMemoryFreshApplicationContext(t *testing.T) {
	s, ctx := memoryFixture(t)
	p := mustSuggest(t, s, ctx, "memory-user", "shell")
	accept(t, s, ctx, p)
	w, e := s.Workspace(ctx)
	if e != nil || w.Context.Mentor == nil || w.Context.Mentor.ID != "mentor_demo_1" || w.Context.Progress == nil {
		t.Fatal(w, e)
	}
	mustSQL(t, s, `INSERT INTO chat_messages(id,conversation_id,sequence,role,status,text,in_reply_to_message_id,trace_id) VALUES('memory-assistant','memory-conversation',2,'assistant','completed','Ответ','memory-user','memory');INSERT INTO access_requests(id,user_id,team_id,conversation_id,assistant_message_id,resource_id,resource_name,permission,reason,status,external_id) VALUES('memory-request','intern_demo_1','team_demo_backend','memory-conversation','memory-assistant','repo','Репозиторий','read','Не копировать личное описание','pending','external-private')`)
	w, e = s.Workspace(ctx)
	if e != nil || len(w.Context.Recent) != 1 || w.Context.Recent[0].Status != "pending" || !strings.Contains(w.Markdown, "memory-request") || strings.Contains(w.Markdown, "external-private") || strings.Contains(w.Markdown, "личное описание") {
		t.Fatal(w, e)
	}
	mustSQL(t, s, `UPDATE access_requests SET status='granted' WHERE id='memory-request'`)
	profile, e := s.Profile(ctx)
	if e != nil || !strings.Contains(profile, "granted") || strings.Contains(profile, "pending") {
		t.Fatal(profile, e)
	}
	mustSQL(t, s, `UPDATE mentor_assignments SET mentor_id='mentor_demo_2' WHERE intern_id='intern_demo_1';UPDATE onboarding_steps SET status='completed' WHERE plan_id=(SELECT id FROM onboarding_plans WHERE user_id='intern_demo_1')`)
	w, e = s.Workspace(ctx)
	if e != nil || w.Context.Mentor.ID != "mentor_demo_2" || *w.Context.Progress != 100 {
		t.Fatal(w, e)
	}
	profile, e = s.Profile(ctx)
	if e != nil || !strings.Contains(profile, w.Context.Mentor.Name) || !strings.Contains(profile, "100%") {
		t.Fatal(profile, e)
	}
	mustSQL(t, s, `DELETE FROM mentor_assignments WHERE intern_id='intern_demo_1'; DELETE FROM onboarding_plans WHERE user_id='intern_demo_1'`)
	w, e = s.Workspace(ctx)
	if e != nil || w.Context.Mentor != nil || w.Context.Progress != nil || !strings.Contains(w.Markdown, "не назначен") {
		t.Fatal(w, e)
	}
}

func TestPostgresMemoryDependencyFailuresNeverReturnOldProfile(t *testing.T) {
	s, ctx := memoryFixture(t)
	p := mustSuggest(t, s, ctx, "memory-user", "shell")
	stored := accept(t, s, ctx, p)
	for _, table := range []string{"memory_entries", "memory_proposals", "mentor_assignments", "access_requests"} {
		mustSQL(t, s, `ALTER TABLE `+table+` RENAME TO memory_fault_table`)
		if _, e := s.Workspace(ctx); !errors.Is(e, ports.ErrUnavailable) {
			t.Fatal(table, e)
		}
		if table != "memory_proposals" {
			if _, e := s.Profile(ctx); !errors.Is(e, ports.ErrUnavailable) {
				t.Fatal(table, e)
			}
		}
		mustSQL(t, s, `ALTER TABLE memory_fault_table RENAME TO `+table)
	}
	mustSQL(t, s, `UPDATE memory_entries SET value='invalid'`)
	if _, e := s.GetEntry(ctx, stored.ID); !errors.Is(e, ErrRejected) {
		t.Fatal(e)
	}
	mustSQL(t, s, `UPDATE memory_entries SET value='powershell';UPDATE memory_proposals SET category='learning'`)
	if _, e := s.Proposals(ctx, "memory-user"); !errors.Is(e, ErrRejected) {
		t.Fatal(e)
	}
	if _, e := s.Decide(ctx, p.ID, Decision{Decision: "accept"}); !errors.Is(e, ErrRejected) {
		t.Fatal(e)
	}
	if _, e := s.Profile(context.Background()); !errors.Is(e, identity.ErrUnauthenticated) {
		t.Fatal(e)
	}
	mentor := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "mentor_demo_1", TeamID: "team_demo_backend", Role: identity.Mentor})
	if _, e := s.Workspace(mentor); !errors.Is(e, ports.ErrForbidden) {
		t.Fatal(e)
	}
	wrongTeam := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "intern_demo_1", TeamID: "team_demo_frontend", Role: identity.Intern})
	if _, e := s.Workspace(wrongTeam); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
	if !errors.Is(mapped(sql.ErrNoRows), ports.ErrNotFound) || !errors.Is(mapped(ports.ErrInvalid), ports.ErrInvalid) || !errors.Is(mapped(errors.New("database")), ports.ErrUnavailable) {
		t.Fatal("error mapping")
	}
}

func TestWindowsStatementWithTrailingNewlines(t *testing.T) {
	for _, source := range []string{
		"у меня на компьютере система windows, это норм?\n\n",
		"Я работаю на виндоус.",
	} {
		if !eligibleSource(source) ||
			!validCandidate(Candidate{Category: "work_environment", Key: "os", Value: "windows", Quote: strings.TrimSpace(source)}, source) {
			t.Errorf("missed Windows statement: %q", source)
		}
	}
}

func TestMemoryOptOut(t *testing.T) {
	for _, text := range []string{"Я использую Bash. Не сохраняй это в память.", "Я использую Bash. Не надо запоминать.", "Я использую Bash. Don't store this.", "Я использую Bash. Сохрани это только до закрытия вкладки, без карточек.", "Я предпочитаю краткие инструкции. Я передумал: длинные инструкции тоже подходят, не делай вывод о моих предпочтениях."} {
		if safeSource(text) {
			t.Errorf("opt-out accepted: %q", text)
		}
	}
}
