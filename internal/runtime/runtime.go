package runtime

import (
	"context"
	"log/slog"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/plex"
	"golang.org/x/sync/errgroup"
)

type EventSource interface {
	StreamEvents(context.Context, func(plex.DeletionEvent)) error
}

type Service interface {
	Reconcile(context.Context) error
	ProcessDue(context.Context) error
	EnqueueEvent(string) error
}

func Run(ctx context.Context, source EventSource, service Service, reconcileInterval time.Duration, logger *slog.Logger) error {
	events := make(chan plex.DeletionEvent, 256)
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return runEventSource(groupCtx, source, events, logger)
	})

	if err := service.Reconcile(groupCtx); err != nil {
		return err
	}
	if err := service.ProcessDue(groupCtx); err != nil {
		logger.Error("initial pending cleanup failed", "error", err)
	}

	reconcileTicker := time.NewTicker(reconcileInterval)
	defer reconcileTicker.Stop()
	processTicker := time.NewTicker(30 * time.Second)
	defer processTicker.Stop()
	group.Go(func() error {
		for {
			select {
			case <-groupCtx.Done():
				return nil
			case event := <-events:
				if err := service.EnqueueEvent(event.RatingKey); err != nil {
					logger.Warn("Plex deletion event requires inventory reconciliation", "rating_key", event.RatingKey, "error", err)
					if err := service.Reconcile(groupCtx); err != nil {
						logger.Error("event-triggered reconciliation failed", "error", err)
					}
				}
			case <-reconcileTicker.C:
				if err := service.Reconcile(groupCtx); err != nil {
					logger.Error("scheduled reconciliation failed", "error", err)
				}
			case <-processTicker.C:
				if err := service.ProcessDue(groupCtx); err != nil {
					logger.Error("pending cleanup processing failed", "error", err)
				}
			}
		}
	})
	return group.Wait()
}

func runEventSource(ctx context.Context, source EventSource, events chan<- plex.DeletionEvent, logger *slog.Logger) error {
	backoff := time.Second
	for {
		err := source.StreamEvents(ctx, func(event plex.DeletionEvent) {
			select {
			case events <- event:
			case <-ctx.Done():
			}
		})
		if ctx.Err() != nil {
			return nil
		}
		logger.Warn("Plex EventSource disconnected", "error", err, "reconnect_in", backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff *= 2
		if backoff > time.Minute {
			backoff = time.Minute
		}
	}
}
