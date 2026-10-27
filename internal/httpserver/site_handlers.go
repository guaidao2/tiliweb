package httpserver

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) fallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.notFound(w, r)
		return
	}
	if rd, err := s.content.RedirectForPath(r.Context(), r.URL.Path); err == nil {
		http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, rd.ToPath), rd.StatusCode)
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	switch len(parts) {
	case 1:
		s.serveChannelOrPage(w, r, parts[0])
	case 2:
		s.serveEntry(w, r, parts[0], parts[1])
	default:
		s.notFound(w, r)
	}
}

func (s *Server) serveChannelOrPage(w http.ResponseWriter, r *http.Request, slug string) {
	if ch, err := s.content.ChannelBySlug(r.Context(), slug); err == nil && ch.Status == "active" {
		entries, _ := s.content.PublicEntries(r.Context(), ch.Slug, 50, 0)
		title := ch.SEOTitle
		if title == "" {
			title = ch.Name
		}
		desc := ch.SEODescription
		if desc == "" {
			desc = ch.Description
		}
		data := s.pageData(r, title, desc)
		data.Channel = ch
		data.Entries = entries
		data.JSONLD = s.jsonLD(map[string]any{
			"@context": "https://schema.org", "@type": "CollectionPage",
			"name": ch.Name, "description": desc,
			"url": s.absoluteURL(joinURLPath(s.cfg.Server.BasePath, "/"+ch.Slug)),
		})
		s.renderSite(w, r, "site/channel", data, http.StatusOK)
		return
	}
	if p, err := s.content.PageBySlug(r.Context(), slug); err == nil {
		title := p.SEOTitle
		if title == "" {
			title = p.Title
		}
		desc := p.SEODescription
		data := s.pageData(r, title, desc)
		data.Page = p
		data.JSONLD = s.jsonLD(map[string]any{
			"@context": "https://schema.org", "@type": "WebPage",
			"name": p.Title, "description": desc,
			"url": s.absoluteURL(joinURLPath(s.cfg.Server.BasePath, "/"+p.Slug)),
		})
		s.renderSite(w, r, "site/page", data, http.StatusOK)
		return
	}
	s.notFound(w, r)
}

func (s *Server) serveEntry(w http.ResponseWriter, r *http.Request, channelSlug, key string) {
	e, err := s.content.PublicEntry(r.Context(), channelSlug, key)
	if err != nil {
		s.notFound(w, r)
		return
	}
	s.content.IncrementEntryView(r.Context(), e.ID)
	title := e.Title
	data := s.pageData(r, title, e.Summary)
	data.Entry = e
	data.OGType = "article"
	if e.ChannelType == "product" {
		data.OGType = "product"
	}
	if e.CoverMediaID > 0 {
		data.OGImage = s.absoluteURL(fmt.Sprintf("/media/%d/image", e.CoverMediaID))
	}
	data.Attachments, _ = s.content.Attachments(r.Context(), e.ID)
	if ch, err := s.content.ChannelBySlug(r.Context(), e.ChannelSlug); err == nil {
		data.CommentsEnabled = ch.CommentsEnabled
		if ch.CommentsEnabled {
			data.Comments, _ = s.content.ApprovedComments(r.Context(), e.ID)
		}
	}
	switch r.URL.Query().Get("comment") {
	case "ok":
		data.Success = "评论已发布。"
	case "pending":
		data.Success = "评论已提交，等待审核。"
	case "error":
		data.Error = "评论提交失败。"
	}
	data.JSONLD = s.entryLD(*e)
	s.renderSite(w, r, "site/entry", data, http.StatusOK)
}

func (s *Server) siteTag(w http.ResponseWriter, r *http.Request) {
	tag, err := s.content.TagBySlug(r.Context(), r.PathValue("tag"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	entries, _ := s.content.PublicEntriesByTag(r.Context(), tag.Slug, 50, 0)
	data := s.pageData(r, tag.Name, "Tag: "+tag.Name)
	data.Tag = tag
	data.Entries = entries
	s.renderSite(w, r, "site/tag", data, http.StatusOK)
}
