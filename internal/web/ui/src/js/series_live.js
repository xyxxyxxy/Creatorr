function refreshSeriesVideos(preserveScroll) {
  if (!window.htmx) return;
  const seriesPage = location.pathname.match(/^\/series\/(\d+)\/?$/);
  if (!seriesPage || !document.getElementById("series-videos-live")) return;
  const y = preserveScroll ? window.scrollY : null;
  const q = location.search || "";
  window.htmx.ajax("GET", "/series/" + seriesPage[1] + "/videos-live" + q, {
    target: "#series-videos-live",
    select: "#series-videos-live",
    swap: "outerHTML",
  });
  if (y == null) return;
  const restore = () => window.scrollTo(0, y);
  document.body.addEventListener("htmx:afterSwap", function onSwap(ev) {
    if (!ev.detail || !ev.detail.target || ev.detail.target.id !== "series-videos-live") return;
    document.body.removeEventListener("htmx:afterSwap", onSwap);
    requestAnimationFrame(restore);
  });
}

export function onSeriesListPage() {
  return /^\/series\/?$/.test(location.pathname);
}

export function onSeriesDetailPage() {
  return /^\/series\/\d+\/?$/.test(location.pathname);
}

function refreshSeriesList(preserveScroll) {
  if (!window.htmx) return;
  if (!onSeriesListPage() || !document.getElementById("series-list-live")) return;
  const y = preserveScroll ? window.scrollY : null;
  const q = location.search || "";
  window.htmx.ajax("GET", "/series/list-live" + q, {
    target: "#series-list-live",
    select: "#series-list-live",
    swap: "outerHTML",
  });
  if (y == null) return;
  const restore = () => window.scrollTo(0, y);
  document.body.addEventListener("htmx:afterSwap", function onSwap(ev) {
    if (!ev.detail || !ev.detail.target || ev.detail.target.id !== "series-list-live") return;
    document.body.removeEventListener("htmx:afterSwap", onSwap);
    requestAnimationFrame(restore);
  });
}

let videosRefreshAt = 0;

export function maybeRefreshSeriesVideos(ev) {
  if (!onSeriesDetailPage()) return;
  let kind = "";
  try {
    const data = JSON.parse(ev.data || "{}");
    kind = data.kind || "";
  } catch (_) {}
  if (ev.type === "task.done" || ev.type === "task.failed") {
    refreshSeriesVideos(true);
    return;
  }
  if (ev.type === "task.updated" && (kind === "scan" || kind === "bulk_edit_videos" || kind === "delete_files")) {
    const now = Date.now();
    if (now - videosRefreshAt < 2000) return;
    videosRefreshAt = now;
    refreshSeriesVideos(true);
  }
}

let seriesListRefreshAt = 0;

export function maybeRefreshSeriesList(ev) {
  if (!onSeriesListPage()) return;
  let kind = "";
  try {
    const data = JSON.parse(ev.data || "{}");
    kind = data.kind || "";
  } catch (_) {}
  // Drop deleted series (and clear deleting state) when delete_files finishes.
  if ((ev.type === "task.done" || ev.type === "task.failed") && (kind === "delete_files" || kind === "bulk_edit_series")) {
    refreshSeriesList(true);
    return;
  }
  if (ev.type === "task.updated" && (kind === "delete_files" || kind === "bulk_edit_series")) {
    const now = Date.now();
    if (now - seriesListRefreshAt < 2000) return;
    seriesListRefreshAt = now;
    refreshSeriesList(true);
  }
}
