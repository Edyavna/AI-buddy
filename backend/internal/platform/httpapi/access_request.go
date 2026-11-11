package httpapi

import (
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type AccessRequest struct {
	ID             string  `json:"id"`
	Revision       int64   `json:"revision"`
	Version        int64   `json:"draft_version"`
	ConversationID string  `json:"conversation_id"`
	StepID         *string `json:"step_id"`
	Resource       struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Permission string `json:"permission"`
	} `json:"resource"`
	Reason        string     `json:"reason"`
	Status        string     `json:"status"`
	ExternalID    *string    `json:"external_id"`
	CheckedAt     *time.Time `json:"status_checked_at"`
	FailureReason string     `json:"failure_reason,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func RequestDTO(r ports.RequestState) AccessRequest {
	v := AccessRequest{
		ID: r.RequestID, Revision: r.Revision, Version: r.Version,
		ConversationID: r.ConversationID, StepID: r.StepID, Reason: r.Reason,
		Status: r.Status, ExternalID: r.ExternalID, FailureReason: r.FailureReason,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
	if r.StatusCheckedAt != nil {
		checkedAt := r.StatusCheckedAt.UTC()
		v.CheckedAt = &checkedAt
	}
	v.Resource.ID, v.Resource.Name, v.Resource.Permission = r.Resource.ID, r.Resource.Name, r.Resource.Permission
	return v
}
