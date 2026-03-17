package main

import (
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

// bcryptGenerateFromPassword wraps bcrypt for use in hashPasswordCmd.
func bcryptGenerateFromPassword(password []byte, cost int) ([]byte, error) {
	return bcrypt.GenerateFromPassword(password, cost)
}

// isTerminal returns true if the file descriptor is a terminal.
func isTerminal(fd int) bool {
	return term.IsTerminal(fd)
}

// readPasswordNoEcho reads a password from a terminal without echoing.
func readPasswordNoEcho(fd int) ([]byte, error) {
	return term.ReadPassword(fd)
}
