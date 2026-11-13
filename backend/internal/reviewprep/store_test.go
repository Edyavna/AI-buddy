package reviewprep_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/reviewprep"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

type fakeProvider struct {
	mu        sync.Mutex
	responses []reviewprep.PRInfo
	errs      []error
	calls     int
}

func (f *fakeProvider) Fetch(ctx context.Context, repo string, number int) (reviewprep.PRInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return reviewprep.PRInfo{}, err
	}
	if len(f.responses) == 0 {
		return reviewprep.PRInfo{}, ports.ErrUnavailable
	}
	info := f.responses[0]
	if len(f.responses) > 1 {
		f.responses = f.responses[1:]
	}
	return info, nil
}

func internCtx(id, team string) context.Context {
	return identity.WithPrincipal(context.Background(), identity.Principal{UserID: id, TeamID: team, Role: identity.Intern})
}

func mentorCtx(id, team string) context.Context {
	return identity.WithPrincipal(context.Background(), identity.Principal{UserID: id, TeamID: team, Role: identity.Mentor})
}

// testDB отдаёт один изолированный PostgreSQL на тест.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	return testkit.Postgres(t)
}

const goodURL = "https://github.com/Edyavna/AI-buddy/pull/59"

func TestTemplateDefaultAndSave(t *testing.T) {
	db := testDB(t)
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	store := reviewprep.Store{DB: db}
	ctx := testkit.Context(context.Background())

	template, err := store.Template(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if template.Custom || template.Revision != 0 || len(template.Items) != len(reviewprep.DefaultTemplate) {
		t.Fatalf("unexpected default template %+v", template)
	}

	if _, err := store.SaveTemplate(ctx, []reviewprep.TemplateItem{{Key: "k", Label: "Пункт"}}, 0); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("intern save must be forbidden, got %v", err)
	}

	mentor := mentorCtx("mentor_demo_1", testkit.TeamID)
	saved, err := store.SaveTemplate(mentor, []reviewprep.TemplateItem{{Key: "k1", Label: "Первый", Required: true}, {Key: "k2", Label: "Второй"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Custom || saved.Revision != 1 || saved.UpdatedBy != "mentor_demo_1" || len(saved.Items) != 2 {
		t.Fatalf("unexpected saved template %+v", saved)
	}
	template, err = store.Template(ctx)
	if err != nil || !template.Custom || template.Items[0].Key != "k1" {
		t.Fatalf("template not visible to intern: %+v %v", template, err)
	}
	if _, err := store.SaveTemplate(mentor, []reviewprep.TemplateItem{{Key: "k3", Label: "Третий"}}, 99); !errors.Is(err, reviewprep.ErrChanged) {
		t.Fatalf("stale template revision must fail, got %v", err)
	}
	if _, err := store.SaveTemplate(mentor, []reviewprep.TemplateItem{{Key: "k3", Label: "Третий"}}, 1); err != nil {
		t.Fatal(err)
	}
}

func TestCreateDraftWithIntegration(t *testing.T) {
	db := testDB(t)
	provider := &fakeProvider{responses: []reviewprep.PRInfo{{Title: "Фича", HeadSHA: "sha1", Checks: []reviewprep.CheckUpdate{{Name: "ci", Status: "passed"}}}}}
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	store := reviewprep.Store{DB: db, Provider: provider}
	ctx := testkit.Context(context.Background())
	step := "step_local_setup"

	prep, err := store.Create(ctx, goodURL, &step, false)
	if err != nil {
		t.Fatal(err)
	}
	if prep.Status != "draft" || prep.PR.Source != "github" || prep.PR.Title != "Фича" || prep.PR.HeadSHA != "sha1" {
		t.Fatalf("unexpected prep %+v", prep)
	}
	if len(prep.Checks) != 1 || prep.Checks[0].Source != "ci" || prep.Checks[0].Name != "ci" {
		t.Fatalf("ci checks not persisted: %+v", prep.Checks)
	}
	if prep.PR.FetchedAt == nil || prep.PR.FetchError != "" {
		t.Fatalf("unexpected fetch state %+v", prep.PR)
	}

	if _, err := store.Create(ctx, goodURL, nil, false); !errors.Is(err, reviewprep.ErrDraftExists) {
		t.Fatalf("second draft must be rejected, got %v", err)
	}
	if _, err := store.Create(ctx, "https://gitlab.com/o/r/pull/1", nil, false); !errors.Is(err, ports.ErrInvalid) {
		t.Fatalf("unsupported url must be invalid, got %v", err)
	}
	if _, err := store.Create(ctx, goodURL, strPtr("no_such_step"), false); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("foreign step must be not found, got %v", err)
	}
}

func TestCreateKeepsDraftOnIntegrationFailure(t *testing.T) {
	db := testDB(t)
	provider := &fakeProvider{errs: []error{reviewprep.ErrPRNotFound}}
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	store := reviewprep.Store{DB: db, Provider: provider}
	ctx := testkit.Context(context.Background())

	prep, err := store.Create(ctx, goodURL, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if prep.PR.Source != "github" || prep.PR.FetchError == "" || prep.PR.FetchedAt != nil {
		t.Fatalf("integration failure must keep manual draft, got %+v", prep.PR)
	}
	if len(prep.Checks) != 0 {
		t.Fatalf("no checks expected on failure, got %+v", prep.Checks)
	}
}

func TestCreateManualAndUnassigned(t *testing.T) {
	db := testDB(t)
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	store := reviewprep.Store{DB: db} // без провайдера — ручной режим
	ctx := testkit.Context(context.Background())

	prep, err := store.Create(ctx, goodURL, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if prep.PR.Source != "manual" {
		t.Fatalf("expected manual source, got %+v", prep.PR)
	}

	unassigned := internCtx("intern_demo_unassigned", testkit.TeamID)
	if _, err := store.Create(unassigned, goodURL, nil, false); !errors.Is(err, ports.ErrMentorUnassigned) {
		t.Fatalf("unassigned intern must fail, got %v", err)
	}
	other := internCtx("intern_demo_other", "team_demo_frontend")
	if _, err := store.Create(other, goodURL, nil, false); !errors.Is(err, ports.ErrMentorUnassigned) {
		t.Fatalf("intern without mentor in own team must fail, got %v", err)
	}
}

func TestUpdateSubmitHistoryAndRestart(t *testing.T) {
	db := testDB(t)
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	store := reviewprep.Store{DB: db}
	ctx := testkit.Context(context.Background())
	prep, err := store.Create(ctx, goodURL, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	update := reviewprep.Update{
		Revision:    prep.Revision,
		Description: "Описание",
		Answers: reviewprep.Answers{
			Summary: "Сделано", Verification: "Ручная проверка",
			Items: map[string]reviewprep.ItemAnswer{"task_done": {Done: true, Note: "ок"}},
		},
		Checks: []reviewprep.CheckUpdate{{Name: "go build", Status: "passed", Detail: "локально"}},
	}
	updated, err := store.Update(ctx, prep.ID, update)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != prep.Revision+1 || len(updated.Checks) != 1 || updated.Checks[0].Source != "intern" {
		t.Fatalf("unexpected update %+v", updated)
	}
	if _, err := store.Update(ctx, prep.ID, update); !errors.Is(err, reviewprep.ErrChanged) {
		t.Fatalf("stale revision must fail, got %v", err)
	}
	update.Revision = updated.Revision
	unchanged, err := store.Update(ctx, prep.ID, update)
	if err != nil || unchanged.Revision != updated.Revision {
		t.Fatalf("unchanged update must keep the revision: %d, %v", unchanged.Revision, err)
	}
	bad := update
	bad.Revision = updated.Revision
	bad.Checks = []reviewprep.CheckUpdate{{Name: "x", Status: "unknown"}}
	if _, err := store.Update(ctx, prep.ID, bad); !errors.Is(err, ports.ErrInvalid) {
		t.Fatalf("invalid check status must fail, got %v", err)
	}

	submitted, err := store.Submit(ctx, prep.ID, reviewprep.Submit{Revision: updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Status != "submitted" || submitted.Revision != updated.Revision+1 || submitted.SubmittedAt == nil {
		t.Fatalf("unexpected submit %+v", submitted)
	}
	if len(submitted.History) != 1 || submitted.History[0].Revision != updated.Revision {
		t.Fatalf("history must contain the previous revision: %+v", submitted.History)
	}
	if _, err := store.Submit(ctx, prep.ID, reviewprep.Submit{Revision: updated.Revision}); !errors.Is(err, reviewprep.ErrChanged) {
		t.Fatalf("repeat submit with stale revision must fail, got %v", err)
	}

	// Повторная отправка отправленной подготовки создаёт новую ревизию с историей.
	update.Revision = submitted.Revision
	update.Description = "Описание v2"
	resubmitted, err := store.Update(ctx, prep.ID, update)
	if err != nil {
		t.Fatal(err)
	}
	resubmitted, err = store.Submit(ctx, prep.ID, reviewprep.Submit{Revision: resubmitted.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if len(resubmitted.History) != 2 {
		t.Fatalf("expected two history entries, got %+v", resubmitted.History)
	}

	// Новый экземпляр store на той же БД имитирует перезапуск процесса.
	restarted := reviewprep.Store{DB: db}
	latest, err := restarted.Latest(ctx)
	if err != nil || latest == nil {
		t.Fatalf("prep must survive restart: %v %+v", err, latest)
	}
	if latest.Description != "Описание v2" || latest.Revision != resubmitted.Revision || len(latest.History) != 2 {
		t.Fatalf("unexpected restarted state %+v", latest)
	}
}

func TestMentorVisibility(t *testing.T) {
	db := testDB(t)
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	store := reviewprep.Store{DB: db}
	ctx := testkit.Context(context.Background())
	prep, err := store.Create(ctx, goodURL, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Submit(ctx, prep.ID, reviewprep.Submit{Revision: prep.Revision}); err != nil {
		t.Fatal(err)
	}

	mentor := mentorCtx("mentor_demo_1", testkit.TeamID)
	items, err := store.MentorList(mentor, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Student.ID != testkit.UserID || items[0].Student.Name == "" {
		t.Fatalf("unexpected mentor list %+v", items)
	}
	detail, err := store.MentorGet(mentor, prep.ID)
	if err != nil || detail.ID != prep.ID {
		t.Fatalf("mentor get failed: %v %+v", err, detail)
	}

	otherMentor := mentorCtx("mentor_demo_2", testkit.TeamID)
	if items, err := store.MentorList(otherMentor, ""); err != nil || len(items) != 0 {
		t.Fatalf("unassigned mentor must see nothing: %v %+v", err, items)
	}
	if _, err := store.MentorGet(otherMentor, prep.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("unassigned mentor get must be not found, got %v", err)
	}
	other := internCtx("intern_demo_2", testkit.TeamID)
	if _, err := store.Get(other, prep.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("other intern get must be not found, got %v", err)
	}
	otherTeam := internCtx("intern_demo_other", "team_demo_frontend")
	if _, err := store.Get(otherTeam, prep.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("other team get must be not found, got %v", err)
	}
}

func TestRefreshBindsChecksToHeadCommit(t *testing.T) {
	db := testDB(t)
	provider := &fakeProvider{responses: []reviewprep.PRInfo{
		{Title: "Фича", HeadSHA: "sha1"}, // Create already fetches PR metadata.
		{Title: "Фича", HeadSHA: "sha1", Checks: []reviewprep.CheckUpdate{{Name: "ci", Status: "passed"}}},
		{Title: "Фича v2", HeadSHA: "sha2", Checks: []reviewprep.CheckUpdate{{Name: "ci", Status: "failed"}}},
	}}
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	store := reviewprep.Store{DB: db, Provider: provider}
	ctx := testkit.Context(context.Background())
	prep, err := store.Create(ctx, goodURL, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, changed, err := store.Refresh(ctx, prep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("first refresh must not report changes")
	}
	if refreshed.PR.HeadSHA != "sha1" || len(refreshed.Checks) != 1 || refreshed.Checks[0].Status != "passed" {
		t.Fatalf("unexpected refresh %+v", refreshed)
	}

	refreshed, changed, err = store.Refresh(ctx, prep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || refreshed.PR.HeadSHA != "sha2" || refreshed.PR.Title != "Фича v2" {
		t.Fatalf("commit change must be reported: %+v %v", refreshed.PR, changed)
	}
	if len(refreshed.Checks) != 1 || refreshed.Checks[0].Status != "failed" || refreshed.Checks[0].Source != "ci" {
		t.Fatalf("ci checks must follow the new commit: %+v", refreshed.Checks)
	}
}

func TestRefreshUnavailableModes(t *testing.T) {
	db := testDB(t)
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	store := reviewprep.Store{DB: db}
	ctx := testkit.Context(context.Background())
	prep, err := store.Create(ctx, goodURL, nil, true) // manual mode
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Refresh(ctx, prep.ID); !errors.Is(err, reviewprep.ErrTransition) {
		t.Fatalf("manual mode refresh must fail, got %v", err)
	}
	if _, _, err := (reviewprep.Store{DB: db, Provider: &fakeProvider{}}).Refresh(ctx, prep.ID); !errors.Is(err, reviewprep.ErrTransition) {
		t.Fatalf("manual source with provider must fail too, got %v", err)
	}
}

func TestConcurrentSubmitLeavesOneHistory(t *testing.T) {
	db := testDB(t)
	if err := demo.Seed(testkit.Context(context.Background()), db, true); err != nil {
		t.Fatal(err)
	}
	store := reviewprep.Store{DB: db}
	ctx := testkit.Context(context.Background())
	prep, err := store.Create(ctx, goodURL, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = store.Submit(ctx, prep.ID, reviewprep.Submit{Revision: prep.Revision})
		}(i)
	}
	wg.Wait()
	success := 0
	for _, err := range errs {
		if err == nil {
			success++
		} else if !errors.Is(err, reviewprep.ErrChanged) {
			t.Fatalf("unexpected error %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("exactly one concurrent submit must succeed, got %d (%v)", success, errs)
	}
	final, err := store.Get(ctx, prep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Revision != prep.Revision+1 || len(final.History) != 1 {
		t.Fatalf("concurrent submit corrupted state: %+v", final)
	}
}

func strPtr(s string) *string { return &s }
