package view

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tiliweb/internal/content"
	"tiliweb/internal/site"
)

type testPageData struct {
	PageData
	NavChannels     []content.Channel
	NavPages        []content.Page
	SiteSettings    site.Settings
	HomeSettings    site.Home
	Banners         []content.Banner
	FeaturedEntries []content.Entry
	LatestEntries   []content.Entry
	ProductEntries  []content.Entry
	CaseEntries     []content.Entry
	FriendLinks     []content.FriendLink
}

func TestEmbeddedRender(t *testing.T) {
	l, err := NewEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	data := testPageData{PageData: PageData{
		SiteName:    "tiliweb",
		BasePath:    "",
		Locale:      "en",
		Version:     "test",
		Title:       "tiliweb",
		Description: "test",
		Year:        2026,
	}}
	if err := l.Render(&buf, "site/home", data); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"tiliweb", "/static/style.css", "欢迎"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered page missing %q:\n%s", want, got)
		}
	}
}

func TestDiskHotReload(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "layout.html"), `{{define "layout"}}<html><body>{{template "content" .}}</body></html>{{end}}`)
	page := filepath.Join(dir, "site", "home.html")
	writeFile(t, page, `{{define "content"}}v1 {{.SiteName}}{{end}}`)

	l, err := NewDisk(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := render(t, l, "site/home"); !strings.Contains(got, "v1 tiliweb") {
		t.Fatalf("render v1 = %q", got)
	}

	writeFile(t, page, `{{define "content"}}v2 {{.SiteName}}{{end}}`)
	if got := render(t, l, "site/home"); !strings.Contains(got, "v2 tiliweb") {
		t.Fatalf("hot reload did not pick up v2: %q", got)
	}
}

func TestDiskCacheWithoutReload(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "layout.html"), `{{define "layout"}}{{template "content" .}}{{end}}`)
	page := filepath.Join(dir, "site", "home.html")
	writeFile(t, page, `{{define "content"}}v1{{end}}`)

	l, err := NewDisk(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := render(t, l, "site/home"); !strings.Contains(got, "v1") {
		t.Fatalf("render v1 = %q", got)
	}
	writeFile(t, page, `{{define "content"}}v2{{end}}`)
	if got := render(t, l, "site/home"); !strings.Contains(got, "v1") {
		t.Fatalf("cached render should stay v1: %q", got)
	}
}

func TestMissingPage(t *testing.T) {
	l, err := NewEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Render(&bytes.Buffer{}, "site/nope", PageData{}); err == nil {
		t.Fatal("expected missing page error")
	}
}

func TestInvalidPageName(t *testing.T) {
	l, err := NewEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{"../layout", "site/../layout", "/layout", "site/..", "Site/Home"} {
		if err := l.Render(&bytes.Buffer{}, page, PageData{}); err == nil {
			t.Fatalf("page %q should be rejected", page)
		}
	}
}

func TestErrorPage(t *testing.T) {
	l, err := NewEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	data := testPageData{PageData: PageData{SiteName: "tiliweb", Title: "Not found", BasePath: ""}}
	if err := l.Render(&buf, "errors/404", data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "页面不存在") {
		t.Fatalf("404 page = %s", buf.String())
	}
}

func render(t *testing.T, l *Loader, page string) string {
	t.Helper()
	var buf bytes.Buffer
	data := PageData{SiteName: "tiliweb", Title: "tiliweb", Locale: "en"}
	if err := l.Render(&buf, page, data); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
