package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/cleanup"
	"github.com/IamStubborN/plex-orphan-watcher/internal/config"
	"github.com/IamStubborN/plex-orphan-watcher/internal/health"
	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
	"github.com/IamStubborN/plex-orphan-watcher/internal/planner"
	"github.com/IamStubborN/plex-orphan-watcher/internal/plex"
	watcherruntime "github.com/IamStubborN/plex-orphan-watcher/internal/runtime"
	"github.com/IamStubborN/plex-orphan-watcher/internal/service"
	"github.com/IamStubborN/plex-orphan-watcher/internal/state"
	"golang.org/x/sync/errgroup"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger, os.Args[1:]); err != nil {
		logger.Error("watcher stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, args []string) error {
	if len(args) > 0 && args[0] == "report" {
		return report(args[1:])
	}
	settings, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	token, err := os.ReadFile(settings.PlexTokenFile)
	if err != nil {
		return fmt.Errorf("read Plex token file: %w", err)
	}
	if len(strings.TrimSpace(string(token))) == 0 {
		return fmt.Errorf("Plex token file is empty")
	}
	store, err := state.Open(settings.StatePath)
	if err != nil {
		return err
	}
	defer store.Close()

	plexClient := plex.New(settings.PlexURL, string(token))
	watcherService := service.New(
		plexClient,
		store,
		planner.New(settings.DeleteRoots, settings.AuditRoots),
		cleanup.NewExecutor(settings.DryRun),
		settings.SettleDelay,
		settings.DryRun,
		logger,
	)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return watcherruntime.Run(groupCtx, plexClient, watcherService, settings.ReconcileInterval, logger)
	})
	group.Go(func() error {
		return health.Run(groupCtx, settings.HealthAddress, readiness(settings, plexClient, store), reportHandler(store))
	})
	logger.Info("watcher started",
		"delete_roots", settings.DeleteRoots, "audit_roots", settings.AuditRoots,
		"dry_run", settings.DryRun, "settle_delay", settings.SettleDelay,
		"reconcile_interval", settings.ReconcileInterval, "state_path", settings.StatePath)
	return group.Wait()
}

func report(args []string) error {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	statePath := flags.String("state", "/state/watcher.db", "path to watcher state database")
	format := flags.String("format", "json", "output format (json)")
	reportURL := flags.String("url", "http://127.0.0.1:8080/report", "running watcher report endpoint")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *format != "json" {
		return fmt.Errorf("unsupported report format %q", *format)
	}
	store, err := state.OpenReadOnly(*statePath)
	if err != nil {
		return fetchReport(*reportURL, err)
	}
	defer store.Close()
	return encodeReport(os.Stdout, store)
}

func reportHandler(store *state.Store) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if err := encodeReport(writer, store); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	})
}

func encodeReport(writer io.Writer, store *state.Store) error {
	pending, err := store.Pending(true)
	if err != nil {
		return err
	}
	plans := pending[:0]
	for _, candidate := range pending {
		if candidate.Status == model.PendingDryRun {
			plans = append(plans, candidate)
		}
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plans)
}

func fetchReport(reportURL string, stateErr error) error {
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(reportURL)
	if err != nil {
		return fmt.Errorf("open state database: %v; query running watcher: %w", stateErr, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("open state database: %v; report endpoint returned %s: %s", stateErr, response.Status, strings.TrimSpace(string(body)))
	}
	_, err = io.Copy(os.Stdout, response.Body)
	return err
}

func readiness(settings config.Config, plexClient *plex.Client, store *state.Store) health.ReadinessCheck {
	return func(request *http.Request) error {
		for _, root := range append(append([]string{}, settings.DeleteRoots...), settings.AuditRoots...) {
			info, err := os.Stat(root)
			if err != nil {
				return fmt.Errorf("root %q: %w", root, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("root %q is not a directory", root)
			}
		}
		if err := plexClient.Ready(request.Context()); err != nil {
			return fmt.Errorf("Plex readiness: %w", err)
		}
		lastSync, err := store.LastSync()
		if err != nil {
			return fmt.Errorf("state readiness: %w", err)
		}
		maxAge := 2*settings.ReconcileInterval + time.Minute
		if time.Since(lastSync) > maxAge {
			return fmt.Errorf("Plex inventory is stale: last sync %s", lastSync)
		}
		info, err := os.Stat(filepath.Dir(settings.StatePath))
		if err != nil {
			return fmt.Errorf("state directory is unavailable: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("state directory is not a directory")
		}
		return nil
	}
}
