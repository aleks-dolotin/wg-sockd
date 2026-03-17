// Package ctxkeys provides shared context key types used across middleware and auth packages.
// Extracting keys into a standalone package avoids circular imports between
// agent/internal/middleware and agent/internal/auth.
package ctxkeys

import "context"

// UsernameKey is the context key for the authenticated username.
type UsernameKey struct{}

// IsUnixSocketKey is the context key for Unix socket connection detection.
type IsUnixSocketKey struct{}

// ConnIDKey is the context key for per-connection identification (rate limiting).
type ConnIDKey struct{}

// UsernameFromContext extracts the authenticated username, or "" if none.
func UsernameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(UsernameKey{}).(string)
	return v
}

// IsUnixSocket returns true if the request arrived via a Unix socket.
func IsUnixSocket(ctx context.Context) bool {
	v, _ := ctx.Value(IsUnixSocketKey{}).(bool)
	return v
}
