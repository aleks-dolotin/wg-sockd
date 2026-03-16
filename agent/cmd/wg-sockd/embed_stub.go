//go:build !embed_ui

// Package main — stub for lean builds without embedded UI.
// embeddedUIFS (declared in embed_var.go) stays nil.
// --serve-ui reports a clear error at runtime.
package main
