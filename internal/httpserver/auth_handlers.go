package httpserver

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tiliweb/internal/auth"
	"tiliweb/internal/content"
	"tiliweb/internal/forum"
	"tiliweb/internal/site"
	"tiliweb/internal/view"
)

type pageData struct {
	view.PageData

	CSRF               string
	AdminPath          string
	Admin              *auth.AdminSession
	User               *auth.UserSession
	Error              string
	Success            string
	Next               string
	RegisterEnabled    bool
	RegisterMode       string
	Counts             auth.Counts
	Users              []auth.User
	Invites            []auth.Invite
	NewCode            string
	Audit              []auth.AuditLog
	Channels           []content.Channel
	Channel            *content.Channel
	Entries            []content.Entry
	Entry              *content.Entry
	Pages              []content.Page
	Page               *content.Page
	Tags               []content.Tag
	Revisions          []content.Revision
	RevisionTargetType string
	RevisionTargetID   int64
	FilterChannelID    int64
	FilterStatus       string
	IncludeDeleted     bool
	Tag                *content.Tag
	Media              []content.Media
	Banners            []content.Banner
	Banner             *content.Banner
	SiteSettings       site.Settings
	HomeSettings       site.Home
	Messages           []site.ContactMessage
	MessageFilter      string
	NewMessages        int
	NavChannels        []content.Channel
	NavPages           []content.Page
	LatestEntries      []content.Entry
	FeaturedEntries    []content.Entry
	NewsEntries        []content.Entry
	CaseEntries        []content.Entry
	ProductEntries     []content.Entry
	SearchResults      []content.SearchResult
	SearchQuery        string
	SearchType         string
	ContactStarted     int64
	Attachments        []content.Attachment
	Comments           []content.Comment
	CommentFilter      string
	PendingComments    int
	FriendLinks        []content.FriendLink
	FriendLink         *content.FriendLink
	Redirects          []content.Redirect
	Redirect           *content.Redirect
	CommentsEnabled    bool
	GuestComment       bool
	CommentModeration  bool
	Boards             []forum.Board
	Board              *forum.Board
	Topics             []forum.Topic
	Topic              *forum.Topic
	Posts              []forum.Post
	CanModerate        bool
	ForumModerators    []forum.Moderator
	AdminBoards        []forum.Board
}

func (s *Server) renderSite(w http.ResponseWriter, r *http.Request, page string, data pageData, status int) {
	s.render(w, r, "", page, data, status)
}

func (s *Server) renderAdmin(w http.ResponseWriter, r *http.Request, page string, data pageData, status int) {
	s.render(w, r, "admin/layout.html", page, data, status)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, layoutPage, page string, data pageData, status int) {
	var buf bytes.Buffer
	var err error
	if layoutPage == "" {
		err = s.view.Render(&buf, page, data)
	} else {
		err = s.view.RenderLayout(&buf, layoutPage, page, data)
	}
	if err != nil {
		s.logger.Error("render page", "page", page, "error", err, "request_id", requestIDFromContext(r.Context()))
		if status == http.StatusOK {
			s.serverError(w, r)
			return
		}
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if status != 0 {
		w.WriteHeader(status)
	}
	_, _ = buf.WriteTo(w)
}

func authErrorMessage(err error) string {
	switch {
	case errors.Is(err, auth.ErrPending):
		return "账号正在等待管理员审批。"
	case errors.Is(err, auth.ErrDisabled):
		return "账号已被禁用。"
	case errors.Is(err, auth.ErrRegisterDisabled):
		return "当前未开放注册。"
	case errors.Is(err, auth.ErrConflict):
		return "用户名或邮箱已存在。"
	case errors.Is(err, auth.ErrInvalidInvite):
		return "邀请码无效、已过期或已被使用。"
	case errors.Is(err, auth.ErrInvalidCredentials):
		return "用户名或密码错误。"
	}
	var rl *auth.RateLimitError
	if errors.As(err, &rl) {
		if rl.Wait > 0 {
			return "尝试过于频繁，请在 " + rl.Wait.Round(time.Second).String() + " 后重试。"
		}
		return "尝试过于频繁，请稍后再试。"
	}
	return err.Error()
}

func authStatus(err error) int {
	var rl *auth.RateLimitError
	if errors.As(err, &rl) {
		return http.StatusTooManyRequests
	}
	return http.StatusUnauthorized
}

func registerStatus(err error) int {
	var rl *auth.RateLimitError
	if errors.As(err, &rl) {
		return http.StatusTooManyRequests
	}
	return http.StatusBadRequest
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if userFromContext(r.Context()) != nil {
		http.Redirect(w, r, s.frontURL("account"), http.StatusSeeOther)
		return
	}
	data := s.pageData(r, "Sign in", "")
	data.Next = safeNext(r.URL.Query().Get("next"))
	switch r.URL.Query().Get("registered") {
	case "pending":
		data.Success = "注册信息已提交，需要管理员审批后才能登录。"
	case "done":
		data.Success = "账号已创建，请登录。"
	}
	s.renderSite(w, r, "site/login", data, http.StatusOK)
}

func (s *Server) loginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	_, token, err := s.auth.UserLogin(r.Context(), username, password,
		clientIPFromContext(r.Context()), r.UserAgent())
	if err != nil {
		data := s.pageData(r, "Sign in", "")
		data.Error = authErrorMessage(err)
		data.Next = next
		if rl, ok := err.(*auth.RateLimitError); ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(rl.Wait.Seconds())))
		}
		s.renderSite(w, r, "site/login", data, authStatus(err))
		return
	}
	s.setUserCookie(w, token)
	if next == "" {
		next = s.frontURL("account")
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) logoutPost(w http.ResponseWriter, r *http.Request) {
	if user := userFromContext(r.Context()); user != nil {
		_ = s.auth.UserLogout(r.Context(), user.ID)
	}
	s.clearUserCookie(w)
	http.Redirect(w, r, s.frontURL(), http.StatusSeeOther)
}

func (s *Server) registerPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Create account", "")
	s.renderSite(w, r, "site/register", data, http.StatusOK)
}

func (s *Server) registerPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	invite := strings.TrimSpace(r.PostFormValue("invite"))

	user, err := s.auth.RegisterUser(r.Context(), username, email, password, invite, clientIPFromContext(r.Context()))
	if err != nil {
		data := s.pageData(r, "Create account", "")
		data.Error = authErrorMessage(err)
		s.renderSite(w, r, "site/register", data, registerStatus(err))
		return
	}
	if user.Status == "pending" {
		http.Redirect(w, r, s.frontURL("login")+"?registered=pending", http.StatusSeeOther)
		return
	}

	_, token, err := s.auth.UserLogin(r.Context(), username, password,
		clientIPFromContext(r.Context()), r.UserAgent())
	if err != nil {
		http.Redirect(w, r, s.frontURL("login")+"?registered=done", http.StatusSeeOther)
		return
	}
	s.setUserCookie(w, token)
	http.Redirect(w, r, s.frontURL("account"), http.StatusSeeOther)
}

func (s *Server) accountPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Your account", "")
	if r.URL.Query().Get("password") == "1" {
		data.Success = "密码已更新。"
	}
	s.renderSite(w, r, "site/account", data, http.StatusOK)
}

func (s *Server) accountPasswordPage(w http.ResponseWriter, r *http.Request) {
	s.renderSite(w, r, "site/account_password", s.pageData(r, "Change password", ""), http.StatusOK)
}

func (s *Server) accountPasswordPost(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, s.frontURL("login"), http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	current := r.PostFormValue("current_password")
	next := r.PostFormValue("new_password")
	if err := s.auth.ChangeUserPassword(r.Context(), user.UserID, current, next, user.ID); err != nil {
		data := s.pageData(r, "Change password", "")
		data.Error = authErrorMessage(err)
		s.renderSite(w, r, "site/account_password", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.frontURL("account")+"?password=1", http.StatusSeeOther)
}

func (s *Server) adminLoginPage(w http.ResponseWriter, r *http.Request) {
	if adminFromContext(r.Context()) != nil {
		http.Redirect(w, r, s.adminHome(), http.StatusSeeOther)
		return
	}
	s.renderAdmin(w, r, "admin/login", s.pageData(r, "Admin sign in", ""), http.StatusOK)
}

func (s *Server) adminLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	_, token, err := s.auth.AdminLogin(r.Context(), username, password,
		clientIPFromContext(r.Context()), r.UserAgent())
	if err != nil {
		data := s.pageData(r, "Admin sign in", "")
		data.Error = authErrorMessage(err)
		if rl, ok := err.(*auth.RateLimitError); ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(rl.Wait.Seconds())))
		}
		s.renderAdmin(w, r, "admin/login", data, authStatus(err))
		return
	}
	s.setAdminCookie(w, token)
	http.Redirect(w, r, s.adminHome(), http.StatusSeeOther)
}

func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	if admin := adminFromContext(r.Context()); admin != nil {
		_ = s.auth.AdminLogout(r.Context(), admin.ID)
	}
	s.clearAdminCookie(w)
	http.Redirect(w, r, s.adminURL("login"), http.StatusSeeOther)
}

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Dashboard", "")
	if counts, err := s.auth.Counts(r.Context()); err == nil {
		data.Counts = counts
	} else {
		data.Error = err.Error()
	}
	if audit, err := s.auth.RecentAudit(r.Context(), 10); err == nil {
		data.Audit = audit
	}
	if n, err := s.site.CountNewContactMessages(r.Context()); err == nil {
		data.NewMessages = n
	}
	if n, err := s.content.PendingComments(r.Context()); err == nil {
		data.PendingComments = n
	}
	s.renderAdmin(w, r, "admin/dashboard", data, http.StatusOK)
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Users", "")
	users, err := s.auth.ListUsers(r.Context())
	if err != nil {
		data.Error = err.Error()
	}
	data.Users = users
	s.renderAdmin(w, r, "admin/users", data, http.StatusOK)
}

func (s *Server) adminUserStatus(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad user id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	admin := adminFromContext(r.Context())
	if err := s.auth.SetUserStatus(r.Context(), admin.AdminID, id, r.PostFormValue("status")); err != nil {
		s.adminUsersError(w, r, err)
		return
	}
	http.Redirect(w, r, s.adminURL("users"), http.StatusSeeOther)
}

func (s *Server) adminUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad user id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	admin := adminFromContext(r.Context())
	if err := s.auth.ResetUserPassword(r.Context(), admin.AdminID, id, r.PostFormValue("password")); err != nil {
		s.adminUsersError(w, r, err)
		return
	}
	http.Redirect(w, r, s.adminURL("users"), http.StatusSeeOther)
}

func (s *Server) adminUsersError(w http.ResponseWriter, r *http.Request, err error) {
	data := s.pageData(r, "Users", "")
	data.Error = err.Error()
	data.Users, _ = s.auth.ListUsers(r.Context())
	s.renderAdmin(w, r, "admin/users", data, http.StatusBadRequest)
}

func (s *Server) adminInvites(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "Invites", "")
	invites, err := s.auth.ListInvites(r.Context())
	if err != nil {
		data.Error = err.Error()
	}
	data.Invites = invites
	s.renderAdmin(w, r, "admin/invites", data, http.StatusOK)
}

func (s *Server) adminInviteCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	hours, err := strconv.Atoi(r.PostFormValue("hours"))
	if err != nil || hours < 1 || hours > 720 {
		hours = 168
	}
	admin := adminFromContext(r.Context())
	code, _, err := s.auth.CreateInvite(r.Context(), admin.AdminID, time.Duration(hours)*time.Hour)
	data := s.pageData(r, "Invites", "")
	if err != nil {
		data.Error = err.Error()
	} else {
		data.NewCode = code
	}
	data.Invites, _ = s.auth.ListInvites(r.Context())
	s.renderAdmin(w, r, "admin/invites", data, http.StatusOK)
}

func (s *Server) adminInviteRevoke(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad invite id", http.StatusBadRequest)
		return
	}
	admin := adminFromContext(r.Context())
	if err := s.auth.RevokeInvite(r.Context(), admin.AdminID, id); err != nil {
		data := s.pageData(r, "Invites", "")
		data.Error = err.Error()
		data.Invites, _ = s.auth.ListInvites(r.Context())
		s.renderAdmin(w, r, "admin/invites", data, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.adminURL("invites"), http.StatusSeeOther)
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}
