package content

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// NormalizeSearch inserts spaces around CJK characters so the unicode61
// tokenizer indexes Chinese/Japanese/Korean text character by character.
func NormalizeSearch(s string) string {
	var b strings.Builder
	space := true
	for _, r := range s {
		if isCJK(r) {
			if !space {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			b.WriteByte(' ')
			space = true
			continue
		}
		if unicode.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		b.WriteRune(unicode.ToLower(r))
		space = false
	}
	return strings.TrimSpace(b.String())
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

func ftsPhrase(q string) string {
	q = NormalizeSearch(q)
	q = strings.ReplaceAll(q, `"`, `""`)
	return `"` + q + `"`
}

func (s *Store) syncEntrySearch(ctx context.Context, id int64) error {
	e, err := s.EntryByID(ctx, id)
	if err != nil || e.Status != "published" || e.DeletedAt > 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM entries_fts WHERE entry_id = ?`, id)
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM entries_fts WHERE entry_id = ?`, id); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO entries_fts(entry_id, title, summary, body) VALUES(?, ?, ?, ?)`,
		id, NormalizeSearch(e.Title), NormalizeSearch(e.Summary), NormalizeSearch(e.BodyMD))
	return err
}

func (s *Store) syncPageSearch(ctx context.Context, id int64) error {
	p, err := s.PageByID(ctx, id)
	if err != nil || p.Status != "published" || p.DeletedAt > 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM pages_fts WHERE page_id = ?`, id)
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM pages_fts WHERE page_id = ?`, id); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO pages_fts(page_id, title, body) VALUES(?, ?, ?)`,
		id, NormalizeSearch(p.Title), NormalizeSearch(p.BodyMD))
	return err
}

// RebuildSearch recreates the FTS rows for all public entries and pages.
func (s *Store) RebuildSearch(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM entries_fts`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM pages_fts`); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM entries WHERE status = 'published' AND deleted_at IS NULL`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.syncEntrySearch(ctx, id); err != nil {
			return err
		}
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id FROM pages WHERE status = 'published' AND deleted_at IS NULL`)
	if err != nil {
		return err
	}
	ids = ids[:0]
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.syncPageSearch(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// Search queries the FTS index and falls back to LIKE if FTS is unavailable.
func (s *Store) Search(ctx context.Context, q, typ string, limit, offset int) ([]SearchResult, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	out, err := s.searchFTS(ctx, q, typ, limit, offset)
	if err != nil {
		return s.searchLike(ctx, q, typ, limit, offset)
	}
	return out, nil
}

func (s *Store) searchFTS(ctx context.Context, q, typ string, limit, offset int) ([]SearchResult, error) {
	phrase := ftsPhrase(q)
	var out []SearchResult
	if typ == "" || typ == "all" || typ != "page" {
		query := `SELECT e.id, e.title, COALESCE(e.summary,''), COALESCE(e.slug,''), c.slug, c.name, COALESCE(e.published_at,0)
			FROM entries_fts f JOIN entries e ON e.id = CAST(f.entry_id AS INTEGER)
			JOIN channels c ON c.id = e.channel_id
			WHERE entries_fts MATCH ? AND e.status='published' AND e.deleted_at IS NULL AND c.status='active'`
		args := []any{phrase}
		if typ != "" && typ != "all" {
			query += ` AND c.type = ?`
			args = append(args, typ)
		}
		query += ` ORDER BY rank LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r SearchResult
			var slug string
			if err := rows.Scan(&r.ID, &r.Title, &r.Summary, &slug, &r.ChannelSlug, &r.ChannelName, &r.PublishedAt); err != nil {
				rows.Close()
				return nil, err
			}
			if slug == "" {
				slug = fmt.Sprintf("%d", r.ID)
			}
			r.Type = "entry"
			r.URL = "/" + r.ChannelSlug + "/" + slug
			out = append(out, r)
		}
		rows.Close()
	}
	if typ == "" || typ == "all" || typ == "page" {
		rows, err := s.db.QueryContext(ctx, `SELECT p.id, p.title, COALESCE(p.body_md,''), COALESCE(p.slug,''), COALESCE(p.updated_at,0)
			FROM pages_fts f JOIN pages p ON p.id = CAST(f.page_id AS INTEGER)
			WHERE pages_fts MATCH ? AND p.status='published' AND p.deleted_at IS NULL
			ORDER BY rank LIMIT ? OFFSET ?`, phrase, limit, offset)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r SearchResult
			var body, slug string
			if err := rows.Scan(&r.ID, &r.Title, &body, &slug, &r.PublishedAt); err != nil {
				rows.Close()
				return nil, err
			}
			r.Type = "page"
			r.Summary = trimSummary(body, 160)
			r.URL = "/" + slug
			out = append(out, r)
		}
		rows.Close()
	}
	return out, nil
}

func (s *Store) searchLike(ctx context.Context, q, typ string, limit, offset int) ([]SearchResult, error) {
	like := "%" + escapeLike(q) + "%"
	var out []SearchResult
	if typ == "" || typ == "all" || typ != "page" {
		query := `SELECT e.id, e.title, COALESCE(e.summary,''), COALESCE(e.slug,''), c.slug, c.name, COALESCE(e.published_at,0)
			FROM entries e JOIN channels c ON c.id=e.channel_id
			WHERE (e.title LIKE ? ESCAPE '\' OR e.summary LIKE ? ESCAPE '\' OR e.body_md LIKE ? ESCAPE '\')
			  AND e.status='published' AND e.deleted_at IS NULL AND c.status='active'`
		args := []any{like, like, like}
		if typ != "" && typ != "all" {
			query += ` AND c.type = ?`
			args = append(args, typ)
		}
		query += ` ORDER BY COALESCE(e.published_at,e.created_at) DESC LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r SearchResult
			var slug string
			if err := rows.Scan(&r.ID, &r.Title, &r.Summary, &slug, &r.ChannelSlug, &r.ChannelName, &r.PublishedAt); err != nil {
				rows.Close()
				return nil, err
			}
			if slug == "" {
				slug = fmt.Sprintf("%d", r.ID)
			}
			r.Type = "entry"
			r.URL = "/" + r.ChannelSlug + "/" + slug
			out = append(out, r)
		}
		rows.Close()
	}
	if typ == "" || typ == "all" || typ == "page" {
		rows, err := s.db.QueryContext(ctx, `SELECT id, title, slug, COALESCE(body_md,''), COALESCE(updated_at,0)
			FROM pages WHERE (title LIKE ? ESCAPE '\' OR body_md LIKE ? ESCAPE '\') AND status='published' AND deleted_at IS NULL
			ORDER BY updated_at DESC LIMIT ? OFFSET ?`, like, like, limit, offset)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r SearchResult
			var slug, body string
			if err := rows.Scan(&r.ID, &r.Title, &slug, &body, &r.PublishedAt); err != nil {
				rows.Close()
				return nil, err
			}
			r.Type = "page"
			r.Summary = trimSummary(body, 160)
			r.URL = "/" + slug
			out = append(out, r)
		}
		rows.Close()
	}
	return out, nil
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func trimSummary(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
