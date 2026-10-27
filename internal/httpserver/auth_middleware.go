package httpserver

import (
	"context"
	"net/http"
	"strings"

	"tiliweb/internal/auth"
	"tiliweb/internal/security"
)

const (
	adminSessionKey contextKey = iota + 10
	userSessionKey
	csrfTokenKey
)

func (s *Server) sessions(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if token := security.Cookie(r, s.cfg.Admin.CookieName); token != "" {
			if session, err := s.auth.LoadAdminSession(ctx, token); err == nil {
				ctx = context.WithValue(ctx, adminSessionKey, session)
			} else {
				s.clearAdminCookie(w)
			}
		}
		if token := security.Cookie(r, s.cfg.Auth.CookieName); token != "" {
			if session, err := s.auth.LoadUserSession(ctx, token); err == nil {
				ctx = context.WithValue(ctx, userSessionKey, session)
			} else {
				s.clearUserCookie(w)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionToken := ""
		if a := adminFromContext(r.Context()); a != nil {
			sessionToken = a.CSRF
		}
		if sessionToken == "" {
			if u := userFromContext(r.Context()); u != nil {
				sessionToken = u.CSRF
			}
		}
		token := sessionToken
		if token == "" {
			token = s.csrfSvc.Ensure(w, r)
		}
		ctx := context.WithValue(r.Context(), csrfTokenKey, token)
		if isUnsafeMethod(r.Method) && !s.csrfSvc.Validate(r, sessionToken) {
			s.logger.Warn("csrf rejected", "method", r.Method, "path", r.URL.Path, "ip", clientIPFromContext(r.Context()))
			http.Error(w, "invalid csrf token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func adminFromContext(ctx context.Context) *auth.AdminSession {
	if v, ok := ctx.Value(adminSessionKey).(*auth.AdminSession); ok {
		return v
	}
	return nil
}

func userFromContext(ctx context.Context) *auth.UserSession {
	if v, ok := ctx.Value(userSessionKey).(*auth.UserSession); ok {
		return v
	}
	return nil
}

func csrfTokenFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(csrfTokenKey).(string); ok {
		return v
	}
	return ""
}

func (s *Server) setAdminCookie(w http.ResponseWriter, token string) {
	security.SetCookie(w, s.cfg.Admin.CookieName, token, s.adminCookiePath,
		s.cfg.Admin.SessionTTL.Std(), s.cfg.Admin.CookieSecure, http.SameSiteStrictMode)
}

func (s *Server) clearAdminCookie(w http.ResponseWriter) {
	security.ClearCookie(w, s.cfg.Admin.CookieName, s.adminCookiePath,
		s.cfg.Admin.CookieSecure, http.SameSiteStrictMode)
}

func (s *Server) setUserCookie(w http.ResponseWriter, token string) {
	security.SetCookie(w, s.cfg.Auth.CookieName, token, s.frontCookiePath,
		s.cfg.Auth.SessionTTL.Std(), s.cfg.Auth.CookieSecure, http.SameSiteLaxMode)
}

func (s *Server) clearUserCookie(w http.ResponseWriter) {
	security.ClearCookie(w, s.cfg.Auth.CookieName, s.frontCookiePath,
		s.cfg.Auth.CookieSecure, http.SameSiteLaxMode)
}

func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if userFromContext(r.Context()) == nil {
			if r.Method == http.MethodGet {
				http.Redirect(w, r, s.frontURL("login"), http.StatusSeeOther)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminFromContext(r.Context()) == nil {
			if r.Method == http.MethodGet {
				http.Redirect(w, r, s.adminURL("login"), http.StatusSeeOther)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) requireSuperadmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		if adminFromContext(r.Context()).Role != "superadmin" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (s *Server) adminURL(parts ...string) string {
	all := []string{s.cfg.Server.BasePath, s.cfg.Admin.Path}
	all = append(all, parts...)
	return joinURLPath(all...)
}

func (s *Server) adminHome() string {
	return s.adminURL() + "/"
}

func (s *Server) frontURL(parts ...string) string {
	all := []string{s.cfg.Server.BasePath}
	all = append(all, parts...)
	return joinURLPath(all...)
}

func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, "\\") {
		return ""
	}
	return next
}
