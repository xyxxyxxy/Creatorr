import { syncQualityProfileGate } from "./controls.js";
import { createLucideIcons } from "./dom_helpers.js";
import { onSeriesListPage } from "./series_live.js";
import { actorRowHTML, stringListRowHTML, syncActorsEmpty, syncStringListEmptyLabel } from "./string_lists.js";

// --- Series list bulk selection (sticky across HTMX filter/pager swaps) ---
const seriesBulkSelected = new Set();

let seriesBulkMode = false;

function seriesBulkFilterTotal() {
  const live = document.getElementById("series-list-live");
  if (!live) return 0;
  const n = parseInt(live.getAttribute("data-filter-total") || "0", 10);
  return Number.isFinite(n) ? n : 0;
}

function seriesBulkBusy() {
  const live = document.getElementById("series-list-live");
  return !!(live && live.getAttribute("data-bulk-busy") === "1");
}

function seriesBulkPageCheckboxes() {
  return Array.from(document.querySelectorAll("#series-list-live .js-series-select"));
}

function fillSeriesBulkIDs(root) {
  const hosts = (root || document).querySelectorAll("[data-bulk-series-ids]");
  hosts.forEach((host) => {
    host.replaceChildren();
    seriesBulkSelected.forEach((id) => {
      const input = document.createElement("input");
      input.type = "hidden";
      input.name = "series_id";
      input.value = String(id);
      host.appendChild(input);
    });
  });
}

function setSeriesBulkMode(on) {
  seriesBulkMode = !!on;
  if (!seriesBulkMode) seriesBulkSelected.clear();
  syncSeriesBulkUI();
}

function toggleSeriesBulkID(id) {
  if (!id || seriesBulkBusy()) return;
  if (seriesBulkSelected.has(id)) seriesBulkSelected.delete(id);
  else seriesBulkSelected.add(id);
  const cb = document.querySelector(
    '#series-list-live .js-series-select[value="' + CSS.escape(id) + '"]'
  );
  if (cb) cb.checked = seriesBulkSelected.has(id);
  syncSeriesBulkUI();
}

function syncSeriesBulkUI() {
  const live = document.getElementById("series-list-live");
  if (!live) return;
  live.setAttribute("data-bulk-mode", seriesBulkMode ? "1" : "0");
  const modeBtn = live.querySelector("[data-series-bulk-mode]");
  if (modeBtn) {
    modeBtn.setAttribute("aria-pressed", seriesBulkMode ? "true" : "false");
    modeBtn.classList.remove("btn-primary", "btn-active");
    const wrap = modeBtn.closest(".js-list-toolbar-dd");
    if (wrap) wrap.classList.toggle("input-primary", seriesBulkMode);
    modeBtn.setAttribute("data-tip", seriesBulkMode ? "Exit multi-select" : "Multi-select");
    modeBtn.setAttribute("aria-label", seriesBulkMode ? "Exit multi-select" : "Multi-select");
  }
  live.querySelectorAll("[data-series-select-wrap]").forEach((wrap) => {
    wrap.classList.toggle("hidden", !seriesBulkMode);
  });
  live.querySelectorAll("[data-series-monitor-wrap]").forEach((wrap) => {
    wrap.classList.remove("hidden");
    const disabled = seriesBulkMode || seriesBulkBusy();
    wrap.querySelectorAll("button, input, .monitor-toggle").forEach((el) => {
      if (el instanceof HTMLButtonElement || el instanceof HTMLInputElement) {
        el.disabled = disabled;
      }
    });
    wrap.querySelectorAll("[aria-disabled]").forEach((el) => {
      el.setAttribute("aria-disabled", disabled ? "true" : "false");
    });
    wrap.classList.toggle("pointer-events-none", disabled);
    wrap.classList.toggle("opacity-40", disabled);
  });
  live.querySelectorAll("#series-list-rows > [data-series-id]").forEach((row) => {
    row.classList.toggle("cursor-pointer", seriesBulkMode);
    const id = row.getAttribute("data-series-id");
    const selected = seriesBulkMode && seriesBulkSelected.has(id);
    row.classList.toggle("bg-base-200", selected);
    if (row.classList.contains("list-row")) {
      row.classList.toggle("rounded-none", selected);
      // Bulk: checkbox|media|grow|monitor. Normal: media|grow|monitor.
      if (seriesBulkMode) {
        row.style.setProperty("--list-grid-cols", "max-content minmax(0, auto) 1fr max-content");
      } else {
        row.style.setProperty("--list-grid-cols", "minmax(0, auto) 1fr max-content");
      }
      // Title stays plain text in multi-select (row click toggles; no link chrome).
      row.querySelectorAll(".list-col-grow a[href]").forEach((a) => {
        a.classList.toggle("link", !seriesBulkMode);
        a.classList.toggle("link-hover", !seriesBulkMode);
      });
    } else {
      // Thumb card: whole card click toggles; strip title link chrome in bulk.
      row.querySelectorAll(".card-body a[href]").forEach((a) => {
        a.classList.toggle("link", !seriesBulkMode);
        a.classList.toggle("link-hover", !seriesBulkMode);
      });
    }
  });
  const bar = live.querySelector("[data-series-bulk-bar]");
  if (bar) {
    bar.classList.toggle("hidden", !seriesBulkMode);
    const wrap = document.getElementById("series-bulk-bar-wrap");
    if (wrap) wrap.classList.toggle("hidden", !seriesBulkMode);
    const n = seriesBulkSelected.size;
    const m = seriesBulkFilterTotal();
    const countEl = bar.querySelector("[data-series-bulk-count]");
    if (countEl) countEl.textContent = n + "/" + m;
    const busy = seriesBulkBusy();
    bar.querySelectorAll("[data-series-bulk-edit], [data-series-bulk-metadata], [data-series-bulk-delete]").forEach((btn) => {
      btn.disabled = busy || n === 0;
    });
    const selectAllBtn = bar.querySelector("[data-series-select-all-matching]");
    if (selectAllBtn) {
      selectAllBtn.disabled = busy || (m > 0 && n >= m);
    }
  }
  // Always mirror Set → checkbox UI (exit mode clears Set but left boxes checked).
  const pageBoxes = seriesBulkPageCheckboxes();
  pageBoxes.forEach((cb) => {
    cb.checked = seriesBulkSelected.has(cb.value);
  });
  if (bar) {
    const busy = seriesBulkBusy();
    const pageAll =
      pageBoxes.length > 0 && pageBoxes.every((cb) => seriesBulkSelected.has(cb.value));
    const pageCb = document.getElementById("series-select-page");
    if (pageCb) {
      pageCb.checked = pageAll;
      pageCb.indeterminate = !pageAll && pageBoxes.some((cb) => seriesBulkSelected.has(cb.value));
      pageCb.disabled = busy || pageBoxes.length === 0;
    }
  }
  fillSeriesBulkIDs(document);
}

function restoreSeriesBulkCheckboxes() {
  syncSeriesBulkUI();
}

function openSeriesBulkModal(id) {
  const toggle = document.getElementById(id);
  if (toggle) toggle.checked = true;
}

export function resetBulkMetadataForm(form) {
  if (!form) return;
  form.querySelectorAll('input[name="studio"], input[name="country"], input[name="mpaa"]').forEach((el) => {
    el.value = "";
  });
  form.querySelectorAll('select[name="special_feature"], [data-bulk-special-feature]').forEach((el) => {
    el.value = "";
  });
  form.querySelectorAll("[data-string-list-editor]").forEach((editor) => {
    const list = editor.querySelector("[data-string-list]");
    if (!list) return;
    list.querySelectorAll("[data-string-list-row]").forEach((row) => row.remove());
    const draft = editor.querySelector("[data-string-list-draft-value]");
    if (draft) draft.value = "";
    syncStringListEmptyLabel(editor);
  });
  const actors = form.querySelector("[data-actors-editor]");
  if (actors) {
    const list = actors.querySelector("[data-actors-list]");
    if (list) {
      list.querySelectorAll("[data-actor-row]").forEach((row) => row.remove());
      syncActorsEmpty(list);
    }
    const nameEl = actors.querySelector("[data-actor-draft-name]");
    const roleEl = actors.querySelector("[data-actor-draft-role]");
    if (nameEl) nameEl.value = "";
    if (roleEl) roleEl.value = "";
  }
}

function resetBulkSettingsForm(form) {
  if (!form) return;
  form.querySelectorAll('select[name="delivery_mode"], select[name="monitored"], select[name="root_id"]').forEach((el) => {
    el.value = "";
  });
  const qp = form.querySelector("[data-quality-profile-select]") || form.querySelector('select[name="quality_profile_id"]');
  if (qp) qp.value = "";
  syncQualityProfileGate(form);
}

export function fillBulkStringList(editor, values) {
  if (!editor || !Array.isArray(values) || values.length === 0) return;
  const list = editor.querySelector("[data-string-list]");
  if (!list) return;
  values.forEach((v) => {
    const item = String(v || "").trim();
    if (!item) return;
    list.insertAdjacentHTML("beforeend", stringListRowHTML(editor, item, false));
    createLucideIcons(list.lastElementChild);
  });
  syncStringListEmptyLabel(editor);
}

export function fillBulkActors(editor, actors) {
  if (!editor || !Array.isArray(actors) || actors.length === 0) return;
  const list = editor.querySelector("[data-actors-list]");
  if (!list) return;
  actors.forEach((a) => {
    const name = String((a && a.name) || "").trim();
    if (!name) return;
    const role = String((a && a.role) || "").trim();
    list.insertAdjacentHTML("beforeend", actorRowHTML(name, role));
    createLucideIcons(list.lastElementChild);
  });
  syncActorsEmpty(list);
}

async function hydrateBulkMetadataForm() {
  const form = document.getElementById("form-bulk-edit-series-metadata");
  if (!form) return;
  resetBulkMetadataForm(form);
  const ids = Array.from(seriesBulkSelected)
    .map((id) => parseInt(id, 10))
    .filter((n) => Number.isFinite(n) && n > 0);
  if (ids.length === 0) return;
  try {
    const resp = await fetch("/series/bulk-metadata-common", {
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

async function hydrateBulkSettingsForm() {
  const form = document.getElementById("form-bulk-edit-series");
  if (!form) return;
  resetBulkSettingsForm(form);
  const ids = Array.from(seriesBulkSelected)
    .map((id) => parseInt(id, 10))
    .filter((n) => Number.isFinite(n) && n > 0);
  if (ids.length === 0) return;
  try {
    const resp = await fetch("/series/bulk-settings-common", {
      method: "POST",
      headers: { Accept: "application/json", "Content-Type": "application/json" },
      body: JSON.stringify({ ids }),
    });
    if (!resp.ok) return;
    const data = await resp.json();
    const setSelect = (name, value) => {
      const el = form.querySelector('select[name="' + name + '"]');
      if (!el) return;
      const v = String(value);
      if ([...el.options].some((o) => o.value === v)) el.value = v;
    };
    if (data.delivery_mode && data.delivery_mode.same && data.delivery_mode.value) {
      setSelect("delivery_mode", data.delivery_mode.value);
    }
    if (data.monitored && data.monitored.same) {
      setSelect("monitored", data.monitored.value ? "1" : "0");
    }
    if (data.root_id && data.root_id.same && data.root_id.value) {
      setSelect("root_id", data.root_id.value);
    }
    if (data.quality_profile_id && data.quality_profile_id.same && data.quality_profile_id.value) {
      const qp =
        form.querySelector("[data-quality-profile-select]") ||
        form.querySelector('select[name="quality_profile_id"]');
      if (qp) {
        const v = String(data.quality_profile_id.value);
        if ([...qp.options].some((o) => o.value === v)) qp.value = v;
      }
    }
    syncQualityProfileGate(form);
  } catch (_) {
    /* leave No-change form */
  }
}

function runSeriesBulkAction(action) {
  if (!seriesBulkMode || seriesBulkBusy() || seriesBulkSelected.size === 0) return;
  const n = seriesBulkSelected.size;
  const m = seriesBulkFilterTotal();
  fillSeriesBulkIDs(document);
  if (action === "edit") {
    const title = document.querySelector("[data-bulk-edit-title]");
    if (title) title.textContent = "Edit " + n + "/" + m + " series";
    openSeriesBulkModal("modal-bulk-edit-series");
    hydrateBulkSettingsForm();
    return;
  }
  if (action === "metadata") {
    const title = document.querySelector("[data-bulk-meta-title]");
    if (title) title.textContent = "Edit metadata (" + n + "/" + m + ")";
    openSeriesBulkModal("modal-bulk-edit-series-metadata");
    hydrateBulkMetadataForm();
    return;
  }
  if (action === "delete") {
    const title = document.querySelector("[data-bulk-delete-title]");
    if (title) title.textContent = "Delete " + n + "/" + m + " series";
    openSeriesBulkModal("modal-bulk-delete-series");
  }
}

async function selectAllMatchingSeries() {
  const q = location.search || "";
  const resp = await fetch("/series/ids" + q, { headers: { Accept: "application/json" } });
  if (!resp.ok) throw new Error("failed to load matching ids");
  const data = await resp.json();
  const ids = Array.isArray(data.ids) ? data.ids : [];
  seriesBulkSelected.clear();
  ids.forEach((id) => seriesBulkSelected.add(String(id)));
  restoreSeriesBulkCheckboxes();
}

export function bootSeriesBulk() {
  document.body.addEventListener("change", (ev) => {
    const t = ev.target;
    if (!(t instanceof HTMLInputElement)) return;
    if (t.classList.contains("js-series-select")) {
      if (t.checked) seriesBulkSelected.add(t.value);
      else seriesBulkSelected.delete(t.value);
      syncSeriesBulkUI();
      return;
    }
    if (t.id === "series-select-page") {
      const boxes = seriesBulkPageCheckboxes();
      if (t.checked) {
        boxes.forEach((cb) => {
          if (cb.disabled) return;
          seriesBulkSelected.add(cb.value);
          cb.checked = true;
        });
      } else {
        // After Select all (or any off-page picks), uncheck clears the whole set.
        const pageIds = new Set(boxes.map((cb) => cb.value));
        const hasOffPage = Array.from(seriesBulkSelected).some((id) => !pageIds.has(id));
        if (hasOffPage) {
          seriesBulkSelected.clear();
          boxes.forEach((cb) => {
            cb.checked = false;
          });
        } else {
          boxes.forEach((cb) => {
            seriesBulkSelected.delete(cb.value);
            cb.checked = false;
          });
        }
      }
      syncSeriesBulkUI();
    }
  });

  document.body.addEventListener("click", (ev) => {
    const modeBtn = ev.target.closest("[data-series-bulk-mode]");
    if (modeBtn) {
      ev.preventDefault();
      setSeriesBulkMode(!seriesBulkMode);
      return;
    }
    if (seriesBulkMode) {
      //! pin: thumb+list bulk click #series-list-rows > [data-series-id]
      const row = ev.target.closest("#series-list-rows > [data-series-id]");
      if (
        row &&
        !ev.target.closest(".js-series-select, [data-series-select-wrap], [data-series-monitor-wrap]")
      ) {
        ev.preventDefault();
        const id = row.getAttribute("data-series-id");
        if (id) toggleSeriesBulkID(id);
        return;
      }
    }
    const selectAll = ev.target.closest("[data-series-select-all-matching]");
    if (selectAll) {
      ev.preventDefault();
      if (seriesBulkBusy() || selectAll.disabled) return;
      selectAll.disabled = true;
      selectAllMatchingSeries()
        .catch(() => {})
        .finally(() => syncSeriesBulkUI());
      return;
    }
    const edit = ev.target.closest("[data-series-bulk-edit]");
    if (edit) {
      ev.preventDefault();
      runSeriesBulkAction("edit");
      return;
    }
    const meta = ev.target.closest("[data-series-bulk-metadata]");
    if (meta) {
      ev.preventDefault();
      runSeriesBulkAction("metadata");
      return;
    }
    const del = ev.target.closest("[data-series-bulk-delete]");
    if (del) {
      ev.preventDefault();
      runSeriesBulkAction("delete");
    }
  });

  document.body.addEventListener("htmx:beforeRequest", (ev) => {
    if (!onSeriesListPage() || !seriesBulkMode) return;
    const elt = ev.detail && ev.detail.elt;
    if (!elt || !elt.closest) return;
    const form = elt.closest("#series-list-live form.js-list-filters") ||
      (elt.matches && elt.matches("#series-list-live form.js-list-filters") ? elt : null);
    if (!form) return;
    // Keep multi-select on; clear selection (filter result set changed).
    seriesBulkSelected.clear();
    syncSeriesBulkUI();
  });

  document.body.addEventListener("htmx:afterSwap", (ev) => {
    const target = ev.detail && ev.detail.target;
    if (!target || target.id !== "series-list-live") return;
    restoreSeriesBulkCheckboxes();
  });

  if (onSeriesListPage()) {
    restoreSeriesBulkCheckboxes();
  }
}
