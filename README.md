# Creatorr

Sonarr for creator VOD: manage creators as TV-style series. Merge multiple source URLs into one Creatorr series, index first, download using yt-dlp, then pack episodes with metadata ready for Emby, Jellyfin, and similar media servers.

![Creatorr Overview](screenshot.png)

## Features

- **Multiple source URLs** - merge channels, playlists, and single-video URLs into one Creatorr series
- **Index first** - scan sources into a video catalog before any download
- **Metadata fetching & management** - fetch and edit series/video metadata; pack NFO and sidecars for Emby, Jellyfin, and similar
- **Quality profiles** - format selectors and optional maturity media/sidecar refresh
- **Domains & queues** - per-host rate limits, credentials (Access cookies), and soft pause
- **Web Archive fallback** - when an indexed YouTube video is deleted or unavailable, queue a [Web Archive](https://archive.org/) download
- **Import existing downloads** - bring in files already on disk with automated matching
- **Audio-only series** - per-series bestaudio remux to MKA as TV-style episodes
- **Video retention** - delete media after a configured number of days
- **SponsorBlock** - chapters, cut-out, and cut-out with an inserted info card
- **FlareSolverr & PO tokens** - Compose sidecars out of the box for challenge pre-solve and proof-of-origin minting
- **Automatic yt-dlp updates** - scheduled GitHub release checks and managed binary updates
- **Notifications** - in-app alerts plus Apprise channels for digests and warnings
- **Web UI & API** - library overview and stats in the browser; public OpenAPI REST for automation

Product behavior: [`docs/`](docs/README.md). REST contract: [`api/openapi.yaml`](api/openapi.yaml). Agent/contributor contract: [`AGENTS.md`](AGENTS.md).

## Quick start (production)

[`docker-compose.yml`](docker-compose.yml) pulls the published image:

```bash
docker compose up -d
```

| | |
| --- | --- |
| Image | `ghcr.io/xyxxyxxy/creatorr:latest` (version tag `v*` on `main`); `:sha-<short>` on every `main` push for pins |
| UI | `http://127.0.0.1:8787/` (first visit: **Setup** account, then login) |
| Health | `GET /api/health` (`ok` \| `degraded` \| `down`; no auth) |
| OpenAPI | `GET /api/openapi.json` (requires API key or session after setup) |

**Volumes** (host `./var` mirrors the container layout):

| Host | Container |
| --- | --- |
| `./var/data` | `/data` (SQLite `creatorr.db` + cache) |
| `./var/import` | `/import` |
| `./var/library` | `/library` (initial root folder seed) |

Compose comments show optional mounts: extra library roots, `/yt-dlp-plugins`.

## Configuration

Most options live in the UI under Settings.

Common env vars (see [`.env.example`](.env.example)):

| Variable | Default | Purpose |
| --- | --- | --- |
| `PUID` / `PGID` | `1000` | Host user for volume ownership |
| `CREATORR_PORT` | `8787` | HTTP port |
| `CREATORR_POT_PROVIDER_URL` | Compose sidecar | PO token provider (empty disables) |
| `CREATORR_FLARESOLVERR_URL` | Compose sidecar | FlareSolverr (empty disables) |
| `TZ` | `UTC` in Compose | Timezone for UI schedule labels |

Full reference: [`docs/settings.md`](docs/settings.md).

### Reset password

Stop Creatorr, clear the password hash, start again, then complete Setup:

```bash
sqlite3 ./var/data/creatorr.db <<'SQL'
UPDATE settings SET value = '' WHERE key = 'auth_password_hash';
SQL
```

Or use [DB Browser for SQLite](https://sqlitebrowser.org/) and clear `auth_password_hash` in the `settings` table.

## Development

Build from source with UI live reload:

```bash
./scripts/compose up -d --build
```

Or:

```bash
docker compose -f docker-compose.dev.yml up -d --build
```

[`docker-compose.dev.yml`](docker-compose.dev.yml) includes production compose, then adds `build: .`, `CREATORR_WEB_DEV`, and mounts `./internal/web`. Copy [`docker-compose.override.dev.example.yml`](docker-compose.override.dev.example.yml) to `docker-compose.override.dev.yml` (gitignored) for sibling plugin mounts. There is no auto-merged `docker-compose.override.yml`.

Local image without registry: `make image` (tags `creatorr:local`).

### Local Go

Paths use `./var/...` when `/data` is not a directory.

```bash
go run ./cmd/creatorr
# or: make build && ./bin/creatorr
```

```bash
make hooks      # once per clone: pre-commit runs make lint + make test
make test       # host Go, or Docker golang image if go missing
make vet
make lint       # golangci-lint in Docker (version pinned in Makefile; needs Docker)
make generate   # after editing api/openapi.yaml
make openapi-check
make css        # after Tailwind/daisyUI class or ECharts vendor changes
make sbom       # CycloneDX SBOM (needs syft); CI also license-gates it
```

Skip hooks for one commit: `SKIP_GITHOOKS=1 git commit ...` or `git commit --no-verify`.

CI: [`.github/workflows/ci.yml`](.github/workflows/ci.yml) on `main` and pull requests. Images: [`.github/workflows/docker.yml`](.github/workflows/docker.yml) (`:sha-<short>` on every `main` push; `:latest`, `:X.Y.Z`, and `:X.Y` when you push a `v*` tag).

## Concepts

Terminology: [`docs/domain-model.md`](docs/domain-model.md). Agent naming pointers: [`AGENTS.md`](AGENTS.md).
