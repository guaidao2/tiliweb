package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"tiliweb/internal/app"
	"tiliweb/internal/auth"
	"tiliweb/internal/backup"
	"tiliweb/internal/config"
	"tiliweb/internal/content"
	"tiliweb/internal/forum"
	"tiliweb/internal/mcp"
	"tiliweb/internal/site"
	"tiliweb/internal/store/sqlite"
	"tiliweb/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return runServe(nil)
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "config":
		if len(args) >= 2 && args[1] == "check" {
			return runConfigCheck(args[2:])
		}
		return usageError("config requires subcommand: check")
	case "migrate":
		return runMigrate(args[1:])
	case "admin":
		return runAdmin(args[1:])
	case "mcp":
		return runMCP(args[1:])
	case "backup":
		return runBackup(args[1:])
	case "version", "-version", "--version":
		printVersion()
		return 0
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		if strings.HasPrefix(args[0], "-") {
			return runServe(args)
		}
		return usageError("unknown command: " + args[0])
	}
}

type commonFlags struct {
	configPath   string
	listen       string
	host         string
	port         int
	templatesDir string
	staticDir    string
	dev          bool
	logLevel     string
}

func addCommonFlags(fs *flag.FlagSet) *commonFlags {
	c := &commonFlags{}
	fs.StringVar(&c.configPath, "config", "", "path to config.yaml")
	fs.StringVar(&c.listen, "listen", "", "listen address host:port")
	fs.StringVar(&c.host, "host", "", "listen host; port stays from config")
	fs.IntVar(&c.port, "port", 0, "listen port; host stays from config")
	fs.StringVar(&c.templatesDir, "templates", "", "load templates from this directory")
	fs.StringVar(&c.staticDir, "static", "", "load static files from this directory")
	fs.BoolVar(&c.dev, "dev", false, "dev mode: disk templates + hot reload")
	fs.StringVar(&c.logLevel, "log-level", "", "override log level (debug|info|warn|error)")
	return c
}

func (c *commonFlags) configFlags(fs *flag.FlagSet) config.Flags {
	out := config.Flags{
		Listen:       c.listen,
		Host:         c.host,
		Port:         c.port,
		TemplatesDir: c.templatesDir,
		StaticDir:    c.staticDir,
		Dev:          c.dev,
		LogLevel:     c.logLevel,
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "port":
			out.PortSet = true
		case "templates":
			out.TemplatesSet = true
		case "static":
			out.StaticSet = true
		case "dev":
			out.DevSet = true
		case "log-level":
			out.LogLevelSet = true
		}
	})
	return out
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := addCommonFlags(fs)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	cfg, err := loadConfig(c, fs)
	if err != nil {
		return fail(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, cfg); err != nil {
		return fail(err)
	}
	return 0
}

func runConfigCheck(args []string) int {
	fs := flag.NewFlagSet("config check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := addCommonFlags(fs)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	cfg, err := loadConfig(c, fs)
	if err != nil {
		return fail(err)
	}
	if err := cfg.CheckPaths(); err != nil {
		return fail(err)
	}
	for _, warning := range cfg.Warnings() {
		fmt.Fprintln(os.Stderr, "warning:", warning)
	}
	out, err := cfg.RedactedYAML()
	if err != nil {
		return fail(err)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		return fail(err)
	}
	return 0
}

func runMigrate(args []string) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := addCommonFlags(fs)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	cfg, err := loadConfig(c, fs)
	if err != nil {
		return fail(err)
	}
	if err := cfg.CheckPaths(); err != nil {
		return fail(err)
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, cfg.Database.DSN, cfg.Database.MaxOpenConns, cfg.Database.BusyTimeout)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = db.Close() }()

	applied, err := sqlite.Migrate(ctx, db)
	if err != nil {
		return fail(err)
	}
	if len(applied) == 0 {
		fmt.Println("no pending migrations")
	} else {
		fmt.Println("applied:", strings.Join(applied, ", "))
	}
	schemaVersion, err := sqlite.SchemaVersion(ctx, db)
	if err != nil {
		return fail(err)
	}
	fmt.Println("schema:", schemaVersion)
	return 0
}

func runAdmin(args []string) int {
	if len(args) == 0 {
		return usageError("admin requires subcommand: create|list|passwd")
	}
	switch args[0] {
	case "create":
		return runAdminCreate(args[1:])
	case "list":
		return runAdminList(args[1:])
	case "passwd":
		return runAdminPasswd(args[1:])
	default:
		return usageError("unknown admin subcommand: " + args[0])
	}
}

func runAdminCreate(args []string) int {
	fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := addCommonFlags(fs)
	username := fs.String("username", "", "admin username")
	password := fs.String("password", "", "admin password (prompted if empty)")
	displayName := fs.String("display-name", "", "display name (defaults to username)")
	role := fs.String("role", "superadmin", "superadmin|editor|moderator")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	if strings.TrimSpace(*username) == "" {
		return fail(errors.New("--username is required"))
	}
	pwd, err := promptPassword(*password)
	if err != nil {
		return fail(err)
	}
	svc, closeDB, err := openAuth(c, fs)
	if err != nil {
		return fail(err)
	}
	defer closeDB()

	admin, err := svc.CreateAdmin(context.Background(), *username, pwd, *displayName, *role)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("created admin %s (id=%d, role=%s)\n", admin.Username, admin.ID, admin.Role)
	return 0
}

func runAdminList(args []string) int {
	fs := flag.NewFlagSet("admin list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := addCommonFlags(fs)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	svc, closeDB, err := openAuth(c, fs)
	if err != nil {
		return fail(err)
	}
	defer closeDB()

	admins, err := svc.ListAdmins(context.Background())
	if err != nil {
		return fail(err)
	}
	fmt.Println("ID\tUSERNAME\tROLE\tSTATUS\tDISPLAY_NAME")
	for _, a := range admins {
		fmt.Printf("%d\t%s\t%s\t%s\t%s\n", a.ID, a.Username, a.Role, a.Status, a.DisplayName)
	}
	return 0
}

func runAdminPasswd(args []string) int {
	fs := flag.NewFlagSet("admin passwd", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := addCommonFlags(fs)
	username := fs.String("username", "", "admin username")
	password := fs.String("password", "", "new password (prompted if empty)")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	if strings.TrimSpace(*username) == "" {
		return fail(errors.New("--username is required"))
	}
	pwd, err := promptPassword(*password)
	if err != nil {
		return fail(err)
	}
	svc, closeDB, err := openAuth(c, fs)
	if err != nil {
		return fail(err)
	}
	defer closeDB()

	if err := svc.SetAdminPassword(context.Background(), *username, pwd, 0); err != nil {
		return fail(err)
	}
	fmt.Printf("password updated for %s; all admin sessions revoked\n", *username)
	return 0
}

func openAuth(c *commonFlags, fs *flag.FlagSet) (*auth.Service, func(), error) {
	cfg, err := loadConfig(c, fs)
	if err != nil {
		return nil, nil, err
	}
	if err := cfg.CheckPaths(); err != nil {
		return nil, nil, err
	}
	ctx := context.Background()
	db, err := sqlite.Open(ctx, cfg.Database.DSN, cfg.Database.MaxOpenConns, cfg.Database.BusyTimeout)
	if err != nil {
		return nil, nil, err
	}
	if _, err := sqlite.Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return auth.NewService(db, cfg, nil), func() { _ = db.Close() }, nil
}

var stdinReader = bufio.NewReader(os.Stdin)

func promptPassword(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	p1, err := readPassword("Password: ")
	if err != nil {
		return "", err
	}
	p2, err := readPassword("Confirm password: ")
	if err != nil {
		return "", err
	}
	if p1 != p2 {
		return "", errors.New("passwords do not match")
	}
	if p1 == "" {
		return "", errors.New("password is empty")
	}
	return p1, nil
}

func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := stdinReader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func runBackup(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := addCommonFlags(fs)
	out := fs.String("out", "", "destination .db file or directory")
	withMedia := fs.Bool("with-media", false, "also create a zip of media and documents")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	cfg, err := loadConfig(c, fs)
	if err != nil {
		return fail(err)
	}
	if err := cfg.CheckPaths(); err != nil {
		return fail(err)
	}
	ctx := context.Background()
	db, err := sqlite.Open(ctx, cfg.Database.DSN, cfg.Database.MaxOpenConns, cfg.Database.BusyTimeout)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = db.Close() }()
	name := backup.DefaultName(time.Now())
	dest := *out
	if dest == "" {
		dest = filepath.Join(cfg.Storage.BackupDir, name)
	} else if info, err := os.Stat(dest); err == nil && info.IsDir() {
		dest = filepath.Join(dest, name)
	} else if filepath.Ext(dest) == "" {
		dest = filepath.Join(dest, name)
	}
	if err := backup.Snapshot(ctx, db, dest); err != nil {
		return fail(err)
	}
	fmt.Printf("database backup: %s\n", dest)
	if *withMedia {
		zipPath := dest + ".media.zip"
		if err := backup.MediaZip(zipPath, cfg.Storage.MediaDir, cfg.Storage.DocumentDir); err != nil {
			return fail(err)
		}
		fmt.Printf("media backup: %s\n", zipPath)
	}
	return 0
}

func runMCP(args []string) int {
	if len(args) > 0 && args[0] == "token" {
		return runMCPToken(args[1:])
	}
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := addCommonFlags(fs)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	cfg, err := loadConfig(c, fs)
	if err != nil {
		return fail(err)
	}
	if !cfg.MCP.Enabled {
		return fail(errors.New("mcp.enabled is false"))
	}
	if !cfg.MCP.Stdio.Enabled {
		return fail(errors.New("mcp.stdio.enabled is false"))
	}
	if err := cfg.CheckPaths(); err != nil {
		return fail(err)
	}
	db, err := sqlite.Open(context.Background(), cfg.Database.DSN, cfg.Database.MaxOpenConns, cfg.Database.BusyTimeout)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = db.Close() }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := mcp.RunStdio(ctx, cfg, db, content.NewService(db), forum.New(db, cfg), site.NewStore(db), logger); err != nil {
		return fail(err)
	}
	return 0
}

func runMCPToken(args []string) int {
	if len(args) == 0 {
		return usageError("mcp token requires subcommand: create | list | revoke")
	}
	action := args[0]
	fs := flag.NewFlagSet("mcp token "+action, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := addCommonFlags(fs)
	name := fs.String("name", "", "token name")
	scopes := fs.String("scopes", "read", "comma-separated scopes")
	ttl := fs.Duration("ttl", 0, "token TTL (e.g. 720h); defaults to mcp.token_ttl")
	rate := fs.Int("rate", 0, "per-token requests per minute; 0 uses global")
	id := fs.Int64("id", 0, "token id for revoke")
	createdBy := fs.Int64("created-by", 0, "admin id recorded as creator")
	if ok, code := parseFlags(fs, args[1:]); !ok {
		return code
	}
	cfg, err := loadConfig(c, fs)
	if err != nil {
		return fail(err)
	}
	if err := cfg.CheckPaths(); err != nil {
		return fail(err)
	}
	ctx := context.Background()
	db, err := sqlite.Open(ctx, cfg.Database.DSN, cfg.Database.MaxOpenConns, cfg.Database.BusyTimeout)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := sqlite.Migrate(ctx, db); err != nil {
		return fail(err)
	}
	store := mcp.NewTokenStore(db)
	switch action {
	case "create":
		normalized, err := mcp.NormalizeScopes(strings.Split(*scopes, ","))
		if err != nil {
			return fail(err)
		}
		if err := mcp.CheckScopesForConfig(cfg, normalized); err != nil {
			return fail(err)
		}
		tokenTTL := cfg.MCP.TokenTTL.Std()
		if *ttl > 0 {
			tokenTTL = *ttl
		}
		plain, tok, err := store.Create(ctx, *name, normalized, tokenTTL, *rate, *createdBy)
		if err != nil {
			return fail(err)
		}
		fmt.Printf("token: %s\n", plain)
		fmt.Printf("id: %d prefix: %s scopes: %s\n", tok.ID, tok.Prefix, strings.Join(tok.Scopes, ","))
	case "list":
		tokens, err := store.List(ctx)
		if err != nil {
			return fail(err)
		}
		for _, t := range tokens {
			fmt.Printf("%d\t%s\t%s\t%s\t%s\n", t.ID, t.Name, t.Prefix, t.Status, strings.Join(t.Scopes, ","))
		}
	case "revoke":
		if *id == 0 {
			return usageError("mcp token revoke requires --id")
		}
		if err := store.Revoke(ctx, *id); err != nil {
			return fail(err)
		}
		fmt.Printf("revoked token %d\n", *id)
	default:
		return usageError("unknown mcp token subcommand: " + action)
	}
	return 0
}

func loadConfig(c *commonFlags, fs *flag.FlagSet) (*config.Config, error) {
	cfg, err := config.Load(c.configPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.ApplyFlags(c.configFlags(fs)); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseFlags(fs *flag.FlagSet, args []string) (bool, int) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, 0
		}
		return false, 2
	}
	return true, 0
}

func printVersion() {
	fmt.Printf("tiliweb %s (commit %s, built %s)\n", version.Version, version.Commit, version.BuildDate)
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "tiliweb:", err)
	return 1
}

func usageError(msg string) int {
	fmt.Fprintln(os.Stderr, "tiliweb:", msg)
	usage(os.Stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprint(w, `tiliweb - open-source Go CMS framework

Usage:
  tiliweb serve [flags]           start the web server (default)
  tiliweb config check [flags]    validate config, create dirs, print effective config
  tiliweb migrate [flags]         apply pending SQLite migrations
  tiliweb admin create [flags]    create an admin account
  tiliweb admin list [flags]      list admin accounts
  tiliweb admin passwd [flags]    reset an admin password
  tiliweb mcp [flags]             MCP stdio transport
  tiliweb mcp token ...           create/list/revoke MCP tokens
  tiliweb backup [flags]          SQLite snapshot, optional --with-media
  tiliweb version                 print version

Common flags:
  -config string      path to config.yaml
  -listen string      listen address host:port
  -host string        listen host; port stays from config
  -port int           listen port; host stays from config
  -templates string   load templates from disk
  -static string      load static files from disk
  -dev                dev mode: disk templates + hot reload
  -log-level string   debug|info|warn|error

Examples:
  tiliweb serve --config config.yaml
  tiliweb serve --listen 0.0.0.0:8080
  tiliweb config check --config config.yaml
  tiliweb migrate --config config.yaml
`)
}
