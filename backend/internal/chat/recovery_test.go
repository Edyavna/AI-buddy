package chat

import (
	"errors"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"testing"
	"time"
)

func TestPostgresFailedFinishRecoversWithoutRestart(t *testing.T) {
	f := newFixture(t, fastAgent(), nil, StreamOptions{})
	c := f.create(t, 1)
	ctx := testContext()
	p, err := f.service.Prepare(ctx, c.ID, uid(2), "Первый вопрос")
	if err != nil {
		t.Fatal(err)
	}
	const partial = "Сохранённая часть 🚀"
	if err := f.store.SavePartial(ctx, testOwner(), c.ID, p.Assistant.ID, partial); err != nil {
		t.Fatal(err)
	}
	// A real SQL write failure rolls back Finish, while checkpoints survive.
	if _, err := f.db.Exec(`CREATE FUNCTION fail_finish() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN IF OLD.status='generating' AND NEW.status<>'generating' THEN
	RAISE EXCEPTION 'temporary final write failure'; END IF; RETURN NEW; END $$;
	CREATE TRIGGER fail_finish BEFORE UPDATE ON chat_messages FOR EACH ROW EXECUTE FUNCTION fail_finish()`); err != nil {
		t.Fatal(err)
	}
	sink := newSink()
	if err := f.service.Stream(ctx, p, sink, StreamOptions{}); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatalf("expected failed Finish: %v", err)
	}
	if sink.contains("message.completed") || sink.contains("message.failed") {
		t.Fatal("unsaved terminal event")
	}
	if _, err := f.db.Exec(`DROP TRIGGER fail_finish ON chat_messages`); err != nil {
		t.Fatal(err)
	}
	// DB has recovered. Finishing operations still have their reserved grace time.
	if _, err := f.db.Exec(`UPDATE chat_messages SET created_at=clock_timestamp()-interval '125 seconds',
	trace_id='recovery-trace',sources='[{"id":"s","document_id":"doc","title":"Источник","excerpt":"Часть"}]' WHERE id=$1`, p.Assistant.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Prepare(ctx, c.ID, uid(3), "Следующий"); !errors.Is(err, ErrBusy) {
		t.Fatalf("live generation was replaced: %v", err)
	}
	// Move persisted start time instead of waiting for the generation budget.
	if _, err := f.db.Exec(`UPDATE chat_messages SET created_at=clock_timestamp()-interval '131 seconds' WHERE id=$1`, p.Assistant.ID); err != nil {
		t.Fatal(err)
	}
	// No Recover call, handler recreation or process restart.
	next, err := f.service.Prepare(ctx, c.ID, uid(3), "Следующий")
	if err != nil {
		t.Fatalf("DB recovered but conversation remains stuck: %v", err)
	}
	all, err := f.store.Messages(ctx, testOwner(), c.ID)
	if err != nil || len(all) != 4 {
		t.Fatal(all, err)
	}
	old := all[1]
	if old.Status != "interrupted" || old.Text != partial || old.Error == nil || old.Error.Error.Code != "GENERATION_INTERRUPTED" {
		t.Fatalf("lost interrupted checkpoint: %+v", old)
	}
	if len(old.Sources) != 1 || old.Sources[0].ID != "s" || old.Error.Error.TraceID != "recovery-trace" {
		t.Fatalf("lost recovery metadata: %+v", old)
	}
	late := p.Assistant
	late.Status, late.Text = "completed", "Поздний финал"
	kept, err := f.store.Finish(ctx, testOwner(), c.ID, late)
	if err != nil || kept.Status != "interrupted" || kept.Text != partial {
		t.Fatalf("late Finish overwrote recovery: %+v %v", kept, err)
	}
	if err := f.store.SavePartial(ctx, testOwner(), c.ID, late.ID, "Поздний checkpoint"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Stream(ctx, next, newSink(), StreamOptions{}); err != nil {
		t.Fatal(err)
	}
	all, err = f.store.Messages(ctx, testOwner(), c.ID)
	if err != nil || all[1].Text != partial || all[1].Status != "interrupted" || all[3].Status != "completed" {
		t.Fatalf("next question did not complete: %+v %v", all, err)
	}
}

func TestStreamBudgetsFitGenerationLease(t *testing.T) {
	for _, options := range []StreamOptions{{}, {
		GenerationTimeout: time.Hour, CleanupTimeout: time.Hour, WriteTimeout: time.Hour,
	}, {
		GenerationTimeout: time.Second, CleanupTimeout: time.Second, WriteTimeout: time.Second,
	}} {
		o := options.defaults()
		if total := o.GenerationTimeout + o.CleanupTimeout + o.WriteTimeout; total > generationLease {
			t.Fatalf("stream can outlive its reservation: %s > %s", total, generationLease)
		}
	}
}

func TestPostgresExpiredGenerationConcurrentReservation(t *testing.T) {
	f := newFixture(t, fastAgent(), nil, StreamOptions{})
	c := f.create(t, 1)
	ctx := testContext()
	p, err := f.service.Prepare(ctx, c.ID, uid(2), "Вопрос")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE chat_messages SET created_at=clock_timestamp()-interval '131 seconds' WHERE id=$1`, p.Assistant.ID); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, n := range []int{3, 4} {
		go func() { <-start; _, err := f.service.Prepare(ctx, c.ID, uid(n), "Следующий"); results <- err }()
	}
	close(start)
	accepted, busy := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrBusy) {
			busy++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || busy != 1 {
		t.Fatalf("expected one new generation: accepted=%d busy=%d", accepted, busy)
	}
}
