package integration

import (
	"context"
	"database/sql"
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
	_ "modernc.org/sqlite"
)

func TestRemovedLastEpisodeDeletesOnlyOrphanedShowDirectory(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Show")
	episode := filepath.Join(show, "Season 01", "Show - S01E01.mkv")
	extra := filepath.Join(show, "Season 01", "Extra", "NCOP.mkv")
	for _, file := range []string{episode, extra} {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	databasePath := filepath.Join(t.TempDir(), "plex.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE media_parts (file TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO media_parts(file) VALUES (?)`, episode); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM media_parts WHERE file = ?`, episode); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err := plex.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	qbit := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v2/torrents/info" {
			http.NotFound(response, request)
			return
		}
		_, _ = io.WriteString(response, `[]`)
	}))
	t.Cleanup(qbit.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	evaluator := cleanup.New([]string{root}, repository, qbittorrent.New(qbit.URL, "", ""), false)
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
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("orphaned show directory was not deleted")
}
