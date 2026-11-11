package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

const MaxMarkdownBytes = 1 << 20

var (
	uuidPattern     = regexp.MustCompile("(?i)^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$")
	ErrAlreadyReady = errors.New("document is already indexed")
)

type Author struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Upload struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	Author     Author    `json:"author"`
	QuestionID *string   `json:"question_id"`
	Error      *string   `json:"error"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type UploadDetail struct {
	Upload
	Content string `json:"content"`
}

type UploadInput struct {
	UploadID, QuestionID, Title, Content string
}

type Uploads struct{ DB *sql.DB }

const uploadColumns = `d.id::text,d.title,d.status,d.author_id,u.name,d.question_id::text,d.error,d.created_at,d.updated_at`
const uploadJoin = ` FROM mentor_documents d JOIN users u ON (u.id,u.team_id)=(d.author_id,d.team_id) `
const ownedUpload = `d.author_id=$1 AND d.team_id=$2`

type uploadRow interface{ Scan(...any) error }

func scanUpload(row uploadRow, content *string) (Upload, error) {
	var d Upload
	fields := []any{&d.ID, &d.Title, &d.Status, &d.Author.ID, &d.Author.Name, &d.QuestionID, &d.Error, &d.CreatedAt, &d.UpdatedAt}
	if content != nil {
		fields = append(fields, content)
	}
	err := row.Scan(fields...)
	d.CreatedAt, d.UpdatedAt = d.CreatedAt.UTC(), d.UpdatedAt.UTC()
	return d, err
}

func mentorPrincipal(ctx context.Context) (identity.Principal, error) {
	p, err := identity.Require(ctx)
	if err == nil && p.Role != identity.Mentor {
		err = ports.ErrForbidden
	}
	return p, err
}

func uploadError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return ports.ErrNotFound
	case errors.Is(err, ports.ErrNotFound), errors.Is(err, ports.ErrConflict), errors.Is(err, ErrAlreadyReady):
		return err
	default:
		return ports.ErrUnavailable
	}
}

func (s Uploads) List(ctx context.Context) ([]Upload, error) {
	p, err := mentorPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+uploadColumns+uploadJoin+`WHERE `+ownedUpload+` ORDER BY d.created_at DESC,d.id`, p.UserID, p.TeamID)
	if err != nil {
		return nil, uploadError(err)
	}
	defer rows.Close()
	items := []Upload{}
	for rows.Next() {
		d, err := scanUpload(rows, nil)
		if err != nil {
			return nil, uploadError(err)
		}
		items = append(items, d)
	}
	return items, uploadError(rows.Err())
}

func (s Uploads) Get(ctx context.Context, id string) (UploadDetail, error) {
	p, err := mentorPrincipal(ctx)
	if err != nil {
		return UploadDetail{}, err
	}
	if !uuidPattern.MatchString(id) {
		return UploadDetail{}, ports.ErrNotFound
	}
	var d UploadDetail
	d.Upload, err = scanUpload(s.DB.QueryRowContext(ctx, `SELECT `+uploadColumns+`,d.content`+uploadJoin+`WHERE `+ownedUpload+` AND d.id=$3`, p.UserID, p.TeamID, id), &d.Content)
	return d, uploadError(err)
}

func (s Uploads) Create(ctx context.Context, in UploadInput) (Upload, bool, error) {
	p, err := mentorPrincipal(ctx)
	if err != nil {
		return Upload{}, false, err
	}
	in.Title = strings.TrimSpace(in.Title)
	in.QuestionID = strings.ToLower(in.QuestionID)
	if !uuidPattern.MatchString(in.UploadID) || (in.QuestionID != "" && !uuidPattern.MatchString(in.QuestionID)) ||
		in.Title == "" || utf8.RuneCountInString(in.Title) > 200 || !validText(in.Title) ||
		len(in.Content) > MaxMarkdownBytes || strings.TrimSpace(strings.TrimPrefix(in.Content, "\uFEFF")) == "" || !validText(in.Content) {
		return Upload{}, false, ports.ErrInvalid
	}
	var d Upload
	var created bool
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		// Serialize requests for this author/key, including the first insert.
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1729011,hashtext($1::text || ':' || lower($2::text)))`, p.UserID, in.UploadID); err != nil {
			return err
		}
		var content string
		var err error
		d, err = scanUpload(tx.QueryRowContext(ctx, `SELECT `+uploadColumns+`,d.content`+uploadJoin+`WHERE `+ownedUpload+` AND d.upload_id=$3`, p.UserID, p.TeamID, in.UploadID), &content)
		if err == nil {
			question := ""
			if d.QuestionID != nil {
				question = *d.QuestionID
			}
			if d.Title != in.Title || question != in.QuestionID || content != in.Content {
				return ports.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if in.QuestionID != "" {
			var found int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM knowledge_gaps q JOIN mentor_assignments a
				ON (a.intern_id,a.team_id,a.mentor_id)=(q.student_id,q.team_id,q.mentor_id)
				WHERE q.id=$1 AND q.mentor_id=$2 AND q.team_id=$3 FOR SHARE OF q,a`, in.QuestionID, p.UserID, p.TeamID).Scan(&found); err != nil {
				return err
			}
		}
		var id string
		err = tx.QueryRowContext(ctx, `INSERT INTO mentor_documents(author_id,team_id,upload_id,question_id,title,content)
			VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6) RETURNING id::text`, p.UserID, p.TeamID, in.UploadID, in.QuestionID, in.Title, in.Content).Scan(&id)
		if err != nil {
			return err
		}
		d, err = scanUpload(tx.QueryRowContext(ctx, `SELECT `+uploadColumns+uploadJoin+`WHERE d.id=$1`, id), nil)
		created = err == nil
		return err
	})
	return d, created, uploadError(err)
}

func validText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func (s Uploads) Retry(ctx context.Context, id string) (Upload, error) {
	p, err := mentorPrincipal(ctx)
	if err != nil {
		return Upload{}, err
	}
	if !uuidPattern.MatchString(id) {
		return Upload{}, ports.ErrNotFound
	}
	read := func() (Upload, error) {
		return scanUpload(s.DB.QueryRowContext(ctx, `SELECT `+uploadColumns+uploadJoin+
			`WHERE `+ownedUpload+` AND d.id=$3`, p.UserID, p.TeamID, id), nil)
	}
	doc, err := read()
	if err != nil {
		return Upload{}, uploadError(err)
	}
	switch doc.Status {
	case "ready":
		return Upload{}, ErrAlreadyReady
	case "processing":
		return doc, nil
	}
	var updated time.Time
	err = s.DB.QueryRowContext(ctx, `UPDATE mentor_documents d
		SET status='processing',error=NULL,updated_at=clock_timestamp()
		WHERE `+ownedUpload+` AND d.id=$3 AND d.status='failed' AND d.updated_at=$4
		RETURNING updated_at`, p.UserID, p.TeamID, id, doc.UpdatedAt).Scan(&updated)
	if errors.Is(err, sql.ErrNoRows) {
		// A concurrent request already retried this failure.
		doc, err = read()
		return doc, uploadError(err)
	}
	if err != nil {
		return Upload{}, uploadError(err)
	}
	doc.Status, doc.Error, doc.UpdatedAt = "processing", nil, updated.UTC()
	return doc, nil
}

// CheckReady holds the ACL and readiness rows until the question update commits.
func (s Uploads) CheckReady(ctx context.Context, tx *sql.Tx, mentor, student identity.Principal, ids []string) error {
	if mentor.TeamID != student.TeamID {
		return ports.ErrNotFound
	}
	for _, id := range ids {
		if !uuidPattern.MatchString(id) {
			return ports.ErrNotFound
		}
		var found int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM mentor_documents u JOIN knowledge_documents d ON d.id=u.id::text
			WHERE u.id=$1 AND u.status='ready' AND u.author_id=$2 AND u.team_id=$3 AND d.team_id=$3
			AND (d.users='[]'::jsonb OR (d.users ? $2 AND d.users ? $4)) FOR SHARE OF u,d`, id, mentor.UserID, mentor.TeamID, student.UserID).Scan(&found)
		if err != nil {
			return uploadError(err)
		}
	}
	return nil
}
