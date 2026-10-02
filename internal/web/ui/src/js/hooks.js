import { syncAddSeriesForm } from "./add_series.js";
import { markAllNotificationsRead, refreshBadge, refreshNotifyBadge, refreshNotifyDropdown } from "./badges.js";
import { initQualityProfileGate, initRangeOutputs, initSponsorBlockExclusive, initSponsorBlockReencodeGate } from "./controls.js";
import { createLucideIcons, formatLocalTimes, htmxSwapRoot, scrollTaskLogsToBottom } from "./dom_helpers.js";
import { initFlashToasts, scheduleFlashToasts } from "./flash.js";
import { snapshotStringListEditors } from "./form_reset.js";
import { syncAllMaturityJoins, syncAllPackRoleJoins, syncAllRateLimitJoins, syncAllScanCronJoins } from "./joins.js";
import { currentKeepScrollRedirect, restoreSeriesScroll, saveKeepScroll, scrollSeriesVideosAnchor, shouldKeepScrollForm, syncKeepScrollRedirect } from "./keep_scroll.js";
import { refreshTasksPanel, restoreTasksLiveAfterSwap, stashTasksLiveBeforeSwap } from "./lanes.js";
import { captureListFilterQFocus, restoreListFilterQFocus } from "./list_filter.js";
import { wireMaintenanceScope } from "./maintenance.js";
import { connectEvents } from "./sse.js";
import { openAddSeriesModal, openSeriesMetadataModal } from "./validity.js";

const LIST_LIVE_IDS = new Set([
  "series-videos-live",
  "series-list-live",
  "videos-list-live",
  "sources-list-live",
]);

function isListLiveEl(el) {
  return !!(el && el.id && LIST_LIVE_IDS.has(el.id));
}

function clearListLiveScrollDatasets() {
  delete document.body.dataset.listLiveScrollY;
  delete document.body.dataset.listLiveAnchorTop;
  delete document.body.dataset.listLiveScrollTarget;
}

/** Keep the live panel's viewport position across outerHTML swap (height collapse clamps scrollY). */
function pinListLiveScroll(target) {
  if (!isListLiveEl(target)) return;
  document.body.dataset.listLiveScrollY = String(window.scrollY);
  document.body.dataset.listLiveAnchorTop = String(target.getBoundingClientRect().top);
  captureListFilterQFocus();
}

function restoreListLiveScroll(root) {
  if (!isListLiveEl(root)) return;
  const scrollTo = document.body.dataset.listLiveScrollTarget;
  const anchor = Number(document.body.dataset.listLiveAnchorTop);
  const y = Number(document.body.dataset.listLiveScrollY);
  clearListLiveScrollDatasets();
  // Infinite keep-depth clamp: list_infinite.js scrolls to list top instead.
  if (root.getAttribute("data-through-clamped") === "1") {
    restoreListFilterQFocus(root);
    return;
  }
  if (scrollTo && root.id === scrollTo) {
    requestAnimationFrame(() => root.scrollIntoView({ block: "start" }));
    restoreListFilterQFocus(root);
    return;
  }
  const apply = () => {
    if (Number.isFinite(anchor)) {
      const delta = root.getBoundingClientRect().top - anchor;
      if (delta !== 0) window.scrollBy(0, delta);
      return;
    }
    if (Number.isFinite(y)) window.scrollTo(0, y);
  };
  apply();
  // Second pass after layout/focus settle (swap can clamp scrollY mid-frame).
  requestAnimationFrame(() => requestAnimationFrame(apply));
  restoreListFilterQFocus(root);
}

export function bootHooks() {
  document.addEventListener("DOMContentLoaded", () => {
    createLucideIcons();
    formatLocalTimes();
    restoreSeriesScroll();
    scrollSeriesVideosAnchor();
    refreshBadge();
    refreshNotifyBadge();
    refreshNotifyDropdown();
    const markAllBtn = document.getElementById("notify-mark-all-read");
    if (markAllBtn) {
      markAllBtn.addEventListener("click", (ev) => {
        ev.preventDefault();
        markAllNotificationsRead();
      });
    }
    document.querySelectorAll("[data-notify-redirect]").forEach((el) => {
      el.value = location.pathname + location.search + location.hash;
    });
    document.querySelectorAll("input[name='redirect'][data-keep-scroll-redirect]").forEach((el) => {
      el.value = currentKeepScrollRedirect();
    });
    connectEvents();
    initFlashToasts();
    initRangeOutputs();
    initSponsorBlockExclusive();
    initSponsorBlockReencodeGate();
    initQualityProfileGate();
    syncAllRateLimitJoins();
    syncAllScanCronJoins();
    syncAllMaturityJoins();
    syncAllPackRoleJoins();
    snapshotStringListEditors(document);
    document.querySelectorAll("form.js-add-series-form").forEach(syncAddSeriesForm);
    openAddSeriesModal();
    openSeriesMetadataModal();
    wireMaintenanceScope();
    // Slow fallback if SSE unavailable.
    setInterval(() => {
      refreshBadge();
      refreshNotifyBadge();
      if (document.getElementById("tasks-list-live")) refreshTasksPanel(false);
    }, 15000);
  });

  document.body.addEventListener("htmx:beforeRequest", (ev) => {
    const cfg = ev.detail && ev.detail.requestConfig;
    const target = (cfg && cfg.target) || (ev.detail && ev.detail.target);
    if (!isListLiveEl(target)) return;
    pinListLiveScroll(target);
    const elt = (cfg && cfg.elt) || (ev.detail && ev.detail.elt);
    const scrollTo = elt && elt.getAttribute && elt.getAttribute("data-scroll-after-swap");
    if (scrollTo) document.body.dataset.listLiveScrollTarget = scrollTo;
    else delete document.body.dataset.listLiveScrollTarget;
  });

  // Soft #tasks-list-live refresh: keep tip hosts that did not change so hover tips do not flicker.
  document.body.addEventListener("htmx:beforeSwap", (ev) => {
    const target = ev.detail && ev.detail.target;
    stashTasksLiveBeforeSwap(target);
    if (!isListLiveEl(target)) return;
    // Refresh pin immediately before detach; blur so focus loss does not yank the viewport.
    pinListLiveScroll(target);
    const ae = document.activeElement;
    if (ae && target.contains(ae) && typeof ae.blur === "function") ae.blur();
  });

  document.body.addEventListener("htmx:afterSwap", (ev) => {
    const root = htmxSwapRoot(ev);
    createLucideIcons(root);
    formatLocalTimes(root);
    scrollTaskLogsToBottom(root);
    restoreTasksLiveAfterSwap(root);
    if (root && root.id === "tasks-list-live") {
      root.querySelectorAll("input[name='redirect'][data-keep-scroll-redirect]").forEach((el) => {
        el.value = currentKeepScrollRedirect();
      });
    }
    if (root && (root.id === "maintenance-live" || root.querySelector?.("#maintenance-live"))) {
      wireMaintenanceScope();
    }
    if (root && (root.id === "video-metadata-body" || root.querySelector?.("[data-pack-role-join]"))) {
      syncAllPackRoleJoins(root);
    }
    if (document.body.dataset.listLiveScrollY != null || document.body.dataset.listLiveAnchorTop != null) {
      restoreListLiveScroll(root);
    }
  });

  document.body.addEventListener("htmx:oobAfterSwap", (ev) => {
    const root = htmxSwapRoot(ev);
    createLucideIcons(root);
    formatLocalTimes(root);
    scheduleFlashToasts();
  });

  document.body.addEventListener("click", (ev) => {
    const a = ev.target.closest("a.js-keep-scroll");
    if (!a || a.hasAttribute("hx-get") || a.hasAttribute("hx-post")) return;
    saveKeepScroll();
  });

  document.body.addEventListener("submit", (ev) => {
    const form = ev.target.closest("form");
    if (!form) return;
    syncKeepScrollRedirect(form);
    if (!shouldKeepScrollForm(form)) return;
    saveKeepScroll();
  });
}
