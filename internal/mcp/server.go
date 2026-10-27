package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"tiliweb/internal/config"
	"tiliweb/internal/content"
	"tiliweb/internal/forum"
	"tiliweb/internal/security"
	"tiliweb/internal/site"
	"tiliweb/internal/version"
)

type AuthContext struct {
	TokenID int64
	AdminID int64
	Scopes  []string
	IP      string
	HTTP    bool
}

type Tool struct {
	Name        string
	Description string
	Scope       string
	Write       bool
	InputSchema map[string]any
	Run         func(context.Context, map[string]any) (any, error)
}

type Server struct {
	cfg         *config.Config
	db          *sql.DB
	content     *content.Service
	forum       *forum.Service
	site        *site.Store
	tokens      *TokenStore
	logger      *slog.Logger
	started     time.Time
	tools       map[string]Tool
	allowedIPs  []*net.IPNet
	trusted     []*net.IPNet
	authLimiter *security.Limiter
	tokenRate   *tokenRate
}

func New(cfg *config.Config, db *sql.DB, contentSvc *content.Service, forumSvc *forum.Service, siteStore *site.Store, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		cfg: cfg, db: db, content: contentSvc, forum: forumSvc, site: siteStore,
		tokens: NewTokenStore(db), logger: logger, started: time.Now(), tools: map[string]Tool{},
		allowedIPs:  parseAllowedIPs(cfg.MCP.HTTP.AllowedIPs),
		trusted:     parseAllowedIPs(cfg.Server.TrustedProxies),
		authLimiter: newAuthLimiter(cfg),
		tokenRate:   newTokenRate(),
	}
	s.registerTools()
	return s
}

func (s *Server) Tokens() *TokenStore { return s.tokens }

func (s *Server) ToolDefinitions() []map[string]any {
	out := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, map[string]any{
			"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema,
		})
	}
	return out
}

func (s *Server) Handle(ctx context.Context, req Request, auth AuthContext) *Response {
	if req.JSONRPC != "2.0" {
		return rpcError(req.ID, -32600, "invalid request")
	}
	switch req.Method {
	case "initialize":
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "tiliweb", "version": version.String()},
		}}
	case "ping":
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": s.ToolDefinitions()}}
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return rpcError(req.ID, -32602, "invalid params")
		}
		result, err := s.CallTool(ctx, params.Name, params.Arguments, auth)
		if err != nil {
			return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
				"content": []any{map[string]any{"type": "text", "text": err.Error()}},
				"isError": true,
			}}
		}
		text, _ := json.Marshal(result)
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content":           []any{map[string]any{"type": "text", "text": string(text)}},
			"structuredContent": result,
			"isError":           false,
		}}
	default:
		return rpcError(req.ID, -32601, "method not found")
	}
}

func (s *Server) CallTool(ctx context.Context, name string, args map[string]any, auth AuthContext) (any, error) {
	t, ok := s.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	if t.Scope != "" && !HasScope(auth.Scopes, t.Scope) {
		s.recordCall(ctx, auth, t, args, "error", "scope_denied", 0)
		return nil, errors.New("forbidden: missing scope " + t.Scope)
	}
	if t.Scope == "admin" && !s.cfg.MCP.AllowAdminScope {
		s.recordCall(ctx, auth, t, args, "error", "admin_disabled", 0)
		return nil, errors.New("admin scope is disabled")
	}
	if t.Write && !s.cfg.MCP.AllowWrite {
		s.recordCall(ctx, auth, t, args, "error", "write_disabled", 0)
		return nil, errors.New("write tools are disabled")
	}
	timeout := s.cfg.MCP.CallTimeout.Std()
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := t.Run(cctx, args)
	dur := time.Since(start)
	if err != nil {
		s.recordCall(ctx, auth, t, args, "error", "tool_error", dur)
		return nil, err
	}
	s.recordCall(ctx, auth, t, args, "ok", "", dur)
	return result, nil
}

func (s *Server) recordCall(ctx context.Context, auth AuthContext, t Tool, args map[string]any, status, code string, dur time.Duration) {
	var token any
	if auth.TokenID > 0 {
		token = auth.TokenID
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	summary, _ := json.Marshal(map[string]any{"keys": keys})
	_, _ = s.db.ExecContext(ctx, `INSERT INTO mcp_tool_calls(token_id, tool, scope, args_summary, status, error_code, duration_ms, ip, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, token, t.Name, t.Scope, string(summary), status, nullString(code), dur.Milliseconds(), nullString(auth.IP), time.Now().UTC().Unix())
	meta, _ := json.Marshal(map[string]any{"tool": t.Name, "scope": t.Scope, "status": status, "error": code})
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_logs(actor_type, actor_id, action, target_type, ip, meta, created_at)
		VALUES('mcp', ?, ?, 'mcp_tool', ?, ?, ?)`, token, t.Name, nullString(auth.IP), string(meta), time.Now().UTC().Unix())
}

func (s *Server) recordAuthFailure(ctx context.Context, ip, prefix string) {
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_logs(actor_type, actor_id, action, target_type, ip, meta, created_at)
		VALUES('system', NULL, 'mcp.auth_failed', 'mcp', ?, ?, ?)`, nullString(ip),
		fmt.Sprintf(`{"token_prefix":%q}`, prefix), time.Now().UTC().Unix())
	_, _ = s.db.ExecContext(ctx, `INSERT INTO login_attempts(scope, key, ip, success, created_at)
		VALUES('mcp', ?, ?, 0, ?)`, prefix, nullString(ip), time.Now().UTC().Unix())
}

func (s *Server) adminID(ctx context.Context, auth AuthContext) int64 {
	if auth.AdminID > 0 {
		return auth.AdminID
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM admin_users ORDER BY id LIMIT 1`).Scan(&id); err == nil {
		return id
	}
	return 0
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func (s *Server) requireAdmin(ctx context.Context, auth AuthContext) (int64, error) {
	id := s.adminID(ctx, auth)
	if id == 0 {
		return 0, errors.New("no admin account available")
	}
	return id, nil
}
