package health

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerReportsHealthy(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	Handler(func(*http.Request) error { return nil }, nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Body.String() != "ok\n" {
		t.Fatalf("body = %q, want %q", recorder.Body.String(), "ok\n")
	}
}

func TestHandlerReportsReadinessFailure(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()

	Handler(func(*http.Request) error { return errors.New("Plex unavailable") }, nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), "Plex unavailable") {
		t.Fatalf("body = %q, want readiness error", recorder.Body.String())
	}
}

func TestHandlerServesInternalReport(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/report", nil)
	recorder := httptest.NewRecorder()
	Handler(func(*http.Request) error { return nil }, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("[]\n"))
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "[]\n" {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}
