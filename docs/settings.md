# Settings (SQLite)

Index: [README.md](README.md). Terminology: [`AGENTS.md`](../AGENTS.md).

Most knobs are Settings keys (UI / SQLite). Process bootstrap env is only `CREATORR_PORT`, `CREATORR_PUBLIC_BASE_URL`, `CREATORR_POT_PROVIDER_URL`, `CREATORR_FLARESOLVERR_URL`, `CREATORR_WEB_*`, `TZ` (see [`config.Load`](../internal/config/config.go)). Paths are fixed (container `/data`, `/cache`, `/media/...`, `/yt-dlp-plugins`, baked POT plugin under `/usr/local/share/yt-dlp-plugins/bgutil`; local `var/...`). HTTP bind is always `0.0.0.0` (not configurable). yt-dlp binary is `/usr/local/bin/yt-dlp` (Docker) or `PATH`; plugins: see [`ytdlp.md`](ytdlp.md).

**External service URLs (env only, not Settings):** `CREATORR_FLARESOLVERR_URL` and `CREATORR_POT_PROVIDER_URL`. Settings → General shows both as disabled URL inputs with a colored status icon beside each with a one-shot health probe on page load. Empty Flare URL skips the health probe and FlareSolverr pre-solve, and clears Use FlareSolverr flags on boot (defaults off; host On → inherit). Compose defaults `http://creatorr-flaresolverr:8191` / `http://creatorr-po-token:4416`. Flare sidecar uses headless Chrome (notable RAM).

Editable settings (examples):

| Key | Role |
|---|---|
| `pot_fetch` | **PO token fetch** (PO = proof-of-origin): `auto` (default) / `always` / `never` → yt-dlp `youtube:fetch_pot`. Settings → General. Control disabled until env `CREATORR_POT_PROVIDER_URL` is set (Compose default `http://creatorr-po-token:4416`). When URL unset, invokes force `never`. |
| `episode_format` | Relative path under the series folder for packed episodes (default `S{year}/S{year}E{episode:000000} [{id}]`; `{year}` = UTC year-season, `{episode}` = `MMDD` + same-day index, default path zero-pads to 6 digits). Series folder is always `SeriesDir` (sanitized title, no rune cap). `/` separates folders under that series. Also `{date}`, `{domain}`, `{title}`, optional `{series}` / `{series:N}` in the stem. `{series:N}` / `{title:N}` cap runes (bare is not truncated). Saving does not rename - use Apply episode format (Settings → Maintenance). |
| `download_wanted_cron` | Schedule to enqueue wanted videos for monitored series. Settings → Scheduler. Seed `@hourly`. Empty = off |
| `download_new_on_scan` | Queues new videos after a scan until the download queue is full; Download wanted schedule fills gaps. Does not apply to full scan. Default on. Settings → Scheduler |
| `download_wanted_order` | Within each series: `oldest` (default) or `newest` by upload date (undated by id). Series are always fair-shared (round-robin, least-loaded first). Settings → Queue |
| `sync_files_cron` | Schedule for library file sync: missing/restore, packed media + sidecar size vs DB, beginning-cache reconcile. Settings → Scheduler. Seed `@daily`. Empty = off. Cron does not enqueue when there are no videos |
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

**Domain limits:** stored on `domains` row `domain=default` (non-NULL cooldown, max download queue, max parallel tasks, download rate, stream play rate, sleep, Use FlareSolverr; optional cookies). Host overrides are other `domains` rows (NULL = inherit). Soft **Pause** is separate (`domain_runtime`; Tasks / API). Settings → Queue (defaults form + overrides list without Active column + shared Edit modal; also from Tasks). Hostname on Add must look like a DNS name (rejects `example,com`, bare labels, URLs). Settings → Domains redirects to Queue. No auto-create on source add. `max_parallel_tasks` must be ≤ `max_download_queue`. Pace: `download_rate_limit` + `sleep_requests` for archive/scan/`cache_beginning`; `stream_play_rate_limit` for live play mux/pipe only (default Unlimited; no sleep on play).

**Notifications:** Settings → General lists a fixed **Creatorr** in-app channel (`creatorr://in-app`, all events, not editable) plus optional Apprise rows in `notify_channels`. `SendEvent` delivers through the same channel list: in-app inserts `notifications`; Apprise URLs fan out. Empty Apprise list = external off; in-app still records. No migrate from legacy `notify_urls`. URL construction: [Apprise supported services](https://appriseit.com/services/). Events per channel (canonical ids; read-time aliases `download_failed`→`ytdlp_failed`, `downloads_done`→`download_digest`):

| Event | Kind | When |
|---|---|---|
| `cookie_invalid` | alert | Cookie/auth failure on a non-system domain task |
| `rate_limited` | alert | Rate limit / IP block |
| `ytdlp_failed` | alert | Any other failed non-system domain task (scan, prefetch, download, stream pack, cache beginning, remux/pack on download, …) |
| `verify_failed` | alert | Post-pack media verify failed (file kept; status `verify_failed`) |
| `file_sync_issues` | alert | End-of-pass digest from `sync_files`: newly missing media/sidecars and/or size mismatches (media → status `verify_failed`; sidecars keep video status; no auto re-download) |
| `pot_provider` | warning | PO token plugin/sidecar problem while yt-dlp continued (task not failed for this alone) |
| `download_digest` | info | Global digest after all media tasks drain and no eligible wanted remain (archive + stream, beginning cached or not) |

**Alerts** and **warnings** (`AlertEvents` / `WarningEvents` in code; both unread-eligible) stay unread in-app until the detail page is opened, marked read (API / mark-all), or any Apprise send succeeds (`external_ok`); they require a `task_id`. **Info** digests are stored already-read with no `task_id`. Alert/warning rows always link the task. UI: History → Notifications (bell = info, amber triangle = warning, red megaphone = alert); detail `/notification/{id}` (opening marks unread events read); top-nav bell dropdown **Unread alerts** + badge for unread alert/warning count. Test button sends to that channel’s URL without requiring an event subscription (not logged in-app).

**Stats sampling** lives in `internal/stats` only - fixed every-minute poll (change-only writes) plus daily library-size sample; do not fold into the main scheduler. Chart JSON forward-fills across change timestamps and appends a synthetic tip at request time (current minute for queue/library charts, current UTC day for storage) so the series reaches "now" without writing an unchanged sample. Live library size pies (`GET /stats/library-size.json?group=root|series`) are not sampled. Storage development chart uses daily samples (display capped at 1 year).

Scan interval lives on each **feed source** (`sources.scan_cron`), not in Settings.

Every settings UI control has help text. See [ui.md](ui.md) § Setting descriptions.

## Library bootstrap (empty tables only)

| Seed | Values |
|---|---|
| Root folder | name = last segment of absolute path from seeded library root (`/media/library` in container; `var/media/library` local), no retention TTL |
| Quality profiles | `best` (format `bv*+ba`, strict highest merge, default), `1080p` (`bv*[height<=1080]+ba/b[height<=1080]/bv*+ba/b`), `720p` (`bv*[height<=720]+ba/b[height<=720]/bv*+ba/b`). Height profiles keep soft unrestricted tails. Bare yt-dlp `best` is avoided (soft progressive on DASH sites). Remux is always MKV. |

Import folder is `/media/import` (container) or `var/media/import` (local Go); not a Setting. Import UI scans the selected folder once on page load (default = inbox); changing Scan folder re-runs. Scope: inbox only, or one library root for **unmanaged orphans** under that online root (no `files` row). Confirm binds inbox via move, library via in-place. Sibling/orphan `.nfo` applies editable episode metadata then regenerates the library NFO from DB (source XML not kept) only when a same-basename video is beside it; alone it is listed only. Orphan thumb also requires a same-basename video beside it (else listed only). Orphan `info.json` is not attachable. `.strm` is not importable (regenerate with current External Creatorr URL). UI lists one item per stem group (media + same-directory same-stem sidecars; stem group key is case-insensitive and strips `[id]` brackets so `S2026E031700 [id].nfo` + `s2026e031700-thumb.jpg` share one Attach row); orphan sidecar stems without media stay one Attach item.
