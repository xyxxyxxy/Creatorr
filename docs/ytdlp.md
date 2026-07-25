# yt-dlp and plugins

Index: [README.md](README.md). Terminology: [`AGENTS.md`](../AGENTS.md).

Creatorr invokes **yt-dlp in-process** (`internal/ytdlp`). There is no external domain-handler registry or CLI protocol.

## Binary

| Path | Role |
| --- | --- |
| `/usr/local/bin/yt-dlp` | Runtime binary in the Docker image (build-time latest release). Creatorr prefers this path, then falls back to `yt-dlp` on `PATH` (local Go). |
| `/yt-dlp-plugins` (container) or `var/yt-dlp-plugins` (local) | Always passed as `--plugin-dirs` (subdirs with a `yt_dlp_plugins` package are included). Independent of the binary path. |

There is **no** in-app yt-dlp update schedule. Bump yt-dlp by rebuilding the image. Boot runs the resolved binary with `--version` and exits if missing or broken.

**Custom yt-dlp (Docker):** bind-mount over the image path:

```yaml
volumes:
  - /path/to/your/yt-dlp:/usr/local/bin/yt-dlp:ro
```

Plugins keep working under `/yt-dlp-plugins` (`--plugin-dirs`).

Image also ships **ffmpeg** (remux) and **Deno** (yt-dlp EJS challenge solver). FlareSolverr is optional external (Settings URL + Domain defaults / per-host On override).

## What Creatorr owns

- FlareSolverr pre-solve when effective **Use FlareSolverr** is on for the host (Domain defaults or host On override) and Settings `flare_solverr_url` is set. UI/API refuse enabling Use FlareSolverr / host On without a URL; clearing the URL turns those flags off.
- Netscape cookie jars (Settings → Queue; host jar, else `default`).
- Pace flags from domain limits: `--limit-rate`; when `sleep_requests` > 0 also `--sleep-requests`, `--sleep-subtitles`, and `--sleep-interval` (same seconds; no `--max-sleep-interval`).
- Subtitle sidecars from Library settings (`subtitle_langs` / `subtitle_auto`): `--write-subs` + `--convert-subs srt` (never `--embed-subs`). Used on archive download, stream `pack_stream`, and Replace sidecars. Empty langs = off. Auto-only tracks are renamed to `.lang.auto.srt` after download.
- Series `auto_ignore_media_types` → download `--match-filters` (`media_type!=…`); reject → `MediaTypeExcluded` (ignored, not download error). Stream `pack_stream` uses the same list against `media_type` from the `urls` extract (ignore before `.strm` / `cache_beginning`).
- Remux (ffmpeg → MKV) and pack after download.
- Stream proxy: resolve URLs (`progressive` / `pipe` / `hls`), HLS mux, beginning-cache.

`CookieInvalid` / `RateLimited` / `DownloadFailed` / `ResolveFailed` → auto soft-pause the hostname lane + notify alert events (`cookie_invalid` / `rate_limited` / `ytdlp_failed`; never auto-unmonitor or deactivate). Same codes from **stream proxy** play (`stream_play` task) also auto soft-pause. Video → `wanted_download_error` when the failing task is a download. Scan/list failures also immediately hold the source (`wanted` → `wanted_source_error`). Remux/pack/verify do not auto-pause. Other failed non-system domain tasks → alert `ytdlp_failed`. Every event is always recorded in-app (alerts unread until read/Apprise OK; digests are info); Apprise is optional fan-out.

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
