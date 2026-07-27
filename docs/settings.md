# Settings (SQLite)

Index: [README.md](README.md). Terminology: [`AGENTS.md`](../AGENTS.md).

Most knobs are Settings keys (UI / SQLite). Process bootstrap env is only `CREATORR_PORT`, `CREATORR_POT_PROVIDER_URL`, `CREATORR_FLARESOLVERR_URL`, `CREATORR_WEB_*`, `TZ` (see [`config.Load`](../internal/config/config.go)). Paths are fixed (container `/data`, `/cache`, `/media/...`, `/yt-dlp-plugins`, baked POT plugin under `/usr/local/share/yt-dlp-plugins/bgutil`; local `var/...`). HTTP bind is always `0.0.0.0` (not configurable). yt-dlp binary is `/usr/local/bin/yt-dlp` (Docker) or `PATH`; plugins: see [`ytdlp.md`](ytdlp.md).

**External service URLs (env only, not Settings):** `CREATORR_FLARESOLVERR_URL` and `CREATORR_POT_PROVIDER_URL`. Settings → General shows both as disabled URL inputs with a colored status icon beside each with a one-shot health probe on page load. Empty Flare URL skips the health probe and FlareSolverr pre-solve, and clears Use FlareSolverr flags on boot (defaults always off; enable On per host override). Compose defaults `http://creatorr-flaresolverr:8191` / `http://creatorr-po-token:4416`. Flare sidecar uses headless Chrome (notable RAM).

**Appearance (browser only):** Settings → General theme picker (Dark / Light / Special curated daisyUI themes). Stored in `localStorage` `creatorr-theme`; not a SQLite setting. OS default is `emerald` (light) / `dark` (dark) until the operator picks one.

Editable settings (examples):

| Key | Role |
|---|---|
| `pot_fetch` | **PO token fetch** (PO = proof-of-origin): `auto` (default) / `always` / `never` → yt-dlp `youtube:fetch_pot`. Settings → General. Control disabled until env `CREATORR_POT_PROVIDER_URL` is set (Compose default `http://creatorr-po-token:4416`). When URL unset, invokes force `never`. |
| `episode_format` | Relative path under the series folder for packed episodes (default `S{year}/S{year}E{episode} [{id}]`; `{year}` = UTC year-season, `{episode}` = `MMDD` + same-day index, zero-padded to 6 digits). Series folder is always `SeriesDir` (sanitized title, no rune cap). `/` separates folders under that series. Also `{date}`, `{domain}`, `{title}`, optional `{series}` / `{series:N}` in the stem. `{series:N}` / `{title:N}` cap runes. Saving does not rename - use Apply episode format (Settings → Maintenance). |
| `download_wanted_cron` | Schedule to enqueue wanted videos for monitored series. Settings → Scheduler. Seed `@hourly`. Empty = off |
| `download_wanted_order` | Which wanted videos to download first inside each series (`oldest` default / `newest` by upload date; no date uses id). Series take turns so one series does not fill the whole queue. Settings → Queue / Domains |
| `sync_files_cron` | Library scan will detect changed files in the root folders and cache directories. Settings → Scheduler. Seed `@daily`. Empty = off. Cron does not enqueue when there are no videos |
| `retention_delete_cron` | Deleting old data according to root folder retention (Settings → Library). Settings → Scheduler. Seed `@daily`. Empty = off. Cron does not enqueue when no root has a TTL |
| `stats_retention_days` | Stats sample retention dropdown: `90` (3 months), `365` (1 year, default), `-1` (forever). Minute metrics stored on change (polled every minute); library size sampled daily. Prune on each sample tick and immediately when this setting is saved (shorter drops older rows; forever never prunes). |
| `source_download_error_threshold` | When this many videos of a source enter an error state, other videos from that source are held until the issue is resolved. Set to 1 so the first error stops further downloads from that source. Integer ≥ 1. Default 2. Settings → Queue / Domains |
| `subtitle_langs` | JSON string array of yt-dlp `--sub-langs` tags (default `[]` = off). Settings → Library → Subtitles. Not retroactive (next download / metadata rescan / Refresh sidecars). |
| `subtitle_auto` | `1` = also `--write-auto-subs`; default `0`. Auto is used only when no custom track exists for that language (yt-dlp preference). Auto-only sidecars are packed as `.lang.auto.srt` (e.g. `.en.auto.srt`). Settings → Library → Subtitles. |
| `metadata_domain_tag` | `1` = prepend source domain to video tags on download and Metadata Save when `source_url` is known (default on). Locked in the video Metadata Tags editor. Settings → Library → Metadata. Not retroactive until next pack or Save. |
| `metadata_genres_from_categories` | `1` = add yt-dlp categories as video genres on download and Metadata Save when categories are known (default on). Locked rows in the video Metadata Genres editor. Settings → Library → Metadata. Not retroactive until next pack or Save. |

Sidecars are always converted to SRT via yt-dlp `--convert-subs srt` (no format setting).

**Quality profile maturity:** `maturity_redownload_hours` (0–168) and `maturity_sidecar_hours` (0–8760) on each quality profile (`0` = that pass off). Settings → Library → Quality profiles (sidecar field shown as days). Applies to every series on the profile. See [`download-and-library.md`](download-and-library.md).

**SponsorBlock (quality profile):** `sponsorblock_mark` / `sponsorblock_remove` (explicit category lists; must be disjoint), `sponsorblock_reencode_cut` (accurate cut via bitrate/codec-matched re-encode; default off = stream-copy keyframe snap), and `sponsorblock_info_cards` (requires reencode_cut; video delivery only - audio has no video track to draw a card on). Creatorr fetches [SponsorBlock](https://sponsor.ajay.app/) itself (never yt-dlp `--sponsorblock-*`). Cut ± re-encode ± cards, remapped chapters/subs, chapter embed in the packed container (creator timeline kept; SB marks additive). Attribution in profile UI and README.

**Media verify (quality profile):** `verify_media` (default off). After archive pack, optional system-lane null-decode. When `maturity_redownload_hours` > 0, only mature packs are verified automatically (young first packs skip until maturity re-download). Fail keeps the file and sets `verify_failed`.

**Domain limits:** stored on `domains` row `domain=default` (non-NULL cooldown, max download queue, max parallel tasks, download rate, sleep; **Use FlareSolverr always off** on defaults). Host overrides are other `domains` rows (NULL limit columns = inherit concurrency defaults). **Access** (FlareSolverr On/Off, Netscape cookies jar, membership credentials) is **override-only** - Domain defaults shows an info blurb only; Save defaults clears any legacy default jar/credentials and forces Flare off. Credentials and cookies do not fall back from `default`. Soft **Pause** is separate (`domain_runtime`; Tasks / API). Settings → Queue / Domains (defaults form + overrides list without Active column + shared Edit modal with Access guides). Hostname on Add/Edit must look like a DNS name (rejects `example,com`, bare labels, URLs). Changing hostname on Edit upserts the posted host only (previous override row left as-is; overrides are soft lookups by hostname). Settings → Domains redirects to Queue / Domains. No auto-create on source add. `max_parallel_tasks` must be ≤ `max_download_queue`. Pace: `download_rate_limit` + `sleep_requests` for archive/scan. Interactive metadata prefetch (`prefetch_*`) ignores rate and sleep.

**Notifications:** Settings → General lists a fixed **Creatorr** in-app channel (`creatorr://in-app`, all events, not editable) plus optional Apprise rows in `notify_channels`. `SendEvent` delivers through the same channel list: in-app inserts `notifications`; Apprise URLs fan out. Empty Apprise list = external off; in-app still records. No migrate from legacy `notify_urls`. URL construction: [Apprise supported services](https://appriseit.com/services/). Events per channel (canonical ids; read-time aliases `download_failed`→`ytdlp_failed`, `downloads_done`→`download_digest`):

| Event | Kind | When |
|---|---|---|
| `cookie_invalid` | alert | Cookie/auth failure on a non-system domain task |
| `rate_limited` | alert | Rate limit / IP block |
| `ytdlp_failed` | alert | Any other failed non-system domain task (scan, prefetch, download, remux/pack on download, …) |
| `verify_failed` | alert | Post-pack media verify failed (file kept; status `verify_failed`) |
| `file_sync_issues` | alert | End-of-pass digest from `sync_files`: newly missing media/sidecars and/or size mismatches (media → status `verify_failed`; sidecars keep video status; no auto re-download) |
| `pot_provider` | warning | PO token plugin/sidecar problem while yt-dlp continued (task not failed for this alone) |
| `download_digest` | info | Global digest after all download tasks drain and no eligible wanted remain |
| `live_skipped` | info | Archive download soft-skipped because yt-dlp `is_live`; video stays `wanted`; notification `task_id` links to the finished task (video history `live_skipped`) |

Settings channel event checkboxes list events **alert → warning → info**, then by label within a level.

**Alerts** and **warnings** (`AlertEvents` / `WarningEvents` in code; both unread-eligible) stay unread in-app until History is opened (marks all read), the detail page is opened, marked read (API / mark-all), or any Apprise send succeeds (`external_ok`); they require a `task_id`. **Info** events are stored already-read; `download_digest` has no `task_id`, while `live_skipped` sets `task_id` so detail links the task. Alert/warning rows always link the task. UI: History → Notifications (bell = info, amber triangle = warning, red megaphone = alert); detail `/notification/{id}` (opening marks unread events read); top-nav bell dropdown **Unread alerts** + badge for unread alert/warning count. Test button sends to that channel’s URL without requiring an event subscription (not logged in-app).

**Stats sampling** lives in `internal/stats` only - fixed every-minute poll (change-only writes) plus daily library-size sample; do not fold into the main scheduler. Chart JSON forward-fills across change timestamps and appends a synthetic tip at request time (current minute for queue/library charts, current UTC day for storage) so the series reaches "now" without writing an unchanged sample. Live library size pies (`GET /stats/library-size.json?group=root|series`) are not sampled. Storage development chart uses daily samples (display capped at 1 year).

Scan interval lives on each **feed source** (`sources.scan_cron`), not in Settings.

Every settings UI control has help text. See [ui.md](ui.md) § Setting descriptions.

## Library bootstrap (empty tables only)

| Seed | Values |
|---|---|
| Root folder | name = last segment of absolute path from seeded library root (`/media/library` in container; `var/media/library` local); operator-added roots may leave name empty. No retention TTL |
| Quality profiles | `best` (format `bv*+ba/b`, default), `1080p` (`bv*[height<=1080]+ba/b[height<=1080]/bv*+ba/b`), `720p` (`bv*[height<=720]+ba/b[height<=720]/bv*+ba/b`), `480p` (`bv*[height<=480]+ba/b[height<=480]/bv*+ba/b`). Height profiles keep soft unrestricted tails. Bare yt-dlp `best` alone is avoided as the primary selector (soft progressive on DASH sites). Remux is always MKV. |

Import folder is `/media/import` (container) or `var/media/import` (local Go); not a Setting. Import UI scans the selected folder once on page load (default = inbox); changing Scan folder re-runs. Scope: inbox only, or one library root for **unmanaged orphans** under that online root (no `files` row). Confirm binds inbox via move, library via in-place. Inbox subtitle/thumb Attach moves the file beside the packed media (subs keep language suffix, e.g. `test.en.srt` → `{episode}.en.srt`); library orphan sidecars must already sit beside the media. Sibling/orphan `.nfo` applies editable episode metadata then regenerates the library NFO from DB (source XML not kept) only when a same-basename video is beside it; alone it is listed only. Orphan thumb also requires a same-basename video beside it (else listed only). Orphan `info.json` is not attachable. UI lists one item per stem group (media + same-directory same-stem sidecars; stem group key is case-insensitive and strips `[id]` brackets so `S2026E031700 [id].nfo` + `s2026e031700-thumb.jpg` share one Attach row); orphan sidecar stems without media stay one Attach item.
