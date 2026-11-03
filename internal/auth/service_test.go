package auth

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tiliweb/internal/config"
	"tiliweb/internal/security"
	"tiliweb/internal/store/sqlite"
)

func TestCreateAdminAndLogin(t *testing.T) {
	svc, cleanup := newTestService(t, nil)
	defer cleanup()
	ctx := context.Background()

	admin, err := svc.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin")
	if err != nil {
		t.Fatal(err)
	}
	if admin.Role != "superadmin" {
		t.Fatalf("role = %q", admin.Role)
	}
	if _, err := svc.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate create = %v", err)
	}

	session, token, err := svc.AdminLogin(ctx, "admin", "password123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || session.CSRF == "" || session.Username != "admin" {
		t.Fatalf("session = %+v token=%q", session, token)
	}

	loaded, err := svc.LoadAdminSession(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AdminID != admin.ID {
		t.Fatalf("loaded admin = %d", loaded.AdminID)
	}

	if err := svc.AdminLogout(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LoadAdminSession(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("after logout = %v", err)
	}
}

func TestAdminWrongPasswordAndLockout(t *testing.T) {
	svc, cleanup := newTestService(t, func(cfg *config.Config) {
		cfg.Admin.LockoutThreshold = 2
		cfg.Admin.LockoutDuration = config.Duration(time.Minute)
		cfg.Admin.LoginRatePerMinute = 100
	})
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AdminLogin(ctx, "admin", "wrong", "127.0.0.1", "test"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password = %v", err)
	}
	if _, _, err := svc.AdminLogin(ctx, "admin", "wrong", "127.0.0.1", "test"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("second wrong password = %v", err)
	}
	_, _, err := svc.AdminLogin(ctx, "admin", "password123", "127.0.0.1", "test")
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected rate limit, got %v", err)
	}
}

func TestHashBusyDoesNotConsumeLoginLockout(t *testing.T) {
	svc, cleanup := newTestService(t, func(cfg *config.Config) {
		cfg.Auth.LockoutThreshold = 1
		cfg.Admin.LockoutThreshold = 1
	})
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.RegisterUser(ctx, "alice", "alice@example.com", "password123", "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin"); err != nil {
		t.Fatal(err)
	}

	fill := func() {
		for i := 0; i < maxConcurrentPasswordHashes; i++ {
			svc.hashSlots <- struct{}{}
		}
	}
	drain := func() {
		for i := 0; i < maxConcurrentPasswordHashes; i++ {
			<-svc.hashSlots
		}
	}

	fill()
	busy, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	_, _, err := svc.UserLogin(busy, "alice", "password123", "127.0.0.1", "test")
	cancel()
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("user busy login = %v", err)
	}
	drain()
	if _, _, err := svc.UserLogin(ctx, "alice", "password123", "127.0.0.1", "test"); err != nil {
		t.Fatalf("user was locked after busy: %v", err)
	}

	fill()
	busy, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
	_, _, err = svc.AdminLogin(busy, "admin", "password123", "127.0.0.1", "test")
	cancel()
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("admin busy login = %v", err)
	}
	drain()
	if _, _, err := svc.AdminLogin(ctx, "admin", "password123", "127.0.0.1", "test"); err != nil {
		t.Fatalf("admin was locked after busy: %v", err)
	}
}

func TestPasswordLengthLimit(t *testing.T) {
	svc, cleanup := newTestService(t, nil)
	defer cleanup()
	ctx := context.Background()
	long := strings.Repeat("a", maxPasswordLength+1)

	if _, err := svc.RegisterUser(ctx, "alice", "alice@example.com", long, "", "127.0.0.1"); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("long registration password = %v", err)
	}
	if _, err := svc.CreateAdmin(ctx, "admin", long, "Admin", "superadmin"); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("long admin password = %v", err)
	}
	user, err := svc.RegisterUser(ctx, "bob", "bob@example.com", "password123", "", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangeUserPassword(ctx, user.ID, "password123", long, ""); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("long changed password = %v", err)
	}
	if err := svc.ResetUserPassword(ctx, 0, user.ID, long); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("long reset password = %v", err)
	}
}

func TestDisabledUserAndInactiveAdminLogin(t *testing.T) {
	svc, cleanup := newTestService(t, nil)
	defer cleanup()
	ctx := context.Background()

	user, err := svc.RegisterUser(ctx, "alice", "alice@example.com", "password123", "", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetUserStatus(ctx, 0, user.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.UserLogin(ctx, "alice", "password123", "127.0.0.1", "test"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled user login = %v", err)
	}

	admin, err := svc.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetAdminStatus(ctx, admin.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AdminLogin(ctx, "admin", "password123", "127.0.0.1", "test"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("inactive admin login = %v", err)
	}
}

func TestStoreErrorIsNotInvalidCredentials(t *testing.T) {
	svc, cleanup := newTestService(t, nil)
	defer cleanup()
	ctx := context.Background()

	if err := svc.store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.UserLogin(ctx, "alice", "password123", "127.0.0.1", "test"); err == nil ||
		errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrBusy) {
		t.Fatalf("user store error = %v", err)
	}
	if _, _, err := svc.AdminLogin(ctx, "admin", "password123", "127.0.0.1", "test"); err == nil ||
		errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrBusy) {
		t.Fatalf("admin store error = %v", err)
	}
}

func TestBusyCountsRateButNotLockout(t *testing.T) {
	svc, cleanup := newTestService(t, func(cfg *config.Config) {
		cfg.Auth.LoginRatePerMinute = 1
		cfg.Auth.LockoutThreshold = 100
	})
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.RegisterUser(ctx, "alice", "alice@example.com", "password123", "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxConcurrentPasswordHashes; i++ {
		svc.hashSlots <- struct{}{}
	}
	busy, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	_, _, err := svc.UserLogin(busy, "alice", "password123", "127.0.0.1", "test")
	cancel()
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("busy login = %v", err)
	}
	for i := 0; i < maxConcurrentPasswordHashes; i++ {
		<-svc.hashSlots
	}

	_, _, err = svc.UserLogin(ctx, "alice", "password123", "127.0.0.1", "test")
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("busy attempt did not count against rate limit: %v", err)
	}
}

func TestRegisterStoreErrorsNotMasked(t *testing.T) {
	ctx := context.Background()

	inviteSvc, inviteCleanup := newTestService(t, func(cfg *config.Config) {
		cfg.Auth.RegisterMode = "invite"
	})
	defer inviteCleanup()
	if err := inviteSvc.store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := inviteSvc.RegisterUser(ctx, "alice", "alice@example.com", "password123", "code", "127.0.0.1"); err == nil || errors.Is(err, ErrInvalidInvite) {
		t.Fatalf("invite store error was masked: %v", err)
	}

	openSvc, openCleanup := newTestService(t, func(cfg *config.Config) {
		cfg.Auth.RegisterMode = "open"
	})
	defer openCleanup()
	if err := openSvc.store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openSvc.RegisterUser(ctx, "bob", "bob@example.com", "password123", "", "127.0.0.1"); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("UserExists store error was masked: %v", err)
	}
}

func TestNonexistentLoginIsInvalidCredentials(t *testing.T) {
	svc, cleanup := newTestService(t, nil)
	defer cleanup()
	ctx := context.Background()

	if _, _, err := svc.UserLogin(ctx, "ghost", "password123", "127.0.0.1", "test"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("nonexistent user login = %v", err)
	}
	if _, _, err := svc.AdminLogin(ctx, "ghost", "password123", "127.0.0.1", "test"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("nonexistent admin login = %v", err)
	}
}

func TestHashSlotBusy(t *testing.T) {
	svc, cleanup := newTestService(t, nil)
	defer cleanup()

	for i := 0; i < maxConcurrentPasswordHashes; i++ {
		svc.hashSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < maxConcurrentPasswordHashes; i++ {
			<-svc.hashSlots
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := svc.hashPassword(ctx, "password123"); !errors.Is(err, ErrBusy) {
		t.Fatalf("hash slot busy = %v", err)
	}
}

func TestUserRegistrationModes(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		svc, cleanup := newTestService(t, func(cfg *config.Config) {
			cfg.Auth.RegisterMode = "open"
		})
		defer cleanup()
		ctx := context.Background()

		user, err := svc.RegisterUser(ctx, "alice", "alice@example.com", "password123", "", "127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		if user.Status != "active" {
			t.Fatalf("status = %q", user.Status)
		}
		session, token, err := svc.UserLogin(ctx, "alice", "password123", "127.0.0.1", "test")
		if err != nil {
			t.Fatal(err)
		}
		if session.Email != "alice@example.com" {
			t.Fatalf("email = %q", session.Email)
		}
		if _, err := svc.LoadUserSession(ctx, token); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("approval", func(t *testing.T) {
		svc, cleanup := newTestService(t, func(cfg *config.Config) {
			cfg.Auth.RegisterMode = "approval"
		})
		defer cleanup()
		ctx := context.Background()

		user, err := svc.RegisterUser(ctx, "bob", "bob@example.com", "password123", "", "127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		if user.Status != "pending" {
			t.Fatalf("status = %q", user.Status)
		}
		if _, _, err := svc.UserLogin(ctx, "bob", "password123", "127.0.0.1", "test"); !errors.Is(err, ErrPending) {
			t.Fatalf("pending login = %v", err)
		}
	})

	t.Run("invite", func(t *testing.T) {
		svc, cleanup := newTestService(t, func(cfg *config.Config) {
			cfg.Auth.RegisterMode = "invite"
		})
		defer cleanup()
		ctx := context.Background()

		admin, err := svc.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin")
		if err != nil {
			t.Fatal(err)
		}
		code, _, err := svc.CreateInvite(ctx, admin.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if code == "" {
			t.Fatal("empty invite code")
		}
		user, err := svc.RegisterUser(ctx, "carol", "carol@example.com", "password123", code, "127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		if user.Status != "active" {
			t.Fatalf("status = %q", user.Status)
		}
		if _, err := svc.RegisterUser(ctx, "dave", "dave@example.com", "password123", code, "127.0.0.1"); !errors.Is(err, ErrInvalidInvite) {
			t.Fatalf("reused invite = %v", err)
		}
		if _, err := svc.RegisterUser(ctx, "erin", "erin@example.com", "password123", "bad-code", "127.0.0.1"); !errors.Is(err, ErrInvalidInvite) {
			t.Fatalf("bad invite = %v", err)
		}
	})
}

func TestSessionIsolation(t *testing.T) {
	svc, cleanup := newTestService(t, nil)
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin"); err != nil {
		t.Fatal(err)
	}
	_, adminToken, err := svc.AdminLogin(ctx, "admin", "password123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterUser(ctx, "alice", "alice@example.com", "password123", "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	_, userToken, err := svc.UserLogin(ctx, "alice", "password123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.LoadUserSession(ctx, adminToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("admin token accepted as user: %v", err)
	}
	if _, err := svc.LoadAdminSession(ctx, userToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("user token accepted as admin: %v", err)
	}
}

func TestChangeUserPassword(t *testing.T) {
	svc, cleanup := newTestService(t, nil)
	defer cleanup()
	ctx := context.Background()

	user, err := svc.RegisterUser(ctx, "alice", "alice@example.com", "password123", "", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := svc.UserLogin(ctx, "alice", "password123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangeUserPassword(ctx, user.ID, "password123", "newpassword123", session.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.UserLogin(ctx, "alice", "password123", "127.0.0.1", "test"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password = %v", err)
	}
	if _, _, err := svc.UserLogin(ctx, "alice", "newpassword123", "127.0.0.1", "test"); err != nil {
		t.Fatalf("new password = %v", err)
	}
}

func TestCountsAndAudit(t *testing.T) {
	svc, cleanup := newTestService(t, nil)
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterUser(ctx, "alice", "alice@example.com", "password123", "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	counts, err := svc.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Admins != 1 || counts.Users != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	logs, err := svc.RecentAudit(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 {
		t.Fatal("expected audit entries")
	}
}

func newTestService(t *testing.T, mutate func(*config.Config)) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Admin.PasswordMinLength = 8
	cfg.Auth.PasswordMinLength = 8
	cfg.Auth.RegisterEnabled = true
	cfg.Auth.RegisterMode = "open"
	cfg.Admin.LockoutThreshold = 5
	cfg.Admin.LockoutDuration = config.Duration(time.Minute)
	cfg.Admin.LoginRatePerMinute = 100
	cfg.Auth.LockoutThreshold = 5
	cfg.Auth.LockoutDuration = config.Duration(time.Minute)
	cfg.Auth.LoginRatePerMinute = 100
	if mutate != nil {
		mutate(&cfg)
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.Storage.DataDir = filepath.Join(dir, "data")
	cfg.Storage.MediaDir = filepath.Join(dir, "media")
	cfg.Storage.DocumentDir = filepath.Join(dir, "documents")
	cfg.Storage.BackupDir = filepath.Join(dir, "backups")
	cfg.Storage.LogDir = filepath.Join(dir, "logs")
	cfg.Database.DSN = filepath.Join(dir, "db", "auth.db")
	cfg.CheckPaths()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, cfg.Database.DSN, 4, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.Migrate(ctx, db); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(db, &cfg, logger)
	svc.SetPasswordParams(security.PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32})
	return svc, func() { _ = db.Close() }
}

var _ = sql.ErrNoRows
