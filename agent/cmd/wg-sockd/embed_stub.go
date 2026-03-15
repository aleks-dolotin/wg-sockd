//go:build !embed_ui

// Package main — stub for lean builds without embedded UI.
// The embeddedUIFS stays nil, and --serve-ui reports a clear error.
package main

import "embed"

// embeddedUIFS is nil in lean builds — checked at runtime.
var embeddedUIFS *embed.FS

