import { fillBulkActors, fillBulkStringList, resetBulkMetadataForm } from "./series_bulk.js";

// --- Video list bulk selection (series detail + library /videos) ---
const videoBulkSelected = new Set();

let videoBulkMode = false;

// Row click target for list + cards/gallery on both live roots.
const VIDEO_BULK_ROW = "#videos-list-rows > [data-video-id]";

function videoBulkLive() {
  // Browser Videos only; series-detail Videos is a locked glance (no multi-select).
  return document.getElementById("videos-list-live");
}

function videoBulkFilterTotal() {
  const live = videoBulkLive();
  if (!live) return 0;
  const n = parseInt(live.getAttribute("data-filter-total") || "0", 10);
  return Number.isFinite(n) ? n : 0;
}

function videoBulkSeriesID() {
  // Bulk runs on Browser Videos only (library-wide ids endpoint).
  return "";
}

function videoBulkPageCheckboxes() {
  const live = videoBulkLive();
  if (!live) return [];
  return Array.from(live.querySelectorAll(".js-video-select"));
}

function fillVideoBulkIDs(root) {
  const hosts = (root || document).querySelectorAll("[data-bulk-video-ids]");
  hosts.forEach((host) => {
    host.replaceChildren();
    videoBulkSelected.forEach((id) => {
      const input = document.createElement("input");
      input.type = "hidden";
      input.name = "video_id";
      input.value = String(id);
      host.appendChild(input);
    });
  });
}

function setVideoBulkMode(on) {
  videoBulkMode = !!on;
  if (!videoBulkMode) videoBulkSelected.clear();
  syncVideoBulkUI();
}

function toggleVideoBulkID(id) {
  if (!id) return;
  const live = videoBulkLive();
  const cb0 = live
    ? live.querySelector('.js-video-select[value="' + CSS.escape(id) + '"]')
    : null;
  if (cb0 instanceof HTMLInputElement && cb0.disabled) return;
  if (videoBulkSelected.has(id)) videoBulkSelected.delete(id);
  else videoBulkSelected.add(id);
  if (cb0 instanceof HTMLInputElement) cb0.checked = videoBulkSelected.has(id);
  syncVideoBulkUI();
}

function syncVideoBulkUI() {
  const live = videoBulkLive();
  if (!live) return;
  live.setAttribute("data-bulk-mode", videoBulkMode ? "1" : "0");
  const modeBtn = live.querySelector("[data-video-bulk-mode]");
  if (modeBtn) {
    modeBtn.setAttribute("aria-pressed", videoBulkMode ? "true" : "false");
    const wrap = modeBtn.closest(".js-list-toolbar-dd");
    if (wrap) wrap.classList.toggle("is-bulk-on", videoBulkMode);
    modeBtn.setAttribute("aria-label", videoBulkMode ? "Exit multi-select" : "Multi-select");
  }
  live.querySelectorAll("[data-video-select-wrap]").forEach((wrap) => {
    wrap.classList.toggle("hidden", !videoBulkMode);
  });
  // Keep per-row actions visible in multi-select; disable so bulk bar is the only path.
  // Tip lives on the wrap: disabled buttons + pointer-events-none children kill daisyUI tips.
  const rowActionsDisabled = videoBulkMode;
  const rowActionsTip = "Use the multi-select bar";
  live.querySelectorAll("[data-video-row-actions]").forEach((wrap) => {
    wrap.querySelectorAll("button").forEach((btn) => {
      if (rowActionsDisabled) {
        if (!btn.hasAttribute("data-bulk-prev-disabled")) {
          btn.setAttribute("data-bulk-prev-disabled", btn.disabled ? "1" : "0");
        }
        btn.disabled = true;
      } else {
        const prev = btn.getAttribute("data-bulk-prev-disabled");
        if (prev !== null) {
          btn.disabled = prev === "1";
          btn.removeAttribute("data-bulk-prev-disabled");
        }
      }
    });
    wrap.querySelectorAll("label[for], label[data-modal-for]").forEach((el) => {
      if (!el.hasAttribute("data-modal-for")) {
        const f = el.getAttribute("for");
        if (f) el.setAttribute("data-modal-for", f);
      }
      el.classList.toggle("btn-disabled", rowActionsDisabled);
      el.classList.toggle("pointer-events-none", rowActionsDisabled);
      el.setAttribute("aria-disabled", rowActionsDisabled ? "true" : "false");
      if (rowActionsDisabled) {
        el.removeAttribute("for");
      } else {
        const modalFor = el.getAttribute("data-modal-for");
        if (modalFor) el.setAttribute("for", modalFor);
      }
    });
    // Tip host is [data-video-row-actions] (never a td - docs/ui.md Tooltips).
    if (rowActionsDisabled) {
      wrap.classList.add("tooltip", "tooltip-top");
      wrap.setAttribute("data-tip", rowActionsTip);
      wrap.querySelectorAll("[data-tip]").forEach((el) => {
        if (el === wrap) return;
        if (!el.hasAttribute("data-bulk-prev-tip")) {
          el.setAttribute("data-bulk-prev-tip", el.getAttribute("data-tip") || "");
          el.setAttribute(
            "data-bulk-prev-tooltip",
            el.classList.contains("tooltip") ? "1" : "0"
          );
        }
        el.removeAttribute("data-tip");
        el.classList.remove("tooltip", "tooltip-top");
      });
    } else {
      wrap.classList.remove("tooltip", "tooltip-top");
      wrap.removeAttribute("data-tip");
      wrap.querySelectorAll("[data-bulk-prev-tip]").forEach((el) => {
        const prev = el.getAttribute("data-bulk-prev-tip");
        const hadTip = el.getAttribute("data-bulk-prev-tooltip") === "1";
        el.removeAttribute("data-bulk-prev-tip");
        el.removeAttribute("data-bulk-prev-tooltip");
        if (prev) el.setAttribute("data-tip", prev);
        if (hadTip) el.classList.add("tooltip", "tooltip-top");
      });
    }
  });
  live.querySelectorAll(VIDEO_BULK_ROW).forEach((row) => {
    row.classList.toggle("cursor-pointer", videoBulkMode);
    const id = row.getAttribute("data-video-id");
    const selected = videoBulkMode && videoBulkSelected.has(id);
    const isCard = row.classList.contains("card");
    row.classList.toggle("bg-base-200", selected && !isCard);
    // Cards: accent outline (image fills the face; bg fill is almost invisible).
    //! pin: selected card outline outline-accent
    row.classList.toggle("outline", selected && isCard);
    row.classList.toggle("outline-2", selected && isCard);
    row.classList.toggle("outline-offset-2", selected && isCard);
    row.classList.toggle("outline-accent", selected && isCard);
    if (row.classList.contains("list-row")) {
      row.classList.toggle("rounded-none", selected);
      if (videoBulkMode) {
        row.style.setProperty("--list-grid-cols", "max-content minmax(0, auto) 1fr max-content");
      } else {
        row.style.setProperty("--list-grid-cols", "minmax(0, auto) 1fr max-content");
      }
      row.querySelectorAll(".list-col-grow a[href]").forEach((a) => {
        a.classList.toggle("link", !videoBulkMode);
        a.classList.toggle("link-hover", !videoBulkMode);
      });
    } else {
      row.querySelectorAll(".card-body a[href]").forEach((a) => {
        a.classList.toggle("link", !videoBulkMode);
        a.classList.toggle("link-hover", !videoBulkMode);
      });
    }
  });
  const bar = live.querySelector("[data-video-bulk-bar]");
  if (bar) {
    bar.classList.toggle("hidden", !videoBulkMode);
    const wrap = document.getElementById("video-bulk-bar-wrap");
    if (wrap) wrap.classList.toggle("hidden", !videoBulkMode);
    const n = videoBulkSelected.size;
    const m = videoBulkFilterTotal();
    const countEl = bar.querySelector("[data-video-bulk-count]");
    if (countEl) countEl.textContent = n + "/" + m;
    bar
      .querySelectorAll(
        "[data-video-bulk-want], [data-video-bulk-ignore], [data-video-bulk-refresh], [data-video-bulk-metadata], [data-video-bulk-delete], [data-video-bulk-clear-errors]"
      )
      .forEach((btn) => {
        btn.disabled = n === 0;
      });
    const selectAllBtn = bar.querySelector("[data-video-select-all-matching]");
    if (selectAllBtn) {
      selectAllBtn.disabled = m > 0 && n >= m;
    }
  }
  const pageBoxes = videoBulkPageCheckboxes();
  pageBoxes.forEach((cb) => {
    cb.checked = videoBulkSelected.has(cb.value);
  });
  if (bar) {
    const unlocked = pageBoxes.filter((cb) => !cb.disabled);
    const pageAll =
      unlocked.length > 0 && unlocked.every((cb) => videoBulkSelected.has(cb.value));
    const pageCb = document.getElementById("video-select-page");
    if (pageCb) {
      pageCb.checked = pageAll;
      pageCb.indeterminate = !pageAll && unlocked.some((cb) => videoBulkSelected.has(cb.value));
      pageCb.disabled = unlocked.length === 0;
    }
  }
  fillVideoBulkIDs(document);
}

function restoreVideoBulkCheckboxes() {
  syncVideoBulkUI();
}

function openVideoBulkModal(id) {
  const toggle = document.getElementById(id);
  if (toggle) toggle.checked = true;
}

/** Confirm modal only when selection exceeds this (≤5 submit immediately). */
const VIDEO_BULK_CONFIRM_AFTER = 5;

function submitVideoBulkForm(formId) {
  fillVideoBulkIDs(document);
  const form = document.getElementById(formId);
  if (form) form.requestSubmit();
}

async function hydrateVideoBulkMetadataForm() {
  const form = document.getElementById("form-bulk-edit-videos-metadata");
  if (!form) return;
  resetBulkMetadataForm(form);
  const ids = Array.from(videoBulkSelected)
    .map((id) => parseInt(id, 10))
    .filter((n) => Number.isFinite(n) && n > 0);
  if (ids.length === 0) return;
  try {
    const resp = await fetch("/videos/bulk-metadata-common", {
      method: "POST",
      headers: { Accept: "application/json", "Content-Type": "application/json" },
      body: JSON.stringify({ ids }),
    });
    if (!resp.ok) return;
    const data = await resp.json();
    const setIfSame = (name, field) => {
      if (!field || !field.same) return;
      const v = String(field.value || "").trim();
      if (!v) return;
      const el = form.querySelector('input[name="' + name + '"]');
      if (el) el.value = v;
    };
    setIfSame("studio", data.studio);
    setIfSame("country", data.country);
    setIfSame("mpaa", data.mpaa);
    //! Special kind stays No change unless the operator picks a value (shared kind is not prefilled).
    if (data.genres && data.genres.same && Array.isArray(data.genres.value) && data.genres.value.length) {
      form.querySelectorAll("[data-string-list-editor]").forEach((ed) => {
        if (ed.getAttribute("data-item-name") === "genre") fillBulkStringList(ed, data.genres.value);
      });
    }
    if (data.tags && data.tags.same && Array.isArray(data.tags.value) && data.tags.value.length) {
      form.querySelectorAll("[data-string-list-editor]").forEach((ed) => {
        if (ed.getAttribute("data-item-name") === "tag") fillBulkStringList(ed, data.tags.value);
      });
    }
    if (data.actors && data.actors.same && Array.isArray(data.actors.value) && data.actors.value.length) {
      fillBulkActors(form.querySelector("[data-actors-editor]"), data.actors.value);
    }
  } catch (_) {
    /* leave empty No-change form */
  }
}

function runVideoBulkAction(action) {
  if (!videoBulkMode || videoBulkSelected.size === 0) return;
  const n = videoBulkSelected.size;
  const m = videoBulkFilterTotal();
  fillVideoBulkIDs(document);
  const setTitle = (sel, text) => {
    const el = document.querySelector(sel);
    if (el) el.textContent = text;
  };
  const needConfirm = n > VIDEO_BULK_CONFIRM_AFTER;
  if (action === "want") {
    if (!needConfirm) {
      submitVideoBulkForm("form-bulk-want-videos");
      return;
    }
    setTitle("[data-video-bulk-want-title]", "Want " + n + "/" + m + " videos");
    openVideoBulkModal("modal-bulk-want-videos");
    return;
  }
  if (action === "clear-errors") {
    if (!needConfirm) {
      submitVideoBulkForm("form-bulk-clear-download-errors");
      return;
    }
    setTitle("[data-video-bulk-clear-errors-title]", "Clear selected errors (" + n + "/" + m + ")");
    openVideoBulkModal("modal-bulk-clear-download-errors");
    return;
  }
  if (action === "ignore") {
    if (!needConfirm) {
      submitVideoBulkForm("form-bulk-ignore-videos");
      return;
    }
    setTitle("[data-video-bulk-ignore-title]", "Ignore " + n + "/" + m + " videos");
    openVideoBulkModal("modal-bulk-ignore-videos");
    return;
  }
  if (action === "refresh") {
    if (!needConfirm) {
      submitVideoBulkForm("form-bulk-refresh-sidecars-videos");
      return;
    }
    setTitle("[data-video-bulk-refresh-title]", "Refresh sidecars (" + n + "/" + m + ")");
    openVideoBulkModal("modal-bulk-refresh-sidecars-videos");
    return;
  }
  if (action === "metadata") {
    // Edit form always opens (not a yes/no confirm).
    setTitle("[data-video-bulk-meta-title]", "Edit metadata (" + n + "/" + m + ")");
    openVideoBulkModal("modal-bulk-edit-videos-metadata");
    hydrateVideoBulkMetadataForm();
    return;
  }
  if (action === "delete") {
    // Delete always confirms.
    setTitle("[data-video-bulk-delete-title]", "Delete downloaded files (" + n + "/" + m + ")");
    openVideoBulkModal("modal-bulk-delete-videos");
  }
}

async function selectAllMatchingVideos() {
  const sid = videoBulkSeriesID();
  const q = location.search || "";
  const url = sid ? "/series/" + sid + "/videos/ids" + q : "/videos/ids" + q;
  const resp = await fetch(url, {
    headers: { Accept: "application/json" },
  });
  if (!resp.ok) throw new Error("failed to load matching ids");
  const data = await resp.json();
  const ids = Array.isArray(data.ids) ? data.ids : [];
  videoBulkSelected.clear();
  ids.forEach((id) => videoBulkSelected.add(String(id)));
  restoreVideoBulkCheckboxes();
}

function onVideoBulkPage() {
  return !!videoBulkLive();
}

/** Re-apply selected Set to checkboxes after infinite append or keep-depth live swap. */
export function refreshVideoBulkAfterDOM() {
  syncVideoBulkUI();
}

export function bootVideoBulk() {
  document.body.addEventListener("change", (ev) => {
    const t = ev.target;
    if (!(t instanceof HTMLInputElement)) return;
    if (t.classList.contains("js-video-select")) {
      if (t.checked) videoBulkSelected.add(t.value);
      else videoBulkSelected.delete(t.value);
      syncVideoBulkUI();
      return;
    }
    if (t.id === "video-select-page") {
      const boxes = videoBulkPageCheckboxes();
      if (t.checked) {
        boxes.forEach((cb) => {
          if (cb.disabled) return;
          videoBulkSelected.add(cb.value);
          cb.checked = true;
        });
      } else {
        const pageIds = new Set(boxes.map((cb) => cb.value));
        const hasOffPage = Array.from(videoBulkSelected).some((id) => !pageIds.has(id));
        if (hasOffPage) {
          videoBulkSelected.clear();
          boxes.forEach((cb) => {
            cb.checked = false;
          });
        } else {
          boxes.forEach((cb) => {
            videoBulkSelected.delete(cb.value);
            cb.checked = false;
          });
        }
      }
      syncVideoBulkUI();
    }
  });

  document.body.addEventListener("click", (ev) => {
    const modeBtn = ev.target.closest("[data-video-bulk-mode]");
    if (modeBtn) {
      ev.preventDefault();
      setVideoBulkMode(!videoBulkMode);
      return;
    }
    if (videoBulkMode) {
      //! pin: thumb+list bulk click #videos-list-rows > [data-video-id]
      const row = ev.target.closest(VIDEO_BULK_ROW);
      if (
        row &&
        !ev.target.closest(".js-video-select, [data-video-select-wrap], [data-video-row-actions]")
      ) {
        ev.preventDefault();
        const id = row.getAttribute("data-video-id");
        if (id) toggleVideoBulkID(id);
        return;
      }
    }
    const selectAll = ev.target.closest("[data-video-select-all-matching]");
    if (selectAll) {
      ev.preventDefault();
      if (selectAll.disabled) return;
      selectAll.disabled = true;
      selectAllMatchingVideos()
        .catch(() => {})
        .finally(() => syncVideoBulkUI());
      return;
    }
    const clearErrors = ev.target.closest("[data-video-bulk-clear-errors]");
    if (clearErrors) {
      ev.preventDefault();
      runVideoBulkAction("clear-errors");
      return;
    }
    const want = ev.target.closest("[data-video-bulk-want]");
    if (want) {
      ev.preventDefault();
      runVideoBulkAction("want");
      return;
    }
    const ignore = ev.target.closest("[data-video-bulk-ignore]");
    if (ignore) {
      ev.preventDefault();
      runVideoBulkAction("ignore");
      return;
    }
    const refresh = ev.target.closest("[data-video-bulk-refresh]");
    if (refresh) {
      ev.preventDefault();
      runVideoBulkAction("refresh");
      return;
    }
    const meta = ev.target.closest("[data-video-bulk-metadata]");
    if (meta) {
      ev.preventDefault();
      runVideoBulkAction("metadata");
      return;
    }
    const del = ev.target.closest("[data-video-bulk-delete]");
    if (del) {
      ev.preventDefault();
      runVideoBulkAction("delete");
    }
  });

  document.body.addEventListener("htmx:beforeRequest", (ev) => {
    if (!onVideoBulkPage() || !videoBulkMode) return;
    const elt = ev.detail && ev.detail.elt;
    if (!elt || !elt.closest) return;
    const live = videoBulkLive();
    if (!live) return;
    const form =
      elt.closest("#" + live.id + " form.js-list-filters") ||
      (elt.matches && elt.matches("#" + live.id + " form.js-list-filters") ? elt : null);
    if (!form) return;
    // Keep multi-select on; clear selection (filter result set changed).
    videoBulkSelected.clear();
    syncVideoBulkUI();
  });

  // afterSwap restores immediately; afterSettle re-applies after HTMX class settle
  // rewrites id'd bar nodes back to the response's `hidden` (table pager swaps).
  const onVideoBulkLiveSwap = (ev) => {
    const target = ev.detail && ev.detail.target;
    if (!target || target.id !== "videos-list-live") return;
    restoreVideoBulkCheckboxes();
  };
  document.body.addEventListener("htmx:afterSwap", onVideoBulkLiveSwap);
  document.body.addEventListener("htmx:afterSettle", onVideoBulkLiveSwap);

  if (onVideoBulkPage()) {
    restoreVideoBulkCheckboxes();
  }
}
