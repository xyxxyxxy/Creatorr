/** Infinite list load mode: through URL + bulk resync after append/full swap. */
//! pin: duplicate data-video-id / data-series-id under #videos-list-rows / #series-list-rows after rapid revealed is a regression

import { refreshSeriesBulkAfterDOM } from "./series_bulk.js";
import { refreshVideoBulkAfterDOM } from "./video_bulk.js";

function syncThroughURL(live) {
  if (!live || live.getAttribute("data-list-mode") !== "infinite") return;
  const through = parseInt(live.getAttribute("data-loaded-through") || "1", 10);
  const u = new URL(location.href);
  if (!Number.isFinite(through) || through <= 1) {
    if (!u.searchParams.has("through")) return;
    u.searchParams.delete("through");
  } else {
    u.searchParams.set("through", String(through));
  }
  history.replaceState(history.state, "", u.pathname + u.search + u.hash);
}

function updateLoadedCount(live) {
  const rowHost =
    live.querySelector("#videos-list-rows") || live.querySelector("#series-list-rows");
  if (!rowHost) return;
  const n = rowHost.querySelectorAll(":scope > [data-video-id], :scope > [data-series-id]").length;
  live.setAttribute("data-loaded-count", String(n));
}

function resyncBulk(live) {
  if (!live) return;
  if (live.id === "videos-list-live") refreshVideoBulkAfterDOM();
  else if (live.id === "series-list-live") refreshSeriesBulkAfterDOM();
}

function scrollLiveToTop(live) {
  if (!live) return;
  const top = live.getBoundingClientRect().top + window.scrollY - 8;
  requestAnimationFrame(() => window.scrollTo(0, Math.max(0, top)));
}

function onInfiniteAfterSwap(ev) {
  const detail = ev.detail || {};
  const target = detail.target;
  const elt = detail.elt;

  // Append: sentinel self-replaced (requesting elt had -infinite id).
  if (elt && elt.id && String(elt.id).endsWith("-infinite")) {
    const live =
      (target && target.closest && target.closest("[data-list-mode='infinite']")) ||
      document.getElementById("videos-list-live") ||
      document.getElementById("series-list-live");
    if (live && live.getAttribute("data-list-mode") === "infinite") {
      const page = parseInt(elt.getAttribute("data-infinite-page") || "0", 10);
      if (Number.isFinite(page) && page > 0) {
        live.setAttribute("data-loaded-through", String(page));
      }
      live.setAttribute("data-through-clamped", "0");
      updateLoadedCount(live);
      syncThroughURL(live);
      resyncBulk(live);
    }
    return;
  }

  // Full live outerHTML swap of an infinite root.
  if (target && target.getAttribute && target.getAttribute("data-list-mode") === "infinite") {
    const clamped = target.getAttribute("data-through-clamped") === "1";
    syncThroughURL(target);
    resyncBulk(target);
    if (clamped) {
      delete document.body.dataset.listLiveScrollY;
      delete document.body.dataset.listLiveAnchorTop;
      delete document.body.dataset.listLiveScrollTarget;
      scrollLiveToTop(target);
    }
  }
}

export function bootListInfinite() {
  document.body.addEventListener("htmx:afterSwap", onInfiniteAfterSwap);
}
