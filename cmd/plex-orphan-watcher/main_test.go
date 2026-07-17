package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestReportDoesNotMaskInvalidStatePathWithHTTPFallback(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("[]"))
	}))
	defer server.Close()

	err := report([]string{
		"--state", filepath.Join(t.TempDir(), "missing", "watcher.db"),
		"--url", server.URL,
	})
	if err == nil {
		t.Fatal("expected invalid state path error")
	}
	if calls.Load() != 0 {
		t.Fatalf("report endpoint calls = %d, want 0", calls.Load())
	}
}
