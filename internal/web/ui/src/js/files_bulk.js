// --- Files Explorer bulk selection (Browser Files only; detail embeds omit) ---
const filesBulkSelected = new Set();

let filesBulkMode = false;

const FILES_BULK_ROW = "#files-list-rows > [data-file-id]";
const FILES_BULK_CONFIRM_AFTER = 5;

function filesBulkLive() {
  return document.getElementById("files-list-live");
}

function filesBulkFilterTotal() {
  const live = filesBulkLive();
  if (!live) return 0;
  const n = parseInt(live.getAttribute("data-filter-total") || "0", 10);
  return Number.isFinite(n) ? n : 0;
}

function filesBulkPageCheckboxes() {
  const live = filesBulkLive();
  if (!live) return [];
  return Array.from(live.querySelectorAll(".js-file-select"));
}

function fillFilesBulkIDs(root) {
  const hosts = (root || document).querySelectorAll("[data-bulk-file-ids]");
  hosts.forEach((host) => {
    host.replaceChildren();
    filesBulkSelected.forEach((id) => {
      const input = document.createElement("input");
      input.type = "hidden";
      input.name = "file_id";
      input.value = String(id);
      host.appendChild(input);
    });
  });
}

function fillFilesBulkRedirect() {
  const redir = location.pathname + location.search;
  document.querySelectorAll("[data-bulk-files-redirect]").forEach((el) => {
    el.value = redir;
  });
}

function setFilesBulkMode(on) {
  filesBulkMode = !!on;
  if (!filesBulkMode) filesBulkSelected.clear();
  syncFilesBulkUI();
}

function toggleFilesBulkID(id) {
  if (!id) return;
  if (filesBulkSelected.has(id)) filesBulkSelected.delete(id);
  else filesBulkSelected.add(id);
  const live = filesBulkLive();
  const cb = live
    ? live.querySelector('.js-file-select[value="' + CSS.escape(id) + '"]')
    : null;
  if (cb) cb.checked = filesBulkSelected.has(id);
  syncFilesBulkUI();
}

function syncFilesBulkUI() {
  const live = filesBulkLive();
  if (!live) return;
  live.setAttribute("data-bulk-mode", filesBulkMode ? "1" : "0");
  const modeBtn = live.querySelector("[data-files-bulk-mode]");
  if (modeBtn) {
    modeBtn.setAttribute("aria-pressed", filesBulkMode ? "true" : "false");
    const wrap = modeBtn.closest(".js-list-toolbar-dd");
    if (wrap) wrap.classList.toggle("is-bulk-on", filesBulkMode);
    modeBtn.setAttribute("aria-label", filesBulkMode ? "Exit multi-select" : "Multi-select");
  }
  live.querySelectorAll("[data-file-select-wrap]").forEach((wrap) => {
    wrap.classList.toggle("hidden", !filesBulkMode);
    if (wrap.tagName === "TH" || wrap.tagName === "TD") {
      wrap.setAttribute("aria-hidden", filesBulkMode ? "false" : "true");
    }
  });
  const rowActionsDisabled = filesBulkMode;
  const rowActionsTip = "Use the multi-select bar";
  live.querySelectorAll("[data-files-row-actions]").forEach((wrap) => {
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
        el.classList.remove("tooltip", "tooltip-top", "tooltip-left");
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
  live.querySelectorAll(FILES_BULK_ROW).forEach((row) => {
    row.classList.toggle("cursor-pointer", filesBulkMode);
    const id = row.getAttribute("data-file-id");
    const selected = filesBulkMode && filesBulkSelected.has(id);
    row.classList.toggle("bg-base-200", selected);
    if (row.classList.contains("list-row")) {
      row.classList.toggle("rounded-none", selected);
      if (filesBulkMode) {
        row.style.setProperty(
          "--list-grid-cols",
          "max-content max-content 1fr 7rem max-content"
        );
      } else {
        row.style.setProperty("--list-grid-cols", "max-content 1fr 7rem max-content");
      }
      row.querySelectorAll(".list-col-grow a[href]").forEach((a) => {
        a.classList.toggle("link", !filesBulkMode);
        a.classList.toggle("link-hover", !filesBulkMode);
      });
    }
  });
  const bar = live.querySelector("[data-files-bulk-bar]");
  if (bar) {
    bar.classList.toggle("hidden", !filesBulkMode);
    const wrap = document.getElementById("files-bulk-bar-wrap");
    if (wrap) wrap.classList.toggle("hidden", !filesBulkMode);
    const n = filesBulkSelected.size;
    const m = filesBulkFilterTotal();
    const countEl = bar.querySelector("[data-files-bulk-count]");
    if (countEl) countEl.textContent = n + "/" + m;
    bar.querySelectorAll("[data-files-bulk-check], [data-files-bulk-delete]").forEach((btn) => {
      btn.disabled = n === 0;
    });
    const selectAllBtn = bar.querySelector("[data-files-select-all-matching]");
    if (selectAllBtn) {
      selectAllBtn.disabled = m > 0 && n >= m;
    }
  }
  const pageBoxes = filesBulkPageCheckboxes();
  pageBoxes.forEach((cb) => {
    cb.checked = filesBulkSelected.has(cb.value);
  });
  if (bar) {
    const pageAll =
      pageBoxes.length > 0 && pageBoxes.every((cb) => filesBulkSelected.has(cb.value));
    const pageCb = document.getElementById("files-select-page");
    if (pageCb) {
      pageCb.checked = pageAll;
      pageCb.indeterminate = !pageAll && pageBoxes.some((cb) => filesBulkSelected.has(cb.value));
      pageCb.disabled = pageBoxes.length === 0;
    }
  }
  fillFilesBulkIDs(document);
}

function openFilesBulkModal(id) {
  const toggle = document.getElementById(id);
  if (toggle) toggle.checked = true;
}

function submitFilesBulkForm(formId) {
  fillFilesBulkIDs(document);
  fillFilesBulkRedirect();
  const form = document.getElementById(formId);
  if (form) form.requestSubmit();
}

function runFilesBulkAction(action) {
  if (!filesBulkMode || filesBulkSelected.size === 0) return;
  const n = filesBulkSelected.size;
  const m = filesBulkFilterTotal();
  fillFilesBulkIDs(document);
  fillFilesBulkRedirect();
  const setTitle = (sel, text) => {
    const el = document.querySelector(sel);
    if (el) el.textContent = text;
  };
  if (action === "check") {
    if (n <= FILES_BULK_CONFIRM_AFTER) {
      submitFilesBulkForm("form-bulk-check-file-hash");
      return;
    }
    setTitle("[data-files-bulk-check-title]", "Check integrity (" + n + "/" + m + ")");
    openFilesBulkModal("modal-bulk-check-file-hash");
    return;
  }
  if (action === "delete") {
    setTitle("[data-files-bulk-delete-title]", "Delete sidecars (" + n + "/" + m + ")");
    openFilesBulkModal("modal-bulk-delete-video-sidecar");
  }
}

async function selectAllMatchingFiles() {
  const live = filesBulkLive();
  const q = new URLSearchParams(location.search || "");
  if (live) {
    const sid = live.getAttribute("data-series-id") || "";
    const vid = live.getAttribute("data-video-id") || "";
    if (sid && !q.get("series_id")) q.set("series_id", sid);
    if (vid && !q.get("video_id")) q.set("video_id", vid);
  }
  const qs = q.toString();
  const url = "/files/ids" + (qs ? "?" + qs : "");
  const resp = await fetch(url, {
    headers: { Accept: "application/json" },
  });
  if (!resp.ok) throw new Error("failed to load matching ids");
  const data = await resp.json();
  const ids = Array.isArray(data.ids) ? data.ids : [];
  filesBulkSelected.clear();
  ids.forEach((id) => filesBulkSelected.add(String(id)));
  syncFilesBulkUI();
}

function onFilesBulkPage() {
  return !!filesBulkLive();
}

export function refreshFilesBulkAfterDOM() {
  syncFilesBulkUI();
}

export function bootFilesBulk() {
  document.body.addEventListener("change", (ev) => {
    const t = ev.target;
    if (!(t instanceof HTMLInputElement)) return;
    if (t.classList.contains("js-file-select")) {
      if (t.checked) filesBulkSelected.add(t.value);
      else filesBulkSelected.delete(t.value);
      syncFilesBulkUI();
      return;
    }
    if (t.id === "files-select-page") {
      const boxes = filesBulkPageCheckboxes();
      if (t.checked) {
        boxes.forEach((cb) => {
          if (cb.disabled) return;
          filesBulkSelected.add(cb.value);
          cb.checked = true;
        });
      } else {
        const pageIds = new Set(boxes.map((cb) => cb.value));
        const hasOffPage = Array.from(filesBulkSelected).some((id) => !pageIds.has(id));
        if (hasOffPage) {
          filesBulkSelected.clear();
          boxes.forEach((cb) => {
            cb.checked = false;
          });
        } else {
          boxes.forEach((cb) => {
            filesBulkSelected.delete(cb.value);
            cb.checked = false;
          });
        }
      }
      syncFilesBulkUI();
    }
  });

  document.body.addEventListener("click", (ev) => {
    const modeBtn = ev.target.closest("[data-files-bulk-mode]");
    if (modeBtn) {
      ev.preventDefault();
      setFilesBulkMode(!filesBulkMode);
      return;
    }
    if (filesBulkMode) {
      const row = ev.target.closest(FILES_BULK_ROW);
      if (
        row &&
        !ev.target.closest(".js-file-select, [data-file-select-wrap], [data-files-row-actions]")
      ) {
        ev.preventDefault();
        const id = row.getAttribute("data-file-id");
        if (id) toggleFilesBulkID(id);
        return;
      }
    }
    const selectAll = ev.target.closest("[data-files-select-all-matching]");
    if (selectAll) {
      ev.preventDefault();
      if (selectAll.disabled) return;
      selectAll.disabled = true;
      selectAllMatchingFiles()
        .catch(() => {})
        .finally(() => syncFilesBulkUI());
      return;
    }
    const check = ev.target.closest("[data-files-bulk-check]");
    if (check) {
      ev.preventDefault();
      runFilesBulkAction("check");
      return;
    }
    const del = ev.target.closest("[data-files-bulk-delete]");
    if (del) {
      ev.preventDefault();
      runFilesBulkAction("delete");
    }
  });

  document.body.addEventListener("htmx:beforeRequest", (ev) => {
    if (!onFilesBulkPage() || !filesBulkMode) return;
    const elt = ev.detail && ev.detail.elt;
    if (!elt || !elt.closest) return;
    const live = filesBulkLive();
    if (!live) return;
    const form =
      elt.closest("#" + live.id + " form.js-list-filters") ||
      (elt.matches && elt.matches("#" + live.id + " form.js-list-filters") ? elt : null);
    if (!form) return;
    filesBulkSelected.clear();
    syncFilesBulkUI();
  });

  // afterSettle: HTMX class settle restores response `hidden` on id'd bulk bar.
  const onFilesBulkLiveSwap = (ev) => {
    const target = ev.detail && ev.detail.target;
    if (!target || target.id !== "files-list-live") return;
    syncFilesBulkUI();
  };
  document.body.addEventListener("htmx:afterSwap", onFilesBulkLiveSwap);
  document.body.addEventListener("htmx:afterSettle", onFilesBulkLiveSwap);

  if (onFilesBulkPage()) {
    syncFilesBulkUI();
  }
}
