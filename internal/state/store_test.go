package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
	bolt "go.etcd.io/bbolt"
)

func TestOpenRejectsCorruptDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watcher.db")
	if err := os.WriteFile(path, []byte("not a bbolt database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected corrupt database error")
	}
}

func TestPendingReturnsEmptySlice(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "watcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pending, err := store.Pending(true)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || len(pending) != 0 {
		t.Fatalf("pending = %#v, want non-nil empty slice", pending)
	}
}

func TestReconcilePersistsRemovedItemAsPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watcher.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	item := fixtureItem()
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	if _, err := store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pending, err := store.Reconcile(model.Snapshot{Items: map[string]model.Item{}, SyncedAt: now.Add(time.Minute)}, now.Add(time.Minute), 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Item.RatingKey != item.RatingKey {
		t.Fatalf("pending = %+v", pending)
	}
	if want := now.Add(16 * time.Minute); !pending[0].DueAt.Equal(want) {
		t.Fatalf("DueAt = %s, want %s", pending[0].DueAt, want)
	}
}

func TestReconcilePersistsOnlyRemovedPartWhenItemRemains(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "watcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	item := fixtureItem()
	item.Parts = []string{"/data/movies/Movie/Movie-part1.mkv", "/data/movies/Movie/Movie-part2.mkv"}
	if _, err := store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, time.Minute); err != nil {
		t.Fatal(err)
	}
	current := item
	current.Parts = []string{item.Parts[1]}
	created, err := store.Reconcile(model.Snapshot{Items: map[string]model.Item{current.RatingKey: current}, SyncedAt: now.Add(time.Minute)}, now.Add(time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || len(created[0].Item.Parts) != 1 || created[0].Item.Parts[0] != item.Parts[0] {
		t.Fatalf("created = %+v, want only removed part", created)
	}
}

func TestReconcileCancelsOnlyPartThatReappears(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "watcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	item := fixtureItem()
	item.Parts = []string{"/data/movies/Movie/Movie-part1.mkv", "/data/movies/Movie/Movie-part2.mkv"}
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, time.Minute)
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{}, SyncedAt: now.Add(time.Minute)}, now.Add(time.Minute), time.Minute)
	reappeared := item
	reappeared.Parts = []string{item.Parts[0]}
	_, err = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: reappeared}, SyncedAt: now.Add(2 * time.Minute)}, now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.Pending(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || len(pending[0].Item.Parts) != 1 || pending[0].Item.Parts[0] != item.Parts[1] {
		t.Fatalf("pending = %+v, want only still-missing part", pending)
	}
}

func TestReconcileResetsSettleDelayWhenAnotherPartDisappears(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "watcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	item := fixtureItem()
	item.Parts = []string{"/data/movies/Movie/part1.mkv", "/data/movies/Movie/part2.mkv"}
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, 15*time.Minute)
	partTwoOnly := item
	partTwoOnly.Parts = []string{item.Parts[1]}
	_, err = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: partTwoOnly}, SyncedAt: now.Add(time.Minute)}, now.Add(time.Minute), 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Reconcile(model.Snapshot{Items: map[string]model.Item{}, SyncedAt: now.Add(10 * time.Minute)}, now.Add(10*time.Minute), 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.Pending(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || len(pending[0].Item.Parts) != 2 {
		t.Fatalf("pending = %+v, want both missing parts", pending)
	}
	if want := now.Add(25 * time.Minute); !pending[0].DueAt.Equal(want) {
		t.Fatalf("due at = %s, want %s", pending[0].DueAt, want)
	}
}

func TestIsLockTimeoutRecognizesWrappedTimeout(t *testing.T) {
	if !IsLockTimeout(fmt.Errorf("wrapped: %w", bolt.ErrTimeout)) {
		t.Fatal("wrapped bbolt timeout was not recognized")
	}
	if IsLockTimeout(errors.New("corrupt database")) {
		t.Fatal("non-lock error was recognized as lock timeout")
	}
}

func TestReconcileCancelsPendingWhenItemReappears(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "watcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	item := fixtureItem()
	now := time.Now().UTC()
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, time.Minute)
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{}, SyncedAt: now.Add(time.Minute)}, now.Add(time.Minute), time.Minute)
	_, err = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now.Add(2 * time.Minute)}, now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.Pending(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %+v, want none", pending)
	}
}

func TestEventEnqueueAndDryRunPlanSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watcher.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	item := fixtureItem()
	now := time.Now().UTC()
	_, _ = store.Reconcile(model.Snapshot{Items: map[string]model.Item{item.RatingKey: item}, SyncedAt: now}, now, time.Minute)
	pending, err := store.Enqueue(item.RatingKey, "plex_event", now.Add(time.Minute), now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	plan := model.DeletionPlan{PendingID: pending.ID, Actions: []model.Action{{Type: model.ActionDeleteFile, Path: "/data/tv/Show/Show - S01E01.ru.ass", Bytes: 10}}}
	if err := store.MarkDryRun(pending.ID, plan); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	items, err := store.Pending(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != model.PendingDryRun || len(items[0].Plan.Actions) != 1 {
		t.Fatalf("pending after restart = %+v", items)
	}
}

func fixtureItem() model.Item {
	return model.Item{
		RatingKey:            "101",
		Type:                 model.ItemEpisode,
		SectionID:            "2",
		Title:                "Episode 1",
		ParentRatingKey:      "51",
		ParentTitle:          "Season 1",
		GrandparentRatingKey: "11",
		GrandparentTitle:     "Show",
		Parts:                []string{"/data/tv/Show/Season 01/Show - S01E01.mkv"},
	}
}
