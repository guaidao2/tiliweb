package mcp

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"tiliweb/internal/config"
	"tiliweb/internal/security"
)

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveMCP)
}

func (s *Server) ServeHTTP(ctx context.Context) error {
	ln, err := s.HTTPListener()
	if err != nil {
		return err
	}
	return s.ServeHTTPListener(ctx, ln)
}

// HTTPListener binds the MCP listener so `serve` can fail before starting.
func (s *Server) HTTPListener() (net.Listener, error) {
	if !s.cfg.MCP.Enabled || !s.cfg.MCP.HTTP.Enabled {
		return nil, errors.New("MCP HTTP transport is disabled")
	}
	ln, err := net.Listen("tcp", s.cfg.MCP.HTTP.Listen)
	if err != nil {
		return nil, err
	}
	if s.cfg.MCP.HTTP.TLS.Enabled {
		cert, err := tls.LoadX509KeyPair(s.cfg.MCP.HTTP.TLS.CertFile, s.cfg.MCP.HTTP.TLS.KeyFile)
		if err != nil {
			_ = ln.Close()
			return nil, err
		}
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	}
	return ln, nil
}

func (s *Server) ServeHTTPListener(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadTimeout:       s.cfg.MCP.HTTP.ReadTimeout.Std(),
		WriteTimeout:      s.cfg.MCP.HTTP.WriteTimeout.Std(),
		ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes:    s.cfg.Server.MaxHeaderBytes,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) serveMCP(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if !s.ipAllowed(ip) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	token := bearerToken(r)
	if token == "" {
		s.recordAuthFailure(r.Context(), ip, "")
		w.Header().Set("WWW-Authenticate", `Bearer realm="tiliweb-mcp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if ok, _ := s.authLimiter.Allow(ip, ip); !ok {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	t, err := s.tokens.Authenticate(r.Context(), token)
	if err != nil {
		s.authLimiter.Fail(ip, ip)
		prefix := token
		if len(prefix) > 16 {
			prefix = prefix[:16]
		}
		s.recordAuthFailure(r.Context(), ip, prefix)
		w.Header().Set("WWW-Authenticate", `Bearer realm="tiliweb-mcp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.authLimiter.Success(ip)
	limit := t.RateLimit
	if limit <= 0 {
		limit = s.cfg.MCP.RateLimitPerMinute
	}
	if !s.tokenRate.allow(t.ID, limit) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	auth := AuthContext{TokenID: t.ID, AdminID: t.CreatedBy, Scopes: t.Scopes, IP: ip, HTTP: true}

	switch r.Method {
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MCP.HTTP.MaxBodyBytes)
		var req Request
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid JSON-RPC request", http.StatusBadRequest)
			return
		}
		if isNotification(req.ID) {
			_ = s.Handle(r.Context(), req, auth)
			w.WriteHeader(http.StatusAccepted)
			return
		}
		resp := s.Handle(r.Context(), req, auth)
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", newSessionID())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	case http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "SSE is not enabled", http.StatusMethodNotAllowed)
	default:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) ipAllowed(ip string) bool {
	if len(s.allowedIPs) == 0 {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range s.allowedIPs {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

func bearerToken(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

func (s *Server) clientIP(r *http.Request) string {
	remote := parseRemoteIP(r.RemoteAddr)
	if remote == nil {
		return r.RemoteAddr
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

func (s *Server) isTrusted(ip net.IP) bool {
	for _, n := range s.trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func parseRemoteIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(strings.Trim(host, "[]"))
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type tokenRate struct {
	mu   sync.Mutex
	now  func() time.Time
	hits map[int64]tokenHit
}

type tokenHit struct {
	count int
	start time.Time
}

func newTokenRate() *tokenRate {
	return &tokenRate{now: time.Now, hits: map[int64]tokenHit{}}
}

func (l *tokenRate) allow(id int64, limit int) bool {
	if limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	h := l.hits[id]
	if h.start.IsZero() || now.Sub(h.start) >= time.Minute {
		h = tokenHit{start: now}
	}
	if h.count >= limit {
		return false
	}
	h.count++
	l.hits[id] = h
	return true
}

func newAuthLimiter(cfg *config.Config) *security.Limiter {
	return security.NewLimiter(5, cfg.MCP.HTTP.AuthLockout.Std(), cfg.MCP.HTTP.AuthRatePerMinute)
}

func parseAllowedIPs(values []string) []*net.IPNet {
	var out []*net.IPNet
	for _, v := range values {
		if strings.Contains(v, "/") {
			if _, n, err := net.ParseCIDR(v); err == nil {
				out = append(out, n)
			}
		} else if ip := net.ParseIP(v); ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return out
}
