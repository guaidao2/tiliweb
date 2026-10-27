package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateAndVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tiliweb.db")
	db, err := Open(ctx, path, 4, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	pending, err := PendingMigrations(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 7 || pending[0] != "0001_init" || pending[1] != "0002_auth" || pending[2] != "0003_content" || pending[3] != "0004_site" || pending[4] != "0005_m4" || pending[5] != "0006_forum" || pending[6] != "0007_mcp" {
		t.Fatalf("pending = %v", pending)
	}

	applied, err := Migrate(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 7 || applied[0] != "0001_init" || applied[1] != "0002_auth" || applied[2] != "0003_content" || applied[3] != "0004_site" || applied[4] != "0005_m4" || applied[5] != "0006_forum" || applied[6] != "0007_mcp" {
		t.Fatalf("applied = %v", applied)
	}

	version, err := SchemaVersion(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if version != "0007_mcp" {
		t.Fatalf("version = %q", version)
	}

	pending, err = PendingMigrations(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after migrate = %v", pending)
	}

	applied, err = Migrate(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("second migrate applied = %v", applied)
	}

	if _, err := db.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES('site.name', 'tiliweb')"); err != nil {
		t.Fatalf("settings table missing or broken: %v", err)
	}
	var value string
	if err := db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'site.name'").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "tiliweb" {
		t.Fatalf("value = %q", value)
	}

	for _, table := range []string{"admin_users", "admin_sessions", "users", "user_sessions", "audit_logs", "login_attempts", "channels", "entries", "pages", "media", "contact_messages", "entries_fts", "pages_fts", "entry_attachments", "comments", "friend_links", "redirects", "boards", "topics", "forum_posts", "forum_moderators", "topics_fts", "forum_posts_fts", "mcp_tokens", "mcp_tool_calls"} {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

func TestSchemaVersionOnNewDB(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "new.db"), 2, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	version, err := SchemaVersion(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if version != "" {
		t.Fatalf("version = %q", version)
	}
}

func TestMigrationsEmbedded(t *testing.T) {
	all, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 4 {
		t.Fatalf("migrations = %d, want at least 4", len(all))
	}
	if all[0].Version != "0001_init" {
		t.Fatalf("first migration = %q", all[0].Version)
	}
	if all[len(all)-1].Version != "0007_mcp" {
		t.Fatalf("last migration = %q", all[len(all)-1].Version)
	}
}
