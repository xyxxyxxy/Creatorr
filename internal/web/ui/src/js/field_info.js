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

export function bootFieldInfo() {
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
}
