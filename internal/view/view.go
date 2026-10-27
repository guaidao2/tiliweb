package view

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sync"
	"time"

	"tiliweb/web"
)

var (
	pageRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9_/-]*$`)
	layoutRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_/-]*\.html$`)
)

// PageData is the common data passed to every page.
type PageData struct {
	SiteName        string
	SiteDescription string
	BasePath        string
	Locale          string
	Version         string
	Title           string
	Description     string
	Path            string
	Year            int
	SignedIn        bool
	Canonical       string
	OGType          string
	OGImage         string
	JSONLD          template.HTML
}

// Loader renders layout.html plus one page file. Embedded templates are parsed
// once per page and cached. Disk templates can reload on every render for dev.
type Loader struct {
	fs     fs.FS
	reload bool

	mu    sync.RWMutex
	cache map[string]*template.Template
}

// NewEmbedded creates a loader backed by the embedded web/templates FS.
func NewEmbedded() (*Loader, error) {
	sub, err := fs.Sub(web.FS, "templates")
	if err != nil {
		return nil, fmt.Errorf("embedded templates: %w", err)
	}
	return &Loader{fs: sub, cache: map[string]*template.Template{}}, nil
}

// NewDisk creates a loader backed by a directory. When reload is true, every
// render re-parses layout and page (dev hot reload).
func NewDisk(dir string, reload bool) (*Loader, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("templates dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("templates dir %s is not a directory", dir)
	}
	return &Loader{fs: os.DirFS(dir), reload: reload, cache: map[string]*template.Template{}}, nil
}

// Render executes the site layout with page's content block.
func (l *Loader) Render(w io.Writer, page string, data any) error {
	return l.RenderLayout(w, "layout.html", page, data)
}

// RenderLayout executes a specific layout (e.g. admin/layout.html) with page's
// content block. Output is buffered so a template execution error cannot
// half-write a response.
func (l *Loader) RenderLayout(w io.Writer, layoutPage, page string, data any) error {
	if !layoutRE.MatchString(layoutPage) || path.Clean(layoutPage) != layoutPage {
		return fmt.Errorf("invalid layout name %q", layoutPage)
	}
	if !pageRE.MatchString(page) || path.Clean(page) != page {
		return fmt.Errorf("invalid page name %q", page)
	}
	tmpl, err := l.load(layoutPage, page)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		return fmt.Errorf("render %s: %w", page, err)
	}
	_, err = buf.WriteTo(w)
	return err
}

func (l *Loader) load(layoutPage, page string) (*template.Template, error) {
	key := layoutPage + "|" + page
	if !l.reload {
		l.mu.RLock()
		tmpl := l.cache[key]
		l.mu.RUnlock()
		if tmpl != nil {
			return tmpl, nil
		}
	}

	tmpl, err := template.New("layout").Funcs(template.FuncMap{
		"year": func() int { return time.Now().Year() },
		"unixTime": func(v int64) string {
			if v <= 0 {
				return "-"
			}
			return time.Unix(v, 0).Format("2006-01-02 15:04")
		},
		"nowUnix":  func() int64 { return time.Now().Unix() },
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"extra": func(m map[string]any, key string) string {
			if m == nil {
				return ""
			}
			if v, ok := m[key]; ok {
				return fmt.Sprint(v)
			}
			return ""
		},
		"statusLabel": func(s string) string {
			switch s {
			case "active":
				return "启用"
			case "hidden":
				return "隐藏"
			case "draft":
				return "草稿"
			case "published":
				return "已发布"
			case "archived":
				return "已归档"
			case "pending":
				return "待审核"
			case "approved":
				return "已通过"
			case "spam":
				return "垃圾"
			case "deleted":
				return "已删除"
			case "read":
				return "已读"
			case "replied":
				return "已回复"
			case "new":
				return "新"
			case "open":
				return "开放"
			case "locked":
				return "已锁定"
			case "visible":
				return "可见"
			case "disabled":
				return "已禁用"
			case "revoked":
				return "已吊销"
			}
			return s
		},
		"roleLabel": func(s string) string {
			switch s {
			case "superadmin":
				return "超级管理员"
			case "editor":
				return "编辑"
			case "moderator":
				return "版主"
			case "admin":
				return "管理员"
			}
			return s
		},
		"visibilityLabel": func(s string) string {
			if s == "members" {
				return "仅会员"
			}
			if s == "public" {
				return "公开"
			}
			return s
		},
	}).ParseFS(l.fs, layoutPage, page+".html")
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", page, err)
	}

	if !l.reload {
		l.mu.Lock()
		l.cache[key] = tmpl
		l.mu.Unlock()
	}
	return tmpl, nil
}
