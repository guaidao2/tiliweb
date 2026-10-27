package httpserver

import (
	"net/http"
	"strings"

	"tiliweb/internal/forum"
)

func (s *Server) adminForumBoards(w http.ResponseWriter, r *http.Request) {
	boards, err := s.forum.ListBoards(r.Context(), true)
	data := s.pageData(r, "Forum boards", "")
	if err != nil {
		data.Error = err.Error()
	}
	data.AdminBoards = boards
	s.renderAdmin(w, r, "admin/forum_boards", data, http.StatusOK)
}

func (s *Server) adminForumBoardNewPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "New board", "")
	data.Board = &forum.Board{Visibility: "public", Status: "active"}
	s.renderAdmin(w, r, "admin/forum_board_form", data, http.StatusOK)
}

func (s *Server) adminForumBoardNewPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	b := boardFromForm(r)
	if _, err := s.forum.CreateBoard(r.Context(), b); err != nil {
		data := s.pageData(r, "New board", "")
		data.Error = err.Error()
		data.Board = b
		s.renderAdmin(w, r, "admin/forum_board_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("forum")+"/boards", http.StatusSeeOther)
}

func (s *Server) adminForumBoardEditPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad board id", http.StatusBadRequest)
		return
	}
	b, err := s.forum.BoardByID(r.Context(), id)
	if err != nil {
		http.Error(w, "board not found", http.StatusNotFound)
		return
	}
	data := s.pageData(r, "Edit board", "")
	data.Board = b
	data.ForumModerators, _ = s.forum.BoardModerators(r.Context(), b.ID)
	s.renderAdmin(w, r, "admin/forum_board_form", data, http.StatusOK)
}

func (s *Server) adminForumBoardEditPost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad board id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	b := boardFromForm(r)
	b.ID = id
	if _, err := s.forum.UpdateBoard(r.Context(), b); err != nil {
		data := s.pageData(r, "Edit board", "")
		data.Error = err.Error()
		data.Board = b
		s.renderAdmin(w, r, "admin/forum_board_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("forum")+"/boards", http.StatusSeeOther)
}

func (s *Server) adminForumBoardDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad board id", http.StatusBadRequest)
		return
	}
	if err := s.forum.DeleteBoard(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("forum")+"/boards", http.StatusSeeOther)
}

func (s *Server) adminForumModerators(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad board id", http.StatusBadRequest)
		return
	}
	b, err := s.forum.BoardByID(r.Context(), id)
	if err != nil {
		http.Error(w, "board not found", http.StatusNotFound)
		return
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		userID := formInt64(r, "user_id")
		switch r.PostFormValue("action") {
		case "add":
			if err := s.forum.AddModerator(r.Context(), b.ID, userID, adminFromContext(r.Context()).AdminID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		case "remove":
			if err := s.forum.RemoveModerator(r.Context(), b.ID, userID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, "invalid action", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, s.adminURL("forum")+"/boards/"+formatID(b.ID)+"/moderators", http.StatusSeeOther)
		return
	}
	data := s.pageData(r, "Board moderators", "")
	data.Board = b
	data.ForumModerators, _ = s.forum.BoardModerators(r.Context(), b.ID)
	data.Users, _ = s.auth.ListUsers(r.Context())
	s.renderAdmin(w, r, "admin/forum_moderators", data, http.StatusOK)
}

func (s *Server) adminForumTopics(w http.ResponseWriter, r *http.Request) {
	topics, err := s.forum.ListAllTopics(r.Context(), 200)
	data := s.pageData(r, "Forum topics", "")
	if err != nil {
		data.Error = err.Error()
	}
	data.Topics = topics
	s.renderAdmin(w, r, "admin/forum_topics", data, http.StatusOK)
}

func (s *Server) adminForumTopicModerate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad topic id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := s.forum.ModerateTopic(r.Context(), id, r.PostFormValue("action")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("forum")+"/topics", http.StatusSeeOther)
}

func (s *Server) adminForumPostModerate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad post id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := s.forum.ModeratePost(r.Context(), id, r.PostFormValue("action")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("forum")+"/topics", http.StatusSeeOther)
}

func boardFromForm(r *http.Request) *forum.Board {
	return &forum.Board{
		ParentID:    formInt64(r, "parent_id"),
		Slug:        strings.TrimSpace(r.PostFormValue("slug")),
		Name:        strings.TrimSpace(r.PostFormValue("name")),
		Description: r.PostFormValue("description"),
		Visibility:  r.PostFormValue("visibility"),
		SortOrder:   formInt(r, "sort_order"),
		Status:      r.PostFormValue("status"),
	}
}
