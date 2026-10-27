package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"tiliweb/internal/store/sqlite"
)

func TestSnapshotAndMediaZip(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	dbPath := filepath.Join(dir, "source.db")
	db, err := sqlite.Open(ctx, dbPath, 2, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('site.name','backup-test')`); err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dest := filepath.Join(dir, "backup.db")
	if err := Snapshot(ctx, db, dest); err != nil {
		t.Fatal(err)
	}
	restored, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dest))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var value string
	if err := restored.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='site.name'`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "backup-test" {
		t.Fatalf("value = %q", value)
	}

	media := filepath.Join(dir, "media")
	docs := filepath.Join(dir, "documents")
	if err := os.MkdirAll(filepath.Join(media, "2026"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, "2026", "a.txt"), []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(docs, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "b.pdf"), []byte("%PDF-1.4"), 0o640); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "media.zip")
	if err := MediaZip(zipPath, media, docs); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	found := map[string]bool{}
	for _, f := range zr.File {
		found[f.Name] = true
	}
	if !found["media/2026/a.txt"] || !found["documents/b.pdf"] {
		t.Fatalf("zip entries = %v", found)
	}
}
