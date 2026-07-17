package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/cleanup"
	"github.com/IamStubborN/plex-orphan-watcher/internal/planner"
	"github.com/IamStubborN/plex-orphan-watcher/internal/plex"
	"github.com/IamStubborN/plex-orphan-watcher/internal/service"
	"github.com/IamStubborN/plex-orphan-watcher/internal/state"
)

func TestDryRunDiscoversOrphansWithoutMutation(t *testing.T) {
	fixture := newFixture(t, true)
	fixture.removeVideo(t)
	fixture.reconcileAndProcess(t)

	if _, err := os.Stat(fixture.subtitle); err != nil {
		t.Fatalf("dry-run removed subtitle: %v", err)
	}
	pending, err := fixture.store.Pending(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || len(pending[0].Plan.Actions) != 1 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestLiveModeRemovesEmptyShowTreeAfterRepeatedPlexValidation(t *testing.T) {
	fixture := newFixture(t, false)
	fixture.removeVideo(t)
	fixture.reconcileAndProcess(t)

	if _, err := os.Stat(fixture.show); !os.IsNotExist(err) {
		t.Fatalf("show still exists: %v", err)
	}
	pending, err := fixture.store.Pending(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}

type fixture struct {
	root     string
	show     string
	video    string
	subtitle string
	deleted  atomic.Bool
	store    *state.Store
	service  *service.Service
}

func newFixture(t *testing.T, dryRun bool) *fixture {
	t.Helper()
	root := t.TempDir()
	show := filepath.Join(root, "Show")
	video := filepath.Join(show, "Season 01", "Show - S01E01.mkv")
	subtitle := filepath.Join(show, "Season 01", "Show - S01E01.ru.ass")
	for _, file := range []string{video, subtitle} {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result := &fixture{root: root, show: show, video: video, subtitle: subtitle}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/library/sections":
			writeJSON(t, response, map[string]any{"MediaContainer": map[string]any{"Directory": []any{
				map[string]any{"key": "2", "type": "show", "Location": []any{map[string]any{"path": root}}},
			}}})
		case "/library/sections/2/all":
			metadata := []any{}
			if !result.deleted.Load() {
				metadata = append(metadata, map[string]any{
					"ratingKey": "101", "type": "episode", "title": "Episode 1",
					"parentRatingKey": "51", "parentTitle": "Season 1",
					"grandparentRatingKey": "11", "grandparentTitle": "Show",
					"Media": []any{map[string]any{"Part": []any{map[string]any{"file": video}}}},
				})
			}
			writeJSON(t, response, map[string]any{"MediaContainer": map[string]any{"totalSize": len(metadata), "size": len(metadata), "Metadata": metadata}})
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	store, err := state.Open(filepath.Join(t.TempDir(), "watcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	result.store = store
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := plex.New(server.URL, "secret")
	result.service = service.New(client, store, planner.New([]string{root}, nil), cleanup.NewExecutor(dryRun), time.Millisecond, dryRun, logger)
	if err := result.service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	return result
}

func (fixture *fixture) removeVideo(t *testing.T) {
	t.Helper()
	if err := os.Remove(fixture.video); err != nil {
		t.Fatal(err)
	}
	fixture.deleted.Store(true)
}

func (fixture *fixture) reconcileAndProcess(t *testing.T) {
	t.Helper()
	if err := fixture.service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := fixture.service.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(value); err != nil {
		t.Errorf("encode fixture: %v", err)
	}
}
