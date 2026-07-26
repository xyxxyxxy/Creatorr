# yt-dlp and plugins

Index: [README.md](README.md). Terminology: [`AGENTS.md`](../AGENTS.md).

Creatorr invokes **yt-dlp in-process** (`internal/ytdlp`). There is no external domain-handler registry or CLI protocol.

## Binary

| Path | Role |
| --- | --- |
| `/usr/local/bin/yt-dlp` | Runtime binary in the Docker image (build-time latest release). Creatorr prefers this path, then falls back to `yt-dlp` on `PATH` (local Go). |
| `/yt-dlp-plugins` (container) or `var/yt-dlp-plugins` (local) | Operator plugin mounts; always passed as `--plugin-dirs` (subdirs with a `yt_dlp_plugins` package are included). |
| `/usr/local/share/yt-dlp-plugins/bgutil` (container) or `var/yt-dlp-plugins/bgutil` (local after `make pot-plugin`) | Baked / seeded **PO Token provider plugin** (GPL-3.0; separate package). Creatorr passes the **parent** (`…/yt-dlp-plugins`) as `--plugin-dirs` so yt-dlp discovers the `bgutil` package. Survives mounting over `/yt-dlp-plugins`. |

There is **no** in-app yt-dlp update schedule. Bump yt-dlp by rebuilding the image. Boot runs the resolved binary with `--version` and exits if missing or broken.

**Custom yt-dlp (Docker):** bind-mount over the image path:

```yaml
volumes:
  - /path/to/your/yt-dlp:/usr/local/bin/yt-dlp:ro
```

Operator plugins keep working under `/yt-dlp-plugins` (`--plugin-dirs`).

Image also ships **ffmpeg** (remux) and **Deno** (yt-dlp EJS challenge solver).

## FlareSolverr

Compose service **`creatorr-flaresolverr`** (`ghcr.io/flaresolverr/flaresolverr`) solves CloudFlare challenges with headless Chrome (**notable RAM**; port 8191 stays internal).

| Piece | Detail |
| --- | --- |
| Env | `CREATORR_FLARESOLVERR_URL` (Compose default `http://creatorr-flaresolverr:8191`). Empty = skip health probe and FlareSolverr pre-solve; boot clears Use FlareSolverr flags. |
| Opt-in | Domain defaults / per-host **Use FlareSolverr** (Settings → Queue). UI/API refuse On without the env URL. |
| Pre-solve | Creatorr calls FlareSolverr `request.get` (not yt-dlp `--flaresolverr`), merges cookies into a Netscape jar, passes `--cookies` / `--user-agent` to yt-dlp. |
| Session | One browser session per hostname (`sessions.create`) while that domain lane has pending/running work; destroyed when the lane drains. `session_ttl_minutes` safety net on each get. |
| Cookie cache | Successful clearance cookies are cached in-process (2–30 min) so warm lanes often skip Flare HTTP; cache miss still hits the warm session when open. |
| Tasks UI | Lane header shield icon when Flare is effective: muted = enabled, `text-info` = session warm. |
| Health | `/api/health` check `flaresolverr` probes the env URL (skipped if unset). Settings → General shows the same probe once on page load (Healthy join). |

## PO Token provider

Compose service **`creatorr-po-token`** (`brainicism/bgutil-ytdlp-pot-provider:*-deno`) generates attestation tokens for hosts that require them. Providing a token does not guarantee bypassing 403 or bot checks.

| Piece | Detail |
| --- | --- |
| Env | `CREATORR_POT_PROVIDER_URL` (default in Compose: `http://creatorr-po-token:4416`). Empty → Settings **PO token fetch** disabled; yt-dlp gets `youtube:fetch_pot=never`. |
| Settings | `pot_fetch`: `auto` (default) / `always` / `never` → `youtube:fetch_pot=…` when URL is set. |
| Trace | When URL is set and fetch is not `never`, Creatorr also passes `youtube:pot_trace=true` so mint/provider lines appear in task logs. |
| Detect | yt-dlp output is scanned for provider failures (`Providers: none`, HTTP ping/mint errors) and successful mints (`Retrieved a … PO Token`). The task still succeeds on provider problems; Creatorr emits warning notification `pot_provider` (unread like alerts). Outcome is stored on the task as detail JSON `po-token` (`issued` / `failed` / `skipped` / `off`) and shown on the task Details row **PO token**. |
| Health | `/api/health` check `pot_provider` probes `GET {URL}/ping` (skipped if URL unset). Settings → General shows the same probe once on page load (Healthy join). |
| Local Go | `make pot-plugin` installs the zip under `var/yt-dlp-plugins/bgutil`; run a provider yourself and set `CREATORR_POT_PROVIDER_URL`. |

Creatorr passes `--extractor-args youtubepot-bgutilhttp:base_url=…` when the env URL is set and fetch is not `never`.

## What Creatorr owns

- FlareSolverr pre-solve when effective **Use FlareSolverr** is on for the host (Domain defaults or host On override) and `CREATORR_FLARESOLVERR_URL` is set (see **FlareSolverr** above).
- PO Token provider URL (env) + Settings **PO token fetch** mode (see above).
- Netscape cookie jars (Settings → Queue; host jar, else `default`).
- Pace flags from domain limits: `--limit-rate` from `download_rate_limit` (archive/scan/`cache_beginning`) or `stream_play_rate_limit` (stream_play mux/pipe only); when `sleep_requests` > 0 also `--sleep-requests`, `--sleep-subtitles`, and `--sleep-interval` (same seconds; no `--max-sleep-interval`). **`stream_play` never gets sleep** (client waits on bytes). Resolve (`urls`) for play has no pace flags.
- Subtitle sidecars from Library settings (`subtitle_langs` / `subtitle_auto`): `--write-subs` + `--convert-subs srt` (never `--embed-subs`). Used on archive download, stream `pack_stream`, and Replace sidecars. Empty langs = off. Auto-only tracks are renamed to `.lang.auto.srt` after download.
- Archive downloads **always** pass `--match-filters` `is_live!=?1` (soft-skip while broadcasting; missing `is_live` passes). Series `auto_ignore_media_types` → additional `media_type!=…` clauses AND'd into the same filter; media-type reject → `MediaTypeExcluded` (ignored, not download error). Live reject → `LiveBroadcastSkipped` (stay `wanted`). Stream `pack_stream` reads `is_live` and `media_type` from the `urls` extract (live soft-skip first, then media-type ignore before `.strm` / `cache_beginning`).
- Remux (ffmpeg → MKV) and pack after download.
- Stream proxy: resolve URLs (`progressive` / `pipe` / `hls`), HLS mux, beginning-cache.

`CookieInvalid` / `RateLimited` / `DownloadFailed` / `ResolveFailed` → auto soft-pause the hostname lane + notify alert events (`cookie_invalid` / `rate_limited` / `ytdlp_failed`; never auto-unmonitor or deactivate). Same codes from **stream proxy** play (`stream_play` task) also auto soft-pause. Video → `wanted_download_error` when the failing task is a download. Scan/list failures also immediately hold the source (`wanted` → `wanted_source_error`). Remux/pack/verify do not auto-pause. Other failed non-system domain tasks → alert `ytdlp_failed`. PO token provider problems while yt-dlp continues → warning `pot_provider` (no soft-pause, task not failed). Every event is always recorded in-app (alerts/warnings unread until read/Apprise OK; digests are info); Apprise is optional fan-out.

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

Downloads/imports set Creatorr-owned columns (`tool`, `download_format_selector`, `download_remux_container` when remux ran, `import_src`, duration/resolution/fps, stream fields). Site-specific detail stays in packed **`info.json`** (opaque - Creatorr never edits its content; copy/move/rename only; write/replace only when media changes). No sticky `handler_id` on sources/videos.

## Entry `upload_date`

RFC3339 UTC only (full timestamp). Same rules as before for cutoff / season / episode / NFO `<aired>` (UTC calendar day).

**List order:** newest-first (yt-dlp flat playlist / plugin extractors must match).

**Channel URLs:** yt-dlp `--flat-playlist` on a channel root returns tab playlists (Videos / Live / Shorts) with videos nested inside. Creatorr expands the **Videos** tab when listing; use a `/videos` URL (or a playlist URL) when you want that catalog explicitly. Metadata prefetch still uses the channel root for art/title.
