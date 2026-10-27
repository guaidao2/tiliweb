package httpserver

import (
	"net/http"
	"strconv"
	"strings"

	"tiliweb/internal/content"
	"tiliweb/internal/media"
)

func (s *Server) requireModerator(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		role := adminFromContext(r.Context()).Role
		if role != "superadmin" && role != "editor" && role != "moderator" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (s *Server) adminEntryAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	entryID, err := pathID(r)
	if err != nil {
		http.Error(w, "bad entry id", http.StatusBadRequest)
		return
	}
	max := s.cfg.Uploads.MaxDocumentBytes + (1 << 20)
	if err := r.ParseMultipartForm(max); err != nil {
		http.Error(w, "bad upload", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	title := strings.TrimSpace(r.PostFormValue("title"))
	if title == "" {
		title = header.Filename
	}
	admin := adminFromContext(r.Context())
	m, err := media.SaveDocument(r.Context(), s.cfg, s.content.Store(), file, header.Filename, "admin", admin.AdminID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.content.CreateAttachment(r.Context(), entryID, m.ID, title, formInt(r, "sort_order")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("entries")+"/"+strconv.FormatInt(entryID, 10)+"/edit", http.StatusSeeOther)
}

func (s *Server) adminAttachmentDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad attachment id", http.StatusBadRequest)
		return
	}
	a, err := s.content.AttachmentByID(r.Context(), id)
	if err != nil {
		http.Error(w, "attachment not found", http.StatusNotFound)
		return
	}
	if err := s.content.DeleteAttachment(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("entries")+"/"+strconv.FormatInt(a.EntryID, 10)+"/edit", http.StatusSeeOther)
}

func (s *Server) adminComments(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	comments, err := s.content.ListComments(r.Context(), status, 200)
	data := s.pageData(r, "Comments", "")
	if err != nil {
		data.Error = err.Error()
	}
	data.Comments = comments
	data.CommentFilter = status
	data.PendingComments, _ = s.content.PendingComments(r.Context())
	s.renderAdmin(w, r, "admin/comments", data, http.StatusOK)
}

func (s *Server) adminCommentStatus(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad comment id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := s.content.SetCommentStatus(r.Context(), id, r.PostFormValue("status")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("comments"), http.StatusSeeOther)
}

func (s *Server) adminLinks(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Friend links", "")
	data.FriendLinks, _ = s.content.ListFriendLinks(r.Context(), false)
	s.renderAdmin(w, r, "admin/links", data, http.StatusOK)
}

func (s *Server) adminLinkNewPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "New friend link", "")
	data.FriendLink = &content.FriendLink{Enabled: true}
	s.renderAdmin(w, r, "admin/link_form", data, http.StatusOK)
}

func (s *Server) adminLinkNewPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := friendLinkFromForm(r)
	if _, err := s.content.CreateFriendLink(r.Context(), in); err != nil {
		data := s.pageData(r, "New friend link", "")
		data.Error = err.Error()
		data.FriendLink = &in
		s.renderAdmin(w, r, "admin/link_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("links"), http.StatusSeeOther)
}

func (s *Server) adminLinkEditPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad link id", http.StatusBadRequest)
		return
	}
	l, err := s.content.FriendLinkByID(r.Context(), id)
	if err != nil {
		http.Error(w, "link not found", http.StatusNotFound)
		return
	}
	data := s.pageData(r, "Edit friend link", "")
	data.FriendLink = l
	s.renderAdmin(w, r, "admin/link_form", data, http.StatusOK)
}

func (s *Server) adminLinkEditPost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad link id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := friendLinkFromForm(r)
	in.ID = id
	if _, err := s.content.UpdateFriendLink(r.Context(), in); err != nil {
		data := s.pageData(r, "Edit friend link", "")
		data.Error = err.Error()
		data.FriendLink = &in
		s.renderAdmin(w, r, "admin/link_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("links"), http.StatusSeeOther)
}

func (s *Server) adminLinkDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad link id", http.StatusBadRequest)
		return
	}
	if err := s.content.DeleteFriendLink(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("links"), http.StatusSeeOther)
}

func (s *Server) adminRedirects(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Redirects", "")
	data.Redirects, _ = s.content.ListRedirects(r.Context())
	s.renderAdmin(w, r, "admin/redirects", data, http.StatusOK)
}

func (s *Server) adminRedirectNewPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "New redirect", "")
	data.Redirect = &content.Redirect{StatusCode: 301}
	s.renderAdmin(w, r, "admin/redirect_form", data, http.StatusOK)
}

func (s *Server) adminRedirectNewPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := redirectFromForm(r)
	if _, err := s.content.CreateRedirect(r.Context(), in); err != nil {
		data := s.pageData(r, "New redirect", "")
		data.Error = err.Error()
		data.Redirect = &in
		s.renderAdmin(w, r, "admin/redirect_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("redirects"), http.StatusSeeOther)
}

func (s *Server) adminRedirectEditPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad redirect id", http.StatusBadRequest)
		return
	}
	rd, err := s.content.RedirectByID(r.Context(), id)
	if err != nil {
		http.Error(w, "redirect not found", http.StatusNotFound)
		return
	}
	data := s.pageData(r, "Edit redirect", "")
	data.Redirect = rd
	s.renderAdmin(w, r, "admin/redirect_form", data, http.StatusOK)
}

func (s *Server) adminRedirectEditPost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad redirect id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := redirectFromForm(r)
	in.ID = id
	if _, err := s.content.UpdateRedirect(r.Context(), in); err != nil {
		data := s.pageData(r, "Edit redirect", "")
		data.Error = err.Error()
		data.Redirect = &in
		s.renderAdmin(w, r, "admin/redirect_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("redirects"), http.StatusSeeOther)
}

func (s *Server) adminRedirectDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad redirect id", http.StatusBadRequest)
		return
	}
	if err := s.content.DeleteRedirect(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("redirects"), http.StatusSeeOther)
}

func friendLinkFromForm(r *http.Request) content.FriendLink {
	return content.FriendLink{
		Name: r.PostFormValue("name"), URL: r.PostFormValue("url"),
		LogoMediaID: formInt64(r, "logo_media_id"), SortOrder: formInt(r, "sort_order"),
		Enabled: formBool(r, "enabled"),
	}
}

func redirectFromForm(r *http.Request) content.Redirect {
	return content.Redirect{
		FromPath: r.PostFormValue("from_path"), ToPath: r.PostFormValue("to_path"),
		StatusCode: formInt(r, "status_code"),
	}
}
