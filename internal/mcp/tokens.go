package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"tiliweb/internal/config"
)

var ErrUnauthorized = errors.New("mcp: unauthorized")

var KnownScopes = []string{
	"read", "content.write", "comment.moderate", "forum.moderate",
	"user.moderate", "maintenance", "backup", "admin",
}

var writeScopes = map[string]bool{
	"content.write": true, "comment.moderate": true, "forum.moderate": true,
	"user.moderate": true, "maintenance": true, "backup": true, "admin": true,
}

type Token struct {
	ID         int64
	Name       string
	Prefix     string
	Scopes     []string
	CreatedBy  int64
	CreatedAt  int64
	ExpiresAt  int64
	LastUsedAt int64
	RateLimit  int
	Status     string
}

type TokenStore struct {
	db *sql.DB
}

func NewTokenStore(db *sql.DB) *TokenStore { return &TokenStore{db: db} }

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NormalizeScopes(scopes []string) ([]string, error) {
	seen := map[string]bool{"read": true}
	out := []string{"read"}
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" || s == "read" {
			continue
		}
		known := false
		for _, k := range KnownScopes {
			if s == k {
				known = true
				break
			}
		}
		if !known {
			return nil, errors.New("unknown MCP scope: " + s)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out, nil
}

func CheckScopesForConfig(cfg *config.Config, scopes []string) error {
	for _, s := range scopes {
		if s == "admin" {
			if !cfg.MCP.AllowWrite || !cfg.MCP.AllowAdminScope {
				return errors.New("admin scope requires mcp.allow_write=true and mcp.allow_admin_scope=true")
			}
			continue
		}
		if writeScopes[s] && !cfg.MCP.AllowWrite {
			return errors.New("write scope " + s + " requires mcp.allow_write=true")
		}
	}
	return nil
}

func HasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == "admin" || s == want {
			return true
		}
	}
	return false
}

func (s *TokenStore) Create(ctx context.Context, name string, scopes []string, ttl time.Duration, rateLimit int, adminID int64) (string, *Token, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, errors.New("token name is required")
	}
	scopes, err := NormalizeScopes(scopes)
	if err != nil {
		return "", nil, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	plain := "tili_mcp_" + base64.RawURLEncoding.EncodeToString(raw)
	hash := HashToken(plain)
	prefix := plain
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	now := time.Now().UTC().Unix()
	var expires any
	if ttl > 0 {
		expires = time.Now().UTC().Add(ttl).Unix()
	}
	scopeJSON, _ := json.Marshal(scopes)
	res, err := s.db.ExecContext(ctx, `INSERT INTO mcp_tokens(name, token_prefix, token_hash, scopes, status,
		created_by_admin_id, created_at, expires_at, rate_limit_per_minute)
		VALUES(?, ?, ?, ?, 'active', ?, ?, ?, ?)`, name, prefix, hash, string(scopeJSON), nullID(adminID), now, expires, nullRate(rateLimit))
	if err != nil {
		return "", nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return "", nil, err
	}
	return plain, &Token{ID: id, Name: name, Prefix: prefix, Scopes: scopes, CreatedBy: adminID,
		CreatedAt: now, ExpiresAt: int64OrZero(expires), RateLimit: rateLimit, Status: "active"}, nil
}

func (s *TokenStore) Authenticate(ctx context.Context, plain string) (*Token, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return nil, ErrUnauthorized
	}
	var t Token
	var scopes string
	var expires, last, rate sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, token_prefix, scopes, status, COALESCE(created_by_admin_id,0),
		created_at, expires_at, last_used_at, rate_limit_per_minute
		FROM mcp_tokens WHERE token_hash = ?`, HashToken(plain)).
		Scan(&t.ID, &t.Name, &t.Prefix, &scopes, &t.Status, &t.CreatedBy, &t.CreatedAt, &expires, &last, &rate)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if t.Status != "active" {
		return nil, ErrUnauthorized
	}
	t.ExpiresAt, t.LastUsedAt, t.RateLimit = expires.Int64, last.Int64, int(rate.Int64)
	if t.ExpiresAt > 0 && t.ExpiresAt <= time.Now().UTC().Unix() {
		return nil, ErrUnauthorized
	}
	_ = json.Unmarshal([]byte(scopes), &t.Scopes)
	if len(t.Scopes) == 0 {
		t.Scopes = []string{"read"}
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE mcp_tokens SET last_used_at = ? WHERE id = ?`, time.Now().UTC().Unix(), t.ID)
	return &t, nil
}

func (s *TokenStore) List(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, token_prefix, scopes, status, COALESCE(created_by_admin_id,0),
		created_at, COALESCE(expires_at,0), COALESCE(last_used_at,0), COALESCE(rate_limit_per_minute,0)
		FROM mcp_tokens ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		var scopes string
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &scopes, &t.Status, &t.CreatedBy,
			&t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RateLimit); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(scopes), &t.Scopes)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *TokenStore) Revoke(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE mcp_tokens SET status = 'revoked', revoked_at = ? WHERE id = ?`,
		time.Now().UTC().Unix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("token not found")
	}
	return nil
}

func nullID(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullRate(v int) any {
	if v <= 0 {
		return nil
	}
	return v
}

func int64OrZero(v any) int64 {
	if n, ok := v.(int64); ok {
		return n
	}
	return 0
}
