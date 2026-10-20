package service

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
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
	logger, logs := bufferedLogger()
	service := New(plex, store, &fakePlanner{}, executor, time.Millisecond, false, logger)
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
	if output := logs.String(); !strings.Contains(output, `decision=canceled`) || !strings.Contains(output, `reason=media_part_present_in_plex`) {
		t.Fatalf("logs = %q, want canceled resolution", output)
	}
}

func TestProcessDueLogsWhenNothingNeedsDeletion(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	store := openStore(t)
	item := itemFixture()
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, time.Minute)
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{}, SyncedAt: now.Add(time.Minute)}, now.Add(time.Minute), time.Minute)
	planner := &fakePlanner{plan: model.DeletionPlan{PendingID: item.RatingKey, Reason: "no_orphans"}}
	plex := &fakePlex{snapshots: []model.Snapshot{{Items: map[string]model.Item{}, SyncedAt: now.Add(3 * time.Minute)}}}
	logger, logs := bufferedLogger()
	service := New(plex, store, planner, &fakeExecutor{}, time.Minute, false, logger)
	service.now = func() time.Time { return now.Add(3 * time.Minute) }
	if err := service.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	pending, err := store.Pending(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %+v, want none", pending)
	}
	if output := logs.String(); !strings.Contains(output, `decision=nothing_to_delete`) || !strings.Contains(output, `reason=no_orphans`) {
		t.Fatalf("logs = %q, want nothing-to-delete resolution", output)
	}
}

func TestProcessDueLogsWhenItemReappearsDuringFinalValidation(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	store := openStore(t)
	item := itemFixture()
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, time.Minute)
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{}, SyncedAt: now.Add(time.Minute)}, now.Add(time.Minute), time.Minute)
	plan := model.DeletionPlan{
		PendingID: item.RatingKey,
		Reason:    "orphan_sidecars",
		Actions:   []model.Action{{Type: model.ActionDeleteFile, Path: "/data/media/Show/Episode.srt", RootPolicy: model.PolicyDelete}},
	}
	plex := &fakePlex{snapshots: []model.Snapshot{
		{Items: map[string]model.Item{}, SyncedAt: now.Add(3 * time.Minute)},
		{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now.Add(3*time.Minute + time.Second)},
	}}
	executor := &fakeExecutor{}
	logger, logs := bufferedLogger()
	service := New(plex, store, &fakePlanner{plan: plan}, executor, time.Minute, false, logger)
	service.now = func() time.Time { return now.Add(3 * time.Minute) }
	if err := service.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if output := logs.String(); !strings.Contains(output, `decision=canceled`) || !strings.Contains(output, `reason=media_part_present_in_plex`) {
		t.Fatalf("logs = %q, want canceled resolution", output)
	}
}

func TestProcessDueReschedulesWhileVideoStillExists(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	store := openStore(t)
	item := itemFixture()
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, time.Minute)
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{}, SyncedAt: now.Add(time.Minute)}, now.Add(time.Minute), time.Minute)
	planner := &fakePlanner{plan: model.DeletionPlan{PendingID: item.RatingKey, Reason: "video_present_on_disk"}}
	executor := &fakeExecutor{}
	plex := &fakePlex{snapshots: []model.Snapshot{{Items: map[string]model.Item{}, SyncedAt: now.Add(3 * time.Minute)}}}
	service := New(plex, store, planner, executor, 15*time.Minute, false, discardLogger())
	service.now = func() time.Time { return now.Add(3 * time.Minute) }
	if err := service.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	pending, err := store.Pending(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Status != model.PendingWaiting {
		t.Fatalf("pending = %+v, want rescheduled item", pending)
	}
	if want := now.Add(18 * time.Minute); !pending[0].DueAt.Equal(want) {
		t.Fatalf("due at = %s, want %s", pending[0].DueAt, want)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
}

func TestProcessDueDoesNotReplanAuditOnlyPlanUntilRestart(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store := openStore(t)
	item := removedItem(t, store, now)
	plan := model.DeletionPlan{PendingID: item.RatingKey, Actions: []model.Action{{Type: model.ActionDeleteTree, Path: "/data/torrents/Show", RootPolicy: model.PolicyAudit}}}
	plex := &fakePlex{snapshots: []model.Snapshot{{Items: map[string]model.Item{}, SyncedAt: now.Add(3 * time.Minute)}}}
	executor := &fakeExecutor{}
	tick := func(service *Service, minute int) {
		t.Helper()
		service.now = func() time.Time { return now.Add(time.Duration(minute) * time.Minute) }
		if err := service.ProcessDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	service := New(plex, store, &fakePlanner{plan: plan}, executor, time.Minute, false, discardLogger())
	service.startedAt = now.Add(2 * time.Minute)
	for minute := 3; minute <= 5; minute++ {
		tick(service, minute)
	}
	if plex.calls != 1 || executor.calls != 0 {
		t.Fatalf("snapshot calls = %d, executor calls = %d; want 1 and 0", plex.calls, executor.calls)
	}
	pending, _ := store.Pending(true)
	if len(pending) != 1 || pending[0].Status != model.PendingDryRun {
		t.Fatalf("pending = %+v", pending)
	}

	restarted := New(plex, store, &fakePlanner{plan: plan}, executor, time.Minute, false, discardLogger())
	restarted.startedAt = now.Add(6 * time.Minute)
	tick(restarted, 6)
	tick(restarted, 7)
	if plex.calls != 2 || executor.calls != 0 {
		t.Fatalf("after restart: snapshot calls = %d, executor calls = %d; want 2 and 0", plex.calls, executor.calls)
	}
}

func TestProcessDueExecutesEarlierDryRunPlanWhenLive(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store := openStore(t)
	item := removedItem(t, store, now)
	plan := model.DeletionPlan{PendingID: item.RatingKey, Actions: []model.Action{{Type: model.ActionDeleteFile, Path: "/data/media/Show/Episode.srt", RootPolicy: model.PolicyDelete}}}
	plex := &fakePlex{snapshots: []model.Snapshot{{Items: map[string]model.Item{}, SyncedAt: now.Add(3 * time.Minute)}}}
	dryRun := New(plex, store, &fakePlanner{plan: plan}, &fakeExecutor{}, time.Minute, true, discardLogger())
	dryRun.now = func() time.Time { return now.Add(3 * time.Minute) }
	if err := dryRun.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}

	executor := &fakeExecutor{}
	live := New(plex, store, &fakePlanner{plan: plan}, executor, time.Minute, false, discardLogger())
	live.startedAt = now.Add(4 * time.Minute)
	live.now = func() time.Time { return now.Add(4 * time.Minute) }
	if err := live.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
}

func removedItem(t *testing.T, store *state.Store, now time.Time) model.Item {
	t.Helper()
	item := itemFixture()
	if _, err := store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reconcile(model.Snapshot{Items: map[string]model.Item{}, SyncedAt: now.Add(time.Minute)}, now.Add(time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	return item
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
	calls     int
}

func (fake *fakePlex) Snapshot(context.Context) (model.Snapshot, error) {
	fake.calls++
	value := fake.snapshots[fake.index]
	if fake.index < len(fake.snapshots)-1 {
		fake.index++
	}
	return value, nil
}

type fakePlanner struct {
	plan model.DeletionPlan
}

func (fake *fakePlanner) Build(_ model.Pending, _ model.Snapshot, now time.Time) (model.DeletionPlan, error) {
	plan := fake.plan
	plan.CreatedAt = now
	return plan, nil
}

type fakeExecutor struct {
	calls int
}

func (fake *fakeExecutor) Execute(model.DeletionPlan) (cleanup.Result, error) {
	fake.calls++
	return cleanup.Result{}, nil
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func bufferedLogger() (*slog.Logger, *bytes.Buffer) {
	var buffer bytes.Buffer
	return slog.New(slog.NewTextHandler(&buffer, nil)), &buffer
}
