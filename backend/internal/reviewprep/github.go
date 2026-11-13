package reviewprep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// GitHubClient — явная интеграция с api.github.com. Token необязателен: без него
// действует анонимный лимит, исчерпание которого отличается от прочих сбоев.
type GitHubClient struct {
	HTTP    *http.Client
	BaseURL string // по умолчанию https://api.github.com
	Token   string
}

func (c GitHubClient) Fetch(ctx context.Context, repo string, number int) (PRInfo, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	var pull struct {
		Title string `json:"title"`
		Head  struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.get(ctx, fmt.Sprintf("%s/repos/%s/pulls/%d", base, repo, number), &pull); err != nil {
		return PRInfo{}, err
	}
	if pull.Head.SHA == "" {
		return PRInfo{}, ports.ErrUnavailable
	}
	var runs struct {
		TotalCount int `json:"total_count"`
		CheckRuns  []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`     // queued, in_progress, completed
			Conclusion string `json:"conclusion"` // success, failure, neutral, skipped, timed_out, cancelled, action_required
		} `json:"check_runs"`
	}
	info := PRInfo{Title: pull.Title, HeadSHA: pull.Head.SHA}
	checksURL := fmt.Sprintf("%s/repos/%s/commits/%s/check-runs", base, repo, pull.Head.SHA)
	if err := c.get(ctx, checksURL, &runs); err != nil {
		// Отсутствие check-runs API (редкий 404/410) или сбой проверок не отменяет
		// сведения о PR: фиксируем коммит, результаты CI останутся пустыми.
		if errors.Is(err, ErrPRNotFound) || errors.Is(err, ports.ErrUnavailable) {
			return info, nil
		}
		return PRInfo{}, err
	}
	for _, run := range runs.CheckRuns {
		info.Checks = append(info.Checks, CheckUpdate{Name: run.Name, Status: mapConclusion(run.Status, run.Conclusion)})
	}
	return info, nil
}

// mapConclusion сводит статус и вывод GitHub к трём состояниям UI.
func mapConclusion(status, conclusion string) string {
	if status != "completed" {
		return "skipped"
	}
	switch conclusion {
	case "success":
		return "passed"
	case "neutral", "skipped":
		return "skipped"
	default:
		return "failed"
	}
}

func (c GitHubClient) get(ctx context.Context, url string, dst any) error {
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ports.ErrUnavailable
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return ports.ErrUnavailable
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ports.ErrUnavailable
	}
	switch resp.StatusCode {
	case http.StatusOK:
		if json.Unmarshal(body, dst) != nil {
			return ports.ErrUnavailable
		}
		return nil
	case http.StatusNotFound:
		return ErrPRNotFound
	case http.StatusForbidden, http.StatusTooManyRequests:
		if resp.StatusCode == http.StatusTooManyRequests || strings.Contains(resp.Header.Get("X-RateLimit-Remaining"), "0") {
			return ErrPRRateLimit
		}
		return ErrPRNotFound // 403 без исчерпания лимита — недоступный репозиторий
	case http.StatusUnauthorized:
		return ports.ErrUnavailable // невалидный токен интеграции
	default:
		return ports.ErrUnavailable
	}
}
