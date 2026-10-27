package security

import (
	"net/http"
	"time"
)

// SetCookie sets an HttpOnly cookie with the given path/TTL/SameSite.
func SetCookie(w http.ResponseWriter, name, value, path string, ttl time.Duration, secure bool, sameSite http.SameSite) {
	if path == "" {
		path = "/"
	}
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		MaxAge:   int(ttl.Seconds()),
	}
	if ttl > 0 {
		c.Expires = time.Now().Add(ttl)
	}
	http.SetCookie(w, c)
}

// ClearCookie expires a cookie on the same path.
func ClearCookie(w http.ResponseWriter, name, path string, secure bool, sameSite http.SameSite) {
	if path == "" {
		path = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func Cookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
