package plex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

func TestCountMediaUnderPaginatesTVSectionsAndUsesDirectoryBoundary(t *testing.T) {
	var starts []int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Plex-Token") != "secret" {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/library/sections":
			writeJSON(t, response, map[string]any{"MediaContainer": map[string]any{"Directory": []any{
				map[string]any{"key": "1", "type": "movie"},
				map[string]any{"key": "2", "type": "show"},
			}}})
		case "/library/sections/2/all":
			start, _ := strconv.Atoi(request.URL.Query().Get("X-Plex-Container-Start"))
			starts = append(starts, start)
			pages := map[int][]any{
				0: {
					metadataWithFiles("/data/tv/Showcase/Season 01/Showcase - S01E01.mkv"),
					metadataWithFiles("/data/tv/Other/Season 01/Other - S01E01.mkv"),
				},
				2: {metadataWithFiles("/data/tv/Show/Season 01/Show - S01E01.mkv")},
			}
			writeJSON(t, response, map[string]any{"MediaContainer": map[string]any{
				"totalSize": 3,
				"size":      len(pages[start]),
				"Metadata":  pages[start],
			}})
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)

	client := New(server.URL, "secret")
	client.pageSize = 2
	count, err := client.CountMediaUnder(context.Background(), "/data/tv/Show")

	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if !reflect.DeepEqual(starts, []int{0, 2}) {
		t.Fatalf("page starts = %v, want [0 2]", starts)
	}
}

func TestCountMediaUnderFailsClosedOnPlexError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "offline", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	if _, err := New(server.URL, "secret").CountMediaUnder(context.Background(), "/data/tv/Show"); err == nil {
		t.Fatal("expected Plex API error")
	}
}

func metadataWithFiles(files ...string) map[string]any {
	parts := make([]any, 0, len(files))
	for _, file := range files {
		parts = append(parts, map[string]any{"file": file})
	}
	return map[string]any{"Media": []any{map[string]any{"Part": parts}}}
}

func writeJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(value); err != nil {
		t.Fatal(err)
	}
}
