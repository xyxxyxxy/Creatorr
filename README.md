# Creatorr

\*arr shaped daemon and streaming proxy for creator content.

**License:** [The Unlicense](LICENSE) (SPDX `Unlicense`).

Repo: [github.com/xyxxyxxy/Creatorr](https://github.com/xyxxyxxy/Creatorr).  
Go module: `github.com/xyxxyxxy/Creatorr`

![Creatorr Overview](screenshot.png)

## Features

- **Index first, then download** - scan sources into a video index, then download wanted videos automatically on schedule (or on demand).
- **Import** - add existing files to a indexed series with automatic video matching based on metadata.
- **Stream via proxy (opt-in)** - per-series stream delivery packs `.strm` + NFO (no local media); clients play through Creatorr’s HTTP proxy. Download mode remains the default.
- **One series, many sources** - combine feed URLs (channels/playlists) and single-video URLs under one series.
- **YouTube channel roots** - a channel URL indexes the **Videos** tab only (not Shorts or Live). Add another source with `/shorts` (or a playlist URL) when you want those catalogs. See [`docs/ytdlp.md`](docs/ytdlp.md).
- **Library pack** - place media under a root folder with media-server-compatible NFO (and optional sidecars).
- **Maturity media refresh** - optional one-shot blind re-download (or stream re-pack) after upload date, timed on the quality profile (`0` = off).
- **Maturity sidecar refresh** - optional one-shot NFO/thumb/subs refresh after upload date (never rewrites `info.json`); same profile delays (UI days, stored hours).
- **SponsorBlock** - optional mark/remove on the quality profile, plus optional **info cards on cut** for downloads (needs re-encode). Uses SponsorBlock data from https://sponsor.ajay.app/ (Creatorr client; never yt-dlp `--sponsorblock-*`). Archive cuts + chapter embed; stream play plan + skip-aware beginning cache.
- **PO token support out of the box** - Compose `creatorr-po-token` sidecar + baked yt-dlp provider plugin; Settings → General controls fetch mode.
- Download via **in-tree yt-dlp** (`internal/ytdlp`); optional plugin mounts under `/yt-dlp-plugins`.
- Track videos (columns + packed `info.json`); admin web UI.
- Support for yt-dlp sites (with plugins where needed).
- Web UI (HTMX + daisyUI).
- Multi-source series; per-domain settings and per-domain queue.
- Subtitles (including ignore auto-generated); title include/exclude; media-type (shorts) detection and filters.
- Media-server NFO; stream without download; maturity media + sidecar refresh.
- Import local video; manage metadata; cookies / private playlists.
- FlareSolverr; SponsorBlock; notifications (in-app + Apprise); public API; download retry.

**Docs**

| Doc | Audience |
| --- | --- |
| [`docs/`](docs/README.md) | Product behavior (domain model, scan/queue, yt-dlp, settings, download/library) |
| [`AGENTS.md`](AGENTS.md) | AI agent contract (hard rules, architecture, workflow); UI details in [`docs/ui.md`](docs/ui.md) |
| [`api/openapi.yaml`](api/openapi.yaml) | REST contract |

**FlareSolverr:** Compose runs `creatorr-flaresolverr` (headless Chrome; notable RAM). Set `CREATORR_FLARESOLVERR_URL` (default `http://creatorr-flaresolverr:8191`). Enable **Use FlareSolverr** on Domain defaults and/or per-host override under Settings → Queue. Creatorr pre-solves via the FlareSolverr HTTP API (per-host session while the lane has work; short cookie cache), then passes `--cookies` / `--user-agent` to yt-dlp.

**PO tokens:** Compose runs `creatorr-po-token`; set `CREATORR_POT_PROVIDER_URL` (default `http://creatorr-po-token:4416`) and **PO token fetch** under Settings → General (`auto` / `always` / `never`). See [`docs/ytdlp.md`](docs/ytdlp.md).

## Run (Docker, production)

[`docker-compose.yml`](docker-compose.yml) pulls the published image (no local build):

```bash
docker compose up -d
```

- Image: `ghcr.io/xyxxyxxy/creatorr:latest` (`main`); `:develop` from `develop`; `:sha-<short>` for pinning
- UI: `http://127.0.0.1:8787/`
- Health: `GET http://127.0.0.1:8787/api/health`
- OpenAPI: `GET http://127.0.0.1:8787/api/openapi.json`

**Volumes** (host `./var` mirrors container layout):

| Host | Container |
| --- | --- |
| `./var/data` | `/data` (SQLite `creatorr.db`) |
| `./var/cache` | `/cache` |
| `./var/media/library` | `/media/library` (seeded root) |
| `./var/media/import` | `/media/import` |

Commented examples in compose: second library at `/media/other`, `/yt-dlp-plugins`, custom yt-dlp binary.

First boot seeds one root (`library` → `/media/library`, no TTL) and quality profiles `best` (format `bv*+ba`), `1080p`, `720p`.

The container runs as **uid/gid 1000** (non-root). Host `./var` must be writable by that user: `sudo chown -R 1000:1000 var`.

Image includes the Go binary plus sidecars: **yt-dlp**, **ffmpeg**, **Deno**. Runtime yt-dlp is `/usr/local/bin/yt-dlp`. First GHCR pull may need `docker login ghcr.io`.

### Custom yt-dlp

Bind-mount over the image path (`:ro`). No `CREATORR_YTDLP_BIN` env. Rebuild the image to bump the baked binary. Plugins stay under `/yt-dlp-plugins` (`--plugin-dirs`).

```yaml
volumes:
  - /path/to/your/yt-dlp:/usr/local/bin/yt-dlp:ro
```

## Run (Docker, development)

Build from source with UI live reload. Prefer the wrapper (syncs LAN public base URL into `.env`):

```bash
./scripts/compose up -d --build
```

Or explicitly:

```bash
docker compose -f docker-compose.dev.yml up -d --build
# optional local plugins:
docker compose -f docker-compose.dev.yml \
  -f docker-compose.override.dev.yml up -d --build
```

[`docker-compose.dev.yml`](docker-compose.dev.yml) **includes** the production compose (ports, `var/` volumes, security), then adds `build: .`, `CREATORR_WEB_DEV`, and mounts `./internal/web`. Copy [`docker-compose.override.dev.example.yml`](docker-compose.override.dev.example.yml) to `docker-compose.override.dev.yml` (gitignored) for sibling plugin mounts. There is **no** auto-merged `docker-compose.override.yml` (that would break production defaults).

Local image without registry: `make image` (tags `creatorr:local` by default).

## Run (local Go)

Paths use `./var/...` when `/data` is not a directory (see [`internal/config`](internal/config/config.go)).

```bash
go run ./cmd/creatorr
# or: make build && ./bin/creatorr
```

```bash
make test
make vet
make generate   # after editing api/openapi.yaml
make openapi-check
make css        # after Tailwind/daisyUI class changes or ECharts npm bump (copies vendor)
make sbom       # repo CycloneDX SBOM (needs syft); CI also uploads + license-gates it
```

CI: [`.github/workflows/ci.yml`](.github/workflows/ci.yml) on `main` + `develop`. Docker: [`.github/workflows/docker.yml`](.github/workflows/docker.yml) (`:latest` on `main`, `:develop` on `develop`, plus `:sha-<short>`).

## Fixed paths (not env)

When `/data` exists (container): SQLite `/data/creatorr.db`, cache `/cache`, seed library `/media/library`, import `/media/import`, plugins `/yt-dlp-plugins`. Local Go mirrors under `var/`.

## Environment variables

Bootstrap only. HTTP always binds `0.0.0.0`. Most runtime knobs are Settings (SQLite / UI). FlareSolverr and PO token provider URLs are env-only. Apprise channels are Settings.

| Variable | Default | Purpose |
|---|---|---|
| `CREATORR_PORT` | `8787` | HTTP port |
| `CREATORR_PUBLIC_BASE_URL` | *(empty)* | Optional first-boot seed into Settings `external_base_url`. Prefer **Settings → Library → Streaming → External Creatorr URL**. Dev: [`scripts/sync-dev-public-url.sh`](scripts/sync-dev-public-url.sh) / [`scripts/compose`](scripts/compose) writes `http://<LAN-IPv4>:8787` into `.env`. |
| `CREATORR_POT_PROVIDER_URL` | *(empty)* | bgutil PO token provider base URL. Compose default `http://creatorr-po-token:4416`. Empty disables Settings **PO token fetch**. Shown read-only on Settings → General. |
| `CREATORR_FLARESOLVERR_URL` | *(empty)* | FlareSolverr base URL. Compose default `http://creatorr-flaresolverr:8191`. Empty skips Flare health/pre-solve and clears Use FlareSolverr flags on boot. Shown read-only on Settings → General. |
| `CREATORR_WEB_DEV` | off | Reload HTML/partials/`static/` from disk each request (`1` in `docker-compose.dev.yml`). |
| `CREATORR_WEB_DIR` | `internal/web` | UI root when `CREATORR_WEB_DEV` is on (`/web` with the dev mount). |
| `TZ` | host / unset | Process local zone for **UI cron labels** only (stored cron still UTC). Prod compose `${TZ:-UTC}`; optional machine-local default in `docker-compose.override.dev.yml` (example: Europe/Berlin). |

For stream delivery, set **External Creatorr URL** in Settings (or seed once via `CREATORR_PUBLIC_BASE_URL` in `.env`).

**UI live reload (dev):** with `CREATORR_WEB_DEV=1`, edit templates/partials under `CREATORR_WEB_DIR` and refresh the browser. New Tailwind/daisyUI classes still need `make css`. Go/handler changes still need rebuild.

## Non-goals

- Migrate older Creatorr SQLite databases (fresh install only). After pulling a schema-breaking change, delete `var/data/creatorr.db` (container: wipe the `/data` volume) and restart - empty index expected; no `ALTER` / migrate helpers.
- CLI tools other than the Creatorr daemon as product entrypoints.

## Concepts

See **Terminology** in [`AGENTS.md`](AGENTS.md). Topic detail under [`docs/`](docs/README.md).
