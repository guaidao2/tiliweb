package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"tiliweb/internal/store/migrations"
)

// Migration aliases the embedded migration type for callers of this package.
type Migration = migrations.Migration

// Migrations returns embedded migrations sorted by version.
func Migrations() ([]Migration, error) {
	return migrations.All()
}

// Migrate applies all pending migrations in order and returns the applied
// versions. It is idempotent.
func Migrate(ctx context.Context, db *sql.DB) ([]string, error) {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return nil, err
	}
	all, err := migrations.All()
	if err != nil {
		return nil, err
	}

	var appliedNow []string
	for _, m := range all {
		if applied[m.Version] {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return appliedNow, fmt.Errorf("begin migration %s: %w", m.Version, err)
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			_ = tx.Rollback()
			return appliedNow, fmt.Errorf("apply migration %s: %w", m.Version, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)",
			m.Version, time.Now().UTC().Unix()); err != nil {
			_ = tx.Rollback()
			return appliedNow, fmt.Errorf("record migration %s: %w", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return appliedNow, fmt.Errorf("commit migration %s: %w", m.Version, err)
		}
		appliedNow = append(appliedNow, m.Version)
	}
	return appliedNow, nil
}

// SchemaVersion returns the highest applied migration version, or "" if none.
func SchemaVersion(ctx context.Context, db *sql.DB) (string, error) {
	var v string
	err := db.QueryRowContext(ctx,
		"SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// PendingMigrations returns versions that are not applied yet.
func PendingMigrations(ctx context.Context, db *sql.DB) ([]string, error) {
	applied, err := appliedVersions(ctx, db)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			applied = map[string]bool{}
		} else {
			return nil, err
		}
	}
	all, err := migrations.All()
	if err != nil {
		return nil, err
	}
	var pending []string
	for _, m := range all {
		if !applied[m.Version] {
			pending = append(pending, m.Version)
		}
	}
	return pending, nil
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}
