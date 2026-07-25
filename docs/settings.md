# Settings (SQLite)

Index: [README.md](README.md). Terminology: [`AGENTS.md`](../AGENTS.md).

Most knobs are Settings keys (UI / SQLite). Process bootstrap env is only `CREATORR_PORT`, `CREATORR_PUBLIC_BASE_URL`, `CREATORR_WEB_*`, `TZ` (see [`config.Load`](../internal/config/config.go)). Paths are fixed (container `/data`, `/cache`, `/media/...`, `/yt-dlp-plugins`; local `var/...`). HTTP bind is always `0.0.0.0` (not configurable). yt-dlp binary is `/usr/local/bin/yt-dlp` (Docker) or `PATH`; plugins live under `/yt-dlp-plugins` - see [`ytdlp.md`](ytdlp.md).

Editable settings (examples):

| Key | Role |
|---|---|
| `flare_solverr_url` | Required for domains behind CloudFlare. Set URL here before enabling **Use FlareSolverr** on Domain defaults and/or per-host On override (Settings → Queue). Empty = skip health probe, never pass `--flaresolverr`, and clears Use FlareSolverr flags (defaults off; host On → inherit) |
| `episode_format` | Relative path under the series folder for packed episodes (default `S{year}/S{year}E{episode:000000} [{id}]`; `{year}` = UTC year-season, `{episode}` = `MMDD` + same-day index, default path zero-pads to 6 digits). Series folder is always `SeriesDir` (sanitized title, no rune cap). `/` separates folders under that series. Also `{date}`, `{domain}`, `{title}`, optional `{series}` / `{series:N}` in the stem. `{series:N}` / `{title:N}` cap runes (bare is not truncated). Saving does not rename - use Apply episode format (Maintenance). |
| `download_wanted_cron` | Schedule to enqueue wanted videos for monitored series. Settings → Scheduler. Seed `@hourly`. Empty = off |
| `download_new_on_scan` | Queues new videos after a scan until the download queue is full; Download wanted schedule fills gaps. Does not apply to full scan. Default on. Settings → Scheduler |
| `download_wanted_order` | Within each series: `oldest` (default) or `newest` by upload date (undated by id). Series are always fair-shared (round-robin, least-loaded first). Settings → Queue |
| `sync_files_cron` | Schedule for detecting external file deletions (and beginning-cache reconcile). Settings → Scheduler. Seed `@daily`. Empty = off. Cron does not enqueue when there are no videos |
| `retention_delete_cron` | Schedule for deleting files past root retention TTL. Settings → Scheduler. Seed `@daily`. Empty = off. Cron does not enqueue when no root has a TTL |
| `stats_retention_days` | Stats sample retention dropdown: `90` (3 months), `365` (1 year, default), `-1` (forever). Minute metrics stored on change (polled every minute); library size sampled daily. Prune on each sample tick and immediately when this setting is saved (shorter drops older rows; forever never prunes). |
| `source_download_error_threshold` | When this many videos on a source are `wanted_download_error`, hold siblings as `wanted_source_error`. Integer ≥ 1 (1 = hold after the first error). Default 2. Settings → Queue |
| `cache_beginning_seconds` | Seconds of stream beginning to cache after pack for pipe streams (0 = off, default 20). Settings → Library → Streaming. Editable when External Creatorr URL is set. |
| `stream_playback_cache` | **Build cache on playback** (default on). Enables later plays to stream without re-fetching via yt-dlp. Settings → Library → Streaming. |
| `stream_playback_cache_max_hours` | **Max playback cache** - rolling total hours of playback cache kept (10–100, step 10, default 20). Over budget evicts LRU whole-video playback caches. Settings → Library → Streaming. |
| `external_base_url` | **External Creatorr URL** - absolute origin media clients use for `.strm` playback through Creatorr (scheme+host+port, no trailing slash). Essential for streaming; empty disables stream delivery. Changing it requires Regenerate all .strm files. Settings → Library → Streaming. Optional one-shot seed from env `CREATORR_PUBLIC_BASE_URL` when Settings empty. |
| `subtitle_langs` | JSON string array of yt-dlp `--sub-langs` tags (default `[]` = off). Settings → Library → Subtitles. Not retroactive (next download / pack_stream / metadata rescan / Refresh sidecars). |
| `subtitle_auto` | `1` = also `--write-auto-subs`; default `0`. Auto is used only when no custom track exists for that language (yt-dlp preference). Auto-only sidecars are packed as `.lang.auto.srt` (e.g. `.en.auto.srt`). Settings → Library → Subtitles. |

Sidecars are always converted to SRT via yt-dlp `--convert-subs srt` (no format setting).

**Quality profile maturity:** `maturity_redownload_hours` (0–168) and `maturity_sidecar_hours` (0–8760) on each quality profile (`0` = that pass off). Settings → Library → Quality profiles (sidecar field shown as days). Applies to every series on the profile. See [`download-and-library.md`](download-and-library.md).

**SponsorBlock (quality profile):** `sponsorblock_mark` / `sponsorblock_remove` (explicit category lists; must be disjoint), `sponsorblock_reencode_cut` (accurate cut via bitrate/codec-matched re-encode; default off = stream-copy keyframe snap), and `sponsorblock_info_cards` (requires reencode_cut). Creatorr fetches [SponsorBlock](https://sponsor.ajay.app/) itself (never yt-dlp `--sponsorblock-*`). Archive: cut ± re-encode ± cards, remapped chapters/subs, MKV chapter embed (creator timeline kept; SB marks additive). Stream: `.sponsorblock.json` play plan beside `.strm`, duration pad, skip-aware download beginning; cards flag only when reencode_cut is on. Attribution in profile UI and README.

**Media verify (quality profile):** `verify_media` (default off). After archive pack, optional system-lane null-decode. When `maturity_redownload_hours` > 0, only mature packs are verified automatically (young first packs skip until maturity re-download). Fail keeps the file and sets `verify_failed`.

**Domain limits:** stored on `domains` row `domain=default` (non-NULL cooldown, max download queue, max parallel tasks, rate/sleep/Use FlareSolverr; optional cookies). Host overrides are other `domains` rows (NULL = inherit). Soft **Pause** is separate (`domain_runtime`; Tasks / API). Settings → Queue (defaults form + overrides list without Active column + shared Edit modal; also from Tasks). Hostname on Add must look like a DNS name (rejects `example,com`, bare labels, URLs). Settings → Domains redirects to Queue. No auto-create on source add. `max_parallel_tasks` must be ≤ `max_download_queue`.

**Notifications:** Settings → General lists a fixed **Creatorr** in-app channel (`creatorr://in-app`, all events, not editable) plus optional Apprise rows in `notify_channels`. `SendEvent` delivers through the same channel list: in-app inserts `notifications`; Apprise URLs fan out. Empty Apprise list = external off; in-app still records. No migrate from legacy `notify_urls`. URL construction: [Apprise supported services](https://appriseit.com/services/). Events per channel (canonical ids; read-time aliases `download_failed`→`ytdlp_failed`, `downloads_done`→`download_digest`):

| Event | Kind | When |
|---|---|---|
| `cookie_invalid` | alert | Cookie/auth failure on a non-system domain task |
| `rate_limited` | alert | Rate limit / IP block |
| `ytdlp_failed` | alert | Any other failed non-system domain task (scan, prefetch, download, stream pack, cache beginning, remux/pack on download, …) |
| `verify_failed` | alert | Post-pack media verify failed (file kept; status `verify_failed`) |
| `download_digest` | info | Global digest after all media tasks drain and no eligible wanted remain (archive + stream, beginning cached or not) |

**Alerts** (`AlertEvents` in code: the four alert rows above) stay unread in-app until the detail page is opened, marked read (API / mark-all), or any Apprise send succeeds (`external_ok`); they require a `task_id`. **Info** digests are stored already-read with no `task_id`. Alert rows always link the failed task. UI: History → Notifications (bell = info, red megaphone = alert); detail `/notification/{id}` (opening marks alert read); top-nav bell dropdown **Unread alerts** + badge for unread alert count. Test button sends to that channel’s URL without requiring an event subscription (not logged in-app).

**Stats sampling** lives in `internal/stats` only - fixed every-minute poll (change-only writes) plus daily library-size sample; do not fold into the main scheduler. Chart JSON forward-fills across change timestamps and appends a synthetic tip at request time (current minute for queue/library charts, current UTC day for storage) so the series reaches "now" without writing an unchanged sample. Live library size pies (`GET /stats/library-size.json?group=root|series`) are not sampled. Storage development chart uses daily samples (display capped at 1 year).

Scan interval lives on each **feed source** (`sources.scan_cron`), not in Settings.

Every settings UI control has help text. See [ui.md](ui.md) § Setting descriptions.

## Library bootstrap (empty tables only)

| Seed | Values |
|---|---|
| Root folder | name = last segment of absolute path from seeded library root (`/media/library` in container; `var/media/library` local), no retention TTL |
| Quality profiles | `best` (format `bv*+ba/b`, default), `1080p` (`bv*[height<=1080]+ba/b[height<=1080]/bv*+ba/b`), `720p` (`bv*[height<=720]+ba/b[height<=720]/bv*+ba/b`). Bare yt-dlp `best` is avoided (soft progressive on DASH sites). Remux is always MKV. |

Import folder is `/media/import` (container) or `var/media/import` (local Go); not a Setting. Import scan also lists **library orphans** (media under online roots with no `files` row). Confirm binds inbox via move, library via in-place. UI groups media with same-directory same-stem sidecars into one row; orphan sidecar stems without media stay one Attach row.
