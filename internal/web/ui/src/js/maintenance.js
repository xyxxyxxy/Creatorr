function onMaintenancePage() {
  return location.pathname === "/settings/maintenance";
}

const maintenanceTaskKinds = new Set([
  "rename_episodes",
  "regenerate_nfo",
  "reset_metadata_from_info",
  "integrity_check",
  "sync_files",
]);

/** Persistent across HTMX refresh of #maintenance-live. Series-only scope. */
let maintenanceScope = {
  seriesIds: [],
  seriesTitles: [],
};

/** Selected action values; restored after HTMX busy refresh. Cleared on Run. */
let maintenanceSelectedActions = new Set();

/** Series catalog for scope picker (GET /api/import/picker). */
let maintenanceSeriesCatalog = [];

let maintenanceSeriesCatalogReady = false;

let maintenanceSeriesCatalogPromise = null;

const maintenanceActionLabels = {
  reset_metadata_from_info: "Reset metadata from info.json",
  apply_episode_naming: "Apply episode format",
  regenerate_nfos: "Regenerate all NFO files",
  integrity_check: "Integrity check",
  sync_files: "File sync",
  refresh_sidecars: "Refresh sidecars",
};

function escapeMaintenanceHtml(s) {
  return String(s || "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function clearMaintenanceScope() {
  maintenanceScope = { seriesIds: [], seriesTitles: [] };
}

function ensureMaintenanceSeriesCatalog() {
  if (maintenanceSeriesCatalogReady) return Promise.resolve();
  if (maintenanceSeriesCatalogPromise) return maintenanceSeriesCatalogPromise;
  maintenanceSeriesCatalogPromise = fetch("/api/import/picker", {
    headers: { Accept: "application/json" },
    credentials: "same-origin",
  })
    .then((res) => {
      if (!res.ok) throw new Error("picker " + res.status);
      return res.json();
    })
    .then((data) => {
      maintenanceSeriesCatalog = Array.isArray(data.series) ? data.series : [];
      maintenanceSeriesCatalogReady = true;
    })
    .catch(() => {
      maintenanceSeriesCatalog = [];
      maintenanceSeriesCatalogReady = false;
    })
    .finally(() => {
      maintenanceSeriesCatalogPromise = null;
    });
  return maintenanceSeriesCatalogPromise;
}

function maintenanceSeriesById(id) {
  const n = Number(id);
  return maintenanceSeriesCatalog.find((s) => Number(s.id) === n) || null;
}

function maintenanceSeriesPosterHTML(s) {
  const fallback =
    '<div class="bg-base-200 size-8 rounded-full flex items-center justify-center shrink-0" aria-hidden="true"><i data-lucide="tv" class="size-4 opacity-40"></i></div>';
  if (!s || !s.poster_url) return fallback;
  // Picker always returns poster_url; missing art 404s - swap to TV icon like Import.
  return (
    '<img class="size-8 rounded-full object-cover shrink-0" src="' +
    escapeMaintenanceHtml(s.poster_url) +
    '" alt="" width="32" height="32" loading="lazy" onerror="this.onerror=null;this.classList.add(\'hidden\');this.nextElementSibling.classList.remove(\'hidden\')" />' +
    '<div class="hidden bg-base-200 size-8 rounded-full flex items-center justify-center shrink-0" aria-hidden="true"><i data-lucide="tv" class="size-4 opacity-40"></i></div>'
  );
}

/** Same row as scope chips; omit remove for confirm modal. */
function maintenanceSeriesScopeRowHTML(id, titleHint, withRemove) {
  const s = id != null ? maintenanceSeriesById(id) : null;
  const title =
    (s && s.title) || titleHint || (id != null ? "Series #" + id : "All series");
  let html =
    '<li class="flex items-center gap-2 min-w-0 rounded-box border border-base-300 bg-base-100 px-2 py-1.5">' +
    maintenanceSeriesPosterHTML(s) +
    '<span class="truncate grow min-w-0 text-sm font-medium">' +
    escapeMaintenanceHtml(title) +
    "</span>";
  if (withRemove && id != null) {
    html +=
      '<button type="button" class="btn btn-soft btn-error btn-square btn-sm shrink-0" data-maintenance-remove-series="' +
      escapeMaintenanceHtml(String(id)) +
      '" aria-label="Remove">' +
      '<i data-lucide="x" class="size-4" aria-hidden="true"></i>' +
      "</button>";
  }
  return html + "</li>";
}

function lucideRefreshMaintenance(root) {
  if (!root || !window.lucide || typeof window.lucide.createIcons !== "function") return;
  window.lucide.createIcons({ root: root, attrs: { "stroke-width": 1.75, "aria-hidden": "true" } });
}

function renderMaintenanceScopeChips() {
  const host = document.getElementById("maintenance-scope-chips");
  if (!host) return;
  if (!maintenanceScope.seriesIds.length) {
    host.innerHTML = "";
    return;
  }
  host.innerHTML = maintenanceScope.seriesIds
    .map((id, i) =>
      maintenanceSeriesScopeRowHTML(id, maintenanceScope.seriesTitles[i], true)
    )
    .join("");
  lucideRefreshMaintenance(host);
}

function fillMaintenanceSeriesPickList(q) {
  const ul = document.querySelector(".js-maintenance-series-list");
  if (!ul) return;
  const needle = String(q || "").trim().toLowerCase();
  const selected = new Set(maintenanceScope.seriesIds.map(Number));
  const rows = maintenanceSeriesCatalog.filter((s) => {
    if (selected.has(Number(s.id))) return false;
    if (!needle) return true;
    return String(s.title || "").toLowerCase().includes(needle);
  });
  if (!maintenanceSeriesCatalogReady) {
    ul.innerHTML = '<li class="list-row opacity-60 text-sm">Loading series…</li>';
    return;
  }
  if (!rows.length) {
    ul.innerHTML =
      '<li class="list-row opacity-60 text-sm">' +
      (needle ? "No matches" : "No series left to add") +
      "</li>";
    return;
  }
  ul.innerHTML = rows
    .map((s) => {
      return (
        '<li class="list-row cursor-pointer" data-maintenance-pick-series="' +
        escapeMaintenanceHtml(String(s.id)) +
        '" role="option">' +
        '<div class="relative shrink-0">' +
        maintenanceSeriesPosterHTML(s) +
        "</div>" +
        '<div class="list-col-grow min-w-0 font-medium truncate">' +
        escapeMaintenanceHtml(s.title || "") +
        "</div></li>"
      );
    })
    .join("");
  lucideRefreshMaintenance(ul);
}

function addMaintenanceSeries(id) {
  const n = Number(id);
  if (!(n > 0) || maintenanceScope.seriesIds.includes(n)) return;
  const s = maintenanceSeriesById(n);
  maintenanceScope.seriesIds.push(n);
  maintenanceScope.seriesTitles.push((s && s.title) || "Series #" + n);
  refreshMaintenanceScopeUI();
}

function removeMaintenanceSeries(id) {
  const n = Number(id);
  const i = maintenanceScope.seriesIds.indexOf(n);
  if (i < 0) return;
  maintenanceScope.seriesIds.splice(i, 1);
  maintenanceScope.seriesTitles.splice(i, 1);
  refreshMaintenanceScopeUI();
}

function syncMaintenanceScopeFields() {
  const host = document.getElementById("maintenance-scope-fields");
  if (!host) return;
  host.innerHTML = "";
  maintenanceScope.seriesIds.forEach((id) => {
    const inp = document.createElement("input");
    inp.type = "hidden";
    inp.name = "series_ids";
    inp.value = String(id);
    host.appendChild(inp);
  });
  renderMaintenanceScopeChips();
}

function readMaintenanceActionChecks() {
  maintenanceSelectedActions = new Set();
  document.querySelectorAll(".js-maintenance-action:checked:not(:disabled)").forEach((el) => {
    if (el.value) maintenanceSelectedActions.add(el.value);
  });
}

function applyMaintenanceActionChecks() {
  document.querySelectorAll(".js-maintenance-action").forEach((el) => {
    if (el.disabled) {
      el.checked = false;
      return;
    }
    el.checked = maintenanceSelectedActions.has(el.value);
  });
  updateMaintenanceRunButton();
}

function updateMaintenanceRunButton() {
  const btn = document.getElementById("maintenance-run-submit");
  const n = document.querySelectorAll(".js-maintenance-action:checked:not(:disabled)").length;
  if (btn) btn.disabled = n === 0;
  updateMaintenancePreviewButton();
}

function updateMaintenancePreviewButton() {
  const btn = document.getElementById("maintenance-preview-rename");
  if (!btn) return;
  const applyEl = document.querySelector(
    '.js-maintenance-action[value="apply_episode_naming"]'
  );
  const applyOn =
    applyEl && applyEl.checked && !applyEl.disabled;
  // Join tip stays on .btn.join-item; use aria-disabled (not :disabled) so seams + tip work.
  if (applyOn) {
    btn.removeAttribute("aria-disabled");
    btn.classList.remove("cursor-not-allowed", "tooltip", "tooltip-top");
    btn.removeAttribute("data-tip");
  } else {
    btn.setAttribute("aria-disabled", "true");
    btn.classList.add("cursor-not-allowed", "tooltip", "tooltip-top");
    btn.setAttribute("data-tip", "Select 'Apply episode format' to preview renames");
  }
}

function openMaintenanceRenamePreview() {
  const toggle = document.getElementById("modal-maintenance-rename-preview");
  const body = document.getElementById("maintenance-rename-preview-body");
  const form = document.getElementById("maintenance-run-form");
  if (!toggle || !body || !form) return;
  syncMaintenanceScopeFields();
  body.innerHTML = '<p class="text-sm opacity-60">Loading…</p>';
  toggle.checked = true;
  const params = new URLSearchParams();
  const fd = new FormData(form);
  for (const [k, v] of fd.entries()) {
    if (k === "actions") continue;
    params.append(k, v);
  }
  fetch("/actions/preview-apply-episode-naming", {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      "HX-Request": "true",
    },
    body: params.toString(),
    credentials: "same-origin",
  })
    .then((res) => res.text())
    .then((html) => {
      body.innerHTML = html;
      if (window.lucide && typeof window.lucide.createIcons === "function") {
        window.lucide.createIcons();
      }
    })
    .catch(() => {
      body.innerHTML = '<p class="text-sm text-error">Preview failed.</p>';
    });
}

function refreshMaintenanceScopeUI() {
  syncMaintenanceScopeFields();
  applyMaintenanceActionChecks();
}

export function wireMaintenanceScope() {
  if (!onMaintenancePage()) return;
  ensureMaintenanceSeriesCatalog().then(() => refreshMaintenanceScopeUI());
}

function selectedMaintenanceActionLabels() {
  readMaintenanceActionChecks();
  const order = [
    "reset_metadata_from_info",
    "apply_episode_naming",
    "regenerate_nfos",
    "sync_files",
    "integrity_check",
    "refresh_sidecars",
  ];
  return order
    .filter((k) => maintenanceSelectedActions.has(k))
    .map((k) => maintenanceActionLabels[k] || k);
}

function openMaintenanceConfirm() {
  const toggle = document.getElementById("modal-maintenance-confirm");
  const titleEl = document.getElementById("maintenance-confirm-title");
  const leadEl = document.getElementById("maintenance-confirm-lead");
  const listEl = document.getElementById("maintenance-confirm-list");
  const actionsEl = document.getElementById("maintenance-confirm-actions");
  const affectedEl = document.getElementById("maintenance-confirm-affected");
  const externalBox = document.getElementById("maintenance-confirm-external");
  const externalText = document.getElementById("maintenance-confirm-external-text");
  const integrityBox = document.getElementById("maintenance-confirm-integrity");
  const resetMetaBox = document.getElementById("maintenance-confirm-reset-meta");
  const form = document.getElementById("maintenance-run-form");
  const actionNames = selectedMaintenanceActionLabels();
  if (
    !toggle ||
    !titleEl ||
    !leadEl ||
    !listEl ||
    !actionsEl ||
    !affectedEl ||
    !externalBox ||
    !externalText ||
    !form ||
    actionNames.length === 0
  ) {
    return false;
  }
  titleEl.textContent = "Confirm run";
  actionsEl.innerHTML = actionNames
    .map(
      (name) =>
        '<li class="list-row py-2 px-3"><span class="list-col-grow min-w-0 truncate">' +
        escapeMaintenanceHtml(name) +
        "</span></li>"
    )
    .join("");
  const nS = maintenanceScope.seriesIds.length;
  let lead = "";
  if (nS > 0) {
    lead = nS === 1 ? "1 series:" : nS + " series:";
    listEl.className =
      "flex flex-col gap-1.5 w-full min-w-0 list-none p-0 m-0 max-h-60 overflow-y-auto mb-2";
    listEl.innerHTML = maintenanceScope.seriesIds
      .map((id, i) =>
        maintenanceSeriesScopeRowHTML(id, maintenanceScope.seriesTitles[i], false)
      )
      .join("");
  } else {
    lead = "Whole library:";
    listEl.className =
      "flex flex-col gap-1.5 w-full min-w-0 list-none p-0 m-0 max-h-60 overflow-y-auto mb-2";
    listEl.innerHTML = maintenanceSeriesScopeRowHTML(null, "All series", false);
  }
  leadEl.textContent = lead;
  lucideRefreshMaintenance(listEl);
  affectedEl.textContent = "Counting packed videos…";
  externalBox.classList.add("hidden");
  externalText.textContent = "";
  if (integrityBox) {
    if (maintenanceSelectedActions.has("integrity_check")) {
      integrityBox.classList.remove("hidden");
    } else {
      integrityBox.classList.add("hidden");
    }
  }
  if (resetMetaBox) {
    if (maintenanceSelectedActions.has("reset_metadata_from_info")) {
      resetMetaBox.classList.remove("hidden");
    } else {
      resetMetaBox.classList.add("hidden");
    }
  }
  toggle.checked = true;

  syncMaintenanceScopeFields();
  const params = new URLSearchParams();
  const fd = new FormData(form);
  for (const [k, v] of fd.entries()) {
    params.append(k, v);
  }
  fetch("/actions/maintenance-confirm-summary", {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      Accept: "application/json",
    },
    body: params.toString(),
    credentials: "same-origin",
  })
    .then((res) => res.json())
    .then((data) => {
      if (!toggle.checked) return;
      if (data && data.error) {
        affectedEl.textContent = "Could not count packed videos.";
        return;
      }
      const n = data && typeof data.packed_videos === "number" ? data.packed_videos : 0;
      affectedEl.textContent =
        n === 1
          ? "Affects 1 packed video in this scope."
          : "Affects " + n + " packed videos in this scope.";
      if (data && data.contacts_external) {
        externalText.textContent =
          "External sites will be contacted. 'Refresh sidecars' queues one fetch per packed video on each video's source domain (cooldown and parallel limits apply). Do not run this against a large scope unless you intend live network traffic to those hosts.";
        externalBox.classList.remove("hidden");
      } else {
        externalBox.classList.add("hidden");
        externalText.textContent = "";
      }
    })
    .catch(() => {
      if (!toggle.checked) return;
      affectedEl.textContent = "Could not count packed videos.";
    });
  return true;
}

function closeMaintenanceConfirm() {
  const toggle = document.getElementById("modal-maintenance-confirm");
  if (toggle) toggle.checked = false;
}

function refreshMaintenanceLive() {
  if (!onMaintenancePage() || !document.getElementById("maintenance-live") || !window.htmx) return;
  window.htmx.ajax("GET", "/settings/maintenance", {
    target: "#maintenance-live",
    select: "#maintenance-live",
    swap: "outerHTML",
  });
}

export function maybeRefreshMaintenance(ev) {
  if (!onMaintenancePage()) return;
  if (ev.type !== "task.done" && ev.type !== "task.failed") return;
  let kind = "";
  try {
    const data = JSON.parse(ev.data || "{}");
    kind = data.kind || "";
  } catch (_) {}
  if (!maintenanceTaskKinds.has(kind)) return;
  refreshMaintenanceLive();
}

export function bootMaintenance() {
  if (!document.documentElement.dataset.maintenanceScopeDelegated) {
    document.documentElement.dataset.maintenanceScopeDelegated = "1";
    document.addEventListener("click", (ev) => {
      if (!onMaintenancePage()) return;
      const removeBtn = ev.target.closest("[data-maintenance-remove-series]");
      if (removeBtn) {
        removeMaintenanceSeries(removeBtn.getAttribute("data-maintenance-remove-series"));
        return;
      }
      const pick = ev.target.closest("[data-maintenance-pick-series]");
      if (pick) {
        ev.preventDefault();
        const dd = pick.closest("details.js-maintenance-series-dd");
        addMaintenanceSeries(pick.getAttribute("data-maintenance-pick-series"));
        if (dd) dd.open = false;
        const qEl = document.querySelector(".js-maintenance-series-q");
        if (qEl) qEl.value = "";
        return;
      }
      if (ev.target.closest("#maintenance-preview-rename")) {
        const btn = document.getElementById("maintenance-preview-rename");
        if (!btn || btn.getAttribute("aria-disabled") === "true") return;
        openMaintenanceRenamePreview();
        return;
      }
      if (ev.target.closest("#maintenance-confirm-submit")) {
        const form = document.getElementById("maintenance-run-form");
        closeMaintenanceConfirm();
        if (!form) return;
        syncMaintenanceScopeFields();
        clearMaintenanceScope();
        maintenanceSelectedActions = new Set();
        form.dataset.maintenanceConfirmed = "1";
        if (typeof form.requestSubmit === "function") form.requestSubmit();
        else form.submit();
      }
    });
    document.addEventListener("toggle", (ev) => {
      if (!onMaintenancePage()) return;
      const dd = ev.target;
      if (!(dd instanceof HTMLDetailsElement) || !dd.open) return;
      if (!dd.classList.contains("js-maintenance-series-dd")) return;
      const qEl = dd.querySelector(".js-maintenance-series-q");
      ensureMaintenanceSeriesCatalog().then(() => {
        fillMaintenanceSeriesPickList(qEl ? qEl.value : "");
        queueMicrotask(() => qEl && qEl.focus({ preventScroll: true }));
      });
    }, true);
    document.addEventListener("pointerdown", (ev) => {
      if (!onMaintenancePage()) return;
      document.querySelectorAll("details.js-maintenance-series-dd[open]").forEach((dd) => {
        if (!dd.contains(ev.target)) dd.open = false;
      });
    }, true);
    document.addEventListener("input", (ev) => {
      if (!onMaintenancePage()) return;
      if (ev.target && ev.target.classList && ev.target.classList.contains("js-maintenance-series-q")) {
        fillMaintenanceSeriesPickList(ev.target.value);
      }
    });
    document.addEventListener("change", (ev) => {
      if (!onMaintenancePage()) return;
      if (!ev.target || !ev.target.classList || !ev.target.classList.contains("js-maintenance-action")) {
        return;
      }
      readMaintenanceActionChecks();
      updateMaintenanceRunButton();
    });
    document.addEventListener("submit", (ev) => {
      if (!onMaintenancePage()) return;
      const form = ev.target && ev.target.closest ? ev.target.closest("form.js-maintenance-run-form") : null;
      if (!form) return;
      if (form.dataset.maintenanceConfirmed === "1") {
        delete form.dataset.maintenanceConfirmed;
        return;
      }
      ev.preventDefault();
      readMaintenanceActionChecks();
      if (maintenanceSelectedActions.size === 0) return;
      openMaintenanceConfirm();
    });
  }
}
