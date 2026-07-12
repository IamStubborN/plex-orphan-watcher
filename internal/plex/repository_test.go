package plex

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCountMediaUnderUsesDirectoryBoundary(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "plex.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE media_parts (file TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"/data/tv/Show/Season 01/Show - S01E01.mkv",
		"/data/tv/Showcase/Season 01/Showcase - S01E01.mkv",
	}
	for _, path := range paths {
		if _, err := db.Exec(`INSERT INTO media_parts(file) VALUES (?)`, path); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })

	count, err := repository.CountMediaUnder(context.Background(), "/data/tv/Show")

	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}
