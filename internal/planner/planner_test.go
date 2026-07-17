package planner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
)

func TestBuildDeletesOnlyExactEpisodeSidecarsWhenOtherVideoRemains(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Show")
	season := filepath.Join(show, "Season 01")
	write(t, filepath.Join(season, "Show - S01E01.ru.ass"))
	write(t, filepath.Join(season, "Show - S01E01.en.srt"))
	write(t, filepath.Join(season, "Show - S01E01.ru.mka"))
	write(t, filepath.Join(season, "Show - S01E10.srt"))
	write(t, filepath.Join(season, "poster.jpg"))
	remaining := filepath.Join(season, "Show - S01E02.mkv")
	write(t, remaining)
	pending := episodePending(filepath.Join(season, "Show - S01E01.mkv"))
	snapshot := model.Snapshot{
		Items:     map[string]model.Item{"102": {RatingKey: "102", Type: model.ItemEpisode, Parts: []string{remaining}}},
		Locations: []model.LibraryLocation{{SectionID: "2", SectionType: model.ItemEpisode, Path: root}},
	}

	plan, err := New([]string{root}, nil).Build(pending, snapshot, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	got := actionPaths(plan)
	want := []string{
		filepath.Join(season, "Show - S01E01.en.srt"),
		filepath.Join(season, "Show - S01E01.ru.ass"),
		filepath.Join(season, "Show - S01E01.ru.mka"),
	}
	if !equalStrings(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
}

func TestBuildCleansRemovedMoviePartWhileRatingKeyRemains(t *testing.T) {
	root := t.TempDir()
	movie := filepath.Join(root, "Movie (2026)")
	removed := filepath.Join(movie, "Movie (2026) - part1.mkv")
	remaining := filepath.Join(movie, "Movie (2026) - part2.mkv")
	sidecar := filepath.Join(movie, "Movie (2026) - part1.ru.srt")
	write(t, remaining)
	write(t, sidecar)
	pending := model.Pending{ID: "10:part1", Item: model.Item{
		RatingKey: "10", Type: model.ItemMovie, Title: "Movie", Parts: []string{removed},
	}}
	snapshot := model.Snapshot{
		Items: map[string]model.Item{"10": {
			RatingKey: "10", Type: model.ItemMovie, Title: "Movie", Parts: []string{remaining},
		}},
		Locations: []model.LibraryLocation{{SectionID: "1", SectionType: model.ItemMovie, Path: root}},
	}
	plan, err := New([]string{root}, nil).Build(pending, snapshot, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got := actionPaths(plan); !equalStrings(got, []string{sidecar}) {
		t.Fatalf("actions = %v, want removed part sidecar", got)
	}
}

func TestBuildCollapsesEmptySeasonAndShowIntoSingleTreeAction(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Show")
	season := filepath.Join(show, "Season 01")
	write(t, filepath.Join(season, "Show - S01E01.ru.ass"))
	write(t, filepath.Join(season, "poster.jpg"))
	write(t, filepath.Join(show, "tvshow.nfo"))

	plan, err := New([]string{root}, nil).Build(episodePending(filepath.Join(season, "Show - S01E01.mkv")), model.Snapshot{
		Items:     map[string]model.Item{},
		Locations: []model.LibraryLocation{{SectionID: "2", SectionType: model.ItemEpisode, Path: root}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Type != model.ActionDeleteTree || plan.Actions[0].Path != show {
		t.Fatalf("actions = %+v", plan.Actions)
	}
	if len(plan.Actions[0].Entries) != 5 {
		t.Fatalf("tree entries = %+v", plan.Actions[0].Entries)
	}
}

func TestBuildMovieFolderTreeAction(t *testing.T) {
	root := t.TempDir()
	movie := filepath.Join(root, "Movie (2026)")
	write(t, filepath.Join(movie, "Movie (2026).en.srt"))
	write(t, filepath.Join(movie, "poster.jpg"))
	pending := model.Pending{ID: "10", Item: model.Item{RatingKey: "10", Type: model.ItemMovie, Title: "Movie", Parts: []string{filepath.Join(movie, "Movie (2026).mkv")}}}
	plan, err := New([]string{root}, nil).Build(pending, model.Snapshot{
		Items:     map[string]model.Item{},
		Locations: []model.LibraryLocation{{SectionID: "1", SectionType: model.ItemMovie, Path: root}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Path != movie || plan.Actions[0].Type != model.ActionDeleteTree {
		t.Fatalf("actions = %+v", plan.Actions)
	}
}

func TestBuildCleansLooseEpisodeSidecarsWithoutDeletingLibraryRoot(t *testing.T) {
	root := t.TempDir()
	part := filepath.Join(root, "Show - S01E01.mkv")
	sidecar := filepath.Join(root, "Show - S01E01.ru.srt")
	write(t, sidecar)
	write(t, filepath.Join(root, "poster.jpg"))
	plan, err := New([]string{root}, nil).Build(episodePending(part), model.Snapshot{
		Items:     map[string]model.Item{},
		Locations: []model.LibraryLocation{{SectionID: "2", SectionType: model.ItemEpisode, Path: root}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got := actionPaths(plan); !equalStrings(got, []string{sidecar}) {
		t.Fatalf("actions = %v, want only exact sidecar", got)
	}
}

func TestBuildDoesNotCollapseMovieCollectionParent(t *testing.T) {
	root := t.TempDir()
	collection := filepath.Join(root, "Collection")
	movie := filepath.Join(collection, "Movie (2026)")
	write(t, filepath.Join(movie, "poster.jpg"))
	write(t, filepath.Join(collection, "collection.nfo"))
	pending := model.Pending{ID: "10", Item: model.Item{RatingKey: "10", Type: model.ItemMovie, Title: "Movie", Parts: []string{filepath.Join(movie, "Movie (2026).mkv")}}}
	plan, err := New([]string{root}, nil).Build(pending, model.Snapshot{
		Items:     map[string]model.Item{},
		Locations: []model.LibraryLocation{{SectionID: "1", SectionType: model.ItemMovie, Path: root}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Path != movie {
		t.Fatalf("actions = %+v, want only movie folder", plan.Actions)
	}
}

func TestBuildMarksTorrentRootAuditOnly(t *testing.T) {
	root := t.TempDir()
	season := filepath.Join(root, "Show", "Season 01")
	write(t, filepath.Join(season, "Show - S01E01.ru.ass"))
	plan, err := New(nil, []string{root}).Build(episodePending(filepath.Join(season, "Show - S01E01.mkv")), model.Snapshot{
		Items:     map[string]model.Item{},
		Locations: []model.LibraryLocation{{SectionID: "2", SectionType: model.ItemEpisode, Path: root}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].RootPolicy != model.PolicyAudit {
		t.Fatalf("actions = %+v", plan.Actions)
	}
}

func TestBuildCancelsWhenPlexStillReferencesPart(t *testing.T) {
	root := t.TempDir()
	part := filepath.Join(root, "Show", "Season 01", "Show - S01E01.mkv")
	plan, err := New([]string{root}, nil).Build(episodePending(part), model.Snapshot{
		Items:     map[string]model.Item{"101": {RatingKey: "101", Type: model.ItemEpisode, Parts: []string{part}}},
		Locations: []model.LibraryLocation{{SectionID: "2", SectionType: model.ItemEpisode, Path: root}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 || plan.Reason != "media_part_present_in_plex" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestBuildRejectsSymlinkInsideTree(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Show")
	season := filepath.Join(show, "Season 01")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(season, "linked")); err != nil {
		t.Fatal(err)
	}
	_, err := New([]string{root}, nil).Build(episodePending(filepath.Join(season, "Show - S01E01.mkv")), model.Snapshot{
		Items:     map[string]model.Item{},
		Locations: []model.LibraryLocation{{SectionID: "2", SectionType: model.ItemEpisode, Path: root}},
	}, time.Now().UTC())
	if err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func episodePending(part string) model.Pending {
	return model.Pending{ID: "101", Trigger: "reconcile", Item: model.Item{
		RatingKey: "101", Type: model.ItemEpisode, Title: "Episode 1",
		ParentRatingKey: "51", ParentTitle: "Season 1", GrandparentRatingKey: "11", GrandparentTitle: "Show",
		Parts: []string{part},
	}}
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func actionPaths(plan model.DeletionPlan) []string {
	paths := make([]string, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		paths = append(paths, action.Path)
	}
	return paths
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
