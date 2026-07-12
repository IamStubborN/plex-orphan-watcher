package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	WatchRoots          []string
	PlexDatabase        string
	QBittorrentURL      string
	QBittorrentUser     string
	QBittorrentPassword string
	DryRun              bool
	DeleteDelay         time.Duration
	RetryInterval       time.Duration
	MaxRetryAge         time.Duration
	HealthAddress       string
}

func Load(getenv func(string) string) (Config, error) {
	config := Config{
		PlexDatabase:        strings.TrimSpace(getenv("PLEX_DATABASE")),
		QBittorrentURL:      strings.TrimSpace(getenv("QBITTORRENT_URL")),
		QBittorrentUser:     getenv("QBITTORRENT_USER"),
		QBittorrentPassword: getenv("QBITTORRENT_PASSWORD"),
		HealthAddress:       valueOrDefault(getenv("HEALTH_ADDRESS"), ":8080"),
	}
	for _, root := range strings.Split(getenv("WATCH_ROOTS"), ",") {
		if root = strings.TrimSpace(root); root != "" {
			config.WatchRoots = append(config.WatchRoots, root)
		}
	}
	if len(config.WatchRoots) == 0 {
		return Config{}, fmt.Errorf("WATCH_ROOTS is required")
	}
	if config.PlexDatabase == "" {
		return Config{}, fmt.Errorf("PLEX_DATABASE is required")
	}
	if config.QBittorrentURL == "" {
		return Config{}, fmt.Errorf("QBITTORRENT_URL is required")
	}

	var err error
	if config.DryRun, err = parseBool(getenv("DRY_RUN"), true); err != nil {
		return Config{}, err
	}
	if config.DeleteDelay, err = parseDuration(getenv("DELETE_DELAY"), 30*time.Second, "DELETE_DELAY"); err != nil {
		return Config{}, err
	}
	if config.RetryInterval, err = parseDuration(getenv("RETRY_INTERVAL"), 30*time.Second, "RETRY_INTERVAL"); err != nil {
		return Config{}, err
	}
	if config.MaxRetryAge, err = parseDuration(getenv("MAX_RETRY_AGE"), 10*time.Minute, "MAX_RETRY_AGE"); err != nil {
		return Config{}, err
	}
	return config, nil
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
