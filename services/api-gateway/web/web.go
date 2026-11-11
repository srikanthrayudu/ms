// Package web embeds the gateway's HTML templates and static assets so the
// gateway ships as a single binary with no external file dependencies.
package web

import "embed"

// TemplatesFS holds the HTML templates.
//
//go:embed all:templates
var TemplatesFS embed.FS

// StaticFS holds static assets (css).
//
//go:embed all:static
var StaticFS embed.FS
