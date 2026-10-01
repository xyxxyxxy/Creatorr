import { createLucideIcons } from "./dom_helpers.js";
import { copyInputValue, scheduleFlashToasts } from "./flash.js";
import { syncAllScanCronJoins, syncScanCronJoin } from "./joins.js";
import { commitActorDraft, commitStringListDraft, moveListRow, syncActorsEmpty, syncStringListEmptyLabel } from "./string_lists.js";
import { setControlValidity } from "./validity.js";

const cronAutosaveTimers = new WeakMap();

export function applySettingsOOB(html) {
  if (!html) return false;
  const doc = new DOMParser().parseFromString(html, "text/html");
  let swapped = false;
  doc.body.querySelectorAll("[hx-swap-oob]").forEach((el) => {
    const spec = el.getAttribute("hx-swap-oob") || "";
    if (spec === "true" || spec.startsWith("outerHTML")) {
      let target = null;
      if (spec.startsWith("outerHTML:")) {
        const sel = spec.slice("outerHTML:".length).trim();
        target = sel.startsWith("#") ? document.getElementById(sel.slice(1)) : document.querySelector(sel);
      } else if (el.id) {
        target = document.getElementById(el.id);
      }
      if (!target) return;
      target.replaceWith(document.adoptNode(el));
      swapped = true;
      return;
    }
    const colon = spec.indexOf(":");
    const mode = colon >= 0 ? spec.slice(0, colon).trim() : "beforeend";
    const sel = (colon >= 0 ? spec.slice(colon + 1) : spec).trim();
    const target = sel === "body" ? document.body : document.querySelector(sel);
    if (!target || mode !== "beforeend") return;
    target.appendChild(document.adoptNode(el));
    swapped = true;
  });
  if (swapped) {
    scheduleFlashToasts();
    createLucideIcons(document.body);
  }
  return swapped;
}

function swapCronFieldError(key, html) {
  const wrap = document.getElementById("setting-cron-wrap-" + key);
  if (!wrap || !html) return "";
  wrap.outerHTML = html;
  syncAllScanCronJoins(document.getElementById("setting-cron-wrap-" + key)?.parentElement);
  createLucideIcons(document.body);
  const input = document.getElementById("setting-" + key);
  const hint = document.querySelector("#setting-cron-wrap-" + key + " .validator-hint");
  const msg = hint?.textContent?.trim() || "";
  if (input instanceof HTMLInputElement && msg) setControlValidity(input, msg);
  return msg;
}

async function postSettingsAutosave(action, body, contentType) {
  const headers = { "HX-Request": "true" };
  if (contentType) headers["Content-Type"] = contentType;
  return fetch(action, {
    method: "POST",
    body,
    headers,
    credentials: "same-origin",
  });
}

function isCronAutosaveError(resp, text, cronHintName) {
  if (!resp || !text) return false;
  if (resp.status === 422) return true;
  if (resp.headers.get("X-Settings-Cron-Error") === "1") return true;
  if (!cronHintName) return false;
  return text.includes("setting-cron-wrap-" + cronHintName) && text.includes("validator-hint");
}

async function handleSettingsAutosaveResponse(resp, text, cronHintName) {
  if (resp.headers.get("HX-Redirect")) {
    window.location.assign(resp.headers.get("HX-Redirect"));
    return;
  }
  const cronKey = resp.headers.get("X-Settings-Cron-Key") || cronHintName || "";
  if (isCronAutosaveError(resp, text, cronHintName)) {
    const msg = swapCronFieldError(cronKey, text) || "Invalid schedule.";
    window.showFlashToast(msg, { error: true });
    return;
  }
  if (!resp.ok) {
    window.showFlashToast("Save failed.", { error: true });
    return;
  }
  if (!applySettingsOOB(text)) {
    window.showFlashToast("Settings saved.");
  }
}

function settingsAutosaveForm(form) {
  if (!form) return;
  const action = form.getAttribute("data-autosave-action") || form.getAttribute("action") || "/actions/save-settings";
  const body = new URLSearchParams(new FormData(form)).toString();
  postSettingsAutosave(action, body, "application/x-www-form-urlencoded")
    .then(async (resp) => {
      const text = await resp.text();
      await handleSettingsAutosaveResponse(resp, text, "");
    })
    .catch(() => window.showFlashToast("Save failed.", { error: true }));
}

export function maybeAutosaveSettingsEditor(editor) {
  const form = editor && editor.closest("form.js-settings-autosave");
  if (form) settingsAutosaveForm(form);
}

export function queueCronAutosave(join, immediate) {
  if (!join) return;
  const run = () => {
    cronAutosaveTimers.delete(join);
    settingsAutosaveCron(join);
  };
  if (immediate) {
    const prev = cronAutosaveTimers.get(join);
    if (prev) window.clearTimeout(prev);
    cronAutosaveTimers.delete(join);
    run();
    return;
  }
  const prev = cronAutosaveTimers.get(join);
  if (prev) window.clearTimeout(prev);
  cronAutosaveTimers.set(join, window.setTimeout(run, 50));
}

async function settingsAutosaveCron(join) {
  if (!join) return;
  syncScanCronJoin(join);
  const form = join.closest("form.js-settings-autosave");
  if (!form) return;
  const action = form.getAttribute("data-autosave-action") || form.getAttribute("action") || "/actions/save-settings";
  const input = join.querySelector("[data-cron-input]");
  if (!(input instanceof HTMLInputElement)) return;
  const name = input.dataset.cronName || "";
  if (!name) return;
  const regular = join.querySelector("[data-cron-regular]");
  const params = new URLSearchParams();
  const redirect = form.querySelector('input[name="redirect"]');
  if (redirect instanceof HTMLInputElement) params.set("redirect", redirect.value);
  if (regular instanceof HTMLInputElement && !regular.checked) {
    params.set(name, "");
  } else {
    const v = input.value.trim();
    params.set(name, v === "never" ? "" : v);
  }
  try {
    const resp = await postSettingsAutosave(action, params.toString(), "application/x-www-form-urlencoded");
    const text = await resp.text();
    await handleSettingsAutosaveResponse(resp, text, name);
  } catch (_) {
    window.showFlashToast("Save failed.", { error: true });
  }
}

export function bootSettingsAutosave() {
  document.body.addEventListener(
    "blur",
    (ev) => {
      const el = ev.target;
      if (el instanceof HTMLInputElement && el.hasAttribute("data-cron-input")) {
        const join = el.closest("[data-cron-join]");
        if (join) queueCronAutosave(join, true);
        return;
      }
      if (!(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement)) return;
      if (el.disabled || el.readOnly) return;
      const form = el.closest("form.js-settings-autosave");
      if (!form) return;
      if (
        el.hasAttribute("data-settings-autosave-blur") ||
        el.type === "number" ||
        el.type === "text" ||
        el.tagName === "TEXTAREA"
      ) {
        settingsAutosaveForm(form);
      }
    },
    true
  );

  document.body.addEventListener("keydown", (ev) => {
    const el = ev.target;
    if (!(el instanceof HTMLInputElement) || !el.hasAttribute("data-cron-input")) return;
    if (ev.key !== "Enter") return;
    const join = el.closest("[data-cron-join]");
    if (!join) return;
    ev.preventDefault();
    queueCronAutosave(join, true);
  });

  document.body.addEventListener("submit", (ev) => {
    const form = ev.target;
    if (!(form instanceof HTMLFormElement) || !form.classList.contains("js-settings-autosave")) return;
    ev.preventDefault();
    settingsAutosaveForm(form);
  });

  document.body.addEventListener("change", (ev) => {
    const el = ev.target;
    const form = el instanceof Element ? el.closest("form.js-settings-autosave") : null;
    if (!form) return;
    if (el instanceof HTMLInputElement && el.hasAttribute("data-cron-input")) {
      const join = el.closest("[data-cron-join]");
      if (join) queueCronAutosave(join, true);
      return;
    }
    if (el instanceof HTMLSelectElement || (el instanceof HTMLInputElement && el.type === "checkbox")) {
      if (el.hasAttribute("data-cron-regular")) return;
      settingsAutosaveForm(form);
    }
  });

  document.body.addEventListener("click", (ev) => {
    const copyBtn = ev.target.closest("[data-copy-input]");
    if (copyBtn) {
      ev.preventDefault();
      const inputId = copyBtn.getAttribute("data-copy-input");
      if (inputId) copyInputValue(inputId);
      return;
    }
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
      if (row && row.hasAttribute("data-string-list-managed-row")) return;
      const editor = listRm.closest("[data-string-list-editor]");
      if (row) row.remove();
      syncStringListEmptyLabel(editor);
      maybeAutosaveSettingsEditor(editor);
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
      'form[action="/actions/save-series-metadata"], form[action="/actions/save-video-metadata"], form[action="/actions/save-settings"], form[action="/actions/update-series"], form[action="/actions/add-series"], form[action="/actions/bulk-edit-series-metadata"], form[action="/actions/bulk-edit-videos-metadata"]'
    );
    if (!form) return;
    const actors = form.querySelector("[data-actors-editor]");
    if (actors) commitActorDraft(actors);
    form.querySelectorAll("[data-string-list-editor]").forEach((ed) => commitStringListDraft(ed));
  });
}
