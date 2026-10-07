import { createLucideIcons } from "./dom_helpers.js";

const badge = () => document.getElementById("queues-badge");

const pausedBadge = () => document.getElementById("queues-paused-badge");

const notifyBadge = () => document.getElementById("notify-badge");

const seriesErrorBadge = () => document.getElementById("series-error-badge");

export async function refreshBadge() {
  try {
    const [tasksRes, seriesErrRes] = await Promise.all([
      fetch("/api/tasks"),
      fetch("/series/error-count.json"),
    ]);
    //! Soft-pause alone must not bump a nav badge; lane chips show Paused. Count all open tasks.
    const pb = pausedBadge();
    if (pb) {
      pb.textContent = "0";
      pb.classList.add("hidden");
    }
    if (tasksRes.ok) {
      const tasks = await tasksRes.json();
      const list = Array.isArray(tasks) ? tasks : [];
      const n = list.length;
      const b = badge();
      if (b) {
        b.textContent = String(n);
        b.classList.toggle("hidden", n === 0);
      }
    }
    if (seriesErrRes.ok) {
      const data = await seriesErrRes.json();
      const n = Math.max(0, Math.floor(Number(data && data.count) || 0));
      const b = seriesErrorBadge();
      if (b) {
        b.textContent = n > 99 ? "99+" : String(n);
        b.classList.toggle("hidden", n === 0);
      }
    }
  } catch (_) {}
}

function setNotifyBadge(n, hasAlert) {
  const b = notifyBadge();
  if (!b) return;
  const count = Math.max(0, Math.floor(Number(n) || 0));
  b.textContent = count > 99 ? "99+" : String(count);
  b.classList.toggle("hidden", count === 0);
  const alert = Boolean(hasAlert) && count > 0;
  // Replace color classes (toggle can leave both if SSR/HTML drifted).
  b.classList.remove("badge-info", "badge-error", "badge-warning");
  b.classList.add(alert ? "badge-error" : "badge-info");
}

export async function refreshNotifyBadge(count, hasAlert) {
  // Only skip the fetch when both values are known; a bare count must not
  // force info color when alerts may still be unread.
  if (typeof count === "number" && typeof hasAlert === "boolean") {
    setNotifyBadge(count, hasAlert);
    return;
  }
  try {
    const res = await fetch("/api/notifications/unread-count");
    if (!res.ok) return;
    const data = await res.json();
    setNotifyBadge(Number(data && data.count) || 0, Boolean(data && data.has_alert));
  } catch (_) {}
}

function formatNotifyAgo(raw) {
  const then = new Date(raw);
  if (Number.isNaN(then.getTime())) return "";
  let t = then.getTime();
  let n = Date.now();
  if (t > n) {
    const swap = t;
    t = n;
    n = swap;
  }
  let years = 0;
  while (true) {
    const d = new Date(t);
    d.setUTCFullYear(d.getUTCFullYear() + years + 1);
    if (d.getTime() > n) break;
    years++;
  }
  const afterYears = new Date(t);
  afterYears.setUTCFullYear(afterYears.getUTCFullYear() + years);
  t = afterYears.getTime();
  let months = 0;
  while (true) {
    const d = new Date(t);
    d.setUTCMonth(d.getUTCMonth() + months + 1);
    if (d.getTime() > n) break;
    months++;
  }
  const afterMonths = new Date(t);
  afterMonths.setUTCMonth(afterMonths.getUTCMonth() + months);
  let rem = n - afterMonths.getTime();
  const dayMs = 24 * 60 * 60 * 1000;
  const hourMs = 60 * 60 * 1000;
  const minMs = 60 * 1000;
  const days = Math.floor(rem / dayMs);
  rem -= days * dayMs;
  const hours = Math.floor(rem / hourMs);
  rem -= hours * hourMs;
  const minutes = Math.floor(rem / minMs);
  const parts = [];
  if (years > 0) parts.push(years + " y");
  if (months > 0) parts.push(months + " mo");
  if (days > 0) parts.push(days + " d");
  if (hours > 0) parts.push(hours + " h");
  if (minutes > 0) parts.push(minutes + " m");
  if (!parts.length) return "just now";
  return parts.slice(0, 2).join(" ") + " ago";
}

const notifyEventLabels = {
  ytdlp_failed: "yt-dlp / site failure",
  cookie_invalid: "Cookie / auth failure",
  rate_limited: "Rate limit / IP block",
  downloaded_integrity_failed: "Integrity check failed",
  file_sync_issues: "File sync issues",
  pot_provider: "PO token provider failure",
  path_collision: "Episode path collision",
  download_digest: "Downloads finished (digest)",
  live_skipped: "Live broadcast skipped",
  archive_fallback: "Web Archive fallback used",
};

function notifyEventLabel(event) {
  const id = String(event || "").trim();
  if (!id) return "";
  return notifyEventLabels[id] || id;
}

export async function refreshNotifyDropdown() {
  const menu = document.getElementById("notify-menu");
  const empty = document.getElementById("notify-dropdown-empty");
  const viewAll = document.getElementById("notify-menu-view-all");
  if (!menu || !empty || !viewAll) return;
  try {
    const res = await fetch("/api/notifications?unread_only=true&limit=4");
    if (!res.ok) return;
    const items = await res.json();
    // Drop previous notification rows (keep mark-all, empty, view-all).
    menu.querySelectorAll("[data-notify-item]").forEach((el) => el.remove());
    if (!Array.isArray(items) || items.length === 0) {
      empty.classList.remove("hidden");
      return;
    }
    empty.classList.add("hidden");
    const frag = document.createDocumentFragment();
    items.forEach((n) => {
      const li = document.createElement("li");
      li.setAttribute("data-notify-item", "");
      const a = document.createElement("a");
      a.href = "/notification/" + n.id;
      a.className = "items-start gap-2 whitespace-normal h-auto min-h-0 py-2";
      if (n.unread) a.classList.add("menu-active");
      const level = String(n.level || "info");
      const iconWrap = document.createElement("span");
      iconWrap.className = "inline-flex shrink-0 mt-0.5";
      const icon = document.createElement("i");
      if (level === "alert") {
        icon.setAttribute("data-lucide", "megaphone");
        icon.className = "size-4 text-error";
      } else if (level === "warning") {
        icon.setAttribute("data-lucide", "siren");
        icon.className = "size-4 text-warning";
      } else {
        icon.setAttribute("data-lucide", "bell");
        icon.className = "size-4 opacity-70";
      }
      iconWrap.appendChild(icon);
      const text = document.createElement("span");
      text.className = "flex flex-col items-start gap-0.5 min-w-0";
      const title = document.createElement("span");
      title.className = "font-medium text-sm";
      title.textContent = n.title || n.event || "Notification";
      const meta = document.createElement("span");
      meta.className = "text-xs opacity-60";
      const eventLabel = notifyEventLabel(n.event);
      const ago = formatNotifyAgo(n.created_at);
      meta.textContent = [eventLabel, ago].filter(Boolean).join(" · ");
      a.setAttribute("aria-label", [title.textContent, eventLabel, ago].filter(Boolean).join(" · "));
      text.appendChild(title);
      text.appendChild(meta);
      a.appendChild(iconWrap);
      a.appendChild(text);
      li.appendChild(a);
      frag.appendChild(li);
    });
    menu.insertBefore(frag, viewAll);
    createLucideIcons(menu);
  } catch (_) {}
}

export async function markAllNotificationsRead() {
  try {
    const res = await fetch("/api/notifications/read-all", { method: "POST" });
    if (!res.ok) return;
    const data = await res.json();
    refreshNotifyBadge(data && data.count, data && data.has_alert);
    await refreshNotifyDropdown();
  } catch (_) {}
}

export function refreshNotificationHistoryPanel() {
  const panel = document.getElementById("notifications-list-live");
  if (!panel || !window.htmx) return;
  const onBrowser =
    location.pathname === "/browser" &&
    new URLSearchParams(location.search).get("type") === "notifications";
  if (!onBrowser) return;
  const params = new URLSearchParams(location.search);
  params.set("type", "notifications");
  params.set("at", "browser");
  window.htmx.ajax("GET", "/explorer/browse?" + params.toString(), {
    target: "#notifications-list-live",
    select: "#notifications-list-live",
    swap: "outerHTML",
  });
}
