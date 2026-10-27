package httpserver

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"tiliweb/internal/content"
)

func (s *Server) requireEditor(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		role := adminFromContext(r.Context()).Role
		if role != "superadmin" && role != "editor" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (s *Server) adminChannels(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Channels", "")
	data.Channels, _ = s.content.ListChannels(r.Context(), true)
	s.renderAdmin(w, r, "admin/channels", data, http.StatusOK)
}

func (s *Server) adminChannelNewPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "New channel", "")
	data.Channel = &content.Channel{Type: "article", Status: "active", ShowInNav: true}
	s.renderAdmin(w, r, "admin/channel_form", data, http.StatusOK)
}

func (s *Server) adminChannelNewPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	ch := channelFromForm(r)
	if _, err := s.content.CreateChannel(r.Context(), ch); err != nil {
		data := s.pageData(r, "New channel", "")
		data.Error = err.Error()
		data.Channel = ch
		s.renderAdmin(w, r, "admin/channel_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("channels"), http.StatusSeeOther)
}

func (s *Server) adminChannelEditPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad channel id", http.StatusBadRequest)
		return
	}
	ch, err := s.content.ChannelByID(r.Context(), id)
	if err != nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	data := s.pageData(r, "Edit channel", "")
	data.Channel = ch
	s.renderAdmin(w, r, "admin/channel_form", data, http.StatusOK)
}

func (s *Server) adminChannelEditPost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad channel id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	ch := channelFromForm(r)
	ch.ID = id
	if _, err := s.content.UpdateChannel(r.Context(), ch); err != nil {
		data := s.pageData(r, "Edit channel", "")
		data.Error = err.Error()
		data.Channel = ch
		s.renderAdmin(w, r, "admin/channel_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("channels"), http.StatusSeeOther)
}

func (s *Server) adminChannelDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad channel id", http.StatusBadRequest)
		return
	}
	if err := s.content.DeleteChannel(r.Context(), id); err != nil {
		data := s.pageData(r, "Channels", "")
		data.Error = err.Error()
		data.Channels, _ = s.content.ListChannels(r.Context(), true)
		s.renderAdmin(w, r, "admin/channels", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("channels"), http.StatusSeeOther)
}

func (s *Server) adminEntries(w http.ResponseWriter, r *http.Request) {
	channelID, _ := strconv.ParseInt(r.URL.Query().Get("channel"), 10, 64)
	status := r.URL.Query().Get("status")
	includeDeleted := r.URL.Query().Get("deleted") == "1"
	entries, err := s.content.ListEntries(r.Context(), channelID, status, includeDeleted, 100, 0)
	data := s.pageData(r, "Entries", "")
	if err != nil {
		data.Error = err.Error()
	}
	data.Entries = entries
	data.Channels, _ = s.content.ListChannels(r.Context(), true)
	data.FilterChannelID = channelID
	data.FilterStatus = status
	data.IncludeDeleted = includeDeleted
	s.renderAdmin(w, r, "admin/entries", data, http.StatusOK)
}

func (s *Server) adminEntryNewPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "New entry", "")
	data.Entry = &content.Entry{Status: "draft"}
	data.Channels, _ = s.content.ListChannels(r.Context(), true)
	s.renderAdmin(w, r, "admin/entry_form", data, http.StatusOK)
}

func (s *Server) adminEntryNewPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := entryFromForm(r)
	if _, err := s.content.CreateEntry(r.Context(), adminFromContext(r.Context()).AdminID, in); err != nil {
		data := s.pageData(r, "New entry", "")
		data.Error = err.Error()
		data.Entry = entryFromInput(in)
		data.Channels, _ = s.content.ListChannels(r.Context(), true)
		s.renderAdmin(w, r, "admin/entry_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("entries"), http.StatusSeeOther)
}

func (s *Server) adminEntryEditPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad entry id", http.StatusBadRequest)
		return
	}
	e, err := s.content.GetEntry(r.Context(), id)
	if err != nil {
		http.Error(w, "entry not found", http.StatusNotFound)
		return
	}
	data := s.pageData(r, "Edit entry", "")
	data.Entry = e
	data.Channels, _ = s.content.ListChannels(r.Context(), true)
	data.Attachments, _ = s.content.Attachments(r.Context(), id)
	s.renderAdmin(w, r, "admin/entry_form", data, http.StatusOK)
}

func (s *Server) adminEntryEditPost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad entry id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := entryFromForm(r)
	if _, err := s.content.UpdateEntry(r.Context(), adminFromContext(r.Context()).AdminID, id, in); err != nil {
		data := s.pageData(r, "Edit entry", "")
		data.Error = err.Error()
		data.Entry = entryFromInput(in)
		data.Entry.ID = id
		data.Channels, _ = s.content.ListChannels(r.Context(), true)
		data.Attachments, _ = s.content.Attachments(r.Context(), id)
		s.renderAdmin(w, r, "admin/entry_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("entries"), http.StatusSeeOther)
}

func (s *Server) adminEntryStatus(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad entry id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := s.content.SetEntryStatus(r.Context(), adminFromContext(r.Context()).AdminID, id, r.PostFormValue("status")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("entries"), http.StatusSeeOther)
}

func (s *Server) adminEntryDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad entry id", http.StatusBadRequest)
		return
	}
	if err := s.content.DeleteEntry(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("entries"), http.StatusSeeOther)
}

func (s *Server) adminEntryRestore(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad entry id", http.StatusBadRequest)
		return
	}
	if err := s.content.RestoreEntry(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("entries")+"?deleted=1", http.StatusSeeOther)
}

func (s *Server) adminEntryRevisions(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad entry id", http.StatusBadRequest)
		return
	}
	e, err := s.content.GetEntry(r.Context(), id)
	if err != nil {
		http.Error(w, "entry not found", http.StatusNotFound)
		return
	}
	data := s.pageData(r, "Entry revisions", "")
	data.Entry = e
	data.Revisions, _ = s.content.Revisions(r.Context(), "entry", id)
	data.RevisionTargetType = "entry"
	data.RevisionTargetID = id
	s.renderAdmin(w, r, "admin/revisions", data, http.StatusOK)
}

func (s *Server) adminPages(w http.ResponseWriter, r *http.Request) {
	pages, err := s.content.ListPages(r.Context(), false)
	data := s.pageData(r, "Pages", "")
	if err != nil {
		data.Error = err.Error()
	}
	data.Pages = pages
	s.renderAdmin(w, r, "admin/pages", data, http.StatusOK)
}

func (s *Server) adminPageNewPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "New page", "")
	data.Page = &content.Page{Template: "page", Status: "published", ShowInNav: true}
	s.renderAdmin(w, r, "admin/page_form", data, http.StatusOK)
}

func (s *Server) adminPageNewPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := pageFromForm(r)
	if _, err := s.content.CreatePage(r.Context(), adminFromContext(r.Context()).AdminID, in); err != nil {
		data := s.pageData(r, "New page", "")
		data.Error = err.Error()
		data.Page = pageFromInput(in)
		s.renderAdmin(w, r, "admin/page_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("pages"), http.StatusSeeOther)
}

func (s *Server) adminPageEditPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad page id", http.StatusBadRequest)
		return
	}
	p, err := s.content.PageByID(r.Context(), id)
	if err != nil {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}
	data := s.pageData(r, "Edit page", "")
	data.Page = p
	s.renderAdmin(w, r, "admin/page_form", data, http.StatusOK)
}

func (s *Server) adminPageEditPost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad page id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := pageFromForm(r)
	if _, err := s.content.UpdatePage(r.Context(), adminFromContext(r.Context()).AdminID, id, in); err != nil {
		data := s.pageData(r, "Edit page", "")
		data.Error = err.Error()
		data.Page = pageFromInput(in)
		data.Page.ID = id
		s.renderAdmin(w, r, "admin/page_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("pages"), http.StatusSeeOther)
}

func (s *Server) adminPageDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad page id", http.StatusBadRequest)
		return
	}
	if err := s.content.DeletePage(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("pages"), http.StatusSeeOther)
}

func (s *Server) adminPageRevisions(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad page id", http.StatusBadRequest)
		return
	}
	p, err := s.content.PageByID(r.Context(), id)
	if err != nil {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}
	data := s.pageData(r, "Page revisions", "")
	data.Page = p
	data.Revisions, _ = s.content.Revisions(r.Context(), "page", id)
	data.RevisionTargetType = "page"
	data.RevisionTargetID = id
	s.renderAdmin(w, r, "admin/revisions", data, http.StatusOK)
}

func (s *Server) adminRevisionRestore(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad revision id", http.StatusBadRequest)
		return
	}
	targetType, targetID, err := s.content.RestoreRevision(r.Context(), adminFromContext(r.Context()).AdminID, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch targetType {
	case "page":
		http.Redirect(w, r, s.adminURL("pages")+"/"+strconv.FormatInt(targetID, 10)+"/edit", http.StatusSeeOther)
	default:
		http.Redirect(w, r, s.adminURL("entries")+"/"+strconv.FormatInt(targetID, 10)+"/edit", http.StatusSeeOther)
	}
}

func (s *Server) adminTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.content.ListTags(r.Context())
	data := s.pageData(r, "Tags", "")
	if err != nil {
		data.Error = err.Error()
	}
	data.Tags = tags
	s.renderAdmin(w, r, "admin/tags", data, http.StatusOK)
}

func channelFromForm(r *http.Request) *content.Channel {
	return &content.Channel{
		Slug:            r.PostFormValue("slug"),
		Name:            r.PostFormValue("name"),
		Type:            r.PostFormValue("type"),
		Description:     r.PostFormValue("description"),
		ShowInNav:       formBool(r, "show_in_nav"),
		CommentsEnabled: formBool(r, "comments_enabled"),
		SortOrder:       formInt(r, "sort_order"),
		Status:          r.PostFormValue("status"),
		SEOTitle:        r.PostFormValue("seo_title"),
		SEODescription:  r.PostFormValue("seo_description"),
	}
}

func entryFromForm(r *http.Request) content.EntryInput {
	extra := map[string]any{}
	if raw := strings.TrimSpace(r.PostFormValue("extra_json")); raw != "" {
		_ = json.Unmarshal([]byte(raw), &extra)
	}
	for _, key := range []string{"model", "price_text", "client", "industry", "results",
		"department", "location", "headcount", "salary_range", "contact_email", "version", "file_note"} {
		if v := strings.TrimSpace(r.PostFormValue("extra_" + key)); v != "" {
			extra[key] = v
		}
	}
	raw, _ := json.Marshal(extra)
	return content.EntryInput{
		ChannelID:    formInt64(r, "channel_id"),
		Slug:         r.PostFormValue("slug"),
		Title:        r.PostFormValue("title"),
		Summary:      r.PostFormValue("summary"),
		BodyMD:       r.PostFormValue("body_md"),
		CoverMediaID: formInt64(r, "cover_media_id"),
		Status:       r.PostFormValue("status"),
		Pinned:       formBool(r, "pinned"),
		Featured:     formBool(r, "featured"),
		SortOrder:    formInt(r, "sort_order"),
		ExtraJSON:    string(raw),
		Tags:         splitTags(r.PostFormValue("tags")),
	}
}

func entryFromInput(in content.EntryInput) *content.Entry {
	extra := map[string]any{}
	if in.ExtraJSON != "" {
		_ = json.Unmarshal([]byte(in.ExtraJSON), &extra)
	}
	return &content.Entry{
		ChannelID:    in.ChannelID,
		Slug:         content.Slugify(in.Slug),
		Title:        in.Title,
		Summary:      in.Summary,
		BodyMD:       in.BodyMD,
		CoverMediaID: in.CoverMediaID,
		Status:       in.Status,
		Pinned:       in.Pinned,
		Featured:     in.Featured,
		SortOrder:    in.SortOrder,
		ExtraJSON:    in.ExtraJSON,
		Extra:        extra,
	}
}

func pageFromForm(r *http.Request) content.PageInput {
	return content.PageInput{
		Slug:           r.PostFormValue("slug"),
		Title:          r.PostFormValue("title"),
		BodyMD:         r.PostFormValue("body_md"),
		Template:       r.PostFormValue("template"),
		ShowInNav:      formBool(r, "show_in_nav"),
		SortOrder:      formInt(r, "sort_order"),
		Status:         r.PostFormValue("status"),
		SEOTitle:       r.PostFormValue("seo_title"),
		SEODescription: r.PostFormValue("seo_description"),
	}
}

func pageFromInput(in content.PageInput) *content.Page {
	return &content.Page{
		Slug:           content.Slugify(in.Slug),
		Title:          in.Title,
		BodyMD:         in.BodyMD,
		Template:       in.Template,
		ShowInNav:      in.ShowInNav,
		SortOrder:      in.SortOrder,
		Status:         in.Status,
		SEOTitle:       in.SEOTitle,
		SEODescription: in.SEODescription,
	}
}

func formBool(r *http.Request, name string) bool {
	switch r.PostFormValue(name) {
	case "1", "on", "true", "yes":
		return true
	default:
		return false
	}
}

func formInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.PostFormValue(name))
	return n
}

func formInt64(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PostFormValue(name), 10, 64)
	return n
}

func splitTags(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
