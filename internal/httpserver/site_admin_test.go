package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"tiliweb/internal/site"
)

func TestAdminSiteManagement(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()
	ctx := context.Background()

	cookie := loginAdmin(t, s, mustAdmin(t, s), "password123")
	session, err := s.auth.LoadAdminSession(ctx, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/admin/banners", "/admin/banners/new", "/admin/home", "/admin/settings", "/admin/messages"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	rec := postAdminForm(t, s, cookie, "/admin/banners/new", url.Values{
		"_csrf":       {session.CSRF},
		"title":       {"Welcome"},
		"link_url":    {"/contact"},
		"button_text": {"Contact"},
		"sort_order":  {"1"},
		"enabled":     {"1"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create banner status=%d body=%s", rec.Code, rec.Body.String())
	}
	banners, err := s.content.ListBanners(ctx, false)
	if err != nil || len(banners) != 1 || banners[0].LinkURL != "/contact" {
		t.Fatalf("banners = %+v, %v", banners, err)
	}

	rec = postAdminForm(t, s, cookie, "/admin/home", url.Values{
		"_csrf":          {session.CSRF},
		"show_banners":   {"1"},
		"latest_count":   {"5"},
		"featured_count": {"2"},
		"cta_title":      {"Talk to us"},
		"cta_text":       {"Tell us"},
		"cta_url":        {"/contact"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save home status=%d body=%s", rec.Code, rec.Body.String())
	}
	settings, err := s.site.Load(ctx, s.cfg)
	if err != nil || settings.Home.LatestCount != 5 || !settings.Home.ShowBanners {
		t.Fatalf("home settings = %+v, %v", settings.Home, err)
	}

	rec = postAdminForm(t, s, cookie, "/admin/settings", url.Values{
		"_csrf":                   {session.CSRF},
		"site_name":               {"Test Site"},
		"site_description":        {"Desc"},
		"contact_email":           {"hello@example.com"},
		"contact_page_md":         {"# Contact"},
		"seo_default_title":       {"Test Site"},
		"seo_default_description": {"SEO"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save settings status=%d body=%s", rec.Code, rec.Body.String())
	}
	settings, err = s.site.Load(ctx, s.cfg)
	if err != nil || settings.SiteName != "Test Site" || settings.Contact.Email != "hello@example.com" {
		t.Fatalf("settings = %+v, %v", settings, err)
	}

	msg, err := s.site.CreateContactMessage(ctx, &site.ContactMessage{Name: "Alice", Message: "Need a quote", SourcePage: "/contact"})
	if err != nil {
		t.Fatal(err)
	}
	rec = postAdminForm(t, s, cookie, "/admin/messages/"+strconv.FormatInt(msg.ID, 10)+"/status", url.Values{
		"_csrf":  {session.CSRF},
		"status": {"replied"},
		"note":   {"called back"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("message status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated, err := s.site.ContactMessageByID(ctx, msg.ID)
	if err != nil || updated.Status != "replied" || updated.AdminNote != "called back" {
		t.Fatalf("message = %+v, %v", updated, err)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/messages", nil)
	req.AddCookie(cookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Need a quote") {
		t.Fatalf("messages page status=%d", rec.Code)
	}
}
