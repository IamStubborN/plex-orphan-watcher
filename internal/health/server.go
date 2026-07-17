package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type ReadinessCheck func(*http.Request) error

func Handler(ready ReadinessCheck, report http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err := ready(request); err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ready\n"))
	})
	if report != nil {
		mux.Handle("GET /report", report)
	}
	return mux
}

func Run(ctx context.Context, address string, ready ReadinessCheck, report http.Handler) error {
	server := &http.Server{
		Addr:              address,
		Handler:           Handler(ready, report),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve health endpoint: %w", err)
	}
	return nil
}
