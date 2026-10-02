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
      if (document.getElementById("tasks-live")) refreshTasksPanel(false);
    }, 15000);
  });

  document.body.addEventListener("htmx:beforeRequest", (ev) => {
    const cfg = ev.detail && ev.detail.requestConfig;
    const target = (cfg && cfg.target) || (ev.detail && ev.detail.target);
    if (
      target &&
      (target.id === "series-videos-live" ||
        target.id === "series-list-live" ||
        target.id === "videos-list-live")
    ) {
      document.body.dataset.listLiveScrollY = String(window.scrollY);
      captureListFilterQFocus();
    }
  });

  // Soft #tasks-live refresh: keep tip hosts that did not change so hover tips do not flicker.
  document.body.addEventListener("htmx:beforeSwap", (ev) => {
    stashTasksLiveBeforeSwap(ev.detail && ev.detail.target);
  });

  document.body.addEventListener("htmx:afterSwap", (ev) => {
    const root = htmxSwapRoot(ev);
    createLucideIcons(root);
    formatLocalTimes(root);
    scrollTaskLogsToBottom(root);
    restoreTasksLiveAfterSwap(root);
    if (root && root.id === "tasks-live") {
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
    const y = document.body.dataset.listLiveScrollY;
    if (
      y != null &&
      root &&
      (root.id === "series-videos-live" ||
        root.id === "series-list-live" ||
        root.id === "videos-list-live")
    ) {
      delete document.body.dataset.listLiveScrollY;
      // Infinite keep-depth clamp: list_infinite.js scrolls to list top instead.
      if (root.getAttribute("data-through-clamped") === "1") {
        restoreListFilterQFocus(root);
        return;
      }
      const top = Number(y);
      if (Number.isFinite(top)) requestAnimationFrame(() => window.scrollTo(0, top));
      restoreListFilterQFocus(root);
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
