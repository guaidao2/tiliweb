package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"tiliweb/internal/config"
)

func TestFrontRegisterOpenAndAccount(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Auth.RegisterMode = "open"
	})
	defer cleanup()

	csrf, csrfCookie := getPageCSRF(t, s, "/register")
	form := url.Values{
		"_csrf":    {csrf},
		"username": {"alice"},
		"email":    {"alice@example.com"},
		"password": {"password123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account" {
		t.Fatalf("register status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	userCookie := cookieFromRec(t, rec, s.cfg.Auth.CookieName)
	if userCookie == nil {
		t.Fatal("missing user session cookie")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(userCookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("account status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"alice", "alice@example.com", "active"} {
		if !strings.Contains(body, want) {
			t.Fatalf("account missing %q", want)
		}
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/account/password", nil)
	req.AddCookie(userCookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("password page status = %d", rec.Code)
	}
	passwordCSRF := extractCSRF(t, rec.Body.String())
	if passwordCSRF == "" {
		t.Fatal("missing session csrf token")
	}

	form = url.Values{"_csrf": {passwordCSRF}, "current_password": {"password123"}, "new_password": {"newpassword123"}}
	req = httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account?password=1" {
		t.Fatalf("password change status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/account?password=1", nil)
	req.AddCookie(userCookie)
	s.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "密码已更新") {
		t.Fatal("password success message missing")
	}

	form = url.Values{"_csrf": {passwordCSRF}}
	req = httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("logout status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(userCookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("after logout status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestFrontApprovalPending(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Auth.RegisterMode = "approval"
	})
	defer cleanup()

	csrf, csrfCookie := getPageCSRF(t, s, "/register")
	form := url.Values{
		"_csrf":    {csrf},
		"username": {"bob"},
		"email":    {"bob@example.com"},
		"password": {"password123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login?registered=pending" {
		t.Fatalf("register status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login?registered=pending", nil))
	if !strings.Contains(rec.Body.String(), "管理员审批") {
		t.Fatalf("approval message missing: %s", rec.Body.String())
	}
	csrf = extractCSRF(t, rec.Body.String())
	var newCSRF *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == s.cfg.Security.CSRFCookieName {
			newCSRF = c
		}
	}
	if csrf == "" || newCSRF == nil {
		t.Fatal("missing login csrf")
	}

	form = url.Values{"_csrf": {csrf}, "username": {"bob"}, "password": {"password123"}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(newCSRF)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "等待管理员审批") {
		t.Fatalf("pending login status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFrontInviteRegistration(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Auth.RegisterMode = "invite"
	})
	defer cleanup()
	ctx := context.Background()

	admin, err := s.auth.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin")
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := s.auth.CreateInvite(ctx, admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	csrf, csrfCookie := getPageCSRF(t, s, "/register")
	form := url.Values{
		"_csrf":    {csrf},
		"username": {"carol"},
		"email":    {"carol@example.com"},
		"password": {"password123"},
		"invite":   {code},
	}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account" {
		t.Fatalf("invite register status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	users, err := s.auth.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Status != "active" || users[0].Username != "carol" {
		t.Fatalf("users = %+v", users)
	}
}

func TestFrontCSRFRequired(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Auth.RegisterMode = "open"
	})
	defer cleanup()

	for _, path := range []string{"/login", "/register"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("username=x&password=y"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
	}
}

func TestFrontSessionIsolation(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Auth.RegisterMode = "open"
	})
	defer cleanup()
	ctx := context.Background()

	if _, err := s.auth.RegisterUser(ctx, "alice", "alice@example.com", "password123", "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	_, userToken, err := s.auth.UserLogin(ctx, "alice", "password123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	userCookie := &http.Cookie{Name: s.cfg.Auth.CookieName, Value: userToken, Path: "/"}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(userCookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("user cookie on admin: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	adminCookie := loginAdmin(t, s, mustAdmin(t, s), "password123")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(adminCookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("admin cookie on account: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(userCookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("user account status = %d", rec.Code)
	}
}

func TestFrontBasePath(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Server.BasePath = "/cms"
	})
	defer cleanup()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/cms/login", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `action="/cms/login"`) {
		t.Fatalf("base path login action missing: %s", rec.Body.String())
	}
}

func getPageCSRF(t *testing.T, s *Server, path string) (string, *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d", path, rec.Code)
	}
	token := extractCSRF(t, rec.Body.String())
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == s.cfg.Security.CSRFCookieName {
			cookie = c
		}
	}
	if token == "" || cookie == nil {
		t.Fatalf("%s missing csrf token/cookie", path)
	}
	return token, cookie
}

func cookieFromRec(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return c
		}
	}
	return nil
}
