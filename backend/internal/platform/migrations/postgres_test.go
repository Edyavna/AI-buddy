package migrations_test

import (
	"context"

	"database/sql"
	"errors"
	"io/fs"
	"strings"

	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func testDB(t *testing.T) *sql.DB { return testkit.UnmigratedPostgres(t) }

func TestPostgresUpgradeFromDev(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	// Reconstruct the released dev migration set, before access requests were added.
	dev := migrationsThrough(t, "000005_chat.sql")
	if err := migrations.Apply(ctx, db, dev); err != nil {
		t.Fatal(err)
	}
	var appliedAt string
	if err := db.QueryRow(`SELECT applied_at::text FROM buddy_schema_migrations WHERE name='000005_chat.sql'`).Scan(&appliedAt); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, db, migrations.Files); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Verify(ctx, db, migrations.Files); err != nil {
		t.Fatal(err)
	}
	var kept string
	if err := db.QueryRow(`SELECT applied_at::text FROM buddy_schema_migrations WHERE name='000005_chat.sql'`).Scan(&kept); err != nil || kept != appliedAt {
		t.Fatalf("dev migration reapplied: %q != %q: %v", kept, appliedAt, err)
	}
	assertDemoAndChat(t, db)
}

func migrationsThrough(t *testing.T, last string) fstest.MapFS {
	t.Helper()
	source := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.Files, "sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() > last {
			continue
		}
		data, err := fs.ReadFile(migrations.Files, "sql/"+entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		source["sql/"+entry.Name()] = &fstest.MapFile{Data: data}
	}
	return source
}

func TestPostgresUpgradePreservesCheckpointProgress(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	if err := migrations.Apply(ctx, db, migrationsThrough(t, "000011_mentor_documents.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO teams(id,name) VALUES ('team_demo_backend','Backend');
INSERT INTO users(id,team_id,name,role,demo_account) VALUES ('intern_demo_1','team_demo_backend','Стажёр','intern',true);
INSERT INTO onboarding_plans(id,user_id,team_id,title) VALUES ('plan_demo_1','intern_demo_1','team_demo_backend','План');
INSERT INTO onboarding_steps(id,plan_id,position,title,description,status,resource_id,permission)
VALUES ('step_local_setup','plan_demo_1',0,'Запустить проект','','completed','repo_demo_backend','read');
INSERT INTO onboarding_steps(id,plan_id,position,title,description,status,blocked_reason)
VALUES ('blocked','plan_demo_1',1,'Запрос доступа','','blocked','Ожидается доступ');
INSERT INTO conversations(id,user_id,team_id,client_conversation_id,step_id,title)
VALUES ('old_chat','intern_demo_1','team_demo_backend','d413d58a-bee0-4f56-baae-7c82f1f06466','blocked','Чат');
INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,status,trace_id,in_reply_to_message_id)
VALUES ('old_user','old_chat',1,'d4b5c050-d632-4a6a-aef1-42811cfe955d','user','completed','trace',NULL),
       ('old_assistant','old_chat',2,NULL,'assistant','completed','trace','old_user');
INSERT INTO access_requests(id,user_id,team_id,conversation_id,assistant_message_id,step_id,resource_id,
                            resource_name,permission,reason,status,external_id)
VALUES ('old_request','intern_demo_1','team_demo_backend','old_chat','old_assistant','blocked','repo_demo_backend',
        'Репозиторий','read','Для задания','pending','ACCESS-1');
INSERT INTO onboarding_step_requests(step_id,request_id,resource_id,permission,status)
VALUES ('blocked','old_request','repo_demo_backend','read','pending');
INSERT INTO chat_message_requests(assistant_message_id,request_id) VALUES ('old_assistant','old_request');`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrations.Apply(ctx, db, migrations.Files); err != nil {
			t.Fatal(err)
		}
		if err := demo.Seed(ctx, db, true); err != nil {
			t.Fatal(err)
		}
	}
	var policy, status, role, author string
	var confirmedAt time.Time
	err := db.QueryRow(`SELECT s.completion_policy,s.status,c.role,c.user_id,c.confirmed_at
FROM onboarding_steps s JOIN onboarding_step_confirmations c ON c.step_id=s.id WHERE s.id='step_local_setup'`).
		Scan(&policy, &status, &role, &author, &confirmedAt)
	if err != nil || policy != "intern" || status != "completed" || role != "intern" || author != "intern_demo_1" || confirmedAt.IsZero() {
		t.Fatal(policy, status, role, author, confirmedAt, err)
	}
	var preserved bool
	err = db.QueryRow(`SELECT EXISTS (
  SELECT 1 FROM onboarding_steps s JOIN onboarding_step_requests sr ON sr.step_id=s.id
  JOIN access_requests r ON r.step_id=s.id AND r.id=sr.request_id
  JOIN conversations ch ON ch.id=r.conversation_id AND ch.step_id=s.id
  JOIN chat_message_requests mr ON mr.request_id=r.id AND mr.assistant_message_id=r.assistant_message_id
  WHERE s.id='blocked' AND s.status='blocked' AND s.blocked_reason='Ожидается доступ'
    AND r.external_id='ACCESS-1' AND sr.status='pending'
    AND NOT EXISTS (SELECT 1 FROM onboarding_step_confirmations c WHERE c.step_id=s.id)
)`).Scan(&preserved)
	if err != nil || !preserved {
		t.Fatal("blocked step, access request or chat history was lost", err)
	}
}

func assertDemoAndChat(t *testing.T, db *sql.DB) {
	t.Helper()
	var names string
	if err := db.QueryRow(`SELECT string_agg(name, ',' ORDER BY name) FROM buddy_schema_migrations`).Scan(&names); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(names, "000004_demo_external.sql,000005_chat.sql,000006_access_requests.sql") {
		t.Fatalf("incorrect migration order: %s", names)
	}
	var present bool
	if err := db.QueryRow(`SELECT to_regclass('demo_external.resources') IS NOT NULL AND to_regclass('chat_messages') IS NOT NULL AND to_regclass('access_request_jobs') IS NOT NULL`).Scan(&present); err != nil || !present {
		t.Fatalf("demo or chat tables missing: %v", err)
	}
}

func TestPostgresMigrationLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := migrations.Verify(ctx, db, migrations.Files); err == nil {
		t.Fatal("empty DB accepted")
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := migrations.Apply(ctx, db, migrations.Files); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := migrations.Verify(ctx, db, migrations.Files); err != nil {
		t.Fatal(err)
	}
	assertDemoAndChat(t, db)
	var count int
	entries, err := fs.ReadDir(migrations.Files, "sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM buddy_schema_migrations").Scan(&count); err != nil || count != len(entries) {
		t.Fatalf("journal=%d: %v", count, err)
	}
	source := fstest.MapFS{}
	for _, entry := range entries {
		data, err := fs.ReadFile(migrations.Files, "sql/"+entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		source["sql/"+entry.Name()] = &fstest.MapFile{Data: data}
	}
	source["sql/999999_broken.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE must_rollback(id integer); SELECT missing_column;")}
	if err := migrations.Apply(ctx, db, source); err == nil {
		t.Fatal("invalid SQL accepted")
	}
	var tableExists bool
	if err := db.QueryRowContext(ctx, "SELECT to_regclass('must_rollback') IS NOT NULL").Scan(&tableExists); err != nil || tableExists {
		t.Fatalf("DDL escaped rollback: %v", err)
	}
	delete(source, "sql/999999_broken.sql")
	source["sql/000001_platform.sql"] = &fstest.MapFile{Data: []byte("SELECT 2;")}
	if err := migrations.Apply(ctx, db, source); err == nil {
		t.Fatal("changed applied migration accepted")
	}
}

func TestPostgresUnitOfWork(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE TABLE atomic_pair(id integer PRIMARY KEY, kind text)"); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("second write failed")
	err := database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO atomic_pair VALUES(1,'draft')"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic swallowed")
			}
		}()
		_ = database.WithinTx(ctx, db, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "INSERT INTO atomic_pair VALUES(2,'draft')"); err != nil {
				t.Fatal(err)
			}
			panic("rollback")
		})
	}()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM atomic_pair").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial commit: %d %v", count, err)
	}
	if err := database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO atomic_pair VALUES(3,'draft'),(4,'link')")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM atomic_pair").Scan(&count); err != nil || count != 2 {
		t.Fatalf("commit missing: %d %v", count, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := database.WithinTx(canceled, db, func(*sql.Tx) error { t.Fatal("called with canceled context"); return nil }); err == nil {
		t.Fatal("canceled transaction accepted")
	}
}
