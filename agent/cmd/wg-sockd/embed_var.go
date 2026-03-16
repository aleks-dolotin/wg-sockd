// Package main — shared variable for embedded UI filesystem.
// This file has no build tags — it's compiled in both lean and embed_ui builds.
// In lean builds (embed_stub.go), embeddedUIFS stays nil.
// In embed_ui builds (embed.go), init() sets embeddedUIFS to the embedded FS.
package main

import "embed"

// embeddedUIFS holds the embedded UI filesystem. Nil in lean builds, set in embed_ui builds.
var embeddedUIFS *embed.FS
