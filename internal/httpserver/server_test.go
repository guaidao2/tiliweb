package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"tiliweb/internal/auth"
	"tiliweb/internal/config"
	"tiliweb/internal/content"
	"tiliweb/internal/forum"
	"tiliweb/internal/security"
	"tiliweb/internal/site"
	"tiliweb/internal/store/sqlite"
	"tiliweb/internal/view"
)

func TestHome(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"tiliweb", "欢迎", "/static/style.css"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q", want)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type = %q", ct)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id")
	}
}

func TestHealthAndReady(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		if body := rec.Body.String(); body != "ok\n" {
			t.Fatalf("%s body = %q", path, body)
		}
	}
}

func TestNotFound(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "页面不存在") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestStatic(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/style.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), ":root") {
		t.Fatal("css body missing :root")
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Fatalf("cache-control = %q", cc)
	}
}

func TestBasePath(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Server.BasePath = "/cms"
	})
	defer cleanup()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/cms/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tiliweb") {
		t.Fatalf("base home status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/cms/static/style.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("base static status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("root health status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/cms/" {
		t.Fatalf("root redirect status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/cms", nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/cms/" {
		t.Fatalf("base redirect status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSecurityHeaders(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	checks := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "SAMEORIGIN",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for k, want := range checks {
		if got := rec.Header().Get(k); got != want {
			t.Fatalf("%s = %q, want %q", k, got, want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("csp = %q", csp)
	}
}

func TestRequestIDPassthrough(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "test-123")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-Id"); got != "test-123" {
		t.Fatalf("request id = %q", got)
	}
}

func TestClientIPTrustedProxy(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Server.TrustedProxies = []string{"10.0.0.1", "10.0.0.2"}
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.2")
	if got := s.clientIP(req); got != "203.0.113.9" {
		t.Fatalf("client ip = %q", got)
	}

	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := s.clientIP(req); got != "198.51.100.7" {
		t.Fatalf("untrusted client ip = %q", got)
	}
}

func TestReadyDBDown(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer func() { _ = s.db.Close() }()
	_ = cleanup

	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRobots(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Disallow: /admin") {
		t.Fatalf("robots = %s", rec.Body.String())
	}
}

func TestStaticDirectoryNotListed(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "style.css") {
		t.Fatal("static directory listing leaked files")
	}
}

func TestHSTSFromTrustedProxy(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Server.TrustedProxies = []string{"10.0.0.1"}
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if hsts := rec.Header().Get("Strict-Transport-Security"); !strings.Contains(hsts, "max-age=31536000") {
		t.Fatalf("hsts = %q", hsts)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if hsts := rec.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Fatalf("untrusted proxy should not enable hsts: %q", hsts)
	}
}

func TestAdminLoginDashboard(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()
	ctx := context.Background()

	admin, err := s.auth.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin")
	if err != nil {
		t.Fatal(err)
	}
	cookie := loginAdmin(t, s, "admin", "password123")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(cookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"仪表盘", "最近审计", "前台用户"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.AddCookie(cookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "前台用户") {
		t.Fatalf("users status=%d body=%s", rec.Code, rec.Body.String())
	}

	session, err := s.auth.LoadAdminSession(ctx, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	if session.AdminID != admin.ID {
		t.Fatalf("session admin = %d", session.AdminID)
	}
	form := url.Values{"_csrf": {session.CSRF}}
	req = httptest.NewRequest(http.MethodPost, "/admin/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("logout status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminCSRFRequired(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()
	if _, err := s.auth.CreateAdmin(context.Background(), "admin", "password123", "Admin", "superadmin"); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"username": {"admin"}, "password": {"password123"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAdminPathRedirect(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/admin/" {
		t.Fatalf("status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminUnauthorizedRedirect(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminUserStatus(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()
	ctx := context.Background()

	cookie := loginAdmin(t, s, mustAdmin(t, s), "password123")
	user, err := s.auth.RegisterUser(ctx, "alice", "alice@example.com", "password123", "", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.auth.LoadAdminSession(ctx, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{"_csrf": {session.CSRF}, "status": {"disabled"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+strconv.FormatInt(user.ID, 10)+"/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	users, err := s.auth.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Status != "disabled" {
		t.Fatalf("users = %+v", users)
	}
}

func TestAdminInvites(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()
	ctx := context.Background()

	cookie := loginAdmin(t, s, mustAdmin(t, s), "password123")
	session, err := s.auth.LoadAdminSession(ctx, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/invites", nil)
	req.AddCookie(cookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invites status = %d", rec.Code)
	}

	form := url.Values{"_csrf": {session.CSRF}, "hours": {"24"}}
	req = httptest.NewRequest(http.MethodPost, "/admin/invites", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "新邀请码") {
		t.Fatalf("create invite status=%d body=%s", rec.Code, rec.Body.String())
	}

	invites, err := s.auth.ListInvites(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(invites) != 1 || invites[0].UsedAt != 0 || invites[0].RevokedAt != 0 {
		t.Fatalf("invites = %+v", invites)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/invites/"+strconv.FormatInt(invites[0].ID, 10)+"/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("revoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	invites, err = s.auth.ListInvites(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(invites) != 1 || invites[0].RevokedAt == 0 {
		t.Fatalf("revoked invites = %+v", invites)
	}
}

func mustAdmin(t *testing.T, s *Server) string {
	t.Helper()
	if _, err := s.auth.CreateAdmin(context.Background(), "admin", "password123", "Admin", "superadmin"); err != nil {
		t.Fatal(err)
	}
	return "admin"
}

func loginAdmin(t *testing.T, s *Server, username, password string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/login", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("login page status = %d", rec.Code)
	}
	csrf := extractCSRF(t, rec.Body.String())
	var csrfCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == s.cfg.Security.CSRFCookieName {
			csrfCookie = c
		}
	}
	if csrf == "" || csrfCookie == nil {
		t.Fatalf("missing csrf token/cookie; body=%s", rec.Body.String())
	}

	form := url.Values{"username": {username}, "password": {password}, "_csrf": {csrf}}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/" {
		t.Fatalf("login status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == s.cfg.Admin.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("missing admin session cookie")
	return nil
}

var csrfInputRE = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

func extractCSRF(t *testing.T, body string) string {
	t.Helper()
	m := csrfInputRE.FindStringSubmatch(body)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

func newTestServer(t *testing.T, mutate func(*config.Config)) (*Server, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.DataDir = filepath.Join(dir, "data")
	cfg.Storage.MediaDir = filepath.Join(dir, "media")
	cfg.Storage.DocumentDir = filepath.Join(dir, "documents")
	cfg.Storage.BackupDir = filepath.Join(dir, "backups")
	cfg.Storage.LogDir = filepath.Join(dir, "logs")
	cfg.Database.DSN = filepath.Join(dir, "db", "tiliweb.db")
	cfg.Log.AccessLog = false
	cfg.Site.Name = "tiliweb"
	cfg.Site.Description = "A test site"
	cfg.Admin.PasswordMinLength = 8
	cfg.Auth.PasswordMinLength = 8
	if mutate != nil {
		mutate(&cfg)
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config: %v", err)
	}
	if err := cfg.CheckPaths(); err != nil {
		t.Fatalf("paths: %v", err)
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, cfg.Database.DSN, 2, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.Migrate(ctx, db); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	v, err := view.NewEmbedded()
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authSvc := auth.NewService(db, &cfg, logger)
	authSvc.SetPasswordParams(security.PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32})
	contentSvc := content.NewService(db)
	forumSvc := forum.New(db, &cfg)
	siteStore := site.NewStore(db)
	s, err := New(&cfg, db, v, authSvc, contentSvc, forumSvc, siteStore, logger)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return s, func() { _ = db.Close() }
}
