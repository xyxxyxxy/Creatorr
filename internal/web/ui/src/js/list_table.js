/** Configurable list-table columns (cookie + show/hide; ≥1 visible). */

function writeColsCookie(name, keys) {
  const maxAge = 365 * 24 * 3600;
  document.cookie =
    name + "=" + encodeURIComponent(keys.join(",")) + "; Path=/; Max-Age=" + maxAge + "; SameSite=Lax";
}

function visibleColCount(menu) {
  return menu.querySelectorAll('input[data-table-col]:checked').length;
}

function syncTableColVisibility(root, key, on) {
  root.querySelectorAll('[data-col="' + CSS.escape(key) + '"]').forEach((el) => {
    el.classList.toggle("hidden", !on);
  });
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
    target.querySelectorAll("[data-list-table-cols]").forEach((menu) => applyColsFromMenu(menu));
  });

  document.querySelectorAll("[data-list-table-cols]").forEach((menu) => applyColsFromMenu(menu));
}
