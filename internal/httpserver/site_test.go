package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tiliweb/internal/content"
)

func TestSiteContentRoutes(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()
	ctx := context.Background()

	admin, err := s.auth.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.content.CreateChannel(ctx, &content.Channel{
		Slug: "news", Name: "News", Type: "article", Status: "active", ShowInNav: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.content.CreateEntry(ctx, admin.ID, content.EntryInput{
		ChannelID: ch.ID, Slug: "hello", Title: "First post", Summary: "Summary",
		BodyMD: "# Body\n\nHello **world**", Status: "published", Tags: []string{"Go"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.content.CreatePage(ctx, admin.ID, content.PageInput{
		Slug: "about", Title: "About us", BodyMD: "About **page**", Status: "published", ShowInNav: true,
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path string
		want string
	}{
		{"/news", "First post"},
		{"/news/hello", "Hello"},
		{"/about", "About us"},
		{"/tags/go", "First post"},
		{"/news/hello", "world"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s missing %q", tc.path, tc.want)
		}
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing-page", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", rec.Code)
	}
}
