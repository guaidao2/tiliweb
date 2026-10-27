package httpserver

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"tiliweb/internal/content"
	"tiliweb/internal/site"
)

func (s *Server) loadSettings(r *http.Request) site.Settings {
	settings, err := s.site.Load(r.Context(), s.cfg)
	if err != nil {
		return site.Defaults(s.cfg)
	}
	return settings
}

func (s *Server) absoluteURL(p string) string {
	base := strings.TrimRight(s.cfg.Server.BaseURL, "/")
	if p == "" {
		return base
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

func (s *Server) jsonLD(v any) template.HTML {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return template.HTML(b)
}

func entryPath(e content.Entry) string {
	key := e.Slug
	if key == "" {
		key = strconv.FormatInt(e.ID, 10)
	}
	return "/" + e.ChannelSlug + "/" + key
}

func (s *Server) entryLD(e content.Entry) template.HTML {
	typ := "Article"
	switch e.ChannelType {
	case "product":
		typ = "Product"
	case "case":
		typ = "CaseStudy"
	case "job":
		typ = "JobPosting"
	case "download":
		typ = "DigitalDocument"
	}
	url := s.absoluteURL(entryPath(e))
	m := map[string]any{
		"@context":         "https://schema.org",
		"@type":            typ,
		"headline":         e.Title,
		"name":             e.Title,
		"description":      e.Summary,
		"url":              url,
		"mainEntityOfPage": map[string]any{"@type": "WebPage", "@id": url},
	}
	if e.PublishedAt > 0 {
		m["datePublished"] = time.Unix(e.PublishedAt, 0).UTC().Format(time.RFC3339)
	}
	if e.AuthorName != "" {
		m["author"] = map[string]any{"@type": "Person", "name": e.AuthorName}
	}
	if e.CoverMediaID > 0 {
		m["image"] = s.absoluteURL(fmt.Sprintf("/media/%d/image", e.CoverMediaID))
	}
	return s.jsonLD(m)
}

func (s *Server) contactPage(w http.ResponseWriter, r *http.Request) {
	settings := s.loadSettings(r)
	data := s.pageData(r, "Contact", settings.SiteDescription)
	data.SiteSettings = settings
	data.ContactStarted = time.Now().Unix()
	if r.URL.Query().Get("sent") == "1" {
		data.Success = "感谢，你的留言已经收到。"
	}
	s.renderSite(w, r, "site/contact", data, http.StatusOK)
}

func (s *Server) contactPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	settings := s.loadSettings(r)
	renderError := func(msg string, status int) {
		data := s.pageData(r, "Contact", settings.SiteDescription)
		data.SiteSettings = settings
		data.Error = msg
		data.ContactStarted = time.Now().Unix()
		s.renderSite(w, r, "site/contact", data, status)
	}

	// Honeypot: pretend success without storing.
	if strings.TrimSpace(r.PostFormValue("website")) != "" {
		http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, "/contact")+"?sent=1", http.StatusSeeOther)
		return
	}
	started, _ := strconv.ParseInt(r.PostFormValue("started"), 10, 64)
	now := time.Now().Unix()
	if started == 0 || now-started < 2 || now-started > 86400 {
		renderError("Form expired. Please reload the page and try again.", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	message := strings.TrimSpace(r.PostFormValue("message"))
	if name == "" || message == "" {
		renderError("Name and message are required.", http.StatusBadRequest)
		return
	}
	if len([]rune(name)) > 100 || len([]rune(message)) > 5000 {
		renderError("Message is too long.", http.StatusBadRequest)
		return
	}
	for _, limit := range []struct {
		field string
		max   int
	}{
		{"phone", 50}, {"email", 254}, {"company", 120}, {"subject", 200},
	} {
		if len([]rune(r.PostFormValue(limit.field))) > limit.max {
			renderError("Message is too long.", http.StatusBadRequest)
			return
		}
	}
	ip := clientIPFromContext(r.Context())
	if !s.contactLimiter.allow(ip) {
		renderError("Too many messages. Please try again later.", http.StatusTooManyRequests)
		return
	}
	_, err := s.site.CreateContactMessage(r.Context(), &site.ContactMessage{
		Name: name, Phone: r.PostFormValue("phone"), Email: r.PostFormValue("email"),
		Company: r.PostFormValue("company"), Subject: r.PostFormValue("subject"),
		Message: message, SourcePage: joinURLPath(s.cfg.Server.BasePath, "/contact"),
		IP: ip, UserAgent: r.UserAgent(), Status: "new",
	})
	if err != nil {
		renderError("Could not save your message. Please try again later.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, joinURLPath(s.cfg.Server.BasePath, "/contact")+"?sent=1", http.StatusSeeOther)
}

func (s *Server) searchPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	typ := r.URL.Query().Get("type")
	if typ == "" {
		typ = "all"
	}
	switch typ {
	case "all", "article", "product", "case", "job", "download", "page", "forum":
	default:
		typ = "all"
	}
	var results []content.SearchResult
	var err error
	if q != "" {
		if typ != "forum" {
			results, err = s.content.Search(r.Context(), q, typ, 20, 0)
		}
		if typ == "all" || typ == "forum" {
			topics, ferr := s.forum.Search(r.Context(), q, 10)
			if ferr != nil && err == nil {
				err = ferr
			}
			for _, t := range topics {
				results = append(results, content.SearchResult{
					Type: "forum", ID: t.ID, Title: t.Title, Summary: t.Summary,
					URL:         "/forum/" + t.BoardSlug + "/" + strconv.FormatInt(t.ID, 10),
					ChannelName: t.BoardName, PublishedAt: t.CreatedAt,
				})
			}
		}
	}
	data := s.pageData(r, "Search", "")
	data.SearchQuery = q
	data.SearchType = typ
	data.SearchResults = results
	if err != nil {
		data.Error = "Search is temporarily unavailable."
	}
	s.renderSite(w, r, "site/search", data, http.StatusOK)
}

type rssFeedXML struct {
	XMLName xml.Name      `xml:"rss"`
	Version string        `xml:"version,attr"`
	Channel rssChannelXML `xml:"channel"`
}

type rssChannelXML struct {
	Title       string       `xml:"title"`
	Link        string       `xml:"link"`
	Description string       `xml:"description"`
	Language    string       `xml:"language,omitempty"`
	Items       []rssItemXML `xml:"item"`
}

type rssItemXML struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate,omitempty"`
	GUID        string `xml:"guid"`
}

func (s *Server) rssFeed(w http.ResponseWriter, r *http.Request) {
	settings := s.loadSettings(r)
	entries, _ := s.content.PublicLatestEntries(r.Context(), 20)
	base := s.absoluteURL(joinURLPath(s.cfg.Server.BasePath, "/"))
	feed := rssFeedXML{
		Version: "2.0",
		Channel: rssChannelXML{
			Title: settings.SiteName, Link: base, Description: settings.SiteDescription,
			Language: s.cfg.Site.Locale,
		},
	}
	for _, e := range entries {
		link := s.absoluteURL(entryPath(e))
		item := rssItemXML{Title: e.Title, Link: link, Description: e.Summary, GUID: link}
		if e.PublishedAt > 0 {
			item.PubDate = time.Unix(e.PublishedAt, 0).UTC().Format(time.RFC1123Z)
		}
		feed.Channel.Items = append(feed.Channel.Items, item)
	}
	body, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		http.Error(w, "feed error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=600")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

type sitemapURLSetXML struct {
	XMLName xml.Name        `xml:"urlset"`
	Xmlns   string          `xml:"xmlns,attr"`
	URLs    []sitemapURLXML `xml:"url"`
}

type sitemapURLXML struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

func sitemapDate(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format("2006-01-02")
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	channels, _ := s.content.ListChannels(r.Context(), false)
	entries, _ := s.content.PublicLatestEntries(r.Context(), 1000)
	pages, _ := s.content.PublicPages(r.Context())
	set := sitemapURLSetXML{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, ch := range channels {
		set.URLs = append(set.URLs, sitemapURLXML{
			Loc: s.absoluteURL(joinURLPath(s.cfg.Server.BasePath, "/"+ch.Slug)), LastMod: sitemapDate(ch.UpdatedAt),
		})
	}
	for _, e := range entries {
		set.URLs = append(set.URLs, sitemapURLXML{
			Loc: s.absoluteURL(joinURLPath(s.cfg.Server.BasePath, entryPath(e))), LastMod: sitemapDate(e.UpdatedAt),
		})
	}
	for _, p := range pages {
		set.URLs = append(set.URLs, sitemapURLXML{
			Loc: s.absoluteURL(joinURLPath(s.cfg.Server.BasePath, "/"+p.Slug)), LastMod: sitemapDate(p.UpdatedAt),
		})
	}
	body, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		http.Error(w, "sitemap error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=600")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

type contactHit struct {
	count int
	start time.Time
}

type contactLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	hits   map[string]contactHit
}

func newContactLimiter(max int) *contactLimiter {
	return newWindowLimiter(max, time.Hour)
}

func newWindowLimiter(max int, window time.Duration) *contactLimiter {
	return &contactLimiter{max: max, window: window, now: time.Now, hits: map[string]contactHit{}}
}

func (l *contactLimiter) allow(key string) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	if key == "" {
		key = "unknown"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.hits) > 1024 {
		for k, h := range l.hits {
			if now.Sub(h.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	h := l.hits[key]
	if h.start.IsZero() || now.Sub(h.start) >= l.window {
		h = contactHit{start: now}
	}
	if h.count >= l.max {
		return false
	}
	h.count++
	l.hits[key] = h
	return true
}
