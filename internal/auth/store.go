package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

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

const adminColumns = `id, username, password_hash, display_name, role, status, created_at, updated_at,
	COALESCE(last_login_at, 0), COALESCE(password_changed_at, 0)`

func scanAdmin(scanner interface{ Scan(...any) error }) (*AdminUser, error) {
	var a AdminUser
	if err := scanner.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.DisplayName, &a.Role, &a.Status,
		&a.CreatedAt, &a.UpdatedAt, &a.LastLoginAt, &a.PasswordChangedAt); err != nil {
		return nil, mapNotFound(err)
	}
	return &a, nil
}

func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash, displayName, role string) (*AdminUser, error) {
	now := nowUnix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_users(username, password_hash, display_name, role, status, created_at, updated_at, password_changed_at)
		VALUES(?, ?, ?, ?, 'active', ?, ?, ?)`,
		username, passwordHash, displayName, role, now, now, now)
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
	return s.AdminByID(ctx, id)
}

func (s *Store) AdminByID(ctx context.Context, id int64) (*AdminUser, error) {
	return scanAdmin(s.db.QueryRowContext(ctx, `SELECT `+adminColumns+` FROM admin_users WHERE id = ?`, id))
}

func (s *Store) AdminByUsername(ctx context.Context, username string) (*AdminUser, error) {
	return scanAdmin(s.db.QueryRowContext(ctx, `SELECT `+adminColumns+` FROM admin_users WHERE username = ? COLLATE NOCASE`, username))
}

func (s *Store) ListAdmins(ctx context.Context) ([]AdminUser, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+adminColumns+` FROM admin_users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminUser
	for rows.Next() {
		a, err := scanAdmin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) UpdateAdminPassword(ctx context.Context, id int64, hash string) error {
	now := nowUnix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE admin_users SET password_hash = ?, password_changed_at = ?, updated_at = ? WHERE id = ?`,
		hash, now, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateAdminLastLogin(ctx context.Context, id int64) error {
	now := nowUnix()
	_, err := s.db.ExecContext(ctx,
		`UPDATE admin_users SET last_login_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

func (s *Store) SetAdminStatus(ctx context.Context, id int64, status string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE admin_users SET status = ?, updated_at = ? WHERE id = ?`, status, nowUnix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateAdminSession(ctx context.Context, id string, adminID int64, csrf, ip, ua string, createdAt, expiresAt, lastSeen int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_sessions(id, admin_id, csrf_token, ip, user_agent, created_at, expires_at, last_seen_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		id, adminID, csrf, ip, ua, createdAt, expiresAt, lastSeen)
	return err
}

const adminSessionColumns = `s.id, s.admin_id, u.username, u.display_name, u.role, u.status,
	s.csrf_token, COALESCE(s.ip, ''), COALESCE(s.user_agent, ''), s.created_at, s.expires_at, s.last_seen_at`

func scanAdminSession(scanner interface{ Scan(...any) error }) (*AdminSession, error) {
	var a AdminSession
	if err := scanner.Scan(&a.ID, &a.AdminID, &a.Username, &a.DisplayName, &a.Role, &a.Status,
		&a.CSRF, &a.IP, &a.UserAgent, &a.CreatedAt, &a.ExpiresAt, &a.LastSeenAt); err != nil {
		return nil, mapNotFound(err)
	}
	return &a, nil
}

func (s *Store) AdminSessionByHash(ctx context.Context, hash string) (*AdminSession, error) {
	return scanAdminSession(s.db.QueryRowContext(ctx, `
		SELECT `+adminSessionColumns+`
		FROM admin_sessions s JOIN admin_users u ON u.id = s.admin_id
		WHERE s.id = ?`, hash))
}

func (s *Store) TouchAdminSession(ctx context.Context, id string, lastSeen int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_sessions SET last_seen_at = ? WHERE id = ?`, lastSeen, id)
	return err
}

func (s *Store) DeleteAdminSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteAllAdminSessions(ctx context.Context, adminID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE admin_id = ?`, adminID)
	return err
}

func (s *Store) DeleteExpiredAdminSessions(ctx context.Context, now int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at <= ?`, now)
	return err
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&n)
	return n, err
}

const userColumns = `id, username, email, password_hash, display_name, COALESCE(avatar_media_id, 0), status,
	created_at, updated_at, COALESCE(last_login_at, 0), COALESCE(password_changed_at, 0)`

func scanUser(scanner interface{ Scan(...any) error }) (*User, error) {
	var u User
	if err := scanner.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.DisplayName,
		&u.AvatarMediaID, &u.Status, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.PasswordChangedAt); err != nil {
		return nil, mapNotFound(err)
	}
	return &u, nil
}

func (s *Store) CreateUser(ctx context.Context, username, email, passwordHash, displayName, status string) (*User, error) {
	return s.CreateUserWithInvite(ctx, username, email, passwordHash, displayName, status, nil)
}

// UserExists reports whether a username or email is already registered.
// It lets the service reject conflicts before doing expensive Argon2 work.
func (s *Store) UserExists(ctx context.Context, username, email string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM users
			WHERE username = ? COLLATE NOCASE OR email = ? COLLATE NOCASE
		)`, username, email).Scan(&exists)
	return exists != 0, err
}

// CreateUserWithInvite inserts the user and, when inviteID is set, consumes the
// invite atomically. A used/revoked/expired invite rolls the user back.
func (s *Store) CreateUserWithInvite(ctx context.Context, username, email, passwordHash, displayName, status string, inviteID *int64) (*User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	now := nowUnix()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO users(username, email, password_hash, display_name, status, created_at, updated_at, password_changed_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		username, email, passwordHash, displayName, status, now, now, now)
	if isUnique(err) {
		_ = tx.Rollback()
		return nil, ErrConflict
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if inviteID != nil {
		ires, err := tx.ExecContext(ctx, `
			UPDATE invites SET used_by_user_id = ?, used_at = ?
			WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL
			  AND (expires_at IS NULL OR expires_at > ?)`,
			id, now, *inviteID, now)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		n, _ := ires.RowsAffected()
		if n != 1 {
			_ = tx.Rollback()
			return nil, ErrInvalidInvite
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.UserByID(ctx, id)
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

func (s *Store) UserByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ? COLLATE NOCASE`, username))
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = ? COLLATE NOCASE`, email))
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) SetUserStatus(ctx context.Context, id int64, status string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET status = ?, updated_at = ? WHERE id = ?`, status, nowUnix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateUserPassword(ctx context.Context, id int64, hash string) error {
	now := nowUnix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, password_changed_at = ?, updated_at = ? WHERE id = ?`,
		hash, now, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateUserLastLogin(ctx context.Context, id int64) error {
	now := nowUnix()
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

func (s *Store) CreateUserSession(ctx context.Context, id string, userID int64, csrf, ip, ua string, createdAt, expiresAt, lastSeen int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_sessions(id, user_id, csrf_token, ip, user_agent, created_at, expires_at, last_seen_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, csrf, ip, ua, createdAt, expiresAt, lastSeen)
	return err
}

const userSessionColumns = `s.id, s.user_id, u.username, u.email, u.display_name, u.status,
	s.csrf_token, COALESCE(s.ip, ''), COALESCE(s.user_agent, ''), s.created_at, s.expires_at, s.last_seen_at`

func scanUserSession(scanner interface{ Scan(...any) error }) (*UserSession, error) {
	var u UserSession
	if err := scanner.Scan(&u.ID, &u.UserID, &u.Username, &u.Email, &u.DisplayName, &u.Status,
		&u.CSRF, &u.IP, &u.UserAgent, &u.CreatedAt, &u.ExpiresAt, &u.LastSeenAt); err != nil {
		return nil, mapNotFound(err)
	}
	return &u, nil
}

func (s *Store) UserSessionByHash(ctx context.Context, hash string) (*UserSession, error) {
	return scanUserSession(s.db.QueryRowContext(ctx, `
		SELECT `+userSessionColumns+`
		FROM user_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id = ?`, hash))
}

func (s *Store) TouchUserSession(ctx context.Context, id string, lastSeen int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_sessions SET last_seen_at = ? WHERE id = ?`, lastSeen, id)
	return err
}

func (s *Store) DeleteUserSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteAllUserSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE user_id = ?`, userID)
	return err
}

func (s *Store) DeleteOtherUserSessions(ctx context.Context, userID int64, keepID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE user_id = ? AND id <> ?`, userID, keepID)
	return err
}

func (s *Store) DeleteExpiredUserSessions(ctx context.Context, now int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE expires_at <= ?`, now)
	return err
}

func (s *Store) CreateInvite(ctx context.Context, codeHash string, adminID int64, createdBy string, expiresAt int64) (*Invite, error) {
	now := nowUnix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO invites(code_hash, created_by_admin_id, created_by, expires_at, created_at)
		VALUES(?, ?, ?, ?, ?)`,
		codeHash, nullInt64(adminID), createdBy, nullInt64(expiresAt), now)
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
	return s.InviteByID(ctx, id)
}

const inviteColumns = `id, code_hash, COALESCE(created_by_admin_id, 0), created_by, COALESCE(expires_at, 0),
	COALESCE(used_by_user_id, 0), COALESCE(used_at, 0), COALESCE(revoked_at, 0), created_at`

func scanInvite(scanner interface{ Scan(...any) error }) (*Invite, error) {
	var i Invite
	if err := scanner.Scan(&i.ID, &i.CodeHash, &i.CreatedByAdminID, &i.CreatedBy, &i.ExpiresAt,
		&i.UsedByUserID, &i.UsedAt, &i.RevokedAt, &i.CreatedAt); err != nil {
		return nil, mapNotFound(err)
	}
	return &i, nil
}

func (s *Store) InviteByID(ctx context.Context, id int64) (*Invite, error) {
	return scanInvite(s.db.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM invites WHERE id = ?`, id))
}

func (s *Store) InviteByCodeHash(ctx context.Context, hash string) (*Invite, error) {
	return scanInvite(s.db.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM invites WHERE code_hash = ?`, hash))
}

func (s *Store) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+inviteColumns+` FROM invites ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		i, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

func (s *Store) RevokeInvite(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE invites SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, nowUnix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Audit(ctx context.Context, actorType string, actorID int64, action, targetType string, targetID int64, ip, meta string) error {
	if meta == "" {
		meta = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_logs(actor_type, actor_id, action, target_type, target_id, ip, meta, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		actorType, nullInt64(actorID), action, targetType, nullInt64(targetID), ip, meta, nowUnix())
	return err
}

func (s *Store) RecentAudit(ctx context.Context, limit int) ([]AuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_type, COALESCE(actor_id, 0), action, COALESCE(target_type, ''), COALESCE(target_id, 0),
		       COALESCE(ip, ''), COALESCE(meta, '{}'), created_at
		FROM audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var a AuditLog
		if err := rows.Scan(&a.ID, &a.ActorType, &a.ActorID, &a.Action, &a.TargetType, &a.TargetID, &a.IP, &a.Meta, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) RecordLoginAttempt(ctx context.Context, scope, key, ip string, success bool) error {
	v := 0
	if success {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO login_attempts(scope, key, ip, success, created_at) VALUES(?, ?, ?, ?, ?)`,
		scope, key, ip, v, nowUnix())
	return err
}

func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var c Counts
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&c.Admins); err != nil {
		return c, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&c.Users); err != nil {
		return c, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE status = 'pending'`).Scan(&c.PendingUsers); err != nil {
		return c, err
	}
	return c, nil
}

func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
