package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakePlex struct {
	count int
	err   error
}

func (fake fakePlex) CountMediaUnder(context.Context, string) (int, error) {
	return fake.count, fake.err
}

type fakeTorrents struct {
	managed bool
	err     error
}

type changingPlex struct {
	counts []int
}

func (fake *changingPlex) CountMediaUnder(context.Context, string) (int, error) {
	count := fake.counts[0]
	fake.counts = fake.counts[1:]
	return count, nil
}

func (fake fakeTorrents) Managed(context.Context, string) (bool, error) {
	return fake.managed, fake.err
}

func TestEvaluateDeletesOrphanWithOnlyExtras(t *testing.T) {
	root, show := orphanFixture(t)
	evaluator := New([]string{root}, fakePlex{}, fakeTorrents{}, false)

	result, err := evaluator.Evaluate(context.Background(), show)

	if err != nil {
		t.Fatal(err)
	}
	if result.Status != Deleted {
		t.Fatalf("status = %q, want %q", result.Status, Deleted)
	}
	if _, err := os.Stat(show); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate still exists or stat failed unexpectedly: %v", err)
	}
}

func TestEvaluateDryRunKeepsCandidate(t *testing.T) {
	root, show := orphanFixture(t)
	evaluator := New([]string{root}, fakePlex{}, fakeTorrents{}, true)

	result, err := evaluator.Evaluate(context.Background(), show)

	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DryRun {
		t.Fatalf("status = %q, want %q", result.Status, DryRun)
	}
	if _, err := os.Stat(show); err != nil {
		t.Fatalf("dry-run removed candidate: %v", err)
	}
}

func TestEvaluateFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		plex     fakePlex
		torrents fakeTorrents
		primary  bool
		status   Status
		wantErr  bool
	}{
		{name: "indexed by Plex", plex: fakePlex{count: 1}, status: BlockedPlex},
		{name: "primary video remains", primary: true, status: BlockedPrimaryVideo},
		{name: "managed by torrent", torrents: fakeTorrents{managed: true}, status: BlockedTorrent},
		{name: "Plex unavailable", plex: fakePlex{err: errors.New("offline")}, wantErr: true},
		{name: "qBittorrent unavailable", torrents: fakeTorrents{err: errors.New("offline")}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, show := orphanFixture(t)
			if test.primary {
				episode := filepath.Join(show, "Season 01", "Show - S01E01.mkv")
				if err := os.WriteFile(episode, []byte("episode"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			evaluator := New([]string{root}, test.plex, test.torrents, false)

			result, err := evaluator.Evaluate(context.Background(), show)

			if test.wantErr {
				if err == nil {
					t.Fatal("expected dependency error")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if result.Status != test.status {
					t.Fatalf("status = %q, want %q", result.Status, test.status)
				}
			}
			if _, statErr := os.Stat(show); statErr != nil {
				t.Fatalf("blocked candidate was removed: %v", statErr)
			}
		})
	}
}

func TestEvaluateRejectsPathOutsideConfiguredRoots(t *testing.T) {
	root := t.TempDir()
	_, outside := orphanFixture(t)
	evaluator := New([]string{root}, fakePlex{}, fakeTorrents{}, false)

	if _, err := evaluator.Evaluate(context.Background(), outside); err == nil {
		t.Fatal("expected path outside configured roots to be rejected")
	}
}

func TestEvaluateRepeatsGuardsImmediatelyBeforeDeletion(t *testing.T) {
	root, show := orphanFixture(t)
	plex := &changingPlex{counts: []int{0, 1}}
	evaluator := New([]string{root}, plex, fakeTorrents{}, false)

	result, err := evaluator.Evaluate(context.Background(), show)

	if err != nil {
		t.Fatal(err)
	}
	if result.Status != BlockedPlex {
		t.Fatalf("status = %q, want %q", result.Status, BlockedPlex)
	}
	if _, err := os.Stat(show); err != nil {
		t.Fatalf("candidate was removed after guard state changed: %v", err)
	}
}

func orphanFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	show := filepath.Join(root, "Show")
	extra := filepath.Join(show, "Season 01", "Extra", "NCOP.mkv")
	if err := os.MkdirAll(filepath.Dir(extra), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(show, "subtitle.ass"), []byte("subtitle"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, show
}
