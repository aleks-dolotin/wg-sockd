package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockChecker implements ReadOnlyChecker for testing.
type mockChecker struct {
	readOnly bool
}

func (m *mockChecker) IsReadOnly() bool { return m.readOnly }

var echoHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestReadOnlyGuard_DiskFull_PostReturns503(t *testing.T) {
	checker := &mockChecker{readOnly: true}
	handler := ReadOnlyGuard(checker)(echoHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/peers", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}

	var body map[string]string
	json.NewDecoder(rr.Body).Decode(&body)
	if body["error"] != "storage_unavailable" {
		t.Errorf("expected error=storage_unavailable, got %q", body["error"])
	}
}

func TestReadOnlyGuard_DiskFull_GetReturns200(t *testing.T) {
	checker := &mockChecker{readOnly: true}
	handler := ReadOnlyGuard(checker)(echoHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for GET even when disk full, got %d", rr.Code)
	}
}

func TestReadOnlyGuard_DiskFull_DeleteReturns503(t *testing.T) {
	checker := &mockChecker{readOnly: true}
	handler := ReadOnlyGuard(checker)(echoHandler)

	req := httptest.NewRequest(http.MethodDelete, "/api/peers/1", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestReadOnlyGuard_DiskFull_PutReturns503(t *testing.T) {
	checker := &mockChecker{readOnly: true}
	handler := ReadOnlyGuard(checker)(echoHandler)

	req := httptest.NewRequest(http.MethodPut, "/api/peers/1", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestReadOnlyGuard_DiskFreed_PostReturns200(t *testing.T) {
	checker := &mockChecker{readOnly: false}
	handler := ReadOnlyGuard(checker)(echoHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/peers", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 when disk OK, got %d", rr.Code)
	}
}

func TestReadOnlyGuard_HealthAlwaysPasses(t *testing.T) {
	checker := &mockChecker{readOnly: true}
	handler := ReadOnlyGuard(checker)(echoHandler)

	// POST to health should still pass even in read-only mode.
	req := httptest.NewRequest(http.MethodPost, "/api/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for health even when disk full, got %d", rr.Code)
	}
}

func TestReadOnlyGuard_AutoRecovery(t *testing.T) {
	checker := &mockChecker{readOnly: true}
	handler := ReadOnlyGuard(checker)(echoHandler)

	// Initially disk full → 503.
	req := httptest.NewRequest(http.MethodPost, "/api/peers", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}

	// Disk freed → 200.
	checker.readOnly = false
	req = httptest.NewRequest(http.MethodPost, "/api/peers", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 after disk freed, got %d", rr.Code)
	}
}

