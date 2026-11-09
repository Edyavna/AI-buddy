// Package identity carries authenticated server-side identity, never model arguments.
package identity

import (
	"context"
	"errors"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type Role string

const (
	Intern Role = "intern"
	Mentor Role = "mentor"
)

type Principal struct {
	UserID string
	TeamID string
	Role   Role
}

type principalKey struct{}

// WithPrincipal is for the session middleware after verification, and test setup.
// Never construct a Principal from HTTP body fields or LLM tool arguments.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func Require(ctx context.Context) (Principal, error) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	if !ok || p.UserID == "" || p.TeamID == "" || (p.Role != Intern && p.Role != Mentor) {
		return Principal{}, ErrUnauthenticated
	}
	return p, nil
}
