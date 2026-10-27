package markdown

import (
	"strings"
	"testing"
)

func TestRenderSanitizes(t *testing.T) {
	html, text := Render("# Hello\n\n<script>alert(1)</script>\n\n**bold**")
	if strings.Contains(html, "<script") {
		t.Fatalf("script leaked: %s", html)
	}
	if !strings.Contains(html, "<h1") || !strings.Contains(html, "<strong>") {
		t.Fatalf("markdown not rendered: %s", html)
	}
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "bold") {
		t.Fatalf("text = %q", text)
	}
	if strings.Contains(text, "<") {
		t.Fatalf("plain text contains tags: %q", text)
	}
}

func TestRenderGFM(t *testing.T) {
	html, _ := Render("| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] done")
	if !strings.Contains(html, "<table>") || !strings.Contains(html, "done") {
		t.Fatalf("gfm not rendered: %s", html)
	}
}
