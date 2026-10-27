package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Storage   StorageConfig   `yaml:"storage"`
	Admin     AdminConfig     `yaml:"admin"`
	Auth      AuthConfig      `yaml:"auth"`
	Forum     ForumConfig     `yaml:"forum"`
	Site      SiteConfig      `yaml:"site"`
	Content   ContentConfig   `yaml:"content"`
	Mail      MailConfig      `yaml:"mail"`
	Uploads   UploadsConfig   `yaml:"uploads"`
	Dev       DevConfig       `yaml:"dev"`
	Log       LogConfig       `yaml:"log"`
	Audit     AuditConfig     `yaml:"audit"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	Security  SecurityConfig  `yaml:"security"`
	MCP       MCPConfig       `yaml:"mcp"`
}

type ServerConfig struct {
	Listen            string    `yaml:"listen"`
	BaseURL           string    `yaml:"base_url"`
	BasePath          string    `yaml:"base_path"`
	TrustedProxies    []string  `yaml:"trusted_proxies"`
	ReadHeaderTimeout Duration  `yaml:"read_header_timeout"`
	ReadTimeout       Duration  `yaml:"read_timeout"`
	WriteTimeout      Duration  `yaml:"write_timeout"`
	IdleTimeout       Duration  `yaml:"idle_timeout"`
	ShutdownTimeout   Duration  `yaml:"shutdown_timeout"`
	MaxHeaderBytes    int       `yaml:"max_header_bytes"`
	MaxBodyBytes      int64     `yaml:"max_body_bytes"`
	EnableGzip        bool      `yaml:"enable_gzip"`
	TLS               TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type DatabaseConfig struct {
	Driver       string `yaml:"driver"`
	DSN          string `yaml:"dsn"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	BusyTimeout  int    `yaml:"busy_timeout"`
	AutoMigrate  bool   `yaml:"auto_migrate"`
}

type StorageConfig struct {
	DataDir     string `yaml:"data_dir"`
	MediaDir    string `yaml:"media_dir"`
	DocumentDir string `yaml:"document_dir"`
	BackupDir   string `yaml:"backup_dir"`
	LogDir      string `yaml:"log_dir"`
}

type AdminConfig struct {
	Path               string   `yaml:"path"`
	SessionTTL         Duration `yaml:"session_ttl"`
	SessionIdle        Duration `yaml:"session_idle"`
	CookieName         string   `yaml:"cookie_name"`
	CookieSecure       bool     `yaml:"cookie_secure"`
	LoginRatePerMinute int      `yaml:"login_rate_per_minute"`
	LockoutThreshold   int      `yaml:"lockout_threshold"`
	LockoutDuration    Duration `yaml:"lockout_duration"`
	PasswordMinLength  int      `yaml:"password_min_length"`
}

type AuthConfig struct {
	RegisterEnabled    bool     `yaml:"register_enabled"`
	RegisterMode       string   `yaml:"register_mode"`
	SessionTTL         Duration `yaml:"session_ttl"`
	SessionIdle        Duration `yaml:"session_idle"`
	CookieName         string   `yaml:"cookie_name"`
	CookieSecure       bool     `yaml:"cookie_secure"`
	PasswordMinLength  int      `yaml:"password_min_length"`
	LoginRatePerMinute int      `yaml:"login_rate_per_minute"`
	LockoutThreshold   int      `yaml:"lockout_threshold"`
	LockoutDuration    Duration `yaml:"lockout_duration"`
	GuestComment       bool     `yaml:"guest_comment"`
}

type ForumConfig struct {
	BoardsPerPage          int      `yaml:"boards_per_page"`
	TopicsPerPage          int      `yaml:"topics_per_page"`
	PostsPerPage           int      `yaml:"posts_per_page"`
	EditWindow             Duration `yaml:"edit_window"`
	NewUserModerationPosts int      `yaml:"new_user_moderation_posts"`
}

type SiteConfig struct {
	Name           string          `yaml:"name"`
	Description    string          `yaml:"description"`
	Locale         string          `yaml:"locale"`
	Timezone       string          `yaml:"timezone"`
	LogoMediaID    int64           `yaml:"logo_media_id"`
	FaviconMediaID int64           `yaml:"favicon_media_id"`
	ICP            string          `yaml:"icp"`
	PoliceRecord   string          `yaml:"police_record"`
	Copyright      string          `yaml:"copyright"`
	Contact        ContactConfig   `yaml:"contact"`
	Analytics      AnalyticsConfig `yaml:"analytics"`
	SEO            SEOConfig       `yaml:"seo"`
}

type ContactConfig struct {
	Phone           string `yaml:"phone"`
	Email           string `yaml:"email"`
	Address         string `yaml:"address"`
	MapURL          string `yaml:"map_url"`
	WeChatQRMediaID int64  `yaml:"wechat_qr_media_id"`
}

type AnalyticsConfig struct {
	Provider string `yaml:"provider"`
	ID       string `yaml:"id"`
	Domain   string `yaml:"domain"`
}

type SEOConfig struct {
	DefaultTitle       string `yaml:"default_title"`
	DefaultDescription string `yaml:"default_description"`
	OGImageMediaID     int64  `yaml:"og_image_media_id"`
	RobotsExtra        string `yaml:"robots_extra"`
}

type ContentConfig struct {
	NewsPerPage         int  `yaml:"news_per_page"`
	ProductsPerPage     int  `yaml:"products_per_page"`
	CasesPerPage        int  `yaml:"cases_per_page"`
	JobsPerPage         int  `yaml:"jobs_per_page"`
	DownloadsPerPage    int  `yaml:"downloads_per_page"`
	CommentsEnabled     bool `yaml:"comments_enabled"`
	CommentModeration   bool `yaml:"comment_moderation"`
	CommentsPerPage     int  `yaml:"comments_per_page"`
	SitemapIncludeForum bool `yaml:"sitemap_include_forum"`
}

type MailConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	From          string `yaml:"from"`
	TLS           string `yaml:"tls"`
	NotifyContact bool   `yaml:"notify_contact"`
}

type UploadsConfig struct {
	MaxImageBytes       int64    `yaml:"max_image_bytes"`
	MaxDocumentBytes    int64    `yaml:"max_document_bytes"`
	ImageMaxPixels      int64    `yaml:"image_max_pixels"`
	AllowedImageTypes   []string `yaml:"allowed_image_types"`
	AllowedDocumentExts []string `yaml:"allowed_document_exts"`
}

type DevConfig struct {
	Enabled         bool   `yaml:"enabled"`
	TemplatesDisk   bool   `yaml:"templates_disk"`
	StaticDisk      bool   `yaml:"static_disk"`
	TemplatesDir    string `yaml:"templates_dir"`
	StaticDir       string `yaml:"static_dir"`
	ReloadTemplates bool   `yaml:"reload_templates"`
}

type LogConfig struct {
	Level           string `yaml:"level"`
	Format          string `yaml:"format"`
	AccessLog       bool   `yaml:"access_log"`
	File            string `yaml:"file"`
	MaxSizeMB       int    `yaml:"max_size_mb"`
	MaxBackups      int    `yaml:"max_backups"`
	RingBufferLines int    `yaml:"ring_buffer_lines"`
}

type AuditConfig struct {
	Enabled       bool `yaml:"enabled"`
	RetentionDays int  `yaml:"retention_days"`
}

type RateLimitConfig struct {
	Enabled         bool `yaml:"enabled"`
	ContactPerHour  int  `yaml:"contact_per_hour"`
	RegisterPerHour int  `yaml:"register_per_hour"`
	PostPerMinute   int  `yaml:"post_per_minute"`
	UploadPerMinute int  `yaml:"upload_per_minute"`
}

type SecurityConfig struct {
	CSRFCookieName              string `yaml:"csrf_cookie_name"`
	AdminRequirePasswordConfirm bool   `yaml:"admin_require_password_confirm"`
	HSTSEnabled                 bool   `yaml:"hsts_enabled"`
	HSTSMaxAge                  int    `yaml:"hsts_max_age"`
	ServerHeader                string `yaml:"server_header"`
}

type MCPConfig struct {
	Enabled                   bool           `yaml:"enabled"`
	Stdio                     MCPStdioConfig `yaml:"stdio"`
	HTTP                      MCPHTTPConfig  `yaml:"http"`
	AllowWrite                bool           `yaml:"allow_write"`
	AllowAdminScope           bool           `yaml:"allow_admin_scope"`
	RequireDestructiveConfirm bool           `yaml:"require_destructive_confirm"`
	TokenTTL                  Duration       `yaml:"token_ttl"`
	RateLimitPerMinute        int            `yaml:"rate_limit_per_minute"`
	MaxRows                   int            `yaml:"max_rows"`
	MaxResultBytes            int            `yaml:"max_result_bytes"`
	CallTimeout               Duration       `yaml:"call_timeout"`
	LongTaskTimeout           Duration       `yaml:"long_task_timeout"`
	LogToolArgs               bool           `yaml:"log_tool_args"`
}

type MCPStdioConfig struct {
	Enabled       bool     `yaml:"enabled"`
	DefaultScopes []string `yaml:"default_scopes"`
}

type MCPHTTPConfig struct {
	Enabled           bool      `yaml:"enabled"`
	Listen            string    `yaml:"listen"`
	Public            bool      `yaml:"public"`
	TLS               TLSConfig `yaml:"tls"`
	AllowedIPs        []string  `yaml:"allowed_ips"`
	AuthRatePerMinute int       `yaml:"auth_rate_per_minute"`
	AuthLockout       Duration  `yaml:"auth_lockout"`
	MaxBodyBytes      int64     `yaml:"max_body_bytes"`
	ReadTimeout       Duration  `yaml:"read_timeout"`
	WriteTimeout      Duration  `yaml:"write_timeout"`
	MaxSSEConnections int       `yaml:"max_sse_connections"`
}

// Default returns the built-in configuration. It mirrors config.example.yaml.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Listen:            "127.0.0.1:8080",
			BaseURL:           "http://localhost:8080",
			BasePath:          "",
			TrustedProxies:    []string{"127.0.0.1", "::1"},
			ReadHeaderTimeout: Duration(5 * 1e9),
			ReadTimeout:       Duration(120 * 1e9),
			WriteTimeout:      Duration(300 * 1e9),
			IdleTimeout:       Duration(120 * 1e9),
			ShutdownTimeout:   Duration(15 * 1e9),
			MaxHeaderBytes:    1 << 20,
			MaxBodyBytes:      64 << 20,
			EnableGzip:        false,
			TLS:               TLSConfig{},
		},
		Database: DatabaseConfig{
			Driver:       "sqlite",
			DSN:          "./data/tiliweb.db",
			MaxOpenConns: 8,
			BusyTimeout:  5000,
			AutoMigrate:  true,
		},
		Storage: StorageConfig{
			DataDir:     "./data",
			MediaDir:    "./data/media",
			DocumentDir: "./data/documents",
			BackupDir:   "./data/backups",
			LogDir:      "./data/logs",
		},
		Admin: AdminConfig{
			Path:               "/admin",
			SessionTTL:         Duration(12 * 3600 * 1e9),
			SessionIdle:        Duration(30 * 60 * 1e9),
			CookieName:         "tili_admin",
			CookieSecure:       false,
			LoginRatePerMinute: 10,
			LockoutThreshold:   5,
			LockoutDuration:    Duration(15 * 60 * 1e9),
			PasswordMinLength:  12,
		},
		Auth: AuthConfig{
			RegisterEnabled:    true,
			RegisterMode:       "approval",
			SessionTTL:         Duration(168 * 3600 * 1e9),
			SessionIdle:        Duration(72 * 3600 * 1e9),
			CookieName:         "tili_session",
			CookieSecure:       false,
			PasswordMinLength:  10,
			LoginRatePerMinute: 10,
			LockoutThreshold:   5,
			LockoutDuration:    Duration(15 * 60 * 1e9),
			GuestComment:       false,
		},
		Forum: ForumConfig{
			BoardsPerPage:          20,
			TopicsPerPage:          20,
			PostsPerPage:           20,
			EditWindow:             Duration(30 * 60 * 1e9),
			NewUserModerationPosts: 3,
		},
		Site: SiteConfig{
			Name:        "tiliweb",
			Description: "",
			Locale:      "zh-CN",
			Timezone:    "Asia/Shanghai",
			Contact:     ContactConfig{},
			Analytics:   AnalyticsConfig{},
			SEO:         SEOConfig{},
		},
		Content: ContentConfig{
			NewsPerPage:         10,
			ProductsPerPage:     12,
			CasesPerPage:        9,
			JobsPerPage:         10,
			DownloadsPerPage:    10,
			CommentsEnabled:     false,
			CommentModeration:   true,
			CommentsPerPage:     20,
			SitemapIncludeForum: true,
		},
		Mail: MailConfig{
			Enabled:       false,
			Port:          587,
			TLS:           "starttls",
			NotifyContact: true,
		},
		Uploads: UploadsConfig{
			MaxImageBytes:       16 << 20,
			MaxDocumentBytes:    50 << 20,
			ImageMaxPixels:      40_000_000,
			AllowedImageTypes:   []string{"image/jpeg", "image/png", "image/gif", "image/webp"},
			AllowedDocumentExts: []string{".pdf", ".zip", ".docx", ".xlsx", ".pptx", ".txt"},
		},
		Dev: DevConfig{
			Enabled:         false,
			TemplatesDisk:   false,
			StaticDisk:      false,
			TemplatesDir:    "./web/templates",
			StaticDir:       "./web/static",
			ReloadTemplates: true,
		},
		Log: LogConfig{
			Level:           "info",
			Format:          "text",
			AccessLog:       true,
			MaxSizeMB:       50,
			MaxBackups:      5,
			RingBufferLines: 5000,
		},
		Audit: AuditConfig{Enabled: true, RetentionDays: 180},
		RateLimit: RateLimitConfig{
			Enabled:         true,
			ContactPerHour:  5,
			RegisterPerHour: 5,
			PostPerMinute:   30,
			UploadPerMinute: 10,
		},
		Security: SecurityConfig{
			CSRFCookieName:              "tili_csrf",
			AdminRequirePasswordConfirm: true,
			HSTSEnabled:                 true,
			HSTSMaxAge:                  31536000,
			ServerHeader:                "",
		},
		MCP: MCPConfig{
			Enabled: false,
			Stdio: MCPStdioConfig{
				Enabled:       true,
				DefaultScopes: []string{"read"},
			},
			HTTP: MCPHTTPConfig{
				Enabled:           false,
				Listen:            "127.0.0.1:8090",
				Public:            false,
				TLS:               TLSConfig{},
				AllowedIPs:        []string{},
				AuthRatePerMinute: 10,
				AuthLockout:       Duration(15 * 60 * 1e9),
				MaxBodyBytes:      1 << 20,
				ReadTimeout:       Duration(30 * 1e9),
				WriteTimeout:      Duration(900 * 1e9),
				MaxSSEConnections: 20,
			},
			AllowWrite:                false,
			AllowAdminScope:           false,
			RequireDestructiveConfirm: true,
			TokenTTL:                  Duration(720 * 3600 * 1e9),
			RateLimitPerMinute:        60,
			MaxRows:                   100,
			MaxResultBytes:            256 << 10,
			CallTimeout:               Duration(30 * 1e9),
			LongTaskTimeout:           Duration(600 * 1e9),
			LogToolArgs:               false,
		},
	}
}

// Load reads YAML from path (or ./config.yaml when path is empty and exists),
// applies TILIWEB_* env overrides, then normalizes. It does not validate; call
// Validate after applying CLI flags.
func Load(path string) (*Config, error) {
	cfg := Default()

	explicit := path != ""
	if path == "" {
		path = "config.yaml"
		if _, err := os.Stat(path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("stat config: %w", err)
			}
			path = ""
		}
	}
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			if explicit {
				return nil, fmt.Errorf("open config %s: %w", path, err)
			}
			return nil, err
		}
		defer f.Close()

		dec := yaml.NewDecoder(bufio.NewReader(f))
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	if err := cfg.ApplyEnv(); err != nil {
		return nil, err
	}
	cfg.Normalize()
	return &cfg, nil
}

// LoadFile is like Load but requires the file to exist.
func LoadFile(path string) (*Config, error) {
	if path == "" {
		return nil, errors.New("config path is required")
	}
	return Load(path)
}

type Flags struct {
	Listen       string
	Host         string
	Port         int
	PortSet      bool
	TemplatesDir string
	TemplatesSet bool
	StaticDir    string
	StaticSet    bool
	Dev          bool
	DevSet       bool
	LogLevel     string
	LogLevelSet  bool
}

func (c *Config) ApplyFlags(f Flags) error {
	if f.Listen != "" {
		c.Server.Listen = f.Listen
	} else if f.Host != "" || f.PortSet {
		host, port, err := net.SplitHostPort(c.Server.Listen)
		if err != nil {
			return fmt.Errorf("current server.listen %q is invalid: %w", c.Server.Listen, err)
		}
		if f.Host != "" {
			host = f.Host
		}
		if f.PortSet {
			if f.Port < 1 || f.Port > 65535 {
				return fmt.Errorf("port %d out of range", f.Port)
			}
			port = strconv.Itoa(f.Port)
		}
		c.Server.Listen = net.JoinHostPort(host, port)
	}

	if f.TemplatesSet {
		c.Dev.TemplatesDisk = true
		c.Dev.TemplatesDir = f.TemplatesDir
		c.Dev.ReloadTemplates = false
	}
	if f.StaticSet {
		c.Dev.StaticDisk = true
		c.Dev.StaticDir = f.StaticDir
		if !f.TemplatesSet {
			c.Dev.ReloadTemplates = false
		}
	}
	if f.DevSet {
		c.Dev.Enabled = f.Dev
		if f.Dev {
			c.Dev.TemplatesDisk = true
			c.Dev.StaticDisk = true
			c.Dev.ReloadTemplates = true
		} else {
			c.Dev.ReloadTemplates = false
		}
	}
	if f.LogLevelSet {
		c.Log.Level = f.LogLevel
	}
	c.Normalize()
	return nil
}

// Normalize canonicalizes harmless variations without hiding real errors.
func (c *Config) Normalize() {
	c.Server.Listen = strings.TrimSpace(c.Server.Listen)
	c.Server.BaseURL = strings.TrimRight(strings.TrimSpace(c.Server.BaseURL), "/")
	if c.Server.BasePath == "/" {
		c.Server.BasePath = ""
	}
	c.Server.BasePath = strings.TrimRight(strings.TrimSpace(c.Server.BasePath), "/")
	c.Admin.Path = strings.TrimRight(strings.TrimSpace(c.Admin.Path), "/")
	c.MCP.HTTP.Listen = strings.TrimSpace(c.MCP.HTTP.Listen)

	c.Database.Driver = strings.ToLower(strings.TrimSpace(c.Database.Driver))
	c.Auth.RegisterMode = strings.ToLower(strings.TrimSpace(c.Auth.RegisterMode))
	c.Log.Level = strings.ToLower(strings.TrimSpace(c.Log.Level))
	c.Log.Format = strings.ToLower(strings.TrimSpace(c.Log.Format))
	c.Mail.TLS = strings.ToLower(strings.TrimSpace(c.Mail.TLS))
	c.Site.Analytics.Provider = strings.ToLower(strings.TrimSpace(c.Site.Analytics.Provider))

	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "text"
	}
	if c.Dev.TemplatesDir == "" {
		c.Dev.TemplatesDir = "./web/templates"
	}
	if c.Dev.StaticDir == "" {
		c.Dev.StaticDir = "./web/static"
	}
	if c.MCP.HTTP.Listen == "" {
		c.MCP.HTTP.Listen = "127.0.0.1:8090"
	}
	if strings.HasPrefix(strings.ToLower(c.Server.BaseURL), "https://") {
		c.Admin.CookieSecure = true
		c.Auth.CookieSecure = true
	}
	if c.Dev.Enabled {
		c.Dev.TemplatesDisk = true
		c.Dev.StaticDisk = true
	}
}

// RedactedYAML returns an effective config with secrets masked.
func (c Config) RedactedYAML() ([]byte, error) {
	copy := c
	if copy.Mail.Password != "" {
		copy.Mail.Password = "***"
	}
	return yaml.Marshal(copy)
}
