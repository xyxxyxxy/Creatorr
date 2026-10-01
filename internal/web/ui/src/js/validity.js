export function openAddSeriesModal() {
  if (new URLSearchParams(location.search).get("add") !== "1") return;
  const el = document.getElementById("modal-add-series");
  if (el) el.checked = true;
}

export function openSeriesMetadataModal() {
  const q = new URLSearchParams(location.search);
  if (q.get("meta") !== "1" && !q.get("prefetch_task")) return;
  const el = document.getElementById("modal-edit-series-metadata");
  if (el) el.checked = true;
}

export function setPanelControls(panel, enabled) {
  if (!panel) return;
  // Caller owns panel visibility (.hidden). Only soft-enable/disable controls here.
  panel.querySelectorAll("input, select, textarea").forEach((el) => {
    // Delivery-mode gate owns these; soft panel toggle must not lock them.
    if (el.hasAttribute("data-quality-profile-hidden") || el.hasAttribute("data-quality-profile-select")) {
      return;
    }
    // Remember HTML-permanent disabled (e.g. a field disabled by server-rendered state)
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

export function clearPanelValues(panel) {
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
      // Restore HTML default (e.g. scan_cron @weekly), do not wipe to empty.
      el.value = el.defaultValue;
    }
  });
}

/** daisyUI validator hint sibling (https://daisyui.com/components/validator/). */
function controlValidatorHint(el) {
  if (!el) return null;
  const labelHost = el.closest("label.input.validator");
  if (labelHost) {
    let n = labelHost.nextElementSibling;
    while (n && n.tagName === "DATALIST") n = n.nextElementSibling;
    if (n && n.classList.contains("validator-hint")) return n;
  }
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
export function setControlValidity(el, msg) {
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
  // label-for-input: paint the label.input host (daisyUI .validator[aria-invalid] → --input-color error).
  const labelHost = el.closest("label.input.validator");
  if (labelHost && labelHost !== el) {
    if (invalid) labelHost.setAttribute("aria-invalid", "true");
    else labelHost.removeAttribute("aria-invalid");
  }
  const hint = controlValidatorHint(el);
  if (!hint) return;
  const daisySibling =
    (labelHost && labelHost.nextElementSibling === hint) ||
    (el.classList.contains("validator") && el.parentElement === hint.parentElement);
  if (text) {
    hint.textContent = text;
    if (!daisySibling) {
      hint.classList.remove("hidden");
      hint.style.visibility = "visible";
      hint.style.color = "var(--color-error)";
    }
  } else if (!daisySibling) {
    hint.style.visibility = "";
    hint.style.color = "";
    hint.classList.add("hidden");
  }
}

export function clearControlValidity(el) {
  setControlValidity(el, "");
}

export function clearFormControlValidity(root) {
  if (!root) return;
  root.querySelectorAll("[aria-invalid]").forEach((el) => el.removeAttribute("aria-invalid"));
}

/** Absolute path: Unix "/" or Windows drive "C:\\" / "C:/". */
export function isAbsolutePathClient(p) {
  const s = String(p || "").trim();
  if (!s) return false;
  if (s.startsWith("/")) return true;
  return /^[A-Za-z]:[\\/]/.test(s);
}

export function setRootFolderFormErr(form, data) {
  if (!form) return;
  clearFormControlValidity(form);
  const msg = String((data && (data.error || data.message)) || "").replace(/^invalid:\s*/i, "").trim();
  if (!msg) return;
  const fieldName = String((data && data.field) || "").trim();
  let field = fieldName ? form.querySelector('[name="' + fieldName + '"]') : null;
  if (!field) {
    const lower = msg.toLowerCase();
    if (/\bpath\b/.test(lower)) field = form.querySelector('input[name="path"]');
    else if (/episode/.test(lower)) field = form.querySelector('input[name="episode_format"]');
    else if (/retention/.test(lower)) field = form.querySelector('input[name="retention_ttl_days"]');
  }
  if (field) {
    setControlValidity(field, msg);
    try {
      field.focus();
      if (typeof field.select === "function") field.select();
    } catch (_) {}
    return;
  }
  if (typeof window.showFlashToast === "function") {
    window.showFlashToast(msg, { error: true });
  }
}

export function bootValidity() {
  window.setControlValidity = setControlValidity;

  window.clearControlValidity = clearControlValidity;

  window.clearFormControlValidity = clearFormControlValidity;
}
