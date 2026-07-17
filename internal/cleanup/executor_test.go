package cleanup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
)

func TestExecutorDryRunDoesNotMutate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Episode.ru.srt")
	writeFile(t, file)
	executor := NewExecutor(true)
	if _, err := executor.Execute(model.DeletionPlan{Actions: []model.Action{{Type: model.ActionDeleteFile, Path: file, RootPolicy: model.PolicyDelete}}}); err != nil {
		t.Fatal(err)
	}
	assertExists(t, file)
}

func TestExecutorNeverMutatesAuditAction(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Episode.ru.srt")
	writeFile(t, file)
	executor := NewExecutor(false)
	if _, err := executor.Execute(model.DeletionPlan{Actions: []model.Action{{Type: model.ActionDeleteFile, Path: file, RootPolicy: model.PolicyAudit}}}); err != nil {
		t.Fatal(err)
	}
	assertExists(t, file)
}

func TestExecutorDeletesPlannedFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Episode.ru.srt")
	writeFile(t, file)
	entry, err := Fingerprint(file)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewExecutor(false).Execute(model.DeletionPlan{Actions: []model.Action{{Type: model.ActionDeleteFile, Path: file, RootPolicy: model.PolicyDelete, Entries: []model.Entry{entry}}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 {
		t.Fatalf("Deleted = %d", result.Deleted)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}

func TestExecutorRejectsChangedTreeAndPreservesEverything(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Show")
	old := filepath.Join(dir, "Episode.ru.srt")
	writeFile(t, old)
	entries, _, err := FingerprintTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "new-file.nfo"))
	_, err = NewExecutor(false).Execute(model.DeletionPlan{Actions: []model.Action{{Type: model.ActionDeleteTree, Path: dir, RootPolicy: model.PolicyDelete, Entries: entries}}})
	if err == nil {
		t.Fatal("expected changed tree rejection")
	}
	assertExists(t, old)
}

func TestExecutorRejectsNewVideoBeforeTreeDeletion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Show")
	writeFile(t, filepath.Join(dir, "poster.jpg"))
	entries, _, err := FingerprintTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "Episode.mkv"))
	_, err = NewExecutor(false).Execute(model.DeletionPlan{Actions: []model.Action{{Type: model.ActionDeleteTree, Path: dir, RootPolicy: model.PolicyDelete, Entries: entries}}})
	if err == nil {
		t.Fatal("expected new video rejection")
	}
	assertExists(t, dir)
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%q should exist: %v", path, err)
	}
}
