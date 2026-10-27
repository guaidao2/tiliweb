// Package markdown renders GFM and sanitizes the resulting HTML.
package markdown

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var (
	md      = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.TaskList))
	policy  = bluemonday.UGCPolicy()
	tagRE   = regexp.MustCompile(`<[^>]+>`)
	spaceRE = regexp.MustCompile(`\s+`)
)

func Render(source string) (html, plain string) {
	var buf strings.Builder
	if err := md.Convert([]byte(source), &buf); err != nil {
		return "", plainText(source)
	}
	html = string(policy.SanitizeBytes([]byte(buf.String())))
	return html, plainText(source)
}

func plainText(source string) string {
	var buf strings.Builder
	if err := md.Convert([]byte(source), &buf); err != nil {
		return strings.TrimSpace(source)
	}
	s := tagRE.ReplaceAllString(buf.String(), " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	return strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
}
