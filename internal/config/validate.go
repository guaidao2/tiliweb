package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	cookieNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

	knownScopes = map[string]bool{
		"read":             true,
		"content.write":    true,
		"comment.moderate": true,
		"forum.moderate":   true,
		"user.moderate":    true,
		"maintenance":      true,
		"backup":           true,
		"admin":            true,
	}

	reservedFrontSlugs = map[string]bool{
		"login": true, "register": true, "logout": true, "account": true,
		"forum": true, "search": true, "contact": true, "links": true,
		"media": true, "download": true, "rss.xml": true, "sitemap.xml": true,
		"robots.txt": true, "healthz": true, "readyz": true, "api": true,
		"static": true, "assets": true, "favicon.ico": true, "password": true,
		"tags": true, "mcp": true,
	}

	reservedRootPaths = map[string]bool{
		"healthz": true, "readyz": true, "robots.txt": true,
		"admin": true, "static": true,
	}
)

// Validate performs static validation. It never touches the filesystem;
// CheckPaths handles directories and certificate files.
func (c *Config) Validate() error {
	var errs []string
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	if _, _, err := parseHostPort(c.Server.Listen); err != nil {
		add("server.listen: %v", err)
	}
	if err := validateBaseURL(c.Server.BaseURL); err != nil {
		add("server.base_url: %v", err)
	}
	if err := validateBasePath(c.Server.BasePath); err != nil {
		add("server.base_path: %v", err)
	}
	for _, p := range c.Server.TrustedProxies {
		if err := validateIPOrCIDR(p); err != nil {
			add("server.trusted_proxies %q: %v", p, err)
		}
	}
	if c.Server.MaxHeaderBytes <= 0 {
		add("server.max_header_bytes must be > 0")
	}
	if c.Server.MaxBodyBytes <= 0 {
		add("server.max_body_bytes must be > 0")
	}
	for name, d := range map[string]Duration{
		"server.read_header_timeout": c.Server.ReadHeaderTimeout,
		"server.read_timeout":        c.Server.ReadTimeout,
		"server.write_timeout":       c.Server.WriteTimeout,
		"server.idle_timeout":        c.Server.IdleTimeout,
		"server.shutdown_timeout":    c.Server.ShutdownTimeout,
	} {
		if d.Std() <= 0 {
			add("%s must be > 0", name)
		}
	}
	if c.Server.TLS.Enabled {
		if c.Server.TLS.CertFile == "" || c.Server.TLS.KeyFile == "" {
			add("server.tls.cert_file and key_file are required when TLS is enabled")
		}
	}

	if c.Database.Driver != "sqlite" {
		add("database.driver must be sqlite in v0.1")
	}
	if strings.TrimSpace(c.Database.DSN) == "" {
		add("database.dsn is required")
	}
	if c.Database.MaxOpenConns <= 0 {
		add("database.max_open_conns must be > 0")
	}
	if c.Database.BusyTimeout <= 0 {
		add("database.busy_timeout must be > 0")
	}

	for name, dir := range map[string]string{
		"storage.data_dir":     c.Storage.DataDir,
		"storage.media_dir":    c.Storage.MediaDir,
		"storage.document_dir": c.Storage.DocumentDir,
		"storage.backup_dir":   c.Storage.BackupDir,
		"storage.log_dir":      c.Storage.LogDir,
	} {
		if strings.TrimSpace(dir) == "" {
			add("%s is required", name)
			continue
		}
		if isRootPath(dir) {
			add("%s must not be a filesystem root", name)
		}
	}

	if err := validateAdminPath(c.Admin.Path); err != nil {
		add("admin.path: %v", err)
	}
	if c.Admin.SessionTTL.Std() <= 0 || c.Admin.SessionIdle.Std() <= 0 {
		add("admin session_ttl/session_idle must be > 0")
	} else if c.Admin.SessionIdle.Std() >= c.Admin.SessionTTL.Std() {
		add("admin.session_idle must be < admin.session_ttl")
	}
	if c.Admin.PasswordMinLength < 8 {
		add("admin.password_min_length must be >= 8")
	}
	if c.Admin.LoginRatePerMinute <= 0 || c.Admin.LockoutThreshold <= 0 || c.Admin.LockoutDuration.Std() <= 0 {
		add("admin login rate/lockout settings must be > 0")
	}

	if c.Auth.SessionTTL.Std() <= 0 || c.Auth.SessionIdle.Std() <= 0 {
		add("auth session_ttl/session_idle must be > 0")
	} else if c.Auth.SessionIdle.Std() >= c.Auth.SessionTTL.Std() {
		add("auth.session_idle must be < auth.session_ttl")
	}
	if c.Auth.PasswordMinLength < 8 {
		add("auth.password_min_length must be >= 8")
	}
	switch c.Auth.RegisterMode {
	case "open", "approval", "invite":
	default:
		add("auth.register_mode must be open, approval or invite")
	}
	if c.Auth.LoginRatePerMinute <= 0 || c.Auth.LockoutThreshold <= 0 || c.Auth.LockoutDuration.Std() <= 0 {
		add("auth login rate/lockout settings must be > 0")
	}

	if c.Forum.BoardsPerPage <= 0 || c.Forum.TopicsPerPage <= 0 || c.Forum.PostsPerPage <= 0 {
		add("forum page sizes must be > 0")
	}
	if c.Forum.EditWindow.Std() <= 0 {
		add("forum.edit_window must be > 0")
	}
	if c.Forum.NewUserModerationPosts < 0 {
		add("forum.new_user_moderation_posts must be >= 0")
	}

	if c.Content.NewsPerPage <= 0 || c.Content.ProductsPerPage <= 0 || c.Content.CasesPerPage <= 0 ||
		c.Content.JobsPerPage <= 0 || c.Content.DownloadsPerPage <= 0 || c.Content.CommentsPerPage <= 0 {
		add("content page sizes must be > 0")
	}

	if c.Mail.Enabled {
		if c.Mail.Host == "" || c.Mail.From == "" {
			add("mail.host and mail.from are required when mail is enabled")
		}
		if c.Mail.Port < 1 || c.Mail.Port > 65535 {
			add("mail.port must be 1..65535")
		}
		switch c.Mail.TLS {
		case "starttls", "ssl", "none":
		default:
			add("mail.tls must be starttls, ssl or none")
		}
	}

	if c.Uploads.MaxImageBytes <= 0 || c.Uploads.MaxDocumentBytes <= 0 || c.Uploads.ImageMaxPixels <= 0 {
		add("uploads.max_* and image_max_pixels must be > 0")
	}
	if c.Server.MaxBodyBytes < c.Uploads.MaxImageBytes || c.Server.MaxBodyBytes < c.Uploads.MaxDocumentBytes {
		add("server.max_body_bytes must be >= uploads.max_image_bytes and uploads.max_document_bytes")
	}
	if len(c.Uploads.AllowedImageTypes) == 0 || len(c.Uploads.AllowedDocumentExts) == 0 {
		add("uploads.allowed_* must not be empty")
	}
	for _, ext := range c.Uploads.AllowedDocumentExts {
		if !strings.HasPrefix(ext, ".") {
			add("uploads.allowed_document_exts %q must start with '.'", ext)
		}
	}

	if c.Log.RingBufferLines <= 0 {
		add("log.ring_buffer_lines must be > 0")
	}
	if c.Log.MaxSizeMB < 0 || c.Log.MaxBackups < 0 {
		add("log.max_size_mb and log.max_backups must be >= 0")
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		add("log.level must be debug, info, warn or error")
	}
	switch c.Log.Format {
	case "text", "json":
	default:
		add("log.format must be text or json")
	}

	if c.Audit.Enabled && c.Audit.RetentionDays <= 0 {
		add("audit.retention_days must be > 0")
	}
	if c.RateLimit.Enabled {
		if c.RateLimit.ContactPerHour <= 0 || c.RateLimit.RegisterPerHour <= 0 ||
			c.RateLimit.PostPerMinute <= 0 || c.RateLimit.UploadPerMinute <= 0 {
			add("rate_limit values must be > 0")
		}
	}

	if !cookieNameRE.MatchString(c.Admin.CookieName) {
		add("admin.cookie_name is not a valid cookie name")
	}
	if !cookieNameRE.MatchString(c.Auth.CookieName) {
		add("auth.cookie_name is not a valid cookie name")
	}
	if !cookieNameRE.MatchString(c.Security.CSRFCookieName) {
		add("security.csrf_cookie_name is not a valid cookie name")
	}
	if c.Admin.CookieName == c.Auth.CookieName ||
		c.Admin.CookieName == c.Security.CSRFCookieName ||
		c.Auth.CookieName == c.Security.CSRFCookieName {
		add("admin/auth/csrf cookie names must be distinct")
	}
	if c.Security.HSTSMaxAge < 0 {
		add("security.hsts_max_age must be >= 0")
	}
	if strings.ContainsAny(c.Security.ServerHeader, "\r\n") {
		add("security.server_header must not contain CR/LF")
	}

	if err := c.validateMCP(); err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid config: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (c *Config) validateMCP() error {
	var errs []string
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	if c.MCP.AllowAdminScope && !c.MCP.AllowWrite {
		add("mcp.allow_admin_scope requires mcp.allow_write=true")
	}
	if c.MCP.HTTP.Enabled && !c.MCP.Enabled {
		add("mcp.http.enabled requires mcp.enabled=true")
	}
	if c.MCP.Enabled && !c.MCP.Stdio.Enabled && !c.MCP.HTTP.Enabled {
		add("mcp.enabled requires stdio or http transport")
	}
	if c.MCP.Stdio.Enabled {
		if err := validateScopes(c.MCP.Stdio.DefaultScopes, c.MCP.AllowWrite, c.MCP.AllowAdminScope); err != nil {
			add("mcp.stdio.default_scopes: %v", err)
		}
	}

	if c.MCP.HTTP.Enabled {
		host, _, err := parseHostPort(c.MCP.HTTP.Listen)
		if err != nil {
			add("mcp.http.listen: %v", err)
		}
		if c.MCP.HTTP.Listen == c.Server.Listen {
			add("mcp.http.listen must differ from server.listen")
		}
		loopback := isLoopbackHost(host)
		if !c.MCP.HTTP.Public && !loopback {
			add("mcp.http.public=false cannot bind non-loopback address %q", c.MCP.HTTP.Listen)
		}
		if c.MCP.HTTP.Public && !loopback && !c.MCP.HTTP.TLS.Enabled {
			add("mcp.http.public=true with non-loopback listen requires mcp.http.tls.enabled=true")
		}
		if c.MCP.HTTP.TLS.Enabled {
			if c.MCP.HTTP.TLS.CertFile == "" || c.MCP.HTTP.TLS.KeyFile == "" {
				add("mcp.http.tls.cert_file and key_file are required when TLS is enabled")
			}
		}
		for _, p := range c.MCP.HTTP.AllowedIPs {
			if err := validateIPOrCIDR(p); err != nil {
				add("mcp.http.allowed_ips %q: %v", p, err)
			}
		}
		if c.MCP.HTTP.AuthRatePerMinute <= 0 || c.MCP.HTTP.AuthLockout.Std() <= 0 {
			add("mcp.http.auth_rate_per_minute/auth_lockout must be > 0")
		}
		if c.MCP.HTTP.MaxBodyBytes <= 0 || c.MCP.HTTP.ReadTimeout.Std() <= 0 ||
			c.MCP.HTTP.WriteTimeout.Std() <= 0 || c.MCP.HTTP.MaxSSEConnections <= 0 {
			add("mcp.http body/timeout/max_sse_connections must be > 0")
		}
		if c.MCP.LongTaskTimeout.Std() >= c.MCP.HTTP.WriteTimeout.Std() {
			add("mcp.long_task_timeout must be < mcp.http.write_timeout")
		}
	}

	if c.MCP.Enabled {
		if c.MCP.TokenTTL.Std() <= 0 || c.MCP.RateLimitPerMinute <= 0 ||
			c.MCP.MaxRows <= 0 || c.MCP.MaxResultBytes <= 0 ||
			c.MCP.CallTimeout.Std() <= 0 || c.MCP.LongTaskTimeout.Std() <= 0 {
			add("mcp token/rate/result/timeout settings must be > 0")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func validateScopes(scopes []string, allowWrite, allowAdmin bool) error {
	if len(scopes) == 0 {
		return fmt.Errorf("at least read is required")
	}
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if !knownScopes[s] {
			return fmt.Errorf("unknown scope %q", s)
		}
		if s == "admin" {
			if !allowAdmin {
				return fmt.Errorf("admin scope requires mcp.allow_admin_scope=true")
			}
			if !allowWrite {
				return fmt.Errorf("admin scope requires mcp.allow_write=true")
			}
			continue
		}
		if s != "read" && !allowWrite {
			return fmt.Errorf("write scope %q requires mcp.allow_write=true", s)
		}
	}
	return nil
}

// CheckPaths creates required directories, verifies they are writable, and
// checks TLS files. It is called by serve and config check, not by Load.
func (c *Config) CheckPaths() error {
	dirs := []string{
		c.Storage.DataDir,
		c.Storage.MediaDir,
		c.Storage.DocumentDir,
		c.Storage.BackupDir,
		c.Storage.LogDir,
	}
	for _, dir := range dirs {
		if err := ensureWritableDir(dir); err != nil {
			return fmt.Errorf("storage dir %s: %w", dir, err)
		}
	}
	if p := dsnPath(c.Database.DSN); p != "" && p != ":memory:" {
		if err := ensureWritableDir(filepath.Dir(p)); err != nil {
			return fmt.Errorf("database dir: %w", err)
		}
	}
	if c.Log.File != "" {
		if err := ensureWritableDir(filepath.Dir(c.Log.File)); err != nil {
			return fmt.Errorf("log dir: %w", err)
		}
	}
	if c.Server.TLS.Enabled {
		if err := checkReadableFile(c.Server.TLS.CertFile); err != nil {
			return fmt.Errorf("server tls cert: %w", err)
		}
		if err := checkReadableFile(c.Server.TLS.KeyFile); err != nil {
			return fmt.Errorf("server tls key: %w", err)
		}
	}
	if c.MCP.HTTP.TLS.Enabled {
		if err := checkReadableFile(c.MCP.HTTP.TLS.CertFile); err != nil {
			return fmt.Errorf("mcp tls cert: %w", err)
		}
		if err := checkReadableFile(c.MCP.HTTP.TLS.KeyFile); err != nil {
			return fmt.Errorf("mcp tls key: %w", err)
		}
	}
	if c.Dev.TemplatesDisk {
		if err := ensureExistingDir(c.Dev.TemplatesDir); err != nil {
			return fmt.Errorf("dev templates dir: %w", err)
		}
	}
	if c.Dev.StaticDisk {
		if err := ensureExistingDir(c.Dev.StaticDir); err != nil {
			return fmt.Errorf("dev static dir: %w", err)
		}
	}
	return nil
}

func (c *Config) Warnings() []string {
	var out []string
	u, err := url.Parse(c.Server.BaseURL)
	if err == nil && u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		out = append(out, "server.base_url is http on a non-local host; terminate TLS at the reverse proxy or enable server.tls")
	}
	if c.Dev.Enabled {
		out = append(out, "dev mode is enabled: templates are read from disk and may hot-reload; do not use in production")
	} else if c.Dev.TemplatesDisk || c.Dev.StaticDisk {
		out = append(out, "disk templates/static are enabled without dev mode; changes require a restart")
	}
	if c.MCP.Enabled && c.MCP.HTTP.Enabled && c.MCP.HTTP.Public && isLoopbackHost(hostOnly(c.MCP.HTTP.Listen)) {
		out = append(out, "mcp.http.public=true with loopback listen assumes a TLS reverse proxy; confirm HTTPS before exposing it")
	}
	if c.MCP.Enabled && c.MCP.AllowAdminScope {
		out = append(out, "mcp.allow_admin_scope=true allows admin-equivalent tokens; keep TTL short and audit calls")
	}
	if c.MCP.Enabled && !c.MCP.RequireDestructiveConfirm {
		out = append(out, "mcp.require_destructive_confirm=false lets destructive tools run without a confirm token")
	}
	if c.Admin.Path != "/admin" {
		out = append(out, "admin.path is not /admin; the first segment is reserved automatically")
	}
	return out
}

func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if !u.IsAbs() || u.Host == "" {
		return fmt.Errorf("must be an absolute URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	return nil
}

func validateBasePath(p string) error {
	if p == "" {
		return nil
	}
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("must start with /")
	}
	if strings.HasSuffix(p, "/") {
		return fmt.Errorf("must not end with /")
	}
	if strings.Contains(p, "..") || strings.Contains(p, "//") {
		return fmt.Errorf("must not contain .. or //")
	}
	if strings.ContainsAny(p, "{}*?# \t") {
		return fmt.Errorf("must not contain mux metacharacters or whitespace")
	}
	if path.Clean(p) != p {
		return fmt.Errorf("must be a clean path")
	}
	first := strings.Split(strings.TrimPrefix(p, "/"), "/")[0]
	if reservedRootPaths[first] {
		return fmt.Errorf("first segment %q conflicts with a root route", first)
	}
	return nil
}

func validateAdminPath(p string) error {
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("must start with /")
	}
	if p == "/" {
		return fmt.Errorf("must not be /")
	}
	if strings.HasSuffix(p, "/") {
		return fmt.Errorf("must not end with /")
	}
	if strings.Contains(p, "..") || strings.Contains(p, "//") {
		return fmt.Errorf("must not contain .. or //")
	}
	if strings.ContainsAny(p, "{}*?# \t") {
		return fmt.Errorf("must not contain mux metacharacters or whitespace")
	}
	if path.Clean(p) != p {
		return fmt.Errorf("must be a clean path")
	}
	first := strings.Split(strings.TrimPrefix(p, "/"), "/")[0]
	if reservedFrontSlugs[first] && first != "admin" {
		return fmt.Errorf("first segment %q conflicts with a front-end route", first)
	}
	return nil
}

func parseHostPort(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("port must be 1..65535")
	}
	return host, port, nil
}

func isLoopbackHost(host string) bool {
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func hostOnly(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func validateIPOrCIDR(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("empty")
	}
	if net.ParseIP(strings.Trim(s, "[]")) != nil {
		return nil
	}
	if _, _, err := net.ParseCIDR(s); err != nil {
		return fmt.Errorf("not an IP or CIDR")
	}
	return nil
}

func isRootPath(p string) bool {
	clean := filepath.Clean(p)
	vol := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, vol)
	return rest == "" || rest == string(filepath.Separator)
}

func ensureWritableDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("empty path")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tiliweb-write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

func checkReadableFile(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return f.Close()
}

func ensureExistingDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("empty path")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory")
	}
	return nil
}

func dsnPath(dsn string) string {
	s := strings.TrimPrefix(dsn, "file:")
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	return s
}
