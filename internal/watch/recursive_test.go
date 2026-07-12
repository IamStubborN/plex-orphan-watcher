package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecursiveWatcherEmitsTopLevelCandidateOnFileRemoval(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Show")
	season := filepath.Join(show, "Season 01")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	episode := filepath.Join(season, "Show - S01E01.mkv")
	if err := os.WriteFile(episode, []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}

	candidates := make(chan string, 1)
	watcher, err := NewRecursive([]string{root}, func(candidate string) {
		candidates <- candidate
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = watcher.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = watcher.Run(ctx) }()

	if err := os.Remove(episode); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-candidates:
		if got != show {
			t.Fatalf("candidate = %q, want %q", got, show)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for removal event")
	}
}
