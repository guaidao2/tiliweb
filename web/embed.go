// Package web embeds templates and static assets for single-binary builds.
package web

import "embed"

//go:embed templates static
var FS embed.FS
