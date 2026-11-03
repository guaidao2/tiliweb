package httpserver

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"tiliweb/internal/auth"
	"tiliweb/internal/config"
	"tiliweb/internal/content"
	"tiliweb/internal/forum"
	"tiliweb/internal/security"
	"tiliweb/internal/site"
	"tiliweb/internal/version"
	"tiliweb/internal/view"
	"tiliweb/web"
)

type Server struct {
	cfg             *config.Config
	db              *sql.DB
	view            *view.Loader
	auth            *auth.Service
	content         *content.Service
	forum           *forum.Service
	site            *site.Store
	csrfSvc         *security.CSRF
	logger          *slog.Logger
	handler         http.Handler
	trusted         []ipRange
	contactLimiter  *contactLimiter
	postLimiter     *contactLimiter
	registerLimiter *contactLimiter

	adminCookiePath string
	frontCookiePath string
}

func New(cfg *config.Config, db *sql.DB, v *view.Loader, authSvc *auth.Service, contentSvc *content.Service, forumSvc *forum.Service, siteStore *site.Store, logger *slog.Logger) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	if db == nil {
		return nil, fmt.Errorf("nil database")
	}
	if v == nil {
		return nil, fmt.Errorf("nil view loader")
	}
	if authSvc == nil {
		return nil, fmt.Errorf("nil auth service")
	}
	if contentSvc == nil {
		return nil, fmt.Errorf("nil content service")
	}
	if forumSvc == nil {
		return nil, fmt.Errorf("nil forum service")
	}
	if siteStore == nil {
		return nil, fmt.Errorf("nil site store")
	}
	if logger == nil {
		logger = slog.Default()
	}

	csrfPath := cfg.Server.BasePath
	if csrfPath == "" {
		csrfPath = "/"
	}
	frontCookiePath := cfg.Server.BasePath
	if frontCookiePath == "" {
		frontCookiePath = "/"
	}
	contactLimit := 0
	postLimit := 0
	registerLimit := 0
	if cfg.RateLimit.Enabled {
		contactLimit = cfg.RateLimit.ContactPerHour
		postLimit = cfg.RateLimit.PostPerMinute
		registerLimit = cfg.RateLimit.RegisterPerHour
	}

	s := &Server{
		cfg:             cfg,
		db:              db,
		view:            v,
		auth:            authSvc,
		content:         contentSvc,
		forum:           forumSvc,
		site:            siteStore,
		csrfSvc:         security.NewCSRF(cfg.Security.CSRFCookieName, csrfPath, cfg.Admin.CookieSecure || cfg.Auth.CookieSecure),
		logger:          logger,
		trusted:         parseTrustedRanges(cfg.Server.TrustedProxies),
		adminCookiePath: joinURLPath(cfg.Server.BasePath, cfg.Admin.Path),
		frontCookiePath: frontCookiePath,
		contactLimiter:  newContactLimiter(contactLimit),
		postLimiter:     newWindowLimiter(postLimit, time.Minute),
		registerLimiter: newWindowLimiter(registerLimit, time.Hour),
	}

	siteMux := http.NewServeMux()
	siteMux.HandleFunc("GET /{$}", s.home)
	siteMux.HandleFunc("GET /healthz", s.healthz)
	siteMux.HandleFunc("GET /readyz", s.readyz)
	siteMux.HandleFunc("GET /robots.txt", s.robots)
	siteMux.HandleFunc("GET /rss.xml", s.rssFeed)
	siteMux.HandleFunc("GET /sitemap.xml", s.sitemap)
	siteMux.HandleFunc("GET /contact", s.contactPage)
	siteMux.HandleFunc("POST /contact", s.contactPost)
	siteMux.HandleFunc("GET /search", s.searchPage)
	static, err := s.staticHandler()
	if err != nil {
		return nil, err
	}
	siteMux.Handle("GET /static/", http.StripPrefix("/static/", static))

	// Front auth/account.
	siteMux.HandleFunc("GET /login", s.loginPage)
	siteMux.HandleFunc("POST /login", s.loginPost)
	siteMux.HandleFunc("GET /register", s.registerPage)
	siteMux.HandleFunc("POST /register", s.registerPost)
	siteMux.HandleFunc("POST /logout", s.logoutPost)
	siteMux.HandleFunc("GET /account", s.requireUser(s.accountPage))
	siteMux.HandleFunc("GET /account/password", s.requireUser(s.accountPasswordPage))
	siteMux.HandleFunc("POST /account/password", s.requireUser(s.accountPasswordPost))

	// Public content.
	siteMux.HandleFunc("GET /tags/{tag}", s.siteTag)
	siteMux.HandleFunc("GET /media/{id}/{name}", s.serveMedia)
	siteMux.HandleFunc("GET /links", s.linksPage)
	siteMux.HandleFunc("GET /download/{id}", s.downloadFile)
	siteMux.HandleFunc("POST /{channel}/{key}/comment", s.commentPost)

	// Forum.
	siteMux.HandleFunc("GET /forum", s.forumIndex)
	siteMux.HandleFunc("GET /forum/{board}", s.forumBoard)
	siteMux.HandleFunc("GET /forum/{board}/{topic}", s.forumTopic)
	siteMux.HandleFunc("POST /forum/{board}/new", s.requireUser(s.forumNewTopic))
	siteMux.HandleFunc("POST /forum/topic/{id}/reply", s.requireUser(s.forumReply))
	siteMux.HandleFunc("POST /forum/topic/{id}/moderate", s.requireUser(s.forumModerateTopic))
	siteMux.HandleFunc("POST /forum/post/{id}/moderate", s.requireUser(s.forumModeratePost))

	// Admin framework.
	adminPath := cfg.Admin.Path
	siteMux.HandleFunc("GET "+adminPath+"/login", s.adminLoginPage)
	siteMux.HandleFunc("POST "+adminPath+"/login", s.adminLoginPost)
	siteMux.HandleFunc("POST "+adminPath+"/logout", s.requireAdmin(s.adminLogout))
	siteMux.HandleFunc("GET "+adminPath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, s.adminHome(), http.StatusMovedPermanently)
	})
	siteMux.HandleFunc("GET "+adminPath+"/{$}", s.requireAdmin(s.adminDashboard))
	siteMux.HandleFunc("GET "+adminPath+"/users", s.requireSuperadmin(s.adminUsers))
	siteMux.HandleFunc("POST "+adminPath+"/users/{id}/status", s.requireSuperadmin(s.adminUserStatus))
	siteMux.HandleFunc("POST "+adminPath+"/users/{id}/password", s.requireSuperadmin(s.adminUserPassword))
	siteMux.HandleFunc("GET "+adminPath+"/invites", s.requireSuperadmin(s.adminInvites))
	siteMux.HandleFunc("POST "+adminPath+"/invites", s.requireSuperadmin(s.adminInviteCreate))
	siteMux.HandleFunc("POST "+adminPath+"/invites/{id}/revoke", s.requireSuperadmin(s.adminInviteRevoke))
	siteMux.HandleFunc("GET "+adminPath+"/channels", s.requireEditor(s.adminChannels))
	siteMux.HandleFunc("GET "+adminPath+"/channels/new", s.requireEditor(s.adminChannelNewPage))
	siteMux.HandleFunc("POST "+adminPath+"/channels/new", s.requireEditor(s.adminChannelNewPost))
	siteMux.HandleFunc("GET "+adminPath+"/channels/{id}/edit", s.requireEditor(s.adminChannelEditPage))
	siteMux.HandleFunc("POST "+adminPath+"/channels/{id}/edit", s.requireEditor(s.adminChannelEditPost))
	siteMux.HandleFunc("POST "+adminPath+"/channels/{id}/delete", s.requireEditor(s.adminChannelDelete))
	siteMux.HandleFunc("GET "+adminPath+"/entries", s.requireEditor(s.adminEntries))
	siteMux.HandleFunc("GET "+adminPath+"/entries/new", s.requireEditor(s.adminEntryNewPage))
	siteMux.HandleFunc("POST "+adminPath+"/entries/new", s.requireEditor(s.adminEntryNewPost))
	siteMux.HandleFunc("GET "+adminPath+"/entries/{id}/edit", s.requireEditor(s.adminEntryEditPage))
	siteMux.HandleFunc("POST "+adminPath+"/entries/{id}/edit", s.requireEditor(s.adminEntryEditPost))
	siteMux.HandleFunc("POST "+adminPath+"/entries/{id}/status", s.requireEditor(s.adminEntryStatus))
	siteMux.HandleFunc("POST "+adminPath+"/entries/{id}/delete", s.requireEditor(s.adminEntryDelete))
	siteMux.HandleFunc("POST "+adminPath+"/entries/{id}/restore", s.requireEditor(s.adminEntryRestore))
	siteMux.HandleFunc("GET "+adminPath+"/entries/{id}/revisions", s.requireEditor(s.adminEntryRevisions))
	siteMux.HandleFunc("GET "+adminPath+"/pages", s.requireEditor(s.adminPages))
	siteMux.HandleFunc("GET "+adminPath+"/pages/new", s.requireEditor(s.adminPageNewPage))
	siteMux.HandleFunc("POST "+adminPath+"/pages/new", s.requireEditor(s.adminPageNewPost))
	siteMux.HandleFunc("GET "+adminPath+"/pages/{id}/edit", s.requireEditor(s.adminPageEditPage))
	siteMux.HandleFunc("POST "+adminPath+"/pages/{id}/edit", s.requireEditor(s.adminPageEditPost))
	siteMux.HandleFunc("POST "+adminPath+"/pages/{id}/delete", s.requireEditor(s.adminPageDelete))
	siteMux.HandleFunc("GET "+adminPath+"/pages/{id}/revisions", s.requireEditor(s.adminPageRevisions))
	siteMux.HandleFunc("POST "+adminPath+"/revisions/{id}/restore", s.requireEditor(s.adminRevisionRestore))
	siteMux.HandleFunc("GET "+adminPath+"/tags", s.requireEditor(s.adminTags))
	siteMux.HandleFunc("GET "+adminPath+"/media", s.requireEditor(s.adminMedia))
	siteMux.HandleFunc("POST "+adminPath+"/media/upload", s.requireEditor(s.adminMediaUpload))
	siteMux.HandleFunc("POST "+adminPath+"/media/{id}/delete", s.requireEditor(s.adminMediaDelete))
	siteMux.HandleFunc("GET "+adminPath+"/banners", s.requireEditor(s.adminBanners))
	siteMux.HandleFunc("GET "+adminPath+"/banners/new", s.requireEditor(s.adminBannerNewPage))
	siteMux.HandleFunc("POST "+adminPath+"/banners/new", s.requireEditor(s.adminBannerNewPost))
	siteMux.HandleFunc("GET "+adminPath+"/banners/{id}/edit", s.requireEditor(s.adminBannerEditPage))
	siteMux.HandleFunc("POST "+adminPath+"/banners/{id}/edit", s.requireEditor(s.adminBannerEditPost))
	siteMux.HandleFunc("POST "+adminPath+"/banners/{id}/delete", s.requireEditor(s.adminBannerDelete))
	siteMux.HandleFunc("GET "+adminPath+"/home", s.requireEditor(s.adminHomeModules))
	siteMux.HandleFunc("POST "+adminPath+"/home", s.requireEditor(s.adminHomePost))
	siteMux.HandleFunc("GET "+adminPath+"/messages", s.requireEditor(s.adminMessages))
	siteMux.HandleFunc("POST "+adminPath+"/messages/{id}/status", s.requireEditor(s.adminMessageStatus))
	siteMux.HandleFunc("POST "+adminPath+"/messages/{id}/note", s.requireEditor(s.adminMessageNote))
	siteMux.HandleFunc("GET "+adminPath+"/settings", s.requireSuperadmin(s.adminSettings))
	siteMux.HandleFunc("POST "+adminPath+"/settings", s.requireSuperadmin(s.adminSettingsPost))
	siteMux.HandleFunc("POST "+adminPath+"/entries/{id}/attachments", s.requireEditor(s.adminEntryAttachmentUpload))
	siteMux.HandleFunc("POST "+adminPath+"/attachments/{id}/delete", s.requireEditor(s.adminAttachmentDelete))
	siteMux.HandleFunc("GET "+adminPath+"/comments", s.requireModerator(s.adminComments))
	siteMux.HandleFunc("POST "+adminPath+"/comments/{id}/status", s.requireModerator(s.adminCommentStatus))
	siteMux.HandleFunc("GET "+adminPath+"/links", s.requireEditor(s.adminLinks))
	siteMux.HandleFunc("GET "+adminPath+"/links/new", s.requireEditor(s.adminLinkNewPage))
	siteMux.HandleFunc("POST "+adminPath+"/links/new", s.requireEditor(s.adminLinkNewPost))
	siteMux.HandleFunc("GET "+adminPath+"/links/{id}/edit", s.requireEditor(s.adminLinkEditPage))
	siteMux.HandleFunc("POST "+adminPath+"/links/{id}/edit", s.requireEditor(s.adminLinkEditPost))
	siteMux.HandleFunc("POST "+adminPath+"/links/{id}/delete", s.requireEditor(s.adminLinkDelete))
	siteMux.HandleFunc("GET "+adminPath+"/redirects", s.requireEditor(s.adminRedirects))
	siteMux.HandleFunc("GET "+adminPath+"/redirects/new", s.requireEditor(s.adminRedirectNewPage))
	siteMux.HandleFunc("POST "+adminPath+"/redirects/new", s.requireEditor(s.adminRedirectNewPost))
	siteMux.HandleFunc("GET "+adminPath+"/redirects/{id}/edit", s.requireEditor(s.adminRedirectEditPage))
	siteMux.HandleFunc("POST "+adminPath+"/redirects/{id}/edit", s.requireEditor(s.adminRedirectEditPost))
	siteMux.HandleFunc("POST "+adminPath+"/redirects/{id}/delete", s.requireEditor(s.adminRedirectDelete))
	siteMux.HandleFunc("GET "+adminPath+"/forum/boards", s.requireEditor(s.adminForumBoards))
	siteMux.HandleFunc("GET "+adminPath+"/forum/boards/new", s.requireEditor(s.adminForumBoardNewPage))
	siteMux.HandleFunc("POST "+adminPath+"/forum/boards/new", s.requireEditor(s.adminForumBoardNewPost))
	siteMux.HandleFunc("GET "+adminPath+"/forum/boards/{id}/edit", s.requireEditor(s.adminForumBoardEditPage))
	siteMux.HandleFunc("POST "+adminPath+"/forum/boards/{id}/edit", s.requireEditor(s.adminForumBoardEditPost))
	siteMux.HandleFunc("POST "+adminPath+"/forum/boards/{id}/delete", s.requireEditor(s.adminForumBoardDelete))
	siteMux.HandleFunc("POST "+adminPath+"/forum/boards/{id}/moderators", s.requireEditor(s.adminForumModerators))
	siteMux.HandleFunc("GET "+adminPath+"/forum/boards/{id}/moderators", s.requireEditor(s.adminForumModerators))
	siteMux.HandleFunc("GET "+adminPath+"/forum/topics", s.requireModerator(s.adminForumTopics))
	siteMux.HandleFunc("POST "+adminPath+"/forum/topics/{id}/moderate", s.requireModerator(s.adminForumTopicModerate))
	siteMux.HandleFunc("POST "+adminPath+"/forum/posts/{id}/moderate", s.requireModerator(s.adminForumPostModerate))

	siteMux.HandleFunc("/", s.fallback)

	root := s.rootHandler(siteMux)
	s.handler = s.requestID(s.realIP(s.securityHeaders(s.bodyLimit(s.sessions(s.csrf(s.accessLog(s.recover(root))))))))
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

// CleanupLoop periodically drops expired in-memory limiter windows.
func (s *Server) CleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.auth.CleanupLimiters()
			s.contactLimiter.cleanup()
			s.postLimiter.cleanup()
			s.registerLimiter.cleanup()
		}
	}
}

func (s *Server) rootHandler(site *http.ServeMux) http.Handler {
	if s.cfg.Server.BasePath == "" {
		return site
	}
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", s.healthz)
	root.HandleFunc("GET /readyz", s.readyz)
	root.HandleFunc("GET /robots.txt", s.robots)
	root.HandleFunc("GET /rss.xml", s.rssFeed)
	root.HandleFunc("GET /sitemap.xml", s.sitemap)
	root.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, s.cfg.Server.BasePath+"/", http.StatusFound)
	})
	root.HandleFunc("GET "+s.cfg.Server.BasePath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, s.cfg.Server.BasePath+"/", http.StatusMovedPermanently)
	})
	root.Handle(s.cfg.Server.BasePath+"/", http.StripPrefix(s.cfg.Server.BasePath, site))
	root.HandleFunc("/", s.notFound)
	return root
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	settings := s.loadSettings(r)
	title := settings.SEO.DefaultTitle
	if strings.TrimSpace(title) == "" {
		title = settings.SiteName
	}
	desc := settings.SEO.DefaultDescription
	if strings.TrimSpace(desc) == "" {
		desc = settings.SiteDescription
	}
	data := s.pageData(r, title, desc)
	data.HomeSettings = settings.Home
	if settings.Home.ShowBanners {
		data.Banners, _ = s.content.ListBanners(r.Context(), true)
	}
	data.LatestEntries, _ = s.content.PublicLatestEntries(r.Context(), settings.Home.LatestCount)
	data.FeaturedEntries, _ = s.content.PublicFeaturedEntries(r.Context(), settings.Home.FeaturedCount)
	data.NewsEntries, _ = s.content.PublicEntriesByType(r.Context(), "article", 3)
	data.CaseEntries, _ = s.content.PublicEntriesByType(r.Context(), "case", 3)
	data.ProductEntries, _ = s.content.PublicEntriesByType(r.Context(), "product", 3)
	data.JSONLD = s.jsonLD(map[string]any{
		"@context":    "https://schema.org",
		"@type":       "Organization",
		"name":        settings.SiteName,
		"description": settings.SiteDescription,
		"url":         s.absoluteURL(joinURLPath(s.cfg.Server.BasePath, "/")),
	})
	s.renderSite(w, r, "site/home", data, http.StatusOK)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		s.logger.Warn("readiness failed", "error", err, "request_id", requestIDFromContext(r.Context()))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "database unavailable\n")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	settings := s.loadSettings(r)
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	b.WriteString("Disallow: ")
	b.WriteString(joinURLPath(s.cfg.Server.BasePath, s.cfg.Admin.Path))
	b.WriteString("\n")
	b.WriteString("Disallow: ")
	b.WriteString(joinURLPath(s.cfg.Server.BasePath, "/search"))
	b.WriteString("\n")
	b.WriteString("Sitemap: ")
	b.WriteString(s.absoluteURL(joinURLPath(s.cfg.Server.BasePath, "/sitemap.xml")))
	b.WriteString("\n")
	if extra := strings.TrimSpace(settings.SEO.RobotsExtra); extra != "" {
		b.WriteString(extra)
		if !strings.HasSuffix(extra, "\n") {
			b.WriteString("\n")
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, b.String())
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	if err := s.view.Render(w, "errors/404", s.pageData(r, "Page not found", "")); err != nil {
		s.logger.Error("render 404", "error", err, "request_id", requestIDFromContext(r.Context()))
		_, _ = io.WriteString(w, "404 page not found\n")
	}
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusInternalServerError)
	if err := s.view.Render(w, "errors/500", s.pageData(r, "Server error", "")); err != nil {
		s.logger.Error("render 500", "error", err, "request_id", requestIDFromContext(r.Context()))
		_, _ = io.WriteString(w, "500 internal server error\n")
	}
}

func (s *Server) pageData(r *http.Request, title, description string) pageData {
	locale := s.cfg.Site.Locale
	if locale == "" {
		locale = "zh-CN"
	}
	title = localizeTitle(title)
	settings := s.loadSettings(r)
	siteName := settings.SiteName
	if strings.TrimSpace(siteName) == "" {
		siteName = s.siteName()
	}
	siteDesc := settings.SiteDescription
	if strings.TrimSpace(siteDesc) == "" {
		siteDesc = s.siteDescription()
	}
	navChannels, _ := s.content.ListChannels(r.Context(), false)
	navPages, _ := s.content.PublicPages(r.Context())
	friendLinks, _ := s.content.ListFriendLinks(r.Context(), true)
	canonical := s.absoluteURL(joinURLPath(s.cfg.Server.BasePath, r.URL.Path))
	ogImage := ""
	if settings.SEO.OGImageMediaID > 0 {
		ogImage = s.absoluteURL(joinURLPath(s.cfg.Server.BasePath, fmt.Sprintf("/media/%d/image", settings.SEO.OGImageMediaID)))
	}
	return pageData{
		PageData: view.PageData{
			SiteName:        siteName,
			SiteDescription: siteDesc,
			BasePath:        s.cfg.Server.BasePath,
			Locale:          locale,
			Version:         version.String(),
			Title:           title,
			Description:     description,
			Path:            r.URL.Path,
			Year:            time.Now().Year(),
			SignedIn:        userFromContext(r.Context()) != nil,
			Canonical:       canonical,
			OGType:          "website",
			OGImage:         ogImage,
		},
		CSRF:              csrfTokenFromContext(r.Context()),
		AdminPath:         s.cfg.Admin.Path,
		Admin:             adminFromContext(r.Context()),
		User:              userFromContext(r.Context()),
		RegisterEnabled:   s.cfg.Auth.RegisterEnabled,
		RegisterMode:      s.cfg.Auth.RegisterMode,
		SiteSettings:      settings,
		HomeSettings:      settings.Home,
		NavChannels:       navChannels,
		NavPages:          navPages,
		FriendLinks:       friendLinks,
		GuestComment:      s.cfg.Auth.GuestComment,
		CommentModeration: s.cfg.Content.CommentModeration,
	}
}

func localizeTitle(title string) string {
	switch title {
	case "Dashboard":
		return "仪表盘"
	case "Users":
		return "前台用户"
	case "Invites":
		return "邀请码"
	case "Sign in":
		return "登录"
	case "Create account":
		return "创建账号"
	case "Your account":
		return "个人中心"
	case "Change password":
		return "修改密码"
	case "Admin sign in":
		return "后台登录"
	case "Contact":
		return "联系我们"
	case "Search":
		return "站内搜索"
	case "Page not found":
		return "页面不存在"
	case "Server error":
		return "服务器错误"
	case "Admin":
		return "管理后台"
	case "Channels":
		return "栏目"
	case "New channel":
		return "新建栏目"
	case "Edit channel":
		return "编辑栏目"
	case "Entries":
		return "内容列表"
	case "New entry":
		return "新建内容"
	case "Edit entry":
		return "编辑内容"
	case "Entry revisions":
		return "内容修订"
	case "Pages":
		return "页面"
	case "New page":
		return "新建页面"
	case "Edit page":
		return "编辑页面"
	case "Page revisions":
		return "页面修订"
	case "Tags":
		return "标签"
	case "Media":
		return "媒体库"
	case "Banners":
		return "首页轮播"
	case "New banner":
		return "新建轮播"
	case "Edit banner":
		return "编辑轮播"
	case "Home modules":
		return "首页模块"
	case "Messages":
		return "留言"
	case "Comments":
		return "评论"
	case "Settings":
		return "站点设置"
	case "Forum":
		return "论坛"
	case "Forum boards":
		return "论坛板块"
	case "New board":
		return "新建板块"
	case "Edit board":
		return "编辑板块"
	case "Board moderators":
		return "板块版主"
	case "Forum topics":
		return "论坛主题"
	case "Friend links":
		return "友情链接"
	case "New friend link":
		return "新建友情链接"
	case "Edit friend link":
		return "编辑友情链接"
	case "Redirects":
		return "重定向"
	case "New redirect":
		return "新建重定向"
	case "Edit redirect":
		return "编辑重定向"
	case "Revisions":
		return "修订记录"
	}
	return title
}

func (s *Server) siteName() string {
	if strings.TrimSpace(s.cfg.Site.Name) != "" {
		return s.cfg.Site.Name
	}
	return "tiliweb"
}

func (s *Server) siteTitle() string {
	if strings.TrimSpace(s.cfg.Site.SEO.DefaultTitle) != "" {
		return s.cfg.Site.SEO.DefaultTitle
	}
	return s.siteName()
}

func (s *Server) siteDescription() string {
	if strings.TrimSpace(s.cfg.Site.SEO.DefaultDescription) != "" {
		return s.cfg.Site.SEO.DefaultDescription
	}
	return s.cfg.Site.Description
}

func (s *Server) staticHandler() (http.Handler, error) {
	var fileServer http.Handler
	if s.cfg.Dev.StaticDisk {
		fileServer = http.FileServer(http.Dir(s.cfg.Dev.StaticDir))
	} else {
		sub, err := fs.Sub(web.FS, "static")
		if err != nil {
			return nil, fmt.Errorf("embedded static: %w", err)
		}
		fileServer = http.FileServer(http.FS(sub))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		if s.cfg.Dev.StaticDisk {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

func joinURLPath(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return "/"
	}
	return "/" + strings.Join(out, "/")
}

// requestIDMiddleware key helpers live in middleware.go.
