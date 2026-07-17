package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DeleteRoots       []string
	AuditRoots        []string
	PlexURL           string
	PlexTokenFile     string
	StatePath         string
	DryRun            bool
	SettleDelay       time.Duration
	ReconcileInterval time.Duration
	HealthAddress     string
}

func Load(getenv func(string) string) (Config, error) {
	settings := Config{
		DeleteRoots:   parseRoots(getenv("DELETE_ROOTS")),
		AuditRoots:    parseRoots(getenv("AUDIT_ROOTS")),
		PlexURL:       strings.TrimRight(strings.TrimSpace(getenv("PLEX_URL")), "/"),
		PlexTokenFile: strings.TrimSpace(getenv("PLEX_TOKEN_FILE")),
		StatePath:     valueOrDefault(getenv("STATE_PATH"), "/state/watcher.db"),
		HealthAddress: valueOrDefault(getenv("HEALTH_ADDRESS"), ":8080"),
	}
	if len(settings.DeleteRoots) == 0 {
		return Config{}, fmt.Errorf("DELETE_ROOTS is required")
	}
	if len(settings.AuditRoots) == 0 {
		return Config{}, fmt.Errorf("AUDIT_ROOTS is required")
	}
	if settings.PlexURL == "" {
		return Config{}, fmt.Errorf("PLEX_URL is required")
	}
	if settings.PlexTokenFile == "" {
		return Config{}, fmt.Errorf("PLEX_TOKEN_FILE is required")
	}
	if err := validatePolicies(settings.DeleteRoots, settings.AuditRoots); err != nil {
		return Config{}, err
	}

	var err error
	if settings.DryRun, err = parseBool(getenv("DRY_RUN"), true); err != nil {
		return Config{}, err
	}
	if settings.SettleDelay, err = parseDuration(getenv("SETTLE_DELAY"), 15*time.Minute, "SETTLE_DELAY"); err != nil {
		return Config{}, err
	}
	if settings.ReconcileInterval, err = parseDuration(getenv("RECONCILE_INTERVAL"), time.Hour, "RECONCILE_INTERVAL"); err != nil {
		return Config{}, err
	}
	return settings, nil
}

func parseRoots(value string) []string {
	var roots []string
	for _, root := range strings.Split(value, ",") {
		if root = strings.TrimSpace(root); root != "" {
			roots = append(roots, filepath.Clean(root))
		}
	}
	return roots
}

func validatePolicies(deleteRoots, auditRoots []string) error {
	for _, deleteRoot := range deleteRoots {
		for _, auditRoot := range auditRoots {
			if deleteRoot == auditRoot {
				return fmt.Errorf("root %q has both delete and audit policies", deleteRoot)
			}
		}
	}
	return nil
}

func parseBool(value string, defaultValue bool) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse DRY_RUN: %w", err)
	}
	return parsed, nil
}

func parseDuration(value string, defaultValue time.Duration, name string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return defaultValue, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return parsed, nil
}

func valueOrDefault(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return strings.TrimSpace(value)
}
