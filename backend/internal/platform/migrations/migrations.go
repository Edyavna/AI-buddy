// Package migrations owns the embedded, forward-only SQL migration runner.
package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
)

//go:embed sql/*.sql
var Files embed.FS

type migration struct{ name, checksum, sql string }

var validName = regexp.MustCompile(`^[0-9]{6}_[a-z0-9_]+\.sql$`)

func read(source fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(source, "sql")
	if err != nil {
		return nil, err
	}
	var result []migration
	seen := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !validName.MatchString(name) {
			return nil, fmt.Errorf("invalid migration filename: %s", name)
		}
		if seen[name[:6]] {
			return nil, fmt.Errorf("duplicate migration version: %s", name[:6])
		}
		seen[name[:6]] = true
		data, err := fs.ReadFile(source, "sql/"+name)
		if err != nil {
			return nil, err
		}
		result = append(result, migration{name: name, checksum: fmt.Sprintf("%x", sha256.Sum256(data)), sql: string(data)})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no migrations found")
	}
	return result, nil
}

// Apply serializes concurrent runners, and atomically commits SQL and journal.
// DDL requiring execution outside a transaction is intentionally unsupported.
func Apply(ctx context.Context, db *sql.DB, source fs.FS) error {
	items, err := read(source)
	if err != nil {
		return err
	}
	return database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(737584201)"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS buddy_schema_migrations (
			name text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return err
		}
		applied, err := journal(ctx, tx)
		if err != nil {
			return err
		}
		if err := compare(items, applied, false); err != nil {
			return err
		}
		for _, item := range items {
			if _, exists := applied[item.name]; exists {
				continue
			}
			if _, err := tx.ExecContext(ctx, item.sql); err != nil {
				return fmt.Errorf("apply %s: %w", item.name, err)
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO buddy_schema_migrations(name, checksum) VALUES ($1,$2)", item.name, item.checksum); err != nil {
				return err
			}
		}
		return nil
	})
}

// Verify never mutates the DB. Run migrations explicitly before starting server.
func Verify(ctx context.Context, db *sql.DB, source fs.FS) error {
	items, err := read(source)
	if err != nil {
		return err
	}
	return database.WithinTx(ctx, db, func(tx *sql.Tx) error {
		applied, err := journal(ctx, tx)
		if err != nil {
			return fmt.Errorf("read migration journal; run migrate first: %w", err)
		}
		return compare(items, applied, true)
	})
}

func journal(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name, checksum FROM buddy_schema_migrations ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return nil, err
		}
		result[name] = checksum
	}
	return result, rows.Err()
}

func compare(items []migration, applied map[string]string, complete bool) error {
	known := make(map[string]string)
	latest := ""
	for name := range applied {
		if name > latest {
			latest = name
		}
	}
	for _, item := range items {
		known[item.name] = item.checksum
		if checksum, ok := applied[item.name]; ok {
			if checksum != item.checksum {
				return fmt.Errorf("applied migration changed: %s", item.name)
			}
		} else if complete || strings.Compare(item.name, latest) < 0 {
			return fmt.Errorf("missing or out-of-order migration: %s", item.name)
		}
	}
	for name := range applied {
		if _, ok := known[name]; !ok {
			return fmt.Errorf("unknown applied migration: %s", name)
		}
	}
	return nil
}
