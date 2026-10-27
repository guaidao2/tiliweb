package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestAdminContentCRUD(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()
	ctx := context.Background()

	cookie := loginAdmin(t, s, mustAdmin(t, s), "password123")
	session, err := s.auth.LoadAdminSession(ctx, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/admin/channels", "/admin/channels/new", "/admin/entries", "/admin/entries/new", "/admin/pages", "/admin/pages/new", "/admin/tags"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	channelForm := url.Values{
		"_csrf":       {session.CSRF},
		"slug":        {"news"},
		"name":        {"News"},
		"type":        {"article"},
		"status":      {"active"},
		"show_in_nav": {"1"},
	}
	rec := postAdminForm(t, s, cookie, "/admin/channels/new", channelForm)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create channel status=%d body=%s", rec.Code, rec.Body.String())
	}
	ch, err := s.content.ChannelBySlug(ctx, "news")
	if err != nil {
		t.Fatal(err)
	}

	entryForm := url.Values{
		"_csrf":      {session.CSRF},
		"channel_id": {strconv.FormatInt(ch.ID, 10)},
		"title":      {"First post"},
		"slug":       {"first-post"},
		"body_md":    {"# Hello\n\nBody"},
		"status":     {"published"},
		"tags":       {"Go, Web"},
	}
	rec = postAdminForm(t, s, cookie, "/admin/entries/new", entryForm)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create entry status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/entries", nil)
	req.AddCookie(cookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "First post") {
		t.Fatalf("entries status=%d body=%s", rec.Code, rec.Body.String())
	}

	pageForm := url.Values{
		"_csrf":       {session.CSRF},
		"slug":        {"about"},
		"title":       {"About"},
		"body_md":     {"About page"},
		"template":    {"page"},
		"status":      {"published"},
		"show_in_nav": {"1"},
	}
	rec = postAdminForm(t, s, cookie, "/admin/pages/new", pageForm)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create page status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := s.content.PageBySlug(ctx, "about"); err != nil {
		t.Fatal(err)
	}
}

func postAdminForm(t *testing.T, s *Server, cookie *http.Cookie, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}
