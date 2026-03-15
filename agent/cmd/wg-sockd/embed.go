//go:build embed_ui

// Package main — embedded UI assets for standalone mode.
// Built with: go build -tags embed_ui
package main

import "embed"

//go:embed all:ui_dist
var embeddedUI embed.FS

func init() {
	embeddedUIFS = &embeddedUI
}

