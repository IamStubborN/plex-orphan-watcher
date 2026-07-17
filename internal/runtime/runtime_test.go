package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/plex"
)

func TestRunEventSourceReconnectsAndDeliversEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &flakyEventSource{}
	events := make(chan plex.DeletionEvent, 1)
	done := make(chan error, 1)
	go func() {
		done <- runEventSource(ctx, source, events, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	select {
	case event := <-events:
		if event.RatingKey != "101" {
			t.Fatalf("rating key = %q", event.RatingKey)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event was not delivered after reconnect")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("event source did not stop")
	}
	if source.Calls() < 2 {
		t.Fatalf("calls = %d, want at least 2", source.Calls())
	}
}

func TestNextEventSourceBackoffDoublesAndCaps(t *testing.T) {
	if got := nextEventSourceBackoff(time.Second); got != 2*time.Second {
		t.Fatalf("backoff = %s, want 2s", got)
	}
	if got := nextEventSourceBackoff(time.Minute); got != time.Minute {
		t.Fatalf("backoff = %s, want 1m", got)
	}
}

func TestReconnectDelayResetsAfterStableConnection(t *testing.T) {
	if got := reconnectDelay(time.Minute, stableEventSourceDuration); got != time.Second {
		t.Fatalf("backoff = %s, want 1s", got)
	}
	if got := reconnectDelay(8*time.Second, stableEventSourceDuration-time.Nanosecond); got != 8*time.Second {
		t.Fatalf("backoff = %s, want 8s", got)
	}
}

type flakyEventSource struct {
	mu    sync.Mutex
	calls int
}

func (source *flakyEventSource) StreamEvents(ctx context.Context, handler func(plex.DeletionEvent)) error {
	source.mu.Lock()
	source.calls++
	call := source.calls
	source.mu.Unlock()
	if call == 1 {
		return errors.New("connection lost")
	}
	handler(plex.DeletionEvent{RatingKey: "101"})
	<-ctx.Done()
	return ctx.Err()
}

func (source *flakyEventSource) Calls() int {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.calls
}
