package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tiliweb/internal/content"
	"tiliweb/internal/version"
)

func (s *Server) registerTools() {
	add := func(t Tool) { s.tools[t.Name] = t }
	add(Tool{Name: "tiliweb.site.status", Scope: "read", Description: "Site version, uptime, DB size and content counts.",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.siteStatus(ctx)
		}})
	add(Tool{Name: "tiliweb.site.health", Scope: "read", Description: "DB ping, schema and FTS health.",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.siteHealth(ctx)
		}})
	add(Tool{Name: "tiliweb.config.summary", Scope: "read", Description: "Redacted effective configuration.",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.configSummary(), nil
		}})
	add(Tool{Name: "tiliweb.content.entries.list", Scope: "read", Description: "List content entries.",
		InputSchema: schema(map[string]any{"channel_id": map[string]any{"type": "integer"}, "status": map[string]any{"type": "string"}, "include_deleted": map[string]any{"type": "boolean"}, "limit": map[string]any{"type": "integer"}}),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			limit := clamp(argInt(args, "limit", 50), 1, s.cfg.MCP.MaxRows)
			return s.content.ListEntries(ctx, argInt64(args, "channel_id", 0), argString(args, "status"), argBool(args, "include_deleted"), limit, 0)
		}})
	add(Tool{Name: "tiliweb.content.entries.get", Scope: "read", Description: "Get one entry by id.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.content.GetEntry(ctx, argInt64(args, "id", 0))
		}})
	add(Tool{Name: "tiliweb.content.channels.list", Scope: "read", Description: "List channels.",
		InputSchema: schema(map[string]any{"include_hidden": map[string]any{"type": "boolean"}}),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.content.ListChannels(ctx, argBool(args, "include_hidden"))
		}})
	add(Tool{Name: "tiliweb.content.pages.list", Scope: "read", Description: "List pages.",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.content.ListPages(ctx, false)
		}})
	add(Tool{Name: "tiliweb.content.pages.get", Scope: "read", Description: "Get a page by id.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.content.PageByID(ctx, argInt64(args, "id", 0))
		}})
	add(Tool{Name: "tiliweb.content.tags.list", Scope: "read", Description: "List tags.",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.content.ListTags(ctx)
		}})
	add(Tool{Name: "tiliweb.comments.list", Scope: "read", Description: "List comments.",
		InputSchema: schema(map[string]any{"status": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"}}),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.content.ListComments(ctx, argString(args, "status"), clamp(argInt(args, "limit", 50), 1, s.cfg.MCP.MaxRows))
		}})
	add(Tool{Name: "tiliweb.forum.list_boards", Scope: "read", Description: "List forum boards.",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.forum.ListBoards(ctx, false)
		}})
	add(Tool{Name: "tiliweb.forum.list_topics", Scope: "read", Description: "List topics in a board.",
		InputSchema: schema(map[string]any{"board_id": map[string]any{"type": "integer"}, "limit": map[string]any{"type": "integer"}}, "board_id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.forum.ListTopics(ctx, argInt64(args, "board_id", 0), clamp(argInt(args, "limit", 20), 1, s.cfg.MCP.MaxRows), 0)
		}})
	add(Tool{Name: "tiliweb.forum.get_topic", Scope: "read", Description: "Get a topic and visible posts.",
		InputSchema: schema(map[string]any{"topic_id": map[string]any{"type": "integer"}}, "topic_id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			id := argInt64(args, "topic_id", 0)
			t, err := s.forum.TopicByID(ctx, id)
			if err != nil {
				return nil, err
			}
			posts, err := s.forum.Posts(ctx, id, 0, false)
			return map[string]any{"topic": t, "posts": posts}, err
		}})
	add(Tool{Name: "tiliweb.search.query", Scope: "read", Description: "Search public content and forum topics.",
		InputSchema: schema(map[string]any{"q": map[string]any{"type": "string"}, "type": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"}}, "q"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.content.Search(ctx, argString(args, "q"), argString(args, "type"), clamp(argInt(args, "limit", 20), 1, s.cfg.MCP.MaxRows), 0)
		}})
	add(Tool{Name: "tiliweb.audit.tail", Scope: "read", Description: "Recent audit events.",
		InputSchema: schema(map[string]any{"limit": map[string]any{"type": "integer"}}),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.auditTail(ctx, clamp(argInt(args, "limit", 20), 1, s.cfg.MCP.MaxRows))
		}})
	add(Tool{Name: "tiliweb.settings.get", Scope: "read", Description: "Read site settings (redacted).",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.site.Load(ctx, s.cfg)
		}})
	add(Tool{Name: "tiliweb.media.list", Scope: "read", Description: "List active media.",
		InputSchema: schema(map[string]any{"limit": map[string]any{"type": "integer"}}),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.content.Store().ListMedia(ctx, clamp(argInt(args, "limit", 50), 1, s.cfg.MCP.MaxRows))
		}})
	add(Tool{Name: "tiliweb.db.stats", Scope: "read", Description: "SQLite size and table counts.",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.dbStats(ctx)
		}})
	add(Tool{Name: "tiliweb.backup.list", Scope: "backup", Description: "List backup files.",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.backupList()
		}})

	add(Tool{Name: "tiliweb.content.entries.create", Scope: "content.write", Write: true, Description: "Create a content entry.",
		InputSchema: schema(map[string]any{"channel_id": map[string]any{"type": "integer"}, "title": map[string]any{"type": "string"}, "body_md": map[string]any{"type": "string"}, "summary": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"}, "tags": map[string]any{"type": "array"}}, "channel_id", "title"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			adminID, err := s.requireAdmin(ctx, AuthContext{})
			if err != nil {
				return nil, err
			}
			e, err := s.content.CreateEntry(ctx, adminID, content.EntryInput{
				ChannelID: argInt64(args, "channel_id", 0), Title: argString(args, "title"),
				Slug: argString(args, "slug"), Summary: argString(args, "summary"),
				BodyMD: argString(args, "body_md"), Status: argString(args, "status"),
				Tags: argStrings(args, "tags"),
			})
			return e, err
		}})
	add(Tool{Name: "tiliweb.content.entries.update", Scope: "content.write", Write: true, Description: "Update an entry.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}, "title": map[string]any{"type": "string"}, "body_md": map[string]any{"type": "string"}, "summary": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"}}, "id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			adminID, err := s.requireAdmin(ctx, AuthContext{})
			if err != nil {
				return nil, err
			}
			e, err := s.content.GetEntry(ctx, argInt64(args, "id", 0))
			if err != nil {
				return nil, err
			}
			in := content.EntryInput{ChannelID: e.ChannelID, Slug: e.Slug, Title: e.Title,
				Summary: e.Summary, BodyMD: e.BodyMD, CoverMediaID: e.CoverMediaID, Status: e.Status,
				Pinned: e.Pinned, Featured: e.Featured, SortOrder: e.SortOrder, ExtraJSON: e.ExtraJSON}
			for _, t := range e.Tags {
				in.Tags = append(in.Tags, t.Name)
			}
			if v, ok := args["title"].(string); ok && v != "" {
				in.Title = v
			}
			if v, ok := args["body_md"].(string); ok {
				in.BodyMD = v
			}
			if v, ok := args["summary"].(string); ok {
				in.Summary = v
			}
			if v, ok := args["status"].(string); ok && v != "" {
				in.Status = v
			}
			return s.content.UpdateEntry(ctx, adminID, e.ID, in)
		}})
	add(Tool{Name: "tiliweb.content.entries.publish", Scope: "content.write", Write: true, Description: "Publish an entry.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			adminID, err := s.requireAdmin(ctx, AuthContext{})
			if err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, s.content.SetEntryStatus(ctx, adminID, argInt64(args, "id", 0), "published")
		}})
	add(Tool{Name: "tiliweb.content.entries.unpublish", Scope: "content.write", Write: true, Description: "Unpublish an entry.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			adminID, err := s.requireAdmin(ctx, AuthContext{})
			if err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, s.content.SetEntryStatus(ctx, adminID, argInt64(args, "id", 0), "draft")
		}})
	add(Tool{Name: "tiliweb.content.entries.delete", Scope: "content.write", Write: true, Description: "Soft-delete an entry.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{"ok": true}, s.content.DeleteEntry(ctx, argInt64(args, "id", 0))
		}})
	add(Tool{Name: "tiliweb.content.entries.restore", Scope: "content.write", Write: true, Description: "Restore a soft-deleted entry.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{"ok": true}, s.content.RestoreEntry(ctx, argInt64(args, "id", 0))
		}})
	add(Tool{Name: "tiliweb.comments.set_status", Scope: "comment.moderate", Write: true, Description: "Set comment status.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}, "status": map[string]any{"type": "string"}}, "id", "status"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{"ok": true}, s.content.SetCommentStatus(ctx, argInt64(args, "id", 0), argString(args, "status"))
		}})
	add(Tool{Name: "tiliweb.forum.moderate_topic", Scope: "forum.moderate", Write: true, Description: "Pin/lock/delete/restore a topic.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}, "action": map[string]any{"type": "string"}}, "id", "action"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{"ok": true}, s.forum.ModerateTopic(ctx, argInt64(args, "id", 0), argString(args, "action"))
		}})
	add(Tool{Name: "tiliweb.users.set_status", Scope: "user.moderate", Write: true, Description: "Enable or disable a front user.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}, "status": map[string]any{"type": "string"}}, "id", "status"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			status := argString(args, "status")
			if status != "active" && status != "disabled" {
				return nil, fmt.Errorf("status must be active or disabled")
			}
			_, err := s.db.ExecContext(ctx, `UPDATE users SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now().Unix(), argInt64(args, "id", 0))
			return map[string]any{"ok": err == nil}, err
		}})
	add(Tool{Name: "tiliweb.media.delete", Scope: "content.write", Write: true, Description: "Soft-delete media metadata.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{"ok": true}, s.content.Store().DeleteMedia(ctx, argInt64(args, "id", 0))
		}})
	add(Tool{Name: "tiliweb.search.reindex", Scope: "maintenance", Write: true, Description: "Rebuild content FTS index.",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{"ok": true}, s.content.RebuildSearch(ctx)
		}})
	add(Tool{Name: "tiliweb.sessions.cleanup", Scope: "maintenance", Write: true, Description: "Delete expired admin/front sessions.",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.cleanupSessions(ctx)
		}})
	add(Tool{Name: "tiliweb.settings.update", Scope: "admin", Write: true, Description: "Update site name/contact settings.",
		InputSchema: schema(map[string]any{"site_name": map[string]any{"type": "string"}, "site_description": map[string]any{"type": "string"}, "contact_email": map[string]any{"type": "string"}, "contact_phone": map[string]any{"type": "string"}, "contact_address": map[string]any{"type": "string"}}),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			settings, err := s.site.Load(ctx, s.cfg)
			if err != nil {
				return nil, err
			}
			if v := argString(args, "site_name"); v != "" {
				settings.SiteName = v
			}
			if v := argString(args, "site_description"); v != "" {
				settings.SiteDescription = v
			}
			if v := argString(args, "contact_email"); v != "" {
				settings.Contact.Email = v
			}
			if v := argString(args, "contact_phone"); v != "" {
				settings.Contact.Phone = v
			}
			if v := argString(args, "contact_address"); v != "" {
				settings.Contact.Address = v
			}
			return map[string]any{"ok": true}, s.site.Save(ctx, settings)
		}})
	add(Tool{Name: "tiliweb.mcp.tokens.list", Scope: "admin", Description: "List MCP tokens (no secrets).",
		InputSchema: schema(nil), Run: func(ctx context.Context, args map[string]any) (any, error) {
			return s.tokens.List(ctx)
		}})
	add(Tool{Name: "tiliweb.mcp.tokens.create", Scope: "admin", Write: true, Description: "Create an MCP token; returns it once.",
		InputSchema: schema(map[string]any{"name": map[string]any{"type": "string"}, "scopes": map[string]any{"type": "array"}, "ttl_hours": map[string]any{"type": "integer"}}, "name"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			scopes := argStrings(args, "scopes")
			if len(scopes) == 0 {
				scopes = []string{"read"}
			}
			if err := CheckScopesForConfig(s.cfg, scopes); err != nil {
				return nil, err
			}
			adminID, err := s.requireAdmin(ctx, AuthContext{})
			if err != nil {
				return nil, err
			}
			ttl := s.cfg.MCP.TokenTTL.Std()
			if h := argInt(args, "ttl_hours", 0); h > 0 {
				ttl = time.Duration(h) * time.Hour
			}
			token, t, err := s.tokens.Create(ctx, argString(args, "name"), scopes, ttl, 0, adminID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"token": token, "id": t.ID, "prefix": t.Prefix, "scopes": t.Scopes}, nil
		}})
	add(Tool{Name: "tiliweb.mcp.tokens.revoke", Scope: "admin", Write: true, Description: "Revoke an MCP token.",
		InputSchema: schema(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
		Run: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{"ok": true}, s.tokens.Revoke(ctx, argInt64(args, "id", 0))
		}})
}

func (s *Server) siteStatus(ctx context.Context) (any, error) {
	var channels, entries, pages, users int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channels`).Scan(&channels)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE deleted_at IS NULL`).Scan(&entries)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pages WHERE deleted_at IS NULL`).Scan(&pages)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&users)
	var dbSize int64
	_ = s.db.QueryRowContext(ctx, `SELECT (SELECT page_count FROM pragma_page_count()) * (SELECT page_size FROM pragma_page_size())`).Scan(&dbSize)
	var schema string
	_ = s.db.QueryRowContext(ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&schema)
	return map[string]any{"version": version.String(), "uptime_seconds": int64(time.Since(s.started).Seconds()),
		"db_size": dbSize, "schema": schema, "channels": channels, "entries": entries, "pages": pages, "users": users}, nil
}

func (s *Server) siteHealth(ctx context.Context) (any, error) {
	healthy := true
	if err := s.db.PingContext(ctx); err != nil {
		healthy = false
	}
	var fts int
	ftsErr := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries_fts`).Scan(&fts)
	return map[string]any{"healthy": healthy && ftsErr == nil, "fts": ftsErr == nil, "fts_rows": fts}, nil
}

func (s *Server) configSummary() any {
	return map[string]any{"listen": s.cfg.Server.Listen, "base_url": s.cfg.Server.BaseURL,
		"base_path": s.cfg.Server.BasePath, "site_name": s.cfg.Site.Name,
		"mcp_enabled": s.cfg.MCP.Enabled, "allow_write": s.cfg.MCP.AllowWrite,
		"database": filepath.Base(s.cfg.Database.DSN)}
}

func (s *Server) auditTail(ctx context.Context, limit int) (any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, actor_type, COALESCE(actor_id,0), action, COALESCE(target_type,''),
		COALESCE(target_id,0), COALESCE(ip,''), COALESCE(meta,''), created_at FROM audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, actorID, targetID, created int64
		var actor, action, target, ip, meta string
		if err := rows.Scan(&id, &actor, &actorID, &action, &target, &targetID, &ip, &meta, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "actor": actor, "actor_id": actorID, "action": action,
			"target": target, "target_id": targetID, "ip": ip, "meta": meta, "created_at": created})
	}
	return out, rows.Err()
}

func (s *Server) dbStats(ctx context.Context) (any, error) {
	var size int64
	if err := s.db.QueryRowContext(ctx, `SELECT (SELECT page_count FROM pragma_page_count()) * (SELECT page_size FROM pragma_page_size())`).Scan(&size); err != nil {
		return nil, err
	}
	return map[string]any{"size": size}, nil
}

func (s *Server) backupList() (any, error) {
	entries, err := os.ReadDir(s.cfg.Storage.BackupDir)
	if err != nil {
		return []any{}, nil
	}
	var out []map[string]any
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info == nil {
			continue
		}
		out = append(out, map[string]any{"name": e.Name(), "size": info.Size(), "mod_time": info.ModTime().Unix()})
	}
	return out, nil
}

func (s *Server) cleanupSessions(ctx context.Context) (any, error) {
	now := time.Now().Unix()
	admin, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at < ?`, now)
	if err != nil {
		return nil, err
	}
	user, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE expires_at < ?`, now)
	if err != nil {
		return nil, err
	}
	a, _ := admin.RowsAffected()
	u, _ := user.RowsAffected()
	return map[string]any{"admin_sessions": a, "user_sessions": u}, nil
}

func schema(props map[string]any, required ...string) map[string]any {
	out := map[string]any{"type": "object"}
	if props != nil {
		out["properties"] = props
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func argString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func argInt(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := strconv.Atoi(v.String())
		return n
	}
	return def
}

func argInt64(args map[string]any, key string, def int64) int64 {
	switch v := args[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return def
}

func argBool(args map[string]any, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func argStrings(args map[string]any, key string) []string {
	switch v := args[key].(type) {
	case []any:
		var out []string
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return strings.Split(v, ",")
	}
	return nil
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
