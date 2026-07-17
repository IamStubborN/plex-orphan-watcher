package service

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/cleanup"
	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
	"github.com/IamStubborN/plex-orphan-watcher/internal/state"
)

func TestReconcileRecoversDeletionMissedWhileStopped(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store := openStore(t)
	item := itemFixture()
	plex := &fakePlex{snapshots: []model.Snapshot{
		{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now},
		{Items: map[string]model.Item{}, SyncedAt: now.Add(time.Minute)},
	}}
	service := New(plex, store, &fakePlanner{}, &fakeExecutor{}, 15*time.Minute, true, discardLogger())
	service.now = func() time.Time { return now }
	if err := service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(time.Minute) }
	if err := service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	pending, err := store.Pending(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Trigger != "reconcile" {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestProcessDueStoresDryRunPlanWithoutExecuting(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store := openStore(t)
	item := itemFixture()
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, time.Minute)
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{}, SyncedAt: now.Add(time.Minute)}, now.Add(time.Minute), time.Minute)
	plan := model.DeletionPlan{PendingID: item.RatingKey, Actions: []model.Action{{Type: model.ActionDeleteFile, Path: "/data/media/Show/Episode.srt", RootPolicy: model.PolicyDelete}}}
	planner := &fakePlanner{plan: plan}
	executor := &fakeExecutor{}
	plex := &fakePlex{snapshots: []model.Snapshot{{Items: map[string]model.Item{}, SyncedAt: now.Add(3 * time.Minute)}}}
	service := New(plex, store, planner, executor, time.Minute, true, discardLogger())
	service.now = func() time.Time { return now.Add(3 * time.Minute) }
	if err := service.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d", executor.calls)
	}
	pending, _ := store.Pending(true)
	if len(pending) != 1 || pending[0].Status != model.PendingDryRun || len(pending[0].Plan.Actions) != 1 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestProcessDueCancelsWhenItemReappears(t *testing.T) {
	now := time.Now().UTC()
	store := openStore(t)
	item := itemFixture()
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, time.Millisecond)
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{}, SyncedAt: now.Add(time.Second)}, now.Add(time.Second), time.Millisecond)
	executor := &fakeExecutor{}
	plex := &fakePlex{snapshots: []model.Snapshot{{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now.Add(2 * time.Second)}}}
	service := New(plex, store, &fakePlanner{}, executor, time.Millisecond, false, discardLogger())
	service.now = func() time.Time { return now.Add(2 * time.Second) }
	if err := service.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d", executor.calls)
	}
	pending, _ := store.Pending(true)
	if len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}

func openStore(t *testing.T) *state.Store {
	t.Helper()
	store, err := state.Open(filepath.Join(t.TempDir(), "watcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func itemFixture() model.Item {
	return model.Item{RatingKey: "101", Type: model.ItemEpisode, Title: "Episode", Parts: []string{"/data/media/Show/Season 01/Episode.mkv"}}
}

type fakePlex struct {
	snapshots []model.Snapshot
	index     int
}

func (fake *fakePlex) Snapshot(context.Context) (model.Snapshot, error) {
	value := fake.snapshots[fake.index]
	if fake.index < len(fake.snapshots)-1 {
		fake.index++
	}
	return value, nil
}

type fakePlanner struct {
	plan model.DeletionPlan
}

func (fake *fakePlanner) Build(model.Pending, model.Snapshot, time.Time) (model.DeletionPlan, error) {
	return fake.plan, nil
}

type fakeExecutor struct {
	calls int
}

func (fake *fakeExecutor) Execute(model.DeletionPlan) (cleanup.Result, error) {
	fake.calls++
	return cleanup.Result{}, nil
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
