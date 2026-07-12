package config

import (
	"testing"
	"time"
)

func TestLoadAppliesSafeDefaults(t *testing.T) {
	values := map[string]string{
		"WATCH_ROOTS":          "/data/internal/torrents/tv, /data/usb_drive/torrents/tv",
		"PLEX_URL":             "http://plex:32400",
		"PLEX_TOKEN_FILE":      "/run/secrets/plex_token",
		"QBITTORRENT_URL":      "http://gluetun:8400",
		"QBITTORRENT_USER":     "user",
		"QBITTORRENT_PASSWORD": "secret",
	}

	config, err := Load(func(key string) string { return values[key] })

	if err != nil {
		t.Fatal(err)
	}
	if len(config.WatchRoots) != 2 {
		t.Fatalf("WatchRoots = %v, want two roots", config.WatchRoots)
	}
	if !config.DryRun {
		t.Fatal("DRY_RUN must default to true")
	}
	if config.DeleteDelay != 30*time.Second || config.RetryInterval != 30*time.Second || config.MaxRetryAge != 10*time.Minute {
		t.Fatalf("unexpected duration defaults: %+v", config)
	}
}

func TestLoadRejectsMissingRequiredValues(t *testing.T) {
	if _, err := Load(func(string) string { return "" }); err == nil {
		t.Fatal("expected missing configuration error")
	}
}

func TestLoadRejectsInvalidBoolean(t *testing.T) {
	values := map[string]string{
		"WATCH_ROOTS":     "/data/tv",
		"PLEX_URL":        "http://plex:32400",
		"PLEX_TOKEN_FILE": "/run/secrets/plex_token",
		"QBITTORRENT_URL": "http://qbittorrent:8400",
		"DRY_RUN":         "sometimes",
	}
	if _, err := Load(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected invalid DRY_RUN error")
	}
}
