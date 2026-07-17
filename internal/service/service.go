package service

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/cleanup"
	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
	"github.com/IamStubborN/plex-orphan-watcher/internal/state"
)

type Plex interface {
	Snapshot(context.Context) (model.Snapshot, error)
}

type Planner interface {
	Build(model.Pending, model.Snapshot, time.Time) (model.DeletionPlan, error)
}

type Executor interface {
	Execute(model.DeletionPlan) (cleanup.Result, error)
}

type Service struct {
	plex        Plex
	store       *state.Store
	planner     Planner
	executor    Executor
	settleDelay time.Duration
	dryRun      bool
	logger      *slog.Logger
	now         func() time.Time
}

func New(plex Plex, store *state.Store, planner Planner, executor Executor, settleDelay time.Duration, dryRun bool, logger *slog.Logger) *Service {
	return &Service{
		plex: plex, store: store, planner: planner, executor: executor,
		settleDelay: settleDelay, dryRun: dryRun, logger: logger,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (service *Service) Reconcile(ctx context.Context) error {
	snapshot, err := service.plex.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("snapshot Plex inventory: %w", err)
	}
	created, err := service.store.Reconcile(snapshot, service.now(), service.settleDelay)
	if err != nil {
		return fmt.Errorf("persist Plex inventory: %w", err)
	}
	for _, pending := range created {
		service.logger.Info("cleanup candidate discovered",
			"trigger", pending.Trigger, "item_type", pending.Item.Type, "rating_key", pending.Item.RatingKey,
			"title", pending.Item.Title, "decision", "scheduled", "due_at", pending.DueAt)
	}
	return nil
}

func (service *Service) EnqueueEvent(ratingKey string) error {
	now := service.now()
	pending, err := service.store.Enqueue(ratingKey, "plex_event", now, now.Add(service.settleDelay))
	if err != nil {
		return err
	}
	service.logger.Info("cleanup candidate discovered",
		"trigger", pending.Trigger, "item_type", pending.Item.Type, "rating_key", pending.Item.RatingKey,
		"title", pending.Item.Title, "decision", "scheduled", "due_at", pending.DueAt)
	return nil
}

func (service *Service) ProcessDue(ctx context.Context) error {
	pending, err := service.store.Pending(!service.dryRun)
	if err != nil {
		return fmt.Errorf("load pending cleanup: %w", err)
	}
	now := service.now()
	if !hasDue(pending, now) {
		return nil
	}

	snapshot, err := service.plex.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("revalidate Plex inventory: %w", err)
	}
	if _, err := service.store.Reconcile(snapshot, now, service.settleDelay); err != nil {
		return fmt.Errorf("persist revalidated inventory: %w", err)
	}
	pending, err = service.store.Pending(!service.dryRun)
	if err != nil {
		return err
	}
	for _, candidate := range pending {
		if candidate.DueAt.After(now) {
			continue
		}
		plan, err := service.planner.Build(candidate, snapshot, now)
		if err != nil {
			service.logger.Error("cleanup planning failed", "trigger", candidate.Trigger, "rating_key", candidate.Item.RatingKey, "title", candidate.Item.Title, "decision", "retry", "error", err)
			continue
		}
		service.logPlan(candidate, plan)
		if len(plan.Actions) == 0 {
			if err := service.store.Complete(candidate.ID); err != nil {
				return err
			}
			continue
		}
		if service.dryRun || auditOnly(plan) {
			if err := service.store.MarkDryRun(candidate.ID, plan); err != nil {
				return err
			}
			continue
		}
		if err := service.executeRevalidated(ctx, candidate, plan); err != nil {
			service.logger.Error("cleanup execution failed", "rating_key", candidate.Item.RatingKey, "title", candidate.Item.Title, "decision", "retry", "error", err)
			continue
		}
	}
	return nil
}

func (service *Service) executeRevalidated(ctx context.Context, candidate model.Pending, original model.DeletionPlan) error {
	now := service.now()
	snapshot, err := service.plex.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("final Plex inventory check: %w", err)
	}
	if _, err := service.store.Reconcile(snapshot, now, service.settleDelay); err != nil {
		return err
	}
	current, err := findPending(service.store, candidate.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	revalidated, err := service.planner.Build(*current, snapshot, now)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(original.Actions, revalidated.Actions) {
		if err := service.store.ResetWaiting(candidate.ID); err != nil {
			return err
		}
		return fmt.Errorf("cleanup plan changed during final validation")
	}
	result, err := service.executor.Execute(revalidated)
	if err != nil {
		return err
	}
	service.logger.Info("cleanup completed", "mode", "live", "rating_key", candidate.Item.RatingKey, "title", candidate.Item.Title, "decision", "deleted", "deleted_files", result.Deleted, "bytes", result.Bytes)
	return service.store.Complete(candidate.ID)
}

func (service *Service) logPlan(pending model.Pending, plan model.DeletionPlan) {
	mode := "live"
	if service.dryRun {
		mode = "dry_run"
	}
	for _, action := range plan.Actions {
		decision := "delete"
		if service.dryRun || action.RootPolicy == model.PolicyAudit {
			decision = "would_delete"
		}
		service.logger.Info("cleanup plan",
			"trigger", pending.Trigger, "mode", mode, "item_type", pending.Item.Type,
			"rating_key", pending.Item.RatingKey, "title", pending.Item.Title,
			"old_media_path", plan.OldMediaPath, "action", action.Type, "target_path", action.Path,
			"bytes", action.Bytes, "root_policy", action.RootPolicy, "decision", decision, "reason", plan.Reason)
	}
}

func hasDue(pending []model.Pending, now time.Time) bool {
	for _, candidate := range pending {
		if !candidate.DueAt.After(now) {
			return true
		}
	}
	return false
}

func auditOnly(plan model.DeletionPlan) bool {
	for _, action := range plan.Actions {
		if action.RootPolicy != model.PolicyAudit {
			return false
		}
	}
	return true
}

func findPending(store *state.Store, id string) (*model.Pending, error) {
	pending, err := store.Pending(true)
	if err != nil {
		return nil, err
	}
	for _, candidate := range pending {
		if candidate.ID == id {
			copy := candidate
			return &copy, nil
		}
	}
	return nil, nil
}
