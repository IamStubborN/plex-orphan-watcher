package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/IamStubborN/plex-orphan-watcher/internal/cleanup"
	"github.com/IamStubborN/plex-orphan-watcher/internal/config"
	"github.com/IamStubborN/plex-orphan-watcher/internal/health"
	"github.com/IamStubborN/plex-orphan-watcher/internal/plex"
	"github.com/IamStubborN/plex-orphan-watcher/internal/qbittorrent"
	"github.com/IamStubborN/plex-orphan-watcher/internal/scheduler"
	watcher "github.com/IamStubborN/plex-orphan-watcher/internal/watch"
	"golang.org/x/sync/errgroup"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("watcher stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	repository, err := plex.Open(settings.PlexDatabase)
	if err != nil {
		return err
	}
	defer repository.Close()

	torrents := qbittorrent.New(
		settings.QBittorrentURL,
		settings.QBittorrentUser,
		settings.QBittorrentPassword,
	)
	evaluator := cleanup.New(settings.WatchRoots, repository, torrents, settings.DryRun)
	queue := scheduler.New(
		settings.DeleteDelay,
		settings.RetryInterval,
		settings.MaxRetryAge,
		evaluator,
		logger,
	)
	filesystemWatcher, err := watcher.NewRecursive(settings.WatchRoots, queue.Enqueue)
	if err != nil {
		return err
	}
	defer filesystemWatcher.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return queue.Run(groupCtx) })
	group.Go(func() error { return filesystemWatcher.Run(groupCtx) })
	group.Go(func() error { return health.Run(groupCtx, settings.HealthAddress) })

	logger.Info(
		"watcher started",
		"roots", settings.WatchRoots,
		"dry_run", settings.DryRun,
		"delete_delay", settings.DeleteDelay,
		"retry_interval", settings.RetryInterval,
		"max_retry_age", settings.MaxRetryAge,
	)
	return group.Wait()
}
