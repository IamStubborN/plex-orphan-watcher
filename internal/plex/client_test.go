package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
)

func TestSnapshotLoadsMoviesEpisodesLocationsAndPaginates(t *testing.T) {
	var starts []int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Plex-Token") != "secret" {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/library/sections":
			writeJSON(t, response, map[string]any{"MediaContainer": map[string]any{"Directory": []any{
				map[string]any{"key": "1", "type": "movie", "Location": []any{map[string]any{"path": "/data/media/movies"}}},
				map[string]any{"key": "2", "type": "show", "Location": []any{map[string]any{"path": "/data/media/tv"}}},
			}}})
		case "/library/sections/1/all":
			if request.URL.Query().Get("type") != "1" {
				t.Errorf("movie type = %q", request.URL.Query().Get("type"))
			}
			writeJSON(t, response, page(1, []any{metadata("10", "movie", "Movie", "", "", "", "", "/data/media/movies/Movie/Movie.mkv")}))
		case "/library/sections/2/all":
			if request.URL.Query().Get("type") != "4" {
				t.Errorf("episode type = %q", request.URL.Query().Get("type"))
			}
			start, _ := strconv.Atoi(request.URL.Query().Get("X-Plex-Container-Start"))
			starts = append(starts, start)
			pages := map[int][]any{
				0: {metadata("101", "episode", "Episode 1", "51", "Season 1", "11", "Show", "/data/media/tv/Show/Season 01/Show - S01E01.mkv")},
				1: {metadata("102", "episode", "Episode 2", "51", "Season 1", "11", "Show", "/data/media/tv/Show/Season 01/Show - S01E02.mkv")},
			}
			writeJSON(t, response, page(2, pages[start]))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)

	client := New(server.URL, "secret")
	client.pageSize = 200
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 3 || snapshot.Items["10"].Type != model.ItemMovie || snapshot.Items["101"].Type != model.ItemEpisode {
		t.Fatalf("items = %+v", snapshot.Items)
	}
	if got := snapshot.Items["101"].GrandparentTitle; got != "Show" {
		t.Fatalf("GrandparentTitle = %q", got)
	}
	if len(snapshot.Locations) != 2 {
		t.Fatalf("locations = %+v", snapshot.Locations)
	}
	if !reflect.DeepEqual(starts, []int{0, 1}) {
		t.Fatalf("starts = %v, want [0 1]", starts)
	}
}

func TestStreamEventsEmitsOnlyLibraryDeletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/:/eventsource/notifications" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintln(response, `data: {"NotificationContainer":{"type":"timeline","TimelineEntry":[{"state":0,"ratingKey":"100","identifier":"com.plexapp.plugins.library"},{"state":9,"ratingKey":"101","identifier":"com.plexapp.plugins.library"},{"state":9,"itemID":102,"identifier":"com.plexapp.plugins.library"},{"state":9,"itemID":"103","identifier":"com.plexapp.plugins.library"},{"state":9,"ratingKey":"999","identifier":"other"}]}}`)
	}))
	t.Cleanup(server.Close)

	var events []DeletionEvent
	err := New(server.URL, "secret").StreamEvents(context.Background(), func(event DeletionEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []DeletionEvent{{RatingKey: "101"}, {RatingKey: "102"}, {RatingKey: "103"}}) {
		t.Fatalf("events = %+v", events)
	}
}

func TestReadyUsesLightweightIdentityEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/identity" {
			http.NotFound(response, request)
			return
		}
		writeJSON(t, response, map[string]any{"MediaContainer": map[string]any{"machineIdentifier": "test"}})
	}))
	t.Cleanup(server.Close)
	if err := New(server.URL, "secret").Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotFailsClosedOnPlexError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "offline", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	if _, err := New(server.URL, "secret").Snapshot(context.Background()); err == nil {
		t.Fatal("expected Plex API error")
	}
}

func metadata(key, itemType, title, parentKey, parentTitle, grandparentKey, grandparentTitle string, files ...string) map[string]any {
	parts := make([]any, 0, len(files))
	for _, file := range files {
		parts = append(parts, map[string]any{"file": file})
	}
	return map[string]any{
		"ratingKey": key, "type": itemType, "title": title,
		"parentRatingKey": parentKey, "parentTitle": parentTitle,
		"grandparentRatingKey": grandparentKey, "grandparentTitle": grandparentTitle,
		"Media": []any{map[string]any{"Part": parts}},
	}
}

func page(total int, items []any) map[string]any {
	return map[string]any{"MediaContainer": map[string]any{"totalSize": total, "size": len(items), "Metadata": items}}
}

func writeJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(value); err != nil {
		t.Errorf("encode HTTP fixture: %v", err)
	}
}

func TestSnapshotSetsSyncTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writeJSON(t, response, map[string]any{"MediaContainer": map[string]any{}})
	}))
	t.Cleanup(server.Close)
	before := time.Now().UTC()
	snapshot, err := New(server.URL, "secret").Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SyncedAt.Before(before) {
		t.Fatalf("SyncedAt = %s", snapshot.SyncedAt)
	}
}
