package mcp

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"tiliweb/internal/config"
	"tiliweb/internal/content"
	"tiliweb/internal/forum"
	"tiliweb/internal/site"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func rpcError(id json.RawMessage, code int, message string) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: message}}
}

func isNotification(id json.RawMessage) bool {
	return len(id) == 0 || string(id) == "null"
}

// RunStdio serves newline-delimited JSON-RPC over stdin/stdout.
func RunStdio(ctx context.Context, cfg *config.Config, db *sql.DB, contentSvc *content.Service, forumSvc *forum.Service, siteStore *site.Store, logger *slog.Logger) error {
	if !cfg.MCP.Enabled || !cfg.MCP.Stdio.Enabled {
		return fmt.Errorf("MCP stdio is disabled")
	}
	scopes, err := NormalizeScopes(cfg.MCP.Stdio.DefaultScopes)
	if err != nil {
		return err
	}
	if err := CheckScopesForConfig(cfg, scopes); err != nil {
		return err
	}
	srv := New(cfg, db, contentSvc, forumSvc, siteStore, logger)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	enc := json.NewEncoder(os.Stdout)
	auth := AuthContext{Scopes: scopes}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(rpcError(nil, -32700, "parse error"))
			continue
		}
		resp := srv.Handle(ctx, req, auth)
		if resp == nil || isNotification(req.ID) {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}
