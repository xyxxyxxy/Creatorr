/** Configurable list-table columns (cookie + show/hide; ≥1 visible). */

function writeColsCookie(name, keys) {
  const maxAge = 365 * 24 * 3600;
  // Plain comma list: keys are [a-z0-9_]+. encodeURIComponent turns commas into
  // %2C, which Go Cookie().Value does not decode, so filter HTMX swaps ignored
  // the cookie and reset columns to defaults.
  document.cookie = name + "=" + keys.join(",") + "; Path=/; Max-Age=" + maxAge + "; SameSite=Lax";
}

function readColsCookie(name) {
  if (!name) return "";
  const prefix = name + "=";
  for (const part of document.cookie.split(";")) {
    const s = part.trim();
    if (!s.startsWith(prefix)) continue;
    let v = s.slice(prefix.length);
    try {
      v = decodeURIComponent(v);
    } catch (_) {
      /* keep raw */
    }
    return v;
  }
  return "";
}

function visibleColCount(menu) {
  return menu.querySelectorAll("input[data-table-col]:checked").length;
}

function syncTableColVisibility(root, key, on) {
  root.querySelectorAll('[data-col="' + CSS.escape(key) + '"]').forEach((el) => {
    el.classList.toggle("hidden", !on);
  });
}

/** Prefer cookie over swapped markup so filter refreshes keep the operator's columns. */
function syncMenuFromCookie(menu) {
  const raw = readColsCookie(menu.getAttribute("data-cols-cookie") || "");
  if (!raw) return;
  const want = new Set(
    raw
      .split(",")
      .map((k) => k.trim().toLowerCase())
      .filter(Boolean),
  );
  if (!want.size) return;
  let any = false;
  menu.querySelectorAll("input[data-table-col]").forEach((cb) => {
    const key = (cb.getAttribute("data-table-col") || "").toLowerCase();
    const on = want.has(key);
    cb.checked = on;
    if (on) any = true;
  });
  if (!any) {
    const first = menu.querySelector("input[data-table-col]");
    if (first) first.checked = true;
  }
}

function applyColsFromMenu(menu) {
  const tableRoot = menu.closest("[data-list-table]");
  if (!tableRoot) return;
  const cookie = menu.getAttribute("data-cols-cookie") || "";
  const keys = [];
  menu.querySelectorAll("input[data-table-col]").forEach((cb) => {
    const key = cb.getAttribute("data-table-col");
    if (!key) return;
    const on = !!cb.checked;
    syncTableColVisibility(tableRoot, key, on);
    if (on) keys.push(key);
  });
  if (cookie && keys.length) writeColsCookie(cookie, keys);
  // Keep at least one: disable uncheck on the sole checked box.
  const checked = menu.querySelectorAll("input[data-table-col]:checked");
  menu.querySelectorAll("input[data-table-col]").forEach((cb) => {
    cb.disabled = checked.length === 1 && cb.checked;
  });
}

export function bootListTable() {
  document.body.addEventListener("change", (ev) => {
    const t = ev.target;
    if (!(t instanceof HTMLInputElement)) return;
    if (!t.hasAttribute("data-table-col")) return;
    const menu = t.closest("[data-list-table-cols]");
    if (!menu) return;
    if (!t.checked && visibleColCount(menu) === 0) {
      t.checked = true;
      return;
    }
    applyColsFromMenu(menu);
  });

  document.body.addEventListener("htmx:afterSwap", (ev) => {
    const target = ev.detail && ev.detail.target;
    if (!target || !target.querySelector) return;
    target.querySelectorAll("[data-list-table-cols]").forEach((menu) => {
      syncMenuFromCookie(menu);
      applyColsFromMenu(menu);
    });
  });

  document.querySelectorAll("[data-list-table-cols]").forEach((menu) => {
    syncMenuFromCookie(menu);
    applyColsFromMenu(menu);
  });
}
