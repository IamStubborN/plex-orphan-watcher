package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/cleanup"
	"github.com/IamStubborN/plex-orphan-watcher/internal/plex"
	"github.com/IamStubborN/plex-orphan-watcher/internal/qbittorrent"
	"github.com/IamStubborN/plex-orphan-watcher/internal/scheduler"
	watcher "github.com/IamStubborN/plex-orphan-watcher/internal/watch"
)

func TestRemovedLastEpisodeQuarantinesOnlyOrphanedShowDirectory(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Show")
	episode := filepath.Join(show, "Season 01", "Show - S01E01.mkv")
	subtitle := filepath.Join(show, "Season 01", "Show - S01E01.ru.ass")
	for _, file := range []string{episode, subtitle} {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	plexServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Plex-Token") != "secret" {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/library/sections":
			_, _ = io.WriteString(response, `{"MediaContainer":{"Directory":[{"key":"2","type":"show"}]}}`)
		case "/library/sections/2/all":
			_, _ = io.WriteString(response, `{"MediaContainer":{"totalSize":0,"size":0,"Metadata":[]}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(plexServer.Close)
	qbit := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v2/torrents/info" {
			http.NotFound(response, request)
			return
		}
		_, _ = io.WriteString(response, `[]`)
	}))
	t.Cleanup(qbit.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	evaluator := cleanup.New([]string{root}, plex.New(plexServer.URL, "secret"), qbittorrent.New(qbit.URL, "", ""), false)
	queue := scheduler.New(20*time.Millisecond, 20*time.Millisecond, time.Second, evaluator, logger)
	filesystemWatcher, err := watcher.NewRecursive([]string{root}, queue.Enqueue)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = filesystemWatcher.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = queue.Run(ctx) }()
	go func() { _ = filesystemWatcher.Run(ctx) }()

	if err := os.Remove(episode); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(show); os.IsNotExist(err) {
			matches, globErr := filepath.Glob(filepath.Join(root, ".plex-orphan-quarantine", "*-Show"))
			if globErr != nil {
				t.Fatal(globErr)
			}
			if len(matches) == 1 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("orphaned show directory was not quarantined")
}
