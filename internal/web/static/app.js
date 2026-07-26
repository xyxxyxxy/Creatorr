(() => {
  const THEME_KEY = "creatorr-theme";
  const THEME_LIGHT = "light";
  const THEME_DARK = "dim"; // OS dark + sun/moon dark side
  // daisyUI themeOrder - keep in sync with node_modules/daisyui/functions/themeOrder.js
  const THEMES = [
    "light", "dark", "cupcake", "bumblebee", "emerald", "corporate", "synthwave", "retro",
    "cyberpunk", "valentine", "halloween", "garden", "forest", "aqua", "lofi", "pastel",
    "fantasy", "wireframe", "black", "luxury", "dracula", "cmyk", "autumn", "business",
    "acid", "lemonade", "night", "coffee", "winter", "dim", "nord", "sunset",
    "caramellatte", "abyss", "silk",
  ];

  function osTheme() {
    try {
      return matchMedia("(prefers-color-scheme: dark)").matches ? THEME_DARK : THEME_LIGHT;
    } catch (_) {
      return THEME_DARK;
    }
  }

  function isTheme(t) {
    return typeof t === "string" && THEMES.includes(t);
  }

  function storedTheme() {
    try {
      const t = localStorage.getItem(THEME_KEY);
      if (isTheme(t)) return t;
    } catch (_) {}
    return null;
  }

  function resolveTheme() {
    return storedTheme() || osTheme();
  }

  function isDarkScheme() {
    try {
      return getComputedStyle(document.documentElement).getPropertyValue("color-scheme").includes("dark");
    } catch (_) {
      return false;
    }
  }

  function syncThemeControls(theme) {
    document.querySelectorAll("[data-theme-toggle]").forEach((el) => {
      el.checked = isDarkScheme();
    });
    document.querySelectorAll('input[name="theme-dropdown"]').forEach((el) => {
      el.checked = el.value === theme;
    });
  }

  function applyTheme(theme, opts) {
    const t = isTheme(theme) ? theme : osTheme();
    document.documentElement.setAttribute("data-theme", t);
    syncThemeControls(t);
    if (opts && opts.persist) {
      try {
        localStorage.setItem(THEME_KEY, t);
      } catch (_) {}
    }
    window.dispatchEvent(new CustomEvent("creatorr:theme", { detail: { theme: t } }));
  }

  function buildThemeMenu() {
    const ul = document.getElementById("theme-menu");
    if (!ul || ul.dataset.ready === "1") return;
    ul.dataset.ready = "1";
    const cur = resolveTheme();
    THEMES.forEach((name) => {
      const li = document.createElement("li");
      const input = document.createElement("input");
      input.type = "radio";
      input.name = "theme-dropdown";
      input.value = name;
      input.className = "theme-controller btn btn-sm btn-block btn-ghost justify-start w-full";
      input.setAttribute("aria-label", name);
      input.autocomplete = "off";
      if (name === cur) input.checked = true;
      input.addEventListener("change", () => {
        if (input.checked) applyTheme(name, { persist: true });
      });
      li.appendChild(input);
      ul.appendChild(li);
    });
  }

  function initTheme() {
    buildThemeMenu();
    applyTheme(resolveTheme(), { persist: false });
    document.querySelectorAll("[data-theme-toggle]").forEach((el) => {
      el.addEventListener("change", () => {
        applyTheme(el.checked ? THEME_DARK : THEME_LIGHT, { persist: true });
      });
    });
    try {
      matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
        if (storedTheme()) return;
        applyTheme(osTheme(), { persist: false });
      });
    } catch (_) {}
  }

  initTheme();

  const badge = () => document.getElementById("tasks-badge");
  const pausedBadge = () => document.getElementById("tasks-paused-badge");
  const notifyBadge = () => document.getElementById("notify-badge");

  async function refreshBadge() {
    try {
      const [tasksRes, pausedRes] = await Promise.all([
        fetch("/api/tasks"),
        fetch("/api/domains/paused"),
      ]);
      if (tasksRes.ok) {
        const tasks = await tasksRes.json();
        const n = Array.isArray(tasks) ? tasks.length : 0;
        const b = badge();
        if (b) {
          b.textContent = String(n);
          b.classList.toggle("hidden", n === 0);
        }
      }
      if (pausedRes.ok) {
        const paused = await pausedRes.json();
        const n = Array.isArray(paused) ? paused.length : 0;
        const b = pausedBadge();
        if (b) {
          b.textContent = String(n);
          b.classList.toggle("hidden", n === 0);
        }
      }
    } catch (_) {}
  }

  function setNotifyBadge(n) {
    const b = notifyBadge();
    if (!b) return;
    const count = Math.max(0, Math.floor(Number(n) || 0));
    b.textContent = count > 99 ? "99+" : String(count);
    b.classList.toggle("hidden", count === 0);
  }

  async function refreshNotifyBadge(count) {
    if (typeof count === "number") {
      setNotifyBadge(count);
      return;
    }
    try {
      const res = await fetch("/api/notifications/unread-count");
      if (!res.ok) return;
      const data = await res.json();
      setNotifyBadge(data && data.count);
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

  async function refreshNotifyDropdown() {
    const menu = document.getElementById("notify-menu");
    const empty = document.getElementById("notify-dropdown-empty");
    const viewAll = document.getElementById("notify-menu-view-all");
    if (!menu || !empty || !viewAll) return;
    try {
      const res = await fetch("/api/notifications?limit=8");
      if (!res.ok) return;
      const items = await res.json();
      // Drop previous notification rows (keep empty, view-all).
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
        const event = n.event || "";
        const ago = formatNotifyAgo(n.created_at);
        meta.textContent = [event, ago].filter(Boolean).join(" · ");
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

  function refreshNotificationHistoryPanel() {
    if (!location.pathname.startsWith("/history")) return;
    const panel = document.getElementById("notification-history-live");
    if (panel && window.htmx) {
      const q = location.search || "";
      window.htmx.ajax("GET", location.pathname + q, {
        target: "#notification-history-live",
        select: "#notification-history-live",
        swap: "outerHTML",
      });
    }
  }

  function refreshTasksPanel() {
    const panel = document.getElementById("tasks-live");
    if (panel && window.htmx) {
      const q = location.search || "";
      window.htmx.ajax("GET", "/tasks" + q, { target: "#tasks-live", select: "#tasks-live", swap: "outerHTML" });
    }
  }

  function clearCooldownBusyTip(wrap) {
    wrap.classList.remove("tooltip", "tooltip-top");
    wrap.removeAttribute("data-tip");
  }

  function showCooldownPaused(wrap) {
    wrap.removeAttribute("data-ends-at");
    wrap.removeAttribute("data-total-sec");
    clearCooldownBusyTip(wrap);
    wrap.setAttribute("aria-label", "Paused");
    wrap.innerHTML =
      '<span data-cd-label class="shrink-0 leading-none text-warning">Paused</span>' +
      '<progress data-cd-bar class="progress progress-warning w-24 sm:w-32 h-2 shrink-0" value="100" max="100" aria-hidden="true"></progress>';
  }

  function showCooldownBusy(wrap) {
    wrap.removeAttribute("data-ends-at");
    wrap.removeAttribute("data-total-sec");
    wrap.classList.add("tooltip", "tooltip-top");
    wrap.setAttribute("data-tip", "Maximum of parallel tasks reached");
    wrap.setAttribute("aria-label", "Maximum of parallel tasks reached");
    wrap.innerHTML =
      '<span data-cd-label class="shrink-0 leading-none">Busy</span>' +
      '<progress data-cd-bar class="progress progress-primary w-24 sm:w-32 h-2 shrink-0" aria-hidden="true"></progress>';
  }

  function showCooldownReady(wrap) {
    if (wrap.hasAttribute("data-paused")) {
      showCooldownPaused(wrap);
      return;
    }
    if (wrap.hasAttribute("data-slots-full")) {
      showCooldownBusy(wrap);
      return;
    }
    wrap.removeAttribute("data-ends-at");
    wrap.removeAttribute("data-total-sec");
    clearCooldownBusyTip(wrap);
    wrap.setAttribute("aria-label", "Ready for tasks");
    wrap.innerHTML =
      '<span data-cd-label class="shrink-0 leading-none text-base-content/50">Ready for tasks</span>' +
      '<progress data-cd-bar class="progress progress-primary w-24 sm:w-32 h-2 shrink-0" value="0" max="100" aria-hidden="true"></progress>';
  }

  function tickDomainCooldowns() {
    document.querySelectorAll("[data-domain-cooldown]").forEach((wrap) => {
      if (wrap.hasAttribute("data-paused")) return;
      const endsAttr = wrap.getAttribute("data-ends-at");
      if (!endsAttr) return;
      const ends = Date.parse(endsAttr);
      if (!Number.isFinite(ends)) {
        showCooldownReady(wrap);
        return;
      }
      let rem = Math.ceil((ends - Date.now()) / 1000);
      if (rem <= 0) {
        showCooldownReady(wrap);
        return;
      }
      let total = parseInt(wrap.getAttribute("data-total-sec") || "0", 10);
      if (!Number.isFinite(total) || total < 1) total = rem;
      if (rem > total) total = rem;
      clearCooldownBusyTip(wrap);
      let bar = wrap.querySelector("[data-cd-bar]");
      let label = wrap.querySelector("[data-cd-label]");
      if (!bar || !label) {
        wrap.innerHTML =
          '<span data-cd-label class="shrink-0 leading-none">Cooldown</span>' +
          '<progress data-cd-bar class="progress progress-primary w-24 sm:w-32 h-2 shrink-0" value="' +
          rem +
          '" max="' +
          total +
          '" aria-hidden="true"></progress>';
        bar = wrap.querySelector("[data-cd-bar]");
        label = wrap.querySelector("[data-cd-label]");
      }
      if (label) {
        label.textContent = "Cooldown";
        label.classList.remove("text-base-content/50", "text-warning");
      }
      if (bar) {
        bar.className = "progress progress-primary w-24 sm:w-32 h-2 shrink-0";
        bar.max = total;
        bar.value = rem;
      }
      wrap.setAttribute("aria-label", "Cooldown " + rem + "s remaining");
    });
  }

  setInterval(tickDomainCooldowns, 250);

  function refreshHistoryPanel() {
    if (!location.pathname.startsWith("/history")) return;
    const panel = document.getElementById("history-live");
    if (panel && window.htmx) {
      const q = location.search || "";
      window.htmx.ajax("GET", location.pathname + q, {
        target: "#history-live",
        select: "#history-live",
        swap: "outerHTML",
      });
      return;
    }
    // Detail page: reload so finished task appears / updates.
    if (/^\/history\/\d+/.test(location.pathname)) {
      location.reload();
    }
  }

  /** Match partials/status_badge.html (icon + tooltip). */
  function statusBadgeEl(status) {
    const s = String(status || "");
    const tips = {
      wanted_source_error: "Source has too many download errors - Retry on the source",
      wanted_download_error: "Last download failed",
      verify_failed: "Post-pack media verify failed - file kept; Want or Download now",
      streamable: "Stream files ready - play via Creatorr proxy",
      missing: "File path recorded but media not on disk - file sync may restore",
    };
    const icons = {
      running: { icon: "loader-circle", color: "text-info", spin: true },
      pending: { icon: "list-ordered", color: "text-base-content/50" },
      failed: { icon: "circle-x", color: "text-error" },
      failure: { icon: "circle-x", color: "text-error" },
      cancelled: { icon: "ban", color: "text-base-content/50" },
      done: { icon: "circle-check", color: "text-success" },
      success: { icon: "circle-check", color: "text-success" },
      wanted: { icon: "download", color: "text-warning" },
      wanted_source_error: { icon: "circle-alert", color: "text-error" },
      wanted_download_error: { icon: "circle-x", color: "text-error" },
      verify_failed: { icon: "badge-alert", color: "text-warning" },
      streamable: { icon: "radio", color: "text-info" },
      downloaded: { icon: "circle-check", color: "text-success" },
      missing: { icon: "file-question", color: "text-warning" },
      deleted: { icon: "trash-2", color: "text-base-content/50" },
      ignored: { icon: "eye-off", color: "text-base-content/50" },
    };
    const labels = {
      wanted_source_error: "wanted (source error)",
      wanted_download_error: "wanted (download error)",
      verify_failed: "Verify failed",
    };
    const meta = icons[s] || { icon: "circle-help", color: "text-base-content/50" };
    const tip = tips[s] || s || "-";
    const label = labels[s] || s || "-";
    const wrap = document.createElement("span");
    wrap.className = "inline-flex tooltip tooltip-top " + meta.color;
    wrap.setAttribute("data-tip", tip);
    wrap.setAttribute("aria-label", label);
    const i = document.createElement("i");
    i.setAttribute("data-lucide", meta.icon);
    i.className = "size-4" + (meta.spin ? " animate-spin" : "");
    wrap.appendChild(i);
    return wrap;
  }

  function patchStatusCell(root, status) {
    if (!root || typeof status !== "string" || !status) return;
    if (root.hasAttribute("data-task-row-status")) {
      root.setAttribute("data-task-row-status", status);
    }
    const cell = root.querySelector("[data-task-status]");
    if (!cell) return;
    cell.replaceChildren(statusBadgeEl(status));
    createLucideIcons(cell);
  }

  /** Sync Tasks list progress: pending label, running determinate, or indeterminate. */
  function syncTaskRowProgress(row, status, progress) {
    const wrap = row.querySelector("[data-task-progress-wrap]");
    if (!wrap) return;
    const st = status === "pending" ? "pending" : "running";
    const hasPct =
      progress != null && Number.isFinite(Number(progress)) && Number(progress) > 0 && Number(progress) < 1;
    wrap.replaceChildren();
    const bar = document.createElement("progress");
    bar.setAttribute("data-task-progress", "");
    if (st === "pending") {
      const paused = row.hasAttribute("data-lane-paused");
      bar.className = "progress " + (paused ? "progress-warning" : "progress-secondary") + " w-full";
      bar.value = 0;
      bar.max = 100;
      bar.setAttribute("aria-label", "pending");
      wrap.appendChild(bar);
      return;
    }
    bar.className = "progress progress-secondary w-full";
    if (hasPct) {
      const pct = Math.max(0, Math.min(100, Math.round(Number(progress) * 100)));
      bar.value = pct;
      bar.max = 100;
      bar.setAttribute("aria-label", pct + "%");
    } else {
      bar.removeAttribute("value");
      bar.setAttribute("aria-label", "In progress");
    }
    wrap.appendChild(bar);
  }

  /** Patch message/progress on an existing task row. Avoids full panel swap (button flicker). */
  function patchTaskRow(ev) {
    let data;
    try {
      data = JSON.parse(ev.data || "{}");
    } catch (_) {
      return false;
    }
    const id = data.task_id;
    if (!id) return false;
    const row = document.getElementById("task-row-" + id);
    if (!row) return false;
    const statusChanged = typeof data.status === "string" && data.status;
    if (statusChanged) {
      patchStatusCell(row, data.status);
    }
    const msgEl = row.querySelector("[data-task-message]");
    if (msgEl) {
      const st = statusChanged
        ? data.status
        : row.getAttribute("data-task-row-status") || "";
      if (st === "pending") {
        msgEl.textContent = "Queued";
      } else if (typeof data.message === "string") {
        msgEl.textContent = data.message || "-";
      }
    }
    const progressChanged = Object.prototype.hasOwnProperty.call(data, "progress");
    if (statusChanged || progressChanged) {
      const st = statusChanged
        ? data.status
        : row.getAttribute("data-task-row-status") || "running";
      let progress = null;
      if (progressChanged) {
        progress = data.progress;
      } else {
        const bar = row.querySelector("progress[data-task-progress]");
        if (bar && bar.hasAttribute("value") && Number(bar.max) > 0) {
          progress = Number(bar.value) / Number(bar.max);
        }
      }
      syncTaskRowProgress(row, st, progress);
    }
    return true;
  }

  /** Patch message/progress on /task/{id} detail page. */
  function patchTaskDetail(ev) {
    let data;
    try {
      data = JSON.parse(ev.data || "{}");
    } catch (_) {
      return false;
    }
    const id = data.task_id;
    if (!id) return false;
    const root = document.querySelector('[data-task-detail="' + id + '"]');
    if (!root) return false;
    const page = root.closest("main") || document;
    if (typeof data.status === "string" && data.status) {
      patchStatusCell(page, data.status);
    }
    const msgEl = page.querySelector("[data-task-message]");
    if (msgEl) {
      const st =
        typeof data.status === "string" && data.status
          ? data.status
          : page.querySelector("[data-task-status]")?.getAttribute("aria-label") || "";
      if (st === "pending") {
        msgEl.textContent = "Queued";
      } else if (typeof data.message === "string") {
        msgEl.textContent = data.message || "-";
      }
    }
    if (data.progress != null && Number.isFinite(Number(data.progress))) {
      const raw = Number(data.progress);
      const pct = Math.max(0, Math.min(100, Math.round(raw * 100)));
      const cell = page.querySelector("[data-task-progress-cell]");
      let bar = page.querySelector("progress[data-task-progress]");
      const showBar = raw > 0 && raw < 1;
      if (!showBar) {
        if (bar) bar.remove();
        if (cell && !cell.querySelector("[data-task-progress]")) {
          cell.textContent = "";
          const dash = document.createElement("span");
          dash.className = "opacity-60";
          dash.textContent = "-";
          cell.appendChild(dash);
        }
      } else {
        if (!bar && cell) {
          cell.textContent = "";
          bar = document.createElement("progress");
          bar.setAttribute("data-task-progress", "");
          bar.className = "progress progress-primary w-full max-w-xs";
          bar.max = 100;
          cell.appendChild(bar);
        }
        if (bar) bar.value = pct;
      }
    } else if (data.progress === null) {
      const cell = page.querySelector("[data-task-progress-cell]");
      const bar = page.querySelector("progress[data-task-progress]");
      if (bar) bar.remove();
      if (cell && !cell.querySelector("progress")) {
        cell.textContent = "";
        const dash = document.createElement("span");
        dash.className = "opacity-60";
        dash.textContent = "-";
        cell.appendChild(dash);
      }
    }
    return true;
  }

  let videoHistoryRefreshAt = 0;
  let taskHistoryRefreshAt = 0;
  let videoStreamCacheRefreshAt = 0;

  /** Refresh video History panel while a related task is progressing. */
  function refreshVideoHistoryIfMatch(ev) {
    if (!window.htmx) return;
    let data;
    try {
      data = JSON.parse(ev.data || "{}");
    } catch (_) {
      return;
    }
    const m = location.pathname.match(/^\/series\/(\d+)\/videos\/(\d+)/);
    if (!m) return;
    const pageVid = Number(m[2]);
    if (!pageVid || !data.video_id || Number(data.video_id) !== pageVid) return;
    if (!document.getElementById("video-history-live")) return;
    const now = Date.now();
    if (now - videoHistoryRefreshAt < 1500) return;
    videoHistoryRefreshAt = now;
    const q = location.search || "";
    window.htmx.ajax("GET", location.pathname + q, {
      target: "#video-history-live",
      select: "#video-history-live",
      swap: "outerHTML",
    });
  }

  /** Refresh Stream cache row while stream_play / cache_beginning / download progresses. */
  function refreshVideoStreamCacheIfMatch(ev) {
    if (!window.htmx) return;
    let data;
    try {
      data = JSON.parse(ev.data || "{}");
    } catch (_) {
      return;
    }
    const kind = data.kind || "";
    if (
      kind !== "stream_play" &&
      kind !== "cache_beginning" &&
      kind !== "download"
    ) {
      return;
    }
    const m = location.pathname.match(/^\/series\/(\d+)\/videos\/(\d+)/);
    if (!m) return;
    const pageVid = Number(m[2]);
    if (!pageVid || !data.video_id || Number(data.video_id) !== pageVid) return;
    if (!document.getElementById("video-stream-cache-live")) return;
    const now = Date.now();
    if (now - videoStreamCacheRefreshAt < 1500) return;
    videoStreamCacheRefreshAt = now;
    const q = location.search || "";
    window.htmx.ajax("GET", location.pathname + q, {
      target: "#video-stream-cache-live",
      select: "#video-stream-cache-live",
      swap: "outerHTML",
    });
  }

  /** Refresh Detail panel on /task/{id} while that task progresses (video history → linked lists). */
  function refreshTaskVideoHistoryIfMatch(ev) {
    if (!window.htmx) return;
    let data;
    try {
      data = JSON.parse(ev.data || "{}");
    } catch (_) {
      return;
    }
    const id = taskDetailID();
    if (!id || Number(data.task_id) !== id) return;
    if (!document.getElementById("task-detail-live")) return;
    const now = Date.now();
    if (now - taskHistoryRefreshAt < 1500) return;
    taskHistoryRefreshAt = now;
    const q = location.search || "";
    window.htmx.ajax("GET", location.pathname + q, {
      target: "#task-detail-live",
      select: "#task-detail-live",
      swap: "outerHTML",
    });
  }

  function taskDetailID() {
    const m = location.pathname.match(/^\/task\/(\d+)\/?$/);
    return m ? Number(m[1]) : 0;
  }

  function reloadTaskDetailIfMatch(ev) {
    let data;
    try {
      data = JSON.parse(ev.data || "{}");
    } catch (_) {
      return;
    }
    const id = taskDetailID();
    if (!id || Number(data.task_id) !== id) return;
    location.reload();
  }

  /** Full reload when a task for this video finishes (status, files, actions). */
  function reloadVideoDetailIfMatch(ev) {
    let data;
    try {
      data = JSON.parse(ev.data || "{}");
    } catch (_) {
      return;
    }
    // Ephemeral metadata fetch finishes in-modal via HTMX poll; reload would close the editor.
    const kind = data.kind || "";
    if (
      kind === "prefetch_video_meta" ||
      kind === "prefetch_series_meta" ||
      kind === "prefetch_add_series" ||
      kind === "prefetch_add_video"
    ) {
      return;
    }
    const m = location.pathname.match(/^\/series\/(\d+)\/videos\/(\d+)/);
    if (!m) return;
    const pageVid = Number(m[2]);
    if (!pageVid || !data.video_id || Number(data.video_id) !== pageVid) return;
    location.reload();
  }

  function refreshTaskIndicators() {
    if (!window.htmx) return;
    const videoPage = location.pathname.match(/^\/series\/(\d+)\/videos\/(\d+)/);
    if (videoPage) {
      window.htmx.ajax("GET", "/series/" + videoPage[1] + "/videos/" + videoPage[2] + "/task-indicator", {
        target: "body",
        swap: "none",
      });
      return;
    }
    const sourcePage = location.pathname.match(/^\/series\/(\d+)\/sources\/(\d+)/);
    if (sourcePage) {
      window.htmx.ajax("GET", "/series/" + sourcePage[1] + "/task-indicators", {
        target: "body",
        swap: "none",
      });
      return;
    }
    const seriesPage = location.pathname.match(/^\/series\/(\d+)\/?$/);
    if (seriesPage) {
      const q = location.search || "";
      window.htmx.ajax("GET", "/series/" + seriesPage[1] + "/task-indicators" + q, {
        target: "body",
        swap: "none",
      });
    }
  }

  /** Stable fingerprint for task-indicator OOB skips (avoids tooltip flicker on poll/SSE). */
  function taskIndicatorFingerprint(el) {
    if (!el || !el.getAttribute) return "";
    const tip =
      el.getAttribute("data-tip") ||
      (el.querySelector && el.querySelector("[data-tip]") && el.querySelector("[data-tip]").getAttribute("data-tip")) ||
      "";
    const busy = el.getAttribute("aria-busy") || "";
    const tipOn = el.classList && el.classList.contains("tooltip") ? "1" : "0";
    let state = "empty";
    const radial = el.querySelector && el.querySelector(".radial-progress");
    if (el.querySelector && el.querySelector(".loading")) {
      state = "spin";
    } else if (radial) {
      state = "prog:" + (radial.style.getPropertyValue("--value") || "").trim();
    } else {
      const lucideEl = el.querySelector && el.querySelector("[data-lucide]");
      if (lucideEl) {
        state = "icon:" + (lucideEl.getAttribute("data-lucide") || "");
      } else {
        const svg = el.querySelector && el.querySelector("svg.lucide, svg[class*='lucide-']");
        if (svg) {
          const cls = [...svg.classList].find((c) => c.startsWith("lucide-") && c !== "lucide");
          state = "icon:" + (cls ? cls.slice("lucide-".length) : "");
        }
      }
    }
    return [tip, busy, tipOn, state].join("|");
  }

  /** Source status OOB: icon fingerprint alone misses label / tooltip-content updates after scan. */
  function sourceStatusFingerprint(el) {
    if (!el || !el.querySelector) return "";
    const labelEl = el.querySelector("a.link") || el.querySelector("span.truncate");
    const tipEl = el.querySelector(".tooltip-content");
    const label = labelEl ? labelEl.textContent.trim() : "";
    const tipBody = tipEl ? tipEl.textContent.trim() : "";
    return taskIndicatorFingerprint(el) + "|" + label + "|" + tipBody;
  }

  // Poll/SSE OOB-replaces every task-indicator; identical swaps reset :hover and tip flickers.
  document.body.addEventListener("htmx:oobBeforeSwap", (ev) => {
    const detail = ev.detail || {};
    const target = detail.target;
    let incoming = detail.fragment;
    if (incoming && incoming.nodeType === 11) incoming = incoming.firstElementChild;
    if (!target || !incoming) return;
    if (
      target.classList.contains("source-status-cell") &&
      incoming.classList.contains("source-status-cell")
    ) {
      if (sourceStatusFingerprint(target) === sourceStatusFingerprint(incoming)) {
        detail.shouldSwap = false;
      }
      return;
    }
    if (target.classList.contains("task-indicator") && incoming.classList.contains("task-indicator")) {
      if (taskIndicatorFingerprint(target) === taskIndicatorFingerprint(incoming)) {
        detail.shouldSwap = false;
      }
    }
  });

  function refreshSeriesVideos(preserveScroll) {
    if (!window.htmx) return;
    const seriesPage = location.pathname.match(/^\/series\/(\d+)\/?$/);
    if (!seriesPage || !document.getElementById("series-videos-live")) return;
    const y = preserveScroll ? window.scrollY : null;
    const q = location.search || "";
    window.htmx.ajax("GET", "/series/" + seriesPage[1] + "/videos-live" + q, {
      target: "#series-videos-live",
      select: "#series-videos-live",
      swap: "outerHTML",
    });
    if (y == null) return;
    const restore = () => window.scrollTo(0, y);
    document.body.addEventListener("htmx:afterSwap", function onSwap(ev) {
      if (!ev.detail || !ev.detail.target || ev.detail.target.id !== "series-videos-live") return;
      document.body.removeEventListener("htmx:afterSwap", onSwap);
      requestAnimationFrame(restore);
    });
  }

  function onSeriesListPage() {
    return /^\/series\/?$/.test(location.pathname);
  }

  function refreshSeriesList(preserveScroll) {
    if (!window.htmx) return;
    if (!onSeriesListPage() || !document.getElementById("series-list-live")) return;
    const y = preserveScroll ? window.scrollY : null;
    const q = location.search || "";
    window.htmx.ajax("GET", "/series/list-live" + q, {
      target: "#series-list-live",
      select: "#series-list-live",
      swap: "outerHTML",
    });
    if (y == null) return;
    const restore = () => window.scrollTo(0, y);
    document.body.addEventListener("htmx:afterSwap", function onSwap(ev) {
      if (!ev.detail || !ev.detail.target || ev.detail.target.id !== "series-list-live") return;
      document.body.removeEventListener("htmx:afterSwap", onSwap);
      requestAnimationFrame(restore);
    });
  }

  let videosRefreshAt = 0;
  function maybeRefreshSeriesVideos(ev) {
    let kind = "";
    try {
      const data = JSON.parse(ev.data || "{}");
      kind = data.kind || "";
    } catch (_) {}
    if (ev.type === "task.done" || ev.type === "task.failed") {
      refreshSeriesVideos(true);
      return;
    }
    if (ev.type === "task.updated" && kind === "scan") {
      const now = Date.now();
      if (now - videosRefreshAt < 2000) return;
      videosRefreshAt = now;
      refreshSeriesVideos(true);
    }
  }

  let seriesListRefreshAt = 0;
  function maybeRefreshSeriesList(ev) {
    if (!onSeriesListPage()) return;
    let kind = "";
    try {
      const data = JSON.parse(ev.data || "{}");
      kind = data.kind || "";
    } catch (_) {}
    // Drop deleted series (and clear deleting state) when delete_files finishes.
    if ((ev.type === "task.done" || ev.type === "task.failed") && kind === "delete_files") {
      refreshSeriesList(true);
      return;
    }
    if (ev.type === "task.updated" && kind === "delete_files") {
      const now = Date.now();
      if (now - seriesListRefreshAt < 2000) return;
      seriesListRefreshAt = now;
      refreshSeriesList(true);
    }
  }

  function onSSE(ev) {
    refreshBadge();
    if (ev.type === "notification.created" || ev.type === "notification.read") {
      let uc;
      try {
        const data = JSON.parse(ev.data || "{}");
        if (typeof data.unread_count === "number") uc = data.unread_count;
      } catch (_) {}
      refreshNotifyBadge(uc);
      refreshNotifyDropdown();
      refreshNotificationHistoryPanel();
      return;
    }
    if (ev.type === "task.updated") {
      // In-place patch on Tasks page - full swap recreates Cancel buttons every tick.
      if (!patchTaskRow(ev)) refreshTasksPanel();
      patchTaskDetail(ev);
      refreshTaskIndicators();
      refreshVideoHistoryIfMatch(ev);
      refreshVideoStreamCacheIfMatch(ev);
      refreshTaskVideoHistoryIfMatch(ev);
    } else if (ev.type === "task.done" || ev.type === "task.failed") {
      refreshTasksPanel();
      refreshTaskIndicators();
      refreshHistoryPanel();
      reloadTaskDetailIfMatch(ev);
      reloadVideoDetailIfMatch(ev);
    }
    maybeRefreshSeriesVideos(ev);
    maybeRefreshSeriesList(ev);
    if (typeof window.refreshImportFullScanNote === "function") {
      window.refreshImportFullScanNote(ev);
    }
  }

  // Full-page nav that should not jump to top: mark link/form with js-keep-scroll.
  // Live panels (e.g. #series-videos-live) use HTMX + dataset restore instead.
  const SCROLL_KEY = "creatorr:keep-scroll";

  function saveKeepScroll() {
    sessionStorage.setItem(
      SCROLL_KEY,
      JSON.stringify({ path: location.pathname, y: window.scrollY })
    );
  }

  function restoreKeepScroll() {
    const raw = sessionStorage.getItem(SCROLL_KEY);
    if (raw == null) return;
    sessionStorage.removeItem(SCROLL_KEY);
    let data;
    try {
      data = JSON.parse(raw);
    } catch {
      return;
    }
    if (!data || data.path !== location.pathname) return;
    const top = Number(data.y);
    if (!Number.isFinite(top)) return;
    requestAnimationFrame(() => window.scrollTo(0, top));
  }

  function saveSeriesScroll() {
    saveKeepScroll();
  }

  function restoreSeriesScroll() {
    restoreKeepScroll();
  }

  function connectEvents() {
    if (!window.EventSource) return;
    const es = new EventSource("/api/events");
    ["task.updated", "task.done", "task.failed", "notification.created", "notification.read"].forEach((name) => {
      es.addEventListener(name, onSSE);
    });
    es.onerror = () => {
      // Browser reconnects automatically; keep polling badge as fallback.
    };
  }

  /** Rewrite <time data-local-time datetime="…"> to browser locale + timezone. */
  function formatLocalTimes(root) {
    let scope = root && root.nodeType === 1 ? root : document;
    if (scope !== document && scope.isConnected === false) {
      const id = scope.id;
      scope = (id && document.getElementById(id)) || document.body;
    }
    const nodes = [];
    if (scope.matches && scope.matches("time[data-local-time]")) nodes.push(scope);
    if (scope.querySelectorAll) {
      scope.querySelectorAll("time[data-local-time]").forEach((el) => nodes.push(el));
    }
    nodes.forEach((el) => {
      const raw = el.getAttribute("datetime");
      if (!raw) return;
      const d = new Date(raw);
      if (Number.isNaN(d.getTime())) return;
      el.textContent = d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
    });
  }

  function createLucideIcons(root) {
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
  function htmxSwapRoot(ev) {
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

  const fieldInfoPlaces = ["tooltip-top", "tooltip-bottom", "tooltip-left", "tooltip-right"];
  const fieldInfoAligns = ["tooltip-start", "tooltip-center", "tooltip-end"];

  /** Tip root may be a wrapper or the .js-field-info button itself (join case). */
  function fieldInfoButton(tip) {
    if (tip.classList.contains("js-field-info")) return tip;
    return tip.querySelector(".js-field-info");
  }

  function fieldInfoRoot(el) {
    if (!(el instanceof Element)) return null;
    const btn = el.closest(".js-field-info");
    if (!btn) return null;
    if (btn.classList.contains("tooltip")) return btn;
    return (
      btn.closest(".tooltip:has(> .js-field-info)") ||
      btn.closest(".tooltip:has(> .tooltip-content)") ||
      btn.closest(".tooltip")
    );
  }

  function clearFieldInfoPlacement(tip) {
    tip.classList.remove(...fieldInfoPlaces, ...fieldInfoAligns);
    tip.style.removeProperty("--tt-trans");
  }

  function resetFieldInfoPlacement(tip) {
    clearFieldInfoPlacement(tip);
    tip.classList.add("tooltip-top");
  }

  /** Pick daisyUI placement so .tooltip-content fits in the viewport. */
  function placeFieldInfoTooltip(tip) {
    const content = tip.querySelector(".tooltip-content");
    const btn = fieldInfoButton(tip);
    if (!content || !btn) return;

    clearFieldInfoPlacement(tip);
    tip.classList.add("tooltip-top", "tooltip-center");

    const pad = 8;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const b = btn.getBoundingClientRect();

    let r = content.getBoundingClientRect();
    // Prefer top; flip if clipped above and bottom has room.
    tip.classList.remove("tooltip-top", "tooltip-bottom");
    if (r.top < pad && b.bottom + r.height + pad < vh) {
      tip.classList.add("tooltip-bottom");
    } else {
      tip.classList.add("tooltip-top");
    }

    r = content.getBoundingClientRect();
    tip.classList.remove(...fieldInfoAligns);
    if (r.right > vw - pad) {
      tip.classList.add("tooltip-end");
    } else if (r.left < pad) {
      tip.classList.add("tooltip-start");
    } else {
      tip.classList.add("tooltip-center");
    }

    r = content.getBoundingClientRect();
    // Still clipped horizontally → sit beside the button.
    if (r.right > vw - pad || r.left < pad) {
      tip.classList.remove("tooltip-top", "tooltip-bottom", ...fieldInfoAligns);
      if (b.left >= vw - b.right) {
        tip.classList.add("tooltip-left");
      } else {
        tip.classList.add("tooltip-right");
      }
      r = content.getBoundingClientRect();
      // Nudge vertical centering if side tip clips.
      if (r.top < pad || r.bottom > vh - pad) {
        const shift = r.top < pad ? pad - r.top : vh - pad - r.bottom;
        tip.style.setProperty("--tt-trans", `calc(-50% + ${shift}px)`);
      }
      return;
    }

    // Final horizontal clamp for top/bottom (wide tips near corners).
    r = content.getBoundingClientRect();
    if (r.left < pad || r.right > vw - pad) {
      const shift = r.left < pad ? pad - r.left : vw - pad - r.right;
      const base = tip.classList.contains("tooltip-end") || tip.classList.contains("tooltip-start") ? "0px" : "-50%";
      tip.style.setProperty("--tt-trans", `calc(${base} + ${shift}px)`);
    }
  }

  function setFieldInfoTooltip(tip, open) {
    const btn = fieldInfoButton(tip);
    if (!open) {
      tip.classList.remove("tooltip-open");
      resetFieldInfoPlacement(tip);
      if (btn) {
        btn.classList.remove("btn-active");
        btn.setAttribute("aria-pressed", "false");
      }
      return;
    }
    tip.classList.add("tooltip-open");
    if (btn) {
      btn.classList.add("btn-active");
      btn.setAttribute("aria-pressed", "true");
    }
    // Measure after open styles apply (opacity / --tt-pos).
    requestAnimationFrame(() => placeFieldInfoTooltip(tip));
  }

  function closeFieldInfoTooltips(except) {
    document.querySelectorAll(".tooltip.tooltip-open").forEach((el) => {
      if (except && el === except) return;
      if (!fieldInfoButton(el)) return;
      setFieldInfoTooltip(el, false);
    });
  }

  document.body.addEventListener("click", (ev) => {
    const tip = fieldInfoRoot(ev.target);
    if (tip) {
      ev.preventDefault();
      ev.stopPropagation();
      const open = tip.classList.contains("tooltip-open");
      closeFieldInfoTooltips(tip);
      setFieldInfoTooltip(tip, !open);
      return;
    }
    if (!ev.target.closest(".tooltip.tooltip-open")) {
      closeFieldInfoTooltips();
    }
  });

  // Hover path also shows daisyUI tips - place before paint when possible.
  document.body.addEventListener(
    "pointerenter",
    (ev) => {
      const tip = fieldInfoRoot(ev.target);
      if (!tip) return;
      placeFieldInfoTooltip(tip);
    },
    true
  );

  document.body.addEventListener("change", (ev) => {
    const t = ev.target;
    if (!t || !t.classList || !t.classList.contains("modal-toggle")) return;
    if (!t.checked) closeFieldInfoTooltips();
  });

  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape") closeFieldInfoTooltips();
  });

  function scheduleFlashToasts() {
    document.querySelectorAll("[data-flash-toast]").forEach((el) => {
      if (el.dataset.flashScheduled) return;
      el.dataset.flashScheduled = "1";
      window.setTimeout(() => {
        el.remove();
      }, 3500);
    });
  }

  /** Client-side flash toast (same markup as partials/flash_toast). opts: { error, warning }. */
  window.showFlashToast = function (message, opts) {
    opts = opts || {};
    const toast = document.createElement("div");
    toast.className = "toast toast-top toast-end z-[60]";
    toast.setAttribute("data-flash-toast", "");
    const alert = document.createElement("div");
    alert.setAttribute("role", "status");
    let kind = "alert-success";
    if (opts.error) kind = "alert-error";
    else if (opts.warning) kind = "alert-warning";
    alert.className = "alert " + kind + " shadow-lg";
    const span = document.createElement("span");
    span.textContent = String(message || "");
    alert.appendChild(span);
    toast.appendChild(alert);
    document.body.appendChild(toast);
    scheduleFlashToasts();
  };

  function initFlashToasts() {
    const el = document.querySelector("[data-flash-toast]");
    if (!el) return;
    try {
      const u = new URL(location.href);
      let dirty = false;
      for (const k of ["ok", "err", "rewrote", "failed"]) {
        if (u.searchParams.has(k)) {
          u.searchParams.delete(k);
          dirty = true;
        }
      }
      if (dirty) {
        const q = u.searchParams.toString();
        history.replaceState({}, "", u.pathname + (q ? "?" + q : "") + u.hash);
      }
    } catch (_) {}
    scheduleFlashToasts();
  }

  function syncRangeOutput(el) {
    if (!(el instanceof HTMLInputElement) || el.type !== "range") return;
    const sel = el.getAttribute("data-range-output");
    if (!sel) return;
    const out = document.querySelector(sel);
    if (!out) return;
    const labelsRaw = el.getAttribute("data-range-labels");
    if (labelsRaw) {
      try {
        const labels = JSON.parse(labelsRaw);
        const idx = Number(el.value);
        if (Array.isArray(labels) && idx >= 0 && idx < labels.length) {
          out.textContent = labels[idx];
          return;
        }
      } catch (_) {}
    }
    const zero = el.getAttribute("data-range-zero");
    if (zero != null && el.value === "0") {
      out.textContent = zero;
      return;
    }
    const unit = el.getAttribute("data-range-unit") || "";
    out.textContent = el.value + unit;
  }

  function initRangeOutputs() {
    document.querySelectorAll('input[type="range"][data-range-output]').forEach(syncRangeOutput);
  }

  document.body.addEventListener("input", (ev) => {
    syncRangeOutput(ev.target);
  });

  function initSponsorBlockExclusive() {
    document.body.addEventListener("change", (ev) => {
      const el = ev.target;
      if (!el || !el.matches || !el.matches("input[data-sb-exclusive]")) return;
      if (!el.checked) return;
      const root = el.closest("[data-sb-profile]");
      if (!root) return;
      const side = el.getAttribute("data-sb-exclusive");
      const cat = el.getAttribute("data-sb-cat");
      const other = side === "mark" ? "remove" : "mark";
      root.querySelectorAll(`input[data-sb-exclusive="${other}"][data-sb-cat="${cat}"]`).forEach((o) => {
        o.checked = false;
      });
    });
  }

  function syncSponsorBlockCardsGate(root) {
    if (!root) return;
    const reenc = root.querySelector("input[data-sb-reencode]");
    const cards = root.querySelector("input[data-sb-cards]");
    const wrap = root.querySelector("[data-sb-cards-wrap]");
    if (!reenc || !cards) return;
    const on = !!reenc.checked;
    cards.disabled = !on;
    if (!on) cards.checked = false;
    if (wrap) {
      if (on) {
        wrap.classList.remove("tooltip", "tooltip-top");
        wrap.removeAttribute("data-tip");
      } else {
        wrap.classList.add("tooltip", "tooltip-top");
        wrap.setAttribute("data-tip", "Requires re-encode to be enabled");
      }
    }
  }

  function initSponsorBlockReencodeGate() {
    document.body.addEventListener("change", (ev) => {
      const el = ev.target;
      if (!el || !el.matches || !el.matches("input[data-sb-reencode]")) return;
      syncSponsorBlockCardsGate(el.closest("[data-sb-profile]"));
    });
    document.querySelectorAll("[data-sb-profile]").forEach(syncSponsorBlockCardsGate);
  }

  function syncPlaybackCacheHoursGate() {
    const toggle = document.querySelector("input.js-playback-cache-toggle");
    const hours = document.querySelector("input.js-playback-cache-hours");
    if (!toggle || !hours) return;
    const on = !!toggle.checked;
    hours.disabled = !on;
    const fieldset = hours.closest("fieldset");
    if (fieldset) fieldset.classList.toggle("opacity-60", !on);
  }

  function initPlaybackCacheHoursGate() {
    document.body.addEventListener("change", (ev) => {
      const el = ev.target;
      if (!el || !el.matches || !el.matches("input.js-playback-cache-toggle")) return;
      syncPlaybackCacheHoursGate();
    });
    syncPlaybackCacheHoursGate();
  }

  document.addEventListener("DOMContentLoaded", () => {
    createLucideIcons();
    formatLocalTimes();
    restoreSeriesScroll();
    refreshBadge();
    refreshNotifyBadge();
    refreshNotifyDropdown();
    document.querySelectorAll("[data-notify-redirect]").forEach((el) => {
      el.value = location.pathname + location.search + location.hash;
    });
    connectEvents();
    initFlashToasts();
    initRangeOutputs();
    initSponsorBlockExclusive();
    initSponsorBlockReencodeGate();
    initPlaybackCacheHoursGate();
    syncAllRateLimitJoins();
    syncAllScanCronJoins();
    document.querySelectorAll("form.js-add-series-form").forEach(syncAddSeriesForm);
    openAddSeriesModal();
    openSeriesMetadataModal();
    // Slow fallback if SSE unavailable.
    setInterval(() => {
      refreshBadge();
      refreshNotifyBadge();
      if (document.getElementById("tasks-live")) refreshTasksPanel();
    }, 15000);
  });

  document.body.addEventListener("htmx:beforeRequest", (ev) => {
    const cfg = ev.detail && ev.detail.requestConfig;
    const target = (cfg && cfg.target) || (ev.detail && ev.detail.target);
    if (target && (target.id === "series-videos-live" || target.id === "series-list-live")) {
      document.body.dataset.listLiveScrollY = String(window.scrollY);
      captureListFilterQFocus();
    }
  });

  document.body.addEventListener("htmx:afterSwap", (ev) => {
    const root = htmxSwapRoot(ev);
    createLucideIcons(root);
    formatLocalTimes(root);
    const y = document.body.dataset.listLiveScrollY;
    if (y != null && root && (root.id === "series-videos-live" || root.id === "series-list-live")) {
      delete document.body.dataset.listLiveScrollY;
      const top = Number(y);
      if (Number.isFinite(top)) requestAnimationFrame(() => window.scrollTo(0, top));
      restoreListFilterQFocus(root);
    }
  });
  document.body.addEventListener("htmx:oobAfterSwap", (ev) => {
    const root = htmxSwapRoot(ev);
    createLucideIcons(root);
    formatLocalTimes(root);
    scheduleFlashToasts();
  });

  document.body.addEventListener("click", (ev) => {
    const a = ev.target.closest("a.js-keep-scroll");
    if (!a || a.hasAttribute("hx-get") || a.hasAttribute("hx-post")) return;
    saveKeepScroll();
  });
  document.body.addEventListener("submit", (ev) => {
    const form = ev.target.closest("form.js-keep-scroll");
    if (!form) return;
    if (form.hasAttribute("hx-get") || form.hasAttribute("hx-post")) return;
    saveKeepScroll();
  });
  function openAddSeriesModal() {
    if (new URLSearchParams(location.search).get("add") !== "1") return;
    const el = document.getElementById("modal-add-series");
    if (el) el.checked = true;
  }

  function openSeriesMetadataModal() {
    const q = new URLSearchParams(location.search);
    if (q.get("meta") !== "1" && !q.get("prefetch_task")) return;
    const el = document.getElementById("modal-edit-series-metadata");
    if (el) el.checked = true;
  }

  function setPanelControls(panel, enabled) {
    if (!panel) return;
    // Caller owns panel visibility (.hidden). Only soft-enable/disable controls here.
    panel.querySelectorAll("input, select, textarea").forEach((el) => {
      // Remember HTML-permanent disabled (e.g. Stream when External Creatorr URL unset)
      // before we soft-disable for a hidden wizard step.
      if (el.dataset.permanentlyDisabled == null) {
        el.dataset.permanentlyDisabled =
          el.disabled && el.dataset.panelSoftDisabled !== "1" ? "1" : "0";
      }
      if (el.dataset.permanentlyDisabled === "1") {
        el.disabled = true;
        return;
      }
      if (enabled) {
        el.disabled = false;
        delete el.dataset.panelSoftDisabled;
      } else {
        el.disabled = true;
        el.dataset.panelSoftDisabled = "1";
      }
    });
  }

  function clearPanelValues(panel) {
    if (!panel) return;
    panel.querySelectorAll("input, select, textarea").forEach((el) => {
      if (el.type === "hidden") return;
      if (el.type === "checkbox" || el.type === "radio") {
        el.checked = el.defaultChecked;
      } else if (el.tagName === "SELECT") {
        el.selectedIndex = 0;
        for (let i = 0; i < el.options.length; i++) {
          if (el.options[i].defaultSelected) {
            el.selectedIndex = i;
            break;
          }
        }
      } else {
        el.value = "";
      }
    });
  }

  function existingAddSeriesSourceURLs() {
    const el = document.getElementById("add-series-all-source-urls");
    if (!el) return [];
    try {
      const v = JSON.parse(el.textContent || "[]");
      return Array.isArray(v) ? v.map(normalizeSourceURLClient) : [];
    } catch (_) {
      return [];
    }
  }

  /** daisyUI validator hint sibling (https://daisyui.com/components/validator/). */
  function controlValidatorHint(el) {
    if (!el) return null;
    const join = el.closest(".join.validator");
    if (join) {
      let n = join.nextElementSibling;
      while (n && n.tagName === "DATALIST") n = n.nextElementSibling;
      if (n && n.classList.contains("validator-hint")) return n;
    }
    let n = el.nextElementSibling;
    while (n && n.tagName === "DATALIST") n = n.nextElementSibling;
    if (n && n.classList.contains("validator-hint")) return n;
    const fs = el.closest("fieldset");
    if (fs) {
      const hints = fs.querySelectorAll(":scope > .validator-hint");
      if (hints.length) return hints[hints.length - 1];
    }
    return null;
  }

  /** Mark control invalid via aria-invalid; daisyUI paints error + shows sibling .validator-hint. */
  function setControlValidity(el, msg) {
    if (!el) return;
    const text = String(msg || "").trim();
    const invalid = !!text;
    if (invalid) el.setAttribute("aria-invalid", "true");
    else el.removeAttribute("aria-invalid");
    const join = el.closest(".join.validator");
    if (join) {
      if (invalid) join.setAttribute("aria-invalid", "true");
      else join.removeAttribute("aria-invalid");
    }
    const hint = controlValidatorHint(el);
    if (hint && text) hint.textContent = text;
  }
  window.setControlValidity = setControlValidity;

  function clearControlValidity(el) {
    setControlValidity(el, "");
  }
  window.clearControlValidity = clearControlValidity;

  function clearFormControlValidity(root) {
    if (!root) return;
    root.querySelectorAll("[aria-invalid]").forEach((el) => el.removeAttribute("aria-invalid"));
  }
  window.clearFormControlValidity = clearFormControlValidity;

  function addSeriesURLClash(form) {
    if (!form) return false;
    const urlEl = form.querySelector("#add-series-url");
    const typed = normalizeSourceURLClient(urlEl && urlEl.value);
    if (!typed) return false;
    return existingAddSeriesSourceURLs().some((u) => u === typed);
  }

  function addSeriesURLInvalid(form) {
    if (!form) return false;
    const urlEl = form.querySelector("#add-series-url");
    const raw = String((urlEl && urlEl.value) || "").trim();
    return raw !== "" && !isValidSourceURLClient(raw);
  }

  function syncAddSeriesSourceNav(form) {
    const urlEl = form.querySelector("#add-series-url");
    const cont = form.querySelector(".js-add-series-fetch");
    if (!urlEl) return;
    const has = String(urlEl.value || "").trim() !== "";
    const invalid = addSeriesURLInvalid(form);
    const clash = !invalid && addSeriesURLClash(form);
    const blocked = form.querySelector("[data-add-series-submit]")?.getAttribute("data-blocked") === "1";
    if (cont) cont.disabled = blocked || !has || clash || invalid || cont.dataset.busy === "1";
    if (invalid) {
      setControlValidity(urlEl, "Enter a valid http(s) URL with a host.");
    } else if (clash) {
      setControlValidity(urlEl, "This URL is already a source on another series.");
    } else {
      clearControlValidity(urlEl);
    }
  }

  function setAddSeriesAlert(form, msg) {
    const errEl = form.querySelector(".js-add-series-fetch-err");
    if (!errEl) return;
    const span = errEl.querySelector("span") || errEl;
    span.textContent = String(msg || "").replace(/^conflict:\s*/i, "").trim();
    // Visibility is owned by syncAddSeriesForm (needs step + message).
  }

  /** Route create/fetch errors to daisyUI validators when the message maps to a field. */
  function setAddSeriesFetchErr(form, msg) {
    if (!form) return;
    clearFormControlValidity(form);
    const text = String(msg || "").replace(/^conflict:\s*/i, "").trim();
    if (!text) {
      setAddSeriesAlert(form, "");
      return;
    }
    const lower = text.toLowerCase();
    let field = null;
    if (/\btitle\b/.test(lower) && /required|already exists|same root/.test(lower)) {
      field = form.querySelector("#add-series-title");
    } else if (/source url|url already|valid http|with a host|already used by series/.test(lower)) {
      field = form.querySelector("#add-series-url");
    } else if (/\broot\b/.test(lower)) {
      field = form.querySelector('select[name="root_id"]');
    } else if (/quality|profile/.test(lower)) {
      field = form.querySelector('select[name="quality_profile_id"]');
    } else if ((form.dataset.addSeriesStep || "") === "source") {
      field = form.querySelector("#add-series-url");
    }
    if (field) {
      if (field.id === "add-series-url") {
        form.dataset.addSeriesStep = "source";
      } else {
        form.dataset.addSeriesStep = "series";
      }
      setControlValidity(field, text);
      setAddSeriesAlert(form, "");
      try {
        field.focus();
        if (typeof field.select === "function") field.select();
      } catch (_) {}
      return;
    }
    setAddSeriesAlert(form, text);
  }
  window.setAddSeriesFetchErr = setAddSeriesFetchErr;

  /** Snapshot form as urlencoded body (matches server ParseForm / tests). Includes disabled fields. */
  function serializeAddSeriesForm(form) {
    const params = new URLSearchParams();
    form.querySelectorAll("input, select, textarea").forEach((el) => {
      if (!el.name || el.type === "submit" || el.type === "button" || el.type === "file" || el.type === "reset") {
        return;
      }
      if (el.type === "checkbox") {
        // Disabled checkbox: companion hidden may carry the value.
        // Disabled radio: Import-forced index_as_ignored uses a companion hidden.
        if (el.disabled) return;
        if (el.checked) params.append(el.name, el.value || "1");
        return;
      }
      if (el.type === "radio") {
        if (el.disabled || !el.checked) return;
        params.append(el.name, el.value);
        return;
      }
      params.append(el.name, el.value);
    });
    // Title must always win over any earlier empty same-name control.
    const titleEl = form.querySelector("#add-series-title") || form.querySelector('input[name="title"]');
    if (titleEl) params.set("title", String(titleEl.value || ""));
    // Manual path: drop empty source_url so the handler takes the manual branch cleanly.
    if ((form.dataset.addSeriesMode || "") !== "url") {
      const su = String(params.get("source_url") || "").trim();
      if (!su) params.delete("source_url");
    }
    return params;
  }

  // AJAX create: keep modal + draft on error so the operator can fix the title.
  document.body.addEventListener("submit", async (ev) => {
    const form = ev.target.closest("form.js-add-series-form");
    if (!form) return;
    ev.preventDefault();
    const submitBtn = form.querySelector("[data-add-series-submit]");
    if (submitBtn) submitBtn.disabled = true;
    setAddSeriesFetchErr(form, "");
    // Enable series controls before read so soft-disable cannot drop title.
    const seriesPanel = form.querySelector('[data-add-series-step="series"]');
    if (seriesPanel) setPanelControls(seriesPanel, true);
    const titleEl = form.querySelector("#add-series-title") || form.querySelector('input[name="title"]');
    const titleVal = String((titleEl && titleEl.value) || "").trim();
    if (!titleVal) {
      const mode = form.dataset.addSeriesMode || "";
      setAddSeriesFetchErr(
        form,
        mode === "manual"
          ? "title is required when creating manually"
          : "title is required - fetch metadata again or enter a title"
      );
      form.dataset.addSeriesStep = "series";
      syncAddSeriesForm(form);
      return;
    }
    const body = serializeAddSeriesForm(form);
    // Belt: ensure title is present after snapshot (defends against empty append races).
    body.set("title", titleVal);
    syncAddSeriesForm(form);
    try {
      const res = await fetch("/actions/add-series", {
        method: "POST",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
        },
        body: body.toString(),
        credentials: "same-origin",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        const msg = (data && (data.error || data.message)) || ("Create series failed (" + res.status + ")");
        setAddSeriesFetchErr(form, msg);
        if (!form.dataset.addSeriesStep || form.dataset.addSeriesStep === "fetching") {
          form.dataset.addSeriesStep = "series";
        }
        syncAddSeriesForm(form);
        return;
      }
      if (!data || data.id == null) {
        setAddSeriesFetchErr(form, "Create series failed: invalid response");
        form.dataset.addSeriesStep = "series";
        syncAddSeriesForm(form);
        return;
      }
      if (form.dataset.importMatchLock === "1") {
        form.dispatchEvent(new CustomEvent("creatorr:series-created", {
          bubbles: true,
          detail: { id: data.id, title: data.title || "", warning: data.warning || "" },
        }));
        return;
      }
      if (data.warning) {
        location.assign("/series/" + data.id + "?err=" + encodeURIComponent(data.warning));
        return;
      }
      location.assign("/series/" + data.id);
    } catch (e) {
      setAddSeriesFetchErr(form, e && e.message ? e.message : "Create series failed");
      form.dataset.addSeriesStep = "series";
      syncAddSeriesForm(form);
    } finally {
      syncAddSeriesForm(form);
    }
  });

  // Steps: "" (choice) | "source" | "fetching" | "series". Mode: "url" | "manual".
  function syncAddSeriesForm(form) {
    if (!form || !form.classList.contains("js-add-series-form")) return;
    const mode = form.dataset.addSeriesMode || "";
    const step = form.dataset.addSeriesStep || "";
    const choice = form.querySelector("[data-add-series-choice]");
    const stepsBar = form.querySelector("[data-add-series-steps]");
    const source = form.querySelector('[data-add-series-step="source"]');
    const fetching = form.querySelector('[data-add-series-step="fetching"]');
    const series = form.querySelector('[data-add-series-step="series"]');
    const submit = form.querySelector("[data-add-series-submit]");
    const titleEl = form.querySelector("#add-series-title");
    const errEl = form.querySelector(".js-add-series-fetch-err");
    const titleWrap = titleEl && (titleEl.closest(".flex.items-center") || titleEl.closest(".flex"));
    const titleInfo = titleWrap
      ? titleWrap.querySelector(".tooltip-content p")
      : form.querySelector(".js-add-series-title-help");


    if (choice) choice.classList.toggle("hidden", step !== "");
    if (stepsBar) {
      const showSteps = mode === "url" && step !== "";
      stepsBar.classList.toggle("hidden", !showSteps);
      const order = ["source", "fetching", "series"];
      const cur = order.indexOf(step);
      stepsBar.querySelectorAll("[data-add-series-step-ind]").forEach((li) => {
        const i = order.indexOf(li.getAttribute("data-add-series-step-ind"));
        li.classList.toggle("step-primary", showSteps && i >= 0 && i <= cur);
      });
    }
    // Keep alert under steps; show on source or series when it has text (title conflicts stay editable).
    if (errEl) {
      const hasMsg = !!(errEl.querySelector("span") || errEl).textContent.trim();
      errEl.classList.toggle("hidden", !hasMsg || step === "" || step === "fetching");
    }
    if (source) {
      // Keep source fields enabled on later URL steps so they still submit; only hide.
      source.classList.toggle("hidden", step !== "source");
      source.querySelectorAll("input, select, textarea").forEach((el) => {
        if (el.type === "hidden") return;
        el.disabled = mode !== "url";
      });
    }
    if (fetching) fetching.classList.toggle("hidden", step !== "fetching");
    if (series) {
      const showSeries = step === "series";
      series.classList.toggle("hidden", !showSeries);
      // Keep series fields enabled for the whole URL/manual flow (hide-only when off-step).
      // Soft-disable made title look filled while submit omitted it (browser skips disabled).
      setPanelControls(series, mode === "manual" || mode === "url");
    }
    if (titleInfo) {
      titleInfo.textContent = mode === "manual"
        ? "Display name for this series. Add sources after creation from the series page."
        : "Filled from the channel/playlist when available. Edit if needed.";
    }
    if (titleEl) titleEl.required = step === "series";
    const cont = form.querySelector("[data-add-series-continue]");
    if (cont) cont.classList.toggle("hidden", step !== "source");
    if (submit) {
      const blocked = submit.getAttribute("data-blocked") === "1";
      const onSeries = step === "series";
      submit.classList.toggle("hidden", !onSeries);
      submit.disabled = blocked || !onSeries;
    }
    if (step === "source") syncAddSeriesSourceNav(form);
    syncImportAddSeriesIgnored(form);
  }

  /** On 'Import', force discovered status to 'Ignored' + disable join (tooltip); hidden keeps value. */
  function syncImportAddSeriesIgnored(form) {
    if (!form) return;
    const join = form.querySelector("[data-index-as-ignored-join]");
    if (!join) return;
    const radios = join.querySelectorAll('input[type="radio"][name="index_as_ignored"]');
    if (!radios.length) return;
    const lock =
      form.dataset.importMatchLock === "1" ||
      location.pathname === "/import" ||
      location.pathname.endsWith("/import");
    const tip =
      "Required on 'Import' so new videos are indexed for matching without downloading.";
    let tipWrap = join.parentElement;
    if (tipWrap && !tipWrap.classList.contains("tooltip")) tipWrap = null;
    let hidden = form.querySelector('input[type="hidden"][name="index_as_ignored"][data-import-force]');

    if (lock) {
      radios.forEach((r) => {
        r.checked = r.value === "1";
        r.disabled = true;
      });
      if (!hidden) {
        hidden = document.createElement("input");
        hidden.type = "hidden";
        hidden.name = "index_as_ignored";
        hidden.value = "1";
        hidden.dataset.importForce = "1";
        join.insertAdjacentElement("beforebegin", hidden);
      } else {
        hidden.value = "1";
      }
      join.classList.add("opacity-60", "pointer-events-none");
      if (!tipWrap) {
        tipWrap = document.createElement("span");
        tipWrap.className = "tooltip tooltip-top block w-full";
        join.parentNode.insertBefore(tipWrap, join);
        tipWrap.appendChild(join);
      }
      tipWrap.setAttribute("data-tip", tip);
      return;
    }

    if (hidden) hidden.remove();
    radios.forEach((r) => {
      r.disabled = false;
    });
    join.classList.remove("opacity-60", "pointer-events-none");
    if (tipWrap) {
      tipWrap.removeAttribute("data-tip");
      const parent = tipWrap.parentNode;
      if (parent) {
        parent.insertBefore(join, tipWrap);
        tipWrap.remove();
      }
    }
  }

  window.syncAddSeriesForm = syncAddSeriesForm;

  document.body.addEventListener("click", (ev) => {
    const pick = ev.target.closest(".js-add-series-pick");
    if (!pick) return;
    const form = pick.closest("form.js-add-series-form");
    if (!form) return;
    const mode = pick.getAttribute("data-mode");
    if (!mode) return;
    const source = form.querySelector('[data-add-series-step="source"]');
    const series = form.querySelector('[data-add-series-step="series"]');
    clearPanelValues(source);
    clearPanelValues(series);
    const tok = form.querySelector("#add-series-draft-token");
    if (tok) tok.value = "";
    setAddSeriesFetchErr(form, "");
    form.dataset.addSeriesMode = mode;
    form.dataset.addSeriesStep = mode === "url" ? "source" : "series";
    syncAddSeriesForm(form);
    if (mode === "url") form.querySelector("#add-series-url")?.focus();
    else form.querySelector("#add-series-title")?.focus();
  });

  document.body.addEventListener("click", async (ev) => {
    const btn = ev.target.closest(".js-add-series-fetch");
    if (!btn) return;
    const form = btn.closest("form.js-add-series-form");
    if (!form) return;
    const urlEl = form.querySelector("#add-series-url");
    if (!urlEl || !String(urlEl.value || "").trim() || addSeriesURLClash(form)) {
      syncAddSeriesSourceNav(form);
      return;
    }
    setAddSeriesFetchErr(form, "");
    form.dataset.addSeriesStep = "fetching";
    syncAddSeriesForm(form);
    try {
      const body = new URLSearchParams();
      body.set("source_url", String(urlEl.value || "").trim());
      const res = await fetch("/actions/fetch-add-series", {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: body.toString(),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        throw new Error(data.error || ("Fetch failed (" + res.status + ")"));
      }
      const tid = data.task_id;
      const draftToken = data.draft_token || "";
      if (!tid) {
        throw new Error("missing task id");
      }
      const result = await pollAddSeriesPrefetch(tid);
      if (result.error) {
        throw new Error(result.error);
      }
      const titleEl = form.querySelector("#add-series-title");
      if (titleEl) titleEl.value = result.title || "";
      const tok = form.querySelector("#add-series-draft-token");
      if (tok) tok.value = draftToken || result.draft_token || "";
      form.dataset.addSeriesStep = "series";
      syncAddSeriesForm(form);
      titleEl?.focus();
    } catch (e) {
      form.dataset.addSeriesStep = "source";
      setAddSeriesFetchErr(form, e && e.message ? e.message : "Fetch failed");
      syncAddSeriesForm(form);
      syncAddSeriesSourceNav(form);
    }
  });

  async function pollAddSeriesPrefetch(taskId) {
    const deadline = Date.now() + 120000;
    while (Date.now() < deadline) {
      const res = await fetch("/actions/add-series-prefetch/" + encodeURIComponent(taskId), {
        headers: { Accept: "application/json" },
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        throw new Error(data.error || ("Status failed (" + res.status + ")"));
      }
      const st = data.status;
      if (st === "pending" || st === "running") {
        await new Promise((r) => setTimeout(r, 500));
        continue;
      }
      return data;
    }
    throw new Error("Fetch timed out");
  }
  // Filter bar × clears query params for that field and reloads.
  document.body.addEventListener("click", (ev) => {
    const btn = ev.target.closest("[data-filter-clear]");
    if (!btn) return;
    ev.preventDefault();
    const name = btn.getAttribute("data-filter-clear");
    if (!name) return;
    const u = new URL(location.href);
    u.searchParams.delete(name);
    u.searchParams.delete("page");
    location.assign(u.pathname + u.search);
  });

  // List filters (js-list-filters): select → submit now; search → submit on blur.
  // Keep caret in search field across HTMX swap of live list panels.
  let listFilterQFocus = null;
  function captureListFilterQFocus() {
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
  function restoreListFilterQFocus(root) {
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
  document.body.addEventListener("focusout", (ev) => {
    const el = ev.target;
    if (!el || el.tagName !== "INPUT" || el.type !== "search") return;
    const form = el.closest("form.js-list-filters");
    if (!form) return;
    const next = ev.relatedTarget;
    if (next && form.contains(next)) return;
    submitListFilters(form);
  });

  // Cancel discards draft; closing via toggle (not backdrop - inert) keeps form state.
  document.body.addEventListener("click", (ev) => {
    const cancel = ev.target.closest("label.modal-cancel");
    if (!cancel) return;
    const modal = cancel.closest(".modal");
    if (!modal) return;
    modal.querySelectorAll("form").forEach((form) => {
      form.reset();
      resetArtSlots(form);
      delete form.dataset.addSeriesMode;
      delete form.dataset.addSeriesStep;
      delete form.dataset.importMatchLock;
      if (form.classList.contains("js-add-series-form")) setAddSeriesFetchErr(form, "");
      clearFormControlValidity(form);
      form.querySelectorAll("[data-user-edited]").forEach((el) => {
        el.dataset.userEdited = "";
        el.disabled = false;
        el.removeAttribute("aria-busy");
      });
      form.querySelectorAll("input.preset-fill-input").forEach(syncPresetChips);
      syncAllScanCronJoins(form);
      syncSourceURLForm(form);
      syncAddSeriesForm(form);
    });
  });

  function resetArtSlots(form) {
    if (!form) return;
    form.querySelectorAll("[data-art-slot]").forEach((slot) => {
      const img = slot.querySelector("[data-art-preview]");
      const wrap = slot.querySelector("[data-art-preview-wrap]");
      const pref = slot.querySelector("input[data-art-prefetch]");
      const clear = slot.querySelector("input[data-art-clear]");
      if (pref) pref.disabled = false;
      if (clear) clear.value = "";
      if (!img) return;
      const prev = img.dataset.objectUrl;
      if (prev) {
        URL.revokeObjectURL(prev);
        delete img.dataset.objectUrl;
      }
      const orig = img.dataset.origSrc || "";
      if (orig) {
        img.src = orig;
        if (wrap) wrap.classList.remove("hidden");
      } else {
        img.removeAttribute("src");
        if (wrap) wrap.classList.add("hidden");
      }
    });
  }

  function markArtCleared(slot) {
    if (!slot) return;
    const clear = slot.querySelector("input[data-art-clear]");
    const file = slot.querySelector("input[data-art-file]");
    const img = slot.querySelector("[data-art-preview]");
    const wrap = slot.querySelector("[data-art-preview-wrap]");
    const pref = slot.querySelector("input[data-art-prefetch]");
    if (clear) clear.value = "1";
    if (pref) pref.disabled = true;
    if (file) file.value = "";
    if (img) {
      const prev = img.dataset.objectUrl;
      if (prev) {
        URL.revokeObjectURL(prev);
        delete img.dataset.objectUrl;
      }
      img.removeAttribute("src");
    }
    // Hide wrap as a unit - never hide the indicator-item alone (that jumps the X).
    if (wrap) wrap.classList.add("hidden");
  }

  function syncScanCronJoin(join) {
    if (!join) return;
    const input = join.querySelector("[data-cron-input]");
    const regular = join.querySelector("[data-cron-regular]");
    if (!(input instanceof HTMLInputElement) || !(regular instanceof HTMLInputElement)) return;
    const cronPh = input.dataset.cronPlaceholder || "* * * * *";
    const cronName = input.dataset.cronName || "scan_cron";
    let hidden = join.querySelector("[data-cron-submit]");
    if (!regular.checked) {
      const cur = input.value.trim();
      if (cur && cur !== "never") input.dataset.prevCron = input.value;
      input.value = "never";
      input.disabled = true;
      input.removeAttribute("name");
      input.removeAttribute("required");
      input.placeholder = cronPh;
      input.classList.add("opacity-60");
      if (!hidden) {
        hidden = document.createElement("input");
        hidden.type = "hidden";
        hidden.setAttribute("data-cron-submit", "");
        join.insertBefore(hidden, input);
      }
      hidden.name = cronName;
      hidden.value = "";
    } else {
      input.disabled = false;
      input.name = cronName;
      input.placeholder = cronPh;
      input.classList.remove("opacity-60");
      if (hidden) hidden.remove();
      if (!input.value.trim() || input.value.trim() === "never") {
        input.value = input.dataset.prevCron || "";
      }
    }
  }

  function syncAllScanCronJoins(root) {
    (root || document).querySelectorAll("[data-cron-join]").forEach((join) => {
      const input = join.querySelector("[data-cron-input]");
      const regular = join.querySelector("[data-cron-regular]");
      if (!(input instanceof HTMLInputElement) || !(regular instanceof HTMLInputElement)) return;
      // After form.reset(), HTML may restore name/value/disabled; derive from cron text.
      const v = input.value.trim();
      regular.checked = !!v && v !== "never";
      syncScanCronJoin(join);
    });
  }

  function syncRateLimitJoin(join) {
    if (!join) return;
    const unit = join.querySelector("[data-rate-unit]");
    const num = join.querySelector("[data-rate-value]");
    if (!unit || !num) return;
    const off = unit.value === "off";
    const inherit = unit.value === "";
    num.disabled = off;
    num.readOnly = false;
    if (off) {
      num.value = "";
      num.removeAttribute("required");
    } else if (join.hasAttribute("data-rate-required") && !inherit) {
      num.required = true;
    } else {
      num.removeAttribute("required");
    }
  }

  function syncAllRateLimitJoins(root) {
    (root || document).querySelectorAll("[data-rate-limit-join]").forEach(syncRateLimitJoin);
  }

  function syncPresetChips(input) {
    // no-op: cron fields use datalist; chips removed
  }

  // Preset chips fill the linked text field (legacy).
  document.body.addEventListener("click", (ev) => {
    const artClearBtn = ev.target.closest("[data-art-clear-btn]");
    if (artClearBtn) {
      ev.preventDefault();
      markArtCleared(artClearBtn.closest("[data-art-slot]"));
      return;
    }
    const clearBtn = ev.target.closest("button[data-clear-override]");
    if (clearBtn) {
      ev.preventDefault();
      const wrap = clearBtn.closest("fieldset") || clearBtn.parentElement;
      const join = wrap && wrap.querySelector("[data-rate-limit-join]");
      const inputs = wrap && wrap.querySelectorAll("[data-override-clear]");
      if (inputs && inputs.length) {
        inputs.forEach((input) => {
          if (input.hasAttribute("data-rate-unit") && join) {
            input.value = join.getAttribute("data-default-unit") || "M";
          } else if (input.hasAttribute("data-override-reset")) {
            input.value = input.getAttribute("data-override-reset") || "";
          } else {
            input.value = "";
          }
          input.classList.add("opacity-70");
          input.dispatchEvent(new Event("input", { bubbles: true }));
          input.dispatchEvent(new Event("change", { bubbles: true }));
        });
      }
      if (join) {
        join.classList.add("opacity-70");
        syncRateLimitJoin(join);
      }
      const flareJoin = wrap && wrap.querySelector("select[name='use_flaresolverr']")?.closest(".join");
      if (flareJoin) flareJoin.classList.add("opacity-70");
      return;
    }
    const chip = ev.target.closest("button.preset-chip");
    if (!chip) return;
    ev.preventDefault();
    const target = chip.getAttribute("data-preset-for");
    const value = chip.getAttribute("data-value");
    if (!target || value == null) return;
    const input = document.querySelector(target);
    if (!input) return;
    input.value = value;
    input.dispatchEvent(new Event("input", { bubbles: true }));
    syncPresetChips(input);
    input.focus();
  });

  document.body.addEventListener("input", (ev) => {
    const el = ev.target;
    if (!(el instanceof HTMLInputElement) && !(el instanceof HTMLTextAreaElement) && !(el instanceof HTMLSelectElement)) {
      return;
    }
    if (el.id === "add-series-url") {
      const form = el.closest("form.js-add-series-form");
      if (form) syncAddSeriesSourceNav(form);
    } else if (el.getAttribute("aria-invalid") || el.closest(".join.validator")?.hasAttribute("aria-invalid")) {
      // Clear server/client field errors as the operator edits (URL sync re-applies when still bad).
      clearControlValidity(el);
    }
    if (el instanceof HTMLInputElement && el.classList.contains("preset-fill-input")) syncPresetChips(el);
    if (el instanceof HTMLInputElement && el.hasAttribute("data-cron-input")) {
      const join = el.closest("[data-cron-join]");
      const regular = join && join.querySelector("[data-cron-regular]");
      if (regular instanceof HTMLInputElement && el.value.trim() && el.value.trim() !== "never") {
        regular.checked = true;
        syncScanCronJoin(join);
      }
    }
    const rateJoin = el.closest("[data-rate-limit-join]");
    if (rateJoin && el.hasAttribute("data-rate-value")) {
      rateJoin.classList.remove("opacity-70");
    }
  });

  document.body.addEventListener("change", (ev) => {
    const el = ev.target;
    if (el instanceof HTMLSelectElement && (el.getAttribute("aria-invalid") || el.classList.contains("validator"))) {
      clearControlValidity(el);
    }
  });
  document.body.addEventListener("change", (ev) => {
    const el = ev.target;
    if (el instanceof HTMLInputElement && el.hasAttribute("data-cron-regular")) {
      const join = el.closest("[data-cron-join]");
      if (join) syncScanCronJoin(join);
      return;
    }
    if (!(el instanceof HTMLSelectElement) || !el.hasAttribute("data-rate-unit")) {
      if (el instanceof HTMLSelectElement && el.name === "use_flaresolverr") {
        const flareJoin = el.closest(".join");
        if (flareJoin) {
          flareJoin.classList.toggle("opacity-70", el.value === "default");
        }
        el.classList.remove("opacity-70");
      }
      return;
    }
    const join = el.closest("[data-rate-limit-join]");
    if (!join) return;
    if (el.value !== "") join.classList.remove("opacity-70");
    syncRateLimitJoin(join);
  });

  // Monitor toggles: sync hidden value then HTMX-submit (partial swap, no full reload).
  document.body.addEventListener("change", (ev) => {
    const toggle = ev.target.closest("input.monitor-toggle");
    if (!toggle || !toggle.form) return;
    const hidden = toggle.form.querySelector(".monitor-toggle-value");
    if (hidden) hidden.value = toggle.checked ? "1" : "0";
    if (typeof toggle.form.requestSubmit === "function") {
      toggle.form.requestSubmit();
    } else {
      toggle.form.submit();
    }
  });

  function lastPathSegment(path) {
    let p = String(path || "").replace(/\\/g, "/").replace(/\/+$/, "");
    if (!p) return "";
    const i = p.lastIndexOf("/");
    return i >= 0 ? p.slice(i + 1) : p;
  }

  function autofillNameFromPath(pathInput) {
    const nameSel = pathInput.getAttribute("data-autofill-name");
    if (!nameSel) return;
    const nameEl = document.querySelector(nameSel);
    if (!nameEl || nameEl.dataset.userEdited === "1") return;
    const seg = lastPathSegment(pathInput.value);
    if (!seg) return;
    nameEl.value = seg;
  }

  document.body.addEventListener("input", (ev) => {
    const el = ev.target;
    if (!(el instanceof HTMLInputElement)) return;
    if (el.hasAttribute("data-autofill-name")) autofillNameFromPath(el);
  });
  document.body.addEventListener("change", (ev) => {
    const el = ev.target;
    if (!(el instanceof HTMLInputElement)) return;
    if (el.hasAttribute("data-autofill-name")) autofillNameFromPath(el);
  });

  // Approximate Go normalizeSourceURL: trim, lower host, strip leading www.
  function normalizeSourceURLClient(raw) {
    const s = String(raw || "").trim();
    if (!s) return "";
    try {
      const u = new URL(s);
      let host = (u.hostname || "").toLowerCase();
      if (host.startsWith("www.")) host = host.slice(4);
      u.hostname = host;
      return u.toString();
    } catch (_) {
      return s;
    }
  }

  // Match library.ValidateSourceURL: http(s) with a host.
  function isValidSourceURLClient(raw) {
    const s = String(raw || "").trim();
    if (!s) return false;
    try {
      const u = new URL(s);
      const scheme = String(u.protocol || "").toLowerCase();
      if (scheme !== "http:" && scheme !== "https:") return false;
      return String(u.hostname || "").trim() !== "";
    } catch (_) {
      return false;
    }
  }

  function existingSourceURLs() {
    const el = document.getElementById("series-source-urls");
    if (!el) return [];
    try {
      const v = JSON.parse(el.textContent || "[]");
      return Array.isArray(v) ? v.map(normalizeSourceURLClient) : [];
    } catch (_) {
      return [];
    }
  }

  function syncSourceURLForm(form) {
    if (!form || !form.classList.contains("js-source-url-form")) return;
    const input = form.querySelector(".js-source-url");
    const submit = form.querySelector(".js-source-url-submit");
    if (!input || !submit) return;
    const raw = String(input.value || "").trim();
    const cur = normalizeSourceURLClient(form.getAttribute("data-current-url") || "");
    const typed = normalizeSourceURLClient(input.value);
    const invalid = raw !== "" && !isValidSourceURLClient(raw);
    const existing = existingSourceURLs();
    const clash = !invalid && typed !== "" && existing.some((u) => u === typed && u !== cur);
    submit.disabled = clash || invalid;
    if (invalid) {
      setControlValidity(input, "Enter a valid http(s) URL with a host.");
    } else if (clash) {
      setControlValidity(input, "This URL is already a source on this series.");
    } else {
      clearControlValidity(input);
    }
  }

  document.body.addEventListener("input", (ev) => {
    const input = ev.target.closest(".js-source-url");
    if (!input) return;
    syncSourceURLForm(input.closest("form"));
  });
  document.body.addEventListener("submit", (ev) => {
    const form = ev.target.closest(".js-source-url-form");
    if (!form) return;
    syncSourceURLForm(form);
    const submit = form.querySelector(".js-source-url-submit");
    if (submit && submit.disabled) ev.preventDefault();
  });

  document.body.addEventListener("change", (ev) => {
    const input = ev.target.closest("input[data-art-file]");
    if (!input || input.type !== "file") return;
    const slot = input.closest("[data-art-slot]");
    if (!slot) return;
    const img = slot.querySelector("[data-art-preview]");
    const wrap = slot.querySelector("[data-art-preview-wrap]");
    if (!img) return;
    const pref = slot.querySelector("input[data-art-prefetch]");
    const clear = slot.querySelector("input[data-art-clear]");
    const prev = img.dataset.objectUrl;
    if (prev) {
      URL.revokeObjectURL(prev);
      delete img.dataset.objectUrl;
    }
    const file = input.files && input.files[0];
    if (!file || !String(file.type || "").startsWith("image/")) {
      if (pref) pref.disabled = false;
      if (clear && clear.value === "1") {
        img.removeAttribute("src");
        if (wrap) wrap.classList.add("hidden");
        return;
      }
      const orig = img.dataset.origSrc || "";
      if (orig) {
        img.src = orig;
        if (wrap) wrap.classList.remove("hidden");
      } else {
        img.removeAttribute("src");
        if (wrap) wrap.classList.add("hidden");
      }
      return;
    }
    // Do not submit stale prefetch path when a new file was chosen.
    if (pref) pref.disabled = true;
    if (clear) clear.value = "";
    const url = URL.createObjectURL(file);
    img.dataset.objectUrl = url;
    img.src = url;
    if (wrap) wrap.classList.remove("hidden");
  });

  function moveListRow(row, dir) {
    if (!row || !row.parentElement) return;
    if (dir < 0) {
      const prev = row.previousElementSibling;
      if (prev) row.parentElement.insertBefore(row, prev);
      return;
    }
    const next = row.nextElementSibling;
    if (next) row.parentElement.insertBefore(next, row);
  }

  function actorRowHTML(name, role) {
    const esc = (s) =>
      String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
    const roleLabel = role ? esc(role) : "-";
    return (
      '<div class="flex gap-1 items-center" data-actor-row>' +
      '<input type="hidden" name="actor_name" value="' +
      esc(name) +
      '" />' +
      '<input type="hidden" name="actor_role" value="' +
      esc(role) +
      '" />' +
      '<span class="tabular-nums text-xs opacity-60 w-4 shrink-0 text-right" data-list-ord aria-hidden="true"></span>' +
      '<div class="join shrink-0">' +
      '<button type="button" class="btn btn-ghost btn-sm btn-square join-item" data-actor-up aria-label="Move actor up"><i data-lucide="chevron-up" class="size-4"></i></button>' +
      '<button type="button" class="btn btn-ghost btn-sm btn-square join-item" data-actor-down aria-label="Move actor down"><i data-lucide="chevron-down" class="size-4"></i></button>' +
      "</div>" +
      '<div class="grid grid-cols-2 gap-2 grow min-w-0 text-sm">' +
      '<span class="truncate">' +
      esc(name) +
      "</span>" +
      '<span class="truncate opacity-60">' +
      roleLabel +
      "</span>" +
      "</div>" +
      '<button type="button" class="btn btn-ghost btn-sm btn-square shrink-0" data-actor-remove aria-label="Remove actor"><i data-lucide="minus" class="size-4"></i></button>' +
      "</div>"
    );
  }

  function syncActorsEmpty(list) {
    if (!list) return;
    const empty = list.querySelector("[data-actors-empty]");
    const hasRows = list.querySelector("[data-actor-row]");
    if (hasRows) {
      if (empty) empty.remove();
      return;
    }
    if (!empty) {
      list.insertAdjacentHTML(
        "afterbegin",
        '<p class="text-sm opacity-60" data-actors-empty>none</p>'
      );
    }
  }

  function commitActorDraft(editor) {
    if (!editor) return false;
    const nameEl = editor.querySelector("[data-actor-draft-name]");
    const roleEl = editor.querySelector("[data-actor-draft-role]");
    const list = editor.querySelector("[data-actors-list]");
    if (!nameEl || !list) return false;
    const name = String(nameEl.value || "").trim();
    if (!name) return false;
    const existing = Array.from(list.querySelectorAll('input[name="actor_name"]')).map((el) =>
      String(el.value || "").trim().toLowerCase()
    );
    if (existing.includes(name.toLowerCase())) {
      nameEl.value = "";
      if (roleEl) roleEl.value = "";
      nameEl.focus();
      return false;
    }
    const role = String((roleEl && roleEl.value) || "").trim();
    list.insertAdjacentHTML("beforeend", actorRowHTML(name, role));
    createLucideIcons(list.lastElementChild);
    syncActorsEmpty(list);
    nameEl.value = "";
    if (roleEl) roleEl.value = "";
    nameEl.focus();
    return true;
  }

  function stringListRowHTML(editor, value) {
    const name = editor.getAttribute("data-item-name") || "item";
    const singular = editor.getAttribute("data-item-singular") || name;
    const unordered = editor.hasAttribute("data-string-list-unordered");
    const esc = (s) =>
      String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
    if (unordered) {
      return (
        '<span class="badge badge-lg gap-1 pr-0.5 max-w-full" data-string-list-row>' +
        '<input type="hidden" name="' +
        esc(name) +
        '" value="' +
        esc(value) +
        '" />' +
        '<span class="truncate max-w-48">' +
        esc(value) +
        "</span>" +
        '<button type="button" class="btn btn-ghost btn-xs btn-square h-4 min-h-4 w-4 p-0 shrink-0" data-string-list-remove aria-label="Remove ' +
        esc(singular) +
        '"><i data-lucide="x" class="size-3"></i></button>' +
        "</span>"
      );
    }
    return (
      '<div class="flex gap-1 items-center" data-string-list-row>' +
      '<input type="hidden" name="' +
      esc(name) +
      '" value="' +
      esc(value) +
      '" />' +
      '<span class="tabular-nums text-xs opacity-60 w-4 shrink-0 text-right" data-list-ord aria-hidden="true"></span>' +
      '<div class="join shrink-0">' +
      '<button type="button" class="btn btn-ghost btn-sm btn-square join-item" data-string-list-up aria-label="Move ' +
      esc(singular) +
      ' up"><i data-lucide="chevron-up" class="size-4"></i></button>' +
      '<button type="button" class="btn btn-ghost btn-sm btn-square join-item" data-string-list-down aria-label="Move ' +
      esc(singular) +
      ' down"><i data-lucide="chevron-down" class="size-4"></i></button>' +
      "</div>" +
      '<span class="grow min-w-0 text-sm truncate">' +
      esc(value) +
      "</span>" +
      '<button type="button" class="btn btn-ghost btn-sm btn-square shrink-0" data-string-list-remove aria-label="Remove ' +
      esc(singular) +
      '"><i data-lucide="minus" class="size-4"></i></button>' +
      "</div>"
    );
  }

  function syncStringListEmptyLabel(editor) {
    if (!editor) return;
    const list = editor.querySelector("[data-string-list]");
    const emptyText = editor.getAttribute("data-string-list-empty");
    if (!list || !emptyText) return;
    const hasRows = !!list.querySelector("[data-string-list-row]");
    let label = list.querySelector("[data-string-list-empty-label]");
    if (hasRows) {
      if (label) label.remove();
      return;
    }
    if (!label) {
      label = document.createElement("p");
      const unordered = editor.hasAttribute("data-string-list-unordered");
      label.className = "text-sm opacity-60" + (unordered ? " w-full" : "");
      label.setAttribute("data-string-list-empty-label", "");
      label.textContent = emptyText;
      list.prepend(label);
    }
  }

  function commitStringListDraft(editor, value) {
    if (!editor) return false;
    const name = editor.getAttribute("data-item-name") || "item";
    const input = editor.querySelector("[data-string-list-draft-value]");
    const list = editor.querySelector("[data-string-list]");
    if (!input || !list) return false;
    const item = String(value != null ? value : input.value || "").trim();
    if (!item) return false;
    if (item.toLowerCase() === "unknown") {
      input.value = "";
      input.focus();
      return false;
    }
    const existing = Array.from(list.querySelectorAll('input[name="' + name + '"]')).map((el) =>
      String(el.value || "").trim().toLowerCase()
    );
    if (existing.includes(item.toLowerCase())) {
      input.value = "";
      input.focus();
      return false;
    }
    list.insertAdjacentHTML("beforeend", stringListRowHTML(editor, item));
    createLucideIcons(list.lastElementChild);
    syncStringListEmptyLabel(editor);
    input.value = "";
    input.focus();
    return true;
  }

  document.body.addEventListener("click", (ev) => {
    const addBtn = ev.target.closest("[data-actor-add]");
    if (addBtn) {
      ev.preventDefault();
      commitActorDraft(addBtn.closest("[data-actors-editor]"));
      return;
    }
    const actorUp = ev.target.closest("[data-actor-up]");
    if (actorUp) {
      ev.preventDefault();
      moveListRow(actorUp.closest("[data-actor-row]"), -1);
      return;
    }
    const actorDown = ev.target.closest("[data-actor-down]");
    if (actorDown) {
      ev.preventDefault();
      moveListRow(actorDown.closest("[data-actor-row]"), 1);
      return;
    }
    const rm = ev.target.closest("[data-actor-remove]");
    if (rm) {
      ev.preventDefault();
      const row = rm.closest("[data-actor-row]");
      const list = row && row.closest("[data-actors-list]");
      if (row) row.remove();
      syncActorsEmpty(list);
      return;
    }
    const listAdd = ev.target.closest("[data-string-list-add]");
    if (listAdd) {
      ev.preventDefault();
      commitStringListDraft(listAdd.closest("[data-string-list-editor]"));
      return;
    }
    const listUp = ev.target.closest("[data-string-list-up]");
    if (listUp) {
      ev.preventDefault();
      moveListRow(listUp.closest("[data-string-list-row]"), -1);
      return;
    }
    const listDown = ev.target.closest("[data-string-list-down]");
    if (listDown) {
      ev.preventDefault();
      moveListRow(listDown.closest("[data-string-list-row]"), 1);
      return;
    }
    const listRm = ev.target.closest("[data-string-list-remove]");
    if (listRm) {
      ev.preventDefault();
      const row = listRm.closest("[data-string-list-row]");
      const editor = listRm.closest("[data-string-list-editor]");
      if (row) row.remove();
      syncStringListEmptyLabel(editor);
      return;
    }
  });

  document.body.addEventListener("keydown", (ev) => {
    if (ev.key !== "Enter") return;
    const actorDraft = ev.target.closest("[data-actor-draft]");
    if (actorDraft) {
      ev.preventDefault();
      commitActorDraft(actorDraft.closest("[data-actors-editor]"));
      return;
    }
    const listDraft = ev.target.closest("[data-string-list-draft]");
    if (listDraft) {
      ev.preventDefault();
      commitStringListDraft(listDraft.closest("[data-string-list-editor]"));
    }
  });

  document.body.addEventListener("submit", (ev) => {
    const form = ev.target.closest(
      'form[action="/actions/save-series-metadata"], form[action="/actions/save-video-metadata"], form[action="/actions/save-settings"], form[action="/actions/update-series"], form[action="/actions/add-series"]'
    );
    if (!form) return;
    const actors = form.querySelector("[data-actors-editor]");
    if (actors) commitActorDraft(actors);
    form.querySelectorAll("[data-string-list-editor]").forEach((ed) => commitStringListDraft(ed));
  });

  function syncConfirmSubmit(form) {
    if (!form || !form.classList.contains("js-confirm-submit")) return;
    const check = form.querySelector(".js-confirm-submit-check");
    const btn = form.querySelector('button[type="submit"]');
    if (!check || !btn) return;
    btn.disabled = !check.checked;
  }

  document.body.addEventListener("change", (ev) => {
    const t = ev.target;
    if (!(t instanceof HTMLInputElement)) return;
    if (t.classList.contains("js-confirm-submit-check")) {
      syncConfirmSubmit(t.closest("form"));
      return;
    }
    if (t.classList.contains("modal-toggle") && !t.checked) {
      const modal = t.nextElementSibling;
      if (modal && modal.classList.contains("modal")) {
        modal.querySelectorAll("form.js-confirm-submit").forEach((form) => {
          form.reset();
          syncConfirmSubmit(form);
        });
      }
    }
  });

  document.querySelectorAll("form.js-confirm-submit").forEach(syncConfirmSubmit);
})();
