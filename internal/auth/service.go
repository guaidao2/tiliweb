package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"tiliweb/internal/config"
	"tiliweb/internal/security"
)

var usernameRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{2,31}$`)

// Argon2id uses 64 MiB per operation with the default parameters. Keep a
// small global cap so anonymous or concurrent requests cannot multiply that
// into an OOM. Queueing is bounded by the request context.
const maxConcurrentPasswordHashes = 4
const hashSlotWait = 5 * time.Second
const maxPasswordLength = 1024
const maxLoginNameLength = 254

type Service struct {
	store          *Store
	cfg            *config.Config
	logger         *slog.Logger
	adminLimiter   *security.Limiter
	userLimiter    *security.Limiter
	passwordParams security.PasswordParams
	hashSlots      chan struct{}
}

func NewService(db *sql.DB, cfg *config.Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:  NewStore(db),
		cfg:    cfg,
		logger: logger,
		adminLimiter: security.NewLimiter(
			cfg.Admin.LockoutThreshold,
			cfg.Admin.LockoutDuration.Std(),
			cfg.Admin.LoginRatePerMinute,
		),
		userLimiter: security.NewLimiter(
			cfg.Auth.LockoutThreshold,
			cfg.Auth.LockoutDuration.Std(),
			cfg.Auth.LoginRatePerMinute,
		),
		passwordParams: security.DefaultPasswordParams,
		hashSlots:      make(chan struct{}, maxConcurrentPasswordHashes),
	}
}

// SetPasswordParams is intended for tests and for future parameter upgrades.
func (s *Service) SetPasswordParams(p security.PasswordParams) {
	s.passwordParams = p
}

func (s *Service) hashPassword(ctx context.Context, password string) (string, error) {
	if err := s.acquireHashSlot(ctx); err != nil {
		return "", hashWaitError(err)
	}
	defer s.releaseHashSlot()
	return security.HashPasswordWithParams(password, s.passwordParams)
}

func (s *Service) verifyPassword(ctx context.Context, password, encoded string) (bool, error) {
	if err := s.acquireHashSlot(ctx); err != nil {
		return false, hashWaitError(err)
	}
	defer s.releaseHashSlot()
	return security.VerifyPassword(password, encoded)
}

func hashWaitError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrBusy
	}
	return err
}

func (s *Service) acquireHashSlot(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	waitCtx, cancel := context.WithTimeout(ctx, hashSlotWait)
	defer cancel()
	select {
	case s.hashSlots <- struct{}{}:
		return nil
	case <-waitCtx.Done():
		return waitCtx.Err()
	}
}

func (s *Service) releaseHashSlot() {
	<-s.hashSlots
}

func (s *Service) Store() *Store {
	return s.store
}

// CleanupLimiters drops expired lockout/rate windows. Call periodically.
func (s *Service) CleanupLimiters() {
	s.adminLimiter.Cleanup()
	s.userLimiter.Cleanup()
}

func (s *Service) CreateAdmin(ctx context.Context, username, password, displayName, role string) (*AdminUser, error) {
	username = strings.TrimSpace(username)
	if !usernameRE.MatchString(username) {
		return nil, fmt.Errorf("%w: 3-32 chars", ErrInvalidUsername)
	}
	if role == "" {
		role = "superadmin"
	}
	if !validRole(role) {
		return nil, fmt.Errorf("invalid admin role")
	}
	if len(password) < s.cfg.Admin.PasswordMinLength {
		return nil, fmt.Errorf("%w: minimum %d", ErrPasswordTooShort, s.cfg.Admin.PasswordMinLength)
	}
	if len(password) > maxPasswordLength {
		return nil, ErrPasswordTooLong
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = username
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return nil, err
	}
	admin, err := s.store.CreateAdmin(ctx, username, hash, displayName, role)
	if err != nil {
		return nil, err
	}
	_ = s.store.Audit(ctx, "system", 0, "admin.create", "admin", admin.ID, "", metaJSON(map[string]any{"username": username, "role": role}))
	return admin, nil
}

func (s *Service) ListAdmins(ctx context.Context) ([]AdminUser, error) {
	return s.store.ListAdmins(ctx)
}

func (s *Service) AdminLogin(ctx context.Context, username, password, ip, ua string) (*AdminSession, string, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > maxLoginNameLength {
		s.adminLimiter.NoteRate(ip)
		return nil, "", ErrInvalidCredentials
	}
	key := "admin|" + strings.ToLower(username)
	now := time.Now().UTC().Unix()

	if ok, wait := s.adminLimiter.Allow(key, ip); !ok {
		_ = s.store.RecordLoginAttempt(ctx, "admin", key, ip, false)
		_ = s.store.Audit(ctx, "system", 0, "admin.login_locked", "admin", 0, ip, metaJSON(map[string]any{"username": username}))
		return nil, "", &RateLimitError{Wait: wait}
	}
	if password == "" || len(password) > maxPasswordLength {
		s.adminLimiter.Fail(key, ip)
		_ = s.store.RecordLoginAttempt(ctx, "admin", key, ip, false)
		_ = s.store.Audit(ctx, "system", 0, "admin.login_failed", "admin", 0, ip, metaJSON(map[string]any{"username": username}))
		return nil, "", ErrInvalidCredentials
	}

	admin, err := s.store.AdminByUsername(ctx, username)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, "", err
		}
		// Equalize the nonexistent-admin path with a real password check and
		// never spend lockout budget on a busy hash queue.
		if _, derr := s.hashPassword(ctx, password); derr != nil {
			if errors.Is(derr, ErrBusy) {
				s.adminLimiter.NoteRate(ip)
			}
			return nil, "", derr
		}
		s.adminLimiter.Fail(key, ip)
		_ = s.store.RecordLoginAttempt(ctx, "admin", key, ip, false)
		_ = s.store.Audit(ctx, "system", 0, "admin.login_failed", "admin", 0, ip, metaJSON(map[string]any{"username": username}))
		return nil, "", ErrInvalidCredentials
	}
	ok, err := s.verifyPassword(ctx, password, admin.PasswordHash)
	if errors.Is(err, ErrBusy) {
		s.adminLimiter.NoteRate(ip)
		return nil, "", ErrBusy
	}
	if err != nil {
		s.logger.Error("admin password verify failed", "error", err, "admin_id", admin.ID)
		s.adminLimiter.Fail(key, ip)
		_ = s.store.RecordLoginAttempt(ctx, "admin", key, ip, false)
		_ = s.store.Audit(ctx, "system", 0, "admin.login_failed", "admin", admin.ID, ip, metaJSON(map[string]any{"username": username}))
		return nil, "", ErrInvalidCredentials
	}
	if !ok {
		s.adminLimiter.Fail(key, ip)
		_ = s.store.RecordLoginAttempt(ctx, "admin", key, ip, false)
		_ = s.store.Audit(ctx, "system", 0, "admin.login_failed", "admin", admin.ID, ip, metaJSON(map[string]any{"username": username}))
		return nil, "", ErrInvalidCredentials
	}
	if admin.Status != "active" {
		_ = s.store.RecordLoginAttempt(ctx, "admin", key, ip, false)
		_ = s.store.Audit(ctx, "system", 0, "admin.login_failed", "admin", admin.ID, ip, metaJSON(map[string]any{"username": username, "reason": "inactive"}))
		return nil, "", ErrInvalidCredentials
	}

	s.adminLimiter.Success(key)
	if security.PasswordNeedsRehash(admin.PasswordHash, s.passwordParams) {
		if hash, err := s.hashPassword(ctx, password); err == nil {
			_ = s.store.UpdateAdminPassword(ctx, admin.ID, hash)
		}
	}

	token, err := security.NewSessionToken()
	if err != nil {
		return nil, "", err
	}
	csrf, err := security.NewCSRFToken()
	if err != nil {
		return nil, "", err
	}
	expiresAt := now + int64(s.cfg.Admin.SessionTTL.Std().Seconds())
	sessionID := security.HashToken(token)
	if err := s.store.CreateAdminSession(ctx, sessionID, admin.ID, csrf, ip, ua, now, expiresAt, now); err != nil {
		return nil, "", err
	}
	_ = s.store.UpdateAdminLastLogin(ctx, admin.ID)
	_ = s.store.RecordLoginAttempt(ctx, "admin", key, ip, true)
	_ = s.store.Audit(ctx, "admin", admin.ID, "admin.login", "admin", admin.ID, ip, "")

	return &AdminSession{
		ID:          sessionID,
		AdminID:     admin.ID,
		Username:    admin.Username,
		DisplayName: admin.DisplayName,
		Role:        admin.Role,
		Status:      admin.Status,
		CSRF:        csrf,
		IP:          ip,
		UserAgent:   ua,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		LastSeenAt:  now,
	}, token, nil
}

func (s *Service) LoadAdminSession(ctx context.Context, token string) (*AdminSession, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	session, err := s.store.AdminSessionByHash(ctx, security.HashToken(token))
	if err != nil {
		return nil, ErrUnauthorized
	}
	now := time.Now().UTC().Unix()
	idle := int64(s.cfg.Admin.SessionIdle.Std().Seconds())
	if session.Status != "active" || session.ExpiresAt <= now || now-session.LastSeenAt > idle {
		_ = s.store.DeleteAdminSession(ctx, session.ID)
		return nil, ErrUnauthorized
	}
	if now-session.LastSeenAt > 300 {
		_ = s.store.TouchAdminSession(ctx, session.ID, now)
		session.LastSeenAt = now
	}
	return session, nil
}

func (s *Service) AdminLogout(ctx context.Context, sessionID string) error {
	return s.store.DeleteAdminSession(ctx, sessionID)
}

func (s *Service) SetAdminPassword(ctx context.Context, username, password string, actorID int64) error {
	if len(password) < s.cfg.Admin.PasswordMinLength {
		return fmt.Errorf("%w: minimum %d", ErrPasswordTooShort, s.cfg.Admin.PasswordMinLength)
	}
	if len(password) > maxPasswordLength {
		return ErrPasswordTooLong
	}
	admin, err := s.store.AdminByUsername(ctx, username)
	if err != nil {
		return err
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return err
	}
	if err := s.store.UpdateAdminPassword(ctx, admin.ID, hash); err != nil {
		return err
	}
	_ = s.store.DeleteAllAdminSessions(ctx, admin.ID)
	_ = s.store.Audit(ctx, "system", actorID, "admin.password_reset", "admin", admin.ID, "", "")
	return nil
}

func (s *Service) RegisterUser(ctx context.Context, username, email, password, inviteCode, ip string) (*User, error) {
	if !s.cfg.Auth.RegisterEnabled {
		return nil, ErrRegisterDisabled
	}
	username = strings.TrimSpace(username)
	email = strings.ToLower(strings.TrimSpace(email))
	if !usernameRE.MatchString(username) {
		return nil, fmt.Errorf("%w: 3-32 chars", ErrInvalidUsername)
	}
	if !validEmail(email) {
		return nil, ErrInvalidEmail
	}
	if len(password) < s.cfg.Auth.PasswordMinLength {
		return nil, fmt.Errorf("%w: minimum %d", ErrPasswordTooShort, s.cfg.Auth.PasswordMinLength)
	}
	if len(password) > maxPasswordLength {
		return nil, ErrPasswordTooLong
	}

	mode := s.cfg.Auth.RegisterMode
	status := "active"
	var inviteID *int64
	switch mode {
	case "open":
		status = "active"
	case "approval":
		status = "pending"
	case "invite":
		code := strings.TrimSpace(inviteCode)
		if code == "" {
			return nil, ErrInvalidInvite
		}
		invite, err := s.store.InviteByCodeHash(ctx, security.HashToken(code))
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
				return nil, ErrInvalidInvite
			}
			return nil, err
		}
		now := time.Now().UTC().Unix()
		if invite.UsedAt != 0 || invite.RevokedAt != 0 || (invite.ExpiresAt != 0 && invite.ExpiresAt <= now) {
			return nil, ErrInvalidInvite
		}
		status = "active"
		inviteID = &invite.ID
	default:
		return nil, fmt.Errorf("invalid registration mode")
	}

	exists, err := s.store.UserExists(ctx, username, email)
	if err != nil {
		return nil, err
	}
	if exists {
		// Keep the conflict path's timing close to a successful registration;
		// the rate limiter and hash-slot cap bound the cost. Propagate a busy
		// slot error so conflict and success fail in the same way.
		if _, err := s.hashPassword(ctx, password); err != nil {
			return nil, err
		}
		return nil, ErrConflict
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return nil, err
	}
	user, err := s.store.CreateUserWithInvite(ctx, username, email, hash, username, status, inviteID)
	if err != nil {
		return nil, err
	}
	_ = s.store.Audit(ctx, "user", user.ID, "user.register", "user", user.ID, ip, metaJSON(map[string]any{"mode": mode, "status": status}))
	return user, nil
}

func (s *Service) UserLogin(ctx context.Context, username, password, ip, ua string) (*UserSession, string, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > maxLoginNameLength {
		s.userLimiter.NoteRate(ip)
		return nil, "", ErrInvalidCredentials
	}
	key := "user|" + strings.ToLower(username)
	now := time.Now().UTC().Unix()

	if ok, wait := s.userLimiter.Allow(key, ip); !ok {
		_ = s.store.RecordLoginAttempt(ctx, "user", key, ip, false)
		return nil, "", &RateLimitError{Wait: wait}
	}
	if password == "" || len(password) > maxPasswordLength {
		s.userLimiter.Fail(key, ip)
		_ = s.store.RecordLoginAttempt(ctx, "user", key, ip, false)
		return nil, "", ErrInvalidCredentials
	}

	user, err := s.findUserForLogin(ctx, username)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, "", err
		}
		// Equalize the expensive path so a nonexistent account cannot be
		// fingerprinted by response timing. Do not spend lockout budget on a
		// busy hash queue.
		if _, derr := s.hashPassword(ctx, password); derr != nil {
			if errors.Is(derr, ErrBusy) {
				s.userLimiter.NoteRate(ip)
			}
			return nil, "", derr
		}
		s.userLimiter.Fail(key, ip)
		_ = s.store.RecordLoginAttempt(ctx, "user", key, ip, false)
		return nil, "", ErrInvalidCredentials
	}
	ok, err := s.verifyPassword(ctx, password, user.PasswordHash)
	if errors.Is(err, ErrBusy) {
		s.userLimiter.NoteRate(ip)
		return nil, "", ErrBusy
	}
	if err != nil {
		s.logger.Error("user password verify failed", "error", err, "user_id", user.ID)
		s.userLimiter.Fail(key, ip)
		_ = s.store.RecordLoginAttempt(ctx, "user", key, ip, false)
		_ = s.store.Audit(ctx, "system", 0, "user.login_failed", "user", user.ID, ip, "")
		return nil, "", ErrInvalidCredentials
	}
	if !ok {
		s.userLimiter.Fail(key, ip)
		_ = s.store.RecordLoginAttempt(ctx, "user", key, ip, false)
		_ = s.store.Audit(ctx, "system", 0, "user.login_failed", "user", user.ID, ip, "")
		return nil, "", ErrInvalidCredentials
	}

	// Status is disclosed only after a successful password check, so an
	// unauthenticated caller cannot use login as a pending/disabled oracle.
	// These branches must not consume lockout budget: the password is valid,
	// and a pending user may retry after approval.
	if user.Status == "disabled" {
		_ = s.store.RecordLoginAttempt(ctx, "user", key, ip, false)
		return nil, "", ErrDisabled
	}
	if user.Status == "pending" {
		_ = s.store.RecordLoginAttempt(ctx, "user", key, ip, false)
		return nil, "", ErrPending
	}
	if user.Status != "active" {
		_ = s.store.RecordLoginAttempt(ctx, "user", key, ip, false)
		return nil, "", ErrInvalidCredentials
	}

	s.userLimiter.Success(key)
	if security.PasswordNeedsRehash(user.PasswordHash, s.passwordParams) {
		if hash, err := s.hashPassword(ctx, password); err == nil {
			_ = s.store.UpdateUserPassword(ctx, user.ID, hash)
		}
	}
	token, err := security.NewSessionToken()
	if err != nil {
		return nil, "", err
	}
	csrf, err := security.NewCSRFToken()
	if err != nil {
		return nil, "", err
	}
	expiresAt := now + int64(s.cfg.Auth.SessionTTL.Std().Seconds())
	sessionID := security.HashToken(token)
	if err := s.store.CreateUserSession(ctx, sessionID, user.ID, csrf, ip, ua, now, expiresAt, now); err != nil {
		return nil, "", err
	}
	_ = s.store.UpdateUserLastLogin(ctx, user.ID)
	_ = s.store.RecordLoginAttempt(ctx, "user", key, ip, true)
	_ = s.store.Audit(ctx, "user", user.ID, "user.login", "user", user.ID, ip, "")

	return &UserSession{
		ID:          sessionID,
		UserID:      user.ID,
		Username:    user.Username,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      user.Status,
		CSRF:        csrf,
		IP:          ip,
		UserAgent:   ua,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		LastSeenAt:  now,
	}, token, nil
}

func (s *Service) LoadUserSession(ctx context.Context, token string) (*UserSession, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	session, err := s.store.UserSessionByHash(ctx, security.HashToken(token))
	if err != nil {
		return nil, ErrUnauthorized
	}
	now := time.Now().UTC().Unix()
	idle := int64(s.cfg.Auth.SessionIdle.Std().Seconds())
	if session.Status != "active" || session.ExpiresAt <= now || now-session.LastSeenAt > idle {
		_ = s.store.DeleteUserSession(ctx, session.ID)
		return nil, ErrUnauthorized
	}
	if now-session.LastSeenAt > 300 {
		_ = s.store.TouchUserSession(ctx, session.ID, now)
		session.LastSeenAt = now
	}
	return session, nil
}

func (s *Service) UserLogout(ctx context.Context, sessionID string) error {
	return s.store.DeleteUserSession(ctx, sessionID)
}

func (s *Service) ChangeUserPassword(ctx context.Context, userID int64, current, next, keepSessionID string) error {
	user, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := s.verifyPassword(ctx, current, user.PasswordHash)
	if errors.Is(err, ErrBusy) {
		return ErrBusy
	}
	if err != nil {
		s.logger.Error("user password verify failed", "error", err, "user_id", user.ID)
		return ErrInvalidCredentials
	}
	if !ok {
		return ErrInvalidCredentials
	}
	if len(next) < s.cfg.Auth.PasswordMinLength {
		return fmt.Errorf("%w: minimum %d", ErrPasswordTooShort, s.cfg.Auth.PasswordMinLength)
	}
	if len(next) > maxPasswordLength {
		return ErrPasswordTooLong
	}
	hash, err := s.hashPassword(ctx, next)
	if err != nil {
		return err
	}
	if err := s.store.UpdateUserPassword(ctx, user.ID, hash); err != nil {
		return err
	}
	_ = s.store.DeleteOtherUserSessions(ctx, user.ID, keepSessionID)
	_ = s.store.Audit(ctx, "user", user.ID, "user.password_change", "user", user.ID, "", "")
	return nil
}

func (s *Service) SetUserStatus(ctx context.Context, actorAdminID, userID int64, status string) error {
	switch status {
	case "active", "pending", "disabled":
	default:
		return fmt.Errorf("invalid user status")
	}
	if err := s.store.SetUserStatus(ctx, userID, status); err != nil {
		return err
	}
	if status == "disabled" {
		_ = s.store.DeleteAllUserSessions(ctx, userID)
	}
	_ = s.store.Audit(ctx, "admin", actorAdminID, "user.status", "user", userID, "", metaJSON(map[string]any{"status": status}))
	return nil
}

func (s *Service) ResetUserPassword(ctx context.Context, actorAdminID, userID int64, password string) error {
	if len(password) < s.cfg.Auth.PasswordMinLength {
		return fmt.Errorf("%w: minimum %d", ErrPasswordTooShort, s.cfg.Auth.PasswordMinLength)
	}
	if len(password) > maxPasswordLength {
		return ErrPasswordTooLong
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return err
	}
	if err := s.store.UpdateUserPassword(ctx, userID, hash); err != nil {
		return err
	}
	_ = s.store.DeleteAllUserSessions(ctx, userID)
	_ = s.store.Audit(ctx, "admin", actorAdminID, "user.password_reset", "user", userID, "", "")
	return nil
}

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	return s.store.ListUsers(ctx)
}

func (s *Service) CreateInvite(ctx context.Context, actorAdminID int64, ttl time.Duration) (string, *Invite, error) {
	code, err := security.NewInviteCode()
	if err != nil {
		return "", nil, err
	}
	expiresAt := int64(0)
	if ttl > 0 {
		expiresAt = time.Now().UTC().Add(ttl).Unix()
	}
	invite, err := s.store.CreateInvite(ctx, security.HashToken(code), actorAdminID, "admin", expiresAt)
	if err != nil {
		return "", nil, err
	}
	_ = s.store.Audit(ctx, "admin", actorAdminID, "invite.create", "invite", invite.ID, "", "")
	return code, invite, nil
}

func (s *Service) ListInvites(ctx context.Context) ([]Invite, error) {
	return s.store.ListInvites(ctx)
}

func (s *Service) RevokeInvite(ctx context.Context, actorAdminID, inviteID int64) error {
	if err := s.store.RevokeInvite(ctx, inviteID); err != nil {
		return err
	}
	_ = s.store.Audit(ctx, "admin", actorAdminID, "invite.revoke", "invite", inviteID, "", "")
	return nil
}

func (s *Service) Counts(ctx context.Context) (Counts, error) {
	return s.store.Counts(ctx)
}

func (s *Service) RecentAudit(ctx context.Context, limit int) ([]AuditLog, error) {
	return s.store.RecentAudit(ctx, limit)
}

func validRole(role string) bool {
	switch role {
	case "superadmin", "editor", "moderator":
		return true
	default:
		return false
	}
}

func (s *Service) findUserForLogin(ctx context.Context, username string) (*User, error) {
	if strings.Contains(username, "@") {
		return s.store.UserByEmail(ctx, strings.ToLower(username))
	}
	return s.store.UserByUsername(ctx, username)
}

func validEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	return strings.EqualFold(addr.Address, email) && strings.Contains(email, "@")
}

func metaJSON(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
