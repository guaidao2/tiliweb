package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// BuildDSN turns a configured library path into a file: URI with per-connection
// pragmas. database.busy_timeout is the single source of the busy timeout.
func BuildDSN(dsn string, busyTimeoutMS int) string {
	base := strings.TrimSpace(dsn)
	if base == "" {
		base = "./data/tiliweb.db"
	}
	if !strings.HasPrefix(base, "file:") {
		base = "file:" + base
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	if busyTimeoutMS <= 0 {
		busyTimeoutMS = 5000
	}
	return base + sep +
		"_pragma=busy_timeout(" + strconv.Itoa(busyTimeoutMS) + ")" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)"
}

// PathFromDSN returns the filesystem path part of a config DSN. It returns ""
// for in-memory databases.
func PathFromDSN(dsn string) string {
	s := strings.TrimSpace(dsn)
	s = strings.TrimPrefix(s, "file:")
	if strings.HasPrefix(s, ":memory:") || s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	return s
}

// Open opens the SQLite database, creates its parent directory, and verifies
// the per-connection pragmas.
func Open(ctx context.Context, dsn string, maxOpenConns, busyTimeoutMS int) (*sql.DB, error) {
	if maxOpenConns <= 0 {
		maxOpenConns = 8
	}
	if p := PathFromDSN(dsn); p != "" {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return nil, fmt.Errorf("create database dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", BuildDSN(dsn, busyTimeoutMS))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxOpenConns)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	var foreignKeys, busyTimeout int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read foreign_keys pragma: %w", err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read busy_timeout pragma: %w", err)
	}
	if foreignKeys != 1 {
		_ = db.Close()
		return nil, fmt.Errorf("foreign_keys pragma is %d, want 1 (check database.dsn)", foreignKeys)
	}
	if busyTimeout <= 0 {
		_ = db.Close()
		return nil, fmt.Errorf("busy_timeout pragma is %d, want > 0", busyTimeout)
	}
	return db, nil
}
