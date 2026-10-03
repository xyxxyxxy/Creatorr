import { refreshBadge, refreshNotificationHistoryPanel, refreshNotifyBadge, refreshNotifyDropdown } from "./badges.js";
import { maybeRefreshFilesList } from "./files_live.js";
import { refreshTasksPanel } from "./lanes.js";
import { maybeRefreshMaintenance } from "./maintenance.js";
import { maybeRefreshSeriesList, maybeRefreshSeriesVideos } from "./series_live.js";
import { patchTaskDetail, patchTaskRow, refreshHistoryPanel, refreshTaskIndicators, refreshTaskVideoHistoryIfMatch, refreshVideoHistoryIfMatch, reloadTaskDetailIfMatch, reloadVideoDetailIfMatch } from "./taskrows.js";

function onConnectPage() {
  return location.pathname === "/settings/connect";
}

const ytdlpUpdateTaskKind = "ytdlp_update";

function refreshYtDlpConnectLive() {
  if (!onConnectPage() || !window.htmx) return;
  const version = document.getElementById("ytdlp-connect-installed-version");
  if (version) {
    window.htmx.ajax("GET", "/settings/connect/ytdlp-installed-version", {
      target: "#ytdlp-connect-installed-version",
      swap: "outerHTML",
    });
  }
  const lastChecked = document.getElementById("ytdlp-connect-last-checked");
  if (lastChecked) {
    window.htmx.ajax("GET", "/settings/connect/ytdlp-last-checked", {
      target: "#ytdlp-connect-last-checked",
      swap: "outerHTML",
    });
  }
}

function maybeRefreshYtDlpConnect(ev) {
  if (!onConnectPage()) return;
  if (ev.type !== "task.done" && ev.type !== "task.failed" && ev.type !== "task.updated") return;
  let kind = "";
  try {
    const data = JSON.parse(ev.data || "{}");
    kind = data.kind || "";
  } catch (_) {}
  if (kind !== ytdlpUpdateTaskKind) return;
  refreshYtDlpConnectLive();
}

function onSSE(ev) {
  refreshBadge();
  if (ev.type === "notification.created" || ev.type === "notification.read") {
    let uc;
    try {
      const data = JSON.parse(ev.data || "{}");
      if (typeof data.unread_count === "number") uc = data.unread_count;
    } catch (_) {}
    refreshNotifyBadge(uc);
    refreshNotifyDropdown();
    refreshNotificationHistoryPanel();
    return;
  }
  if (ev.type === "task.updated") {
    // In-place patch on Tasks page - full swap recreates Busy/Cancel every tick.
    if (!patchTaskRow(ev)) refreshTasksPanel(false);
    patchTaskDetail(ev);
    refreshTaskIndicators();
    refreshVideoHistoryIfMatch(ev);
    refreshTaskVideoHistoryIfMatch(ev);
  } else if (ev.type === "task.done" || ev.type === "task.failed") {
    patchTaskDetail(ev);
    refreshTasksPanel(true);
    refreshTaskIndicators();
    refreshHistoryPanel();
    reloadTaskDetailIfMatch(ev);
    reloadVideoDetailIfMatch(ev);
  }
  maybeRefreshSeriesVideos(ev);
  maybeRefreshSeriesList(ev);
  maybeRefreshFilesList(ev);
  maybeRefreshMaintenance(ev);
  maybeRefreshYtDlpConnect(ev);
  if (typeof window.refreshImportTasksBusy === "function") {
    window.refreshImportTasksBusy(ev);
  }
}

export function connectEvents() {
  if (!window.EventSource) return;
  const es = new EventSource("/api/events");
  ["task.updated", "task.done", "task.failed", "notification.created", "notification.read"].forEach((name) => {
    es.addEventListener(name, onSSE);
  });
  es.onerror = () => {
    // Browser reconnects automatically; keep polling badge as fallback.
  };
}
