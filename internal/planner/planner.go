package planner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/cleanup"
	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
)

var sidecarExtensions = map[string]struct{}{
	".aac": {}, ".ac3": {}, ".ass": {}, ".dts": {}, ".eac3": {}, ".flac": {},
	".idx": {}, ".jpeg": {}, ".jpg": {}, ".mka": {}, ".mp3": {}, ".nfo": {},
	".ogg": {}, ".opus": {}, ".png": {}, ".srt": {}, ".ssa": {}, ".sub": {},
	".sup": {}, ".tbn": {}, ".vtt": {}, ".wav": {}, ".webp": {}, ".xml": {},
}

type Planner struct {
	deleteRoots []string
	auditRoots  []string
}

func New(deleteRoots, auditRoots []string) *Planner {
	return &Planner{deleteRoots: cleanPaths(deleteRoots), auditRoots: cleanPaths(auditRoots)}
}

func (planner *Planner) Build(pending model.Pending, snapshot model.Snapshot, now time.Time) (model.DeletionPlan, error) {
	plan := model.DeletionPlan{
		PendingID: pending.ID, RatingKey: pending.Item.RatingKey, ItemType: pending.Item.Type,
		Title: pending.Item.Title, CreatedAt: now,
	}
	if _, exists := snapshot.Items[pending.Item.RatingKey]; exists {
		plan.Reason = "item_present_in_plex"
		return plan, nil
	}

	actions := make(map[string]model.Action)
	for _, part := range pending.Item.Parts {
		part = filepath.Clean(part)
		if referenced(snapshot, part) {
			plan.Reason = "media_part_present_in_plex"
			return plan, nil
		}
		if info, err := os.Lstat(part); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return model.DeletionPlan{}, fmt.Errorf("media part %q is a symlink", part)
			}
			plan.Reason = "video_present_on_disk"
			return plan, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return model.DeletionPlan{}, fmt.Errorf("inspect media part %q: %w", part, err)
		}

		location, policy, err := planner.locationFor(part, snapshot.Locations)
		if err != nil {
			return model.DeletionPlan{}, err
		}
		if plan.OldMediaPath == "" {
			plan.OldMediaPath = part
		}
		if err := ensureNoSymlinkBetween(location, filepath.Dir(part)); err != nil {
			return model.DeletionPlan{}, err
		}
		boundary, ok := cleanupBoundary(part, location, pending.Item.Type)
		if ok {
			tree, empty, err := highestEmptyTree(filepath.Dir(part), boundary, snapshot)
			if err != nil {
				return model.DeletionPlan{}, err
			}
			if empty {
				entries, bytes, err := cleanup.FingerprintTree(tree)
				if err != nil {
					return model.DeletionPlan{}, err
				}
				actions[tree] = model.Action{Type: model.ActionDeleteTree, Path: tree, Bytes: bytes, RootPolicy: policy, Entries: entries}
				continue
			}
		}
		files, err := matchingSidecars(part)
		if err != nil {
			return model.DeletionPlan{}, err
		}
		for _, file := range files {
			entry, err := cleanup.Fingerprint(file)
			if err != nil {
				return model.DeletionPlan{}, err
			}
			actions[file] = model.Action{Type: model.ActionDeleteFile, Path: file, Bytes: entry.Size, RootPolicy: policy, Entries: []model.Entry{entry}}
		}
	}

	for path, action := range actions {
		for treePath, treeAction := range actions {
			if treeAction.Type == model.ActionDeleteTree && path != treePath && contained(treePath, path) {
				delete(actions, path)
				break
			}
		}
		if action.Type == model.ActionDeleteTree {
			for otherPath := range actions {
				if otherPath != path && contained(path, otherPath) {
					delete(actions, otherPath)
				}
			}
		}
	}
	for _, action := range actions {
		plan.Actions = append(plan.Actions, action)
	}
	sort.Slice(plan.Actions, func(i, j int) bool { return plan.Actions[i].Path < plan.Actions[j].Path })
	if len(plan.Actions) == 0 {
		plan.Reason = "no_orphans"
	} else {
		plan.Reason = "cleanup_candidate"
	}
	return plan, nil
}

func highestEmptyTree(start, boundary string, snapshot model.Snapshot) (string, bool, error) {
	start = filepath.Clean(start)
	boundary = filepath.Clean(boundary)
	if !contained(boundary, start) {
		return "", false, fmt.Errorf("cleanup start %q is outside boundary %q", start, boundary)
	}
	var highest string
	for candidate := start; contained(boundary, candidate); candidate = filepath.Dir(candidate) {
		info, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", false, fmt.Errorf("candidate %q is not a real directory", candidate)
		}
		entries, _, err := cleanup.FingerprintTree(candidate)
		if err != nil {
			return "", false, err
		}
		if cleanup.HasVideoEntries(entries) || referencesUnder(snapshot, candidate) {
			break
		}
		highest = candidate
		if candidate == boundary {
			break
		}
	}
	return highest, highest != "", nil
}

func cleanupBoundary(part, libraryRoot string, itemType model.ItemType) (string, bool) {
	part = filepath.Clean(part)
	libraryRoot = filepath.Clean(libraryRoot)
	directory := filepath.Dir(part)
	if directory == libraryRoot || !contained(libraryRoot, directory) {
		return "", false
	}
	if itemType == model.ItemMovie {
		return directory, true
	}
	relative, err := filepath.Rel(libraryRoot, directory)
	if err != nil || relative == "." {
		return "", false
	}
	first := strings.Split(relative, string(filepath.Separator))[0]
	return filepath.Join(libraryRoot, first), true
}

func matchingSidecars(part string) ([]string, error) {
	directory := filepath.Dir(part)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stem := strings.TrimSuffix(filepath.Base(part), filepath.Ext(part))
	prefix := stem + "."
	var result []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		if _, ok := sidecarExtensions[strings.ToLower(filepath.Ext(entry.Name()))]; !ok {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("sidecar %q is a symlink", path)
		}
		if info.Mode().IsRegular() {
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result, nil
}

func (planner *Planner) locationFor(part string, locations []model.LibraryLocation) (string, model.RootPolicy, error) {
	var best string
	for _, location := range locations {
		if contained(location.Path, part) && len(location.Path) > len(best) {
			best = filepath.Clean(location.Path)
		}
	}
	if best == "" {
		return "", "", fmt.Errorf("media part %q is outside Plex library locations", part)
	}
	policy, ok := policyFor(best, planner.deleteRoots, planner.auditRoots)
	if !ok {
		return "", "", fmt.Errorf("Plex library location %q has no configured root policy", best)
	}
	return best, policy, nil
}

func policyFor(path string, deleteRoots, auditRoots []string) (model.RootPolicy, bool) {
	var selected model.RootPolicy
	best := 0
	for _, root := range deleteRoots {
		if contained(root, path) && len(root) > best {
			selected, best = model.PolicyDelete, len(root)
		}
	}
	for _, root := range auditRoots {
		if contained(root, path) && len(root) >= best {
			selected, best = model.PolicyAudit, len(root)
		}
	}
	return selected, best > 0
}

func referenced(snapshot model.Snapshot, path string) bool {
	path = filepath.Clean(path)
	for _, item := range snapshot.Items {
		for _, part := range item.Parts {
			if filepath.Clean(part) == path {
				return true
			}
		}
	}
	return false
}

func referencesUnder(snapshot model.Snapshot, directory string) bool {
	for _, item := range snapshot.Items {
		for _, part := range item.Parts {
			if contained(directory, part) {
				return true
			}
		}
	}
	return false
}

func ensureNoSymlinkBetween(root, target string) error {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if !contained(root, target) {
		return fmt.Errorf("target %q is outside root %q", target, root)
	}
	for current := target; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink directory %q is not allowed", current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if current == root {
			return nil
		}
	}
}

func contained(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func cleanPaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" {
			result = append(result, filepath.Clean(path))
		}
	}
	return result
}
