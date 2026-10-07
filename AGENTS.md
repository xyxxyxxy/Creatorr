# AGENTS.md - Creatorr agent contract

Mandatory reading for AI agents. Creatorr is a Sonarr-shaped Go daemon for creator VOD: mirror channels/playlists, download via in-tree yt-dlp (+ optional plugins), track videos + packed `info.json`, pack TV libraries (video + NFO).

**Stack:** Go only (`github.com/xyxxyxxy/Creatorr`). SQLite for app state; published images on GHCR (`:latest` / `:dev` / `:X.Y.Z` / `:X.Y` / `:sha-<short>`).

## Hard rules

- **No commits on main:** GitHub Flow. Never commit or push to `main`; use a short-lived branch and a pull request. If already on `main`, branch first. See `.cursor/rules/github-flow.mdc`.
- **Metadata suggestion pool:** studio, genres, tags, country, mpaa, actor name, actor role - each field name is one library-wide distinct-value pool across `series` + `videos` (`ListMetaSuggestions`). Series and video Metadata modals share the same pools; never series-only or video-only datalists. SoftFill-blocked values are omitted from datalists. SoftFill (code) / **auto-fill** (UI copy): toggles `softfill_tags` / `softfill_genres` / `softfill_domain_tag` (default on) and SoftFill blocklist (`softfill_blocklist`, ingress-only) live under Settings → Library → Metadata and Settings → Catalog. Catalog rename/remove/remove+block auto-fill rewrites series+videos+sources via path-touching `rewrite_catalog_meta`. Operator-facing strings say auto-fill, not SoftFill. Details: [`docs/download-and-library.md`](docs/download-and-library.md), [`docs/settings.md`](docs/settings.md).
- **info.json provenance:** write or replace packed `info.json` only when the video media file changes (archive download / maturity re-download pack). Independent sidecar refresh and metadata rescan must never delete or rewrite `info.json`. Details: [`docs/download-and-library.md`](docs/download-and-library.md).
- **No em dash:** never write Unicode em dash (U+2014) in docs, UI copy, comments, OpenAPI, flash strings, or commits. Prefer ` - `, `: `, or a period. Empty UI placeholders use ASCII `-`. See `.cursor/rules/no-em-dash.mdc`.
- **API:** edit `api/openapi.yaml` → `make generate` → implement handlers. Never hand-edit `internal/api/gen/`.
- **Never invoke yt-dlp from HTTP handlers** - enqueue a task; worker runs yt-dlp.
- **Flags never auto-cascade** - series `monitored` and domain `active` are operator-only; cookie/rate failures soft-pause the domain lane and notify, do not deactivate.
- **Ask when ambiguous** - behavior not in [`docs/`](docs/README.md), API/UI forks, missing env, large/unclear CSS overrides, adding a non-daisyUI library. Short question + recommended default → wait → record in the matching docs file (AGENTS only for agent-contract rules).
- **daisyUI first** - stock daisyUI + Tailwind in markup; minimal `input.css` only for small glue (document why). Do not add another UI kit. UI work: read [`docs/ui.md`](docs/ui.md).
- **File size:** prefer new/changed Go files under ~500 lines. Do not grow former gods (`queue`, `import`, `maintenance`, `series_meta`, `settings_actions`, monolith `app.js` sources) without splitting first. Same-package file split over new packages unless a boundary already exists.
- **No write on list/read paths:** video list enrichment must not `UPDATE` SQLite (resolution/duration labels are read-only on GET; backfill only on pack/download/explicit jobs).
- **Batch hot loops:** scheduler/maintenance loops that check per-video pending download or media presence must prefetch sets, not N+1 `HasVideoFile` / `hasPendingDownload`.
- **Do not discard multi-step errors:** folder move / NFO rewrite / apply-naming / task `Finish` chains must propagate or fail the task, not `_ =` / `_, _, _ =` on partial success.
- **No History-only kinds:** do not add task kinds whose only job is a finished History stamp. Sidecar delete is unlink + drop `files` row (no History, no task). Series folder title/root change is the async system task `series_move` (`queue.KindSeriesMove`): HTTP and bulk edit only enqueue; the worker updates SQLite title/root, moves the folder, writes `tvshow.nfo`, then applies naming (episode NFOs are not rewritten: they omit `showtitle`). No SyncDisk. Path-touching exclusivity is library-wide with `rename_episodes`, `regenerate_nfo`, `reset_metadata_from_info`, `sync_files`, `retention_delete`, `rewrite_catalog_meta`, and any other `series_move`. UI locks Edit title/root and refuses Download now / path-affecting video Metadata while one is open for that series. Details: [`docs/scan-and-queue.md`](docs/scan-and-queue.md).
- **JS modules:** edit sources under `internal/web/ui/src/js/`; bundle with esbuild via `make css`; do not grow committed `app.js` / `import.js` by hand; no second bundler. New interactive UI contracts need a test or string-guard pin.
- Keep files small; no god modules. New endpoint → small handler file or package method.
- **Portable examples only** - no real hostnames/IPs/home paths; use `example.com` and env placeholders. Tests: `t.TempDir()`, fixtures - never infer paths from this machine. Operator-facing UI/docs copy: do not name real video sites (generic DASH/CDN language only).
- **Local-only tests:** never trigger requests to external sites or the public internet from tests (`go test` / `make test`). Doubles stay on-machine: `httptest` / loopback, fake yt-dlp scripts, `t.TempDir()`, in-process swaps (e.g. `SetSendFnForTest`), checked-in fixtures/goldens. `example.com`-style URLs must resolve only via mocks (never leave the process). Forbidden: live FlareSolverr / POT / Apprise / GitHub / CDN / site extractors, or any "just this once" live smoke inside unit/integration tests. Agents must not add, enable, or suggest live-net test paths.

## Architecture

```text
cmd/creatorr/           main entrypoint
api/openapi.yaml        REST contract (source of truth)
internal/api/gen/       oapi-codegen output (committed; do not hand-edit)
internal/api/           handler impl, SSE, route mounting
internal/config/        env bootstrap + settings bridge
internal/db/            SQLite open + schema + stepwise migrations (`schema_version`)
internal/library/       series, source, video types + files, pack, remux, import, NFO
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
internal/web/           HTMX UI (see docs/ui.md); JS sources in internal/web/ui/src/js/ (esbuild via make css)
internal/testutil/      shared test DB opener (+ fakemedia)
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
- **ErrorResponse:** `{code, message, detail?}` - stable `code` (`CookieInvalid`, `DownloadFailed`, `RemuxFailed`, `PackFailed`, …). Worker stores `error_code` + `error_message` on tasks (plus separate `message`); sources keep their own error fields. Cookie/rate failures (`CookieInvalid` / `RateLimited`) **auto soft-pause** the domain lane and notify as alerts (no auto-deactivate). Soft-pause alone never bumps the Queues nav badge (open tasks only). Generic `DownloadFailed` / `ResolveFailed` fail the task (download → `wanted_download_error`) and notify `ytdlp_failed` but do **not** soft-pause. Never bare HTTP status with empty body.
- **Out of OpenAPI:** HTMX routes + SSE `GET /api/events` (`EventSource`). Events: `task.updated` | `task.done` | `task.failed` | `notification.created` | `notification.read` (JSON in `data:`; keepalive ~15s; outside 60s HTTP timeout). Product behavior: [`docs/`](docs/README.md).

## Workflow

1. Read this file + [`docs/README.md`](docs/README.md), then the topic doc for the task (UI → [`docs/ui.md`](docs/ui.md)).
2. API: OpenAPI → `make generate` → handlers → tests.
3. UI: daisyUI + shared partials; follow setting-description rules in `docs/ui.md`. JS: edit `internal/web/ui/src/js/`, then `make css`.
4. Branch: never commit on `main`. Before push: `make test vet lint openapi-check` (and `make css` if UI classes/vendors/JS sources changed). After clone, `make hooks` enables `.githooks/pre-commit` (lint + test on each commit; skip with `SKIP_GITHOOKS=1`).
5. Prompt on uncertainty; do not guess.

## Testing

- **Not mandatory TDD.** Red-green test-first is optional where the design is stable (settings, queue, yt-dlp parsers, handlers after the OpenAPI contract is drafted). Skip strict TDD for exploratory UI, remux/ffmpeg paths.
- **Required on behavior change:** ship a test or updated fixture/golden that would catch the bug. Outcome matters more than red-green order.
- **Must-cover:** behavior changes to Want/Ignore (incl. bulk), soft-pause orchestration, download-wanted eligibility, and queue ClaimNext/cooldown ship with unit tests. Prefer shared `internal/testutil` DB opener over copy-pasted SeedDefaults blocks in new API/web tests.
- **Layers:** unit (library/settings/parsers); yt-dlp via fake binary + goldens under `internal/ytdlp/testdata/` (see `fake-yt-dlp`); integration (temp SQLite + worker/queue); API httptest for regression-critical paths. Prefer **narrow** goldens (yt-dlp List/Resolve JSON dumps; not NFO/HTML). Hermetic externals only: FlareSolverr / POT / Apprise / GitHub via `httptest` or test swaps - never live (Hard rule **Local-only tests**). OpenAPI contract drift stays CI `make openapi-check` (not a `go test` schema suite).
- **Fake media tools:** PATH-gated ffmpeg/ffprobe tests use `internal/testutil/fakemedia` (local scripts; never require host ffmpeg for those cases).
- **Preferred test-first zones:** library/queue/parsers; then handlers; UI tests after markup settles.

## Ship

- **Health:** `GET /api/health` - `ok` | `degraded` | `down`; checks `db`, `worker` (in-process heartbeat, not SQLite), `ytdlp`, `disk`, `flaresolverr`, `pot_provider` (last two skipped if URL unset). Compose healthcheck should use it.
- **Images:** `ghcr.io/xyxxyxxy/creatorr:latest`, `:X.Y.Z`, and `:X.Y` from git tags `v*` on `main` (docker/metadata strips the `v` prefix); `:dev` tracks tip of `main` (and `workflow_dispatch` rebuilds); `:sha-<short>` on every `main` push for pins and pre-release testing. Compose: [`docker-compose.yml`](docker-compose.yml).
- **Tests:** see **Testing** above. Before push: `make test` (and the rest of the pre-push checklist in Workflow).
- **Branching:** GitHub Flow - `main` is the only long-lived branch. Never commit on `main`. Short-lived branch, then a pull request into `main`.
- **Commits:** Conventional Commits; one logical step each; subject ≤72 chars; body explains why when not obvious.
