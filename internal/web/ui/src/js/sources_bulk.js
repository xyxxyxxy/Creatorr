// --- Sources Explorer bulk selection (Browser + series-detail embed) ---
const sourcesBulkSelected = new Set();

let sourcesBulkMode = false;

const SOURCES_BULK_ROW = "#sources-list-rows > [data-source-id]";
const SOURCES_BULK_CONFIRM_AFTER = 5;

function sourcesBulkLive() {
  return document.getElementById("sources-list-live");
}

function sourcesBulkFilterTotal() {
  const live = sourcesBulkLive();
  if (!live) return 0;
  const n = parseInt(live.getAttribute("data-filter-total") || "0", 10);
  return Number.isFinite(n) ? n : 0;
}

function sourcesBulkPageCheckboxes() {
  const live = sourcesBulkLive();
  if (!live) return [];
  return Array.from(live.querySelectorAll(".js-source-select"));
}

function fillSourcesBulkIDs(root) {
  const hosts = (root || document).querySelectorAll("[data-bulk-source-ids]");
  hosts.forEach((host) => {
    host.replaceChildren();
    sourcesBulkSelected.forEach((id) => {
      const input = document.createElement("input");
      input.type = "hidden";
      input.name = "source_id";
      input.value = String(id);
      host.appendChild(input);
    });
  });
}

function fillSourcesBulkRedirect() {
  const redir = location.pathname + location.search;
  document.querySelectorAll("[data-bulk-sources-redirect]").forEach((el) => {
    el.value = redir;
  });
}

function setSourcesBulkMode(on) {
  sourcesBulkMode = !!on;
  if (!sourcesBulkMode) sourcesBulkSelected.clear();
  syncSourcesBulkUI();
}

function toggleSourcesBulkID(id) {
  if (!id) return;
  if (sourcesBulkSelected.has(id)) sourcesBulkSelected.delete(id);
  else sourcesBulkSelected.add(id);
  const live = sourcesBulkLive();
  const cb = live
    ? live.querySelector('.js-source-select[value="' + CSS.escape(id) + '"]')
    : null;
  if (cb) cb.checked = sourcesBulkSelected.has(id);
  syncSourcesBulkUI();
}

function syncSourcesBulkUI() {
  const live = sourcesBulkLive();
  if (!live) return;
  live.setAttribute("data-bulk-mode", sourcesBulkMode ? "1" : "0");
  const modeBtn = live.querySelector("[data-sources-bulk-mode]");
  if (modeBtn) {
    modeBtn.setAttribute("aria-pressed", sourcesBulkMode ? "true" : "false");
    const wrap = modeBtn.closest(".js-list-toolbar-dd");
    if (wrap) wrap.classList.toggle("is-bulk-on", sourcesBulkMode);
    modeBtn.setAttribute("data-tip", sourcesBulkMode ? "Exit multi-select" : "Multi-select");
    modeBtn.setAttribute("aria-label", sourcesBulkMode ? "Exit multi-select" : "Multi-select");
  }
  live.querySelectorAll("[data-source-select-wrap]").forEach((wrap) => {
    wrap.classList.toggle("hidden", !sourcesBulkMode);
    if (wrap.tagName === "TH" || wrap.tagName === "TD") {
      wrap.setAttribute("aria-hidden", sourcesBulkMode ? "false" : "true");
    }
  });
  const rowActionsDisabled = sourcesBulkMode;
  const rowActionsTip = "Use the multi-select bar";
  live.querySelectorAll("[data-sources-row-actions]").forEach((wrap) => {
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
    wrap.querySelectorAll("span.btn").forEach((el) => {
      el.classList.toggle("pointer-events-none", rowActionsDisabled);
      if (rowActionsDisabled) el.setAttribute("aria-disabled", "true");
    });
    if (rowActionsDisabled) {
      wrap.classList.add("tooltip", "tooltip-left");
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
        el.classList.remove("tooltip", "tooltip-top", "tooltip-left");
      });
    } else {
      wrap.classList.remove("tooltip", "tooltip-left");
      wrap.removeAttribute("data-tip");
      wrap.querySelectorAll("[data-bulk-prev-tip]").forEach((el) => {
        const prev = el.getAttribute("data-bulk-prev-tip");
        const hadTip = el.getAttribute("data-bulk-prev-tooltip") === "1";
        el.removeAttribute("data-bulk-prev-tip");
        el.removeAttribute("data-bulk-prev-tooltip");
        if (prev) el.setAttribute("data-tip", prev);
        if (hadTip) el.classList.add("tooltip", "tooltip-left");
      });
    }
  });
  live.querySelectorAll(SOURCES_BULK_ROW).forEach((row) => {
    row.classList.toggle("cursor-pointer", sourcesBulkMode);
    const id = row.getAttribute("data-source-id");
    const selected = sourcesBulkMode && sourcesBulkSelected.has(id);
    row.classList.toggle("bg-base-200", selected);
    if (row.classList.contains("list-row")) {
      row.classList.toggle("rounded-none", selected);
      const hasActions = !!row.querySelector("[data-sources-row-actions]");
      const hasSelect = !!row.querySelector("[data-source-select-wrap]");
      // Match sources_list_row: no Status track. Browser keeps a select column;
      // series-detail is grow + actions only.
      let cols = hasActions ? "1fr max-content" : "1fr";
      if (hasSelect) {
        cols = hasActions ? "max-content 1fr max-content" : "max-content 1fr";
      }
      row.style.setProperty("--list-grid-cols", cols);
      row.querySelectorAll(".list-col-grow a[href]").forEach((a) => {
        a.classList.toggle("link", !sourcesBulkMode);
        a.classList.toggle("link-hover", !sourcesBulkMode);
      });
    }
  });
  const bar = live.querySelector("[data-sources-bulk-bar]");
  if (bar) {
    bar.classList.toggle("hidden", !sourcesBulkMode);
    const wrap = document.getElementById("sources-bulk-bar-wrap");
    if (wrap) wrap.classList.toggle("hidden", !sourcesBulkMode);
    const n = sourcesBulkSelected.size;
    const m = sourcesBulkFilterTotal();
    const countEl = bar.querySelector("[data-sources-bulk-count]");
    if (countEl) countEl.textContent = n + "/" + m;
    bar.querySelectorAll("[data-sources-bulk-scan], [data-sources-bulk-delete]").forEach((btn) => {
      btn.disabled = n === 0;
    });
    const selectAllBtn = bar.querySelector("[data-sources-select-all-matching]");
    if (selectAllBtn) {
      selectAllBtn.disabled = m > 0 && n >= m;
    }
  }
  const pageBoxes = sourcesBulkPageCheckboxes();
  pageBoxes.forEach((cb) => {
    cb.checked = sourcesBulkSelected.has(cb.value);
  });
  if (bar) {
    const pageAll =
      pageBoxes.length > 0 && pageBoxes.every((cb) => sourcesBulkSelected.has(cb.value));
    const pageCb = document.getElementById("sources-select-page");
    if (pageCb) {
      pageCb.checked = pageAll;
      pageCb.indeterminate = !pageAll && pageBoxes.some((cb) => sourcesBulkSelected.has(cb.value));
      pageCb.disabled = pageBoxes.length === 0;
    }
  }
  fillSourcesBulkIDs(document);
}

function openSourcesBulkModal(id) {
  const toggle = document.getElementById(id);
  if (toggle) toggle.checked = true;
}

function submitSourcesBulkForm(formId) {
  fillSourcesBulkIDs(document);
  fillSourcesBulkRedirect();
  const form = document.getElementById(formId);
  if (form) form.requestSubmit();
}

function setSourcesBulkScanConfirm(n) {
  const title = document.querySelector("[data-sources-bulk-scan-title]");
  if (title) title.textContent = "Scan " + n + " sources";
  const summary = document.querySelector("[data-sources-bulk-scan-summary]");
  if (summary) {
    summary.textContent =
      "Enqueue tip Scan for " +
      n +
      " matching sources. Already-queued or inactive-domain rows are skipped.";
  }
}

function runSourcesBulkAction(action) {
  if (!sourcesBulkMode || sourcesBulkSelected.size === 0) return;
  const n = sourcesBulkSelected.size;
  const m = sourcesBulkFilterTotal();
  fillSourcesBulkIDs(document);
  fillSourcesBulkRedirect();
  const setTitle = (sel, text) => {
    const el = document.querySelector(sel);
    if (el) el.textContent = text;
  };
  if (action === "scan") {
    // Same threshold as video Want / files Check integrity: ≤5 submit now; more confirms.
    if (n <= SOURCES_BULK_CONFIRM_AFTER) {
      submitSourcesBulkForm("form-bulk-scan-sources");
      return;
    }
    setSourcesBulkScanConfirm(n);
    openSourcesBulkModal("modal-bulk-scan-sources");
    return;
  }
  if (action === "delete") {
    setTitle("[data-sources-bulk-delete-title]", "Delete sources (" + n + "/" + m + ")");
    fillSourcesBulkDeleteImpact()
      .catch(() => {
        setSourcesBulkDeleteImpact(0, 0);
      })
      .finally(() => openSourcesBulkModal("modal-bulk-delete-sources"));
  }
}

function setSourcesBulkDeleteImpact(indexed, downloaded) {
  const idx = Number.isFinite(indexed) ? indexed : 0;
  const dl = Number.isFinite(downloaded) ? downloaded : 0;
  const summary = document.querySelector("[data-sources-bulk-delete-summary]");
  if (summary) {
    summary.textContent =
      "Removes each selected source and all of its indexed videos including library files on disk. Related scan/download tasks are cancelled.";
  }
  const confirm = document.querySelector("[data-sources-bulk-delete-confirm]");
  if (confirm) {
    confirm.textContent =
      "Delete " + dl + " downloaded and " + idx + " indexed videos";
  }
}

async function fillSourcesBulkDeleteImpact() {
  const params = new URLSearchParams();
  sourcesBulkSelected.forEach((id) => params.append("source_id", String(id)));
  const url = "/sources/delete-impact?" + params.toString();
  const resp = await fetch(url, { headers: { Accept: "application/json" } });
  if (!resp.ok) throw new Error("failed to load delete impact");
  const data = await resp.json();
  setSourcesBulkDeleteImpact(data.indexed, data.downloaded);
}

async function selectAllMatchingSources() {
  const live = sourcesBulkLive();
  const q = new URLSearchParams(location.search || "");
  if (live) {
    const sid = live.getAttribute("data-series-id") || "";
    if (sid && sid !== "0" && !q.get("series_id")) q.set("series_id", sid);
  }
  if (!q.get("type")) q.set("type", "sources");
  const qs = q.toString();
  const url = "/sources/ids" + (qs ? "?" + qs : "");
  const resp = await fetch(url, {
    headers: { Accept: "application/json" },
  });
  if (!resp.ok) throw new Error("failed to load matching ids");
  const data = await resp.json();
  const ids = Array.isArray(data.ids) ? data.ids : [];
  sourcesBulkSelected.clear();
  ids.forEach((id) => sourcesBulkSelected.add(String(id)));
  syncSourcesBulkUI();
}

function onSourcesBulkPage() {
  return !!sourcesBulkLive();
}

export function refreshSourcesBulkAfterDOM() {
  syncSourcesBulkUI();
}

export function bootSourcesBulk() {
  document.body.addEventListener("change", (ev) => {
    const t = ev.target;
    if (!(t instanceof HTMLInputElement)) return;
    if (t.classList.contains("js-source-select")) {
      if (t.checked) sourcesBulkSelected.add(t.value);
      else sourcesBulkSelected.delete(t.value);
      syncSourcesBulkUI();
      return;
    }
    if (t.id === "sources-select-page") {
      const boxes = sourcesBulkPageCheckboxes();
      if (t.checked) {
        boxes.forEach((cb) => {
          if (cb.disabled) return;
          sourcesBulkSelected.add(cb.value);
          cb.checked = true;
        });
      } else {
        const pageIds = new Set(boxes.map((cb) => cb.value));
        const hasOffPage = Array.from(sourcesBulkSelected).some((id) => !pageIds.has(id));
        if (hasOffPage) {
          sourcesBulkSelected.clear();
          boxes.forEach((cb) => {
            cb.checked = false;
          });
        } else {
          boxes.forEach((cb) => {
            sourcesBulkSelected.delete(cb.value);
            cb.checked = false;
          });
        }
      }
      syncSourcesBulkUI();
    }
  });

  document.body.addEventListener("click", (ev) => {
    const modeBtn = ev.target.closest("[data-sources-bulk-mode]");
    if (modeBtn) {
      ev.preventDefault();
      setSourcesBulkMode(!sourcesBulkMode);
      return;
    }
    if (sourcesBulkMode) {
      const row = ev.target.closest(SOURCES_BULK_ROW);
      if (
        row &&
        !ev.target.closest(".js-source-select, [data-source-select-wrap], [data-sources-row-actions]")
      ) {
        ev.preventDefault();
        const id = row.getAttribute("data-source-id");
        if (id) toggleSourcesBulkID(id);
        return;
      }
    }
    const selectAll = ev.target.closest("[data-sources-select-all-matching]");
    if (selectAll) {
      ev.preventDefault();
      if (selectAll.disabled) return;
      selectAll.disabled = true;
      selectAllMatchingSources()
        .catch(() => {})
        .finally(() => syncSourcesBulkUI());
      return;
    }
    const scan = ev.target.closest("[data-sources-bulk-scan]");
    if (scan) {
      ev.preventDefault();
      runSourcesBulkAction("scan");
      return;
    }
    const del = ev.target.closest("[data-sources-bulk-delete]");
    if (del) {
      ev.preventDefault();
      runSourcesBulkAction("delete");
    }
  });

  document.body.addEventListener("submit", (ev) => {
    const form = ev.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.id !== "form-bulk-scan-sources" && form.id !== "form-bulk-scan-sources-confirm") {
      return;
    }
    fillSourcesBulkIDs(document);
    fillSourcesBulkRedirect();
  });

  document.body.addEventListener("htmx:beforeRequest", (ev) => {
    if (!onSourcesBulkPage() || !sourcesBulkMode) return;
    const elt = ev.detail && ev.detail.elt;
    if (!elt || !elt.closest) return;
    const live = sourcesBulkLive();
    if (!live) return;
    const form =
      elt.closest("#" + live.id + " form.js-list-filters") ||
      (elt.matches && elt.matches("#" + live.id + " form.js-list-filters") ? elt : null);
    if (!form) return;
    sourcesBulkSelected.clear();
    syncSourcesBulkUI();
  });

  // afterSettle: HTMX class settle restores response `hidden` on id'd bulk bar.
  const onSourcesBulkLiveSwap = (ev) => {
    const target = ev.detail && ev.detail.target;
    if (!target || target.id !== "sources-list-live") return;
    syncSourcesBulkUI();
  };
  document.body.addEventListener("htmx:afterSwap", onSourcesBulkLiveSwap);
  document.body.addEventListener("htmx:afterSettle", onSourcesBulkLiveSwap);

  if (onSourcesBulkPage()) {
    syncSourcesBulkUI();
  }
}
