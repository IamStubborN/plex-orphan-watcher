package cleanup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
)

var videoExtensions = map[string]struct{}{
	".avi": {}, ".m2ts": {}, ".m4v": {}, ".mkv": {}, ".mov": {}, ".mp4": {},
	".mpeg": {}, ".mpg": {}, ".ts": {}, ".webm": {}, ".wmv": {},
}

type Result struct {
	Deleted int
	Bytes   int64
	Skipped int
}

type Executor struct {
	dryRun bool
}

func NewExecutor(dryRun bool) *Executor { return &Executor{dryRun: dryRun} }

func Fingerprint(path string) (model.Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return model.Entry{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return model.Entry{}, fmt.Errorf("symlink %q is not allowed", path)
	}
	return model.Entry{
		Path: filepath.Clean(path), Size: info.Size(), ModTimeUnixNano: info.ModTime().UnixNano(),
		Mode: uint32(info.Mode()), IsDir: info.IsDir(),
	}, nil
}

func FingerprintTree(root string) ([]model.Entry, int64, error) {
	var entries []model.Entry
	var bytes int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		fingerprint, err := Fingerprint(path)
		if err != nil {
			return err
		}
		entries = append(entries, fingerprint)
		if !fingerprint.IsDir {
			bytes += fingerprint.Size
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, bytes, nil
}

func HasVideoEntries(entries []model.Entry) bool {
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		if _, ok := videoExtensions[strings.ToLower(filepath.Ext(entry.Path))]; ok {
			return true
		}
	}
	return false
}

func (executor *Executor) Execute(plan model.DeletionPlan) (Result, error) {
	var result Result
	for _, action := range plan.Actions {
		if executor.dryRun || action.RootPolicy == model.PolicyAudit {
			result.Skipped++
			continue
		}
		switch action.Type {
		case model.ActionDeleteFile:
			deleted, err := executeFile(action)
			if err != nil {
				return result, err
			}
			if deleted {
				result.Deleted++
				result.Bytes += action.Bytes
			}
		case model.ActionDeleteTree:
			deleted, err := executeTree(action)
			if err != nil {
				return result, err
			}
			if deleted {
				result.Deleted += countFiles(action.Entries)
				result.Bytes += action.Bytes
			}
		default:
			return result, fmt.Errorf("unknown action type %q", action.Type)
		}
	}
	return result, nil
}

func executeFile(action model.Action) (bool, error) {
	if len(action.Entries) != 1 {
		return false, fmt.Errorf("file action %q has %d fingerprints", action.Path, len(action.Entries))
	}
	current, err := Fingerprint(action.Path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(current, action.Entries[0]) {
		return false, fmt.Errorf("file %q changed after planning", action.Path)
	}
	if current.IsDir {
		return false, fmt.Errorf("file action %q points to a directory", action.Path)
	}
	if err := os.Remove(action.Path); err != nil {
		return false, fmt.Errorf("delete file %q: %w", action.Path, err)
	}
	return true, nil
}

func executeTree(action model.Action) (bool, error) {
	current, _, err := FingerprintTree(action.Path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if HasVideoEntries(current) {
		return false, fmt.Errorf("tree %q contains a video file", action.Path)
	}
	if !reflect.DeepEqual(current, action.Entries) {
		return false, fmt.Errorf("tree %q changed after planning", action.Path)
	}
	ordered := append([]model.Entry(nil), current...)
	sort.Slice(ordered, func(i, j int) bool {
		leftDepth := strings.Count(filepath.Clean(ordered[i].Path), string(filepath.Separator))
		rightDepth := strings.Count(filepath.Clean(ordered[j].Path), string(filepath.Separator))
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		if ordered[i].IsDir != ordered[j].IsDir {
			return !ordered[i].IsDir
		}
		return ordered[i].Path > ordered[j].Path
	})
	for _, entry := range ordered {
		currentEntry, err := Fingerprint(entry.Path)
		if err != nil {
			return false, fmt.Errorf("revalidate %q: %w", entry.Path, err)
		}
		if entry.IsDir {
			if !currentEntry.IsDir {
				return false, fmt.Errorf("directory %q changed type during deletion", entry.Path)
			}
		} else if !reflect.DeepEqual(currentEntry, entry) {
			return false, fmt.Errorf("entry %q changed during deletion", entry.Path)
		}
		if err := os.Remove(entry.Path); err != nil {
			return false, fmt.Errorf("delete %q: %w", entry.Path, err)
		}
	}
	return true, nil
}

func countFiles(entries []model.Entry) int {
	count := 0
	for _, entry := range entries {
		if !entry.IsDir {
			count++
		}
	}
	return count
}
