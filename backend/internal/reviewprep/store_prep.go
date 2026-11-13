package reviewprep

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// ErrDraftExists — у стажёра уже есть неотправленная подготовка.
var ErrDraftExists = errors.New("review preparation draft already exists")

const prepColumns = `p.id::text,p.intern_id,p.step_id,p.pr_url,p.pr_repo,p.pr_number,p.pr_title,p.pr_head_sha,
 p.pr_source,p.pr_fetch_error,p.pr_fetched_at,p.status,p.revision,p.answers,p.description,p.created_at,p.updated_at,p.submitted_at`

func scanPrep(row interface{ Scan(...any) error }) (Preparation, error) {
	var prep Preparation
	var stepID, fetchErr sql.NullString
	var fetchedAt, submittedAt sql.NullTime
	var answers []byte
	err := row.Scan(&prep.ID, &prep.PR.Repo, &stepID, &prep.PR.URL, &prep.PR.Repo, &prep.PR.Number,
		&prep.PR.Title, &prep.PR.HeadSHA, &prep.PR.Source, &fetchErr, &fetchedAt, &prep.Status, &prep.Revision,
		&answers, &prep.Description, &prep.CreatedAt, &prep.UpdatedAt, &submittedAt)
	if err != nil {
		return prep, err
	}
	if stepID.Valid {
		prep.StepID = &stepID.String
	}
	if fetchErr.Valid {
		prep.PR.FetchError = fetchErr.String
	}
	if fetchedAt.Valid {
		t := fetchedAt.Time.UTC()
		prep.PR.FetchedAt = &t
	}
	if submittedAt.Valid {
		t := submittedAt.Time.UTC()
		prep.SubmittedAt = &t
	}
	prep.CreatedAt, prep.UpdatedAt = prep.CreatedAt.UTC(), prep.UpdatedAt.UTC()
	if json.Unmarshal(answers, &prep.Answers) != nil {
		return prep, errors.New("corrupt answers")
	}
	if prep.Answers.Items == nil {
		prep.Answers.Items = map[string]ItemAnswer{}
	}
	return prep, nil
}

func loadChecks(ctx context.Context, db *sql.DB, prepID string) ([]CheckResult, error) {
	rows, err := db.QueryContext(ctx, `SELECT id::text,name,status,source,detail,created_at
    FROM review_preparation_checks WHERE preparation_id=$1 ORDER BY created_at,id`, prepID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checks := []CheckResult{}
	for rows.Next() {
		var check CheckResult
		if err := rows.Scan(&check.ID, &check.Name, &check.Status, &check.Source, &check.Detail, &check.CreatedAt); err != nil {
			return nil, err
		}
		check.CreatedAt = check.CreatedAt.UTC()
		checks = append(checks, check)
	}
	return checks, rows.Err()
}

func loadHistory(ctx context.Context, db *sql.DB, prepID string) ([]HistoryEntry, error) {
	rows, err := db.QueryContext(ctx, `SELECT revision,submitted_at FROM review_preparation_history
    WHERE preparation_id=$1 ORDER BY revision`, prepID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	history := []HistoryEntry{}
	for rows.Next() {
		var entry HistoryEntry
		if err := rows.Scan(&entry.Revision, &entry.SubmittedAt); err != nil {
			return nil, err
		}
		entry.SubmittedAt = entry.SubmittedAt.UTC()
		history = append(history, entry)
	}
	return history, rows.Err()
}

func (s Store) fill(ctx context.Context, prep *Preparation) error {
	checks, err := loadChecks(ctx, s.DB, prep.ID)
	if err != nil {
		return err
	}
	history, err := loadHistory(ctx, s.DB, prep.ID)
	if err != nil {
		return err
	}
	prep.Checks, prep.History = checks, history
	return nil
}

// Latest возвращает последнюю подготовку стажёра: неотправленный черновик важнее отправленной.
func (s Store) Latest(ctx context.Context) (*Preparation, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return nil, err
	}
	row := s.DB.QueryRowContext(ctx, `SELECT `+prepColumns+` FROM review_preparations p
    WHERE p.intern_id=$1 AND p.team_id=$2
    ORDER BY CASE WHEN p.status='draft' THEN 0 ELSE 1 END, p.updated_at DESC, p.id LIMIT 1`, p.UserID, p.TeamID)
	prep, err := scanPrep(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapped(err)
	}
	return &prep, s.fill(ctx, &prep)
}

// Get отдаёт подготовку стажёра только её автору.
func (s Store) Get(ctx context.Context, id string) (Preparation, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return Preparation{}, err
	}
	prep, err := scanPrep(s.DB.QueryRowContext(ctx, `SELECT `+prepColumns+` FROM review_preparations p
    WHERE p.id::text=$1 AND p.intern_id=$2 AND p.team_id=$3`, id, p.UserID, p.TeamID))
	if err != nil {
		return Preparation{}, mapped(err)
	}
	return prep, s.fill(ctx, &prep)
}

// fetchErrorText превращает сбой интеграции в безопасное сообщение для UI.
func fetchErrorText(err error) string {
	switch {
	case errors.Is(err, ErrPRNotFound):
		return "Репозиторий недоступен: проверьте ссылку и права доступа"
	case errors.Is(err, ErrPRRateLimit):
		return "Превышен лимит запросов GitHub, повторите позже"
	default:
		return "Интеграция GitHub недоступна, сведения можно заполнить вручную"
	}
}

// Create создаёт черновик подготовки. Сбой интеграции GitHub не отменяет черновик:
// ошибка сохраняется в pr_fetch_error, а сведения заполняются вручную.
func (s Store) Create(ctx context.Context, prURL string, stepID *string, manual bool) (Preparation, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return Preparation{}, err
	}
	repo, number, err := ParsePRURL(prURL)
	if err != nil {
		return Preparation{}, err
	}
	if stepID != nil && *stepID == "" {
		stepID = nil
	}
	var mentorID string
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var err error
		mentorID, err = mentorOfTx(ctx, tx, p)
		return err
	})
	if err != nil {
		return Preparation{}, mapped(err)
	}

	source := "github"
	fetchError := ""
	info := PRInfo{}
	var fetchedAt any
	integrate := !manual && s.Provider != nil
	if integrate {
		info, err = s.Provider.Fetch(ctx, repo, number)
		if err != nil {
			fetchError = fetchErrorText(err)
		} else {
			fetchedAt = time.Now()
		}
	} else {
		source = "manual"
	}

	var prep Preparation
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if stepID != nil {
			var belongs int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM onboarding_steps s JOIN onboarding_plans pl ON pl.id=s.plan_id
        WHERE s.id=$1 AND pl.user_id=$2 AND pl.team_id=$3`, *stepID, p.UserID, p.TeamID).Scan(&belongs); err != nil {
				return err
			}
		}
		var id string
		err := tx.QueryRowContext(ctx, `INSERT INTO review_preparations
      (team_id,intern_id,mentor_id,step_id,pr_url,pr_repo,pr_number,pr_title,pr_head_sha,pr_source,pr_fetch_error,pr_fetched_at,status,answers)
      VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'draft','{}'::jsonb)
      ON CONFLICT (intern_id, team_id) WHERE status='draft' DO NOTHING
      RETURNING id::text`, p.TeamID, p.UserID, mentorID, stepID, prURL, repo, number, info.Title, info.HeadSHA, source, fetchError, fetchedAt).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDraftExists
		}
		if err != nil {
			return err
		}
		prep, err = scanPrep(tx.QueryRowContext(ctx, `SELECT `+prepColumns+` FROM review_preparations p WHERE p.id::text=$1`, id))
		return err
	})
	if err != nil {
		return Preparation{}, mapped(err)
	}
	if integrate && fetchError == "" && len(info.Checks) > 0 {
		if err := s.replaceCIChecks(ctx, prep.ID, info.Checks); err != nil {
			return Preparation{}, mapped(err)
		}
	}
	return prep, s.fill(ctx, &prep)
}

func (s Store) replaceCIChecks(ctx context.Context, prepID string, checks []CheckUpdate) error {
	return database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM review_preparation_checks WHERE preparation_id=$1 AND source='ci'`, prepID); err != nil {
			return err
		}
		for _, check := range checks {
			if _, err := tx.ExecContext(ctx, `INSERT INTO review_preparation_checks(preparation_id,name,status,source,detail) VALUES ($1,$2,$3,'ci','')`,
				prepID, check.Name, check.Status); err != nil {
				return err
			}
		}
		return nil
	})
}

func snapshotOf(prep Preparation) (string, error) {
	payload, err := json.Marshal(struct {
		Status      string        `json:"status"`
		PR          PRSnapshot    `json:"pr"`
		Description string        `json:"description"`
		Answers     Answers       `json:"answers"`
		Checks      []CheckResult `json:"checks"`
	}{prep.Status, prep.PR, prep.Description, prep.Answers, prep.Checks})
	return string(payload), err
}

// writeHistory фиксирует текущую ревизию как отправленную; повторная отправка той же ревизии идемпотентна.
func writeHistory(ctx context.Context, tx *sql.Tx, prep Preparation) error {
	snapshot, err := snapshotOf(prep)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO review_preparation_history(preparation_id,revision,snapshot,submitted_at)
    VALUES ($1,$2,$3,clock_timestamp()) ON CONFLICT DO NOTHING`, prep.ID, prep.Revision, snapshot)
	return err
}

func replaceInternChecks(ctx context.Context, tx *sql.Tx, prepID string, checks []CheckUpdate) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM review_preparation_checks WHERE preparation_id=$1 AND source='intern'`, prepID); err != nil {
		return err
	}
	for _, check := range checks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO review_preparation_checks(preparation_id,name,status,source,detail) VALUES ($1,$2,$3,'intern',$4)`,
			prepID, check.Name, check.Status, check.Detail); err != nil {
			return err
		}
	}
	return nil
}

// lockOwnPrep блокирует подготовку стажёра и проверяет ревизию.
func lockOwnPrep(ctx context.Context, tx *sql.Tx, p identity.Principal, id string, revision int64) (Preparation, error) {
	prep, err := scanPrep(tx.QueryRowContext(ctx, `SELECT `+prepColumns+` FROM review_preparations p
    WHERE p.id::text=$1 AND p.intern_id=$2 AND p.team_id=$3 FOR UPDATE OF p`, id, p.UserID, p.TeamID))
	if err != nil {
		return prep, err
	}
	if prep.Revision != revision {
		return prep, ErrChanged
	}
	return prep, nil
}

// Update saves changed content and increments its revision.
func (s Store) Update(ctx context.Context, id string, in Update) (Preparation, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return Preparation{}, err
	}
	if in.Revision < 1 {
		return Preparation{}, ports.ErrInvalid
	}
	if len([]rune(in.Description)) > 20000 {
		return Preparation{}, ports.ErrInvalid
	}
	if err := validateAnswers(in.Answers); err != nil {
		return Preparation{}, err
	}
	if err := validateChecks(in.Checks); err != nil {
		return Preparation{}, err
	}
	answers := normalizeAnswers(in.Answers)
	checks := normalizeChecks(in.Checks)
	payload, err := json.Marshal(answers)
	if err != nil {
		return Preparation{}, ports.ErrUnavailable
	}
	var prep Preparation
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		current, err := lockOwnPrep(ctx, tx, p, id, in.Revision)
		if err != nil {
			return err
		}
		if err := s.loadChecksTx(ctx, tx, &current); err != nil {
			return err
		}
		description := clamp(in.Description, 20000)
		if current.Description == description && sameAnswers(current.Answers, answers) && sameInternChecks(current.Checks, checks) {
			prep = current
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE review_preparations SET description=$2,answers=$3,revision=revision+1,updated_at=clock_timestamp() WHERE id::text=$1`,
			id, description, payload); err != nil {
			return err
		}
		if err := replaceInternChecks(ctx, tx, id, checks); err != nil {
			return err
		}
		prep, err = scanPrep(tx.QueryRowContext(ctx, `SELECT `+prepColumns+` FROM review_preparations p WHERE p.id::text=$1`, id))
		return err
	})
	if err != nil {
		return Preparation{}, mapped(err)
	}
	return prep, s.fill(ctx, &prep)
}

func (s Store) loadChecksTx(ctx context.Context, tx *sql.Tx, prep *Preparation) error {
	rows, err := tx.QueryContext(ctx, `SELECT id::text,name,status,source,detail,created_at
    FROM review_preparation_checks WHERE preparation_id=$1 ORDER BY created_at,id`, prep.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	prep.Checks = []CheckResult{}
	for rows.Next() {
		var check CheckResult
		if err := rows.Scan(&check.ID, &check.Name, &check.Status, &check.Source, &check.Detail, &check.CreatedAt); err != nil {
			return err
		}
		check.CreatedAt = check.CreatedAt.UTC()
		prep.Checks = append(prep.Checks, check)
	}
	return rows.Err()
}

func sameAnswers(a, b Answers) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

func sameInternChecks(existing []CheckResult, incoming []CheckUpdate) bool {
	var intern []CheckResult
	for _, check := range existing {
		if check.Source == "intern" {
			intern = append(intern, check)
		}
	}
	if len(intern) != len(incoming) {
		return false
	}
	for i, check := range intern {
		if check.Name != incoming[i].Name || check.Status != incoming[i].Status || check.Detail != incoming[i].Detail {
			return false
		}
	}
	return true
}

// Submit — явное действие пользователя: draft → submitted, повторная отправка
// отправленной подготовки создаёт новую ревизию с историей.
func (s Store) Submit(ctx context.Context, id string, in Submit) (Preparation, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return Preparation{}, err
	}
	if in.Revision < 1 {
		return Preparation{}, ports.ErrInvalid
	}
	var prep Preparation
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		current, err := lockOwnPrep(ctx, tx, p, id, in.Revision)
		if err != nil {
			return err
		}
		if err := s.loadChecksTx(ctx, tx, &current); err != nil {
			return err
		}
		if current.Status != "draft" && current.Status != "submitted" {
			return ErrTransition
		}
		if err := writeHistory(ctx, tx, current); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE review_preparations SET status='submitted',revision=revision+1,
      submitted_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id::text=$1`, id); err != nil {
			return err
		}
		prep, err = scanPrep(tx.QueryRowContext(ctx, `SELECT `+prepColumns+` FROM review_preparations p WHERE p.id::text=$1`, id))
		return err
	})
	if err != nil {
		return Preparation{}, mapped(err)
	}
	return prep, s.fill(ctx, &prep)
}

// Refresh синхронизирует сведения о PR с GitHub и привязывает CI-результаты
// к новому head-коммиту. Возвращает признак смены коммита с момента последней синхронизации.
func (s Store) Refresh(ctx context.Context, id string) (Preparation, bool, error) {
	p, err := principal(ctx, identity.Intern)
	if err != nil {
		return Preparation{}, false, err
	}
	if s.Provider == nil {
		return Preparation{}, false, ErrTransition
	}
	current, err := s.Get(ctx, id)
	if err != nil {
		return Preparation{}, false, err
	}
	if current.PR.Source != "github" {
		return Preparation{}, false, ErrTransition
	}
	info, err := s.Provider.Fetch(ctx, current.PR.Repo, current.PR.Number)
	if err != nil {
		return Preparation{}, false, mapped(err)
	}
	changed := current.PR.HeadSHA != "" && current.PR.HeadSHA != info.HeadSHA
	var prep Preparation
	err = database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		locked, err := lockOwnPrep(ctx, tx, p, id, current.Revision)
		if err != nil {
			return err
		}
		_ = locked
		if _, err := tx.ExecContext(ctx, `UPDATE review_preparations SET pr_title=$2,pr_head_sha=$3,pr_fetch_error='',
      pr_fetched_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id::text=$1`, id, info.Title, info.HeadSHA); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM review_preparation_checks WHERE preparation_id=$1 AND source='ci'`, id); err != nil {
			return err
		}
		for _, check := range info.Checks {
			if _, err := tx.ExecContext(ctx, `INSERT INTO review_preparation_checks(preparation_id,name,status,source,detail) VALUES ($1,$2,$3,'ci','')`,
				id, check.Name, check.Status); err != nil {
				return err
			}
		}
		prep, err = scanPrep(tx.QueryRowContext(ctx, `SELECT `+prepColumns+` FROM review_preparations p WHERE p.id::text=$1`, id))
		return err
	})
	if err != nil {
		return Preparation{}, false, mapped(err)
	}
	return prep, changed, s.fill(ctx, &prep)
}

// MentorList показывает наставнику подготовки назначенных стажёров.
func (s Store) MentorList(ctx context.Context, internID string) ([]ListItem, error) {
	p, err := principal(ctx, identity.Mentor)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+prepColumns+`,u.id,u.name FROM review_preparations p
    JOIN users u ON u.id=p.intern_id AND u.team_id=p.team_id
    WHERE p.mentor_id=$1 AND p.team_id=$2 AND ($3='' OR p.intern_id=$3) AND EXISTS (
      SELECT 1 FROM mentor_assignments a WHERE a.intern_id=p.intern_id AND a.mentor_id=p.mentor_id AND a.team_id=p.team_id)
    ORDER BY p.updated_at DESC,p.id`, p.UserID, p.TeamID, internID)
	if err != nil {
		return nil, mapped(err)
	}
	defer rows.Close()
	items := []ListItem{}
	for rows.Next() {
		var item ListItem
		var stepID, fetchErr sql.NullString
		var fetchedAt, submittedAt sql.NullTime
		var answers []byte
		if err := rows.Scan(&item.ID, &item.PR.Repo, &stepID, &item.PR.URL, &item.PR.Repo, &item.PR.Number,
			&item.PR.Title, &item.PR.HeadSHA, &item.PR.Source, &fetchErr, &fetchedAt, &item.Status, &item.Revision,
			&answers, &item.Description, &item.CreatedAt, &item.UpdatedAt, &submittedAt, &item.Student.ID, &item.Student.Name); err != nil {
			return nil, mapped(err)
		}
		if stepID.Valid {
			item.StepID = &stepID.String
		}
		if fetchErr.Valid {
			item.PR.FetchError = fetchErr.String
		}
		if fetchedAt.Valid {
			t := fetchedAt.Time.UTC()
			item.PR.FetchedAt = &t
		}
		if submittedAt.Valid {
			t := submittedAt.Time.UTC()
			item.SubmittedAt = &t
		}
		item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
		if json.Unmarshal(answers, &item.Answers) != nil {
			return nil, ports.ErrUnavailable
		}
		if item.Answers.Items == nil {
			item.Answers.Items = map[string]ItemAnswer{}
		}
		items = append(items, item)
	}
	return items, mapped(rows.Err())
}

// MentorGet отдаёт полную подготовку назначенного стажёра с историей ревизий.
func (s Store) MentorGet(ctx context.Context, id string) (Preparation, error) {
	p, err := principal(ctx, identity.Mentor)
	if err != nil {
		return Preparation{}, err
	}
	prep, err := scanPrep(s.DB.QueryRowContext(ctx, `SELECT `+prepColumns+` FROM review_preparations p
    WHERE p.id::text=$1 AND p.mentor_id=$2 AND p.team_id=$3 AND EXISTS (
      SELECT 1 FROM mentor_assignments a WHERE a.intern_id=p.intern_id AND a.mentor_id=p.mentor_id AND a.team_id=p.team_id)`, id, p.UserID, p.TeamID))
	if err != nil {
		return Preparation{}, mapped(err)
	}
	return prep, s.fill(ctx, &prep)
}
