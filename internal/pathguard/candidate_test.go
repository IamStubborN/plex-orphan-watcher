package pathguard

import (
	"path/filepath"
	"testing"
)

func TestCandidateFromEventRejectsQuarantine(t *testing.T) {
	root := t.TempDir()
	if candidate, valid := CandidateFromEvent(root, filepath.Join(root, ".plex-orphan-quarantine", "Show", "file.ass")); valid {
		t.Fatalf("quarantine event produced candidate %q", candidate)
	}
}

func TestCandidateFromEventReturnsDirectChild(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "The Eminence in Shadow (2022)")
	event := filepath.Join(show, "Season 01", "episode.mkv")

	got, ok := CandidateFromEvent(root, event)

	if !ok {
		t.Fatal("expected event to resolve to a show candidate")
	}
	if got != show {
		t.Fatalf("candidate = %q, want %q", got, show)
	}
}

func TestCandidateFromEventRejectsRootAndOutsidePaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	tests := []string{
		root,
		filepath.Join(outside, "episode.mkv"),
		filepath.Join(root, "..", filepath.Base(outside), "episode.mkv"),
	}
	for _, event := range tests {
		if got, ok := CandidateFromEvent(root, event); ok {
			t.Fatalf("CandidateFromEvent(%q) = %q, true; want rejection", event, got)
		}
	}
}
