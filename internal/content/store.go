package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func nowUnix() int64 { return time.Now().UTC().Unix() }

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

const channelColumns = `id, COALESCE(parent_id, 0), slug, name, type, COALESCE(description, ''),
	COALESCE(list_template, ''), COALESCE(detail_template, ''), show_in_nav, comments_enabled,
	sort_order, status, COALESCE(seo_title, ''), COALESCE(seo_description, ''), created_at, updated_at`

func scanChannel(scanner interface{ Scan(...any) error }) (*Channel, error) {
	var c Channel
	var nav, comments int
	if err := scanner.Scan(&c.ID, &c.ParentID, &c.Slug, &c.Name, &c.Type, &c.Description,
		&c.ListTemplate, &c.DetailTemplate, &nav, &comments, &c.SortOrder, &c.Status,
		&c.SEOTitle, &c.SEODescription, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, mapNotFound(err)
	}
	c.ShowInNav = nav != 0
	c.CommentsEnabled = comments != 0
	return &c, nil
}

func (s *Store) CreateChannel(ctx context.Context, c *Channel) (*Channel, error) {
	now := nowUnix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO channels(parent_id, slug, name, type, description, list_template, detail_template,
			show_in_nav, comments_enabled, sort_order, status, seo_title, seo_description, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullInt(c.ParentID), c.Slug, c.Name, c.Type, c.Description, c.ListTemplate, c.DetailTemplate,
		boolInt(c.ShowInNav), boolInt(c.CommentsEnabled), c.SortOrder, c.Status, c.SEOTitle, c.SEODescription, now, now)
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	return s.ChannelByID(ctx, id)
}

func (s *Store) UpdateChannel(ctx context.Context, c *Channel) (*Channel, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE channels SET parent_id = ?, slug = ?, name = ?, type = ?, description = ?,
			list_template = ?, detail_template = ?, show_in_nav = ?, comments_enabled = ?,
			sort_order = ?, status = ?, seo_title = ?, seo_description = ?, updated_at = ?
		WHERE id = ?`,
		nullInt(c.ParentID), c.Slug, c.Name, c.Type, c.Description, c.ListTemplate, c.DetailTemplate,
		boolInt(c.ShowInNav), boolInt(c.CommentsEnabled), c.SortOrder, c.Status, c.SEOTitle, c.SEODescription, nowUnix(), c.ID)
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.ChannelByID(ctx, c.ID)
}

func (s *Store) ChannelByID(ctx context.Context, id int64) (*Channel, error) {
	return scanChannel(s.db.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM channels WHERE id = ?`, id))
}

func (s *Store) ChannelBySlug(ctx context.Context, slug string) (*Channel, error) {
	return scanChannel(s.db.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM channels WHERE slug = ? COLLATE NOCASE`, slug))
}

func (s *Store) ListChannels(ctx context.Context, includeHidden bool) ([]Channel, error) {
	q := `SELECT ` + channelColumns + ` FROM channels`
	if !includeHidden {
		q += ` WHERE status = 'active'`
	}
	q += ` ORDER BY sort_order, name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteChannel(ctx context.Context, id int64) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE channel_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrChannelNotEmpty
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

const entryColumns = `e.id, e.channel_id, c.slug, c.name, c.type, COALESCE(e.slug, ''), e.title,
	COALESCE(e.summary, ''), e.body_md, e.body_html, COALESCE(e.cover_media_id, 0), e.author_admin_id,
	COALESCE(a.display_name, ''), e.status, e.pinned, e.featured, e.sort_order, e.view_count, e.extra_json,
	COALESCE(e.published_at, 0), e.created_at, e.updated_at, COALESCE(e.deleted_at, 0)`

func scanEntry(scanner interface{ Scan(...any) error }) (*Entry, error) {
	var e Entry
	var pinned, featured int
	if err := scanner.Scan(&e.ID, &e.ChannelID, &e.ChannelSlug, &e.ChannelName, &e.ChannelType, &e.Slug,
		&e.Title, &e.Summary, &e.BodyMD, &e.BodyHTML, &e.CoverMediaID, &e.AuthorAdminID, &e.AuthorName,
		&e.Status, &pinned, &featured, &e.SortOrder, &e.ViewCount, &e.ExtraJSON,
		&e.PublishedAt, &e.CreatedAt, &e.UpdatedAt, &e.DeletedAt); err != nil {
		return nil, mapNotFound(err)
	}
	e.Pinned = pinned != 0
	e.Featured = featured != 0
	if e.ExtraJSON != "" {
		_ = json.Unmarshal([]byte(e.ExtraJSON), &e.Extra)
	}
	return &e, nil
}

const entryJoin = ` FROM entries e JOIN channels c ON c.id = e.channel_id LEFT JOIN admin_users a ON a.id = e.author_admin_id`

func (s *Store) CreateEntry(ctx context.Context, e *Entry) (*Entry, error) {
	now := nowUnix()
	if e.ExtraJSON == "" {
		e.ExtraJSON = "{}"
	}
	var published any
	if e.Status == "published" {
		if e.PublishedAt == 0 {
			e.PublishedAt = now
		}
		published = e.PublishedAt
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO entries(channel_id, slug, title, summary, body_md, body_html, cover_media_id,
			author_admin_id, status, pinned, featured, sort_order, extra_json, published_at, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ChannelID, nullString(e.Slug), e.Title, e.Summary, e.BodyMD, e.BodyHTML, nullInt(e.CoverMediaID),
		e.AuthorAdminID, e.Status, boolInt(e.Pinned), boolInt(e.Featured), e.SortOrder, e.ExtraJSON,
		published, now, now)
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	created, err := s.EntryByID(ctx, id)
	if err == nil {
		_ = s.syncEntrySearch(ctx, id)
	}
	return created, err
}

func (s *Store) UpdateEntry(ctx context.Context, e *Entry, editorAdminID int64) (*Entry, error) {
	old, err := s.EntryByID(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO content_revisions(target_type, target_id, title, body_md, body_html, editor_admin_id, created_at)
		VALUES('entry', ?, ?, ?, ?, ?, ?)`,
		old.ID, old.Title, old.BodyMD, old.BodyHTML, editorAdminID, nowUnix()); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if e.ExtraJSON == "" {
		e.ExtraJSON = "{}"
	}
	var published any
	if e.Status == "published" {
		if e.PublishedAt == 0 {
			e.PublishedAt = nowUnix()
		}
		published = e.PublishedAt
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE entries SET channel_id = ?, slug = ?, title = ?, summary = ?, body_md = ?, body_html = ?,
			cover_media_id = ?, status = ?, pinned = ?, featured = ?, sort_order = ?, extra_json = ?,
			published_at = ?, updated_at = ? WHERE id = ?`,
		e.ChannelID, nullString(e.Slug), e.Title, e.Summary, e.BodyMD, e.BodyHTML, nullInt(e.CoverMediaID),
		e.Status, boolInt(e.Pinned), boolInt(e.Featured), e.SortOrder, e.ExtraJSON, published, nowUnix(), e.ID)
	if isUnique(err) {
		_ = tx.Rollback()
		return nil, ErrConflict
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		_ = tx.Rollback()
		return nil, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	updated, err := s.EntryByID(ctx, e.ID)
	if err == nil {
		_ = s.syncEntrySearch(ctx, e.ID)
	}
	return updated, err
}

func (s *Store) EntryByID(ctx context.Context, id int64) (*Entry, error) {
	return scanEntry(s.db.QueryRowContext(ctx, `SELECT `+entryColumns+entryJoin+` WHERE e.id = ?`, id))
}

func (s *Store) EntryByChannelSlug(ctx context.Context, channelSlug, key string, publicOnly bool) (*Entry, error) {
	q := `SELECT ` + entryColumns + entryJoin + ` WHERE c.slug = ? COLLATE NOCASE AND (e.slug = ? COLLATE NOCASE OR CAST(e.id AS TEXT) = ?)`
	args := []any{channelSlug, key, key}
	if publicOnly {
		q += ` AND e.status = 'published' AND e.deleted_at IS NULL AND c.status = 'active'`
	}
	return scanEntry(s.db.QueryRowContext(ctx, q, args...))
}

func (s *Store) ListEntries(ctx context.Context, channelID int64, status string, includeDeleted bool, limit, offset int) ([]Entry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + entryColumns + entryJoin + ` WHERE 1=1`
	args := []any{}
	if channelID > 0 {
		q += ` AND e.channel_id = ?`
		args = append(args, channelID)
	}
	if status != "" {
		q += ` AND e.status = ?`
		args = append(args, status)
	}
	if !includeDeleted {
		q += ` AND e.deleted_at IS NULL`
	}
	q += ` ORDER BY e.pinned DESC, COALESCE(e.published_at, e.created_at) DESC, e.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *Store) PublicEntries(ctx context.Context, channelSlug string, limit, offset int) ([]Entry, error) {
	if limit <= 0 || limit > 200 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+entryColumns+entryJoin+`
		WHERE c.slug = ? COLLATE NOCASE AND c.status = 'active'
		  AND e.status = 'published' AND e.deleted_at IS NULL
		ORDER BY e.pinned DESC, e.published_at DESC, e.id DESC LIMIT ? OFFSET ?`,
		channelSlug, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *Store) PublicLatestEntries(ctx context.Context, limit int) ([]Entry, error) {
	return s.publicEntries(ctx, "", nil, limit)
}

func (s *Store) PublicFeaturedEntries(ctx context.Context, limit int) ([]Entry, error) {
	return s.publicEntries(ctx, " AND e.featured = 1", nil, limit)
}

func (s *Store) PublicEntriesByType(ctx context.Context, typ string, limit int) ([]Entry, error) {
	return s.publicEntries(ctx, " AND c.type = ?", []any{typ}, limit)
}

func (s *Store) publicEntries(ctx context.Context, extra string, args []any, limit int) ([]Entry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 10
	}
	q := `SELECT ` + entryColumns + entryJoin + `
		WHERE c.status = 'active' AND e.status = 'published' AND e.deleted_at IS NULL` + extra + `
		ORDER BY e.pinned DESC, e.published_at DESC, e.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *Store) PublicPages(ctx context.Context) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+pageColumns+` FROM pages
		WHERE status = 'published' AND deleted_at IS NULL ORDER BY sort_order, title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Page
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) SetEntryStatus(ctx context.Context, id int64, status string, editorAdminID int64) error {
	old, err := s.EntryByID(ctx, id)
	if err != nil {
		return err
	}
	now := nowUnix()
	published := any(nil)
	if status == "published" {
		if old.PublishedAt > 0 {
			published = old.PublishedAt
		} else {
			published = now
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE entries SET status = ?, published_at = ?, updated_at = ? WHERE id = ?`,
		status, published, now, id)
	if err != nil {
		return err
	}
	_ = s.insertRevision(ctx, "entry", id, old.Title, old.BodyMD, old.BodyHTML, editorAdminID)
	_ = s.syncEntrySearch(ctx, id)
	return nil
}

func (s *Store) SoftDeleteEntry(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE entries SET deleted_at = ?, updated_at = ? WHERE id = ?`, nowUnix(), nowUnix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	_ = s.syncEntrySearch(ctx, id)
	return nil
}

func (s *Store) RestoreEntry(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE entries SET deleted_at = NULL, updated_at = ? WHERE id = ?`, nowUnix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	_ = s.syncEntrySearch(ctx, id)
	return nil
}

func (s *Store) IncrementEntryView(ctx context.Context, id int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE entries SET view_count = view_count + 1 WHERE id = ?`, id)
}

func (s *Store) SetEntryTags(ctx context.Context, entryID int64, tags []Tag) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM entry_tags WHERE entry_id = ?`, entryID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, t := range tags {
		if t.Name == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(slug, name) VALUES(?, ?)`, t.Slug, t.Name); err != nil {
			_ = tx.Rollback()
			return err
		}
		var tagID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE slug = ?`, t.Slug).Scan(&tagID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO entry_tags(entry_id, tag_id) VALUES(?, ?)`, entryID, tagID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) EntryTags(ctx context.Context, entryID int64) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.slug, t.name FROM tags t JOIN entry_tags et ON et.tag_id = t.id
		WHERE et.entry_id = ? ORDER BY t.name`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) TagBySlug(ctx context.Context, slug string) (*Tag, error) {
	var t Tag
	if err := s.db.QueryRowContext(ctx, `SELECT id, slug, name FROM tags WHERE slug = ? COLLATE NOCASE`, slug).
		Scan(&t.ID, &t.Slug, &t.Name); err != nil {
		return nil, mapNotFound(err)
	}
	return &t, nil
}

func (s *Store) PublicEntriesByTag(ctx context.Context, tagSlug string, limit, offset int) ([]Entry, error) {
	if limit <= 0 || limit > 200 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+entryColumns+entryJoin+`
		JOIN entry_tags et ON et.entry_id = e.id JOIN tags t ON t.id = et.tag_id
		WHERE t.slug = ? COLLATE NOCASE AND c.status = 'active'
		  AND e.status = 'published' AND e.deleted_at IS NULL
		ORDER BY e.published_at DESC, e.id DESC LIMIT ? OFFSET ?`, tagSlug, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

const pageColumns = `id, slug, title, body_md, body_html, template, show_in_nav, sort_order, status,
	COALESCE(seo_title, ''), COALESCE(seo_description, ''), created_at, updated_at, COALESCE(deleted_at, 0)`

func scanPage(scanner interface{ Scan(...any) error }) (*Page, error) {
	var p Page
	var nav int
	if err := scanner.Scan(&p.ID, &p.Slug, &p.Title, &p.BodyMD, &p.BodyHTML, &p.Template, &nav,
		&p.SortOrder, &p.Status, &p.SEOTitle, &p.SEODescription, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt); err != nil {
		return nil, mapNotFound(err)
	}
	p.ShowInNav = nav != 0
	return &p, nil
}

func (s *Store) CreatePage(ctx context.Context, p *Page) (*Page, error) {
	now := nowUnix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO pages(slug, title, body_md, body_html, template, show_in_nav, sort_order, status,
			seo_title, seo_description, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Slug, p.Title, p.BodyMD, p.BodyHTML, p.Template, boolInt(p.ShowInNav), p.SortOrder, p.Status,
		p.SEOTitle, p.SEODescription, now, now)
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	created, err := s.PageByID(ctx, id)
	if err == nil {
		_ = s.syncPageSearch(ctx, id)
	}
	return created, err
}

func (s *Store) UpdatePage(ctx context.Context, p *Page, editorAdminID int64) (*Page, error) {
	old, err := s.PageByID(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO content_revisions(target_type, target_id, title, body_md, body_html, editor_admin_id, created_at)
		VALUES('page', ?, ?, ?, ?, ?, ?)`,
		old.ID, old.Title, old.BodyMD, old.BodyHTML, editorAdminID, nowUnix()); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE pages SET slug = ?, title = ?, body_md = ?, body_html = ?, template = ?, show_in_nav = ?,
			sort_order = ?, status = ?, seo_title = ?, seo_description = ?, updated_at = ?
		WHERE id = ?`,
		p.Slug, p.Title, p.BodyMD, p.BodyHTML, p.Template, boolInt(p.ShowInNav), p.SortOrder, p.Status,
		p.SEOTitle, p.SEODescription, nowUnix(), p.ID)
	if isUnique(err) {
		_ = tx.Rollback()
		return nil, ErrConflict
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		_ = tx.Rollback()
		return nil, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	updated, err := s.PageByID(ctx, p.ID)
	if err == nil {
		_ = s.syncPageSearch(ctx, p.ID)
	}
	return updated, err
}

func (s *Store) PageByID(ctx context.Context, id int64) (*Page, error) {
	return scanPage(s.db.QueryRowContext(ctx, `SELECT `+pageColumns+` FROM pages WHERE id = ?`, id))
}

func (s *Store) PageBySlug(ctx context.Context, slug string, publicOnly bool) (*Page, error) {
	q := `SELECT ` + pageColumns + ` FROM pages WHERE slug = ? COLLATE NOCASE`
	if publicOnly {
		q += ` AND status = 'published' AND deleted_at IS NULL`
	}
	return scanPage(s.db.QueryRowContext(ctx, q, slug))
}

func (s *Store) ListPages(ctx context.Context, includeDeleted bool) ([]Page, error) {
	q := `SELECT ` + pageColumns + ` FROM pages`
	if !includeDeleted {
		q += ` WHERE deleted_at IS NULL`
	}
	q += ` ORDER BY sort_order, title`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Page
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) SoftDeletePage(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE pages SET deleted_at = ?, updated_at = ? WHERE id = ?`, nowUnix(), nowUnix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	_ = s.syncPageSearch(ctx, id)
	return nil
}

func (s *Store) insertRevision(ctx context.Context, targetType string, targetID int64, title, bodyMD, bodyHTML string, editorID int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO content_revisions(target_type, target_id, title, body_md, body_html, editor_admin_id, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, targetType, targetID, title, bodyMD, bodyHTML, editorID, nowUnix())
	return err
}

func (s *Store) Revisions(ctx context.Context, targetType string, targetID int64) ([]Revision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, target_type, target_id, COALESCE(title, ''), COALESCE(body_md, ''), COALESCE(body_html, ''),
		       editor_admin_id, created_at
		FROM content_revisions WHERE target_type = ? AND target_id = ? ORDER BY id DESC`, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Revision
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.TargetType, &r.TargetID, &r.Title, &r.BodyMD, &r.BodyHTML, &r.EditorAdminID, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RevisionByID(ctx context.Context, id int64) (*Revision, error) {
	var r Revision
	err := s.db.QueryRowContext(ctx, `
		SELECT id, target_type, target_id, COALESCE(title, ''), COALESCE(body_md, ''), COALESCE(body_html, ''),
		       editor_admin_id, created_at
		FROM content_revisions WHERE id = ?`, id).
		Scan(&r.ID, &r.TargetType, &r.TargetID, &r.Title, &r.BodyMD, &r.BodyHTML, &r.EditorAdminID, &r.CreatedAt)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return &r, nil
}

func (s *Store) CreateMedia(ctx context.Context, m *Media, variants []MediaVariant) (*Media, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO media(kind, stored_name, original_name, mime, size, width, height, sha256,
			uploader_type, uploader_id, status, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?)`,
		m.Kind, m.StoredName, m.OriginalName, m.MIME, m.Size, m.Width, m.Height, m.SHA256,
		m.UploaderType, nullInt(m.UploaderID), nowUnix())
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	for _, v := range variants {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO media_variants(media_id, label, stored_name, width, height, size)
			VALUES(?, ?, ?, ?, ?, ?)`, id, v.Label, v.StoredName, v.Width, v.Height, v.Size); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.MediaByID(ctx, id)
}

func (s *Store) MediaByID(ctx context.Context, id int64) (*Media, error) {
	var m Media
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, stored_name, original_name, mime, size, COALESCE(width, 0), COALESCE(height, 0),
		       sha256, uploader_type, COALESCE(uploader_id, 0), status, created_at
		FROM media WHERE id = ?`, id).
		Scan(&m.ID, &m.Kind, &m.StoredName, &m.OriginalName, &m.MIME, &m.Size, &m.Width, &m.Height,
			&m.SHA256, &m.UploaderType, &m.UploaderID, &m.Status, &m.CreatedAt)
	if err != nil {
		return nil, mapNotFound(err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, media_id, label, stored_name, COALESCE(width,0), COALESCE(height,0), COALESCE(size,0) FROM media_variants WHERE media_id = ? ORDER BY label`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v MediaVariant
		if err := rows.Scan(&v.ID, &v.MediaID, &v.Label, &v.StoredName, &v.Width, &v.Height, &v.Size); err != nil {
			return nil, err
		}
		m.Variants = append(m.Variants, v)
	}
	return &m, rows.Err()
}

func (s *Store) ListMedia(ctx context.Context, limit int) ([]Media, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, stored_name, original_name, mime, size, COALESCE(width,0), COALESCE(height,0),
		       sha256, uploader_type, COALESCE(uploader_id,0), status, created_at
		FROM media WHERE status = 'active' ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.Kind, &m.StoredName, &m.OriginalName, &m.MIME, &m.Size, &m.Width, &m.Height,
			&m.SHA256, &m.UploaderType, &m.UploaderID, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMedia(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE media SET status = 'deleted', deleted_at = ? WHERE id = ?`, nowUnix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Counts(ctx context.Context) (channels, entries, pages, media int, err error) {
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channels`).Scan(&channels); err != nil {
		return
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE deleted_at IS NULL`).Scan(&entries); err != nil {
		return
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pages WHERE deleted_at IS NULL`).Scan(&pages); err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media WHERE status = 'active'`).Scan(&media)
	return
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

const bannerColumns = `id, COALESCE(title,''), COALESCE(subtitle,''), COALESCE(image_media_id,0),
	COALESCE(link_url,''), COALESCE(button_text,''), sort_order, enabled, created_at, updated_at`

func scanBanner(scanner interface{ Scan(...any) error }) (*Banner, error) {
	var b Banner
	var enabled int
	if err := scanner.Scan(&b.ID, &b.Title, &b.Subtitle, &b.ImageMediaID, &b.LinkURL, &b.ButtonText,
		&b.SortOrder, &enabled, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, mapNotFound(err)
	}
	b.Enabled = enabled != 0
	return &b, nil
}

func (s *Store) CreateBanner(ctx context.Context, b *Banner) (*Banner, error) {
	now := nowUnix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO banners(title, subtitle, image_media_id, link_url, button_text, sort_order, enabled, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullString(b.Title), nullString(b.Subtitle), nullInt(b.ImageMediaID), nullString(b.LinkURL),
		nullString(b.ButtonText), b.SortOrder, boolInt(b.Enabled), now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.BannerByID(ctx, id)
}

func (s *Store) UpdateBanner(ctx context.Context, b *Banner) (*Banner, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE banners SET title = ?, subtitle = ?, image_media_id = ?, link_url = ?, button_text = ?,
			sort_order = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		nullString(b.Title), nullString(b.Subtitle), nullInt(b.ImageMediaID), nullString(b.LinkURL),
		nullString(b.ButtonText), b.SortOrder, boolInt(b.Enabled), nowUnix(), b.ID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.BannerByID(ctx, b.ID)
}

func (s *Store) BannerByID(ctx context.Context, id int64) (*Banner, error) {
	return scanBanner(s.db.QueryRowContext(ctx, `SELECT `+bannerColumns+` FROM banners WHERE id = ?`, id))
}

func (s *Store) ListBanners(ctx context.Context, enabledOnly bool) ([]Banner, error) {
	q := `SELECT ` + bannerColumns + ` FROM banners`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY sort_order, id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Banner
	for rows.Next() {
		b, err := scanBanner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteBanner(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM banners WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MustEntryByID(ctx context.Context, id int64) (*Entry, error) {
	e, err := s.EntryByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("entry %d: %w", id, err)
	}
	return e, nil
}
