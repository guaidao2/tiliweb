package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDSN(t *testing.T) {
	dsn := BuildDSN("./data/tiliweb.db", 7000)
	if !strings.HasPrefix(dsn, "file:./data/tiliweb.db?") {
		t.Fatalf("dsn = %q", dsn)
	}
	for _, want := range []string{
		"_pragma=busy_timeout(7000)",
		"_pragma=foreign_keys(1)",
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
	} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("dsn missing %q: %s", want, dsn)
		}
	}
}

func TestPathFromDSN(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"./data/tiliweb.db", "./data/tiliweb.db"},
		{"file:./data/tiliweb.db?_pragma=busy_timeout(5000)", "./data/tiliweb.db"},
		{":memory:", ""},
		{"file::memory:?cache=shared", ""},
	}
	for _, tc := range cases {
		if got := PathFromDSN(tc.in); got != tc.want {
			t.Fatalf("PathFromDSN(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOpenPragmas(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db", "tiliweb.db")
	db, err := Open(ctx, path, 4, 7000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var fk, bt int
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&bt); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d", fk)
	}
	if bt != 7000 {
		t.Fatalf("busy_timeout = %d", bt)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode = %q", mode)
	}
}
