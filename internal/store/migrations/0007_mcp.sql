-- MCP tokens and tool-call audit.

CREATE TABLE IF NOT EXISTS mcp_tokens (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    scopes TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    created_by_admin_id INTEGER REFERENCES admin_users(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER,
    last_used_at INTEGER,
    revoked_at INTEGER,
    rate_limit_per_minute INTEGER
);
CREATE INDEX IF NOT EXISTS mcp_tokens_status_idx ON mcp_tokens(status, expires_at);

CREATE TABLE IF NOT EXISTS mcp_tool_calls (
    id INTEGER PRIMARY KEY,
    token_id INTEGER REFERENCES mcp_tokens(id) ON DELETE SET NULL,
    tool TEXT NOT NULL,
    scope TEXT,
    args_summary TEXT,
    status TEXT NOT NULL CHECK (status IN ('ok', 'error')),
    error_code TEXT,
    duration_ms INTEGER,
    ip TEXT,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS mcp_tool_calls_token_idx ON mcp_tool_calls(token_id, created_at DESC);
CREATE INDEX IF NOT EXISTS mcp_tool_calls_tool_idx ON mcp_tool_calls(tool, created_at DESC);
