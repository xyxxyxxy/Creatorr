# Scan, jobs, and domain queue

Index: [README.md](README.md). Terminology: [`AGENTS.md`](../AGENTS.md).

**Naming:** **Full scan** = archive index (`mode=full`). **Scan** = tip catch-up (`mode=scan`). **History** = finished tasks / `video_history` (not a scan mode).

## Jobs vs tasks

**Jobs** are **implicit only** - intervals/flags on series, sources, and global settings. No `jobs` table. No Jobs UI. The DB stores **tasks** (domain queue) plus settings.

| Implicit schedule | Creates |
|---|---|
| Per-source Scan (`sources.scan_cron` + latest tip `source_history` `scanned` with `mode=scan`) | One **Scan** per due feed source (empty cron = no schedule). Tip Scan when `full_scan_done`; otherwise **full scan**. UI is free-form cron / `@hourly`…`@monthly`; empty = never (manual still OK). Process start does **not** catch up fires missed while down; waits for the next cron after boot. **Cron fields are UTC**; UI labels (`Describe` / Scan row) show the equivalent wall clock in process local time (`TZ` - see compose / `.env`) |
| Download wanted (global cron) | **Download tasks** for wanted videos. Same no-catch-up-on-boot rule as Scan |
| File sync (`sync_files_cron`) | Enqueues **`sync_files`** on the **`system`** lane when the library has videos (not inline). No catch-up on boot |
| Retention delete (`retention_delete_cron`) | Enqueues **`retention_delete`** on the **`system`** lane when any root has TTL (not inline). No catch-up on boot |

There is **no** global Settings `scan_cron`.

## System lane and interactive tasks

| Kind / path | Domain | Notes |
|---|---|---|
| `import` | `system` | Per-video duplicate guard |
| `sync_files` | `system` | One pending/running; cron or Maintenance → Scan for missing files; higher priority than Apply |
| `retention_delete` | `system` | One pending/running; cron enqueues at higher priority than Apply |
| `rename_episodes` | `system` | One pending/running; Settings → Maintenance → Apply episode format |
| `regenerate_nfo` | `system` | One pending/running; Settings → Library → Regenerate NFO; resumable cursors |
| `delete_files` | `system` | No duplicate reject; worker owns disk + DB; UI delete queues it |
| `delete_sidecar` | `system` | Sync bookkeeping only (InsertRunning + Finish); per-file `sub`/`thumb`/`other` delete; history `sidecar_deleted` |
| `sponsorblock_cut` | `system` | Per-video dup; **low priority** (`PrioritySponsorblockCut`); archive remove/cut + pack after download staging. Not one-per-kind (many may queue). |
| `media_verify` | `system` | Per-video dup; **lowest** system priority (`PriorityMediaVerify=-20`, below cut). Null-decode packed library media after pack when profile `verify_media` (mature-only gate when maturity hours set). Import confirm `verify` always enqueues. Fail → `verify_failed` (keep files). |
| `prefetch_series_meta` | fetch URL hostname | **Interactive:** ClaimInteractive + concurrent run; ignores soft Pause (and domain busy/cooldown); Finish does not start cooldown; still requires domain active; **no** `download_rate_limit` / `sleep_requests` |
| `prefetch_video_meta` | fetch URL hostname | **Interactive:** same as prefetch_series_meta; resolve into video Metadata modal draft (+ soft-download thumb into `cache/video-meta/{id}/` when URL present) |
| `prefetch_add_series` | fetch URL hostname | **Interactive:** Add series wizard fetch; draft under `cache/add-series/{token}/` (no series row yet); ignores soft Pause; Finish does not start cooldown; no rate/sleep |
| `prefetch_add_video` | fetch URL hostname | **Interactive:** Add video modal fetch; draft under `cache/add-video/{token}/` (no video row yet); ignores soft Pause; Finish does not start cooldown; no rate/sleep |

System maintenance runs **concurrent with hostname** work (worker goroutines after claim). The **`system` lane is always serial** (exactly one running task), hard-coded in claim - Settings `max_parallel_tasks` and `task_cooldown_seconds` do not apply to `system` (no cooldown between system tasks). Tasks page always shows the `system` lane (pinned first) plus a lane for every known host (domains rows, source URL hostnames, and soft-paused hosts from `domain_runtime`), including empty queues.

## Gradual scan

Flat listing only (newest-first). No full metadata extract during scan. YouTube **channel roots** list the Videos tab (see [`ytdlp.md`](ytdlp.md)); plain playlists list their entries as-is.

**UI video order:** dated videos by `upload_date` DESC; undated after that by `id` DESC.

| Mode | When | Behavior |
|---|---|---|
| **Full scan** (`mode=full`) | Source `full_scan_done` is false | One task lists the whole feed (uncapped, newest-first), upserts entries **until scan cutoff** (days **before** cutoff are **not** indexed; cutoff day is included; walk stops), then sets `full_scan_done`. Domain must be active; series monitored **not** required. Status: **scanning** / **queued** while a task runs; **pending** if domain inactive with no task; **incomplete** if domain active but no task queued. |
| **Scan** (`mode=scan`) | Source `full_scan_done` is true **and** `kind=feed` | One task walks newest → **stop at first already-known** `remote_id` **or scan cutoff**. Status shows how many **new** videos found. **`kind=single` never tip Scan.** Idle Status label: last-scan summary or **indexed**. |
| **Full scan** (restart) | Series or source action; feed and single | Keep indexed videos/files. Clear `full_scan_done`. Next full scan walks again and **adds** newly found videos only. |

**Kick (series / per-source Scan button):** enqueue tip **Scan** for feed sources with full scan done when domain active (series monitored **not** required; same idea as **Download now**). Manual Scan is allowed even if the source interval is `never`. **Scheduled Scan cron** (`EnqueueScansDue`): monitored series only; same due check for scheduled feeds; enqueues tip Scan when full scan done, else full scan (`EnqueueScanSource` mode switch). **Add source / Full scan / API full scan:** always enqueue full scan when domain active (series monitored not required). Domain queue + per-domain delay spaces execution. Interrupted full scans resume via `RequeueStaleRunning` (re-list + idempotent upserts), **Full scan**, or the next due schedule tick.

**Source kinds**

| Kind | UI | After first index |
|---|---|---|
| `feed` (default) | Add feed; cutoff + title include/exclude + Scan interval + Mark new videos as ignored | Tip Scan on per-source `scan_cron` / series **Scan** / per-source button |
| `single` | Add single; URL + label | No tip Scan; Full scan OK |

**Mark new videos as ignored** (`index_as_ignored`): when on (**feed** sources only; UI hidden for singles), new videos get status `ignored` instead of `wanted` until manually set to wanted. **Title include / exclude** (`title_regexp_include`, `title_regexp_exclude`): optional Go regexps on **feed** sources (UI hidden for singles). Index only when include matches (if set) and exclude does not match (if set); **exclude wins** when both match (no video row). Tip scan walks past title skips (continue; stop only on known remote id or cutoff). Empty = no filter. Invalid patterns rejected on save. **Auto ignore filters** (`auto_ignore_media_types`): optional on **series** (JSON string array of yt-dlp `media_type` values; Edit series / add-series under Auto ignore filters). Videos are indexed, then marked `ignored` when type is known (create-time if listed; download `--match-filters` otherwise; before `index_as_ignored` at create). Missing/empty media type is never auto-ignored (type often unknown at list time). Empty list = none. **Live soft-skip** (always on for download): yt-dlp `is_live` → task `done`, status stays `wanted`, history `live_skipped`, info notify `live_skipped` with task link (retry after broadcast). **Scan cutoff** (optional YYYY-MM-DD): full scan and tip Scan stop at the first listing **before** that UTC day; the cutoff day is indexed; older days are **not**.

**Tip scan** indexes new `wanted` videos only (no immediate download enqueue). Full scan stays index-only. Wanted videos wait for `download_wanted_cron` / **Download now**.

**Per-domain settings (UI: default + overrides on Settings → Queue / Domains):**

| Field | Default | Role |
|---|---|---|
| `task_cooldown_seconds` | `30` | Pause after a task **starts** before another may start on that host domain (still applies with parallel tasks). Interactive prefetch kinds do not start cooldown. Does not apply to the `system` lane. |
| `max_download_queue` | `8` | Max pending+running download tasks **for this hostname**. Auto-enqueue stops when full; **Download now** bypasses. Upper bound for max parallel tasks. |
| `max_parallel_tasks` | `1` | Max concurrent **running** non-interactive tasks on this hostname (scan, download, …). Set ≥ 2 so a tip scan can start while a download runs. Must be ≤ max download queue. |
| `download_rate_limit` | `10M` | Passed to yt-dlp as `--limit-rate` for archive download and scan/list. UI: number + unit join (`K`/`M`/`G`, or Unlimited → `off`). `off` / `0` / `none` = unlimited. Not used for **interactive** metadata prefetch (`prefetch_*`). |
| `sleep_requests` | `1` | Seconds for yt-dlp pacing: `--sleep-requests`, `--sleep-subtitles`, and `--sleep-interval` (same value). `0` = off. Applies to archive/scan; **not** to interactive metadata prefetch. |

Global defaults live on `domains` row **`default`** (non-NULL limit columns). Per-host overrides are other `domains` rows (NULL = inherit `default`). Missing host row = implicitly active + `default` limits. Operator creates/deletes overrides on Settings → Queue / Domains. Reserved name `default` is not a host override.

**Enqueue guards (all task kinds):** `queue.Enqueue` rejects duplicates - equivalent pending/running task already exists (download by `video_id`, scan by `source_id` in payload, metadata rescan by video or series, import by video, video removal one global). Scan enqueue also pre-checks pending+running via `HasActiveScanForSource` and returns `conflict: scan already queued or running` (UI flash strips the `conflict: ` prefix). Download also rejects when that domain’s `max_download_queue` is full (pending+running download tasks on that hostname).

**Scan / Full scan buttons:** disabled while that source already has a pending or running scan (series source row actions + source detail); tip shows `Scan already queued or running`.

**Claim:** up to `max_parallel_tasks` non-interactive tasks may run per domain; cooldown spaces starts.

**Source UI (series detail):** one **Status** column (icon + short label). Kind (feed/single) sits on the URL row.

| Priority | Icon | Label (examples) |
|---|---|---|
| Running / pending scan task | spinner / queue | `scanning` / `queued` (or short task message) |
| Latest list-pass error | error | truncated error (links to task) |
| Full scan incomplete, has schedule, no task | calendar-x-2 (warning) | `incomplete` (or `pending` if domain inactive); tip: `Full scan incomplete` + next-scan line |
| Full scan incomplete, no schedule | calendar-off (error/red) | `incomplete` / `pending`; tip: `Full scan incomplete` + `No scan scheduled`; escalates to series status |
| Full scan done | calendar-clock / calendar-check-2 (single) | last-scan summary (`9 h 54 m ago (1 new)`) for feeds; singles `complete` |
| Else | calendar-off | `no schedule` / `scanning` |

Tooltip for indexed feeds: **Scanned until YYYY-MM-DD** (when cutoff set), then **Regexp filters apply** when title include/exclude is set, then **Next scan in …** (`tooltip-content` + newlines). Missed schedule slots use the next cron after now (not “due now”). Last-scan detail only when schedule is off. Full-scan wording appears only while full scan is incomplete. Live OOB swaps the Status cell.

**History (finished scan task):** one finished scan task with detail, e.g. `Scan: indexed 12 videos (3 new, 1 skipped by title include, 1 skipped by title exclude, 1 ignored by media type, 1 marked as ignored)` (mode lives in task detail `full` / history `mode`; optional `, cutoff reached`); `created_ids` / `updated_ids`; `skipped_title_regexp_include` / `skipped_title_regexp_exclude` (`[{remote_id, title}, …]`); `ignored_media_type_ids` / `ignored_index_as_ignored_ids` (subsets of creates). History list shows the task message only (open task detail for video links). Task detail (`/task/{id}`): Detail section lists JSON keys; `created_ids` show id, title, required scan-time state (`wanted`\|`ignored`), and ignore reason when ignored; `skipped_title_regexp_include` / `skipped_title_regexp_exclude` list titles (no video links; ~20 then “and N more”); ignore id arrays render as counts. Not one History row per video.

**Source history:** each list pass also writes one `source_history` row (`scanned` or `scan_error`) with `mode`, counts, `created_ids` / `updated_ids`, `skipped_title_regexp_include` / `skipped_title_regexp_exclude`, and the same ignore id lists. Cancelled scan tasks write `cancelled` (listed on source detail History; does not affect sticky status or tip due). That row is canonical for source detail History, sticky status, tip due (`mode=scan` only), and projected discover/update on video detail. List passes do **not** write per-video `video_history` discover/update/rescan_metadata rows.

## Task kinds (domain queue)

| Kind | Trigger | Behavior |
|---|---|---|
| `scan` | Add source, Full scan, tip Scan schedule/manual | **One task = one source**. Full scan walks entire list; tip Scan stops at first known. **Index only**. Progress on series page |
| `download` | Download-wanted cron, Download now, retry, **maturity media** (payload `maturity`) | Handler fetch (format selector: profile ladder for **video** delivery, fixed `ba/bestaudio/b` for **audio**); when profile **remove** is empty: remux (MKV video / MKA audio) + optional mark-only SB + **pack**. When remove is set: stage under `{CacheDir}/sponsorblock-cut/{videoID}/` and enqueue `sponsorblock_cut` (video stays `wanted`). |
| `sponsorblock_cut` | After archive download when remove categories set | System lane (low priority): remux only if copy-cut; ApplyArchive cut/reencode (ffmpeg progress 0–1 on keep encode + card filter-stitch); pack; cleanup staging. Cancel wipes staging (like cancel download). |
| `media_verify` | After successful archive pack when profile gate says so; import confirm `verify` | System lane (lowest priority, after cut): ffmpeg null-decode (`-xerror`) of library media with `-progress`. Success → history `verified`. Fail → keep files, status `verify_failed`, notify `verify_failed`, code `MediaVerifyFailed`. Cancel leaves `downloaded`. New pack cancels prior verify (superseded). |
| `refresh_sidecars` | Maturity sidecar cron; video detail **Refresh sidecars** | NFO/thumb/subs only beside pack anchor; never media or `info.json`; maturity cron also sets `sidecars_acquired_at` |
| `rescan_metadata` | User on video or series | Update metadata for existing videos only; no new videos. Rewrites NFO/thumb/subs (not `info.json`). API: `POST /api/series/{id}/metadata-rescan`, `POST /api/videos/{id}/metadata-rescan` |
| `import` | Import UI | Scan chosen folder (inbox default, or one library root); match to indexed videos **or create unmatched** via Match modal (name search + poster/thumb lists); confirm enqueues **pack** (inbox move) or in-place bind (library). Create unmatched requires `upload_date` (scan suggests from sidecar or file mtime; API falls back to mtime). Media video list defaults to videos without packed media; **Allow matching existing media** in the modal opts in to replace (API `replace=true` deletes existing library media then imports). ID match order: filename `[id]`, then `info.json` `id`, then NFO `<uniqueid>`; then title similarity; series similarity suggests a series only (no video / no auto-CREATE - operator picks or creates a video in Match). Orphan `info.json` is not attachable (provenance with media only). Sidecar Attach Match only lists videos that already have packed library media (API also rejects attach without media). Inbox subtitle/thumb Attach moves into the episode folder beside media; library orphans must already be beside media. Orphan/sibling `.nfo` applies editable episode metadata to the video row then regenerates the library NFO from DB (source XML not kept). `.nfo` is data import only and requires a same-basename video beside it (bundled with media import, or orphan beside packed library media); alone with no sibling video it is listed only. |

Store in `tasks` table. Never use kind name `poll`. Boot renames legacy kind strings and related settings keys (`file_sync` → `sync_files`, `retention_purge` → `retention_delete`, …).

### Maintenance outside the domain queue

| Pass | Trigger | Behavior |
|---|---|---|
| **File sync** | `sync_files_cron` (optional); Settings → Maintenance → **Scan for missing files** | Online roots only. Cron/manual enqueue is a no-op when there are no videos. (1) `downloaded`/`verify_failed` + path gone → `missing` (keep rows). (2) `missing` + path back → `downloaded`. (3) present media: non-NULL `files.size_bytes` vs on-disk size; mismatch → `verify_failed`, update size, history `file_externally_changed` (no auto re-download); NULL size backfilled quietly. (4) Registered sidecars (`kind != video`): missing → keep row + `size_bytes = -1` + `sidecar_missing` (status unchanged); restore + `sidecar_restored`; size mismatch + `sidecar_externally_changed` (status unchanged); NULL size backfilled quietly. Size only - no hash/mtime. History detail when changed (`missing_ids` / `restored_ids` / `externally_changed_ids` / `sidecar_*`). One alert digest `file_sync_issues` when media or sidecar missing/size issues found. |
| **Retention delete** | `retention_delete_cron` (optional) | Online roots only. Cron enqueue is a no-op when no root has `retention_ttl_seconds`. Roots with TTL: delete expired artifacts → `deleted` + history `retention`; prune empty series dirs. Default root has **no** TTL. History detail on the finished `retention_delete` task when changed (`retention_ids`). |
| **NFO regenerate** | Settings → Maintenance | System task `regenerate_nfo` with video/series cursors (resumes after Creatorr restart via `RequeueStaleRunning`). Skips write when on-disk bytes match; episode rewrite appends `video_history` (`nfo_regenerated`) with task id. One History outcome on the finished task (`tasks.detail`) on completion. |
| **File delete** | Series UI delete (required confirm) / video Delete | System task `delete_files` (payload `series_ids` / `video_ids` + cursors). HTTP cancels related work and enqueues; rows stay until worker finishes. Standalone video → `MarkDeleted(..., task_id)`; series purge → disk unlink then `DELETE series`. UI shows **queued for deletion** from payload membership (no new `videos.status`). REST `DELETE /api/series/{id}` keeps library files. | Per-sidecar Delete (`sub`/`thumb`/`other`): sync `delete_sidecar` bookkeeping + `sidecar_deleted` (not this task).
| **Apply episode format** | Settings → Maintenance | System task `rename_episodes` renames packed episode file sets to current formats. Per-video history `renamed` on success. |

## Queue (per domain)

- Tasks that touch a site belong to a **domain** (from URL hostname).
- One ordered queue per domain: position number, FIFO processing.
- **Pause** (Tasks lane / `PUT /api/domains/{domain}/paused`): soft stop - no new **ClaimNext** claims; pending stay queued; running continue to Finish. Stored in `domain_runtime` (no `domains` override row). **Interactive** metadata prefetch (`prefetch_series_meta` / `prefetch_video_meta` / `prefetch_add_series` / `prefetch_add_video` via ClaimInteractive) still runs while paused (ignores busy/cooldown slots). Cookie-auth, rate-limit, IP-block, download, and resolve (`CookieInvalid` / `RateLimited` / `DownloadFailed` / `ResolveFailed`) failures **auto soft-pause** that hostname and notify as **alerts** (`cookie_invalid` / `rate_limited` / `ytdlp_failed`; always recorded in-app). Remux/pack/verify failures notify as alerts but do **not** auto-pause. Domains are never auto-deactivated. `GET /api/domains/paused` lists soft-paused hosts. Tasks page shows an alert above the lanes summarizing Pause vs interactive; each host lane links to History filtered by that domain.
- **Inactive** (`domains.active=0`, API): hard stop - no claims; deactivate cancels pending+running and creates a host `domains` row. Not shown on Settings → Queue / Domains overrides UI.
- Cooldown between tasks is **per-domain** (`task_cooldown_seconds`; starts on **ClaimNext** for host lanes, not on Finish, not after interactive prefetch, and never on the `system` lane). On `/tasks` each host lane always shows queue status on line 1 left of History: labeled daisyUI [progress](https://daisyui.com/components/progress/) for pause / cooldown / busy (all parallel slots taken, indeterminate), or muted `Idle` when idle; `Active` when running with free parallel slots (indeterminate bar); `Busy` when all parallel slots are taken (indeterminate bar).
- UI **Tasks** page: in-progress + queued tasks grouped by domain (`system` always first, then every known host from domains rows, source URLs, and soft-paused `domain_runtime` hosts, including empty lanes); each lane is one daisyUI task list; Pause/Resume per host lane; kind/message link to `/task/{id}`.
- **Task detail** (`/task/{id}`): any status; live status/message/progress via SSE; in-memory progress logs while running (one auto Refresh on open, then manual Refresh). Running task detail / Tasks list: mid `(0,1)` determinate bar; 0% / 100% / nil = indeterminate busy. Compact indicators still spinner on nil / 0% / 100%. Download messages label per-format steps. Host-lane tasks snapshot **Domain access** chips into `tasks.detail` `domain-access` at claim (rate / sleep / cookies / Flare / credentials; same icons as Tasks lane; queue / parallel / cooldown omitted). **Commands** panel lists each yt-dlp / ffmpeg / ffprobe argv (shell-quoted) recorded while the task ran; stored on `tasks.commands` and kept after finish (unlike live Logs). Pretty toggle (same as JSON file view) switches one-line vs `--`-flag line breaks.
- **Video detail**: on `task.done` / `task.failed` for that video, full page reload (status, files, actions) - indicator OOB alone is not enough.
- Series detail: per-source combined **Status** (icon + short label: running/pending/error/incomplete/indexed/…). Tooltip covers cutoff, next/last scan. Kind icon on the URL row. Remonitor does not auto-enqueue scans.
- **Source detail** (`/series/{id}/sources/{sid}`): same Status chip in the subtitle; details and History for that source (videos live on the series video list, filterable by source).
- Series / source **Full scan**: enqueues immediately (no confirm); videos kept; new findings added; resets `full_scan_done` only; OK when series unmonitored.
- Inactive domain: source/video action buttons disabled with tooltip to Settings → Queue / Domains.

## Scan vs metadata rescan

| | **Scan / Full scan** | **Metadata rescan** |
|---|---|---|
| Scope | Series → its **sources** (manual tip Scan / full scan: domain active; scheduled tip Scan also needs series monitored) | Existing **videos** (one or all in series) |
| Discovers new videos | Yes | **No** |
| Updates existing | Soft-fill empty **title** / **description** / **thumbnail_url** only | Soft-fill empty **title** / **description** / **thumbnail_url** only; may merge **genres** from packed `info.json` `categories` when `metadata_genres_from_categories` is on |
| On-disk sidecars | Unchanged | Rewrites **NFO / thumb / subs** beside existing media; **never** replaces the video file or `info.json` |
| Missing at source | May mark/unlist per rules (TBD) | Leaves video unchanged |

Metadata Save / import NFO still overwrite editable fields (operator intent). See soft-fill notes in [`download-and-library.md`](download-and-library.md).
