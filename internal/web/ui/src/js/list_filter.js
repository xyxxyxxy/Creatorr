// List filters (js-list-filters): select/date → submit now; search → debounce after
// typing stops, and flush on blur. Keep caret in search across HTMX live swaps.
// Filter menu: date change applies immediately (no Apply button). Multi value
// rows are plain links (same as single-select); server Href toggles the query.
let listFilterQFocus = null;

let listFilterSearchTimer = null;

export const LIST_FILTER_SEARCH_MS = 350;

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
  document.body.addEventListener("change", (ev) => {
    const el = ev.target;
    if (!el) return;

    const menu = el.closest("[data-list-filter-menu]");
    if (menu) {
      if (el.type === "date") {
        const wrap = el.closest("[data-list-filter-date]");
        if (wrap) applyDateFilter(wrap);
      }
      return;
    }

    const form = el.closest("form.js-list-filters");
    if (!form) return;
    if (el.tagName === "SELECT") {
      if (el.name === "q_field") {
        const input = form.querySelector('input[type="search"][name="q"]');
        const opt = el.options[el.selectedIndex];
        if (input && opt) {
          const ph = "Search by " + opt.text;
          input.placeholder = ph;
          input.setAttribute("aria-label", ph);
        }
      }
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
