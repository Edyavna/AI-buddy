package reviewprep

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func githubServer(t *testing.T, handler http.Handler) (*httptest.Server, GitHubClient) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, GitHubClient{HTTP: server.Client(), BaseURL: server.URL}
}

func TestGitHubFetchSuccess(t *testing.T) {
	_, client := githubServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/5":
			_, _ = w.Write([]byte(`{"title":"Добавить тесты","head":{"sha":"abc123"}}`))
		case "/repos/o/r/commits/abc123/check-runs":
			_, _ = w.Write([]byte(`{"total_count":4,"check_runs":[
				{"name":"build","status":"completed","conclusion":"success"},
				{"name":"lint","status":"completed","conclusion":"failure"},
				{"name":"e2e","status":"completed","conclusion":"skipped"},
				{"name":"deploy","status":"in_progress","conclusion":null}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	info, err := client.Fetch(context.Background(), "o/r", 5)
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Добавить тесты" || info.HeadSHA != "abc123" {
		t.Fatalf("unexpected info %+v", info)
	}
	want := []CheckUpdate{{"build", "passed", ""}, {"lint", "failed", ""}, {"e2e", "skipped", ""}, {"deploy", "skipped", ""}}
	if len(info.Checks) != len(want) {
		t.Fatalf("got %d checks, want %d", len(info.Checks), len(want))
	}
	for i, check := range want {
		if info.Checks[i].Name != check.Name || info.Checks[i].Status != check.Status {
			t.Fatalf("check %d: got %+v, want %+v", i, info.Checks[i], check)
		}
	}
}

func TestGitHubFetchMissingPull(t *testing.T) {
	_, client := githubServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	_, err := client.Fetch(context.Background(), "o/r", 5)
	if !errors.Is(err, ErrPRNotFound) {
		t.Fatalf("expected ErrPRNotFound, got %v", err)
	}
}

func TestGitHubFetchRateLimit(t *testing.T) {
	_, client := githubServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
	}))
	_, err := client.Fetch(context.Background(), "o/r", 5)
	if !errors.Is(err, ErrPRRateLimit) {
		t.Fatalf("expected ErrPRRateLimit, got %v", err)
	}
}

func TestGitHubFetchForbiddenWithoutRateLimit(t *testing.T) {
	_, client := githubServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Resource not accessible"}`, http.StatusForbidden)
	}))
	_, err := client.Fetch(context.Background(), "o/r", 5)
	if !errors.Is(err, ErrPRNotFound) {
		t.Fatalf("expected ErrPRNotFound for inaccessible repo, got %v", err)
	}
}

func TestGitHubFetchUnavailable(t *testing.T) {
	_, client := githubServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	_, err := client.Fetch(context.Background(), "o/r", 5)
	if !errors.Is(err, ports.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

// Отсутствие check-runs у коммита (приватный CI или ещё не запускался) не отменяет сведения о PR.
func TestGitHubFetchWithoutChecksKeepsInfo(t *testing.T) {
	_, client := githubServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/5":
			_, _ = w.Write([]byte(`{"title":"Без CI","head":{"sha":"sha9"}}`))
		case "/repos/o/r/commits/sha9/check-runs":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	info, err := client.Fetch(context.Background(), "o/r", 5)
	if err != nil {
		t.Fatal(err)
	}
	if info.HeadSHA != "sha9" || len(info.Checks) != 0 {
		t.Fatalf("unexpected info %+v", info)
	}
}

func TestGitHubFetchBrokenJSON(t *testing.T) {
	_, client := githubServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{`))
	}))
	_, err := client.Fetch(context.Background(), "o/r", 5)
	if !errors.Is(err, ports.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}
