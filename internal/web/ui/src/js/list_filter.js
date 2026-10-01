// List filters (js-list-filters): select/date → submit now; search → debounce after
// typing stops, and flush on blur. Keep caret in search across HTMX live swaps.
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
    input.focus();
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
