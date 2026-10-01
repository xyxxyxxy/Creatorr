/** Rewrite <time data-local-time datetime="…"> to browser locale + timezone. */
export function formatLocalTimes(root) {
  let scope = root && root.nodeType === 1 ? root : document;
  if (scope !== document && scope.isConnected === false) {
    const id = scope.id;
    scope = (id && document.getElementById(id)) || document.body;
  }
  const nodes = [];
  if (scope.matches && scope.matches("time[data-local-time], time[data-local-date]")) nodes.push(scope);
  if (scope.querySelectorAll) {
    scope.querySelectorAll("time[data-local-time], time[data-local-date]").forEach((el) => nodes.push(el));
  }
  nodes.forEach((el) => {
    const raw = el.getAttribute("datetime");
    if (!raw) return;
    const d = new Date(raw);
    if (Number.isNaN(d.getTime())) return;
    if (el.hasAttribute("data-local-date")) {
      el.textContent = d.toLocaleDateString(undefined, { dateStyle: "medium" });
      return;
    }
    el.textContent = d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
  });
  const dateInputs = [];
  if (scope.matches && scope.matches("input[data-local-date]")) dateInputs.push(scope);
  if (scope.querySelectorAll) {
    scope.querySelectorAll("input[data-local-date]").forEach((el) => dateInputs.push(el));
  }
  dateInputs.forEach((el) => {
    const raw = el.getAttribute("data-datetime");
    if (!raw) return;
    const d = new Date(raw);
    if (Number.isNaN(d.getTime())) return;
    el.value = d.toLocaleDateString(undefined, { dateStyle: "medium" });
  });
  syncAllSpecialKindSelects(scope);
}

export function syncSpecialKindSelect(sel) {
  if (!(sel instanceof HTMLSelectElement)) return;
  const none = !sel.value || sel.value === "episode";
  sel.classList.toggle("opacity-60", none);
}

function syncAllSpecialKindSelects(root) {
  let scope = root && root.nodeType === 1 ? root : document;
  if (scope !== document && scope.isConnected === false) {
    const id = scope.id;
    scope = (id && document.getElementById(id)) || document.body;
  }
  if (scope.matches && scope.matches("[data-special-kind]")) syncSpecialKindSelect(scope);
  if (scope.querySelectorAll) {
    scope.querySelectorAll("[data-special-kind]").forEach(syncSpecialKindSelect);
  }
}

export function createLucideIcons(root) {
  if (!window.lucide || typeof window.lucide.createIcons !== "function") return;
  // Prefer a connected root. HTMX OOB outerHTML leaves detail.target detached.
  let scope = root && root.nodeType === 1 ? root : document;
  if (scope !== document && scope.isConnected === false) {
    const id = scope.id;
    scope = (id && document.getElementById(id)) || document.body;
  }
  // Only convert pending <i> placeholders - skip already-rendered <svg data-lucide>.
  const pending = [];
  if (scope.matches && scope.matches("i[data-lucide]")) pending.push(scope);
  if (scope.querySelectorAll) {
    scope.querySelectorAll("i[data-lucide]").forEach((el) => pending.push(el));
  }
  if (!pending.length) return;
  const parents = new Set();
  pending.forEach((el) => {
    if (el.parentElement) parents.add(el.parentElement);
  });
  const attrs = {
    "stroke-width": 1.75,
    "aria-hidden": "true",
  };
  parents.forEach((parent) => {
    window.lucide.createIcons({ root: parent, attrs });
  });
}

/** Live element after HTMX swap (detail.target may be detached after outerHTML). */
export function htmxSwapRoot(ev) {
  if (ev.target && ev.target.nodeType === 1 && ev.target.isConnected) return ev.target;
  const detail = ev.detail && ev.detail.target;
  if (detail && detail.nodeType === 1) {
    if (detail.isConnected) return detail;
    if (detail.id) {
      const live = document.getElementById(detail.id);
      if (live) return live;
    }
  }
  return document.body;
}

/** After /task/{id}/logs HTMX swap, pin the log pane to the newest lines. */
export function scrollTaskLogsToBottom(root) {
  if (!root || root.nodeType !== 1) return;
  const box = root.id === "task-logs" ? root : root.querySelector("#task-logs");
  if (!box) return;
  const pre = box.querySelector("pre");
  if (!pre) return;
  requestAnimationFrame(() => {
    pre.scrollTop = pre.scrollHeight;
  });
}
