package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

type contextKey int

const (
	requestIDKey contextKey = iota
	clientIPKey
)

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if id == "" || len(id) > 128 {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (s *Server) realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), clientIPKey, s.clientIP(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func clientIPFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(clientIPKey).(string); ok {
		return v
	}
	return ""
}

func (s *Server) clientIP(r *http.Request) string {
	remote := remoteIP(r)
	if remote == nil {
		return ""
	}
	if !s.isTrusted(remote) {
		return remote.String()
	}

	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	ips := make([]net.IP, 0, len(parts)+1)
	for _, p := range parts {
		if ip := net.ParseIP(strings.TrimSpace(p)); ip != nil {
			ips = append(ips, ip)
		}
	}
	ips = append(ips, remote)
	for i := len(ips) - 1; i >= 0; i-- {
		if !s.isTrusted(ips[i]) {
			return ips[i].String()
		}
	}
	return remote.String()
}

func remoteIP(r *http.Request) net.IP {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return net.ParseIP(strings.Trim(host, "[]"))
}

type ipRange struct {
	ip      net.IP
	network *net.IPNet
}

func (r ipRange) contains(ip net.IP) bool {
	if r.network != nil {
		return r.network.Contains(ip)
	}
	return r.ip.Equal(ip)
}

func parseTrustedRanges(entries []string) []ipRange {
	out := make([]ipRange, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if ip := net.ParseIP(strings.Trim(entry, "[]")); ip != nil {
			out = append(out, ipRange{ip: ip})
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil {
			out = append(out, ipRange{network: network})
		}
	}
	return out
}

func (s *Server) isTrusted(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, r := range s.trusted {
		if r.contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return s.isTrusted(remoteIP(r)) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		if s.cfg.Security.HSTSEnabled && s.isHTTPS(r) {
			h.Set("Strict-Transport-Security", fmt.Sprintf("max-age=%d", s.cfg.Security.HSTSMaxAge))
		}
		if s.cfg.Security.ServerHeader != "" {
			h.Set("Server", s.cfg.Security.ServerHeader)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && s.cfg.Server.MaxBodyBytes > 0 && methodHasBody(r.Method) {
			r.Body = http.MaxBytesReader(w, r.Body, s.cfg.Server.MaxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func methodHasBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)

		if !s.cfg.Log.AccessLog || skipAccessLog(r.URL.Path) {
			return
		}
		s.logger.LogAttrs(r.Context(), slog.LevelInfo, "http_request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.Status()),
			slog.Int64("bytes", sw.Bytes()),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.String("ip", clientIPFromContext(r.Context())),
			slog.String("request_id", requestIDFromContext(r.Context())),
		)
	})
}

func skipAccessLog(path string) bool {
	if path == "/healthz" || path == "/readyz" {
		return true
	}
	return strings.HasPrefix(path, "/static/")
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic",
					"error", fmt.Sprint(rec),
					"stack", string(debug.Stack()),
					"method", r.Method,
					"path", r.URL.Path,
					"ip", clientIPFromContext(r.Context()),
					"request_id", requestIDFromContext(r.Context()),
				)
				if sw, ok := w.(*statusWriter); ok && sw.Written() {
					return
				}
				s.serverError(w, r)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
