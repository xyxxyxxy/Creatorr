import { setAddSeriesFetchErr, syncAddSeriesForm } from "./add_series.js";
import { createLucideIcons } from "./dom_helpers.js";
import { resetCredentialsPasswordUI, syncAllScanCronJoins, syncPresetChips } from "./joins.js";
import { syncSourceURLForm } from "./source_forms.js";
import { syncStringListEmptyLabel } from "./string_lists.js";
import { clearFormControlValidity } from "./validity.js";

export function snapshotStringListEditors(root) {
  (root || document).querySelectorAll("[data-string-list-editor]").forEach((editor) => {
    if (editor.dataset.stringListOrig != null) return;
    const list = editor.querySelector("[data-string-list]");
    if (!list) return;
    editor.dataset.stringListOrig = list.innerHTML;
  });
}

function resetStringListEditors(form) {
  if (!form) return;
  snapshotStringListEditors(form);
  form.querySelectorAll("[data-string-list-editor]").forEach((editor) => {
    const list = editor.querySelector("[data-string-list]");
    if (!list || editor.dataset.stringListOrig == null) return;
    list.innerHTML = editor.dataset.stringListOrig;
    createLucideIcons(list);
    syncStringListEmptyLabel(editor);
    const draft = editor.querySelector("[data-string-list-draft-value]");
    if (draft instanceof HTMLInputElement) draft.value = "";
  });
}

function resetArtSlots(form) {
  if (!form) return;
  form.querySelectorAll("[data-art-slot]").forEach((slot) => {
    const img = slot.querySelector("[data-art-preview]");
    const empty = slot.querySelector("[data-art-empty]");
    const clearBtn = slot.querySelector("[data-art-clear-btn]");
    const pref = slot.querySelector("input[data-art-prefetch]");
    const clear = slot.querySelector("input[data-art-clear]");
    const file = slot.querySelector("input[data-art-file]");
    if (pref) pref.disabled = false;
    if (clear) clear.value = "";
    if (file) file.value = "";
    if (!img) return;
    const prev = img.dataset.objectUrl;
    if (prev) {
      URL.revokeObjectURL(prev);
      delete img.dataset.objectUrl;
    }
    const orig = img.dataset.origSrc || "";
    if (orig) {
      img.src = orig;
      img.classList.remove("hidden");
      if (empty) empty.classList.add("hidden");
      if (clearBtn) {
        clearBtn.disabled = false;
        clearBtn.classList.remove("hidden");
        clearBtn.removeAttribute("aria-hidden");
        clearBtn.removeAttribute("tabindex");
      }
    } else {
      img.removeAttribute("src");
      img.classList.add("hidden");
      if (empty) empty.classList.remove("hidden");
      if (clearBtn) {
        clearBtn.disabled = true;
        clearBtn.classList.remove("hidden");
        clearBtn.removeAttribute("aria-hidden");
        clearBtn.removeAttribute("tabindex");
      }
    }
  });
}

export function setArtPreviewVisible(slot, visible) {
  if (!slot) return;
  const img = slot.querySelector("[data-art-preview]");
  const empty = slot.querySelector("[data-art-empty]");
  const clearBtn = slot.querySelector("[data-art-clear-btn]");
  const editBtn = slot.querySelector("[data-art-edit-btn]");
  if (img) img.classList.toggle("hidden", !visible);
  if (empty) {
    empty.classList.toggle("hidden", visible);
    empty.setAttribute("aria-hidden", visible ? "true" : "false");
  }
  if (clearBtn) {
    clearBtn.disabled = !visible;
    clearBtn.removeAttribute("aria-hidden");
    clearBtn.removeAttribute("tabindex");
    clearBtn.classList.remove("hidden");
  }
  if (editBtn) {
    const label = (editBtn.getAttribute("aria-label") || "").replace(/^(Set|Replace)\b/, visible ? "Replace" : "Set");
    editBtn.setAttribute("aria-label", label);
  }
}

export function markArtCleared(slot) {
  if (!slot) return;
  const clear = slot.querySelector("input[data-art-clear]");
  const file = slot.querySelector("input[data-art-file]");
  const img = slot.querySelector("[data-art-preview]");
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
  // Drop image from UI only; disk clear waits for Save (clear_{Role}=1).
  setArtPreviewVisible(slot, false);
}

export function bootFormReset() {
  // Cancel discards draft; closing via toggle (not backdrop - inert) keeps form state.
  // Metadata Fetch HTMX-swaps the body with draft values as new defaults, so form.reset()
  // cannot restore saved fields - reload a clean body from the server instead.
  document.body.addEventListener("click", (ev) => {
    const cancel = ev.target.closest("label.modal-cancel");
    if (!cancel) return;
    const modal = cancel.closest(".modal");
    if (!modal) return;

    const metaBody = modal.querySelector("[data-meta-reset]");
    if (metaBody && metaBody.dataset.metaReset && window.htmx) {
      let url = metaBody.dataset.metaReset;
      const tidEl = metaBody.querySelector('input[name="prefetch_task_id"]');
      const tid = tidEl && String(tidEl.value || "").trim();
      if (tid) {
        url += (url.indexOf("?") >= 0 ? "&" : "?") + "discard=" + encodeURIComponent(tid);
      } else {
        const poll = metaBody.getAttribute("hx-get") || "";
        const m = poll.match(/\/metadata\/prefetch\/(\d+)/);
        if (m) {
          url += (url.indexOf("?") >= 0 ? "&" : "?") + "discard=" + encodeURIComponent(m[1]);
        }
      }
      window.htmx.ajax("GET", url, { target: metaBody, swap: "outerHTML" });
      try {
        const u = new URL(location.href);
        if (u.searchParams.has("prefetch_task") || u.searchParams.has("meta_prefetch") || u.searchParams.get("meta") === "1") {
          u.searchParams.delete("prefetch_task");
          u.searchParams.delete("meta_prefetch");
          u.searchParams.delete("meta");
          const qs = u.searchParams.toString();
          history.replaceState({}, "", u.pathname + (qs ? "?" + qs : "") + u.hash);
        }
      } catch (_) {
        /* ignore */
      }
      return;
    }

    modal.querySelectorAll("form").forEach((form) => {
      form.reset();
      resetArtSlots(form);
      resetStringListEditors(form);
      resetCredentialsPasswordUI(form);
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
}
