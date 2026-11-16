package personalmemory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

var ErrChanged = errors.New("personal memory changed")
var ErrRejected = errors.New("memory fact is not permitted")
var ErrDeleted = fmt.Errorf("%w: personal memory entry deleted", ports.ErrNotFound)

type Store struct {
	DB       *sql.DB
	Selector Selector
}
type Entry struct {
	ID string `json:"id"`
	Option
	SourceMessageID string    `json:"source_message_id"`
	ConversationID  string    `json:"conversation_id"`
	Revision        int64     `json:"revision"`
	Edited          bool      `json:"edited"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type Proposal struct {
	ID string `json:"id"`
	Option
	SourceMessageID string    `json:"source_message_id"`
	ConversationID  string    `json:"conversation_id"`
	State           string    `json:"state"`
	CreatedAt       time.Time `json:"created_at"`
	EntryID         *string   `json:"entry_id"`
	Replaces        *Entry    `json:"replaces"`
	Uncertain       bool      `json:"uncertain,omitempty"`
}
type Proposals struct {
	State string     `json:"state"`
	Items []Proposal `json:"items"`
}
type Decision struct {
	Decision         string  `json:"decision"`
	ExpectedEntryID  *string `json:"expected_entry_id"`
	ExpectedRevision int64   `json:"expected_revision"`
}
type DecisionResult struct {
	Proposal Proposal `json:"proposal"`
	Entry    *Entry   `json:"entry"`
}

func principal(ctx context.Context) (identity.Principal, error) {
	p, e := identity.Require(ctx)
	if e == nil && p.Role != identity.Intern {
		e = ports.ErrForbidden
	}
	return p, e
}
func mapped(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrNotFound
	}
	for _, known := range []error{ports.ErrInvalid, ports.ErrForbidden, ports.ErrNotFound, ports.ErrUnavailable, identity.ErrUnauthenticated, ErrChanged, ErrRejected} {
		if errors.Is(err, known) {
			return err
		}
	}
	return fmt.Errorf("%w: %w", ports.ErrUnavailable, err)
}
func owner(ctx context.Context, tx *sql.Tx, p identity.Principal) error {
	if err := hrscope.LockTeam(ctx, tx, p.TeamID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(628179,hashtext($1))`, p.UserID); err != nil {
		return err
	}
	var id string
	return tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND team_id=$2 AND role='intern' FOR SHARE`, p.UserID, p.TeamID).Scan(&id)
}
func (s Store) transaction(ctx context.Context, work func(*sql.Tx, identity.Principal) error) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return mapped(database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if e := owner(ctx, tx, p); e != nil {
			return e
		}
		return work(tx, p)
	}))
}

type source struct {
	ID, Conversation, Text string
	Sequence               int64
}

func readSource(ctx context.Context, tx *sql.Tx, p identity.Principal, id string) (source, error) {
	var out source
	err := tx.QueryRowContext(ctx, `SELECT m.id,c.id,m.text,m.sequence FROM chat_messages m JOIN conversations c ON c.id=m.conversation_id WHERE m.id=$1 AND m.role='user' AND m.status='completed' AND c.user_id=$2 AND c.team_id=$3 AND NOT c.markdown FOR SHARE OF c,m`, id, p.UserID, p.TeamID).Scan(&out.ID, &out.Conversation, &out.Text, &out.Sequence)
	return out, err
}

func selectionSource(ctx context.Context, tx *sql.Tx, message source) (source, error) {
	if !rememberPrevious.MatchString(message.Text) {
		return message, nil
	}
	if !safeSource(message.Text) {
		return source{}, nil
	}
	// The conversation was authorized and locked by readSource. Stop at the
	// preceding user turn so a bare command cannot jump over a change of topic.
	var previous source
	err := tx.QueryRowContext(ctx, `SELECT id,conversation_id,text,sequence FROM chat_messages WHERE conversation_id=$1 AND sequence<$2 AND role='user' AND status='completed' ORDER BY sequence DESC LIMIT 1 FOR SHARE`, message.Conversation, message.Sequence).Scan(&previous.ID, &previous.Conversation, &previous.Text, &previous.Sequence)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return previous, err
}

type scanner interface{ Scan(...any) error }

const sourceJoin = ` JOIN chat_messages m ON m.id=e.source_message_id AND m.role='user' AND m.status='completed' JOIN conversations c ON c.id=m.conversation_id AND c.user_id=e.user_id AND c.team_id=e.team_id AND NOT c.markdown `
const entryColumns = `e.id::text,e.category,e.fact_key,e.value,e.source_message_id,c.id,e.revision,e.edited,e.created_at,e.updated_at`

func scanEntry(row scanner) (Entry, error) {
	var e Entry
	err := row.Scan(&e.ID, &e.Category, &e.Key, &e.Value, &e.SourceMessageID, &e.ConversationID, &e.Revision, &e.Edited, &e.CreatedAt, &e.UpdatedAt)
	if err == nil {
		opt, ok := findOption(e.Key, e.Value)
		if !ok || opt.Category != e.Category {
			return e, ErrRejected
		}
		e.Text = opt.Text
	}
	e.CreatedAt = e.CreatedAt.UTC()
	e.UpdatedAt = e.UpdatedAt.UTC()
	return e, err
}
func entries(ctx context.Context, tx *sql.Tx, p identity.Principal) ([]Entry, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+entryColumns+` FROM memory_entries e`+sourceJoin+`WHERE e.user_id=$1 AND e.team_id=$2 ORDER BY e.fact_key,e.value LIMIT 32`, p.UserID, p.TeamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func entry(ctx context.Context, tx *sql.Tx, p identity.Principal, id string) (Entry, error) {
	return scanEntry(tx.QueryRowContext(ctx, `SELECT `+entryColumns+` FROM memory_entries e`+sourceJoin+`WHERE e.id::text=$1 AND e.user_id=$2 AND e.team_id=$3 FOR UPDATE OF e`, id, p.UserID, p.TeamID))
}
func currentEntry(ctx context.Context, tx *sql.Tx, p identity.Principal, key, value string) (*Entry, error) {
	e, err := scanEntry(tx.QueryRowContext(ctx, `SELECT `+entryColumns+` FROM memory_entries e`+sourceJoin+`WHERE e.user_id=$1 AND e.team_id=$2 AND e.slot=$3 FOR UPDATE OF e`, p.UserID, p.TeamID, slot(key, value)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

const proposalColumns = `e.id::text,e.category,e.fact_key,e.value,e.source_message_id,c.id,e.state,e.created_at,e.entry_id::text,e.uncertain`

func scanProposal(row scanner) (Proposal, error) {
	var e Proposal
	err := row.Scan(&e.ID, &e.Category, &e.Key, &e.Value, &e.SourceMessageID, &e.ConversationID, &e.State, &e.CreatedAt, &e.EntryID, &e.Uncertain)
	if err == nil {
		opt, ok := findOption(e.Key, e.Value)
		if !ok || opt.Category != e.Category {
			return e, ErrRejected
		}
		e.Text = opt.Text
	}
	e.CreatedAt = e.CreatedAt.UTC()
	return e, err
}
func proposals(ctx context.Context, tx *sql.Tx, p identity.Principal, message string) ([]Proposal, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+proposalColumns+` FROM memory_proposals e`+sourceJoin+`WHERE e.user_id=$1 AND e.team_id=$2 AND (($3='' AND e.state='pending') OR e.source_message_id=$3) ORDER BY e.created_at DESC,e.id LIMIT 40`, p.UserID, p.TeamID, message)
	if err != nil {
		return nil, err
	}
	out := []Proposal{}
	for rows.Next() {
		e, err := scanProposal(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].State == "pending" {
			out[i].Replaces, err = currentEntry(ctx, tx, p, out[i].Key, out[i].Value)
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
func readExtraction(ctx context.Context, tx *sql.Tx, p identity.Principal, id string) (state, sourceID string, err error) {
	state, sourceID = "not_requested", id
	err = tx.QueryRowContext(ctx, `SELECT state,COALESCE(source_message_id,message_id) FROM memory_extractions WHERE message_id=$1 AND user_id=$2 AND team_id=$3`, id, p.UserID, p.TeamID).Scan(&state, &sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return state, sourceID, err
}

func saveExtraction(ctx context.Context, tx *sql.Tx, p identity.Principal, id, sourceID, state string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO memory_extractions(message_id,user_id,team_id,source_message_id,state) VALUES($1,$2,$3,NULLIF($4,''),$5) ON CONFLICT(message_id) DO UPDATE SET state=EXCLUDED.state,source_message_id=EXCLUDED.source_message_id,updated_at=now()`, id, p.UserID, p.TeamID, sourceID, state)
	return err
}

func (s Store) Proposals(ctx context.Context, id string) (Proposals, error) {
	out := Proposals{State: "not_requested", Items: []Proposal{}}
	err := s.transaction(ctx, func(tx *sql.Tx, p identity.Principal) error {
		message, err := readSource(ctx, tx, p, id)
		if err != nil {
			return err
		}
		state, sourceID, err := readExtraction(ctx, tx, p, id)
		if err != nil {
			return err
		}
		out.State = state
		if sourceID != id {
			original, err := readSource(ctx, tx, p, sourceID)
			if err != nil {
				return err
			}
			if original.Conversation != message.Conversation || original.Sequence >= message.Sequence {
				return ports.ErrNotFound
			}
		}
		out.Items, err = proposals(ctx, tx, p, sourceID)
		return err
	})
	return out, err
}

// Model work is outside the write transaction. A second ACL/source check and the
// owner lock protect the commit; duplicate requests reuse the durable extraction.
func (s Store) Suggest(ctx context.Context, id string) (Proposals, error) {
	previous, err := s.Proposals(ctx, id)
	if err != nil {
		return previous, err
	}
	if previous.State == "ready" || previous.State == "skipped" {
		return previous, nil
	}
	var message, original source
	var originalState string
	err = s.transaction(ctx, func(tx *sql.Tx, p identity.Principal) error {
		var err error
		message, err = readSource(ctx, tx, p, id)
		if err != nil {
			return err
		}
		original, err = selectionSource(ctx, tx, message)
		if err != nil || original.ID == "" || original.ID == id {
			return err
		}
		originalState, _, err = readExtraction(ctx, tx, p, original.ID)
		return err
	})
	if err != nil {
		return previous, err
	}
	state := "skipped"
	var facts []Candidate
	if eligibleSource(original.Text) {
		state = "ready"
		if originalState != "ready" {
			facts = s.selectFacts(ctx, original.Text)
			if facts == nil {
				state = "failed"
			}
		}
	}
	err = s.transaction(ctx, func(tx *sql.Tx, p identity.Principal) error {
		current, e := readSource(ctx, tx, p, id)
		if e != nil {
			return e
		}
		resolved, e := selectionSource(ctx, tx, current)
		if e != nil {
			return e
		}
		if current != message || resolved != original {
			return ErrChanged
		}
		existing, _, e := readExtraction(ctx, tx, p, id)
		if e != nil {
			return e
		}
		if existing == "ready" || existing == "skipped" {
			return nil
		}
		if original.ID != "" && original.ID != id && state != "skipped" {
			cached, _, e := readExtraction(ctx, tx, p, original.ID)
			if e != nil {
				return e
			}
			if cached == "ready" {
				return saveExtraction(ctx, tx, p, id, original.ID, "ready")
			}
		}
		if state != "ready" {
			return saveExtraction(ctx, tx, p, id, "", state)
		}
		for _, f := range facts {
			if _, e = tx.ExecContext(ctx, `INSERT INTO memory_proposals(user_id,team_id,source_message_id,category,fact_key,value,uncertain) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, p.UserID, p.TeamID, original.ID, f.Category, f.Key, f.Value, f.Uncertain); e != nil {
				return e
			}
		}
		if e = saveExtraction(ctx, tx, p, original.ID, "", "ready"); e != nil || original.ID == id {
			return e
		}
		return saveExtraction(ctx, tx, p, id, original.ID, "ready")
	})
	if err != nil {
		return previous, err
	}
	return s.Proposals(ctx, id)
}

func (s Store) selectFacts(ctx context.Context, text string) []Candidate {
	if s.Selector == nil {
		return nil
	}
	work, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	facts, err := s.Selector.Select(work, text)
	if err != nil || len(facts) > 4 {
		return nil
	}
	for _, fact := range facts {
		if !validCandidate(fact, text) {
			return nil
		}
	}
	return facts
}
func (s Store) GetEntry(ctx context.Context, id string) (Entry, error) {
	var out Entry
	err := s.transaction(ctx, func(tx *sql.Tx, p identity.Principal) error {
		var err error
		out, err = entry(ctx, tx, p, id)
		if errors.Is(err, sql.ErrNoRows) {
			_, deleted, checkErr := deletion(ctx, tx, p, id)
			if checkErr != nil {
				return checkErr
			}
			if deleted {
				return ErrDeleted
			}
		}
		return err
	})
	return out, err
}
func (s Store) Decide(ctx context.Context, id string, in Decision) (DecisionResult, error) {
	var out DecisionResult
	if in.Decision != "accept" && in.Decision != "dismiss" || in.ExpectedRevision < 0 {
		return out, ports.ErrInvalid
	}
	err := s.transaction(ctx, func(tx *sql.Tx, p identity.Principal) error {
		q, e := scanProposal(tx.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM memory_proposals e`+sourceJoin+`WHERE e.id::text=$1 AND e.user_id=$2 AND e.team_id=$3 FOR UPDATE OF e`, id, p.UserID, p.TeamID))
		if e != nil {
			return e
		}
		if q.State != "pending" {
			if (q.State == "accepted") != (in.Decision == "accept") {
				return ErrChanged
			}
			if q.EntryID != nil {
				stored, e := entry(ctx, tx, p, *q.EntryID)
				if e != nil {
					return e
				}
				out.Entry = &stored
			}
			out.Proposal = q
			return nil
		}
		if in.Decision == "accept" {
			original, e := readSource(ctx, tx, p, q.SourceMessageID)
			if e != nil {
				return e
			}
			// Recheck the canonical fact against its source at the confirmation boundary.
			if !storedFactAllowed(q.Category, q.Key, q.Value, original.Text) {
				return ErrRejected
			}
			old, e := currentEntry(ctx, tx, p, q.Key, q.Value)
			if e != nil {
				return e
			}
			if old == nil && (in.ExpectedEntryID != nil || in.ExpectedRevision != 0) || old != nil && (in.ExpectedEntryID == nil || *in.ExpectedEntryID != old.ID || in.ExpectedRevision != old.Revision) {
				return ErrChanged
			}
			var entryID string
			e = tx.QueryRowContext(ctx, `INSERT INTO memory_entries(user_id,team_id,slot,category,fact_key,value,source_message_id) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(user_id,team_id,slot) DO UPDATE SET category=EXCLUDED.category,value=EXCLUDED.value,source_message_id=EXCLUDED.source_message_id,edited=false,revision=memory_entries.revision+1,updated_at=now() RETURNING id::text`, p.UserID, p.TeamID, slot(q.Key, q.Value), q.Category, q.Key, q.Value, q.SourceMessageID).Scan(&entryID)
			if e != nil {
				return e
			}
			q.State = "accepted"
			q.EntryID = &entryID
			stored, e := entry(ctx, tx, p, entryID)
			if e != nil {
				return e
			}
			out.Entry = &stored
		} else {
			q.State = "dismissed"
		}
		_, e = tx.ExecContext(ctx, `UPDATE memory_proposals SET state=$2,entry_id=$3 WHERE id::text=$1`, q.ID, q.State, q.EntryID)
		out.Proposal = q
		return e
	})
	return out, err
}
func (s Store) Edit(ctx context.Context, id, value string, revision int64) (Entry, error) {
	var out Entry
	if revision < 1 {
		return out, ports.ErrInvalid
	}
	err := s.transaction(ctx, func(tx *sql.Tx, p identity.Principal) error {
		old, e := entry(ctx, tx, p, id)
		if e != nil {
			return e
		}
		if old.Revision != revision {
			return ErrChanged
		}
		opt, ok := findOption(old.Key, value)
		if !ok {
			return ErrRejected
		}
		other, e := currentEntry(ctx, tx, p, old.Key, value)
		if e != nil {
			return e
		}
		if other != nil && other.ID != id {
			return ErrChanged
		}
		if old.Value == value {
			out = old
			return nil
		}
		_, e = tx.ExecContext(ctx, `UPDATE memory_entries SET value=$2,slot=$3,category=$4,revision=revision+1,edited=true,updated_at=now() WHERE id::text=$1`, id, value, slot(old.Key, value), opt.Category)
		if e != nil {
			return e
		}
		out, e = entry(ctx, tx, p, id)
		return e
	})
	return out, err
}
func (s Store) Delete(ctx context.Context, id string, revision int64) error {
	if revision < 1 {
		return ports.ErrInvalid
	}
	return s.transaction(ctx, func(tx *sql.Tx, p identity.Principal) error {
		old, e := entry(ctx, tx, p, id)
		if errors.Is(e, sql.ErrNoRows) {
			previous, deleted, err := deletion(ctx, tx, p, id)
			if err != nil {
				return err
			}
			if deleted {
				if previous != revision {
					return ErrChanged
				}
				return nil
			}
		}
		if e != nil {
			return e
		}
		if old.Revision != revision {
			return ErrChanged
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM memory_proposals WHERE entry_id::text=$1 AND user_id=$2 AND team_id=$3`, id, p.UserID, p.TeamID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM memory_entries WHERE id::text=$1`, id); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO memory_deletions(entry_id,user_id,team_id,revision) VALUES($1,$2,$3,$4)`, id, p.UserID, p.TeamID, revision)
		return e
	})
}
func deletion(ctx context.Context, tx *sql.Tx, p identity.Principal, id string) (int64, bool, error) {
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM memory_deletions WHERE entry_id::text=$1 AND user_id=$2 AND team_id=$3`, id, p.UserID, p.TeamID).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return revision, err == nil, err
}
