package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/accessrequests"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestPostgresAccessRequestProcessRecovery(t *testing.T) {
	for _, boundary := range []string{"before-submit", "after-submit"} {
		t.Run(boundary, func(t *testing.T) {
			db := testkit.Postgres(t)
			h, cookie, _ := prepareDraft(t, db)
			provider := integrations.New(db)
			control, err := integrations.NewControl(db, true)
			if err != nil {
				t.Fatal(err)
			}
			ctx := testkit.Context(t.Context())

			var databaseName string
			if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
				t.Fatal(err)
			}
			dsn, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			dsn.Path = "/" + databaseName
			query := dsn.Query()
			query.Del("search_path")
			dsn.RawQuery = query.Encode()

			externalID := killPausedAccessWorker(t, dsn.String(), boundary, cookie)
			submitting := getRequest(t, h, cookie)
			if submitting.Status != "submitting" || submitting.ExternalID != nil || submitting.CheckedAt != nil {
				t.Fatalf("unexpected state after process death: %+v", submitting)
			}
			var attempts int
			if err := db.QueryRowContext(ctx, "SELECT attempt FROM access_request_jobs WHERE request_id=$1", requestID).Scan(&attempts); err != nil || attempts != 1 {
				t.Fatalf("expected a durable claimed job, attempt=%d: %v", attempts, err)
			}
			if boundary == "before-submit" {
				assertExternalCount(t, control, 0)
				if externalID != "" {
					t.Fatalf("unexpected external ID before Submit: %q", externalID)
				}
			} else {
				assertExternalCount(t, control, 1)
				result, err := provider.Lookup(ctx, requestID)
				if err != nil || externalID == "" || result.ExternalID != externalID || result.Status != "pending" {
					t.Fatalf("external result at process death: %+v, expected ID %q: %v", result, externalID, err)
				}
			}
			if plan := decodePlan(t, apiRequest(t, h, "GET", planURL, "", cookie, 200)); plan.Steps[0].Status != "blocked" {
				t.Fatalf("step is not blocked during reconciliation: %+v", plan)
			}

			// Advance the persisted lease after killing the process. The replacement
			// worker claims it through the same due-job query as in production.
			dueNow(t, db)
			runAccessWorkerProcess(t, dsn.String())
			pending := getRequest(t, h, cookie)
			result, err := provider.Lookup(ctx, requestID)
			if err != nil || pending.Status != "pending" || pending.ExternalID == nil ||
				*pending.ExternalID != result.ExternalID || pending.CheckedAt == nil ||
				(externalID != "" && *pending.ExternalID != externalID) {
				t.Fatalf("reconciliation: internal=%+v external=%+v original ID=%q: %v", pending, result, externalID, err)
			}
			repeated := decodeRequest(t, apiRequest(t, h, "POST", confirmURL, confirmBody, cookie, 200))
			if repeated.ExternalID == nil || *repeated.ExternalID != *pending.ExternalID || repeated.Revision != pending.Revision {
				t.Fatalf("repeat changed the pending request: %+v", repeated)
			}
			assertExternalCount(t, control, 1)

			if err := control.Decide(ctx, requestID, "granted", ""); err != nil {
				t.Fatal(err)
			}
			dueNow(t, db)
			runAccessWorkerProcess(t, dsn.String())
			granted := getRequest(t, h, cookie)
			if granted.Status != "granted" || granted.ExternalID == nil || *granted.ExternalID != *pending.ExternalID {
				t.Fatalf("grant changed the external result: %+v", granted)
			}
			if plan := decodePlan(t, apiRequest(t, h, "GET", planURL, "", cookie, 200)); plan.Steps[0].Status != "in_progress" || plan.ProgressPercent != 0 {
				t.Fatalf("grant did not unblock the step: %+v", plan)
			}
			assertExternalCount(t, control, 1)
		})
	}
}

func accessWorkerCommand(t *testing.T, dsn, mode string) *exec.Cmd {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestAccessWorkerProcess$")
	cmd.Env = append(os.Environ(), "ACCESS_WORKER_TEST_DSN="+dsn, "ACCESS_WORKER_TEST_MODE="+mode)
	return cmd
}

func killPausedAccessWorker(t *testing.T, dsn, boundary string, cookie *http.Cookie) string {
	t.Helper()
	cmd := accessWorkerCommand(t, dsn, boundary)
	cmd.Env = append(cmd.Env, "ACCESS_WORKER_TEST_COOKIE="+cookie.Name+"="+cookie.Value)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	line, readErr := bufio.NewReader(stdout).ReadString('\n')
	killErr := cmd.Process.Kill()
	waitErr := cmd.Wait()
	externalID, paused := strings.CutPrefix(strings.TrimSpace(line), "submit-paused:")
	var exitErr *exec.ExitError
	if readErr != nil || !paused || killErr != nil || !errors.As(waitErr, &exitErr) {
		t.Fatalf("worker did not die at %s: output=%q read=%v kill=%v wait=%v", boundary, line, readErr, killErr, waitErr)
	}
	return externalID
}

func runAccessWorkerProcess(t *testing.T, dsn string) {
	t.Helper()
	cmd := accessWorkerCommand(t, dsn, "recover")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("replacement worker: %v\n%s", err, output)
	}
}

func TestAccessWorkerProcess(t *testing.T) {
	dsn := os.Getenv("ACCESS_WORKER_TEST_DSN")
	if dsn == "" {
		return
	}
	db, err := database.Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider := integrations.New(db)
	worker := accessrequests.Worker{Store: accessrequests.Store{DB: db}, Provider: provider}
	mode := os.Getenv("ACCESS_WORKER_TEST_MODE")
	switch mode {
	case "before-submit", "after-submit":
		cookies, err := http.ParseCookie(os.Getenv("ACCESS_WORKER_TEST_COOKIE"))
		if err != nil || len(cookies) != 1 {
			t.Fatalf("invalid worker test session cookie: %v", err)
		}
		h := requestHandler(db, Dependencies{Access: provider})
		apiRequest(t, h, "POST", confirmURL, confirmBody, cookies[0], 202)
		worker.Provider = pausedAccessProvider{AccessProvisioner: provider, boundary: mode}
	case "recover":
	default:
		t.Fatalf("invalid worker test mode %q", mode)
	}
	runDue(t, t.Context(), worker)
}

type pausedAccessProvider struct {
	ports.AccessProvisioner
	boundary string
}

func (p pausedAccessProvider) Submit(ctx context.Context, input ports.AccessSubmission) (ports.AccessResult, error) {
	var result ports.AccessResult
	if p.boundary == "after-submit" {
		var err error
		result, err = p.AccessProvisioner.Submit(ctx, input)
		if err != nil {
			return result, err
		}
	}
	if _, err := fmt.Fprintln(os.Stdout, "submit-paused:"+result.ExternalID); err != nil {
		return result, err
	}
	// The parent holds stdin open until it has killed this process.
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		return result, err
	}
	return result, errors.New("worker process survived the crash barrier")
}
