// --- Browser Notifications bulk selection (mark read / unread) ---
const notificationsBulkSelected = new Set();

let notificationsBulkMode = false;

/** Confirm modal only when selection exceeds this (≤5 submit immediately). */
const NOTIFICATIONS_BULK_CONFIRM_AFTER = 5;

const NOTIFICATIONS_BULK_ROW = "#notifications-list-rows > [data-notification-id]";

function notificationsBulkLive() {
  return document.getElementById("notifications-list-live");
}

function notificationsBulkFilterTotal() {
  const live = notificationsBulkLive();
  if (!live) return 0;
  const n = parseInt(live.getAttribute("data-filter-total") || "0", 10);
  return Number.isFinite(n) ? n : 0;
}

function notificationsBulkPageCheckboxes() {
  const live = notificationsBulkLive();
  if (!live) return [];
  return Array.from(live.querySelectorAll(".js-notification-select"));
}

function fillNotificationsBulkIDs(root) {
  const hosts = (root || document).querySelectorAll("[data-bulk-notification-ids]");
  hosts.forEach((host) => {
    host.replaceChildren();
    notificationsBulkSelected.forEach((id) => {
      const input = document.createElement("input");
      input.type = "hidden";
      input.name = "notification_id";
      input.value = String(id);
      host.appendChild(input);
    });
  });
}

function setNotificationsBulkMode(on) {
  notificationsBulkMode = !!on;
  if (!notificationsBulkMode) notificationsBulkSelected.clear();
  syncNotificationsBulkUI();
}

function toggleNotificationsBulkID(id) {
  if (!id) return;
  if (notificationsBulkSelected.has(id)) notificationsBulkSelected.delete(id);
  else notificationsBulkSelected.add(id);
  const live = notificationsBulkLive();
  const cb = live
    ? live.querySelector('.js-notification-select[value="' + CSS.escape(id) + '"]')
    : null;
  if (cb) cb.checked = notificationsBulkSelected.has(id);
  syncNotificationsBulkUI();
}

function syncNotificationsBulkUI() {
  const live = notificationsBulkLive();
  if (!live) return;
  live.setAttribute("data-bulk-mode", notificationsBulkMode ? "1" : "0");
  const modeBtn = live.querySelector("[data-notifications-bulk-mode]");
  if (modeBtn) {
    modeBtn.setAttribute("aria-pressed", notificationsBulkMode ? "true" : "false");
    const wrap = modeBtn.closest(".js-list-toolbar-dd");
    if (wrap) wrap.classList.toggle("is-bulk-on", notificationsBulkMode);
    modeBtn.setAttribute(
      "data-tip",
      notificationsBulkMode ? "Exit multi-select" : "Multi-select"
    );
    modeBtn.setAttribute(
      "aria-label",
      notificationsBulkMode ? "Exit multi-select" : "Multi-select"
    );
  }
  live.querySelectorAll("[data-notification-select-wrap]").forEach((wrap) => {
    const hasCb = !!wrap.querySelector(".js-notification-select");
    wrap.classList.toggle("hidden", !notificationsBulkMode || !hasCb);
    if (wrap.tagName === "TH" || wrap.tagName === "TD") {
      wrap.setAttribute("aria-hidden", notificationsBulkMode && hasCb ? "false" : "true");
    }
  });
  const rowActionsDisabled = notificationsBulkMode;
  live.querySelectorAll("[data-notifications-row-actions]").forEach((wrap) => {
    wrap.classList.remove("tooltip", "tooltip-left");
    wrap.removeAttribute("data-tip");
    wrap.querySelectorAll("button").forEach((btn) => {
      if (rowActionsDisabled) {
        if (!btn.hasAttribute("data-bulk-prev-disabled")) {
          btn.setAttribute("data-bulk-prev-disabled", btn.disabled ? "1" : "0");
        }
        btn.disabled = true;
        if (!btn.hasAttribute("data-bulk-prev-tip")) {
          btn.setAttribute("data-bulk-prev-tip", btn.getAttribute("data-tip") || "");
          btn.setAttribute(
            "data-bulk-prev-tooltip",
            btn.classList.contains("tooltip") ? "1" : "0"
          );
        }
        btn.removeAttribute("data-tip");
        btn.classList.remove("tooltip", "tooltip-top", "tooltip-left");
        btn.classList.add("opacity-50", "cursor-not-allowed");
      } else {
        const prev = btn.getAttribute("data-bulk-prev-disabled");
        if (prev !== null) {
          btn.disabled = prev === "1";
          btn.removeAttribute("data-bulk-prev-disabled");
        }
        if (btn.hasAttribute("data-bulk-prev-tip")) {
          const tip = btn.getAttribute("data-bulk-prev-tip");
          const hadTip = btn.getAttribute("data-bulk-prev-tooltip") === "1";
          btn.removeAttribute("data-bulk-prev-tip");
          btn.removeAttribute("data-bulk-prev-tooltip");
          if (tip) btn.setAttribute("data-tip", tip);
          if (hadTip) btn.classList.add("tooltip", "tooltip-top");
        }
        btn.classList.remove("opacity-50", "cursor-not-allowed");
      }
    });
  });
  live.querySelectorAll(NOTIFICATIONS_BULK_ROW).forEach((row) => {
    row.classList.toggle("cursor-pointer", notificationsBulkMode);
    const id = row.getAttribute("data-notification-id");
    const selected = notificationsBulkMode && notificationsBulkSelected.has(id);
    row.classList.toggle("bg-base-200", selected);
    if (row.classList.contains("list-row")) {
      row.classList.toggle("rounded-none", selected);
      if (notificationsBulkMode) {
        row.style.setProperty(
          "--list-grid-cols",
          "max-content max-content 1fr max-content"
        );
      } else {
        row.style.setProperty("--list-grid-cols", "max-content 1fr max-content");
      }
      row.querySelectorAll(".list-col-grow a[href]").forEach((a) => {
        a.classList.toggle("link", !notificationsBulkMode);
        a.classList.toggle("link-hover", !notificationsBulkMode);
      });
    }
  });
  const bar = live.querySelector("[data-notifications-bulk-bar]");
  if (bar) {
    bar.classList.toggle("hidden", !notificationsBulkMode);
    const wrap = document.getElementById("notifications-bulk-bar-wrap");
    if (wrap) wrap.classList.toggle("hidden", !notificationsBulkMode);
    const n = notificationsBulkSelected.size;
    const m = notificationsBulkFilterTotal();
    const countEl = bar.querySelector("[data-notifications-bulk-count]");
    if (countEl) countEl.textContent = n + "/" + m;
    bar
      .querySelectorAll("[data-notifications-bulk-read], [data-notifications-bulk-unread]")
      .forEach((btn) => {
        btn.disabled = n === 0;
      });
    const selectAllBtn = bar.querySelector("[data-notifications-select-all-matching]");
    if (selectAllBtn) {
      selectAllBtn.disabled = m > 0 && n >= m;
    }
  }
  const pageBoxes = notificationsBulkPageCheckboxes();
  pageBoxes.forEach((cb) => {
    cb.checked = notificationsBulkSelected.has(cb.value);
  });
  if (bar) {
    const pageAll =
      pageBoxes.length > 0 &&
      pageBoxes.every((cb) => notificationsBulkSelected.has(cb.value));
    const pageCb = document.getElementById("notifications-select-page");
    if (pageCb) {
      pageCb.checked = pageAll;
      pageCb.indeterminate =
        !pageAll && pageBoxes.some((cb) => notificationsBulkSelected.has(cb.value));
      pageCb.disabled = pageBoxes.length === 0;
    }
  }
  fillNotificationsBulkIDs(live);
}

function submitNotificationsBulk(formId) {
  fillNotificationsBulkIDs(document);
  const form = document.getElementById(formId);
  if (!form) return;
  notificationsBulkSelected.clear();
  form.requestSubmit();
}

function openNotificationsBulkModal(id) {
  const toggle = document.getElementById(id);
  if (toggle) toggle.checked = true;
}

function runNotificationsBulkAction(action) {
  if (!notificationsBulkMode || notificationsBulkSelected.size === 0) return;
  const n = notificationsBulkSelected.size;
  fillNotificationsBulkIDs(document);
  const setTitle = (sel, text) => {
    const el = document.querySelector(sel);
    if (el) el.textContent = text;
  };
  if (action === "read") {
    if (n <= NOTIFICATIONS_BULK_CONFIRM_AFTER) {
      submitNotificationsBulk("form-bulk-notification-read");
      return;
    }
    setTitle("[data-notifications-bulk-read-title]", "Mark read " + n + " notifications");
    openNotificationsBulkModal("modal-bulk-notification-read");
    return;
  }
  if (action === "unread") {
    if (n <= NOTIFICATIONS_BULK_CONFIRM_AFTER) {
      submitNotificationsBulk("form-bulk-notification-unread");
      return;
    }
    setTitle("[data-notifications-bulk-unread-title]", "Mark unread " + n + " notifications");
    openNotificationsBulkModal("modal-bulk-notification-unread");
  }
}

async function selectAllMatchingNotifications() {
  const q = location.search || "";
  const url = "/notifications/ids" + q;
  const resp = await fetch(url, { headers: { Accept: "application/json" } });
  if (!resp.ok) throw new Error("failed to load matching ids");
  const data = await resp.json();
  const ids = Array.isArray(data.ids) ? data.ids : [];
  notificationsBulkSelected.clear();
  ids.forEach((id) => notificationsBulkSelected.add(String(id)));
  syncNotificationsBulkUI();
}

function onNotificationsBulkPage() {
  return !!notificationsBulkLive();
}

export function refreshNotificationsBulkAfterDOM() {
  syncNotificationsBulkUI();
}

export function bootNotificationsBulk() {
  document.body.addEventListener("change", (ev) => {
    const t = ev.target;
    if (!(t instanceof HTMLInputElement)) return;
    if (t.classList.contains("js-notification-select")) {
      if (t.checked) notificationsBulkSelected.add(t.value);
      else notificationsBulkSelected.delete(t.value);
      syncNotificationsBulkUI();
      return;
    }
    if (t.id === "notifications-select-page") {
      const boxes = notificationsBulkPageCheckboxes();
      if (t.checked) {
        boxes.forEach((cb) => {
          notificationsBulkSelected.add(cb.value);
          cb.checked = true;
        });
      } else {
        const pageIds = new Set(boxes.map((cb) => cb.value));
        const hasOffPage = Array.from(notificationsBulkSelected).some(
          (id) => !pageIds.has(id)
        );
        if (hasOffPage) {
          notificationsBulkSelected.clear();
          boxes.forEach((cb) => {
            cb.checked = false;
          });
        } else {
          boxes.forEach((cb) => {
            notificationsBulkSelected.delete(cb.value);
            cb.checked = false;
          });
        }
      }
      syncNotificationsBulkUI();
    }
  });

  document.body.addEventListener("click", (ev) => {
    const modeBtn = ev.target.closest("[data-notifications-bulk-mode]");
    if (modeBtn) {
      ev.preventDefault();
      setNotificationsBulkMode(!notificationsBulkMode);
      return;
    }
    if (notificationsBulkMode) {
      const row = ev.target.closest(NOTIFICATIONS_BULK_ROW);
      if (
        row &&
        !ev.target.closest(
          ".js-notification-select, [data-notification-select-wrap], [data-notifications-row-actions]"
        )
      ) {
        ev.preventDefault();
        const id = row.getAttribute("data-notification-id");
        if (id) toggleNotificationsBulkID(id);
        return;
      }
    }
    const selectAll = ev.target.closest("[data-notifications-select-all-matching]");
    if (selectAll) {
      ev.preventDefault();
      if (selectAll.disabled) return;
      selectAll.disabled = true;
      selectAllMatchingNotifications()
        .catch(() => {})
        .finally(() => syncNotificationsBulkUI());
      return;
    }
    const read = ev.target.closest("[data-notifications-bulk-read]");
    if (read) {
      ev.preventDefault();
      runNotificationsBulkAction("read");
      return;
    }
    const unread = ev.target.closest("[data-notifications-bulk-unread]");
    if (unread) {
      ev.preventDefault();
      runNotificationsBulkAction("unread");
    }
  });

  document.body.addEventListener("submit", (ev) => {
    const form = ev.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (
      form.id !== "form-bulk-notification-read-confirm" &&
      form.id !== "form-bulk-notification-unread-confirm"
    ) {
      return;
    }
    fillNotificationsBulkIDs(document);
  });

  document.body.addEventListener("htmx:beforeRequest", (ev) => {
    if (!onNotificationsBulkPage() || !notificationsBulkMode) return;
    const elt = ev.detail && ev.detail.elt;
    if (!elt || !elt.closest) return;
    const live = notificationsBulkLive();
    if (!live) return;
    const form =
      elt.closest("#" + live.id + " form.js-list-filters") ||
      (elt.matches && elt.matches("#" + live.id + " form.js-list-filters") ? elt : null);
    if (!form) return;
    notificationsBulkSelected.clear();
    syncNotificationsBulkUI();
  });

  // afterSettle: HTMX class settle restores response `hidden` on id'd bulk bar.
  const onNotificationsBulkLiveSwap = (ev) => {
    const target = ev.detail && ev.detail.target;
    if (!target || target.id !== "notifications-list-live") return;
    syncNotificationsBulkUI();
  };
  document.body.addEventListener("htmx:afterSwap", onNotificationsBulkLiveSwap);
  document.body.addEventListener("htmx:afterSettle", onNotificationsBulkLiveSwap);

  if (onNotificationsBulkPage()) {
    syncNotificationsBulkUI();
  }
}
