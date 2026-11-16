package app

import (
	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
	"testing"
)

type reviewCheckerOnly struct{ ports.AccessChecker }

func TestReviewSeparateAccessChecker(t *testing.T) {
	db := testkit.Postgres(t)
	_, cookie, _ := prepareDraft(t, db)
	handler := requestHandler(db, Dependencies{Access: reviewCheckerOnly{integrations.New(db)}})
	apiRequest(t, handler, "POST", confirmURL, confirmBody, cookie, 202)
}
