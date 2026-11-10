// Package auth owns server-side sessions. Only verified sessions establish identity.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
)

type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type User struct {
	ID   string        `json:"id"`
	Name string        `json:"name"`
	Role identity.Role `json:"role"`
	Team Team          `json:"team"`
}

type Store interface {
	Accounts(context.Context) ([]User, error)
	CreateSession(ctx context.Context, accountID string, hash, oldHash []byte, expires time.Time) (User, error)
	Resolve(ctx context.Context, hash []byte, now time.Time) (User, error)
	DeleteSession(ctx context.Context, hash []byte) error
}

type Service struct {
	Store Store
	TTL   time.Duration
	// Now is injectable for expiry tests; nil uses the real clock.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func digest(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}
func (s *Service) Login(ctx context.Context, accountID, oldToken string) (User, string, time.Time, error) {
	token := rand.Text()
	expires := s.now().Add(s.TTL)
	user, err := s.Store.CreateSession(ctx, accountID, digest(token), digest(oldToken), expires)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	return user, token, expires, nil
}
func (s *Service) Current(ctx context.Context, token string) (User, error) {
	if token == "" || len(token) > 256 {
		return User{}, identity.ErrUnauthenticated
	}
	return s.Store.Resolve(ctx, digest(token), s.now())
}
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.Store.DeleteSession(ctx, digest(token))
}
