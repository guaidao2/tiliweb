package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"tiliweb/internal/auth"
	"tiliweb/internal/config"
	"tiliweb/internal/content"
	"tiliweb/internal/forum"
	"tiliweb/internal/httpserver"
	"tiliweb/internal/mcp"
	"tiliweb/internal/site"
	"tiliweb/internal/store/sqlite"
	"tiliweb/internal/version"
	"tiliweb/internal/view"
)

// Run opens the database, applies migrations, builds the HTTP handler, and
// serves until ctx is canceled or the server fails.
func Run(ctx context.Context, cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}

	logger, closeLog, err := newLogger(cfg)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer func() { _ = closeLog() }()
	slog.SetDefault(logger)

	for _, warning := range cfg.Warnings() {
		logger.Warn(warning)
	}
	if cfg.Log.File != "" {
		logger.Warn("log file enabled", "file", cfg.Log.File, "max_size_mb", cfg.Log.MaxSizeMB, "max_backups", cfg.Log.MaxBackups)
	}
	if cfg.Server.EnableGzip {
		logger.Warn("server.enable_gzip is not implemented; terminate compression at the reverse proxy")
	}
	if err := cfg.CheckPaths(); err != nil {
		return err
	}

	db, err := sqlite.Open(ctx, cfg.Database.DSN, cfg.Database.MaxOpenConns, cfg.Database.BusyTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	var applied []string
	if cfg.Database.AutoMigrate {
		applied, err = sqlite.Migrate(ctx, db)
		if err != nil {
			return err
		}
		if len(applied) > 0 {
			logger.Info("migrations applied", "versions", applied)
		} else {
			logger.Debug("no pending migrations")
		}
	}
	schemaVersion, err := sqlite.SchemaVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("schema version: %w", err)
	}

	loader, err := loadView(cfg)
	if err != nil {
		return err
	}
	authSvc := auth.NewService(db, cfg, logger)
	contentSvc := content.NewService(db)
	for _, v := range applied {
		if v == "0004_site" {
			if err := contentSvc.RebuildSearch(ctx); err != nil {
				logger.Warn("rebuild search index after 0004_site", "error", err)
			}
		}
	}
	forumSvc := forum.New(db, cfg)
	siteStore := site.NewStore(db)
	mcpSvc := mcp.New(cfg, db, contentSvc, forumSvc, siteStore, logger)
	if cfg.MCP.Enabled && cfg.MCP.HTTP.Enabled {
		ln, err := mcpSvc.HTTPListener()
		if err != nil {
			return fmt.Errorf("mcp http listen %s: %w", cfg.MCP.HTTP.Listen, err)
		}
		logger.Info("MCP HTTP listening", "listen", cfg.MCP.HTTP.Listen, "tls", cfg.MCP.HTTP.TLS.Enabled)
		go func() {
			if err := mcpSvc.ServeHTTPListener(ctx, ln); err != nil {
				logger.Error("MCP HTTP stopped", "error", err)
			}
		}()
	}
	web, err := httpserver.New(cfg, db, loader, authSvc, contentSvc, forumSvc, siteStore, logger)
	if err != nil {
		return err
	}
	go web.CleanupLoop(ctx)

	httpSrv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           web.Handler(),
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout.Std(),
		ReadTimeout:       cfg.Server.ReadTimeout.Std(),
		WriteTimeout:      cfg.Server.WriteTimeout.Std(),
		IdleTimeout:       cfg.Server.IdleTimeout.Std(),
		MaxHeaderBytes:    cfg.Server.MaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	useTLS := cfg.Server.TLS.Enabled
	if useTLS {
		cert, err := tls.LoadX509KeyPair(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)
		if err != nil {
			return fmt.Errorf("load server TLS keypair: %w", err)
		}
		httpSrv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}

	ln, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Server.Listen, err)
	}
	logger.Info("tiliweb starting",
		"version", version.String(),
		"listen", ln.Addr().String(),
		"tls", useTLS,
		"templates", templatesMode(cfg),
		"schema", schemaVersion,
		"database", cfg.Database.DSN,
	)

	return httpserver.Serve(ctx, httpSrv, ln, useTLS, cfg.Server.ShutdownTimeout.Std(), logger)
}

func loadView(cfg *config.Config) (*view.Loader, error) {
	if cfg.Dev.TemplatesDisk {
		return view.NewDisk(cfg.Dev.TemplatesDir, cfg.Dev.ReloadTemplates)
	}
	return view.NewEmbedded()
}

func templatesMode(cfg *config.Config) string {
	if !cfg.Dev.TemplatesDisk {
		return "embedded"
	}
	if cfg.Dev.ReloadTemplates {
		return "disk+hot-reload:" + cfg.Dev.TemplatesDir
	}
	return "disk:" + cfg.Dev.TemplatesDir
}
