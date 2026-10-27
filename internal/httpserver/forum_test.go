package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"tiliweb/internal/config"
	"tiliweb/internal/forum"
)

func TestForumFlow(t *testing.T) {
	s, cleanup := newTestServer(t, func(cfg *config.Config) {
		cfg.Auth.RegisterEnabled = true
		cfg.Auth.RegisterMode = "open"
		cfg.Forum.NewUserModerationPosts = 0
	})
	defer cleanup()
	ctx := context.Background()

	user, err := s.auth.RegisterUser(ctx, "alice", "alice@example.com", "password1234", "", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := s.auth.UserLogin(ctx, "alice", "password1234", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.auth.LoadUserSession(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	userCookie := &http.Cookie{Name: s.cfg.Auth.CookieName, Value: token, Path: "/"}

	board, err := s.forum.CreateBoard(ctx, &forum.Board{
		Slug: "general", Name: "General", Visibility: "public", Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if body := getPublic(t, s, "/forum").Body.String(); !strings.Contains(body, "General") {
		t.Fatalf("forum index missing board")
	}

	rec := postAdminForm(t, s, userCookie, "/forum/general/new", url.Values{
		"_csrf": {session.CSRF}, "title": {"Hello 产品"}, "body": {"Forum body"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("new topic status=%d body=%s", rec.Code, rec.Body.String())
	}
	topics, err := s.forum.ListTopics(ctx, board.ID, 10, 0)
	if err != nil || len(topics) != 1 {
		t.Fatalf("topics = %+v, %v", topics, err)
	}
	topic := topics[0]
	if body := getPublic(t, s, "/forum/general/"+strconv.FormatInt(topic.ID, 10)).Body.String(); !strings.Contains(body, "Forum body") {
		t.Fatalf("topic body missing")
	}

	rec = postAdminForm(t, s, userCookie, "/forum/topic/"+strconv.FormatInt(topic.ID, 10)+"/reply", url.Values{
		"_csrf": {session.CSRF}, "body": {"First reply"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reply status=%d body=%s", rec.Code, rec.Body.String())
	}
	if body := getPublic(t, s, "/forum/general/"+strconv.FormatInt(topic.ID, 10)).Body.String(); !strings.Contains(body, "First reply") {
		t.Fatalf("reply missing")
	}

	adminCookie := loginAdmin(t, s, mustAdmin(t, s), "password123")
	adminSession, err := s.auth.LoadAdminSession(ctx, adminCookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	rec = postAdminForm(t, s, adminCookie, "/admin/forum/boards/"+strconv.FormatInt(board.ID, 10)+"/moderators", url.Values{
		"_csrf": {adminSession.CSRF}, "user_id": {strconv.FormatInt(user.ID, 10)}, "action": {"add"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("appoint moderator status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !s.forum.IsModerator(ctx, board.ID, user.ID) {
		t.Fatal("moderator not appointed")
	}

	rec = postAdminForm(t, s, userCookie, "/forum/topic/"+strconv.FormatInt(topic.ID, 10)+"/moderate", url.Values{
		"_csrf": {session.CSRF}, "action": {"pin"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("pin status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated, _ := s.forum.TopicByID(ctx, topic.ID)
	if !updated.Pinned {
		t.Fatal("topic not pinned")
	}
	rec = postAdminForm(t, s, userCookie, "/forum/topic/"+strconv.FormatInt(topic.ID, 10)+"/moderate", url.Values{
		"_csrf": {session.CSRF}, "action": {"lock"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("lock status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = postAdminForm(t, s, userCookie, "/forum/topic/"+strconv.FormatInt(topic.ID, 10)+"/reply", url.Values{
		"_csrf": {session.CSRF}, "body": {"blocked"},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("locked reply status=%d", rec.Code)
	}

	if body := getPublic(t, s, "/search?q=%E4%BA%A7%E5%93%81&type=forum").Body.String(); !strings.Contains(body, "Hello 产品") {
		t.Fatalf("forum search missing topic")
	}

	for _, path := range []string{"/admin/forum/boards", "/admin/forum/topics"} {
		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(adminCookie)
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, rec.Code)
		}
	}

	members, err := s.forum.CreateBoard(ctx, &forum.Board{Slug: "members", Name: "Members", Visibility: "members", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	rec = getPublic(t, s, "/forum/"+members.Slug)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("members guest status=%d", rec.Code)
	}
}
