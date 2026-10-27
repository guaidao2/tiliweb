package httpserver

import (
	"mime"
	"net/http"
	"os"
	"strings"

	"tiliweb/internal/content"
	"tiliweb/internal/media"
)

func (s *Server) linksPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Friend links", "")
	s.renderSite(w, r, "site/links", data, http.StatusOK)
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.content.AttachmentByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	e, err := s.content.GetEntry(r.Context(), a.EntryID)
	if err != nil || e.Status != "published" || e.DeletedAt != 0 {
		http.NotFound(w, r)
		return
	}
	ch, err := s.content.ChannelBySlug(r.Context(), e.ChannelSlug)
	if err != nil || ch.Status != "active" {
		http.NotFound(w, r)
		return
	}
	mediaRow, err := s.content.Store().MediaByID(r.Context(), a.MediaID)
	if err != nil || mediaRow.Status != "active" {
		http.NotFound(w, r)
		return
	}
	full, ok := media.Path(s.cfg.Storage.DocumentDir, a.StoredName)
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": a.OriginalName})
	if disposition == "" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", a.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Cache-Control", "private, max-age=0")
	s.content.Store().IncrementAttachmentDownload(r.Context(), id)
	http.ServeContent(w, r, a.OriginalName, st.ModTime(), f)
}

func (s *Server) commentPost(w http.ResponseWriter, r *http.Request) {
	channelSlug, key := r.PathValue("channel"), r.PathValue("key")
	e, err := s.content.PublicEntry(r.Context(), channelSlug, key)
	if err != nil {
		s.notFound(w, r)
		return
	}
	ch, err := s.content.ChannelBySlug(r.Context(), channelSlug)
	if err != nil || !ch.CommentsEnabled {
		http.Error(w, "comments are disabled", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	user := userFromContext(r.Context())
	if user == nil && !s.cfg.Auth.GuestComment {
		http.Error(w, "sign in to comment", http.StatusForbidden)
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	if body == "" || len([]rune(body)) > 5000 {
		http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, entryPath(*e))+"?comment=error", http.StatusSeeOther)
		return
	}
	name, email, userID := "", "", int64(0)
	if user != nil {
		userID = user.UserID
		name = user.DisplayName
		if name == "" {
			name = user.Username
		}
		email = user.Email
	} else {
		name = strings.TrimSpace(r.PostFormValue("author_name"))
		email = strings.TrimSpace(r.PostFormValue("author_email"))
		if name == "" || len([]rune(name)) > 100 {
			http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, entryPath(*e))+"?comment=error", http.StatusSeeOther)
			return
		}
	}
	ip := clientIPFromContext(r.Context())
	if !s.postLimiter.allow(ip) {
		http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, entryPath(*e))+"?comment=error", http.StatusSeeOther)
		return
	}
	_, err = s.content.CreateComment(r.Context(), e.ID, userID, name, email, body,
		ip, s.cfg.Content.CommentModeration)
	if err != nil {
		http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, entryPath(*e))+"?comment=error", http.StatusSeeOther)
		return
	}
	code := "ok"
	if s.cfg.Content.CommentModeration {
		code = "pending"
	}
	http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, entryPath(*e))+"?comment="+code, http.StatusSeeOther)
}

func entryComments(comments []content.Comment) []content.Comment {
	return comments
}
