package qbittorrent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagedMatchesTorrentContentPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v2/torrents/info" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`[
            {"name":"Show","save_path":"/data/tv","content_path":"/data/tv/Show"},
            {"name":"Other","save_path":"/data/tv","content_path":"/data/tv/Other"}
        ]`))
	}))
	t.Cleanup(server.Close)

	client := New(server.URL, "", "")
	managed, err := client.Managed(context.Background(), "/data/tv/Show")

	if err != nil {
		t.Fatal(err)
	}
	if !managed {
		t.Fatal("expected matching torrent to protect candidate")
	}
}

func TestManagedRejectsUnrelatedTorrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`[{"name":"Other","save_path":"/data/tv","content_path":"/data/tv/Other"}]`))
	}))
	t.Cleanup(server.Close)

	client := New(server.URL, "", "")
	managed, err := client.Managed(context.Background(), "/data/tv/Show")

	if err != nil {
		t.Fatal(err)
	}
	if managed {
		t.Fatal("unrelated torrent must not protect candidate")
	}
}

func TestManagedFailsClosedWhenAPIIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	serverURL := server.URL
	server.Close()

	client := New(serverURL, "", "")
	if _, err := client.Managed(context.Background(), "/data/tv/Show"); err == nil {
		t.Fatal("expected API failure to be returned")
	}
}
