package forum

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tiliweb/internal/config"
	"tiliweb/internal/content"
	"tiliweb/internal/markdown"
)

var (
	ErrNotFound      = errors.New("forum: not found")
	ErrInvalid       = errors.New("forum: invalid")
	ErrLocked        = errors.New("forum: topic locked")
	ErrForbidden     = errors.New("forum: forbidden")
	ErrBoardNotEmpty = errors.New("forum: board has topics")
)

type Board struct {
	ID          int64
	ParentID    int64
	Slug        string
	Name        string
	Description string
	Visibility  string
	Status      string
	SortOrder   int
	TopicCount  int
	ReplyCount  int
}

type Topic struct {
	ID           int64
	BoardID      int64
	BoardSlug    string
	BoardName    string
	Title        string
	Slug         string
	AuthorUserID int64
	AuthorName   string
	Status       string
	Pinned       bool
	ReplyCount   int
	ViewCount    int
	LastPostAt   int64
	LastPostID   int64
	LastPostName string
	CreatedAt    int64
	UpdatedAt    int64
}

type Post struct {
	ID           int64
	TopicID      int64
	AuthorUserID int64
	AuthorName   string
	QuotePostID  int64
	BodyMD       string
	BodyHTML     string
	FloorNo      int
	Status       string
	CreatedAt    int64
	UpdatedAt    int64
}

type Moderator struct {
	UserID      int64
	Username    string
	DisplayName string
}

type SearchTopic struct {
	ID        int64
	Title     string
	BoardSlug string
	BoardName string
	Summary   string
	CreatedAt int64
}

type Service struct {
	db                *sql.DB
	newUserModeration int
}

func New(db *sql.DB, cfg *config.Config) *Service {
	n := 0
	if cfg != nil {
		n = cfg.Forum.NewUserModerationPosts
	}
	return &Service{db: db, newUserModeration: n}
}

func nowUnix() int64 { return time.Now().UTC().Unix() }

const boardColumns = `id, COALESCE(parent_id,0), slug, name, COALESCE(description,''), visibility, status,
	sort_order, topic_count, reply_count`

func scanBoard(scanner interface{ Scan(...any) error }) (*Board, error) {
	var b Board
	if err := scanner.Scan(&b.ID, &b.ParentID, &b.Slug, &b.Name, &b.Description, &b.Visibility, &b.Status,
		&b.SortOrder, &b.TopicCount, &b.ReplyCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &b, nil
}

func (s *Service) ListBoards(ctx context.Context, includeHidden bool) ([]Board, error) {
	q := `SELECT ` + boardColumns + ` FROM boards`
	if !includeHidden {
		q += ` WHERE status = 'active'`
	}
	q += ` ORDER BY COALESCE(parent_id,0), sort_order, id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Board
	for rows.Next() {
		b, err := scanBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *Service) BoardByID(ctx context.Context, id int64) (*Board, error) {
	return scanBoard(s.db.QueryRowContext(ctx, `SELECT `+boardColumns+` FROM boards WHERE id = ?`, id))
}

func (s *Service) BoardBySlug(ctx context.Context, slug string) (*Board, error) {
	return scanBoard(s.db.QueryRowContext(ctx, `SELECT `+boardColumns+` FROM boards WHERE slug = ? COLLATE NOCASE`, slug))
}

func (s *Service) CreateBoard(ctx context.Context, b *Board) (*Board, error) {
	if err := normalizeBoard(b); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO boards(parent_id, slug, name, description, visibility, sort_order, status)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, nullInt(b.ParentID), b.Slug, b.Name, b.Description, b.Visibility, b.SortOrder, b.Status)
	if isUnique(err) {
		return nil, content.ErrConflict
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.BoardByID(ctx, id)
}

func (s *Service) UpdateBoard(ctx context.Context, b *Board) (*Board, error) {
	if b.ID == 0 {
		return nil, ErrInvalid
	}
	if err := normalizeBoard(b); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE boards SET parent_id=?, slug=?, name=?, description=?, visibility=?, sort_order=?, status=? WHERE id=?`,
		nullInt(b.ParentID), b.Slug, b.Name, b.Description, b.Visibility, b.SortOrder, b.Status, b.ID)
	if isUnique(err) {
		return nil, content.ErrConflict
	}
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.BoardByID(ctx, b.ID)
}

func (s *Service) DeleteBoard(ctx context.Context, id int64) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM topics WHERE board_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrBoardNotEmpty
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM boards WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func normalizeBoard(b *Board) error {
	b.Slug = strings.TrimSpace(b.Slug)
	b.Name = strings.TrimSpace(b.Name)
	if b.Slug == "" || b.Name == "" {
		return ErrInvalid
	}
	if b.Visibility == "" {
		b.Visibility = "public"
	}
	if b.Visibility != "public" && b.Visibility != "members" {
		return ErrInvalid
	}
	if b.Status == "" {
		b.Status = "active"
	}
	if b.Status != "active" && b.Status != "hidden" {
		return ErrInvalid
	}
	return nil
}

func (s *Service) BoardModerators(ctx context.Context, boardID int64) ([]Moderator, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.username, COALESCE(u.display_name,'')
		FROM forum_moderators m JOIN users u ON u.id = m.user_id WHERE m.board_id = ? ORDER BY u.username`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Moderator
	for rows.Next() {
		var m Moderator
		if err := rows.Scan(&m.UserID, &m.Username, &m.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) AddModerator(ctx context.Context, boardID, userID, adminID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO forum_moderators(board_id, user_id, granted_by_admin_id, created_at)
		VALUES(?, ?, ?, ?)`, boardID, userID, nullInt(adminID), nowUnix())
	return err
}

func (s *Service) RemoveModerator(ctx context.Context, boardID, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM forum_moderators WHERE board_id = ? AND user_id = ?`, boardID, userID)
	return err
}

func (s *Service) IsModerator(ctx context.Context, boardID, userID int64) bool {
	if userID == 0 {
		return false
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM forum_moderators WHERE board_id = ? AND user_id = ?`, boardID, userID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

const topicColumns = `t.id, t.board_id, b.slug, b.name, t.title, COALESCE(t.slug,''), t.author_user_id,
	COALESCE(NULLIF(u.display_name,''), u.username, ''), t.status, t.pinned, t.reply_count, t.view_count,
	COALESCE(t.last_post_at,0), COALESCE(t.last_post_user_id,0), COALESCE(NULLIF(lu.display_name,''), lu.username, ''),
	t.created_at, t.updated_at`

func scanTopic(scanner interface{ Scan(...any) error }) (*Topic, error) {
	var t Topic
	var pinned int
	if err := scanner.Scan(&t.ID, &t.BoardID, &t.BoardSlug, &t.BoardName, &t.Title, &t.Slug, &t.AuthorUserID,
		&t.AuthorName, &t.Status, &pinned, &t.ReplyCount, &t.ViewCount, &t.LastPostAt, &t.LastPostID, &t.LastPostName,
		&t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.Pinned = pinned != 0
	return &t, nil
}

const topicJoin = ` FROM topics t JOIN boards b ON b.id=t.board_id
	JOIN users u ON u.id=t.author_user_id LEFT JOIN users lu ON lu.id=t.last_post_user_id`

func (s *Service) ListTopics(ctx context.Context, boardID int64, limit, offset int) ([]Topic, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+topicColumns+topicJoin+`
		WHERE t.board_id = ? AND t.status <> 'deleted'
		ORDER BY t.pinned DESC, COALESCE(t.last_post_at,t.created_at) DESC, t.id DESC LIMIT ? OFFSET ?`,
		boardID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Topic
	for rows.Next() {
		t, err := scanTopic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Service) TopicByKey(ctx context.Context, boardSlug, key string) (*Topic, error) {
	var id int64
	if n, err := strconv.ParseInt(key, 10, 64); err == nil {
		id = n
	} else if i := strings.IndexByte(key, '-'); i > 0 {
		id, _ = strconv.ParseInt(key[:i], 10, 64)
	}
	q := `SELECT ` + topicColumns + topicJoin + ` WHERE b.slug = ? COLLATE NOCASE AND t.status <> 'deleted' AND `
	args := []any{boardSlug}
	if id > 0 {
		q += `t.id = ?`
		args = append(args, id)
	} else {
		q += `t.slug = ? COLLATE NOCASE`
		args = append(args, key)
	}
	return scanTopic(s.db.QueryRowContext(ctx, q, args...))
}

func (s *Service) TopicByID(ctx context.Context, id int64) (*Topic, error) {
	return scanTopic(s.db.QueryRowContext(ctx, `SELECT `+topicColumns+topicJoin+` WHERE t.id = ?`, id))
}

func (s *Service) ListAllTopics(ctx context.Context, limit int) ([]Topic, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+topicColumns+topicJoin+`
		WHERE t.status <> 'deleted'
		ORDER BY COALESCE(t.last_post_at,t.created_at) DESC, t.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Topic
	for rows.Next() {
		t, err := scanTopic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Service) CreateTopic(ctx context.Context, boardID, userID int64, title, body string, moderation bool) (*Topic, error) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" || body == "" || len([]rune(title)) > 200 || len([]rune(body)) > 20000 {
		return nil, ErrInvalid
	}
	b, err := s.BoardByID(ctx, boardID)
	if err != nil {
		return nil, err
	}
	if b.Status != "active" {
		return nil, ErrNotFound
	}
	html, _ := markdown.Render(body)
	status := "visible"
	if moderation {
		status = "pending"
	}
	slug := content.Slugify(title)
	now := nowUnix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO topics(board_id, title, slug, author_user_id, status, reply_count, view_count, last_post_at, last_post_user_id, created_at, updated_at)
		VALUES(?, ?, ?, ?, 'open', 0, 0, ?, ?, ?, ?)`, boardID, title, nullString(slug), userID, now, userID, now, now)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	topicID, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO forum_posts(topic_id, author_user_id, body_md, body_html, floor_no, status, created_at, updated_at)
		VALUES(?, ?, ?, ?, 1, ?, ?, ?)`, topicID, userID, body, html, status, now, now); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE boards SET topic_count = topic_count + 1 WHERE id = ?`, boardID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO topics_fts(topic_id, title, body) VALUES(?, ?, ?)`,
		topicID, content.NormalizeSearch(title), content.NormalizeSearch(body)); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.TopicByID(ctx, topicID)
}

func (s *Service) Reply(ctx context.Context, topicID, userID int64, body string, moderation bool, quoteID int64) (*Post, error) {
	body = strings.TrimSpace(body)
	if topicID == 0 || body == "" || len([]rune(body)) > 20000 {
		return nil, ErrInvalid
	}
	t, err := s.TopicByID(ctx, topicID)
	if err != nil {
		return nil, err
	}
	if t.Status != "open" {
		return nil, ErrLocked
	}
	html, _ := markdown.Render(body)
	status := "visible"
	if moderation {
		status = "pending"
	}
	now := nowUnix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var floor int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(floor_no),0)+1 FROM forum_posts WHERE topic_id = ?`, topicID).Scan(&floor); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO forum_posts(topic_id, author_user_id, quote_post_id, body_md, body_html, floor_no, status, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, topicID, userID, nullInt(quoteID), body, html, floor, status, now, now)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	postID, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE topics SET reply_count = reply_count + 1, last_post_at = ?, last_post_user_id = ?, updated_at = ? WHERE id = ?`,
		now, userID, now, topicID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE boards SET reply_count = reply_count + 1 WHERE id = ?`, t.BoardID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if status == "visible" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO forum_posts_fts(post_id, topic_id, body) VALUES(?, ?, ?)`,
			postID, topicID, content.NormalizeSearch(body)); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.PostByID(ctx, postID)
}

func scanPost(scanner interface{ Scan(...any) error }) (*Post, error) {
	var p Post
	if err := scanner.Scan(&p.ID, &p.TopicID, &p.AuthorUserID, &p.AuthorName, &p.QuotePostID,
		&p.BodyMD, &p.BodyHTML, &p.FloorNo, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

const postColumns = `p.id, p.topic_id, p.author_user_id, COALESCE(NULLIF(u.display_name,''),u.username,''),
	COALESCE(p.quote_post_id,0), p.body_md, p.body_html, p.floor_no, p.status, p.created_at, p.updated_at`
const postJoin = ` FROM forum_posts p JOIN users u ON u.id = p.author_user_id`

func (s *Service) PostByID(ctx context.Context, id int64) (*Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, `SELECT `+postColumns+postJoin+` WHERE p.id = ?`, id))
}

func (s *Service) Posts(ctx context.Context, topicID, viewerID int64, canModerate bool) ([]Post, error) {
	q := `SELECT ` + postColumns + postJoin + ` WHERE p.topic_id = ? AND (p.status = 'visible'`
	args := []any{topicID}
	if viewerID > 0 {
		q += ` OR (p.status = 'pending' AND p.author_user_id = ?)`
		args = append(args, viewerID)
	}
	if canModerate {
		q += ` OR p.status IN ('pending','hidden')`
	}
	q += `) ORDER BY p.floor_no, p.id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Service) IncrementTopicView(ctx context.Context, id int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE topics SET view_count = view_count + 1 WHERE id = ?`, id)
}

func (s *Service) ModerateTopic(ctx context.Context, id int64, action string) error {
	var q string
	switch action {
	case "pin":
		q = `UPDATE topics SET pinned = 1, updated_at = ? WHERE id = ?`
	case "unpin":
		q = `UPDATE topics SET pinned = 0, updated_at = ? WHERE id = ?`
	case "lock":
		q = `UPDATE topics SET status = 'locked', updated_at = ? WHERE id = ?`
	case "unlock":
		q = `UPDATE topics SET status = 'open', updated_at = ? WHERE id = ?`
	case "delete":
		q = `UPDATE topics SET status = 'deleted', updated_at = ? WHERE id = ?`
	case "restore":
		q = `UPDATE topics SET status = 'open', updated_at = ? WHERE id = ?`
	default:
		return ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, q, nowUnix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ModeratePost(ctx context.Context, id int64, action string) error {
	status := ""
	switch action {
	case "approve":
		status = "visible"
	case "hide":
		status = "hidden"
	case "delete":
		status = "deleted"
	case "restore":
		status = "visible"
	default:
		return ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `UPDATE forum_posts SET status = ?, updated_at = ? WHERE id = ?`, status, nowUnix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if status == "visible" {
		var p Post
		var topicID int64
		var body string
		if err := s.db.QueryRowContext(ctx, `SELECT topic_id, body_md FROM forum_posts WHERE id = ?`, id).Scan(&topicID, &body); err == nil {
			p.TopicID = topicID
			_, _ = s.db.ExecContext(ctx, `DELETE FROM forum_posts_fts WHERE post_id = ?`, id)
			_, _ = s.db.ExecContext(ctx, `INSERT INTO forum_posts_fts(post_id, topic_id, body) VALUES(?, ?, ?)`,
				id, p.TopicID, content.NormalizeSearch(body))
		}
	}
	return nil
}

func (s *Service) NeedsModeration(ctx context.Context, userID int64) bool {
	if s.newUserModeration <= 0 || userID == 0 {
		return false
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM forum_posts WHERE author_user_id = ? AND status = 'visible'`, userID).Scan(&n); err != nil {
		return false
	}
	return n < s.newUserModeration
}

func (s *Service) Search(ctx context.Context, q string, limit int) ([]SearchTopic, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	phrase := `"` + strings.ReplaceAll(content.NormalizeSearch(q), `"`, `""`) + `"`
	out := map[int64]SearchTopic{}
	if rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.title, b.slug, b.name, COALESCE(t.created_at,0)
		FROM topics_fts f JOIN topics t ON t.id = CAST(f.topic_id AS INTEGER) JOIN boards b ON b.id=t.board_id
		WHERE topics_fts MATCH ? AND t.status <> 'deleted' AND b.status='active' LIMIT ?`, phrase, limit); err == nil {
		for rows.Next() {
			var r SearchTopic
			if err := rows.Scan(&r.ID, &r.Title, &r.BoardSlug, &r.BoardName, &r.CreatedAt); err == nil {
				out[r.ID] = r
			}
		}
		rows.Close()
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.title, b.slug, b.name, COALESCE(t.created_at,0)
		FROM forum_posts_fts f JOIN forum_posts p ON p.id = CAST(f.post_id AS INTEGER)
		JOIN topics t ON t.id = p.topic_id JOIN boards b ON b.id=t.board_id
		WHERE forum_posts_fts MATCH ? AND p.status='visible' AND t.status <> 'deleted' AND b.status='active' LIMIT ?`, phrase, limit); err == nil {
		for rows.Next() {
			var r SearchTopic
			if err := rows.Scan(&r.ID, &r.Title, &r.BoardSlug, &r.BoardName, &r.CreatedAt); err == nil {
				if _, ok := out[r.ID]; !ok {
					out[r.ID] = r
				}
			}
		}
		rows.Close()
	}
	result := make([]SearchTopic, 0, len(out))
	for _, r := range out {
		r.Summary = "Forum topic"
		result = append(result, r)
	}
	return result, nil
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

func (s *Service) MustBoard(id int64) (*Board, error) {
	b, err := s.BoardByID(context.Background(), id)
	if err != nil {
		return nil, fmt.Errorf("board %d: %w", id, err)
	}
	return b, nil
}
