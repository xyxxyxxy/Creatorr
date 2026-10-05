// List filters (js-list-filters): select/date → submit now; search → debounce after
// typing stops, and flush on blur. Keep caret in search across HTMX live swaps.
// Filter menu: date change applies immediately (no Apply button). Multi value
// rows are plain links (same as single-select); server Href toggles the query.
// Keep the Filter dropdown (+ open accordion fields) open across live swaps so
// the operator can pick another value without reopening the menu.
let listFilterQFocus = null;
let listFilterMenuKeep = null;

let listFilterSearchTimer = null;

export const LIST_FILTER_SEARCH_MS = 350;

function filterMenuFrom(el) {
  return el && el.closest ? el.closest("[data-list-filter-menu]") : null;
}

function snapshotListFilterMenu(menu) {
  if (!menu) return null;
  const openDetails = [];
  menu.querySelectorAll("details[open]").forEach((d) => {
    const labelEl = d.querySelector(":scope > summary .grow");
    const label = labelEl && labelEl.textContent ? labelEl.textContent.trim() : "";
    if (label) openDetails.push(label);
  });
  return { openDetails, scrollTop: Number(menu.scrollTop) || 0 };
}

export function markListFilterMenuKeep(fromEl) {
  const menu = filterMenuFrom(fromEl) ||
    document.querySelector("form.js-list-filters [data-list-filter-menu]");
  if (!menu) return;
  listFilterMenuKeep = snapshotListFilterMenu(menu);
}

/** Refresh keep-open snapshot when the Filter menu is focused or already marked. */
export function captureListFilterMenuKeep() {
  const menu = document.querySelector("form.js-list-filters [data-list-filter-menu]");
  if (!menu) return;
  const dd = menu.closest(".dropdown");
  const focused = !!(dd && (dd.contains(document.activeElement) || menu.contains(document.activeElement)));
  if (listFilterMenuKeep || focused || (dd && dd.classList.contains("dropdown-open"))) {
    listFilterMenuKeep = snapshotListFilterMenu(menu);
  }
}

function closeForcedFilterMenus(except) {
  document.querySelectorAll("form.js-list-filters .dropdown.dropdown-open").forEach((dd) => {
    if (except && (dd === except || dd.contains(except))) return;
    dd.classList.remove("dropdown-open");
  });
}

function restoreListFilterMenuScroll(menu, scrollTop) {
  if (!menu || scrollTop == null) return;
  const max = Math.max(0, menu.scrollHeight - menu.clientHeight);
  menu.scrollTop = Math.min(Math.max(0, Number(scrollTop) || 0), max);
}

/** Re-open Filter dropdown and prior accordion fields after a live panel swap. */
export function restoreListFilterMenuKeep(root) {
  const saved = listFilterMenuKeep;
  listFilterMenuKeep = null;
  if (!saved || !root || !root.querySelector) return;
  const menu = root.querySelector("[data-list-filter-menu]");
  if (!menu) return;
  const dd = menu.closest(".dropdown");
  if (!dd) return;
  // daisyUI: class forces open across swap; :focus-within alone is lost on replace.
  dd.classList.add("dropdown-open");
  saved.openDetails.forEach((label) => {
    menu.querySelectorAll("details").forEach((d) => {
      const labelEl = d.querySelector(":scope > summary .grow");
      const t = labelEl && labelEl.textContent ? labelEl.textContent.trim() : "";
      if (t === label) d.open = true;
    });
  });
  // After opening details (height change), put the panel back where the operator was.
  restoreListFilterMenuScroll(menu, saved.scrollTop);
  requestAnimationFrame(() => {
    if (!dd.isConnected) return;
    dd.classList.add("dropdown-open");
    restoreListFilterMenuScroll(menu, saved.scrollTop);
    if (typeof menu.focus === "function") {
      menu.focus({ preventScroll: true });
    }
  });
}

export function captureListFilterQFocus() {
  const el = document.activeElement;
  if (!el || el.tagName !== "INPUT" || el.type !== "search") {
    listFilterQFocus = null;
    return;
  }
  if (!el.closest("form.js-list-filters")) {
    listFilterQFocus = null;
    return;
  }
  listFilterQFocus = {
    start: el.selectionStart,
    end: el.selectionEnd,
  };
}

export function restoreListFilterQFocus(root) {
  const saved = listFilterQFocus;
  listFilterQFocus = null;
  if (!saved || !root || !root.querySelector) return;
  const input = root.querySelector("form.js-list-filters input[type='search']");
  if (!input) return;
  requestAnimationFrame(() => {
    // preventScroll: focus must not yank the page (HTMX already restored scrollY).
    input.focus({ preventScroll: true });
    try {
      const len = input.value.length;
      const start = Math.min(Number(saved.start) || 0, len);
      const end = Math.min(Number(saved.end) || 0, len);
      input.setSelectionRange(start, end);
    } catch (_) {
      /* ignore unsupported selection */
    }
  });
}

function submitListFilters(form) {
  if (!form || !form.classList.contains("js-list-filters")) return;
  if (typeof form.requestSubmit === "function") form.requestSubmit();
  else form.submit();
}

function clearListFilterSearchTimer() {
  if (listFilterSearchTimer != null) {
    window.clearTimeout(listFilterSearchTimer);
    listFilterSearchTimer = null;
  }
}

function scheduleListFilterSearch(form) {
  clearListFilterSearchTimer();
  listFilterSearchTimer = window.setTimeout(() => {
    listFilterSearchTimer = null;
    if (form && form.isConnected) submitListFilters(form);
  }, LIST_FILTER_SEARCH_MS);
}

export function bootListFilter() {
  // Capture before HTMX/navigation so keep-open survives blur + outerHTML swap.
  document.body.addEventListener(
    "click",
    (ev) => {
      const t = ev.target;
      if (!t || !t.closest) return;
      if (t.closest("[data-list-filter-menu] a") || t.closest("[data-list-filter-menu] [data-filter-date]")) {
        markListFilterMenuKeep(t);
        return;
      }
      // dropdown-open ignores focus loss; clear when clicking outside Filter.
      closeForcedFilterMenus(t);
    },
    true,
  );

  document.body.addEventListener("keydown", (ev) => {
    if (ev.key !== "Escape") return;
    closeForcedFilterMenus(null);
  });

  document.body.addEventListener("change", (ev) => {
    const el = ev.target;
    if (!el) return;

    const menu = el.closest("[data-list-filter-menu]");
    if (menu) {
      if (el.type === "date") {
        const wrap = el.closest("[data-list-filter-date]");
        if (wrap) {
          markListFilterMenuKeep(wrap);
          applyDateFilter(wrap);
        }
      }
      return;
    }

    const form = el.closest("form.js-list-filters");
    if (!form) return;
    if (el.tagName === "SELECT") {
      submitListFilters(form);
      return;
    }
    if (el.tagName === "INPUT" && (el.type === "date" || el.type === "datetime-local")) {
      submitListFilters(form);
    }
  });

  // Sidecar JSON / task Commands: Pretty toggle swaps raw vs indented <pre>.
  document.body.addEventListener("change", (ev) => {
    const t = ev.target.closest(".js-json-pretty-toggle");
    if (!t) return;
    const root = t.closest("[data-json-preview]");
    if (!root) return;
    const on = t.checked;
    root.querySelectorAll("[data-json-raw]").forEach((el) => {
      el.classList.toggle("hidden", on);
    });
    root.querySelectorAll("[data-json-pretty]").forEach((el) => {
      el.classList.toggle("hidden", !on);
    });
  });

  document.body.addEventListener("input", (ev) => {
    const el = ev.target;
    if (!el || el.tagName !== "INPUT" || el.type !== "search") return;
    const form = el.closest("form.js-list-filters");
    if (!form) return;
    scheduleListFilterSearch(form);
  });

  document.body.addEventListener("focusout", (ev) => {
    const el = ev.target;
    if (!el || el.tagName !== "INPUT" || el.type !== "search") return;
    const form = el.closest("form.js-list-filters");
    if (!form) return;
    const next = ev.relatedTarget;
    if (next && form.contains(next)) return;
    clearListFilterSearchTimer();
    submitListFilters(form);
  });
}

function toolbarFormFrom(el) {
  const menu = el.closest("[data-list-filter-menu]");
  const form = el.closest("form.js-list-filters") ||
    (menu && menu.closest("form.js-list-filters"));
  return form;
}

function navigateFilterURL(form, url) {
  const live = (form && form.getAttribute("data-live-target")) ||
    (form && form.closest("[data-list-filter-menu]") && form.closest("[data-list-filter-menu]").getAttribute("data-live-target"));
  if (live && window.htmx) {
    window.htmx.ajax("GET", url, { target: "#" + live, select: "#" + live, swap: "outerHTML", push: url });
    return;
  }
  window.location.assign(url);
}

function copyToolbarParams(form, u) {
  const fd = new FormData(form);
  ["q", "q_field", "sort", "dir", "view"].forEach((name) => {
    const v = fd.get(name);
    if (v == null || String(v) === "") {
      if (name === "q" || name === "q_field") u.searchParams.delete(name);
      return;
    }
    u.searchParams.set(name, String(v));
  });
}

function applyDateFilter(wrap) {
  const form = toolbarFormFrom(wrap);
  if (!form) return;
  const fromEl = wrap.querySelector('[data-filter-date="from"]');
  const toEl = wrap.querySelector('[data-filter-date="to"]');
  const u = new URL(window.location.href);
  u.searchParams.delete("page");
  u.searchParams.delete("from");
  u.searchParams.delete("to");
  const from = fromEl && fromEl.value ? fromEl.value.trim() : "";
  const to = toEl && toEl.value ? toEl.value.trim() : "";
  if (from) u.searchParams.set("from", from);
  if (to) u.searchParams.set("to", to);
  if (from || to) {
    ["empty", "not_empty"].forEach((pk) => {
      const keep = u.searchParams.getAll(pk).filter((v) => v.toLowerCase() !== "upload_date");
      u.searchParams.delete(pk);
      keep.forEach((v) => u.searchParams.append(pk, v));
    });
  }
  copyToolbarParams(form, u);
  navigateFilterURL(form, u.pathname + u.search);
}
