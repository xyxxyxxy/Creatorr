//! pin: SSE task.done|failed for file_hash_check|integrity_check|integrity_check_initial|sync_files must HTMX-refresh #files-list-live

const FILES_LIVE_ID = "files-list-live";

const FILES_REFRESH_KINDS = new Set([
  "file_hash_check",
  "integrity_check",
  "integrity_check_initial",
  "sync_files",
]);

function filesLiveRoot() {
  return document.getElementById(FILES_LIVE_ID);
}

function filesBrowseParams() {
  const live = filesLiveRoot();
  const params = new URLSearchParams(location.search || "");
  params.set("type", "files");
  const videoDetail = location.pathname.match(/^\/series\/(\d+)\/videos\/(\d+)/);
  const seriesDetail = location.pathname.match(/^\/series\/(\d+)\/?$/);
  if (videoDetail) {
    params.set("at", "video-detail");
    params.set("series_id", videoDetail[1]);
    params.set("video_id", videoDetail[2]);
  } else if (seriesDetail) {
    params.set("at", "series-detail");
    params.set("series_id", seriesDetail[1]);
    params.delete("video_id");
  } else if (/^\/browser\/?$/.test(location.pathname)) {
    params.set("at", "browser");
  } else if (live) {
    const sid = live.getAttribute("data-series-id") || "";
    const vid = live.getAttribute("data-video-id") || "";
    if (vid && vid !== "0") {
      params.set("at", "video-detail");
      if (sid && sid !== "0") params.set("series_id", sid);
      params.set("video_id", vid);
    } else if (sid && sid !== "0") {
      params.set("at", "series-detail");
      params.set("series_id", sid);
      params.delete("video_id");
    } else {
      params.set("at", "browser");
    }
  } else {
    params.set("at", "browser");
  }
  return params;
}

function refreshFilesList(preserveScroll) {
  if (!window.htmx || !filesLiveRoot()) return;
  const y = preserveScroll ? window.scrollY : null;
  window.htmx.ajax("GET", "/explorer/browse?" + filesBrowseParams().toString(), {
    target: "#" + FILES_LIVE_ID,
    select: "#" + FILES_LIVE_ID,
    swap: "outerHTML",
  });
  if (y == null) return;
  const restore = () => window.scrollTo(0, y);
  document.body.addEventListener("htmx:afterSwap", function onSwap(ev) {
    if (!ev.detail || !ev.detail.target || ev.detail.target.id !== FILES_LIVE_ID) return;
    document.body.removeEventListener("htmx:afterSwap", onSwap);
    requestAnimationFrame(restore);
  });
}

let filesRefreshAt = 0;

export function maybeRefreshFilesList(ev) {
  if (!filesLiveRoot()) return;
  let kind = "";
  try {
    const data = JSON.parse(ev.data || "{}");
    kind = data.kind || "";
  } catch (_) {}
  if (!FILES_REFRESH_KINDS.has(kind)) return;
  if (ev.type === "task.done" || ev.type === "task.failed") {
    refreshFilesList(true);
    return;
  }
  if (ev.type === "task.updated") {
    const now = Date.now();
    if (now - filesRefreshAt < 2000) return;
    filesRefreshAt = now;
    refreshFilesList(true);
  }
}
