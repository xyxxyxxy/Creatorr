import { syncAddSeriesSourceNav } from "./add_series.js";
import { syncSpecialKindSelect } from "./dom_helpers.js";
import { markArtCleared } from "./form_reset.js";
import { queueCronAutosave } from "./settings_autosave.js";
import { clearControlValidity, setControlValidity } from "./validity.js";

export function syncScanCronJoin(join) {
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
      input.value = (input.dataset.prevCron || input.dataset.cronDefault || "").trim();
    }
  }
}

export function syncAllScanCronJoins(root) {
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

function syncMaturityJoin(join) {
  if (!join) return;
  const input = join.querySelector("[data-maturity-input]");
  const enable = join.querySelector("[data-maturity-enable]");
  if (!(input instanceof HTMLInputElement) || !(enable instanceof HTMLInputElement)) return;
  const fieldName = input.dataset.maturityName || "";
  const activePh = input.dataset.maturityPlaceholder || input.dataset.maturityDefault || "";
  let hidden = join.querySelector("[data-maturity-submit]");
  if (!enable.checked) {
    const cur = input.value.trim();
    if (cur) input.dataset.prevMaturity = input.value;
    input.value = "";
    input.disabled = true;
    input.removeAttribute("name");
    input.removeAttribute("required");
    input.placeholder = "none";
    input.classList.add("opacity-60");
    if (!hidden) {
      hidden = document.createElement("input");
      hidden.type = "hidden";
      hidden.setAttribute("data-maturity-submit", "");
      join.insertBefore(hidden, input);
    }
    hidden.name = fieldName;
    hidden.value = "0";
  } else {
    input.disabled = false;
    input.name = fieldName;
    input.placeholder = activePh;
    input.classList.remove("opacity-60");
    if (hidden) hidden.remove();
    if (!input.value.trim()) {
      const fill = (input.dataset.prevMaturity || input.dataset.maturityDefault || "").trim();
      input.value = fill;
    }
  }
}

export function syncAllMaturityJoins(root) {
  (root || document).querySelectorAll("[data-maturity-join]").forEach((join) => {
    const input = join.querySelector("[data-maturity-input]");
    const enable = join.querySelector("[data-maturity-enable]");
    if (!(input instanceof HTMLInputElement) || !(enable instanceof HTMLInputElement)) return;
    // After form.reset(), derive enable from whether a non-zero value is present.
    const v = input.value.trim();
    enable.checked = !!v && v !== "0";
    syncMaturityJoin(join);
  });
}

function syncPackRoleJoin(join) {
  if (!join) return;
  const sel = join.querySelector("[data-pack-role-select]");
  const enable = join.querySelector("[data-pack-role-enable]");
  if (!(sel instanceof HTMLSelectElement) || !(enable instanceof HTMLInputElement)) return;
  let hidden = join.querySelector("[data-pack-role-submit]");
  const placeholder = sel.querySelector('option[value=""]');
  if (!enable.checked) {
    const cur = sel.value;
    if (cur) sel.dataset.prevPackRole = cur;
    sel.value = "";
    if (placeholder) placeholder.selected = true;
    sel.disabled = true;
    sel.removeAttribute("name");
    sel.classList.add("opacity-60");
    if (!hidden) {
      hidden = document.createElement("input");
      hidden.type = "hidden";
      hidden.setAttribute("data-pack-role-submit", "");
      join.insertBefore(hidden, sel);
    }
    hidden.name = "special_feature";
    hidden.value = "episode";
  } else {
    sel.disabled = false;
    sel.name = "special_feature";
    sel.classList.remove("opacity-60");
    if (hidden) hidden.remove();
    const cur = sel.value.trim();
    if (!cur) {
      let fill = (sel.dataset.prevPackRole || "").trim();
      if (!fill || fill === "episode") {
        fill = sel.dataset.packRoleDefault || "special_episode";
      }
      sel.value = fill;
      if (!sel.value) {
        sel.value = sel.dataset.packRoleDefault || "special_episode";
      }
    }
  }
}

export function syncAllPackRoleJoins(root) {
  (root || document).querySelectorAll("[data-pack-role-join]").forEach((join) => {
    const enable = join.querySelector("[data-pack-role-enable]");
    if (!(enable instanceof HTMLInputElement)) return;
    syncPackRoleJoin(join);
  });
}

function syncRateLimitJoin(join) {
  if (!join) return;
  const unit = join.querySelector("[data-rate-unit]");
  const num = join.querySelector("[data-rate-value]");
  if (!unit || !num) return;
  const wasDisabled = num.disabled;
  const off = unit.value === "off";
  const inherit = unit.value === "";
  num.disabled = off;
  num.readOnly = false;
  if (off) {
    num.value = "";
    num.removeAttribute("required");
  } else {
    if (wasDisabled && String(num.value || "").trim() === "") {
      num.value = "1";
    }
    if (join.hasAttribute("data-rate-required") && !inherit) {
      num.required = true;
    } else {
      num.removeAttribute("required");
    }
  }
}

export function syncAllRateLimitJoins(root) {
  (root || document).querySelectorAll("[data-rate-limit-join]").forEach(syncRateLimitJoin);
}

export function syncPresetChips(input) {
  // no-op: cron fields use datalist; chips removed
}

export function syncCredentialsPasswordValidity(form) {
  if (!form) return "";
  const wrap = form.querySelector(".js-credentials-override");
  if (!wrap) return "";
  const user = wrap.querySelector('input[name="username"]');
  const pass = wrap.querySelector('input[name="password"]');
  if (!user || !pass) return "";
  const keep = wrap.querySelector("[data-password-keep]");
  const keeping = !!(keep && keep.value === "1" && pass.disabled);
  const userSet = user.value.trim() !== "";
  const passSet = String(pass.value || "").trim() !== "";
  clearControlValidity(pass);
  if (userSet && !keeping && !passSet) {
    const msg = "Password required when username is set.";
    setControlValidity(pass, msg);
    return msg;
  }
  return "";
}

function setCredentialsPasswordMode(wrap, editing) {
  if (!wrap) return;
  const keep = wrap.querySelector("[data-password-keep]");
  const btn = wrap.querySelector("[data-credentials-reset-password]");
  const stored = wrap.querySelector("[data-credentials-password-stored]");
  const edit = wrap.querySelector("[data-credentials-password-edit]");
  const pass = edit && edit.querySelector('input[name="password"]');
  if (!keep || !btn || !stored || !edit || !pass) return;
  if (editing) {
    keep.value = "0";
    stored.hidden = true;
    edit.hidden = false;
    pass.disabled = false;
    clearControlValidity(pass);
    pass.focus();
  } else {
    keep.value = "1";
    stored.hidden = false;
    edit.hidden = true;
    pass.value = "";
    pass.disabled = true;
    clearControlValidity(pass);
  }
}

export function resetCredentialsPasswordUI(form) {
  if (!form) return;
  form.querySelectorAll(".js-credentials-override").forEach((wrap) => {
    setCredentialsPasswordMode(wrap, false);
  });
}

export function bootJoins() {
  document.body.addEventListener("input", (ev) => {
    const input = ev.target;
    if (!(input instanceof HTMLInputElement)) return;
    if (input.name !== "username" && input.name !== "password") return;
    const credWrap = input.closest(".js-credentials-override");
    if (!credWrap) return;
    if (input.name === "username") {
      const credInherit = credWrap.querySelector("[data-credentials-inherit]");
      if (credInherit && input.value.trim() !== "") credInherit.value = "0";
    }
    // Clear prior Save error while editing; re-validate only on submit.
    const pass = credWrap.querySelector('input[name="password"]');
    if (pass) clearControlValidity(pass);
  });

  // Preset chips fill the linked text field (legacy).
  document.body.addEventListener("click", (ev) => {
    const resetPassBtn = ev.target.closest("[data-credentials-reset-password]");
    if (resetPassBtn) {
      ev.preventDefault();
      const wrap = resetPassBtn.closest(".js-credentials-override");
      if (!wrap) return;
      setCredentialsPasswordMode(wrap, true);
      return;
    }
    const artClearBtn = ev.target.closest("[data-art-clear-btn]");
    if (artClearBtn) {
      ev.preventDefault();
      ev.stopPropagation();
      if (artClearBtn.disabled || artClearBtn.getAttribute("aria-disabled") === "true") {
        return;
      }
      markArtCleared(artClearBtn.closest("[data-art-slot]"));
      return;
    }
    // Edit is a <label for=file>; keep a click fallback when label wiring is missing.
    const artEditBtn = ev.target.closest("[data-art-edit-btn]");
    if (artEditBtn && artEditBtn.tagName !== "LABEL") {
      ev.preventDefault();
      const slot = artEditBtn.closest("[data-art-slot]");
      const file = slot && slot.querySelector("input[data-art-file]");
      if (file instanceof HTMLInputElement) file.click();
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
    if (el instanceof HTMLInputElement && el.hasAttribute("data-maturity-input")) {
      const join = el.closest("[data-maturity-join]");
      const enable = join && join.querySelector("[data-maturity-enable]");
      if (enable instanceof HTMLInputElement && el.value.trim() && el.value.trim() !== "0") {
        enable.checked = true;
        syncMaturityJoin(join);
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
      if (join) {
        syncScanCronJoin(join);
        queueCronAutosave(join, true);
      }
      return;
    }
    if (el instanceof HTMLInputElement && el.hasAttribute("data-maturity-enable")) {
      const join = el.closest("[data-maturity-join]");
      if (join) syncMaturityJoin(join);
      return;
    }
    if (el instanceof HTMLInputElement && el.hasAttribute("data-pack-role-enable")) {
      const join = el.closest("[data-pack-role-join]");
      if (join) syncPackRoleJoin(join);
      return;
    }
    if (el instanceof HTMLSelectElement && el.hasAttribute("data-special-kind")) {
      syncSpecialKindSelect(el);
      return;
    }
    if (!(el instanceof HTMLSelectElement) || !el.hasAttribute("data-rate-unit")) {
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
}
