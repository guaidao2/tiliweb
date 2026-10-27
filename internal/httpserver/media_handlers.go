package httpserver

import (
	"net/http"

	"tiliweb/internal/media"
)

func (s *Server) adminMedia(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Media", "")
	data.Media, _ = s.content.Store().ListMedia(r.Context(), 100)
	s.renderAdmin(w, r, "admin/media", data, http.StatusOK)
}

func (s *Server) adminMediaUpload(w http.ResponseWriter, r *http.Request) {
	max := s.cfg.Uploads.MaxImageBytes + (1 << 20)
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
	admin := adminFromContext(r.Context())
	if _, err := media.SaveImage(r.Context(), s.cfg, s.content.Store(), file, header.Filename, "admin", admin.AdminID); err != nil {
		data := s.pageData(r, "Media", "")
		data.Error = err.Error()
		data.Media, _ = s.content.Store().ListMedia(r.Context(), 100)
		s.renderAdmin(w, r, "admin/media", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("media"), http.StatusSeeOther)
}

func (s *Server) adminMediaDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad media id", http.StatusBadRequest)
		return
	}
	if err := s.content.Store().DeleteMedia(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("media"), http.StatusSeeOther)
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	m, err := s.content.Store().MediaByID(r.Context(), id)
	if err != nil || m.Status != "active" || m.Kind != "image" {
		http.NotFound(w, r)
		return
	}
	full, ok := media.Path(s.cfg.Storage.MediaDir, m.StoredName)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, full)
}
