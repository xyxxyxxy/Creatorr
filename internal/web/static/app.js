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

  async function refreshNotifyDropdown() {
    const list = document.getElementById("notify-dropdown-list");
    const empty = document.getElementById("notify-dropdown-empty");
    if (!list || !empty) return;
    try {
      const res = await fetch("/api/notifications?unread_only=true&limit=10");
      if (!res.ok) return;
      const items = await res.json();
      list.replaceChildren();
      if (!Array.isArray(items) || items.length === 0) {
        list.classList.add("hidden");
        empty.classList.remove("hidden");
        return;
      }
      empty.classList.add("hidden");
      list.classList.remove("hidden");
      items.forEach((n) => {
        const li = document.createElement("li");
        const a = document.createElement("a");
        a.href = "/notification/" + n.id;
        a.className = "flex flex-col items-start gap-0.5 whitespace-normal";
        const title = document.createElement("span");
        title.className = "font-medium text-sm";
        title.textContent = n.title || n.event || "Notification";
        const meta = document.createElement("span");
        meta.className = "text-xs opacity-60";
        meta.textContent = n.event || "";
        a.appendChild(title);
        a.appendChild(meta);
        li.appendChild(a);
        list.appendChild(li);
      });
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

  function setCountdownValue(el, n) {
    if (!el) return;
    const v = Math.max(0, Math.min(999, Math.floor(n)));
    el.style.setProperty("--value", String(v));
    el.textContent = el.hasAttribute("data-cd-sec")
      ? String(v).padStart(2, "0")
      : String(v);
    el.setAttribute("aria-label", String(v));
  }

  function tickDomainCooldowns() {
    document.querySelectorAll("[data-domain-cooldown]").forEach((wrap) => {
      const ends = Date.parse(wrap.getAttribute("data-ends-at") || "");
      if (!Number.isFinite(ends)) {
        wrap.remove();
        return;
      }
      let rem = Math.ceil((ends - Date.now()) / 1000);
      if (rem <= 0) {
        wrap.remove();
        return;
      }
      if (rem > 999 * 60 + 59) rem = 999 * 60 + 59;
      const min = Math.floor(rem / 60);
      setCountdownValue(wrap.querySelector("[data-cd-min]"), min);
      setCountdownValue(wrap.querySelector("[data-cd-sec]"), rem % 60);
      const minWrap = wrap.querySelector("[data-cd-min-wrap]");
      if (minWrap) minWrap.classList.toggle("hidden", min === 0);
    });
  }

  setInterval(tickDomainCooldowns, 1000);

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
    const cell = root.querySelector("[data-task-status]");
    if (!cell) return;
    cell.replaceChildren(statusBadgeEl(status));
    createLucideIcons(cell);
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
    if (typeof data.status === "string" && data.status) {
      patchStatusCell(row, data.status);
      if (data.status !== "pending") {
        const bump = row.querySelector('form[action="/actions/bump-task"]');
        if (bump) bump.remove();
      }
    }
    const msgEl = row.querySelector("[data-task-message]");
    if (msgEl && typeof data.message === "string") {
      msgEl.textContent = data.message || "-";
    }
    if (data.progress != null && Number.isFinite(Number(data.progress))) {
      const raw = Number(data.progress);
      const pct = Math.max(0, Math.min(100, Math.round(raw * 100)));
      let bar = row.querySelector("progress[data-task-progress]");
      const showBar = raw > 0 && raw < 1;
      if (!showBar) {
        if (bar) bar.remove();
      } else {
        if (!bar) {
          const cell = row.querySelector("[data-task-status-cell]");
          if (cell) {
            bar = document.createElement("progress");
            bar.setAttribute("data-task-progress", "");
            bar.className = "progress progress-primary w-full max-w-xs mt-1";
            bar.max = 100;
            cell.appendChild(bar);
          }
        }
        if (bar) bar.value = pct;
      }
    } else if (data.progress === null) {
      const bar = row.querySelector("progress[data-task-progress]");
      if (bar) bar.remove();
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
    if (msgEl && typeof data.message === "string") {
      msgEl.textContent = data.message || "-";
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
      kind === "prefetch_add_series"
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
      // In-place patch on Tasks page - full swap recreates Cancel/To top buttons every tick.
      if (!patchTaskRow(ev)) refreshTasksPanel();
      patchTaskDetail(ev);
      refreshTaskIndicators();
      refreshVideoHistoryIfMatch(ev);
      refreshTaskVideoHistoryIfMatch(ev);
    } else if (ev.type === "task.done" || ev.type === "task.failed") {
      refreshTasksPanel();
      refreshTaskIndicators();
      refreshHistoryPanel();
      reloadTaskDetailIfMatch(ev);
      reloadVideoDetailIfMatch(ev);
    }
    maybeRefreshSeriesVideos(ev);
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
    panel.classList.toggle("hidden", !enabled);
    panel.querySelectorAll("input, select, textarea").forEach((el) => {
      if (el.dataset.defaultDisabled == null) {
        el.dataset.defaultDisabled = el.disabled ? "1" : "0";
      }
      el.disabled = !enabled || el.dataset.defaultDisabled === "1";
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
    const help = form.querySelector(".js-add-series-url-help");
    const dup = form.querySelector(".js-add-series-url-dup");
    const invalidEl = form.querySelector(".js-add-series-url-invalid");
    if (!urlEl) return;
    const has = String(urlEl.value || "").trim() !== "";
    const invalid = addSeriesURLInvalid(form);
    const clash = !invalid && addSeriesURLClash(form);
    const blocked = form.querySelector("[data-add-series-submit]")?.getAttribute("data-blocked") === "1";
    if (cont) cont.disabled = blocked || !has || clash || invalid || cont.dataset.busy === "1";
    if (dup) dup.classList.toggle("hidden", !clash);
    if (invalidEl) invalidEl.classList.toggle("hidden", !invalid);
    if (help) help.classList.toggle("hidden", clash || invalid);
    urlEl.classList.toggle("input-error", clash || invalid);
  }

  function setAddSeriesFetchErr(form, msg) {
    const errEl = form.querySelector(".js-add-series-fetch-err");
    if (!errEl) return;
    const span = errEl.querySelector("span") || errEl;
    if (msg) {
      span.textContent = msg;
      errEl.classList.remove("hidden");
    } else {
      span.textContent = "";
      errEl.classList.add("hidden");
    }
  }

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
    // Keep alert under steps; only show on source step when it has text.
    if (errEl) {
      const hasMsg = !!(errEl.querySelector("span") || errEl).textContent;
      errEl.classList.toggle("hidden", !(mode === "url" && step === "source" && hasMsg));
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
      setPanelControls(series, showSeries);
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
  }

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
      if (form.classList.contains("js-add-series-form")) setAddSeriesFetchErr(form, "");
      form.querySelectorAll("[data-user-edited]").forEach((el) => {
        el.dataset.userEdited = "";
        el.disabled = false;
        el.removeAttribute("aria-busy");
      });
      form.querySelectorAll("input.preset-fill-input").forEach(syncPresetChips);
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

  function syncRateLimitJoin(join) {
    if (!join) return;
    const unit = join.querySelector("[data-rate-unit]");
    const num = join.querySelector("[data-rate-value]");
    if (!unit || !num) return;
    const off = unit.value === "off";
    const inherit = unit.value === "";
    num.readOnly = off;
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
    if (!(el instanceof HTMLInputElement)) return;
    if (el.id === "add-series-url") {
      const form = el.closest("form.js-add-series-form");
      if (form) syncAddSeriesSourceNav(form);
    }
    if (el.classList.contains("preset-fill-input")) syncPresetChips(el);
    const rateJoin = el.closest("[data-rate-limit-join]");
    if (rateJoin && el.hasAttribute("data-rate-value")) {
      rateJoin.classList.remove("opacity-70");
    }
  });

  document.body.addEventListener("change", (ev) => {
    const el = ev.target;
    if (!(el instanceof HTMLSelectElement) || !el.hasAttribute("data-rate-unit")) return;
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
    const help = form.querySelector(".js-source-url-help");
    const dup = form.querySelector(".js-source-url-dup");
    const invalidEl = form.querySelector(".js-source-url-invalid");
    if (!input || !submit) return;
    const raw = String(input.value || "").trim();
    const cur = normalizeSourceURLClient(form.getAttribute("data-current-url") || "");
    const typed = normalizeSourceURLClient(input.value);
    const invalid = raw !== "" && !isValidSourceURLClient(raw);
    const existing = existingSourceURLs();
    const clash = !invalid && typed !== "" && existing.some((u) => u === typed && u !== cur);
    submit.disabled = clash || invalid;
    if (dup) dup.classList.toggle("hidden", !clash);
    if (invalidEl) invalidEl.classList.toggle("hidden", !invalid);
    if (help) help.classList.toggle("hidden", clash || invalid);
    input.classList.toggle("input-error", clash || invalid);
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
