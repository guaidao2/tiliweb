// Package migrations embeds the forward-only SQL migrations.
package migrations

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

//go:embed *.sql
var files embed.FS

// Migration is one forward migration.
type Migration struct {
	Version string
	SQL     string
}

// All returns embedded migrations sorted by version.
func All() ([]Migration, error) {
	paths, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	out := make([]Migration, 0, len(paths))
	for _, p := range paths {
		b, err := files.ReadFile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{
			Version: strings.TrimSuffix(p, ".sql"),
			SQL:     string(b),
		})
	}
	return out, nil
}
