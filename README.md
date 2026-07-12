# Plex Orphan Watcher

Plex Orphan Watcher removes leftover TV show directories after Plex deletes
the last indexed episode. It is designed for folders that still contain
external subtitles, NCOP/NCED videos, trailers, or other extras that Plex does
not delete with the main media.

The watcher is event-driven. It does not scan and delete pre-existing orphan
directories on startup.

## Safety model

A directory is removed only when every check succeeds:

1. A file removal or rename event occurred below a configured TV root.
2. The candidate is a real directory directly below that root.
3. The authenticated Plex API reports no indexed media path inside the candidate.
4. No primary video remains outside a known extras directory.
5. qBittorrent does not manage the candidate path.
6. `DRY_RUN` is explicitly set to `false`.

Dependency failures are fail-closed. If the Plex API or qBittorrent cannot be
queried, the watcher keeps the directory and retries for `MAX_RETRY_AGE`.

Deleting one episode or one season does not remove the show directory while
Plex still indexes any media below it.

## Configuration

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `WATCH_ROOTS` | yes | - | Comma-separated TV library roots using the same paths as Plex and qBittorrent. |
| `PLEX_URL` | yes | - | Plex Media Server base URL, for example `http://plex:32400`. |
| `PLEX_TOKEN_FILE` | yes | - | Path to a file containing a Plex authentication token. |
| `QBITTORRENT_URL` | yes | - | qBittorrent Web API base URL. |
| `QBITTORRENT_USER` | no | empty | Web API username. |
| `QBITTORRENT_PASSWORD` | no | empty | Web API password. |
| `DRY_RUN` | no | `true` | Log eligible candidates without deleting them. |
| `DELETE_DELAY` | no | `30s` | Quiet period after the last filesystem event. |
| `RETRY_INTERVAL` | no | `30s` | Delay between transient Plex/API retries. |
| `MAX_RETRY_AGE` | no | `10m` | Maximum retry window for a candidate. |
| `HEALTH_ADDRESS` | no | `:8080` | Health endpoint listen address. |

## Docker

Copy `compose.example.yaml`, provide the Plex token secret, and start in dry-run
mode:

```bash
docker compose up -d --build
docker compose logs -f plex-orphan-watcher
```

After verifying candidate logs, set `DRY_RUN=false` and recreate the service.

The container runs as UID/GID `1000`. The TV roots must be writable by that
identity. Do not publish the health port; Docker can check it internally.

## Local development

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/plex-orphan-watcher
```

GitHub Actions are intentionally not configured. Verification is local.

## Known limitations

- Events that occur while the watcher is stopped are not replayed.
- Existing orphan directories are not deleted automatically.
- Only TV roots are supported by the current safety rules.
- Removing a show directly from qBittorrent should be completed in
  qBittorrent; this watcher refuses to delete torrent-managed content.
