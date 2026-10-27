package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tiliweb/internal/config"
	"tiliweb/internal/content"
	"tiliweb/internal/forum"
	"tiliweb/internal/site"
	"tiliweb/internal/store/sqlite"
)

func testServer(t *testing.T) (*Server, *config.Config, context.Context) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.DataDir = filepath.Join(dir, "data")
	cfg.Storage.MediaDir = filepath.Join(dir, "media")
	cfg.Storage.DocumentDir = filepath.Join(dir, "docs")
	cfg.Storage.BackupDir = filepath.Join(dir, "backups")
	cfg.Storage.LogDir = filepath.Join(dir, "logs")
	cfg.Database.DSN = filepath.Join(dir, "db", "tiliweb.db")
	cfg.MCP.Enabled = true
	cfg.MCP.AllowWrite = true
	cfg.MCP.AllowAdminScope = true
	cfg.MCP.Stdio.Enabled = true
	cfg.MCP.HTTP.Enabled = false
	if err := cfg.CheckPaths(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := sqlite.Open(ctx, cfg.Database.DSN, 4, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO admin_users(username,password_hash,display_name,role,status,created_at,updated_at)
		VALUES('admin','x','Admin','superadmin','active',1,1)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(&cfg, db, content.NewService(db), forum.New(db, &cfg), site.NewStore(db), logger)
	return srv, &cfg, ctx
}

func TestMCPTokenAndTools(t *testing.T) {
	srv, _, ctx := testServer(t)
	readToken, _, err := srv.Tokens().Create(ctx, "read", []string{"read"}, time.Hour, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := srv.Tokens().Create(ctx, "admin", []string{"read", "admin"}, time.Hour, 0, 1)
	if err != nil {
		t.Fatal(err)
	}

	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list"}
	resp := srv.Handle(ctx, req, AuthContext{Scopes: []string{"read"}})
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/list = %+v", resp)
	}
	result, _ := resp.Result.(map[string]any)
	tools, _ := result["tools"].([]map[string]any)
	if len(tools) < 10 {
		t.Fatalf("tools = %d", len(tools))
	}

	call := func(token, name string, args map[string]any) map[string]any {
		params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
		resp := srv.Handle(ctx, Request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "tools/call", Params: params},
			AuthContext{Scopes: scopesForToken(ctx, t, srv, token)})
		m, _ := resp.Result.(map[string]any)
		return m
	}
	if got := call(readToken, "tiliweb.site.status", nil); got["isError"] != false {
		t.Fatalf("site.status = %+v", got)
	}
	if got := call(readToken, "tiliweb.content.entries.create", map[string]any{"channel_id": 1, "title": "x"}); got["isError"] != true {
		t.Fatalf("read write should fail: %+v", got)
	}
	if got := call(adminToken, "tiliweb.settings.update", map[string]any{"site_name": "MCP Site"}); got["isError"] != false {
		t.Fatalf("settings.update = %+v", got)
	}

	var calls int
	_ = srv.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_tool_calls`).Scan(&calls)
	if calls == 0 {
		t.Fatal("no mcp_tool_calls recorded")
	}
}

func scopesForToken(ctx context.Context, t *testing.T, srv *Server, plain string) []string {
	t.Helper()
	tok, err := srv.Tokens().Authenticate(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	return tok.Scopes
}

func TestMCPHTTPAuthAndAudit(t *testing.T) {
	srv, _, ctx := testServer(t)
	readToken, _, err := srv.Tokens().Create(ctx, "read", []string{"read"}, time.Hour, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+readToken)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tiliweb.site.status") {
		t.Fatalf("read token status=%d body=%s", rec.Code, rec.Body.String())
	}

	body := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tiliweb.content.entries.create","arguments":{"channel_id":1,"title":"x"}}}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+readToken)
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"isError":true`) {
		t.Fatalf("read write = %s", rec.Body.String())
	}

	var audits int
	_ = srv.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='mcp.auth_failed'`).Scan(&audits)
	if audits == 0 {
		t.Fatal("missing mcp.auth_failed audit")
	}
}
