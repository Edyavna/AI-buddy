package migrations_test

import (
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

func TestPostgresUpgradeFromCheckpointReleasePreservesQuestionsAndDocuments(t *testing.T) {
	db := testDB(t)
	if err := migrations.Apply(t.Context(), db, migrationsThrough(t, "000012_checkpoint_confirmations.sql")); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`
INSERT INTO teams VALUES ('team','Команда');
INSERT INTO users(id,team_id,name,role) VALUES ('intern','team','Стажёр','intern'),('mentor','team','Наставник','mentor');
INSERT INTO sessions VALUES (decode(repeat('1a',32),'hex'),'intern',now()+interval '1 day');
INSERT INTO mentor_assignments VALUES ('intern','mentor','team');
INSERT INTO conversations(id,user_id,team_id,client_conversation_id,title) VALUES ('chat','intern','team',gen_random_uuid(),'Чат');
INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,status,trace_id,in_reply_to_message_id)
VALUES ('user','chat',1,gen_random_uuid(),'user','completed','test',NULL),('assistant','chat',2,NULL,'assistant','completed','test','user');
INSERT INTO knowledge_gaps(id,message_id,student_id,team_id,mentor_id,question,context_excerpt,status,revision,resolution)
VALUES ('2d8ddc28-8cfa-4ef0-b195-c48f829a2234','assistant','intern','team','mentor','Вопрос','Контекст','resolved',2,'Ответ');
INSERT INTO knowledge_gap_history(question_id,revision,snapshot)
VALUES ('2d8ddc28-8cfa-4ef0-b195-c48f829a2234',1,'{"status":"new"}');
INSERT INTO knowledge_documents VALUES ('old','demo','old','Документ','Текст',now(),'team','["intern"]','hash');`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrations.Apply(t.Context(), db, migrations.Files); err != nil {
			t.Fatal(err)
		}
	}
	var status, direction, specialist, response, history, docDirection, acl string
	var revision, docVersion, sessions int
	err = db.QueryRow(`SELECT q.status,q.direction,q.mentor_id,q.resolution,h.snapshot->>'status',d.direction,d.users::text,q.revision,d.version,
(SELECT count(*) FROM sessions WHERE user_id='intern')
FROM knowledge_gaps q JOIN knowledge_gap_history h ON h.question_id=q.id
JOIN knowledge_documents d ON d.id='old' WHERE q.message_id='assistant'`).Scan(&status, &direction, &specialist, &response, &history, &docDirection, &acl, &revision, &docVersion, &sessions)
	if err != nil || status != "resolved" || direction != "mentor" || specialist != "mentor" || response != "Ответ" || history != "new" || docDirection != "mentor" || acl != `["intern"]` || revision != 2 || docVersion != 1 || sessions != 1 {
		t.Fatal(status, direction, specialist, response, history, docDirection, acl, revision, docVersion, sessions, err)
	}
}
