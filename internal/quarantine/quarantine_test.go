package quarantine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReapRemovesOnlyExpiredEntries(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, DirectoryName)
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	oldEntry := filepath.Join(directory, "old-show")
	newEntry := filepath.Join(directory, "new-show")
	for _, entry := range []string{oldEntry, newEntry} {
		if err := os.Mkdir(entry, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(oldEntry, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	removed, err := Reap(root, now.Add(-7*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(oldEntry); !os.IsNotExist(err) {
		t.Fatalf("expired entry still exists: %v", err)
	}
	if _, err := os.Stat(newEntry); err != nil {
		t.Fatalf("fresh entry was removed: %v", err)
	}
}

func TestMoveRefreshesRetentionTimestamp(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "Old Show")
	if err := os.Mkdir(candidate, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(candidate, old, old); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	target, err := Move(candidate, now)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().Before(now.Add(-time.Second)) {
		t.Fatalf("quarantine timestamp = %s, want approximately %s", info.ModTime(), now)
	}
}
