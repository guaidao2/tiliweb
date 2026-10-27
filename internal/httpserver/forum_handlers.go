package httpserver

import (
	"net/http"
	"net/url"
	"strconv"

	"tiliweb/internal/auth"
)

func (s *Server) forumIndex(w http.ResponseWriter, r *http.Request) {
	boards, err := s.forum.ListBoards(r.Context(), false)
	data := s.pageData(r, "Forum", "Community discussions")
	if err != nil {
		data.Error = err.Error()
	}
	data.Boards = boards
	s.renderSite(w, r, "site/forum", data, http.StatusOK)
}

func (s *Server) forumBoard(w http.ResponseWriter, r *http.Request) {
	b, err := s.forum.BoardBySlug(r.Context(), r.PathValue("board"))
	if err != nil || b.Status != "active" {
		s.notFound(w, r)
		return
	}
	if b.Visibility == "members" && userFromContext(r.Context()) == nil {
		http.Redirect(w, r, s.frontURL("login")+"?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	topics, err := s.forum.ListTopics(r.Context(), b.ID, 50, 0)
	data := s.pageData(r, b.Name, b.Description)
	data.Board = b
	data.Topics = topics
	if err != nil {
		data.Error = err.Error()
	}
	s.renderSite(w, r, "site/forum_board", data, http.StatusOK)
}

func (s *Server) forumTopic(w http.ResponseWriter, r *http.Request) {
	b, err := s.forum.BoardBySlug(r.Context(), r.PathValue("board"))
	if err != nil || b.Status != "active" {
		s.notFound(w, r)
		return
	}
	if b.Visibility == "members" && userFromContext(r.Context()) == nil {
		http.Redirect(w, r, s.frontURL("login")+"?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	t, err := s.forum.TopicByKey(r.Context(), b.Slug, r.PathValue("topic"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	user := userFromContext(r.Context())
	canModerate := user != nil && s.forum.IsModerator(r.Context(), b.ID, user.UserID)
	posts, err := s.forum.Posts(r.Context(), t.ID, userID(user), canModerate)
	data := s.pageData(r, t.Title, "")
	data.Board = b
	data.Topic = t
	data.Posts = posts
	data.CanModerate = canModerate
	if err != nil {
		data.Error = err.Error()
	}
	switch r.URL.Query().Get("posted") {
	case "topic":
		data.Success = "主题已发布。"
	case "reply":
		data.Success = "回复已发布。"
	case "pending":
		data.Success = "内容已提交，等待审核。"
	}
	s.forum.IncrementTopicView(r.Context(), t.ID)
	s.renderSite(w, r, "site/forum_topic", data, http.StatusOK)
}

func (s *Server) forumNewTopic(w http.ResponseWriter, r *http.Request) {
	b, err := s.forum.BoardBySlug(r.Context(), r.PathValue("board"))
	if err != nil || b.Status != "active" {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	user := userFromContext(r.Context())
	if !s.postLimiter.allow(clientIPFromContext(r.Context())) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	t, err := s.forum.CreateTopic(r.Context(), b.ID, user.UserID,
		r.PostFormValue("title"), r.PostFormValue("body"), s.forum.NeedsModeration(r.Context(), user.UserID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, "/forum/"+b.Slug+"/"+formatID(t.ID))+"?posted=topic", http.StatusSeeOther)
}

func (s *Server) forumReply(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad topic id", http.StatusBadRequest)
		return
	}
	t, err := s.forum.TopicByID(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if t.Status != "open" {
		http.Error(w, "topic is locked", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	user := userFromContext(r.Context())
	if !s.postLimiter.allow(clientIPFromContext(r.Context())) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	_, err = s.forum.Reply(r.Context(), t.ID, user.UserID, r.PostFormValue("body"),
		s.forum.NeedsModeration(r.Context(), user.UserID), formInt64(r, "quote_post_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, "/forum/"+t.BoardSlug+"/"+formatID(t.ID))+"?posted=reply", http.StatusSeeOther)
}

func (s *Server) forumModerateTopic(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad topic id", http.StatusBadRequest)
		return
	}
	t, err := s.forum.TopicByID(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	user := userFromContext(r.Context())
	if !s.forum.IsModerator(r.Context(), t.BoardID, user.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := s.forum.ModerateTopic(r.Context(), t.ID, r.PostFormValue("action")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, "/forum/"+t.BoardSlug+"/"+formatID(t.ID)), http.StatusSeeOther)
}

func (s *Server) forumModeratePost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad post id", http.StatusBadRequest)
		return
	}
	p, err := s.forum.PostByID(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	t, err := s.forum.TopicByID(r.Context(), p.TopicID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	user := userFromContext(r.Context())
	if !s.forum.IsModerator(r.Context(), t.BoardID, user.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := s.forum.ModeratePost(r.Context(), p.ID, r.PostFormValue("action")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, "/forum/"+t.BoardSlug+"/"+formatID(t.ID)), http.StatusSeeOther)
}

func userID(u *auth.UserSession) int64 {
	if u == nil {
		return 0
	}
	return u.UserID
}

func formatID(id int64) string {
	return strconv.FormatInt(id, 10)
}
