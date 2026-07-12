package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/media"
	"github.com/IamStubborN/plex-orphan-watcher/internal/quarantine"
)

type Status string

const (
	Deleted        Status = "deleted"
	Quarantined    Status = "quarantined"
	DryRun         Status = "dry_run"
	Missing        Status = "missing"
	BlockedPlex    Status = "blocked_plex"
	BlockedVideo   Status = "blocked_video"
	BlockedTorrent Status = "blocked_torrent"
)

type Result struct {
	Status Status
	Path   string
}

type Plex interface {
	CountMediaUnder(ctx context.Context, directory string) (int, error)
}

type Torrents interface {
	Managed(ctx context.Context, directory string) (bool, error)
}

type Evaluator struct {
	roots    []string
	plex     Plex
	torrents Torrents
	dryRun   bool
}

func New(roots []string, plex Plex, torrents Torrents, dryRun bool) *Evaluator {
	cleanRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		if absolute, err := filepath.Abs(root); err == nil {
			cleanRoots = append(cleanRoots, filepath.Clean(absolute))
		}
	}
	return &Evaluator{roots: cleanRoots, plex: plex, torrents: torrents, dryRun: dryRun}
}

func (evaluator *Evaluator) Evaluate(ctx context.Context, candidate string) (Result, error) {
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return Result{}, fmt.Errorf("resolve candidate: %w", err)
	}
	candidate = filepath.Clean(candidate)
	if !evaluator.allowedCandidate(candidate) {
		return Result{}, fmt.Errorf("candidate %q is not a direct child of an allowed root", candidate)
	}

	info, err := os.Lstat(candidate)
	if errors.Is(err, os.ErrNotExist) {
		return Result{Status: Missing}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("inspect candidate %q: %w", candidate, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Result{}, fmt.Errorf("candidate %q is not a real directory", candidate)
	}

	result, err := evaluator.checkGuards(ctx, candidate)
	if err != nil || result.Status != "" {
		return result, err
	}
	if evaluator.dryRun {
		return Result{Status: DryRun}, nil
	}

	// Repeat all dynamic checks immediately before quarantine. Plex scans,
	// downloads, and file copies can race with the initial evaluation.
	result, err = evaluator.checkGuards(ctx, candidate)
	if err != nil || result.Status != "" {
		return result, err
	}
	quarantinePath, err := quarantine.Move(candidate, time.Now())
	if err != nil {
		return Result{}, err
	}
	return Result{Status: Quarantined, Path: quarantinePath}, nil
}

func (evaluator *Evaluator) checkGuards(ctx context.Context, candidate string) (Result, error) {
	count, err := evaluator.plex.CountMediaUnder(ctx, candidate)
	if err != nil {
		return Result{}, err
	}
	if count > 0 {
		return Result{Status: BlockedPlex}, nil
	}

	hasVideo, err := media.HasVideo(candidate)
	if err != nil {
		return Result{}, fmt.Errorf("scan candidate %q: %w", candidate, err)
	}
	if hasVideo {
		return Result{Status: BlockedVideo}, nil
	}

	managed, err := evaluator.torrents.Managed(ctx, candidate)
	if err != nil {
		return Result{}, err
	}
	if managed {
		return Result{Status: BlockedTorrent}, nil
	}
	return Result{}, nil
}

func (evaluator *Evaluator) allowedCandidate(candidate string) bool {
	for _, root := range evaluator.roots {
		relative, err := filepath.Rel(root, candidate)
		if err == nil && relative != "." && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
			!strings.Contains(relative, string(filepath.Separator)) {
			return true
		}
	}
	return false
}
