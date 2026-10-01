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

export function initRangeOutputs() {
  document.querySelectorAll('input[type="range"][data-range-output]').forEach(syncRangeOutput);
}

export function initSponsorBlockExclusive() {
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

export function initSponsorBlockReencodeGate() {
  document.body.addEventListener("change", (ev) => {
    const el = ev.target;
    if (!el || !el.matches || !el.matches("input[data-sb-reencode]")) return;
    syncSponsorBlockCardsGate(el.closest("[data-sb-profile]"));
  });
  document.querySelectorAll("[data-sb-profile]").forEach(syncSponsorBlockCardsGate);
}

const AUDIO_QUALITY_PROFILE_TIP =
  "Audio always uses the best available quality.";

/** Disable the quality profile select when delivery_mode=audio; hidden input carries the value instead.
 *  Bulk Edit uses a delivery select + No-change quality; never mark required. */

export function syncQualityProfileGate(form) {
  if (!form) return;
  const fieldset = form.querySelector("[data-quality-profile-fieldset]");
  if (!fieldset) return;
  const bulk = fieldset.hasAttribute("data-bulk");
  const radio = form.querySelector('input[name="delivery_mode"]:checked');
  const deliverySelect = form.querySelector('select[name="delivery_mode"]');
  let isAudio = false;
  if (radio) {
    isAudio = radio.value === "audio";
  } else if (deliverySelect) {
    isAudio = deliverySelect.value === "audio";
  }
  const select = fieldset.querySelector("[data-quality-profile-select]");
  const hidden = fieldset.querySelector("[data-quality-profile-hidden]");
  const tip = fieldset.querySelector("[data-quality-profile-tip]");
  if (select) {
    select.disabled = isAudio || !select.options.length;
    if (bulk) {
      select.required = false;
      select.classList.remove("validator");
    } else {
      select.required = !isAudio;
      select.classList.toggle("validator", !isAudio);
    }
    if (isAudio) {
      select.removeAttribute("name");
    } else {
      select.setAttribute("name", "quality_profile_id");
    }
  }
  if (hidden) {
    if (select && select.value) hidden.value = select.value;
    if (isAudio) {
      hidden.disabled = false;
      hidden.setAttribute("name", "quality_profile_id");
    } else {
      hidden.disabled = true;
      hidden.removeAttribute("name");
    }
  }
  if (tip) {
    if (isAudio) {
      tip.classList.add("tooltip", "tooltip-top");
      tip.setAttribute("data-tip", AUDIO_QUALITY_PROFILE_TIP);
    } else {
      tip.classList.remove("tooltip", "tooltip-top");
      tip.removeAttribute("data-tip");
    }
  }
}

export function initQualityProfileGate() {
  document.body.addEventListener("change", (ev) => {
    const el = ev.target;
    if (!el || !el.matches || !el.matches('input[name="delivery_mode"], select[name="delivery_mode"]')) {
      return;
    }
    syncQualityProfileGate(el.closest("form"));
  });
  document.querySelectorAll("[data-quality-profile-fieldset]").forEach((fieldset) => {
    syncQualityProfileGate(fieldset.closest("form"));
  });
}

export function bootControls() {
  document.body.addEventListener("input", (ev) => {
    syncRangeOutput(ev.target);
  });
}
