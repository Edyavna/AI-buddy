package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

type SQLStore struct{ DB *sql.DB }

const userColumns = `u.id, u.name, u.role, t.id, t.name`

type scanner interface{ Scan(...any) error }

func scanUser(row scanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Role, &u.Team.ID, &u.Team.Name)
	return u, err
}
func unavailable(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ports.ErrUnavailable, err)
}
func (s SQLStore) Accounts(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+userColumns+` FROM users u JOIN teams t ON t.id=u.team_id WHERE u.demo_account ORDER BY u.id`)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, unavailable(err)
		}
		users = append(users, u)
	}
	return users, unavailable(rows.Err())
}
func (s SQLStore) CreateSession(ctx context.Context, id string, hash, oldHash []byte, expires time.Time) (User, error) {
	var user User
	err := database.WithinTx(ctx, s.DB, func(tx *sql.Tx) error {
		var err error
		user, err = scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users u JOIN teams t ON t.id=u.team_id WHERE u.id=$1 AND u.demo_account`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ports.ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1`, oldHash); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, hash, id, expires)
		return err
	})
	if errors.Is(err, ports.ErrNotFound) {
		return User{}, err
	}
	return user, unavailable(err)
}
func (s SQLStore) Resolve(ctx context.Context, hash []byte, now time.Time) (User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userColumns+` FROM sessions s JOIN users u ON u.id=s.user_id JOIN teams t ON t.id=u.team_id WHERE s.token_hash=$1 AND s.expires_at>$2`, hash, now))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, identity.ErrUnauthenticated
	}
	return u, unavailable(err)
}
func (s SQLStore) DeleteSession(ctx context.Context, hash []byte) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1`, hash)
	return unavailable(err)
}
