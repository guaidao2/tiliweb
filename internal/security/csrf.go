package security

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CSRF implements session-bound tokens and a double-submit cookie for
// anonymous forms (login/register).
type CSRF struct {
	cookieName string
	path       string
	secure     bool
	now        func() time.Time
}

func NewCSRF(cookieName, path string, secure bool) *CSRF {
	if path == "" {
		path = "/"
	}
	return &CSRF{
		cookieName: cookieName,
		path:       path,
		secure:     secure,
		now:        time.Now,
	}
}

// Ensure returns the double-submit token, creating the cookie if needed.
func (c *CSRF) Ensure(w http.ResponseWriter, r *http.Request) string {
	if token := Cookie(r, c.cookieName); token != "" {
		return token
	}
	token, err := NewCSRFToken()
	if err != nil {
		return ""
	}
	SetCookie(w, c.cookieName, token, c.path, 24*time.Hour, c.secure, http.SameSiteLaxMode)
	return token
}

// Token returns the session token when authenticated, otherwise the
// double-submit cookie.
func (c *CSRF) Token(r *http.Request, sessionToken string) string {
	if sessionToken != "" {
		return sessionToken
	}
	return Cookie(r, c.cookieName)
}

// Validate checks the submitted token and, when present, Origin/Sec-Fetch-Site.
func (c *CSRF) Validate(r *http.Request, sessionToken string) bool {
	expected := c.Token(r, sessionToken)
	if expected == "" {
		return false
	}

	provided := r.Header.Get("X-CSRF-Token")
	if provided == "" {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				return false
			}
			provided = r.FormValue("_csrf")
		} else {
			if err := r.ParseForm(); err != nil {
				return false
			}
			provided = r.PostFormValue("_csrf")
		}
	}
	if provided == "" || !ConstantTimeEqual(expected, provided) {
		return false
	}

	if origin := r.Header.Get("Origin"); origin != "" {
		if origin == "null" {
			return false
		}
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, r.Host) {
			return false
		}
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		switch site {
		case "same-origin", "none":
		default:
			return false
		}
	}
	return true
}
