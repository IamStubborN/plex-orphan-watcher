package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/cleanup"
)

type fakeEvaluator struct {
	mu      sync.Mutex
	results []cleanup.Result
	calls   int
}

func (fake *fakeEvaluator) Evaluate(context.Context, string) (cleanup.Result, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls++
	if len(fake.results) == 0 {
		return cleanup.Result{Status: cleanup.Deleted}, nil
	}
	result := fake.results[0]
	fake.results = fake.results[1:]
	return result, nil
}

func (fake *fakeEvaluator) callCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.calls
}

func TestSchedulerRetriesWhilePlexStillIndexesCandidate(t *testing.T) {
	evaluator := &fakeEvaluator{results: []cleanup.Result{
		{Status: cleanup.BlockedPlex},
		{Status: cleanup.Deleted},
	}}
	scheduler := New(5*time.Millisecond, 10*time.Millisecond, time.Second, evaluator, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = scheduler.Run(ctx) }()

	scheduler.Enqueue("/data/tv/Show")
	waitForCalls(t, evaluator, 2)
}

func TestSchedulerDebouncesRepeatedEvents(t *testing.T) {
	evaluator := &fakeEvaluator{}
	scheduler := New(40*time.Millisecond, 10*time.Millisecond, time.Second, evaluator, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = scheduler.Run(ctx) }()

	scheduler.Enqueue("/data/tv/Show")
	time.Sleep(10 * time.Millisecond)
	scheduler.Enqueue("/data/tv/Show")
	waitForCalls(t, evaluator, 1)
	time.Sleep(50 * time.Millisecond)
	if got := evaluator.callCount(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestSchedulerIgnoresEventsImmediatelyAfterCompletion(t *testing.T) {
	evaluator := &fakeEvaluator{}
	scheduler := New(40*time.Millisecond, 10*time.Millisecond, time.Second, evaluator, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = scheduler.Run(ctx) }()

	scheduler.Enqueue("/data/tv/Show")
	waitForCalls(t, evaluator, 1)
	scheduler.Enqueue("/data/tv/Show")
	time.Sleep(30 * time.Millisecond)

	if got := evaluator.callCount(); got != 1 {
		t.Fatalf("calls = %d, want 1 during completion cooldown", got)
	}
}

func waitForCalls(t *testing.T, evaluator *fakeEvaluator, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if evaluator.callCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("calls = %d, want at least %d", evaluator.callCount(), want)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
