package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/cleanup"
)

type Evaluator interface {
	Evaluate(ctx context.Context, candidate string) (cleanup.Result, error)
}

type Scheduler struct {
	delay     time.Duration
	retry     time.Duration
	maxAge    time.Duration
	evaluator Evaluator
	logger    *slog.Logger
	mu        sync.Mutex
	queued    map[string]struct{}
	wake      chan struct{}
}

type candidate struct {
	firstSeen time.Time
	due       time.Time
}

func New(delay, retry, maxAge time.Duration, evaluator Evaluator, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		delay:     delay,
		retry:     retry,
		maxAge:    maxAge,
		evaluator: evaluator,
		logger:    logger,
		queued:    make(map[string]struct{}),
		wake:      make(chan struct{}, 1),
	}
}

func (scheduler *Scheduler) Enqueue(path string) {
	scheduler.mu.Lock()
	scheduler.queued[path] = struct{}{}
	scheduler.mu.Unlock()
	select {
	case scheduler.wake <- struct{}{}:
	default:
	}
}

func (scheduler *Scheduler) Run(ctx context.Context) error {
	interval := minDuration(scheduler.delay, scheduler.retry, time.Second)
	if interval <= 0 {
		interval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pending := make(map[string]candidate)
	completed := make(map[string]time.Time)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-scheduler.wake:
			now := time.Now()
			for _, path := range scheduler.drain() {
				if until, exists := completed[path]; exists && now.Before(until) {
					continue
				}
				item, exists := pending[path]
				if !exists {
					item.firstSeen = now
				}
				item.due = now.Add(scheduler.delay)
				pending[path] = item
				scheduler.logger.Info("candidate scheduled", "path", path, "delay", scheduler.delay)
			}
		case now := <-ticker.C:
			for path, until := range completed {
				if !now.Before(until) {
					delete(completed, path)
				}
			}
			for path, item := range pending {
				if now.Before(item.due) {
					continue
				}
				result, err := scheduler.evaluator.Evaluate(ctx, path)
				if err != nil || result.Status == cleanup.BlockedPlex {
					if now.Sub(item.firstSeen) < scheduler.maxAge {
						item.due = now.Add(scheduler.retry)
						pending[path] = item
						scheduler.logger.Warn("candidate check will retry", "path", path, "status", result.Status, "error", err)
						continue
					}
					scheduler.logger.Error("candidate check expired", "path", path, "status", result.Status, "error", err)
					delete(pending, path)
					continue
				}
				scheduler.logger.Info("candidate check completed", "path", path, "status", result.Status, "result_path", result.Path)
				delete(pending, path)
				completed[path] = now.Add(scheduler.delay)
			}
		}
	}
}

func (scheduler *Scheduler) drain() []string {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	paths := make([]string, 0, len(scheduler.queued))
	for path := range scheduler.queued {
		paths = append(paths, path)
		delete(scheduler.queued, path)
	}
	return paths
}

func minDuration(values ...time.Duration) time.Duration {
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}
