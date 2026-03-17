package auth

import (
	"testing"
	"time"
)

func newTestLoginRateLimiter() *LoginRateLimiter {
	lr := &LoginRateLimiter{
		maxAttempts: 5,
		window:      60 * time.Second,
		attempts:    make(map[string]*loginAttempt),
		nowFunc:     time.Now,
		done:        make(chan struct{}),
	}
	return lr
}

func TestLoginRateLimiter_AllowsUnderLimit(t *testing.T) {
	lr := newTestLoginRateLimiter()
	defer lr.Close()

	for i := 0; i < 4; i++ {
		lr.RecordFailure("192.168.1.1")
	}

	if !lr.Check("192.168.1.1") {
		t.Error("expected to be allowed (4 failures < 5 max)")
	}
}

func TestLoginRateLimiter_BlocksAtLimit(t *testing.T) {
	lr := newTestLoginRateLimiter()
	defer lr.Close()

	for i := 0; i < 5; i++ {
		lr.RecordFailure("192.168.1.1")
	}

	if lr.Check("192.168.1.1") {
		t.Error("expected to be blocked (5 failures = max)")
	}
}

func TestLoginRateLimiter_ResetOnSuccess(t *testing.T) {
	lr := newTestLoginRateLimiter()
	defer lr.Close()

	for i := 0; i < 5; i++ {
		lr.RecordFailure("192.168.1.1")
	}

	lr.Reset("192.168.1.1")

	if !lr.Check("192.168.1.1") {
		t.Error("expected to be allowed after reset")
	}
}

func TestLoginRateLimiter_PerIPIsolation(t *testing.T) {
	lr := newTestLoginRateLimiter()
	defer lr.Close()

	for i := 0; i < 5; i++ {
		lr.RecordFailure("192.168.1.1")
	}

	if !lr.Check("192.168.1.2") {
		t.Error("different IP should not be affected")
	}
}

func TestLoginRateLimiter_WindowExpiry(t *testing.T) {
	now := time.Now()
	lr := newTestLoginRateLimiter()
	lr.nowFunc = func() time.Time { return now }
	defer lr.Close()

	for i := 0; i < 5; i++ {
		lr.RecordFailure("192.168.1.1")
	}

	// Advance past window.
	lr.nowFunc = func() time.Time { return now.Add(61 * time.Second) }

	if !lr.Check("192.168.1.1") {
		t.Error("expected to be allowed after window expires")
	}
}
