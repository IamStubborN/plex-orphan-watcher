package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasPrimaryVideoIgnoresKnownExtras(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		filepath.Join(dir, "Season 01", "RUS Subs", "episode.rus.ass"),
		filepath.Join(dir, "Season 01", "Extra", "NC", "NCOP.mkv"),
		filepath.Join(dir, "trailers", "Official Trailer.mkv"),
	}
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	hasVideo, err := HasPrimaryVideo(dir)

	if err != nil {
		t.Fatal(err)
	}
	if hasVideo {
		t.Fatal("extras and subtitle sidecars must not count as primary video")
	}
}

func TestHasPrimaryVideoFindsEpisodeOutsideExtras(t *testing.T) {
	dir := t.TempDir()
	episode := filepath.Join(dir, "Season 01", "Show - S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(episode), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(episode, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}

	hasVideo, err := HasPrimaryVideo(dir)

	if err != nil {
		t.Fatal(err)
	}
	if !hasVideo {
		t.Fatal("episode video must block recursive deletion")
	}
}
