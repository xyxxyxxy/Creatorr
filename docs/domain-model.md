# Domain model (fresh SQLite schema version 1)

Index: [README.md](README.md). Agent contract: [`AGENTS.md`](../AGENTS.md). Stream delivery terms: [stream-proxy.md](stream-proxy.md). yt-dlp/plugins: [ytdlp.md](ytdlp.md). Scan/queue: [scan-and-queue.md](scan-and-queue.md). Download/library: [download-and-library.md](download-and-library.md).

## Terminology

Use these terms consistently in code comments, UI copy, OpenAPI, tests, and docs. **Naming:** prefer **task** for queued work; **job** = implicit recurring schedule (no `jobs` table); do not use **poll** - use **scan**.

| Term | Meaning |
| --- | --- |
| **Series** | One title, one root folder, one quality profile, **delivery mode** (`download` \| `stream`), **monitored** flag, sources, video **index**, optional show metadata (`tvshow.nfo` + art). |
| **Source** | URL on a series. **Kind** `feed` (full scan then tip Scan) or `single` (index once). Feed: `scan_cron`, optional **Mark new videos as ignored** (`index_as_ignored`), optional **title include/exclude** (`title_regexp_include` / `title_regexp_exclude`), optional **scan cutoff**. Singles: no title filters / mark-new-as-ignored UI (always index as wanted unless series auto-ignore media types apply). No per-source monitored. |
| **Feed / single** | Source kinds. Feed = recurring tip Scan; single = one-shot index (no tip Scan after first index). |
| **Video** | Indexed creator content in a series (not “item”). Status + metadata + optional files + **video history**. On disk after pack it is treated as an **episode** (below). |
| **Episode** | Library / media-server view of a packed **video**: season/episode numbers, episode filename stem, episode NFO (`episodedetails`). Created at **pack** from a video; episode format setting (`episode_format`; relative under the series folder; tokens include `{year}`, `{title:N}`, `{episode}`, `{month}`, `{day}`, `{date}`, `{domain}`, optional `{series}`) applies to on-disk episodes, not to the DB video title shown in the UI. |
| **Video history** | Per-video lifecycle timeline (`video_history`): download/remux/pack/file events; required `task_id`. List-pass discover/update is **not** stored here - projected from **source history** onto video detail History. |
| **Source history** | Per-source list-pass timeline (`source_history`): `scanned` / `scan_error` / `cancelled` with `mode` + id arrays; required `task_id`. Sticky status and tip due use `scanned` / `scan_error` only. |
| **Index** | Videos belonging to a series, built by scanning sources. |
| **Monitored** | Series-only flag. Gates tip Scan + auto download-wanted / stream pack (with domain **active**). Full scan does **not** require monitored. Never auto-toggled. |
| **Mark new videos as ignored** | UI for `index_as_ignored` on **feeds**: new videos start `ignored` until manually set to wanted. Not offered for singles (always start wanted). |
| **Title include / exclude** | Optional feed-source Go regexps (`title_regexp_include`, `title_regexp_exclude`). Index only when include matches (if set) **and** exclude does not match (if set). **Exclude wins** when both match (not indexed). Tip scan walks past skips without treating them as known. Empty = no filter. UI hidden for singles. |
| **Auto ignore filters** | Series `auto_ignore_media_types` (JSON string array of yt-dlp `media_type` values). Videos are **indexed**, then marked **`ignored`** when type is known and listed (create-time if list has type; download `--match-filters` or stream `pack_stream` `urls` extract when type appears then). Empty = none. Missing/empty media type is never auto-ignored. |
| **Scan cutoff** | Optional source `scan_cutoff` (YYYY-MM-DD UTC). Full scan and tip Scan stop at the first listing **before** that day; the cutoff day is indexed; older days are not. Empty = walk full history (until known id on tip Scan). |
| **Active** | Domain flag (`domains.active`). Inactive → no queue claims + enqueue gates; deactivate cancels pending+running. API only (not on Settings → Queue overrides UI). |
| **Paused** | Soft claim stop (`domain_runtime`) for **ClaimNext** only. Pending stay queued; running continue; resume deletes runtime row. Interactive metadata prefetch (`prefetch_series_meta` / `prefetch_video_meta` / `prefetch_add_series` / `prefetch_add_video`) still claims. Never creates a `domains` override row. Operator control on Tasks; also set automatically on yt-dlp-facing task failures (`CookieInvalid` / `RateLimited` / `DownloadFailed` / `ResolveFailed`). |
| **wanted_download_error** / **wanted_source_error** | Download failure status / held-wanted when source error threshold hit. Source **Retry** → `wanted`. |
| **Root folder** / **Quality profile** | Named absolute path (+ optional retention TTL) / named yt-dlp `--format` selector plus optional maturity delays and optional SponsorBlock mark/remove/reencode/info-cards. Remux always MKV. |
| **Series warn** | Virtual UI health (not a column): `incomplete` (full scan stalled with **no** tip schedule) or `error` (download/source errors - overwrites incomplete). Scheduled incomplete does not escalate (tip cron continues full scan). Shown via series status indicator. |
| **Job** / **Task** / **Queue** | Implicit schedule only / one domain-queue unit / per-domain ordered pending+running. |
| **Domain** | Hostname from source URLs: optional limit overrides, FlareSolverr (inherit/on/off), cookies; soft **Paused** via `domain_runtime`. Never auto-deleted. |
| **History** | `/history` UI: shared UTC **From**/**To** range, then **Notifications** (in-app `notifications` log) above **Tasks** (finished tasks). Task rows link to **Task** detail (`/task/{id}`). Notification rows link to `/notification/{id}`. Outcome JSON in `tasks.detail`. Task statuses `done` / `failed` / `cancelled`. Cancelled ≠ download error. |
| **Notification** | In-app row for every notify event (`notifications`), delivered by the fixed Creatorr channel. **Alert** events (`cookie_invalid` / `rate_limited` / `ytdlp_failed` / `verify_failed` / `file_sync_issues`) stay unread until detail open, mark-read, or any Apprise success and require `task_id`. **Info** digests (`download_digest`) are stored read with no `task_id`. Apprise channels are optional fan-out. |
| **Task detail** | `/task/{id}` for any status (pending/running/finished). Live pages SSE-patch status/message/progress; Logs panel (in-memory progress lines + Refresh) only while pending/running. Scan `created_ids` rows show scan-time state (`wanted`\|`ignored`) and optional ignore reason. `skipped_title_regexp_include` / `skipped_title_regexp_exclude` list titles not indexed by title filters. |
| **Scan** | Index from a source (**Full scan** or tip **Scan**). Index-only - see [scan-and-queue.md](scan-and-queue.md). |
| **Download** / **Download wanted** | Fetch→remux→pack task / cron enqueue for `wanted` on monitored download-mode series - see [download-and-library.md](download-and-library.md). |
| **Pack** / **Import** / **Retention** / **File sync** | Turn a **video** into an on-disk **episode** under the root (TV path + NFO + optional sidecars) / inbox or per-root library orphan bind / root TTL purge (`retention_delete`) / missing·restore·size-mismatch (media + sidecars)·beginning-cache pass (`sync_files`). |
| **Wanted** / **Ignored** / **Missing** / **Deleted** | Statuses: eligible / not auto-downloaded / path gone (recoverable) / intentional remove (Import or Want to recover). |
| **Metadata rescan** | Refresh metadata for existing videos only (no discovery). |
| **Cookies** / **Settings** | Netscape jar per domain (`default` fallback) / SQLite runtime config (env seeds first boot). |
| **Source URL** | `videos.source_url` - watch/clip page only (never feed URL). |
| **Metadata suggestion pool** | Library-wide datalist values per field name (studio, genres, tags, country, mpaa, actor name, actor role) from `series` ∪ `videos`. Shared by series and video Metadata forms - see [download-and-library.md](download-and-library.md). |
| **Video columns** / **Acquired** / **info.json** / **File size** / **Upload time** | Creatorr-owned packed fields; episode meta columns (plot=`description`, sorttitle, …) feed episode NFO; `acquired_at` on pack; `sidecars_acquired_at` when sidecars packed/refreshed; opaque yt-dlp sidecar written only with media (never edit content via Metadata editor; never rewrite on sidecar-only refresh); `files.size_bytes` for video kind; `upload_date` RFC3339 UTC. |

Stream delivery (**delivery mode**, **stream pack**, **stream proxy**, **streamable**, **download beginning**): [stream-proxy.md](stream-proxy.md). **yt-dlp** / **plugins**: [ytdlp.md](ytdlp.md).

When introducing a new domain term, add it here (or the topic doc above if it belongs there).

## Core entities

| Entity | Purpose |
|---|---|
| `root_folders` | Named path + optional `retention_ttl_seconds` (UI: days; stored as seconds) |
| `quality_profiles` | Named `format_selector` (yt-dlp `-f` style; passed as `--format`) plus `maturity_redownload_hours` / `maturity_sidecar_hours` (`0` = off), plus `sponsorblock_mark` / `sponsorblock_remove` (JSON category lists), `sponsorblock_reencode_cut`, and `sponsorblock_info_cards` (cards require reencode). Remux is always MKV (Creatorr ffmpeg). |
| `series` | Title, one root folder, one quality profile, monitored, delivery_mode, `auto_ignore_media_types` |
| `sources` | URL, optional name (`label` column), `kind` (`feed`\|`single`), `scan_cron`, `index_as_ignored`, `title_regexp_include`, `title_regexp_exclude`, cutoff, `full_scan_done` (no sticky last_error / last_scanned_at - derived from `source_history`) |
| `source_history` | Per-source list-pass timeline: `scanned` / `scan_error` / `cancelled`; detail `mode` (`full`\|`scan`\|`rescan_metadata`) + counts/ids; required `task_id`; cascades on source delete |
| `videos` | Indexed video: source_id, title, status, season/episode, `source_url`, `acquired_at` / `sidecars_acquired_at` (set on pack; sidecar maturity updates latter only), duration/resolution/stream/download columns, `media_type`; `upload_date` stored as RFC3339 UTC |
| `video_history` | Per-video lifecycle timeline (download/remux/pack/file/…); required `task_id`. Not used for list-pass discover/update |
| `files` | On-disk paths linked to videos: kind (video/nfo/thumb) |
| `tasks` | Queued background work; finished rows power History → Tasks (`status` + optional `detail` outcome JSON) |
| `notifications` | In-app notify log (event, title, body, optional `task_id`, `external_ok`, `read_at`) |
| `cookies` | Netscape jar text per domain key (`default` = fallback for all hosts) |
| `domain_runtime` | Soft pause per hostname (`paused`); missing row = not paused; never a limits override |
| `domains` | Hostname profiles: `default` (global limits + Use FlareSolverr) + host overrides; `active`, optional limit overrides and `use_flaresolverr` (NULL = inherit `default`; 0/1 = Off/On), FlareSolverr HTTP pre-solve when effective on |
| `settings` | Key/value runtime config |

## Video statuses (minimum)

- `wanted` - eligible for download when parents allow (no file yet, or after Want from deleted/ignored/missing)
- `wanted_source_error` - still wanted, but source has too many `wanted_download_error` videos (auto-download held)
- `wanted_download_error` - last download failed; counts toward the per-source error threshold
- `downloaded` - file present on disk
- `verify_failed` - packed media failed post-pack null-decode verify; **file kept**; does not count toward `source_download_error_threshold`. **Want** → `wanted`; **Download now** re-downloads. File sync treats like `downloaded` (path gone → `missing`; path back → `downloaded`). Not eligible for media maturity while in this status.
- `missing` - path still in DB but media not found; file sync may restore to `downloaded` when the file returns. Roots whose path is offline are skipped (no mass-missing on unmounted volume).
- `deleted` - files intentionally gone (retention or user delete); index kept; file rows cleared - re-download via Want
- `ignored` - user ignored, source **Mark new videos as ignored** (`index_as_ignored`), or series **auto ignore filters** (`media_type`) at index, download, or stream pack. Ignoring cancels pending **and** running download tasks for that video (running download asks for confirm in UI). Cancelled downloads appear in History with status **`cancelled`** (reason e.g. `Cancelled (video ignored)`).

**No video monitored flag.** Eligibility is status only (`wanted` vs `ignored` / `deleted` / `missing` / errors). Download-wanted for status `wanted` when series monitored ∧ domain active. **Download now** also accepts ignore/deleted/missing/error/`verify_failed` (sets `wanted`), bypasses the queue cap, and may run when series is unmonitored (domain active still required). **Want** sets `ignored` / `deleted` / `missing` / `verify_failed` → `wanted` without enqueueing.

## Upload time

`videos.upload_date` is always **RFC3339 UTC** in the database (full timestamp for same-day ordering). yt-dlp / plugins must send RFC3339 UTC; Creatorr does not normalize Unix / date-only on ingest. **`season`** is the UTC **calendar year** (year-season, e.g. 2026). **`episode`** is `MMDD` + 0-based same-day index (`MM*10000+DD*100+i`, e.g. 31500 for 15 Mar first that day). Same UTC day: sort by `upload_date` then `id` (arrival). An earlier same-day timestamp reindexes that day and repacks shifted packed files. Undated videos leave season/episode unset in the DB; pack and stream pack use year-season **0**, so `{year}` in `episode_format` renders as **`0000`** (e.g. `S0000`), not TV default `S1`. Cutoff comparisons use the UTC calendar day only.

When media disappears or is purged:

1. **File sync (path gone, root online):** keep file rows; set status **`missing`**; history `file_missing`. Applies to `downloaded` and `verify_failed`. If path returns later → **`downloaded`** + `file_restored`. Registered **sidecars** use the same keep-row / restore path with history `sidecar_missing` / `sidecar_restored` and `size_bytes = -1` while known missing; **video status is not changed** for sidecar-only loss. Sidecar size drift → `sidecar_externally_changed` (status unchanged).
2. **Retention / user delete:** delete artifacts; clear file rows; set status **`deleted`**; history `file_deleted` (reason `retention` | `manual`).
3. **Root offline** (path missing or not a directory): file sync and retention purge skip that root (neither missing nor restore nor retention).
4. **`deleted` is intentional.** File sync does **not** detect files put back on disk for a `deleted` video (no path kept after clear). Operator must **Import** (scan that library root finds the orphan → confirm bind) or Want + download - do not drop files straight into the library tree and expect auto-bind.

User **Want** → status **`wanted`**; download-wanted cron or manual download (no immediate enqueue). From **`verify_failed`**, Want clears the verify hold without deleting the file.

## Source history

Chronological list-pass outcomes per source (DB: `source_history`, required `task_id`):

| Event | When | Detail |
|---|---|---|
| `scanned` | List succeeded | `mode`: `full` \| `scan` \| `rescan_metadata`; `created` / `updated` counts; `created_ids` / `updated_ids`; optional `skipped_title_regexp_include` / `skipped_title_regexp_exclude`; optional `ignored_media_type_ids` / `ignored_index_as_ignored_ids` (subsets of `created_ids`); optional `hit_known` / `hit_cutoff` |
| `scan_error` | List/cookie failure | `mode` (same); `code` |
| `cancelled` | Scan task cancelled (pending or running) | `mode` from task payload; UI Event column shows `scan` |

Latest `scanned` \| `scan_error` → source sticky status (API `last_scanned_at` / `last_error_*`), series warn. `cancelled` is listed on source History but does **not** change sticky status or tip due. Tip Scan cron due uses `created_at` of latest `scanned` with `mode=scan` only (not full / rescan_metadata / cancelled). Source delete hard-deletes videos and cascades `source_history`.

## Video history

Lifecycle entries per video (DB: `video_history`, required `task_id`), e.g.:

- download finished (`downloaded`) when yt-dlp media is ready; library install (`packed` via `CompleteDownload`); failed (`download_failed` with detail `stage`: `fetch` | `remux` | `pack`, plus `code`). Video status stays `wanted_download_error`. Older rows may still use event `wanted_download_error` or pack event `download`.
- post-pack media verify ok (`verified`) or fail (`verify_failed` + status `verify_failed`; file kept; `MediaVerifyFailed`). Does not bump source error threshold.
- source held after threshold (`source_failed`); video status stays `wanted_source_error`. Older rows may still use event `wanted_source_error`.
- remuxed (only when ffmpeg remux actually ran during a download task)
- SponsorBlock cut applied (`sponsorblock_cut`) when remove/cut ran (download-inline or `sponsorblock_cut` task)
- stream pack (`stream_packed`); beginning cached (`beginning_cached`). Older rows may still use `stream_pack` / `download_beginning`.
- cancelled (`cancelled`) when a video-scoped task (`download` / `cache_beginning` / `pack_stream` / `sponsorblock_cut` / `media_verify` / video `rescan_metadata`) is cancelled; detail `kind`; message e.g. `Cancelled` or `Cancelled (video ignored)`. UI Event column shows `detail.kind` (not the literal `cancelled`). Cancelling `sponsorblock_cut` deletes staging under `{CacheDir}/sponsorblock-cut/{videoID}/`.
- import packed (`imported`); unmatched import row created (`import_created`). Older rows may still use `import` / `import_create`.
- import NFO applied to editable episode columns then library NFO regenerated (`nfo_applied`). Older rows may still use event `nfo_imported`.
- maturity media refresh (`maturity_repacked`); maturity sidecar refresh (`maturity_sidecars_refreshed`). Older rows may still use `maturity_redownload` / `maturity_sidecars`.
- file missing / restored (file sync)
- file externally changed / size mismatch (file sync → `verify_failed`)
- sidecar missing / restored / size mismatch (file sync; video status unchanged)
- sidecar deleted individually (`sidecar_deleted`; registered `sub` / `thumb` / `other` only; sync unlink + drop `files` row; bookkeeping task `delete_sidecar`)
- beginning cache missing / restored (file sync; streamable)
- file deleted (manual `delete_files` task or retention via `retention_delete`)
- episode NFO regenerated (`nfo_regenerated`; task kind `regenerate_nfo`; only when on-disk bytes changed). Older rows may still use event `nfo_regenerate`.
- episode renamed (Apply episode format / reindex - detail previous/new name)

**Not** written on list passes: `discovered` / `updated` / per-video `rescan_metadata`. Those appear on video detail History as **projections** from `source_history` where the video id is in `created_ids` or `updated_ids` (display event from `mode`). Older source_history modes may still use `metadata_rescan`.

Task-driven rows always set `video_history.task_id` (download, sync_files, import, rename_episodes, regenerate_nfo, delete_files, …). Manual video file delete enqueues **`delete_files`**; worker writes `file_deleted` with that task’s id. Per-sidecar Delete (`sub` / `thumb` / `other`) is sync: finished system bookkeeping task `delete_sidecar` + history `sidecar_deleted` (no status change). Retention delete links to the `retention_delete` task. Series purge with files removes disk then `DELETE series` (no lasting per-video history; the finished `delete_files` task in History is the durable record).

Global **History** (`/history`) shows Notifications (top) then Tasks (finished tasks `done` / `failed` / `cancelled`); task rows open **Task detail** (`/task/{id}`); notification rows open `/notification/{id}`. Optional outcome JSON in `tasks.detail`. Source detail History is paginated `source_history`. Video detail History merges `video_history` + projected source list-pass events.

## Task statuses

- `pending`, `running`, `done`, `failed`, `cancelled`

**Task detail** (`/task/{id}`) works for any status. History list shows only finished statuses (`done`, `failed`, `cancelled`). Operator cancel (Tasks per-task / per-domain Cancel pending / ignore; API `POST /api/tasks/cancel-all`) → task status **`cancelled`**. Video stays **`wanted`** (or **`ignored`** when ignore). Never `wanted_download_error`; does not count toward `source_download_error_threshold`. While a task runs, progress() status lines are kept in memory only (Logs panel + Refresh); panel is hidden after the task finishes; buffer cleared on finish/cancel or Creatorr restart. Live progress bars / radials show only for mid values in `(0, 1)`; nil, `0`, and `1` use the spinner.
