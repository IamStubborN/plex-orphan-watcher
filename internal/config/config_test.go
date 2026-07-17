package config

import (
	"reflect"
	"testing"
	"time"
)

func TestLoadAppliesSafeDefaults(t *testing.T) {
	values := map[string]string{
		"DELETE_ROOTS":    "/data/internal/media, /data/usb_drive/media",
		"AUDIT_ROOTS":     "/data/internal/torrents, /data/usb_drive/torrents",
		"PLEX_URL":        "http://plex:32400",
		"PLEX_TOKEN_FILE": "/run/secrets/plex_token",
	}

	settings, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(settings.DeleteRoots, []string{"/data/internal/media", "/data/usb_drive/media"}) {
		t.Fatalf("DeleteRoots = %v", settings.DeleteRoots)
	}
	if !reflect.DeepEqual(settings.AuditRoots, []string{"/data/internal/torrents", "/data/usb_drive/torrents"}) {
		t.Fatalf("AuditRoots = %v", settings.AuditRoots)
	}
	if !settings.DryRun {
		t.Fatal("DRY_RUN must default to true")
	}
	if settings.StatePath != "/state/watcher.db" {
		t.Fatalf("StatePath = %q", settings.StatePath)
	}
	if settings.SettleDelay != 15*time.Minute || settings.ReconcileInterval != time.Hour {
		t.Fatalf("unexpected duration defaults: %+v", settings)
	}
}

func TestLoadRejectsMissingRequiredValues(t *testing.T) {
	if _, err := Load(func(string) string { return "" }); err == nil {
		t.Fatal("expected missing configuration error")
	}
}

func TestLoadRejectsInvalidBoolean(t *testing.T) {
	values := map[string]string{
		"DELETE_ROOTS":    "/data/media",
		"AUDIT_ROOTS":     "/data/torrents",
		"PLEX_URL":        "http://plex:32400",
		"PLEX_TOKEN_FILE": "/run/secrets/plex_token",
		"DRY_RUN":         "sometimes",
	}
	if _, err := Load(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected invalid DRY_RUN error")
	}
}

func TestLoadRejectsOverlappingPolicies(t *testing.T) {
	values := map[string]string{
		"DELETE_ROOTS":    "/data",
		"AUDIT_ROOTS":     "/data",
		"PLEX_URL":        "http://plex:32400",
		"PLEX_TOKEN_FILE": "/run/secrets/plex_token",
	}
	if _, err := Load(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected overlapping root policy error")
	}
}
