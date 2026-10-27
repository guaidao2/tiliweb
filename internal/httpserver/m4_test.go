package httpserver

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"tiliweb/internal/config"
	"tiliweb/internal/content"
)

func TestDownloadsCommentsLinksRedirects(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Auth.GuestComment = true
		cfg.Content.CommentModeration = false
	})
	defer cleanup()
	ctx := context.Background()

	cookie := loginAdmin(t, s, mustAdmin(t, s), "password123")
	session, err := s.auth.LoadAdminSession(ctx, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.content.CreateChannel(ctx, &content.Channel{
		Slug: "downloads", Name: "Downloads", Type: "download", Status: "active", CommentsEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.content.CreateEntry(ctx, 1, content.EntryInput{
		ChannelID: ch.ID, Slug: "manual", Title: "资料", BodyMD: "下载资料", Status: "published",
		ExtraJSON: `{"version":"1.0","file_note":"PDF"}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Attachment upload + forced download.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("_csrf", session.CSRF)
	_ = mw.WriteField("title", "Manual PDF")
	fw, _ := mw.CreateFormFile("file", "manual.pdf")
	_, _ = fw.Write([]byte("%PDF-1.4\n%%EOF\n"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/entries/"+strconv.FormatInt(entry.ID, 10)+"/attachments", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	attachments, err := s.content.Attachments(ctx, entry.ID)
	if err != nil || len(attachments) != 1 {
		t.Fatalf("attachments = %+v, %v", attachments, err)
	}
	rec = getPublic(t, s, "/download/"+strconv.FormatInt(attachments[0].ID, 10))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("download status=%d disposition=%q", rec.Code, rec.Header().Get("Content-Disposition"))
	}

	// Guest comment with double-submit CSRF.
	entryPage := getPublic(t, s, "/downloads/manual")
	token := csrfFieldRE.FindStringSubmatch(entryPage.Body.String())
	if len(token) != 2 {
		t.Fatal("missing comment csrf token")
	}
	var csrfCookie *http.Cookie
	for _, c := range entryPage.Result().Cookies() {
		if c.Name == s.cfg.Security.CSRFCookieName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("missing csrf cookie")
	}
	form := url.Values{"_csrf": {token[1]}, "author_name": {"Guest"}, "body": {"Nice manual"}}
	req = httptest.NewRequest(http.MethodPost, "/downloads/manual/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("comment status=%d body=%s", rec.Code, rec.Body.String())
	}
	if body := getPublic(t, s, "/downloads/manual").Body.String(); !strings.Contains(body, "Nice manual") {
		t.Fatalf("comment missing from entry: %s", body)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/comments", nil)
	req.AddCookie(cookie)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin comments status=%d", rec.Code)
	}

	// Friend links.
	rec = postAdminForm(t, s, cookie, "/admin/links/new", url.Values{
		"_csrf": {session.CSRF}, "name": {"Partner"}, "url": {"https://example.com"}, "enabled": {"1"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("link status=%d", rec.Code)
	}
	if body := getPublic(t, s, "/links").Body.String(); !strings.Contains(body, "Partner") {
		t.Fatalf("links page missing partner")
	}

	// 301 redirect.
	rec = postAdminForm(t, s, cookie, "/admin/redirects/new", url.Values{
		"_csrf": {session.CSRF}, "from_path": {"/old"}, "to_path": {"/about"}, "status_code": {"301"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("redirect status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = getPublic(t, s, "/old")
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/about" {
		t.Fatalf("redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// Extend job/product fields via admin form.
	rec = postAdminForm(t, s, cookie, "/admin/entries/"+strconv.FormatInt(entry.ID, 10)+"/edit", url.Values{
		"_csrf": {session.CSRF}, "channel_id": {strconv.FormatInt(ch.ID, 10)}, "title": {"资料"},
		"slug": {"manual"}, "body_md": {"下载资料"}, "status": {"published"},
		"extra_version": {"2.0"}, "extra_file_note": {"Updated PDF"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("entry update status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated, err := s.content.GetEntry(ctx, entry.ID)
	if err != nil || updated.Extra["version"] != "2.0" || updated.Extra["file_note"] != "Updated PDF" {
		t.Fatalf("extra = %+v, %v", updated.Extra, err)
	}
}
