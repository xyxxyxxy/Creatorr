export function refreshTasksPanel(force) {
  const panel = document.getElementById("tasks-list-live");
  if (!panel || !window.htmx) return;
  const now = Date.now();
  // Soft refreshes (SSE miss / 15s poll) recreate lane Busy indeterminate bars;
  // throttle so the CSS animation is not restarted every progress tick.
  if (!force) {
    if (now - (refreshTasksPanel._at || 0) < 2500) return;
  }
  refreshTasksPanel._at = now;
  const q = location.search || "";
  let url;
  if (location.pathname === "/tasks") {
    url = "/tasks" + q;
  } else if (location.pathname === "/browser") {
    const params = new URLSearchParams(q.startsWith("?") ? q.slice(1) : q);
    if (params.get("type") !== "tasks") return;
    params.set("type", "tasks");
    params.set("at", "browser");
    url = "/explorer/browse?" + params.toString();
  } else {
    return;
  }
  window.htmx.ajax("GET", url, { target: "#tasks-list-live", select: "#tasks-list-live", swap: "outerHTML" });
}

/**
 * Fingerprint lane tip hosts (skip cooldown / cancel / History / Pause-Resume).
 * Used to reattach unchanged nodes after #tasks-live outerHTML so daisyUI
 * data-tip does not flicker while hovered during soft refresh.
 */

function laneStableActionsFingerprint(el) {
  if (!el || !el.querySelectorAll) return "";
  return [...el.querySelectorAll("button, label.btn, a.btn, [data-tip]")].map((n) =>
    [
      n.tagName,
      n.getAttribute("data-tip") || "",
      n.getAttribute("aria-label") || "",
      n.disabled || n.classList.contains("pointer-events-none") ? "1" : "0",
      n.getAttribute("href") || "",
      n.getAttribute("for") || "",
      n.getAttribute("type") || "",
      [...n.classList].filter((c) => c.startsWith("btn") || c === "tooltip").join("."),
    ].join("~")
  ).join("|");
}

let stashedLaneStableActions = null;

// htmx:beforeSwap: remember lane tip hosts when #tasks-live is about to be replaced.
export function stashTasksLiveBeforeSwap(target) {
  stashedLaneStableActions = target && target.id === "tasks-list-live" ? stashLaneStableActions(target) : null;
}

// htmx:afterSwap: reattach unchanged tip hosts into the new #tasks-list-live.
export function restoreTasksLiveAfterSwap(root) {
  if (!root || root.id !== "tasks-list-live" || !stashedLaneStableActions) return;
  restoreLaneStableActions(root, stashedLaneStableActions);
  stashedLaneStableActions = null;
}

function stashLaneStableActions(panel) {
  const map = new Map();
  if (!panel || !panel.querySelectorAll) return map;
  panel.querySelectorAll("[data-lane-stable-actions]").forEach((el) => {
    const key = el.getAttribute("data-lane-stable-actions");
    if (!key) return;
    map.set(key, { el, fp: laneStableActionsFingerprint(el) });
  });
  return map;
}

function restoreLaneStableActions(panel, stashed) {
  if (!panel || !stashed || stashed.size === 0) return;
  panel.querySelectorAll("[data-lane-stable-actions]").forEach((el) => {
    const key = el.getAttribute("data-lane-stable-actions");
    const prev = stashed.get(key);
    if (!prev) return;
    if (prev.fp === laneStableActionsFingerprint(el)) {
      el.replaceWith(prev.el);
    }
  });
}

function clearCooldownBusyTip(wrap) {
  wrap.classList.remove("tooltip", "tooltip-top");
  wrap.removeAttribute("data-tip");
}

/** Lane card that owns a cooldown/status wrap (or a task row inside it). */
export function lanePanelFor(el) {
  return el && el.closest ? el.closest("section.list-panel") : null;
}

function inferLaneActivity(wrap) {
  const panel = lanePanelFor(wrap);
  let running = 0;
  let pending = 0;
  let max = 1;
  let empty = true;
  if (panel) {
    panel.querySelectorAll("[data-task-row-status]").forEach((row) => {
      empty = false;
      const st = row.getAttribute("data-task-row-status");
      if (st === "running") running++;
      else if (st === "pending") pending++;
    });
    if (panel.querySelector("[data-scheduled-task]")) empty = false;
    const parallel = panel.querySelector('[aria-label^="Parallel "]');
    const m = parallel && parallel.getAttribute("aria-label").match(/Parallel\s+(\d+)/);
    if (m) {
      const n = parseInt(m[1], 10);
      if (Number.isFinite(n) && n > 0) max = n;
    }
  }
  return { running, pending, max, empty };
}

/**
 * Paint Busy/Active/Idle from task rows in the lane.
 * Used when cooldown ends client-side and when SSE patches pending→running
 * without a full #tasks-live swap (which otherwise leaves a Idle flash).
 */

export function applyInferredLaneStatus(wrap) {
  if (!wrap) return;
  // Soft-pause: keep Paused label; bar is indeterminate warning while
  // tasks are still running, full warning when the lane is quiet.
  if (wrap.hasAttribute("data-paused")) {
    showCooldownPaused(wrap);
    return;
  }
  const endsAttr = wrap.getAttribute("data-ends-at");
  if (endsAttr) {
    const ends = Date.parse(endsAttr);
    if (Number.isFinite(ends) && ends > Date.now()) return;
  }
  wrap.removeAttribute("data-ends-at");
  wrap.removeAttribute("data-total-sec");
  const { running, pending, max } = inferLaneActivity(wrap);
  if (running > 0 && running >= max) {
    showCooldownBusy(wrap);
    return;
  }
  if (running > 0) {
    showCooldownActive(wrap);
    return;
  }
  if (pending > 0) {
    // Cooldown just ended / claim about to land: avoid Idle→Busy flash.
    // max_parallel=1 fills the only slot on the next claim.
    if (max <= 1) {
      showCooldownBusy(wrap);
      return;
    }
    const label = wrap.querySelector("[data-cd-label]");
    if (label && label.textContent === "Cooldown") {
      const bar = wrap.querySelector("[data-cd-bar]");
      if (bar) {
        bar.value = 0;
        if (!bar.max || Number(bar.max) < 1) bar.max = 1;
      }
      wrap.setAttribute("aria-label", "Waiting before the next task on this lane");
      return;
    }
    showCooldownActive(wrap);
    return;
  }
  wrap.removeAttribute("data-slots-full");
  wrap.removeAttribute("data-lane-active");
  showCooldownReady(wrap);
}

function showCooldownPaused(wrap) {
  wrap.removeAttribute("data-ends-at");
  wrap.removeAttribute("data-total-sec");
  wrap.removeAttribute("data-slots-full");
  wrap.removeAttribute("data-lane-active");
  clearCooldownBusyTip(wrap);
  const { running, empty } = inferLaneActivity(wrap);
  const busy = running > 0;
  const quietLabel = empty ? "Paused, no tasks" : "Paused";
  const tipText = "Some active tasks remain";
  wrap.setAttribute(
    "aria-label",
    busy ? "Paused; tasks still running" : quietLabel
  );
  const label = wrap.querySelector("[data-cd-label]");
  const bar = wrap.querySelector("[data-cd-bar]");
  const labelText = label ? label.textContent : "";
  const isPausedLabel =
    labelText === "Paused" || labelText === "Paused, no tasks";
  if (label && bar && isPausedLabel) {
    const warning = bar.classList.contains("progress-warning");
    const indeterminate = !bar.hasAttribute("value");
    const full =
      bar.hasAttribute("value") && Number(bar.value) === 100;
    if (busy && warning && indeterminate) {
      const tip = label.closest(".tooltip");
      if (tip) {
        tip.classList.add("tooltip", "tooltip-top");
        tip.setAttribute("data-tip", tipText);
      }
      return;
    }
    if (!busy && warning && full && labelText === quietLabel) return;
  }
  if (busy) {
    wrap.innerHTML =
      '<span class="tooltip tooltip-top" data-tip="' +
      tipText +
      '">' +
      '<span data-cd-label class="shrink-0 leading-none text-warning">Paused</span></span>' +
      '<progress data-cd-bar class="progress progress-warning w-24 sm:w-32 h-2 shrink-0" max="100" aria-hidden="true"></progress>';
    return;
  }
  wrap.innerHTML =
    '<span data-cd-label class="shrink-0 leading-none text-warning">' +
    quietLabel +
    "</span>" +
    '<progress data-cd-bar class="progress progress-warning w-24 sm:w-32 h-2 shrink-0" value="100" max="100" aria-hidden="true"></progress>';
}

function showCooldownBusy(wrap) {
  wrap.removeAttribute("data-ends-at");
  wrap.removeAttribute("data-total-sec");
  wrap.removeAttribute("data-lane-active");
  wrap.setAttribute("data-slots-full", "");
  wrap.classList.remove("tooltip", "tooltip-top");
  wrap.removeAttribute("data-tip");
  wrap.setAttribute("aria-label", "Maximum of parallel tasks reached");
  // Keep existing indeterminate <progress> - rewriting innerHTML restarts the slide.
  const label = wrap.querySelector("[data-cd-label]");
  const bar = wrap.querySelector("[data-cd-bar]");
  if (label && bar && label.textContent === "Busy" && !bar.hasAttribute("value")) {
    const tip = label.closest(".tooltip");
    if (tip) {
      tip.classList.add("tooltip", "tooltip-top");
      tip.setAttribute("data-tip", "Maximum of parallel tasks reached");
    }
    return;
  }
  wrap.innerHTML =
    '<span class="tooltip tooltip-top" data-tip="Maximum of parallel tasks reached">' +
    '<span data-cd-label class="shrink-0 leading-none">Busy</span></span>' +
    '<progress data-cd-bar class="progress progress-primary w-24 sm:w-32 h-2 shrink-0" max="100" aria-hidden="true"></progress>';
}

function showCooldownActive(wrap) {
  wrap.removeAttribute("data-ends-at");
  wrap.removeAttribute("data-total-sec");
  wrap.removeAttribute("data-slots-full");
  wrap.setAttribute("data-lane-active", "");
  wrap.classList.remove("tooltip", "tooltip-top");
  wrap.removeAttribute("data-tip");
  wrap.setAttribute("aria-label", "Running with free parallel slots");
  const label = wrap.querySelector("[data-cd-label]");
  const bar = wrap.querySelector("[data-cd-bar]");
  if (label && bar && label.textContent === "Active" && !bar.hasAttribute("value")) {
    const tip = label.closest(".tooltip");
    if (tip) {
      tip.classList.add("tooltip", "tooltip-top");
      tip.setAttribute("data-tip", "Running with free parallel slots");
    }
    return;
  }
  wrap.innerHTML =
    '<span class="tooltip tooltip-top" data-tip="Running with free parallel slots">' +
    '<span data-cd-label class="shrink-0 leading-none">Active</span></span>' +
    '<progress data-cd-bar class="progress progress-primary w-24 sm:w-32 h-2 shrink-0" max="100" aria-hidden="true"></progress>';
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
  if (wrap.hasAttribute("data-lane-active")) {
    showCooldownActive(wrap);
    return;
  }
  wrap.removeAttribute("data-ends-at");
  wrap.removeAttribute("data-total-sec");
  wrap.removeAttribute("data-slots-full");
  wrap.removeAttribute("data-lane-active");
  clearCooldownBusyTip(wrap);
  const { empty } = inferLaneActivity(wrap);
  const idleLabel = empty ? "Idle, no tasks" : "Idle";
  wrap.setAttribute("aria-label", idleLabel);
  const label = wrap.querySelector("[data-cd-label]");
  const bar = wrap.querySelector("[data-cd-bar]");
  if (
    label &&
    bar &&
    label.textContent === idleLabel &&
    bar.hasAttribute("value") &&
    Number(bar.value) === 0
  ) {
    return;
  }
  wrap.innerHTML =
    '<span data-cd-label class="shrink-0 leading-none text-base-content/50">' +
    idleLabel +
    "</span>" +
    '<progress data-cd-bar class="progress progress-primary w-24 sm:w-32 h-2 shrink-0" value="0" max="100" aria-hidden="true"></progress>';
}

/** Short span like "3min 2 sec" (at most two units). Matches Go formatDurationCompact. */
function formatDurationCompact(totalSec) {
  let sec = Math.max(0, Math.floor(Number(totalSec) || 0));
  if (sec < 1) return "1 sec";
  const days = Math.floor(sec / 86400);
  sec -= days * 86400;
  const hours = Math.floor(sec / 3600);
  sec -= hours * 3600;
  const minutes = Math.floor(sec / 60);
  sec -= minutes * 60;
  const parts = [];
  const add = (n, u) => {
    if (n > 0) parts.push(n + u);
  };
  add(days, "d");
  add(hours, "h");
  add(minutes, "min");
  if (days === 0) add(sec, " sec");
  if (!parts.length) return "1 sec";
  if (parts.length > 2) parts.length = 2;
  return parts.join(" ");
}

/** Short span with only the largest unit ("3min", "2h", "5 sec"). Matches Go formatDurationLargest. */
function formatDurationLargest(totalSec) {
  let sec = Math.max(0, Math.floor(Number(totalSec) || 0));
  if (sec < 1) return "1 sec";
  const days = Math.floor(sec / 86400);
  if (days > 0) return days + "d";
  const hours = Math.floor(sec / 3600);
  if (hours > 0) return hours + "h";
  const minutes = Math.floor(sec / 60);
  if (minutes > 0) return minutes + "min";
  return Math.max(1, sec) + " sec";
}

function cooldownWaitTip(remSec) {
  const n = Math.max(1, Math.ceil(Number(remSec) || 0));
  return "Waiting " + formatDurationCompact(n);
}

function scheduledTaskWaitTip(remSec) {
  const n = Math.max(1, Math.ceil(Number(remSec) || 0));
  return "in " + formatDurationLargest(n);
}

function tickScheduledTasks() {
  let anyActive = false;
  document.querySelectorAll("[data-scheduled-task]").forEach((row) => {
    const endsAttr = row.getAttribute("data-ends-at");
    if (!endsAttr) return;
    const ends = Date.parse(endsAttr);
    if (!Number.isFinite(ends)) return;
    const remMs = ends - Date.now();
    if (remMs <= 0) {
      refreshTasksPanel(true);
      return;
    }
    anyActive = true;
    const remSec = Math.ceil(remMs / 1000);
    const waitTip = scheduledTaskWaitTip(remSec);
    const schedule = row.getAttribute("data-schedule") || "";
    const labelText = schedule ? schedule + " next " + waitTip : waitTip;
    const absTip = row.getAttribute("data-abs-tip") || "";
    const tipText = absTip ? labelText + " · " + absTip : labelText;
    const label = row.querySelector("[data-scheduled-label]");
    if (label) {
      if (label.textContent !== labelText) label.textContent = labelText;
      label.setAttribute("data-tip", tipText);
    }
    row.setAttribute("aria-label", labelText);
  });
  return anyActive;
}

function tickDomainCooldowns() {
  let anyActive = false;
  document.querySelectorAll("[data-domain-cooldown]").forEach((wrap) => {
    if (wrap.hasAttribute("data-paused")) return;
    // Active / Busy have no ends-at - leave the indeterminate bar alone.
    if (
      (wrap.hasAttribute("data-slots-full") || wrap.hasAttribute("data-lane-active")) &&
      !wrap.getAttribute("data-ends-at")
    ) {
      return;
    }
    const endsAttr = wrap.getAttribute("data-ends-at");
    if (!endsAttr) return;
    const ends = Date.parse(endsAttr);
    if (!Number.isFinite(ends)) {
      applyInferredLaneStatus(wrap);
      refreshTasksPanel(true);
      return;
    }
    const remMs = ends - Date.now();
    if (remMs <= 0) {
      // Do not paint Idle here: claim often lands next and Busy would flash Idle first.
      applyInferredLaneStatus(wrap);
      refreshTasksPanel(true);
      return;
    }
    anyActive = true;
    const remSec = Math.ceil(remMs / 1000);
    const remFrac = remMs / 1000;
    let total = parseInt(wrap.getAttribute("data-total-sec") || "0", 10);
    if (!Number.isFinite(total) || total < 1) total = remSec;
    if (remSec > total) total = remSec;
    clearCooldownBusyTip(wrap);
    let bar = wrap.querySelector("[data-cd-bar]");
    let label = wrap.querySelector("[data-cd-label]");
    const tipText = cooldownWaitTip(remSec);
    if (!bar || !label) {
      wrap.innerHTML =
        '<span class="tooltip tooltip-top" data-tip="' +
        tipText +
        '"><span data-cd-label class="shrink-0 leading-none">Cooldown</span></span>' +
        '<progress data-cd-bar class="progress progress-primary w-24 sm:w-32 h-2 shrink-0" value="' +
        remFrac +
        '" max="' +
        total +
        '" aria-hidden="true"></progress>';
      bar = wrap.querySelector("[data-cd-bar]");
      label = wrap.querySelector("[data-cd-label]");
    }
    if (label) {
      if (label.textContent !== "Cooldown") label.textContent = "Cooldown";
      label.classList.remove("text-base-content/50", "text-warning");
      let tipHost = label.closest(".tooltip");
      if (!tipHost) {
        tipHost = document.createElement("span");
        tipHost.className = "tooltip tooltip-top";
        label.parentNode.insertBefore(tipHost, label);
        tipHost.appendChild(label);
      }
      tipHost.classList.add("tooltip", "tooltip-top");
      tipHost.setAttribute("data-tip", tipText);
    }
    if (bar) {
      const cls = "progress progress-primary w-24 sm:w-32 h-2 shrink-0";
      if (bar.className !== cls) bar.className = cls;
      if (Number(bar.max) !== total) bar.max = total;
      if (Number(bar.value) !== remFrac) bar.value = remFrac;
    }
    wrap.setAttribute("aria-label", tipText);
  });
  return anyActive;
}

export function bootLanes() {
  (function runDomainCooldownLoop() {
    function frame() {
      if (tickDomainCooldowns() || tickScheduledTasks()) {
        requestAnimationFrame(frame);
      } else {
        // Idle: poll slowly so HTMX lane swaps still pick up a new cooldown.
        setTimeout(() => requestAnimationFrame(frame), 250);
      }
    }
    requestAnimationFrame(frame);
  })();
}
