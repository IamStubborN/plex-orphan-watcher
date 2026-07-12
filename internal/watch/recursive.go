package watch

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/IamStubborN/plex-orphan-watcher/internal/pathguard"
	"github.com/fsnotify/fsnotify"
)

type Recursive struct {
	roots   []string
	handler func(string)
	watcher *fsnotify.Watcher
}

func NewRecursive(roots []string, handler func(string)) (*Recursive, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create filesystem watcher: %w", err)
	}
	recursive := &Recursive{handler: handler, watcher: watcher}
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("resolve watch root %q: %w", root, err)
		}
		absolute = filepath.Clean(absolute)
		if err := recursive.addTree(absolute); err != nil {
			_ = watcher.Close()
			return nil, err
		}
		recursive.roots = append(recursive.roots, absolute)
	}
	return recursive, nil
}

func (watcher *Recursive) Close() error {
	return watcher.watcher.Close()
}

func (watcher *Recursive) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-watcher.watcher.Errors:
			if !ok {
				return nil
			}
			return fmt.Errorf("filesystem watcher: %w", err)
		case event, ok := <-watcher.watcher.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := watcher.addTree(event.Name); err != nil {
						return err
					}
				}
			}
			if !event.Has(fsnotify.Remove) && !event.Has(fsnotify.Rename) {
				continue
			}
			for _, root := range watcher.roots {
				if candidate, valid := pathguard.CandidateFromEvent(root, event.Name); valid {
					info, err := os.Lstat(candidate)
					if err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
						watcher.handler(candidate)
					}
					break
				}
			}
		}
	}
}

func (watcher *Recursive) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := watcher.watcher.Add(path); err != nil {
				return fmt.Errorf("watch directory %q: %w", path, err)
			}
		}
		return nil
	})
}
