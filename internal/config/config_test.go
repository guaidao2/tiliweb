package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultValidates(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
	if got, want := cfg.Server.Listen, "127.0.0.1:8080"; got != want {
		t.Fatalf("listen = %q, want %q", got, want)
	}
	if got, want := cfg.MCP.Stdio.DefaultScopes, []string{"read"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("default scopes = %v, want %v", got, want)
	}
}

func TestLoadExampleConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("example config should validate: %v", err)
	}
	if cfg.Site.Name != "tiliweb" {
		t.Fatalf("site name = %q", cfg.Site.Name)
	}
	if cfg.MCP.Enabled {
		t.Fatal("mcp should be disabled by default")
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	path := writeTempConfig(t, "server:\n  listen: \"127.0.0.1:8080\"\n  unknown_knob: true\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown_knob") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("TILIWEB_SERVER_LISTEN", "0.0.0.0:9090")
	t.Setenv("TILIWEB_AUTH_REGISTER_MODE", "invite")
	t.Setenv("TILIWEB_MCP_STDIO_DEFAULT_SCOPES", "read,content.write")
	t.Setenv("TILIWEB_MCP_ALLOW_WRITE", "true")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Listen != "0.0.0.0:9090" {
		t.Fatalf("listen = %q", cfg.Server.Listen)
	}
	if cfg.Auth.RegisterMode != "invite" {
		t.Fatalf("register mode = %q", cfg.Auth.RegisterMode)
	}
	if len(cfg.MCP.Stdio.DefaultScopes) != 2 || cfg.MCP.Stdio.DefaultScopes[1] != "content.write" {
		t.Fatalf("scopes = %v", cfg.MCP.Stdio.DefaultScopes)
	}
	if !cfg.MCP.AllowWrite {
		t.Fatal("allow_write should be true")
	}
}

func TestFlagsListenPrecedence(t *testing.T) {
	cfg := Default()
	if err := cfg.ApplyFlags(Flags{Host: "0.0.0.0", Port: 9090, PortSet: true}); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != "0.0.0.0:9090" {
		t.Fatalf("listen = %q", cfg.Server.Listen)
	}

	cfg = Default()
	if err := cfg.ApplyFlags(Flags{Listen: "10.0.0.1:7070", Host: "0.0.0.0", Port: 9090, PortSet: true}); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != "10.0.0.1:7070" {
		t.Fatalf("listen = %q", cfg.Server.Listen)
	}
}

func TestHTTPSForcesSecureCookies(t *testing.T) {
	cfg := Default()
	cfg.Server.BaseURL = "https://example.com"
	cfg.Admin.CookieSecure = false
	cfg.Auth.CookieSecure = false
	cfg.Normalize()
	if !cfg.Admin.CookieSecure || !cfg.Auth.CookieSecure {
		t.Fatal("cookies should be Secure when base_url is https")
	}
}

func TestBasePathRootNormalized(t *testing.T) {
	cfg := Default()
	cfg.Server.BasePath = "/"
	cfg.Normalize()
	if cfg.Server.BasePath != "" {
		t.Fatalf("base path = %q", cfg.Server.BasePath)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminPathReservedRejected(t *testing.T) {
	cfg := Default()
	cfg.Admin.Path = "/login"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected reserved admin path error")
	}
}

func TestMCPPublicRequiresTLS(t *testing.T) {
	cfg := Default()
	cfg.MCP.Enabled = true
	cfg.MCP.HTTP.Enabled = true
	cfg.MCP.HTTP.Public = true
	cfg.MCP.HTTP.Listen = "0.0.0.0:8090"
	cfg.MCP.HTTP.TLS.Enabled = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-loopback public MCP without TLS to fail")
	}
}

func TestMCPAllowAdminRequiresWrite(t *testing.T) {
	cfg := Default()
	cfg.MCP.Enabled = true
	cfg.MCP.AllowAdminScope = true
	cfg.MCP.AllowWrite = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected admin scope without write to fail")
	}
}

func TestMCPStdioWriteScopeRequiresWrite(t *testing.T) {
	cfg := Default()
	cfg.MCP.Enabled = true
	cfg.MCP.Stdio.DefaultScopes = []string{"read", "content.write"}
	cfg.MCP.AllowWrite = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected write scope without allow_write to fail")
	}
}

func TestCheckPaths(t *testing.T) {
	dir := t.TempDir()
	cfg := Default()
	cfg.Storage.DataDir = filepath.Join(dir, "data")
	cfg.Storage.MediaDir = filepath.Join(dir, "media")
	cfg.Storage.DocumentDir = filepath.Join(dir, "documents")
	cfg.Storage.BackupDir = filepath.Join(dir, "backups")
	cfg.Storage.LogDir = filepath.Join(dir, "logs")
	cfg.Database.DSN = filepath.Join(dir, "db", "tiliweb.db")
	cfg.Log.File = filepath.Join(dir, "logs", "tiliweb.log")
	if err := cfg.CheckPaths(); err != nil {
		t.Fatalf("check paths: %v", err)
	}
	for _, p := range []string{cfg.Storage.DataDir, cfg.Storage.MediaDir, filepath.Join(dir, "db")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s not created: %v", p, err)
		}
	}
}

func TestRedactedYAML(t *testing.T) {
	cfg := Default()
	cfg.Mail.Password = "super-secret"
	out, err := cfg.RedactedYAML()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "super-secret") {
		t.Fatal("password leaked")
	}
	if !strings.Contains(s, "***") {
		t.Fatal("masked password missing")
	}
}

func TestFlagsDiskModes(t *testing.T) {
	cfg := Default()
	if err := cfg.ApplyFlags(Flags{TemplatesDir: "/tmp/tpl", TemplatesSet: true}); err != nil {
		t.Fatal(err)
	}
	if !cfg.Dev.TemplatesDisk || cfg.Dev.StaticDisk || cfg.Dev.Enabled {
		t.Fatalf("templates flag modes = %+v", cfg.Dev)
	}

	cfg = Default()
	if err := cfg.ApplyFlags(Flags{StaticDir: "/tmp/static", StaticSet: true}); err != nil {
		t.Fatal(err)
	}
	if !cfg.Dev.StaticDisk || cfg.Dev.TemplatesDisk || cfg.Dev.Enabled {
		t.Fatalf("static flag modes = %+v", cfg.Dev)
	}

	cfg = Default()
	if err := cfg.ApplyFlags(Flags{Dev: true, DevSet: true}); err != nil {
		t.Fatal(err)
	}
	if !cfg.Dev.Enabled || !cfg.Dev.TemplatesDisk || !cfg.Dev.StaticDisk || !cfg.Dev.ReloadTemplates {
		t.Fatalf("dev flag modes = %+v", cfg.Dev)
	}
}

func TestBasePathReserved(t *testing.T) {
	for _, p := range []string{"/healthz", "/readyz", "/robots.txt", "/admin", "/static", "/cms{}", "/cms path", "/.", "/a/./b"} {
		cfg := Default()
		cfg.Server.BasePath = p
		if err := cfg.Validate(); err == nil {
			t.Fatalf("base path %q should fail validation", p)
		}
	}
}

func TestAdminPathMetachar(t *testing.T) {
	for _, p := range []string{"/adm{}in", "/adm in", "/adm?in"} {
		cfg := Default()
		cfg.Admin.Path = p
		if err := cfg.Validate(); err == nil {
			t.Fatalf("admin path %q should fail validation", p)
		}
	}
}

func TestDurationYAML(t *testing.T) {
	path := writeTempConfig(t, "server:\n  listen: \"127.0.0.1:8080\"\n  read_timeout: 45s\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ReadTimeout.Std() != 45*time.Second {
		t.Fatalf("read timeout = %s", cfg.Server.ReadTimeout)
	}
}

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
