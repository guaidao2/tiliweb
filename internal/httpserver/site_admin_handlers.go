package httpserver

import (
	"net/http"
	"strings"

	"tiliweb/internal/content"
	"tiliweb/internal/site"
)

func (s *Server) adminBanners(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Banners", "")
	data.Banners, _ = s.content.ListBanners(r.Context(), false)
	s.renderAdmin(w, r, "admin/banners", data, http.StatusOK)
}

func (s *Server) adminBannerNewPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "New banner", "")
	data.Banner = &content.Banner{Enabled: true}
	s.renderAdmin(w, r, "admin/banner_form", data, http.StatusOK)
}

func (s *Server) adminBannerNewPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := bannerFromForm(r)
	if _, err := s.content.CreateBanner(r.Context(), in); err != nil {
		data := s.pageData(r, "New banner", "")
		data.Error = err.Error()
		data.Banner = bannerFromInput(in)
		s.renderAdmin(w, r, "admin/banner_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("banners"), http.StatusSeeOther)
}

func (s *Server) adminBannerEditPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad banner id", http.StatusBadRequest)
		return
	}
	b, err := s.content.BannerByID(r.Context(), id)
	if err != nil {
		http.Error(w, "banner not found", http.StatusNotFound)
		return
	}
	data := s.pageData(r, "Edit banner", "")
	data.Banner = b
	s.renderAdmin(w, r, "admin/banner_form", data, http.StatusOK)
}

func (s *Server) adminBannerEditPost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad banner id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := bannerFromForm(r)
	if _, err := s.content.UpdateBanner(r.Context(), id, in); err != nil {
		data := s.pageData(r, "Edit banner", "")
		data.Error = err.Error()
		data.Banner = bannerFromInput(in)
		data.Banner.ID = id
		s.renderAdmin(w, r, "admin/banner_form", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("banners"), http.StatusSeeOther)
}

func (s *Server) adminBannerDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad banner id", http.StatusBadRequest)
		return
	}
	if err := s.content.DeleteBanner(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("banners"), http.StatusSeeOther)
}

func (s *Server) adminHomeModules(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Home modules", "")
	settings, err := s.site.Load(r.Context(), s.cfg)
	if err != nil {
		data.Error = err.Error()
	}
	data.SiteSettings = settings
	data.HomeSettings = settings.Home
	if r.URL.Query().Get("saved") == "1" {
		data.Success = "首页模块已保存。"
	}
	s.renderAdmin(w, r, "admin/home", data, http.StatusOK)
}

func (s *Server) adminHomePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	settings, err := s.site.Load(r.Context(), s.cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	settings.Home = site.Home{
		ShowBanners:   formBool(r, "show_banners"),
		LatestCount:   clamp(formInt(r, "latest_count"), 1, 50),
		FeaturedCount: clamp(formInt(r, "featured_count"), 1, 50),
		CTATitle:      strings.TrimSpace(r.PostFormValue("cta_title")),
		CTAText:       strings.TrimSpace(r.PostFormValue("cta_text")),
		CTAURL:        strings.TrimSpace(r.PostFormValue("cta_url")),
	}
	if err := s.site.Save(r.Context(), settings); err != nil {
		data := s.pageData(r, "Home modules", "")
		data.Error = err.Error()
		data.SiteSettings = settings
		data.HomeSettings = settings.Home
		s.renderAdmin(w, r, "admin/home", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("home")+"?saved=1", http.StatusSeeOther)
}

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Settings", "")
	settings, err := s.site.Load(r.Context(), s.cfg)
	if err != nil {
		data.Error = err.Error()
	}
	data.SiteSettings = settings
	if r.URL.Query().Get("saved") == "1" {
		data.Success = "站点设置已保存。"
	}
	s.renderAdmin(w, r, "admin/settings", data, http.StatusOK)
}

func (s *Server) adminSettingsPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	settings, err := s.site.Load(r.Context(), s.cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	settings.SiteName = strings.TrimSpace(r.PostFormValue("site_name"))
	settings.SiteDescription = strings.TrimSpace(r.PostFormValue("site_description"))
	settings.Contact.Phone = strings.TrimSpace(r.PostFormValue("contact_phone"))
	settings.Contact.Email = strings.TrimSpace(r.PostFormValue("contact_email"))
	settings.Contact.Address = strings.TrimSpace(r.PostFormValue("contact_address"))
	settings.Contact.MapURL = strings.TrimSpace(r.PostFormValue("contact_map_url"))
	settings.Contact.WeChatQRMediaID = formInt64(r, "contact_wechat_qr_media_id")
	settings.SEO.DefaultTitle = strings.TrimSpace(r.PostFormValue("seo_default_title"))
	settings.SEO.DefaultDescription = strings.TrimSpace(r.PostFormValue("seo_default_description"))
	settings.SEO.OGImageMediaID = formInt64(r, "seo_og_image_media_id")
	settings.SEO.RobotsExtra = strings.TrimSpace(r.PostFormValue("seo_robots_extra"))
	settings.ContactPageMD = r.PostFormValue("contact_page_md")
	if err := s.site.Save(r.Context(), settings); err != nil {
		data := s.pageData(r, "Settings", "")
		data.Error = err.Error()
		data.SiteSettings = settings
		s.renderAdmin(w, r, "admin/settings", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("settings")+"?saved=1", http.StatusSeeOther)
}

func (s *Server) adminMessages(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	messages, err := s.site.ListContactMessages(r.Context(), status, 100)
	data := s.pageData(r, "Messages", "")
	if err != nil {
		data.Error = err.Error()
	}
	data.Messages = messages
	data.MessageFilter = status
	data.NewMessages, _ = s.site.CountNewContactMessages(r.Context())
	s.renderAdmin(w, r, "admin/messages", data, http.StatusOK)
}

func (s *Server) adminMessageStatus(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad message id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := s.site.UpdateContactMessage(r.Context(), id, r.PostFormValue("status"), r.PostFormValue("note")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("messages"), http.StatusSeeOther)
}

func (s *Server) adminMessageNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad message id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	m, err := s.site.ContactMessageByID(r.Context(), id)
	if err != nil {
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}
	if err := s.site.UpdateContactMessage(r.Context(), id, m.Status, r.PostFormValue("note")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("messages"), http.StatusSeeOther)
}

func bannerFromForm(r *http.Request) content.BannerInput {
	return content.BannerInput{
		Title:        r.PostFormValue("title"),
		Subtitle:     r.PostFormValue("subtitle"),
		ImageMediaID: formInt64(r, "image_media_id"),
		LinkURL:      r.PostFormValue("link_url"),
		ButtonText:   r.PostFormValue("button_text"),
		SortOrder:    formInt(r, "sort_order"),
		Enabled:      formBool(r, "enabled"),
	}
}

func bannerFromInput(in content.BannerInput) *content.Banner {
	return &content.Banner{
		Title: in.Title, Subtitle: in.Subtitle, ImageMediaID: in.ImageMediaID,
		LinkURL: in.LinkURL, ButtonText: in.ButtonText, SortOrder: in.SortOrder, Enabled: in.Enabled,
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
