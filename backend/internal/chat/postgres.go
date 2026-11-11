package chat

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/jackc/pgx/v5/pgtype"
)

type SQLStore struct{ DB *sql.DB }
type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const conversationColumns = `id,title,step_id,created_at,updated_at,client_conversation_id::text`

type scanner interface{ Scan(...any) error }

func scanConversation(row scanner) (Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.Title, &c.StepID, &c.CreatedAt, &c.UpdatedAt, &c.ClientID)
	c.CreatedAt = c.CreatedAt.UTC()
	c.UpdatedAt = c.UpdatedAt.UTC()
	return c, err
}
func sqlError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrNotFound
	}
	for _, known := range []error{ports.ErrNotFound, ports.ErrInvalid, ports.ErrConflict, ErrDuplicate, ErrBusy, ports.ErrForbidden, identity.ErrUnauthenticated} {
		if errors.Is(err, known) {
			return err
		}
	}
	return fmt.Errorf("%w: %w", ports.ErrUnavailable, err)
}
func owned(ctx context.Context, q queryer, p identity.Principal, id string, lock bool) (Conversation, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	return scanConversation(q.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE id=$1 AND user_id=$2 AND team_id=$3`+suffix, id, p.UserID, p.TeamID))
}
func (s SQLStore) Get(ctx context.Context, p identity.Principal, id string) (Conversation, error) {
	c, err := owned(ctx, s.DB, p, id, false)
	return c, sqlError(err)
}
func (s SQLStore) List(ctx context.Context, p identity.Principal) ([]Conversation, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE user_id=$1 AND team_id=$2 ORDER BY updated_at DESC,id`, p.UserID, p.TeamID)
	if err != nil {
		return nil, sqlError(err)
	}
	defer rows.Close()
	result := make([]Conversation, 0)
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, sqlError(err)
		}
		result = append(result, c)
	}
	return result, sqlError(rows.Err())
}
func sameStep(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
func (s SQLStore) Create(ctx context.Context, p identity.Principal, clientID string, stepID *string) (Conversation, bool, error) {
	var result Conversation
	created := false
	err := database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		title := "Новый диалог"
		if stepID != nil {
			if err := tx.QueryRowContext(ctx, `SELECT s.title FROM onboarding_steps s JOIN onboarding_plans p ON p.id=s.plan_id WHERE s.id=$1 AND p.user_id=$2 AND p.team_id=$3`, *stepID, p.UserID, p.TeamID).Scan(&title); err != nil {
				return err
			}
		}
		var err error
		result, err = scanConversation(tx.QueryRowContext(ctx, `INSERT INTO conversations(id,user_id,team_id,client_conversation_id,step_id,title) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(user_id,client_conversation_id) DO NOTHING RETURNING `+conversationColumns, rand.Text(), p.UserID, p.TeamID, clientID, stepID, title))
		if err == nil {
			created = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		result, err = scanConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE user_id=$1 AND team_id=$2 AND client_conversation_id=$3`, p.UserID, p.TeamID, clientID))
		if err != nil {
			return err
		}
		if !sameStep(result.StepID, stepID) {
			return ports.ErrConflict
		}
		return nil
	})
	return result, created, sqlError(err)
}
func messages(ctx context.Context, q queryer, conversationID string) ([]Message, error) {
	rows, err := q.QueryContext(ctx, `SELECT m.id,m.client_message_id::text,m.role,m.text,m.status,m.sources,m.error,m.created_at,m.in_reply_to_message_id,m.outcome,
 ARRAY(SELECT l.request_id FROM chat_message_requests l WHERE l.assistant_message_id=m.id ORDER BY l.request_id)
 FROM chat_messages m WHERE m.conversation_id=$1 ORDER BY m.sequence`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Message, 0)
	codec := pgtype.NewMap()
	for rows.Next() {
		var m Message
		var sources, apiError []byte
		var links string
		var outcome sql.NullString
		if err := rows.Scan(&m.ID, &m.ClientID, &m.Role, &m.Text, &m.Status, &sources, &apiError, &m.CreatedAt, &m.InReplyTo, &outcome, &links); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(sources, &m.Sources); err != nil {
			return nil, err
		}
		if len(apiError) > 0 {
			if err := json.Unmarshal(apiError, &m.Error); err != nil {
				return nil, err
			}
		}
		if err := codec.Scan(pgtype.TextArrayOID, pgtype.TextFormatCode, []byte(links), &m.RequestIDs); err != nil {
			return nil, err
		}
		if m.Role == "assistant" {
			m.Outcome = ports.OutcomeUnknown
			if outcome.Valid {
				m.Outcome = ports.Outcome(outcome.String)
			}
		}
		m.CreatedAt = m.CreatedAt.UTC()
		result = append(result, m)
	}
	return result, rows.Err()
}
func (s SQLStore) Messages(ctx context.Context, p identity.Principal, id string) ([]Message, error) {
	if _, err := s.Get(ctx, p, id); err != nil {
		return nil, err
	}
	result, err := messages(ctx, s.DB, id)
	return result, sqlError(err)
}
func (s SQLStore) Begin(ctx context.Context, p identity.Principal, id, clientID, text, trace string) (Reservation, error) {
	var r Reservation
	err := database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var err error
		r.Conversation, err = owned(ctx, tx, p, id, true)
		if err != nil {
			return err
		}
		var oldText string
		err = tx.QueryRowContext(ctx, `SELECT text FROM chat_messages WHERE conversation_id=$1 AND client_message_id=$2`, id, clientID).Scan(&oldText)
		if err == nil {
			if oldText == text {
				return ErrDuplicate
			}
			return ports.ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// The conversation lock serializes recovery with Begin, Finish and draft links.
		// Use the persisted start and DB clock, never the most recent checkpoint:
		// even a failed final write must eventually release the conversation.
		if _, err := tx.ExecContext(ctx, `UPDATE chat_messages SET status='interrupted',
 error=jsonb_build_object('error',jsonb_build_object('code','GENERATION_INTERRUPTED',
 'message','Генерация прервана; результат доступен в истории','trace_id',trace_id))
 WHERE conversation_id=$1 AND status='generating'
 AND created_at <= clock_timestamp() - make_interval(secs => $2)`, id, generationLease.Seconds()); err != nil {
			return err
		}
		var busy bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM chat_messages WHERE conversation_id=$1 AND status='generating')`, id).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrBusy
		}
		r.Previous, err = messages(ctx, tx, id)
		if err != nil {
			return err
		}
		userID, assistantID := rand.Text(), rand.Text()
		var sequence int64
		if err := tx.QueryRowContext(ctx, `UPDATE conversations SET next_sequence=next_sequence+2,updated_at=clock_timestamp() WHERE id=$1 RETURNING next_sequence-2,updated_at`, id).Scan(&sequence, &r.Conversation.UpdatedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO chat_messages(id,conversation_id,sequence,client_message_id,role,text,status,trace_id)
 VALUES($1,$2,$3,$4,'user',$5,'completed',$6)`, userID, id, sequence, clientID, text, trace); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO chat_messages(id,conversation_id,sequence,role,status,in_reply_to_message_id,trace_id)
 VALUES($1,$2,$3,'assistant','generating',$4,$5)`, assistantID, id, sequence+1, userID, trace); err != nil {
			return err
		}
		pair, err := messages(ctx, tx, id)
		if err != nil {
			return err
		}
		r.User, r.Assistant = pair[len(pair)-2], pair[len(pair)-1]
		return nil
	})
	return r, sqlError(err)
}
func (s SQLStore) SavePartial(ctx context.Context, p identity.Principal, conversationID, id, text string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_messages m SET text=$1 FROM conversations c WHERE m.id=$2 AND m.conversation_id=c.id AND c.id=$3 AND c.user_id=$4 AND c.team_id=$5 AND m.status='generating'`, text, id, conversationID, p.UserID, p.TeamID)
	return sqlError(err)
}
func (s SQLStore) Finish(ctx context.Context, p identity.Principal, conversationID string, m Message) (Message, error) {
	var result Message
	err := database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if _, err := owned(ctx, tx, p, conversationID, true); err != nil {
			return err
		}
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM chat_messages WHERE id=$1 AND conversation_id=$2 AND role='assistant' FOR UPDATE`, m.ID, conversationID).Scan(&status); err != nil {
			return err
		}
		if status == "generating" {
			for _, id := range m.RequestIDs {
				if err := LinkRequestTx(ctx, tx, p, m.ID, id); err != nil {
					return err
				}
			}
			if m.Sources == nil {
				m.Sources = []Source{}
			}
			sources, err := json.Marshal(m.Sources)
			if err != nil {
				return err
			}
			var failure any
			if m.Error != nil {
				data, err := json.Marshal(m.Error)
				if err != nil {
					return err
				}
				failure = string(data)
			}
			var outcome any
			if m.Status == "completed" && m.Outcome != "" && m.Outcome != ports.OutcomeUnknown {
				outcome = string(m.Outcome)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE chat_messages SET text=$1,status=$2,sources=$3,error=$4,outcome=$5 WHERE id=$6`, m.Text, m.Status, string(sources), failure, outcome, m.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE conversations SET updated_at=clock_timestamp() WHERE id=$1`, conversationID); err != nil {
				return err
			}
		}
		all, err := messages(ctx, tx, conversationID)
		if err != nil {
			return err
		}
		for _, saved := range all {
			if saved.ID == m.ID {
				result = saved
				return nil
			}
		}
		return ports.ErrNotFound
	})
	return result, sqlError(err)
}

// LinkRequestTx is called by #14 only AFTER verifying/saving the request in tx.
// Use the same tx as the draft write; it neither commits nor accepts HTTP IDs.
// Lock order: onboarding plan, request, conversation, assistant.
func LinkRequestTx(ctx context.Context, tx *sql.Tx, p identity.Principal, assistantID, requestID string) error {
	if p.Role != identity.Intern {
		return ports.ErrForbidden
	}
	if requestID == "" {
		return ports.ErrInvalid
	}
	var conversationID string
	if err := tx.QueryRowContext(ctx, `SELECT m.conversation_id FROM chat_messages m JOIN conversations c ON c.id=m.conversation_id WHERE m.id=$1 AND c.user_id=$2 AND c.team_id=$3`, assistantID, p.UserID, p.TeamID).Scan(&conversationID); err != nil {
		return sqlError(err)
	}
	if _, err := owned(ctx, tx, p, conversationID, true); err != nil {
		return sqlError(err)
	}
	var status, role string
	if err := tx.QueryRowContext(ctx, `SELECT status,role FROM chat_messages WHERE id=$1 FOR UPDATE`, assistantID).Scan(&status, &role); err != nil {
		return sqlError(err)
	}
	if role != "assistant" || status != "generating" {
		return ports.ErrConflict
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO chat_message_requests(assistant_message_id,request_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, assistantID, requestID)
	return sqlError(err)
}

// Recover runs only during application startup, before admitting requests.
// This MVP supports one active API process per database, not rolling replicas.
func (s SQLStore) Recover(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET status='interrupted',error=jsonb_build_object('error',jsonb_build_object('code','GENERATION_INTERRUPTED','message','Генерация прервана перезапуском сервера','trace_id',trace_id)) WHERE status='generating'`)
	return sqlError(err)
}
