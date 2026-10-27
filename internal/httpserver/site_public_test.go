package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"tiliweb/internal/content"
)

var csrfFieldRE = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

func TestPublicSiteFeatures(t *testing.T) {
	s, cleanup := newTestServer(t, nil)
	defer cleanup()
	ctx := context.Background()

	admin, err := s.auth.CreateAdmin(ctx, "admin", "password123", "Admin", "superadmin")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.content.CreateChannel(ctx, &content.Channel{Slug: "news", Name: "News", Type: "article", Status: "active", ShowInNav: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.content.CreateEntry(ctx, admin.ID, content.EntryInput{
		ChannelID: ch.ID, Slug: "manual", Title: "产品手册", Summary: "产品说明",
		BodyMD: "# 产品手册\n\n这是产品使用说明。", Status: "published", Featured: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.content.CreatePage(ctx, admin.ID, content.PageInput{
		Slug: "about", Title: "About us", BodyMD: "About **us**", Status: "published", ShowInNav: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.content.CreateBanner(ctx, content.BannerInput{
		Title: "Welcome banner", Subtitle: "A better site", LinkURL: "/contact", ButtonText: "Contact", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	settings, err := s.site.Load(ctx, s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	settings.SiteName = "Tili Site"
	settings.SiteDescription = "Site description"
	settings.Contact.Email = "hello@example.com"
	settings.SEO.DefaultTitle = "Tili Site"
	settings.Home.CTATitle = "Talk to us"
	settings.ContactPageMD = "# Contact\n\nEmail us."
	if err := s.site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	rec := getPublic(t, s, "/")
	body := rec.Body.String()
	for _, want := range []string{"Welcome banner", "产品手册", "Tili Site", "Talk to us", "application/ld+json", "Organization"} {
		if !strings.Contains(body, want) {
			t.Fatalf("home missing %q", want)
		}
	}

	rec = getPublic(t, s, "/news/manual")
	body = rec.Body.String()
	if !strings.Contains(body, "产品手册") || !strings.Contains(body, "application/ld+json") || !strings.Contains(body, "Article") {
		t.Fatalf("entry SEO body = %s", body)
	}

	rec = getPublic(t, s, "/contact")
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "hello@example.com") || !strings.Contains(body, "发送留言") {
		t.Fatalf("contact page status=%d body=%s", rec.Code, body)
	}
	token := csrfFieldRE.FindStringSubmatch(body)
	if len(token) != 2 {
		t.Fatal("missing csrf token")
	}
	var csrfCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == s.cfg.Security.CSRFCookieName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("missing csrf cookie")
	}
	form := url.Values{
		"_csrf":   {token[1]},
		"started": {strconv.FormatInt(time.Now().Unix()-3, 10)},
		"name":    {"Alice"},
		"email":   {"alice@example.com"},
		"message": {"Need a quote"},
	}
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	postRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(postRec, req)
	if postRec.Code != http.StatusSeeOther {
		t.Fatalf("contact post status=%d body=%s", postRec.Code, postRec.Body.String())
	}
	messages, err := s.site.ListContactMessages(ctx, "new", 10)
	if err != nil || len(messages) != 1 || messages[0].Name != "Alice" {
		t.Fatalf("messages = %+v, %v", messages, err)
	}

	rec = getPublic(t, s, "/search?q=%E4%BA%A7%E5%93%81")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "产品手册") {
		t.Fatalf("search status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = getPublic(t, s, "/rss.xml")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<rss") || !strings.Contains(rec.Body.String(), "产品手册") {
		t.Fatalf("rss status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = getPublic(t, s, "/sitemap.xml")
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "http://localhost:8080/news/manual") || !strings.Contains(body, "/about") {
		t.Fatalf("sitemap status=%d body=%s", rec.Code, body)
	}
	rec = getPublic(t, s, "/robots.txt")
	body = rec.Body.String()
	if !strings.Contains(body, "Sitemap:") || !strings.Contains(body, "/admin") || !strings.Contains(body, "/search") {
		t.Fatalf("robots = %s", body)
	}
}

func getPublic(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}
