import { createLucideIcons } from "./dom_helpers.js";
import { applyInferredLaneStatus, lanePanelFor, refreshTasksPanel } from "./lanes.js";
import { flushNotesAutosave } from "./notes.js";

export function refreshHistoryPanel() {
  if (!location.pathname.startsWith("/history")) return;
  const panel = document.getElementById("history-live");
  if (panel && window.htmx) {
    const q = location.search || "";
    window.htmx.ajax("GET", location.pathname + q, {
      target: "#history-live",
      select: "#history-live",
      swap: "outerHTML",
    });
    return;
  }
  // Detail page: reload so finished task appears / updates.
  if (/^\/history\/\d+/.test(location.pathname)) {
    location.reload();
  }
}

/** Match partials/status_badge.html (icon + tooltip). */
function statusBadgeEl(status) {
  const s = String(status || "");
  const tips = {
    wanted_download_error: "Last download failed",
    wanted_archive: "Live source gone; waiting for Web Archive download",
    verify_failed: "Integrity check failed - file kept; Want or Download now",
    downloaded_integrity_failed: "Integrity check failed - file kept; Want or Download now",
    missing: "File path recorded but media not on disk - file sync may restore",
  };
  const icons = {
    running: { icon: "loader-circle", color: "text-info", spin: true },
    pending: { icon: "list-ordered", color: "text-base-content/50" },
    failed: { icon: "circle-x", color: "text-error" },
    failure: { icon: "circle-x", color: "text-error" },
    cancelled: { icon: "ban", color: "text-base-content/50" },
    done: { icon: "circle-check", color: "text-success" },
    success: { icon: "circle-check", color: "text-success" },
    wanted: { icon: "download", color: "text-warning" },
    wanted_download_error: { icon: "circle-x", color: "text-error" },
    wanted_archive: { icon: "archive", color: "text-warning" },
    verify_failed: { icon: "badge-alert", color: "text-warning" },
    downloaded_integrity_failed: { icon: "badge-alert", color: "text-warning" },
    downloaded: { icon: "circle-check", color: "text-success" },
    missing: { icon: "file-question", color: "text-warning" },
    deleted: { icon: "trash-2", color: "text-base-content/50" },
    ignored: { icon: "eye-off", color: "text-base-content/50" },
  };
  const labels = {
    wanted_download_error: "wanted (download error)",
    wanted_archive: "wanted (Web Archive)",
    verify_failed: "Integrity check failed",
    downloaded_integrity_failed: "Integrity check failed",
  };
  const meta = icons[s] || { icon: "circle-help", color: "text-base-content/50" };
  const tip = tips[s] || s || "-";
  const label = labels[s] || s || "-";
  const wrap = document.createElement("span");
  wrap.className = "inline-flex tooltip tooltip-top " + meta.color;
  wrap.setAttribute("data-tip", tip);
  wrap.setAttribute("aria-label", label);
  const i = document.createElement("i");
  i.setAttribute("data-lucide", meta.icon);
  i.className = "size-4" + (meta.spin ? " animate-spin" : "");
  wrap.appendChild(i);
  return wrap;
}

function patchStatusCell(root, status) {
  if (!root || typeof status !== "string" || !status) return;
  if (root.hasAttribute("data-task-row-status")) {
    root.setAttribute("data-task-row-status", status);
  }
  const cell = root.querySelector("[data-task-status]");
  if (!cell) return;
  cell.replaceChildren(statusBadgeEl(status));
  createLucideIcons(cell);
}

function setTaskPosRunning(posEl) {
  if (!posEl) return;
  posEl.replaceChildren();
  const tip = document.createElement("span");
  tip.className = "tooltip tooltip-top inline-flex";
  tip.setAttribute("data-tip", "Running");
  tip.setAttribute("aria-label", "Running");
  const i = document.createElement("i");
  i.setAttribute("data-lucide", "activity");
  i.className = "size-5";
  tip.appendChild(i);
  posEl.appendChild(tip);
  createLucideIcons(posEl);
}

/** Sync Tasks list progress bar in place. Skip no-op writes so daisyUI
 * indeterminate CSS animation is not restarted every SSE tick. */

function syncTaskRowProgress(row, status, progress) {
  const wrap = row.querySelector("[data-task-progress-wrap]");
  if (!wrap) return;
  let bar = wrap.querySelector("progress[data-task-progress]");
  if (!bar) {
    bar = document.createElement("progress");
    bar.setAttribute("data-task-progress", "");
    bar.max = 100;
    wrap.replaceChildren(bar);
  }
  if (status === "pending") {
    const paused = row.hasAttribute("data-lane-paused");
    const cls = "progress " + (paused ? "progress-warning" : "progress-secondary") + " w-full";
    if (bar.className !== cls) bar.className = cls;
    bar.max = 100;
    if (bar.getAttribute("aria-label") !== "pending" || Number(bar.value) !== 0 || !bar.hasAttribute("value")) {
      bar.value = 0;
      bar.setAttribute("aria-label", "pending");
    }
    return;
  }
  const cls = "progress progress-secondary w-full";
  if (bar.className !== cls) bar.className = cls;
  bar.max = 100;
  const n = progress == null ? NaN : Number(progress);
  // Mid (0,1): determinate. 0% / 100% / nil: daisyUI indeterminate busy slide.
  const mid = Number.isFinite(n) && n > 0 && n < 1;
  if (mid) {
    const pct = Math.max(1, Math.min(99, Math.round(n * 100)));
    if (!bar.hasAttribute("value") || Number(bar.value) !== pct) {
      bar.value = pct;
      bar.setAttribute("aria-label", pct + "%");
    }
    return;
  }
  // Already busy: do not touch value/class (re-removing value restarts animation).
  if (!bar.hasAttribute("value")) {
    if (bar.getAttribute("aria-label") !== "In progress") {
      bar.setAttribute("aria-label", "In progress");
    }
    return;
  }
  bar.removeAttribute("value");
  bar.setAttribute("aria-label", "In progress");
}

/** Patch message/progress on an existing task row. Avoids full panel swap (button flicker). */
export function patchTaskRow(ev) {
  let data;
  try {
    data = JSON.parse(ev.data || "{}");
  } catch (_) {
    return false;
  }
  const id = data.task_id;
  if (!id) return false;
  const row = document.getElementById("task-row-" + id);
  if (!row) return false;
  const prevStatus = row.getAttribute("data-task-row-status") || "";
  const nextStatus = typeof data.status === "string" && data.status ? data.status : "";
  // Progress ticks always include status=running; only act when it actually changes.
  const statusChanged = nextStatus !== "" && nextStatus !== prevStatus;
  if (statusChanged) {
    patchStatusCell(row, nextStatus);
    const panel = lanePanelFor(row);
    const wrap = panel && panel.querySelector("[data-domain-cooldown]");
    if (wrap) applyInferredLaneStatus(wrap);
    if (nextStatus === "running") {
      const pos = row.querySelector("[data-task-pos]");
      if (pos) setTaskPosRunning(pos);
    }
    // Queue #N + To top enabled/disabled come from server (pending-only position).
    refreshTasksPanel(true);
  }
  const msgEl = row.querySelector("[data-task-message]");
  if (msgEl) {
    const st = statusChanged
      ? nextStatus
      : row.getAttribute("data-task-row-status") || "";
    if (st === "pending") {
      msgEl.textContent = "Queued";
    } else if (typeof data.message === "string") {
      msgEl.textContent = data.message || "-";
    }
  }
  const progressChanged = Object.prototype.hasOwnProperty.call(data, "progress");
  if (statusChanged || progressChanged) {
    const st = statusChanged
      ? nextStatus
      : row.getAttribute("data-task-row-status") || "running";
    let progress = null;
    if (progressChanged) {
      progress = data.progress;
    } else {
      const bar = row.querySelector("progress[data-task-progress]");
      if (bar && bar.hasAttribute("value") && Number(bar.max) > 0) {
        progress = Number(bar.value) / Number(bar.max);
      }
    }
    syncTaskRowProgress(row, st, progress);
  }
  return true;
}

/** Patch message/progress on /task/{id} detail page. */
export function patchTaskDetail(ev) {
  let data;
  try {
    data = JSON.parse(ev.data || "{}");
  } catch (_) {
    return false;
  }
  const id = data.task_id;
  if (!id) return false;
  const root = document.querySelector('[data-task-detail="' + id + '"]');
  if (!root) return false;
  const page = root.closest("main") || document;
  const statusChanged = typeof data.status === "string" && data.status;
  if (statusChanged) {
    patchStatusCell(page, data.status);
    if (root.hasAttribute("data-task-row-status")) {
      root.setAttribute("data-task-row-status", data.status);
    }
  }
  const msgEls = page.querySelectorAll("[data-task-message]");
  if (msgEls.length) {
    const st = statusChanged
      ? data.status
      : root.getAttribute("data-task-row-status") ||
        page.querySelector("[data-task-status]")?.getAttribute("aria-label") ||
        "";
    let text = null;
    if (st === "pending") {
      text = "Queued";
    } else if (typeof data.message === "string") {
      text = data.message || "-";
    }
    if (text != null) {
      msgEls.forEach((el) => {
        el.textContent = text;
      });
    }
  }
  const progressChanged = Object.prototype.hasOwnProperty.call(data, "progress");
  if (statusChanged || progressChanged) {
    const st = statusChanged
      ? data.status
      : root.getAttribute("data-task-row-status") || "running";
    if (st === "done" || st === "failed" || st === "cancelled") {
      // Drop progress + Cancel chrome (not just hide the bar).
      const chrome = root.querySelector("[data-task-progress-wrap]")?.closest(".flex-none");
      if (chrome) chrome.remove();
      else {
        const wrap = root.querySelector("[data-task-progress-wrap]");
        if (wrap) wrap.remove();
        root.querySelector('label[for="modal-cancel-task"]')?.remove();
      }
      root.removeAttribute("data-task-row-status");
    } else {
      let progress = null;
      if (progressChanged) {
        progress = data.progress;
      } else {
        const bar = root.querySelector("progress[data-task-progress]");
        if (bar && bar.hasAttribute("value") && Number(bar.max) > 0) {
          progress = Number(bar.value) / Number(bar.max);
        }
      }
      syncTaskRowProgress(root, st, progress);
    }
  }
  return true;
}

let videoHistoryRefreshAt = 0;

let taskHistoryRefreshAt = 0;

/** Refresh video History panel while a related task is progressing. */
export function refreshVideoHistoryIfMatch(ev) {
  if (!window.htmx) return;
  let data;
  try {
    data = JSON.parse(ev.data || "{}");
  } catch (_) {
    return;
  }
  const m = location.pathname.match(/^\/series\/(\d+)\/videos\/(\d+)/);
  if (!m) return;
  const pageVid = Number(m[2]);
  if (!pageVid || !data.video_id || Number(data.video_id) !== pageVid) return;
  if (!document.getElementById("video-history-live")) return;
  const now = Date.now();
  if (now - videoHistoryRefreshAt < 1500) return;
  videoHistoryRefreshAt = now;
  const q = location.search || "";
  window.htmx.ajax("GET", location.pathname + q, {
    target: "#video-history-live",
    select: "#video-history-live",
    swap: "outerHTML",
  });
}

/** Refresh Stages/Commands/Detail on /task/{id} while that task progresses. */
export function refreshTaskVideoHistoryIfMatch(ev) {
  if (!window.htmx) return;
  let data;
  try {
    data = JSON.parse(ev.data || "{}");
  } catch (_) {
    return;
  }
  const id = taskDetailID();
  if (!id || Number(data.task_id) !== id) return;
  if (!document.getElementById("task-detail-live")) return;
  const now = Date.now();
  if (now - taskHistoryRefreshAt < 1500) return;
  taskHistoryRefreshAt = now;
  const q = location.search || "";
  window.htmx.ajax("GET", location.pathname + q, {
    target: "#task-detail-live",
    select: "#task-detail-live",
    swap: "outerHTML",
  });
}

function taskDetailID() {
  const m = location.pathname.match(/^\/task\/(\d+)\/?$/);
  return m ? Number(m[1]) : 0;
}

export function reloadTaskDetailIfMatch(ev) {
  let data;
  try {
    data = JSON.parse(ev.data || "{}");
  } catch (_) {
    return;
  }
  const id = taskDetailID();
  if (!id || Number(data.task_id) !== id) return;
  location.reload();
}

/** Full reload when a task for this video finishes (status, files, actions). */
export function reloadVideoDetailIfMatch(ev) {
  let data;
  try {
    data = JSON.parse(ev.data || "{}");
  } catch (_) {
    return;
  }
  // Ephemeral metadata fetch finishes in-modal via HTMX poll; reload would close the editor.
  const kind = data.kind || "";
  if (
    kind === "prefetch_video_meta" ||
    kind === "prefetch_series_meta" ||
    kind === "prefetch_add_series" ||
    kind === "prefetch_add_video"
  ) {
    return;
  }
  const m = location.pathname.match(/^\/series\/(\d+)\/videos\/(\d+)/);
  if (!m) return;
  const pageVid = Number(m[2]);
  if (!pageVid || !data.video_id || Number(data.video_id) !== pageVid) return;
  Promise.resolve(flushNotesAutosave())
    .catch(() => {})
    .finally(() => {
      location.reload();
    });
}

export function refreshTaskIndicators() {
  if (!window.htmx) return;
  const videoPage = location.pathname.match(/^\/series\/(\d+)\/videos\/(\d+)/);
  if (videoPage) {
    window.htmx.ajax("GET", "/series/" + videoPage[1] + "/videos/" + videoPage[2] + "/task-indicator", {
      target: "body",
      swap: "none",
    });
    return;
  }
  const sourcePage = location.pathname.match(/^\/series\/(\d+)\/sources\/(\d+)/);
  if (sourcePage) {
    window.htmx.ajax("GET", "/series/" + sourcePage[1] + "/task-indicators", {
      target: "body",
      swap: "none",
    });
    return;
  }
  const seriesPage = location.pathname.match(/^\/series\/(\d+)\/?$/);
  if (seriesPage) {
    const q = location.search || "";
    window.htmx.ajax("GET", "/series/" + seriesPage[1] + "/task-indicators" + q, {
      target: "body",
      swap: "none",
    });
  }
}

/** Stable fingerprint for task-indicator OOB skips (avoids tooltip flicker on poll/SSE). */
function taskIndicatorFingerprint(el) {
  if (!el || !el.getAttribute) return "";
  const tip =
    el.getAttribute("data-tip") ||
    (el.querySelector && el.querySelector("[data-tip]") && el.querySelector("[data-tip]").getAttribute("data-tip")) ||
    "";
  const busy = el.getAttribute("aria-busy") || "";
  const tipOn = el.classList && el.classList.contains("tooltip") ? "1" : "0";
  let state = "empty";
  const radial = el.querySelector && el.querySelector(".radial-progress");
  if (el.querySelector && el.querySelector(".loading")) {
    state = "spin";
  } else if (radial) {
    state = "prog:" + (radial.style.getPropertyValue("--value") || "").trim();
  } else {
    const lucideEl = el.querySelector && el.querySelector("[data-lucide]");
    if (lucideEl) {
      state = "icon:" + (lucideEl.getAttribute("data-lucide") || "");
    } else {
      const svg = el.querySelector && el.querySelector("svg.lucide, svg[class*='lucide-']");
      if (svg) {
        const cls = [...svg.classList].find((c) => c.startsWith("lucide-") && c !== "lucide");
        state = "icon:" + (cls ? cls.slice("lucide-".length) : "");
      }
    }
  }
  return [tip, busy, tipOn, state].join("|");
}

/** Source status OOB: icon fingerprint alone misses label / tooltip-content updates after scan. */
function sourceStatusFingerprint(el) {
  if (!el || !el.querySelector) return "";
  const labelEl = el.querySelector("a.link") || el.querySelector("span.truncate");
  const tipEl = el.querySelector(".tooltip-content");
  const label = labelEl ? labelEl.textContent.trim() : "";
  const tipBody = tipEl ? tipEl.textContent.trim() : "";
  return taskIndicatorFingerprint(el) + "|" + label + "|" + tipBody;
}

/** Scan/Full-scan join buttons: skip identical OOB so hover tips do not flicker on download SSE. */
function sourceScanActionsFingerprint(el) {
  if (!el || !el.querySelectorAll) return "";
  const tips = [...el.querySelectorAll("[data-tip]")].map((n) => n.getAttribute("data-tip") || "");
  const icons = [...el.querySelectorAll("[data-lucide]")].map((n) => n.getAttribute("data-lucide") || "");
  const disabled = el.querySelectorAll(".btn-disabled, [aria-disabled='true']").length;
  const enabled = el.querySelectorAll("button[type='submit']:not(:disabled)").length;
  return [tips.join(";"), icons.join(";"), disabled, enabled].join("|");
}

export function bootTaskrows() {
  // Poll/SSE OOB-replaces every task-indicator; identical swaps reset :hover and tip flickers.
  document.body.addEventListener("htmx:oobBeforeSwap", (ev) => {
    const detail = ev.detail || {};
    const target = detail.target;
    let incoming = detail.fragment;
    if (incoming && incoming.nodeType === 11) incoming = incoming.firstElementChild;
    if (!target || !incoming) return;
    // Edit series Title/Root lock: skip while modal open; skip when busy flag unchanged.
    if (target.id === "edit-series-settings-fields") {
      const modal = document.getElementById("modal-edit-series");
      if (modal && modal.checked) {
        detail.shouldSwap = false;
        return;
      }
      if (
        target.getAttribute("data-folder-rename-busy") ===
        incoming.getAttribute("data-folder-rename-busy")
      ) {
        detail.shouldSwap = false;
      }
      return;
    }
    if (
      target.classList.contains("source-status-cell") &&
      incoming.classList.contains("source-status-cell")
    ) {
      if (sourceStatusFingerprint(target) === sourceStatusFingerprint(incoming)) {
        detail.shouldSwap = false;
      }
      return;
    }
    if (
      target.classList.contains("source-scan-actions") &&
      incoming.classList.contains("source-scan-actions")
    ) {
      if (sourceScanActionsFingerprint(target) === sourceScanActionsFingerprint(incoming)) {
        detail.shouldSwap = false;
      }
      return;
    }
    if (target.classList.contains("task-indicator") && incoming.classList.contains("task-indicator")) {
      if (taskIndicatorFingerprint(target) === taskIndicatorFingerprint(incoming)) {
        detail.shouldSwap = false;
      }
    }
  });
}
