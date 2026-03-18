package main

// Version variables — injected via ldflags at build time.
// Default values are used for development builds (go run / go build without ldflags).
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)
