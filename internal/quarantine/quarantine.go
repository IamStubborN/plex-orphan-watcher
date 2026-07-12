package quarantine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DirectoryName = ".plex-orphan-quarantine"

func Move(candidate string, now time.Time) (string, error) {
	root := filepath.Dir(candidate)
	directory := filepath.Join(root, DirectoryName)
	if err := ensureDirectory(directory); err != nil {
		return "", err
	}
	target := filepath.Join(directory, fmt.Sprintf("%d-%s", now.UnixNano(), filepath.Base(candidate)))
	if err := os.Rename(candidate, target); err != nil {
		return "", fmt.Errorf("move candidate %q to quarantine: %w", candidate, err)
	}
	if err := os.Chtimes(target, now, now); err != nil {
		if rollbackErr := os.Rename(target, candidate); rollbackErr != nil {
			return "", fmt.Errorf("set quarantine retention timestamp: %w (rollback failed: %v)", err, rollbackErr)
		}
		return "", fmt.Errorf("set quarantine retention timestamp: %w", err)
	}
	return target, nil
}

func Reap(root string, cutoff time.Time) (int, error) {
	directory := filepath.Join(filepath.Clean(root), DirectoryName)
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inspect quarantine %q: %w", directory, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("quarantine %q is not a real directory", directory)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, fmt.Errorf("read quarantine %q: %w", directory, err)
	}
	removed := 0
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		entryInfo, err := os.Lstat(path)
		if err != nil {
			return removed, fmt.Errorf("inspect quarantine entry %q: %w", path, err)
		}
		if !entryInfo.ModTime().Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return removed, fmt.Errorf("remove expired quarantine entry %q: %w", path, err)
		}
		removed++
	}
	return removed, nil
}

func Run(ctx context.Context, roots []string, retention time.Duration, logger *slog.Logger) error {
	reap := func(now time.Time) {
		for _, root := range roots {
			removed, err := Reap(root, now.Add(-retention))
			if err != nil {
				logger.Error("quarantine reap failed", "root", root, "error", err)
				continue
			}
			if removed > 0 {
				logger.Info("expired quarantine entries removed", "root", root, "count", removed)
			}
		}
	}
	reap(time.Now())
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			reap(now)
		}
	}
}

func Contains(path string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == DirectoryName {
			return true
		}
	}
	return false
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create quarantine %q: %w", path, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect quarantine %q: %w", path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("quarantine %q is not a real directory", path)
	}
	return nil
}
