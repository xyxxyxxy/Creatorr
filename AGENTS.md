# AGENTS.md - Creatorr agent contract

Mandatory reading for AI agents. Creatorr is a Sonarr-shaped Go daemon for creator VOD: mirror channels/playlists, download via in-tree yt-dlp (+ optional plugins), track videos + packed `info.json`, pack TV libraries (video + NFO).

**Stack:** Go only (`github.com/xyxxyxxy/Creatorr`). SQLite for app state; published images on GHCR (`:latest` / `:sha-<short>`).

## Hard rules

- **Metadata suggestion pool:** studio, genres, tags, country, mpaa, actor name, actor role - each field name is one library-wide distinct-value pool across `series` + `videos` (`ListMetaSuggestions`). Series and video Metadata modals share the same pools; never series-only or video-only datalists. Details: [`docs/download-and-library.md`](docs/download-and-library.md).
- **info.json provenance:** write or replace packed `info.json` only when the video media file changes (archive download / maturity re-download pack). Independent sidecar refresh and metadata rescan must never delete or rewrite `info.json`. Details: [`docs/download-and-library.md`](docs/download-and-library.md).
- **No em dash:** never write Unicode em dash (U+2014) in docs, UI copy, comments, OpenAPI, flash strings, or commits. Prefer ` - `, `: `, or a period. Empty UI placeholders use ASCII `-`. See `.cursor/rules/no-em-dash.mdc`.
- **API:** edit `api/openapi.yaml` → `make generate` → implement handlers. Never hand-edit `internal/api/gen/`.
- **Never invoke yt-dlp from HTTP handlers** - enqueue a task; worker runs yt-dlp.
- **Flags never auto-cascade** - series `monitored` and domain `active` are operator-only; cookie/rate failures soft-pause the domain lane and notify, do not deactivate.
- **Ask when ambiguous** - behavior not in [`docs/`](docs/README.md), API/UI forks, missing env, large/unclear CSS overrides, adding a non-daisyUI library. Short question + recommended default → wait → record in the matching docs file (AGENTS only for agent-contract rules).
- **daisyUI first** - stock daisyUI + Tailwind in markup; minimal `input.css` only for small glue (document why). Do not add another UI kit. UI work: read [`docs/ui.md`](docs/ui.md).
- Keep files small; no god modules. New endpoint → small handler file or package method.
- **Portable examples only** - no real hostnames/IPs/home paths; use `example.com` and env placeholders. Tests: `t.TempDir()`, fixtures - never infer paths from this machine. Operator-facing UI/docs copy: do not name real video sites (generic DASH/CDN language only).

## Architecture

```text
cmd/creatorr/           main entrypoint
api/openapi.yaml        REST contract (source of truth)
internal/api/gen/       oapi-codegen output (committed; do not hand-edit)
internal/api/           handler impl, SSE, route mounting
internal/config/        env bootstrap + settings bridge
internal/db/            SQLite open + schema + stepwise migrations (`schema_version`)
internal/domain/        series, source, video types
internal/library/       series/videos/files, pack, remux, import, NFO
internal/queue/         per-domain task queue, cooldown, History (finished tasks)
internal/domains/       known hostnames: active + optional limit overrides; soft pause in domain_runtime; Access cookies/credentials on host rows
internal/settings/      SQLite settings keys + domain_queue JSON
internal/scheduler/     cron kicks (scan, download-wanted, file sync, retention purge)
internal/worker/        background task runner
internal/events/        SSE hub
internal/health/        /api/health dependency checks
internal/ytdlp/         in-tree yt-dlp invoke, image/PATH binary, plugins
internal/notify/        Apprise (apprise-go) + in-app log (info digests vs unread alerts/warnings)
internal/stats/         every-minute change-only sampler + daily library size + chart series for /stats
internal/web/           HTMX UI (see docs/ui.md)
internal/errors/        AppError codes + ErrorResponse mapping
.github/workflows/      GitHub Actions CI + GHCR images
```

Image sidecars / yt-dlp binary / plugins: [`docs/ytdlp.md`](docs/ytdlp.md). **Stats** live only in `internal/stats` (not the main scheduler) - global change-only samples, daily storage, retention and pie endpoints: [`docs/settings.md`](docs/settings.md).

## Terminology

**Naming:** prefer **task** for queued work. **Job** = implicit recurring schedule (no `jobs` table). **History** = finished tasks (`done`/`failed`/`cancelled`). Do not use **poll**; use **scan**. Do not use **Activity** as a product term.

**Glossary:** [`docs/domain-model.md`](docs/domain-model.md). yt-dlp: [`docs/ytdlp.md`](docs/ytdlp.md). Scan/queue: [`docs/scan-and-queue.md`](docs/scan-and-queue.md). Download/library: [`docs/download-and-library.md`](docs/download-and-library.md). UI: [`docs/ui.md`](docs/ui.md).

New domain term → matching docs file (domain-model by default).

## API & errors

- Contract: [`api/openapi.yaml`](api/openapi.yaml). Generate: `make generate`. Serve: `GET /api/openapi.json`. CI: `make openapi-check`.
- **ErrorResponse:** `{code, message, detail?}` - stable `code` (`CookieInvalid`, `DownloadFailed`, `RemuxFailed`, `PackFailed`, …). Worker stores `code` + `message` on tasks/sources. Cookie/rate failures (`CookieInvalid` / `RateLimited`) **auto soft-pause** the domain lane and notify as alerts (no auto-deactivate). Generic `DownloadFailed` / `ResolveFailed` fail the task (download → `wanted_download_error`) and notify `ytdlp_failed` but do **not** soft-pause. Never bare HTTP status with empty body.
- **Out of OpenAPI:** HTMX routes + SSE `GET /api/events` (`EventSource`). Events: `task.updated` | `task.done` | `task.failed` | `notification.created` | `notification.read` (JSON in `data:`; keepalive ~15s; outside 60s HTTP timeout). Product behavior: [`docs/`](docs/README.md).

## Workflow

1. Read this file + [`docs/README.md`](docs/README.md), then the topic doc for the task (UI → [`docs/ui.md`](docs/ui.md)).
2. API: OpenAPI → `make generate` → handlers → tests.
3. UI: daisyUI + shared partials; follow setting-description rules in `docs/ui.md`.
4. Before push: `make test vet lint openapi-check` (and `make css` if UI classes/vendors changed). After clone, `make hooks` enables `.githooks/pre-commit` (lint + test on each commit; skip with `SKIP_GITHOOKS=1`).
5. Prompt on uncertainty; do not guess.

## Ship

- **Health:** `GET /api/health` - `ok` | `degraded` | `down`; checks `db`, `worker` (in-process heartbeat, not SQLite), `ytdlp`, `disk`, `flaresolverr`, `pot_provider` (last two skipped if URL unset). Compose healthcheck should use it.
- **Images:** `ghcr.io/xyxxyxxy/creatorr:latest` and `:vX.Y.Z` from version tags (`v*`) on `main`; `:sha-<short>` on every `main` push for pins and pre-release testing. Compose: [`docker-compose.yml`](docker-compose.yml).
- **Tests:** unit (domain/settings), yt-dlp fixtures (no live net), integration (temp SQLite + worker/queue), API httptest + schema. Prefer golden fixtures; add tests for behavior changes.
- **Branching:** GitHub Flow - `main` is the only long-lived branch; use short-lived branches and pull requests into `main`.
- **Commits:** Conventional Commits; one logical step each; subject ≤72 chars; body explains why when not obvious.
