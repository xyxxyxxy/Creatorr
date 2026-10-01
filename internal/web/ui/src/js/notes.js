import { LIST_FILTER_SEARCH_MS } from "./list_filter.js";
import { applySettingsOOB } from "./settings_autosave.js";

// Operator notes (js-notes-autosave): 4× list-filter search pause; flush on blur,
// pagehide / leave-link, and before video-detail task reload. Success/error use
// top-right flash toasts. Keepalive path must not reuse a non-keepalive in-flight
// fetch (that request is aborted on navigate).
const NOTES_AUTOSAVE_MS = LIST_FILTER_SEARCH_MS * 4;

const notesAutosaveTimers = new WeakMap();

const notesAutosaveState = new WeakMap();

function notesState(ta) {
  let st = notesAutosaveState.get(ta);
  if (!st) {
    st = {
      saved: ta.value,
      dirty: false,
      inFlight: null,
    };
    notesAutosaveState.set(ta, st);
  }
  return st;
}

function clearNotesTimer(ta) {
  const t = notesAutosaveTimers.get(ta);
  if (t != null) {
    window.clearTimeout(t);
    notesAutosaveTimers.delete(ta);
  }
}

function notesAutosaveBody(ta) {
  const idName = ta.getAttribute("data-id-name") || "";
  const idValue = ta.getAttribute("data-id-value") || "";
  const body = new URLSearchParams();
  body.set(idName, idValue);
  body.set("notes", ta.value);
  return body;
}

function postNotesKeepalive(ta) {
  const action = ta.getAttribute("data-autosave-action") || "";
  const idName = ta.getAttribute("data-id-name") || "";
  if (!action || !idName) return;
  const st = notesState(ta);
  const value = ta.value;
  if (value === st.saved && !st.dirty) return;
  clearNotesTimer(ta);
  try {
    fetch(action, {
      method: "POST",
      body: notesAutosaveBody(ta).toString(),
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        "HX-Request": "true",
      },
      credentials: "same-origin",
      keepalive: true,
    }).catch(() => {});
  } catch (_) {
    /* unload best-effort */
  }
  st.saved = value;
  st.dirty = false;
  st.inFlight = null;
}

function postNotesAutosave(ta, opts) {
  const keepalive = !!(opts && opts.keepalive);
  if (keepalive) {
    postNotesKeepalive(ta);
    return Promise.resolve();
  }
  const st = notesState(ta);
  const action = ta.getAttribute("data-autosave-action") || "";
  const idName = ta.getAttribute("data-id-name") || "";
  if (!action || !idName) return Promise.resolve();
  const value = ta.value;
  if (value === st.saved && !st.dirty) return Promise.resolve();
  if (st.inFlight) {
    st.dirty = true;
    return st.inFlight;
  }
  st.dirty = true;
  const p = fetch(action, {
    method: "POST",
    body: notesAutosaveBody(ta).toString(),
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      "HX-Request": "true",
    },
    credentials: "same-origin",
  })
    .then(async (resp) => {
      const text = await resp.text().catch(() => "");
      if (text) applySettingsOOB(text);
      if (!resp.ok) {
        const tooLong = resp.status === 422;
        const msg = tooLong ? "Notes too long." : "Save failed.";
        if (!text) window.showFlashToast(msg, { error: true });
        throw new Error(msg);
      }
      st.saved = value;
      st.dirty = ta.value !== st.saved;
      window.showFlashToast("Saved.");
    })
    .finally(() => {
      st.inFlight = null;
      if (ta.value !== st.saved) {
        st.dirty = true;
        postNotesAutosave(ta);
      }
    });
  st.inFlight = p;
  return p;
}

function scheduleNotesAutosave(ta) {
  if (!ta || !ta.classList.contains("js-notes-autosave")) return;
  const st = notesState(ta);
  if (ta.value === st.saved) {
    st.dirty = false;
    clearNotesTimer(ta);
    return;
  }
  st.dirty = true;
  clearNotesTimer(ta);
  notesAutosaveTimers.set(
    ta,
    window.setTimeout(() => {
      notesAutosaveTimers.delete(ta);
      postNotesAutosave(ta);
    }, NOTES_AUTOSAVE_MS)
  );
}

export function flushNotesAutosave(opts) {
  const keepalive = !!(opts && opts.keepalive);
  const nodes = document.querySelectorAll("textarea.js-notes-autosave");
  if (keepalive) {
    nodes.forEach((ta) => postNotesKeepalive(ta));
    return Promise.resolve();
  }
  const jobs = [];
  nodes.forEach((ta) => {
    clearNotesTimer(ta);
    const st = notesState(ta);
    if (ta.value !== st.saved || st.dirty || st.inFlight) {
      jobs.push(postNotesAutosave(ta));
    }
  });
  if (!jobs.length) return Promise.resolve();
  return Promise.allSettled(jobs);
}

function notesLeaveNeedsFlush(el) {
  if (!el || !(el instanceof Element)) return false;
  const a = el.closest("a[href]");
  if (!a) return false;
  const href = a.getAttribute("href") || "";
  if (!href || href.startsWith("#") || href.startsWith("javascript:")) return false;
  if (a.hasAttribute("download") || a.getAttribute("target") === "_blank") return false;
  return true;
}

export function bootNotes() {
  document.body.addEventListener("input", (ev) => {
    const ta = ev.target;
    if (!ta || ta.tagName !== "TEXTAREA" || !ta.classList.contains("js-notes-autosave")) return;
    scheduleNotesAutosave(ta);
  });

  document.body.addEventListener("focusout", (ev) => {
    const ta = ev.target;
    if (!ta || ta.tagName !== "TEXTAREA" || !ta.classList.contains("js-notes-autosave")) return;
    clearNotesTimer(ta);
    const st = notesState(ta);
    if (ta.value === st.saved && !st.dirty) return;
    if (notesLeaveNeedsFlush(ev.relatedTarget)) {
      postNotesKeepalive(ta);
      return;
    }
    postNotesAutosave(ta);
  });

  document.addEventListener(
    "click",
    (ev) => {
      if (ev.defaultPrevented || ev.button !== 0) return;
      if (ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey) return;
      if (!notesLeaveNeedsFlush(ev.target)) return;
      flushNotesAutosave({ keepalive: true });
    },
    true
  );

  window.addEventListener("pagehide", () => {
    flushNotesAutosave({ keepalive: true });
  });

  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") flushNotesAutosave({ keepalive: true });
  });
}
