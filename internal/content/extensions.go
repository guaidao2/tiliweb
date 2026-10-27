package content

import (
	"context"
	"net/url"
	"strings"

	"tiliweb/internal/markdown"
)

// Attachments -----------------------------------------------------------------

const attachmentColumns = `a.id, a.entry_id, a.media_id, a.title, a.sort_order, a.download_count,
	m.stored_name, m.original_name, m.mime, m.size`

func scanAttachment(scanner interface{ Scan(...any) error }) (*Attachment, error) {
	var a Attachment
	if err := scanner.Scan(&a.ID, &a.EntryID, &a.MediaID, &a.Title, &a.SortOrder, &a.DownloadCount,
		&a.StoredName, &a.OriginalName, &a.MIME, &a.Size); err != nil {
		return nil, mapNotFound(err)
	}
	return &a, nil
}

func (s *Store) CreateAttachment(ctx context.Context, a *Attachment) (*Attachment, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO entry_attachments(entry_id, media_id, title, sort_order)
		VALUES(?, ?, ?, ?)`, a.EntryID, a.MediaID, a.Title, a.SortOrder)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.AttachmentByID(ctx, id)
}

func (s *Store) AttachmentByID(ctx context.Context, id int64) (*Attachment, error) {
	return scanAttachment(s.db.QueryRowContext(ctx, `SELECT `+attachmentColumns+`
		FROM entry_attachments a JOIN media m ON m.id = a.media_id WHERE a.id = ?`, id))
}

func (s *Store) ListAttachments(ctx context.Context, entryID int64) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+attachmentColumns+`
		FROM entry_attachments a JOIN media m ON m.id = a.media_id
		WHERE a.entry_id = ? AND m.status = 'active' ORDER BY a.sort_order, a.id`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAttachment(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM entry_attachments WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) IncrementAttachmentDownload(ctx context.Context, id int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE entry_attachments SET download_count = download_count + 1 WHERE id = ?`, id)
}

// Comments --------------------------------------------------------------------

const commentColumns = `id, entry_id, COALESCE(user_id,0), COALESCE(author_name,''), COALESCE(author_email,''),
	body_md, body_html, status, COALESCE(ip,''), created_at, updated_at`

func scanComment(scanner interface{ Scan(...any) error }) (*Comment, error) {
	var c Comment
	if err := scanner.Scan(&c.ID, &c.EntryID, &c.UserID, &c.AuthorName, &c.AuthorEmail,
		&c.BodyMD, &c.BodyHTML, &c.Status, &c.IP, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, mapNotFound(err)
	}
	return &c, nil
}

func (s *Store) CreateComment(ctx context.Context, c *Comment) (*Comment, error) {
	now := nowUnix()
	if c.Status == "" {
		c.Status = "pending"
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO comments(entry_id, user_id, author_name, author_email,
		body_md, body_html, status, ip, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.EntryID, nullInt(c.UserID), nullString(c.AuthorName), nullString(c.AuthorEmail),
		c.BodyMD, c.BodyHTML, c.Status, nullString(c.IP), now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.CommentByID(ctx, id)
}

func (s *Store) CommentByID(ctx context.Context, id int64) (*Comment, error) {
	return scanComment(s.db.QueryRowContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE id = ?`, id))
}

func (s *Store) ListApprovedComments(ctx context.Context, entryID int64) ([]Comment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+commentColumns+` FROM comments
		WHERE entry_id = ? AND status = 'approved' ORDER BY created_at, id`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) ListComments(ctx context.Context, status string, limit int) ([]Comment, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := `SELECT ` + commentColumns + ` FROM comments`
	args := []any{}
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) SetCommentStatus(ctx context.Context, id int64, status string) error {
	switch status {
	case "pending", "approved", "spam", "deleted":
	default:
		return ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `UPDATE comments SET status = ?, updated_at = ? WHERE id = ?`, status, nowUnix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CountPendingComments(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE status = 'pending'`).Scan(&n)
	return n, err
}

// Friend links ----------------------------------------------------------------

const friendLinkColumns = `id, name, url, COALESCE(logo_media_id,0), sort_order, enabled, created_at`

func scanFriendLink(scanner interface{ Scan(...any) error }) (*FriendLink, error) {
	var l FriendLink
	var enabled int
	if err := scanner.Scan(&l.ID, &l.Name, &l.URL, &l.LogoMediaID, &l.SortOrder, &enabled, &l.CreatedAt); err != nil {
		return nil, mapNotFound(err)
	}
	l.Enabled = enabled != 0
	return &l, nil
}

func (s *Store) CreateFriendLink(ctx context.Context, l *FriendLink) (*FriendLink, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO friend_links(name, url, logo_media_id, sort_order, enabled, created_at)
		VALUES(?, ?, ?, ?, ?, ?)`, l.Name, l.URL, nullInt(l.LogoMediaID), l.SortOrder, boolInt(l.Enabled), nowUnix())
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.FriendLinkByID(ctx, id)
}

func (s *Store) UpdateFriendLink(ctx context.Context, l *FriendLink) (*FriendLink, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE friend_links SET name=?, url=?, logo_media_id=?, sort_order=?, enabled=? WHERE id=?`,
		l.Name, l.URL, nullInt(l.LogoMediaID), l.SortOrder, boolInt(l.Enabled), l.ID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.FriendLinkByID(ctx, l.ID)
}

func (s *Store) FriendLinkByID(ctx context.Context, id int64) (*FriendLink, error) {
	return scanFriendLink(s.db.QueryRowContext(ctx, `SELECT `+friendLinkColumns+` FROM friend_links WHERE id = ?`, id))
}

func (s *Store) ListFriendLinks(ctx context.Context, enabledOnly bool) ([]FriendLink, error) {
	q := `SELECT ` + friendLinkColumns + ` FROM friend_links`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY sort_order, id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FriendLink
	for rows.Next() {
		l, err := scanFriendLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (s *Store) DeleteFriendLink(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM friend_links WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Redirects -------------------------------------------------------------------

const redirectColumns = `id, from_path, to_path, status_code, created_at`

func scanRedirect(scanner interface{ Scan(...any) error }) (*Redirect, error) {
	var r Redirect
	if err := scanner.Scan(&r.ID, &r.FromPath, &r.ToPath, &r.StatusCode, &r.CreatedAt); err != nil {
		return nil, mapNotFound(err)
	}
	return &r, nil
}

func (s *Store) CreateRedirect(ctx context.Context, r *Redirect) (*Redirect, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO redirects(from_path, to_path, status_code, created_at) VALUES(?, ?, ?, ?)`,
		r.FromPath, r.ToPath, r.StatusCode, nowUnix())
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
	return s.RedirectByID(ctx, id)
}

func (s *Store) UpdateRedirect(ctx context.Context, r *Redirect) (*Redirect, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE redirects SET from_path=?, to_path=?, status_code=? WHERE id=?`,
		r.FromPath, r.ToPath, r.StatusCode, r.ID)
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
	return s.RedirectByID(ctx, r.ID)
}

func (s *Store) RedirectByID(ctx context.Context, id int64) (*Redirect, error) {
	return scanRedirect(s.db.QueryRowContext(ctx, `SELECT `+redirectColumns+` FROM redirects WHERE id = ?`, id))
}

func (s *Store) RedirectByPath(ctx context.Context, from string) (*Redirect, error) {
	return scanRedirect(s.db.QueryRowContext(ctx, `SELECT `+redirectColumns+` FROM redirects WHERE from_path = ?`, from))
}

func (s *Store) ListRedirects(ctx context.Context) ([]Redirect, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+redirectColumns+` FROM redirects ORDER BY from_path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Redirect
	for rows.Next() {
		r, err := scanRedirect(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRedirect(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM redirects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Service wrappers ------------------------------------------------------------

func (s *Service) CreateAttachment(ctx context.Context, entryID, mediaID int64, title string, sortOrder int) (*Attachment, error) {
	title = strings.TrimSpace(title)
	if entryID == 0 || mediaID == 0 || title == "" {
		return nil, ErrInvalid
	}
	m, err := s.store.MediaByID(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if m.Kind != "document" || m.Status != "active" {
		return nil, ErrInvalid
	}
	if _, err := s.store.EntryByID(ctx, entryID); err != nil {
		return nil, err
	}
	return s.store.CreateAttachment(ctx, &Attachment{EntryID: entryID, MediaID: mediaID, Title: title, SortOrder: sortOrder})
}

func (s *Service) Attachments(ctx context.Context, entryID int64) ([]Attachment, error) {
	return s.store.ListAttachments(ctx, entryID)
}

func (s *Service) AttachmentByID(ctx context.Context, id int64) (*Attachment, error) {
	return s.store.AttachmentByID(ctx, id)
}

func (s *Service) DeleteAttachment(ctx context.Context, id int64) error {
	return s.store.DeleteAttachment(ctx, id)
}

func (s *Service) CreateComment(ctx context.Context, entryID, userID int64, authorName, authorEmail, body, ip string, moderation bool) (*Comment, error) {
	body = strings.TrimSpace(body)
	if entryID == 0 || body == "" {
		return nil, ErrInvalid
	}
	if _, err := s.store.EntryByID(ctx, entryID); err != nil {
		return nil, err
	}
	html, _ := markdown.Render(body)
	status := "approved"
	if moderation {
		status = "pending"
	}
	return s.store.CreateComment(ctx, &Comment{
		EntryID: entryID, UserID: userID, AuthorName: strings.TrimSpace(authorName),
		AuthorEmail: strings.TrimSpace(authorEmail), BodyMD: body, BodyHTML: html,
		Status: status, IP: ip,
	})
}

func (s *Service) ApprovedComments(ctx context.Context, entryID int64) ([]Comment, error) {
	return s.store.ListApprovedComments(ctx, entryID)
}

func (s *Service) ListComments(ctx context.Context, status string, limit int) ([]Comment, error) {
	return s.store.ListComments(ctx, status, limit)
}

func (s *Service) SetCommentStatus(ctx context.Context, id int64, status string) error {
	return s.store.SetCommentStatus(ctx, id, status)
}

func (s *Service) PendingComments(ctx context.Context) (int, error) {
	return s.store.CountPendingComments(ctx)
}

func (s *Service) CreateFriendLink(ctx context.Context, in FriendLink) (*FriendLink, error) {
	if err := normalizeFriendLink(&in); err != nil {
		return nil, err
	}
	return s.store.CreateFriendLink(ctx, &in)
}

func (s *Service) UpdateFriendLink(ctx context.Context, in FriendLink) (*FriendLink, error) {
	if in.ID == 0 {
		return nil, ErrInvalid
	}
	if err := normalizeFriendLink(&in); err != nil {
		return nil, err
	}
	return s.store.UpdateFriendLink(ctx, &in)
}

func (s *Service) FriendLinkByID(ctx context.Context, id int64) (*FriendLink, error) {
	return s.store.FriendLinkByID(ctx, id)
}

func (s *Service) ListFriendLinks(ctx context.Context, enabledOnly bool) ([]FriendLink, error) {
	return s.store.ListFriendLinks(ctx, enabledOnly)
}

func (s *Service) DeleteFriendLink(ctx context.Context, id int64) error {
	return s.store.DeleteFriendLink(ctx, id)
}

func normalizeFriendLink(l *FriendLink) error {
	l.Name = strings.TrimSpace(l.Name)
	l.URL = strings.TrimSpace(l.URL)
	if l.Name == "" || l.URL == "" {
		return ErrInvalid
	}
	u, err := url.Parse(l.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ErrInvalid
	}
	return nil
}

func (s *Service) CreateRedirect(ctx context.Context, in Redirect) (*Redirect, error) {
	if err := normalizeRedirect(&in); err != nil {
		return nil, err
	}
	return s.store.CreateRedirect(ctx, &in)
}

func (s *Service) UpdateRedirect(ctx context.Context, in Redirect) (*Redirect, error) {
	if in.ID == 0 {
		return nil, ErrInvalid
	}
	if err := normalizeRedirect(&in); err != nil {
		return nil, err
	}
	return s.store.UpdateRedirect(ctx, &in)
}

func (s *Service) RedirectByID(ctx context.Context, id int64) (*Redirect, error) {
	return s.store.RedirectByID(ctx, id)
}

func (s *Service) ListRedirects(ctx context.Context) ([]Redirect, error) {
	return s.store.ListRedirects(ctx)
}

func (s *Service) DeleteRedirect(ctx context.Context, id int64) error {
	return s.store.DeleteRedirect(ctx, id)
}

func (s *Service) RedirectForPath(ctx context.Context, path string) (*Redirect, error) {
	return s.store.RedirectByPath(ctx, path)
}

func normalizeRedirect(r *Redirect) error {
	r.FromPath = strings.TrimSpace(r.FromPath)
	r.ToPath = strings.TrimSpace(r.ToPath)
	if r.FromPath == "" || r.ToPath == "" {
		return ErrInvalid
	}
	if !strings.HasPrefix(r.FromPath, "/") || strings.HasPrefix(r.FromPath, "//") ||
		strings.ContainsAny(r.FromPath, "?#\\\r\n") {
		return ErrInvalid
	}
	if !strings.HasPrefix(r.ToPath, "/") || strings.HasPrefix(r.ToPath, "//") ||
		strings.ContainsAny(r.ToPath, "?#\\\r\n") {
		return ErrInvalid
	}
	if r.StatusCode == 0 {
		r.StatusCode = 301
	}
	if r.StatusCode != 301 && r.StatusCode != 302 {
		return ErrInvalid
	}
	return nil
}
