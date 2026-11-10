package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/demo"
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

func TestRealDemoGrantEnablesStepCompletion(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := testkit.Context(context.Background())
	if err := demo.Seed(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DemoMode: true, SessionTTL: time.Hour, AllowedOrigins: []string{"http://localhost:5173"}}
	h := NewHandler(cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, Dependencies{})
	cookie := loginCookie(t, h, testkit.UserID, nil)
	p := integrations.New(db)
	task, err := p.FirstTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Submit(ctx, ports.AccessSubmission{RequestID: "http-grant", Resource: task.Resource, Reason: "Демо"}); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"completed"}`, cookie, 409)
	c, _ := integrations.NewControl(db, true)
	if err := c.Decide(ctx, "http-grant", "granted", ""); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, h, "PATCH", "/api/v1/onboarding/steps/step_local_setup", `{"status":"completed"}`, cookie, 200)
	apiRequest(t, h, "POST", "/api/v1/demo-control/approve", `{}`, cookie, 404)
}
