// Package reviewprep хранит подготовку стажёра к ревью PR: черновик, отправку
// наставнику, командный чек-лист и результаты проверок с обязательным источником.
package reviewprep

import (
	"context"
	"errors"
	"time"
)

var (
	ErrChanged     = errors.New("review preparation changed")
	ErrTransition  = errors.New("review preparation transition forbidden")
	ErrPRNotFound  = errors.New("pull request not found or repository inaccessible")
	ErrPRRateLimit = errors.New("github rate limit exceeded")
)

// TemplateItem — один пункт командного чек-листа.
type TemplateItem struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// Template — действующий шаблон команды: свой, если наставник сохранил, иначе DefaultTemplate.
type Template struct {
	Items     []TemplateItem `json:"items"`
	Revision  int64          `json:"revision"`
	UpdatedAt time.Time      `json:"updated_at"`
	UpdatedBy string         `json:"updated_by"`
	Custom    bool           `json:"custom"`
}

// DefaultTemplate действует, пока наставник команды не сохранил свой шаблон.
var DefaultTemplate = []TemplateItem{
	{Key: "task_done", Label: "Код решает задачу целиком", Required: true},
	{Key: "tests_added", Label: "Добавлены или обновлены тесты", Required: true},
	{Key: "checks_green", Label: "Проверки CI проходят на head-коммите", Required: true},
	{Key: "docs_updated", Label: "Документация и описание PR обновлены", Required: false},
	{Key: "no_secrets", Label: "Нет секретов, отладочного кода и лишних файлов", Required: true},
}

// ItemAnswer — ответ стажёра на пункт чек-листа.
type ItemAnswer struct {
	Done bool   `json:"done"`
	Note string `json:"note"`
}

// Answers — заполненный стажёром чек-лист и сопроводительные поля.
type Answers struct {
	Summary      string                `json:"summary"`
	Verification string                `json:"verification"`
	Limitations  string                `json:"limitations"`
	Questions    string                `json:"questions"`
	Items        map[string]ItemAnswer `json:"items"`
}

// CheckResult — результат одной проверки; Source обязателен и показывается в UI.
type CheckResult struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"` // passed | failed | skipped
	Source    string    `json:"source"` // intern | ci
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

// PRSnapshot — сведения о PR: из интеграции GitHub или заполнены стажёром вручную.
type PRSnapshot struct {
	URL        string     `json:"url"`
	Repo       string     `json:"repo"`
	Number     int        `json:"number"`
	Title      string     `json:"title"`
	HeadSHA    string     `json:"head_sha"`
	Source     string     `json:"source"` // github | manual
	FetchError string     `json:"fetch_error"`
	FetchedAt  *time.Time `json:"fetched_at"`
}

// HistoryEntry — видимая наставнику запись об отправленной ревизии.
type HistoryEntry struct {
	Revision    int64     `json:"revision"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// Preparation — сохраняемая подготовка к ревью.
type Preparation struct {
	ID          string         `json:"id"`
	Status      string         `json:"status"` // draft | submitted
	Revision    int64          `json:"revision"`
	StepID      *string        `json:"step_id"`
	PR          PRSnapshot     `json:"pr"`
	Description string         `json:"description"`
	Answers     Answers        `json:"answers"`
	Checks      []CheckResult  `json:"checks"`
	History     []HistoryEntry `json:"history"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	SubmittedAt *time.Time     `json:"submitted_at"`
}

// Student — автор подготовки в списке наставника.
type Student struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListItem — карточка подготовки в списке наставника.
type ListItem struct {
	Preparation
	Student Student `json:"student"`
}

// Update — тело PUT: новое состояние черновика или обновление отправленной подготовки.
type Update struct {
	Revision    int64         `json:"revision"`
	Description string        `json:"description"`
	Answers     Answers       `json:"answers"`
	Checks      []CheckUpdate `json:"checks"`
}

// CheckUpdate — результат проверки из UI; source выставляет сервер: стажёру
// доступен только intern, ci попадает сюда только через интеграцию.
type CheckUpdate struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Submit — явное подтверждение пользователем отправки на ревью.
type Submit struct {
	Revision int64 `json:"revision"`
}

// HelpResult — подсказка ассистента: формулировка описания и пропущенные пункты.
type HelpResult struct {
	Summary string   `json:"summary"`
	Missing []string `json:"missing"`
}

// Assistant помогает сформулировать описание и найти пропуски по сохранённым данным.
type Assistant interface {
	Help(ctx context.Context, prep Preparation, template Template) (HelpResult, error)
}

// PRInfo — данные интеграции о PR и его проверках на конкретном коммите.
type PRInfo struct {
	Title   string
	HeadSHA string
	Checks  []CheckUpdate
}

// PRProvider получает сведения о PR; nil означает ручной режим без интеграции.
type PRProvider interface {
	Fetch(ctx context.Context, repo string, number int) (PRInfo, error)
}
