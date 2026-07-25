# Stream proxy

Index: [README.md](README.md). Terminology: [`AGENTS.md`](../AGENTS.md). Download/archive pipeline: [download-and-library.md](download-and-library.md). yt-dlp: [ytdlp.md](ytdlp.md).

Opt-in **stream delivery** lets any `.strm`-aware client play indexed videos through Creatorr without storing full media on disk. Default **download** delivery is unchanged.

## Delivery mode (per series)

Each series has `delivery_mode`:

| Mode | Behavior |
| --- | --- |
| `download` (default) | Wanted videos enqueue **download** tasks; yt-dlp fetch → Creatorr remux (MKV) → **pack** media + NFO + sidecars. Status **`downloaded`**. |
| `stream` | Wanted videos enqueue **pack_stream** tasks instead of download. Creatorr writes `.strm` + episode NFO (+ optional thumb + subtitle sidecars when Library langs are set) under the series root. Status **`streamable`**. Playback goes through the **stream proxy** (below). **No remux** and no local media file. |

Stream mode requires:

- Settings **External Creatorr URL** (`external_base_url`) set (see below). Managed yt-dlp is boot-enforced.
- Stream resolve uses in-tree `urls` / pipe mux (plugins may be download-only).

Changing delivery mode does not move existing files. Operator toggles only.

## Stream pack

**Task kind:** `pack_stream` (UI: Prepare stream / refresh).

Worker calls yt-dlp **`urls`** (not full download) to validate the site can resolve a play path. When that extract reports a `media_type` listed in the series **auto ignore filters**, pack finishes **`done`**, video → **`ignored`** (reason `media_type`), and **no** `.strm` / NFO write and **no** `cache_beginning` enqueue. Empty/missing type never auto-ignores (same as download). Otherwise writes library files:

1. **`.strm`** - one line: Creatorr proxy URL (see below). Still **one** `.strm` entry regardless of progressive / pipe / HLS.
2. **`.nfo`** - full episode NFO (same shape as download pack), including **runtime / durationinseconds** when resolve/urls reports duration (Emby uses this for `.strm` length).
3. **Optional thumb** - copied beside the episode when `videos.thumbnail_url` is fetchable.
4. **Optional subtitle sidecars** - when Library `subtitle_langs` is non-empty, yt-dlp fetches subtitle files (same settings as archive download: langs / auto; always converted to SRT) and copies them beside the `.strm` with language in the filename (e.g. `.en.srt`, or `.en.auto.srt` for auto-only). Media servers can load these as external tracks. Subtitles are **not** muxed into the stream proxy. Soft-ok if fetch fails. Empty langs = skip. Replace sidecars also refreshes subs for `streamable` episodes (never rewrites `info.json`).

**Maturity:** when a series' quality profile `maturity_redownload_hours` > 0, streamable videos due for media maturity enqueue **`pack_stream`** again (rebuild beginning + sidecars policy). Sidecar maturity uses `refresh_sidecars` without touching media. See [`download-and-library.md`](download-and-library.md).

**SponsorBlock:** when the series quality profile has `sponsorblock_remove` categories, `pack_stream` writes `.sponsorblock.json` beside the `.strm` (frozen skip plan; `info_cards` only when `sponsorblock_reencode_cut` is also on). NFO/playlist duration is padded to the play timeline. **Download beginning** is skip-aware: fills **playback** seconds from keep windows (handoff uses source time after skips). Clear the plan sidecar when remove is empty on re-pack. Uses SponsorBlock data from https://sponsor.ajay.app/.

On success, video status becomes **`streamable`** and `files` rows record `strm` / `nfo` / `thumb` / `sub` kinds (no `video` kind - no packed media bytes).

When Settings **`cache_beginning_seconds`** > 0 (default **20**), pack success also enqueues a **`cache_beginning`** task **only when** the last `urls` kind is **`pipe`**. Shares that domain’s `max_download_queue`. Soft failure leaves the video **`streamable`**. Re-pack and delete clear the cache. Columns store `stream_urls_kind` / `stream_beginning_cached`. **File sync** can requeue beginning on loss. **Play uses the beginning** when present: beginning segs then live mux from N, framed as VOD + START:0 + duration pad + ENDLIST (Emby scrubber length). `0` = off. Settings → Library → **Streaming** shows this knob; editable when External Creatorr URL is set (same gate as series Stream mode).

**Progressive playback cache:** when Settings **`stream_playback_cache`** is on, pipe play copies live mux segments into `{CacheDir}/playback-cache/{videoID}/` (seeds from beginning when present). Later plays prefer that cache: partial handoff resumes yt-dlp/ffmpeg at handoff source time N; complete caches serve static VOD without a new mux. **`stream_playback_cache_max_hours`** (default **20**, range 10–100 step 10) is a rolling **total** content budget; over budget evicts least-recently-accessed whole-video progressive caches (not beginning). Disable stops new writes; existing entries remain until eviction or clear. Video stays **`streamable`**. Video detail shows combined **Stream cache** % (max of beginning + progressive) with a progress bar.

Series list icons (idle `streamable`):

| Icon | Meaning |
| --- | --- |
| Lucide **`radio`** (info) | Pipe (or unknown) - no beginning on disk; first play may wait |
| Lucide **`circle-play`** (success) | Pipe beginning cached - instant start on play |
| Lucide **`zap`** (success) | CDN HLS/progressive kind |

Series list **Progress** for stream series: stacked bar + `optimized+cold/total` where **optimized** = beginning cached or CDN, **cold** = streamable without beginning, **total** = streamable + wanted.

**Auto enqueue:** On the same cron as **download wanted** (`download_wanted_cron`), Creatorr runs **`EnqueuePackStreamWanted`**: monitored stream-mode series, status **`wanted`**, domain active, yt-dlp ready for stream resolve. **Download wanted** explicitly skips `delivery_mode = 'stream'` series - stream and archive paths do not overlap.

Manual **Prepare stream** enqueues one `pack_stream` at front of the domain lane when allowed.

## External Creatorr URL

| Key | Purpose |
| --- | --- |
| `external_base_url` (Settings → Library → Streaming) | Absolute origin the external media server uses to reach Creatorr for `.strm` playback (scheme+host+port, no trailing slash). **Essential for streaming** - Emby/Jellyfin/Kodi/etc. play `.strm` by streaming **through** Creatorr’s proxy. |

- **Empty:** stream pack and stream delivery UI disabled (`pack_stream` enqueue fails; proxy route still mounted but pack requires base URL).
- **Set:** `.strm` contents and proxy links use `{external_base_url}/stream/videos/{id}/master.m3u8?token=…` (`.m3u8` suffix so Emby treats it as HLS). Legacy `/stream/videos/{id}?token=` still works.

**Bootstrap:** if Settings is empty on first boot and env `CREATORR_PUBLIC_BASE_URL` is set, Creatorr copies it into `external_base_url` once. Prefer the Settings field afterward; env is not a live override.

Creatorr stores a random **`stream_url_token`** in SQLite (auto-created). Shown under Streaming when the external URL is set; **Generate new token** invalidates existing `.strm` URLs (confirm modal warns to run **Regenerate all .strm files**). Every proxy request must include `?token=` matching the stored value.

**Regenerate all .strm files** (Maintenance): system-lane `regenerate_strm` rewrites on-disk `.strm` lines to the current external URL + token (no yt-dlp). Run after rotating the token or changing the external URL.

**Clear beginning of stream cache** (Maintenance): system-lane `clear_beginning_cache` deletes beginning caches under `{CacheDir}/download-beginnings`, clears `stream_beginning_cached`, and cancels pending `cache_beginning` tasks.

**Clear progressive stream cache** (Maintenance): system-lane `clear_playback_cache` deletes `{CacheDir}/playback-cache`, zeros progressive cache columns. Does not touch beginning caches.

## Stream proxy

**Routes** (outside OpenAPI; long timeout; not subject to normal API auth):

| Path | Role |
| --- | --- |
| `GET`/`HEAD` `/stream/videos/{id}/master.m3u8?token=` | Entry from `.strm` (also bare `/stream/videos/{id}`). `HEAD` returns Content-Type only (no mux). Pipe mux; beginning handoff when beginning cached. |
| `GET`/`HEAD` `/stream/videos/{id}/hls?token=&u=` | HLS media playlists + segments (rewritten URIs only). |
| `GET`/`HEAD` `/stream/videos/{id}/hls/local/{sid}/{file}?token=` | Pipe session playlist/segments (MPEG-TS). |
| `GET`/`HEAD` `/stream/videos/{id}/beginning/{file}?token=` | Cached download-beginning segments (durable; not library media). |
| `GET`/`HEAD` `/stream/videos/{id}/playback/{file}?token=` | Progressive on-play cache segments (durable; not library media). |

Flow when a client opens a `.strm` URL:

1. Validate token and load video; require stream-mode series and status **`streamable`**.
2. Resolve page URL (video `source_url` only - no feed URL fallback).
3. Gate: domain **active**, yt-dlp binary present.
4. Invoke yt-dlp **`urls`** with quality profile format selector, domain cookies, optional FlareSolverr. **Does not** pass `download_rate_limit` (avoids throttling play bytes).
5. **Play path:** master/`.strm` always **pipe** (Emby cancels native CDN HLS on `.strm`). When a **download beginning** is on disk: beginning segs immediately, live mux at N (`--hls-start-sec`), one playlist with `#EXT-X-DISCONTINUITY`, VOD + START:0 + duration pad + ENDLIST. Without beginning: pipe from 0 + pad. Native `/hls?u=` and progressive routes remain for non-master use.

| `kind` from `urls` | Used for | Seek |
| --- | --- | --- |
| (any on master/`.strm`, beginning cached) | Beginning segs + live session MPEG-TS HLS from N; VOD pad | **Linear** - scrubber from pad; mid scrub may stall |
| (any on master/`.strm`, no beginning) | Session MPEG-TS HLS from 0; VOD pad | **Linear** - scrubber from pad |
| `hls` (via `/hls?u=`) | Rewritten CDN playlist + segment proxy | Segment-based |
| `progressive` (non-master) | Range-proxy CDN | Good (HTTP Range) |

**ABR** = HLS multi-variant through Creatorr rewrite when using `/hls`. Not an Emby multi-quality API and not DASH MPD passthrough.

Remux does **not** run on the proxy path - archive **download** remux stays MKV-only ([download-and-library.md](download-and-library.md) § Remux). Creatorr owns A+V merge for `pipe` (ffmpeg / session HLS via yt-dlp).

**DASH / progressive HD:** Progressive-only CDN URLs often cap ~360p–720p. yt-dlp prefers **hls** when a usable master exists, else **pipe** (DASH A+V → session MPEG-TS HLS; Matroska fallback), else progressive. Stream play prefers **H.264+AAC** when available so Emby can Direct Play HLS (AV1/VP9+Opus in HLS often fails with "No compatible streams"). See [ytdlp.md](ytdlp.md).

**Cold start:** Pipe HLS needs one yt-dlp `urls` resolve plus ffmpeg until the first short segment (`-hls_init_time 1`). Emby HEAD probes skip resolve. Short in-memory `urls` cache (~45s) and a **warm HLS session** (idle TTL ~2m) skip resolve on master re-fetch. No mid-session mux restart for scrub. Media playlists are rewritten as **VOD** with `#EXT-X-START:TIME-OFFSET=0`; when runtime is known, Creatorr **pads** future `segNNNNN.ts` entries + `#EXT-X-ENDLIST` so Emby shows full duration. When a **download beginning** is on disk, play serves those segs first and starts live mux at N (pad covers the rest). Padded segs wait on the linear mux (`ensureSessionSegment`). Mid-timeline scrub may stall/404 until mux catches up.

**Skipping resolve entirely on first cold play** is not reliable when CDN URLs from `urls` expire; packing them into the DB would still need a refresh on 403. Native `kind: hls` / progressive have the same expiry problem.

**Concurrency:** No hard session cap in this phase; `pipe` costs CPU (ffmpeg) per concurrent play.

## Operator expectations

- **Site load:** Progressive/HLS still pull origin/CDN bytes for a full watch. Pipe muxes A+V in Creatorr. Benefits are **disk space** and **never-watched** videos (index + `.strm` only), not necessarily lower origin bandwidth.
- **Library scan:** Point the media client at the same root folder as the series; `.strm` + NFO appear like normal TV episodes. Do **not** point Emby at Creatorr `/cache` (beginning `.ts` must not be scanned as library media).
- **Network:** The client must reach the External Creatorr URL; Creatorr must reach upstream CDN (and FlareSolverr when enabled). Use a hostname/IP the client can resolve (not `127.0.0.1` on another host unless tunneled).

## Related tasks

| Task | Stream series |
| --- | --- |
| Scan / index | Same as download mode |
| Download wanted | Skipped (`delivery_mode = 'download'` only) |
| Pack stream wanted | Same cron as download wanted |
| Stream play | Live or cache `.strm` playback: `stream_play` occupancy task (InsertRunning); works while soft-paused; occupies parallel slot; play yt-dlp pause codes auto soft-pause |
| Download beginning | After successful pack_stream when `cache_beginning_seconds` > 0 (not after auto-ignore); shares max download queue |
| Download now | N/A (use Prepare stream) |
| Metadata rescan | Same (refresh index metadata; does not replace stream pack) |

See [scan-and-queue.md](scan-and-queue.md) for queue/task details and [ytdlp.md](ytdlp.md) for yt-dlp / plugins.
