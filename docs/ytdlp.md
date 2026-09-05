# yt-dlp and plugins

Index: [README.md](README.md). Terminology: [`AGENTS.md`](../AGENTS.md).

Creatorr invokes **yt-dlp in-process** (`internal/ytdlp`). There is no external domain-handler registry or CLI protocol.

## Binary

| Path | Role |
| --- | --- |
| `/data/bin/yt-dlp` (container) or `var/data/bin/yt-dlp` (local Go) | **Managed runtime** binary on the data volume. Creatorr always execs this path. |
| `/usr/local/share/creatorr/yt-dlp` (container image only) | **Bootstrap** copy baked at image build (SHA2-256 verified). First boot copies to the managed path when missing; not the runtime path. |
| `/yt-dlp-plugins` (container) or `var/yt-dlp-plugins` (local) | Operator plugin mounts; always passed as `--plugin-dirs` (subdirs with a `yt_dlp_plugins` package are included). |
| `/usr/local/share/yt-dlp-plugins/bgutil` (container) or `var/yt-dlp-plugins/bgutil` (local after `make pot-plugin`) | Baked / seeded **PO Token provider plugin** (GPL-3.0; separate package). Creatorr passes the **parent** (`…/yt-dlp-plugins`) as `--plugin-dirs` so yt-dlp discovers the `bgutil` package. Survives mounting over `/yt-dlp-plugins`. |

Boot runs `PrepareManagedBin`: when the managed file is missing or fails `--version`, Creatorr copies the image bootstrap (or, local dev only, GitHub-downloads once when no bootstrap exists). Startup **exits** if yt-dlp cannot be established at the managed path.

### Automatic updates

When **`ytdlp_update_cron`** is non-empty (Settings → Scheduler; seed `@weekly`), Creatorr enqueues a **`ytdlp_update`** task on the **`system`** lane on boot, on schedule, and via Settings → Connect → **Update now**. The worker downloads from GitHub (`stable` or `nightly` via **`ytdlp_update_channel`**), verifies **SHA2-256** against the release `SHA2-256SUMS`, runs `--version` on the temp file, then atomically replaces the managed binary and hot-swaps the in-process client path.

**Empty `ytdlp_update_cron`** disables boot, cron, and manual GitHub updates so you can pin a **custom binary**: stop Creatorr, replace the managed path, restart. Set a schedule again to re-enable managed updates.

Failed updates leave the prior binary intact and do **not** soft-pause domain lanes.

Image rebuild refreshes the bootstrap baseline; a running instance updates via `ytdlp_update`, not rebuild.

Image also ships **ffmpeg** (remux) and **Deno** (yt-dlp EJS challenge solver).

## FlareSolverr

Compose service **`creatorr-flaresolverr`** (`ghcr.io/flaresolverr/flaresolverr`) solves CloudFlare challenges with headless Chrome (**notable RAM**; port 8191 stays internal).

| Piece | Detail |
| --- | --- |
| Env | `CREATORR_FLARESOLVERR_URL` (Compose default `http://creatorr-flaresolverr:8191`). Empty = skip health probe and FlareSolverr pre-solve; boot clears Use FlareSolverr flags. |
| Opt-in | Per-host **Use FlareSolverr** On (Settings → Queue / Domains override). Domain defaults Flare is always off. UI/API refuse On without the env URL. |
| Pre-solve | Creatorr calls FlareSolverr `request.get` (not yt-dlp `--flaresolverr`), merges cookies into a Netscape jar, passes `--cookies` / `--user-agent` to yt-dlp. |
| Session | One browser session per hostname (`sessions.create`) while that domain lane has pending/running work; destroyed when the lane drains. `session_ttl_minutes` safety net on each get. |
| Cookie cache | Successful clearance cookies are cached in-process (2–30 min) so warm lanes often skip Flare HTTP; cache miss still hits the warm session when open. |
| Tasks UI | Lane header shield icon when Flare is effective: muted = enabled, `text-info` = session warm. |
| Health | `/api/health` check `flaresolverr` probes the env URL (skipped if unset). Settings → Connect loads the same probe asynchronously after the page shell (Healthy join). |

## PO Token provider

Compose service **`creatorr-po-token`** (`brainicism/bgutil-ytdlp-pot-provider:*-deno`) generates attestation tokens for hosts that require them. Providing a token does not guarantee bypassing 403 or bot checks.

| Piece | Detail |
| --- | --- |
| Env | `CREATORR_POT_PROVIDER_URL` (default in Compose: `http://creatorr-po-token:4416`). Empty → Settings **PO token fetch** disabled; yt-dlp gets `youtube:fetch_pot=never`. |
| Settings | `pot_fetch`: `auto` (default) / `always` / `never` → `youtube:fetch_pot=…` when URL is set. |
| Trace | When URL is set and fetch is not `never`, Creatorr also passes `youtube:pot_trace=true` so mint/provider lines appear in task logs. |
| Detect | yt-dlp output is scanned for provider failures (`Providers: none`, HTTP ping/mint errors) and successful mints (`Retrieved a … PO Token`). The task still succeeds on provider problems; Creatorr emits warning notification `pot_provider` (unread like alerts). Outcome is stored on the task as detail JSON `po-token` (`issued` / `failed` / `skipped` / `off`) and shown on the task Details row **PO token**. |
| Health | `/api/health` check `pot_provider` probes `GET {URL}/ping` (skipped if URL unset). Settings → Connect loads the same probe asynchronously after the page shell (Healthy join). |
| Local Go | `make pot-plugin` installs the zip under `var/yt-dlp-plugins/bgutil`; run a provider yourself and set `CREATORR_POT_PROVIDER_URL`. |

Creatorr passes `--extractor-args youtubepot-bgutilhttp:base_url=…` when the env URL is set and fetch is not `never`.

## What Creatorr owns

- FlareSolverr pre-solve when the host override sets **Use FlareSolverr** On and `CREATORR_FLARESOLVERR_URL` is set (see **FlareSolverr** above).
- PO Token provider URL (env) + Settings **PO token fetch** mode (see above).
- Netscape cookie jars on host Domain overrides only (`domains.cookies`; Settings → Queue / Domains) for Cloudflare clearance and similar. No default-jar fallback.
- Membership credentials on host `domains` override rows only (`username` / `password`): passed as yt-dlp `--username` / `--password` when non-empty. Site plugins may cache access tokens in yt-dlp's default cache (`~/.cache/yt-dlp` under process `HOME`; Creatorr does not pass `--cache-dir`). Do not export member session cookies into the jar when a plugin supports login.
- **Delivery mode format selector:** series `delivery_mode` picks the format string. **Video** (default) passes the series' quality profile `format_selector` as `--format`. **Audio** ignores the profile selector and always passes `ba/bestaudio/b` (best available audio); the quality profile is still attached to the series (maturity delays, SponsorBlock) but its format ladder does not apply.
- Pace flags from domain limits: `--limit-rate` from `download_rate_limit` (archive/scan); when `sleep_requests` > 0 also `--sleep-requests`, `--sleep-subtitles`, and `--sleep-interval` (same seconds; no `--max-sleep-interval`).
- Subtitle sidecars from Library settings (`subtitle_langs` / `subtitle_auto`): `--write-subs` + `--convert-subs srt` (never `--embed-subs`). Used on archive download and sidecar refresh (`refresh_sidecars` / metadata rescan). Empty langs = off. Auto-only tracks are renamed to `.lang.auto.srt` after download.
- Archive downloads **always** pass `--match-filters` `is_live!=?1` (soft-skip while broadcasting; missing `is_live` passes). Series `auto_ignore_media_types` → additional `media_type!=…` clauses AND'd into the same filter; media-type reject → `MediaTypeExcluded` (ignored, not download error). Live reject → `LiveBroadcastSkipped` (stay `wanted`).
- Remux (ffmpeg → MKV video / MKA audio) and pack after download.

`CookieInvalid` / `RateLimited` → auto soft-pause the hostname lane + notify alert events (`cookie_invalid` / `rate_limited`; never auto-unmonitor or deactivate). `DownloadFailed` / `ResolveFailed` → notify `ytdlp_failed` only (no soft-pause); download tasks still set video → `wanted_download_error`. Narrow YouTube unavailable (removed/deleted/unavailable/terminated) with `archive_fallback` on → video `wanted_archive` + separate `archive.org` download (`ytarchive:{remote_id}`); suppress `ytdlp_failed` for that live failure; successful archive pack notifies info `archive_fallback` and sets `acquired_via=archive`. `AgeRestricted` → video `wanted_download_error` only (per-video age gate; no soft-pause, no alert). Scan/list failures with cookie/rate pause codes soft-pause the domain only (no per-source hold). **Download-wanted** skips enqueue for soft-paused domains. Remux/pack/verify do not auto-pause. Other failed non-system domain tasks → alert `ytdlp_failed`. PO token provider problems while yt-dlp continues → warning `pot_provider` (no soft-pause, task not failed). Every event is always recorded in-app (alerts/warnings unread until History open / in-app read; digests are info); Apprise is optional fan-out and does not clear unread.

## Plugins (site extractors)

Optional yt-dlp extractor packages live in **separate repos** (local sibling checkouts for now; GitHub hosting later), volume-mounted under `/yt-dlp-plugins/<name>/` (each mount root must contain `yt_dlp_plugins/`).

Tracked examples use `example.com` only. For dev, copy [`docker-compose.override.dev.example.yml`](../docker-compose.override.dev.example.yml) to `docker-compose.override.dev.yml` (gitignored) and use it with [`docker-compose.dev.yml`](../docker-compose.dev.yml) (see README).

Without plugins, unsupported hosts fail with yt-dlp’s normal “unsupported URL” (no ghost registry).

Example compose mounts:

```yaml
volumes:
  - ../Creatorr-handler-example.com:/yt-dlp-plugins/example:ro
```

## Video metadata columns + info.json

Downloads/imports set Creatorr-owned columns (`tool`, `download_format_selector`, `download_remux_container` when remux ran, `import_src`, duration/resolution/fps). Site-specific detail stays in packed **`info.json`** (opaque - Creatorr never edits its content; copy/move/rename only; write/replace only when media changes). No sticky `handler_id` on sources/videos.

## Entry `upload_date`

RFC3339 UTC only (full timestamp). Same rules as before for season / episode / NFO `<aired>` (UTC calendar day).

**List order:** newest-first assumed (yt-dlp flat playlist / plugin extractors must match). Order is site-specific; `--playlist-end` (source `full_scan_limit`) takes the first N entries in that extractor order.

**Channel URLs:** yt-dlp `--flat-playlist` on a channel root returns tab playlists (Videos / Live / Shorts) with videos nested inside. Creatorr expands the **Videos** tab when listing; use a `/videos` URL (or a playlist URL) when you want that catalog explicitly. Metadata prefetch still uses the channel root for art/title.
