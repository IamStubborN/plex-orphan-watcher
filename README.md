# Plex Orphan Watcher

Plex Orphan Watcher removes sidecar files and empty media directories left
behind after Plex removes movies or TV episodes. It supports subtitles,
external audio, artwork, and metadata without depending on qBittorrent,
Gluetun, filesystem notifications, or the Plex SQLite database.

## How it works

The service combines two signals:

1. Plex EventSource timeline deletion events provide low-latency hints.
2. Hourly Plex API inventory reconciliation recovers events missed while the
   service was stopped or disconnected.

The previous inventory and pending deletions are stored in BoltDB. A Plex
timeline event is never treated as sufficient authorization to delete data.
Every cleanup waits for the settle delay and uses a fresh Plex inventory before
planning and again immediately before execution.

## Cleanup behavior

For a removed movie or episode, the watcher matches sidecars using the exact
video basename. For example, removing `Show - S01E01.mkv` can select:

```text
Show - S01E01.ru.ass
Show - S01E01.en.srt
Show - S01E01.ru.mka
Show - S01E01.nfo
Show - S01E01.jpg
```

It does not select `S01E10`, `poster.jpg`, or sidecars belonging to another
video. When a season, show, or movie directory contains no video and Plex no
longer references anything below it, the whole remaining directory is selected.

Deletion is fail-closed. The watcher refuses to act when Plex is unavailable,
a path leaves an allowed library location, a symlink is present, a video
reappears, or the directory changes after planning. Recursive cleanup is an
enumerated and fingerprinted sequence of `remove` operations; it does not use
an unconditional recursive delete.

## Root policies

- `DELETE_ROOTS` can be modified when `DRY_RUN=false`.
- `AUDIT_ROOTS` are always report-only, even in live mode.
- Plex library locations outside both policies are ignored with an error.

Use media directories as delete roots and torrent directories as audit roots.
This prevents the watcher from interfering with seeded content.

## Configuration

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PLEX_URL` | yes | - | Plex base URL. |
| `PLEX_TOKEN_FILE` | yes | - | File containing the Plex token. |
| `DELETE_ROOTS` | yes | - | Comma-separated writable media root parents. |
| `AUDIT_ROOTS` | yes | - | Comma-separated permanently read-only root parents. |
| `STATE_PATH` | no | `/state/watcher.db` | Persistent BoltDB path. |
| `DRY_RUN` | no | `true` | Plan and persist actions without deleting anything. |
| `SETTLE_DELAY` | no | `15m` | Delay before a removed item becomes eligible. |
| `RECONCILE_INTERVAL` | no | `1h` | Full Plex inventory interval. |
| `HEALTH_ADDRESS` | no | `:8080` | Health endpoint address. |

`/healthz` reports process liveness. `/readyz` additionally verifies the root
mounts, Plex API, state database, and freshness of the last inventory sync.
EventSource connectivity is intentionally not a readiness requirement because
reconciliation remains an independent fallback.

## Dry-run rollout

Run the first deployment with `DRY_RUN=true` and all media mounts read-only.
After at least one week, inspect the persisted plans:

```bash
docker exec plex-orphan-watcher plex-orphan-watcher \
  report --state /state/watcher.db --format json
```

When the primary process owns the BoltDB lock, the command automatically reads
the same report through the container-internal `/report` endpoint. The health
port should not be published outside the Docker network.

After review, change only media mounts to read-write and set `DRY_RUN=false`.
Accumulated dry-run plans are revalidated against Plex and the filesystem before
they can be applied. Audit roots remain read-only.

## Local verification

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/plex-orphan-watcher
```

GitHub Actions are intentionally not required. The release image can be built
and pushed locally with Docker Buildx.
