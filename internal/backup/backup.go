// Package backup creates consistent SQLite snapshots and media archives.
package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Snapshot writes a consistent SQLite database snapshot using VACUUM INTO.
func Snapshot(ctx context.Context, db *sql.DB, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup destination already exists: %s", dest)
	}
	quoted := "'" + strings.ReplaceAll(dest, "'", "''") + "'"
	if _, err := db.ExecContext(ctx, "VACUUM INTO "+quoted); err != nil {
		return err
	}
	return nil
}

// DefaultName returns a timestamped database filename.
func DefaultName(now time.Time) string {
	return "tiliweb-" + now.UTC().Format("20060102-150405") + ".db"
}

// MediaZip archives media and document roots. It skips missing roots and
// stores files under their root directory name.
func MediaZip(dest string, roots ...string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		base := filepath.Base(filepath.Clean(root))
		err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(filepath.Join(base, rel))
			w, err := zw.Create(name)
			if err != nil {
				return err
			}
			src, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(w, src)
			_ = src.Close()
			return copyErr
		})
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
