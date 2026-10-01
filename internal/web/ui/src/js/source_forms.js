import { setArtPreviewVisible } from "./form_reset.js";
import { syncCredentialsPasswordValidity } from "./joins.js";
import { clearControlValidity, setControlValidity } from "./validity.js";

// Approximate Go normalizeSourceURL: trim, lower host, strip leading www.
function normalizeSourceURLClient(raw) {
  const s = String(raw || "").trim();
  if (!s) return "";
  try {
    const u = new URL(s);
    let host = (u.hostname || "").toLowerCase();
    if (host.startsWith("www.")) host = host.slice(4);
    u.hostname = host;
    return u.toString();
  } catch (_) {
    return s;
  }
}

// Match library.ValidateSourceURL: http(s) with a host.
export function isValidSourceURLClient(raw) {
  const s = String(raw || "").trim();
  if (!s) return false;
  try {
    const u = new URL(s);
    const scheme = String(u.protocol || "").toLowerCase();
    if (scheme !== "http:" && scheme !== "https:") return false;
    return String(u.hostname || "").trim() !== "";
  } catch (_) {
    return false;
  }
}

function existingSourceURLs() {
  const el = document.getElementById("series-source-urls");
  if (!el) return [];
  try {
    const v = JSON.parse(el.textContent || "[]");
    return Array.isArray(v) ? v.map(normalizeSourceURLClient) : [];
  } catch (_) {
    return [];
  }
}

export function syncSourceURLForm(form) {
  if (!form || !form.classList.contains("js-source-url-form")) return;
  const input = form.querySelector(".js-source-url");
  const submit = form.querySelector(".js-source-url-submit");
  if (!input || !submit) return;
  const raw = String(input.value || "").trim();
  const cur = normalizeSourceURLClient(form.getAttribute("data-current-url") || "");
  const typed = normalizeSourceURLClient(input.value);
  const invalid = raw !== "" && !isValidSourceURLClient(raw);
  const existing = existingSourceURLs();
  const clash = !invalid && typed !== "" && existing.some((u) => u === typed && u !== cur);
  submit.disabled = clash || invalid;
  if (invalid) {
    setControlValidity(input, "Enter a valid http(s) URL with a host.");
  } else if (clash) {
    setControlValidity(input, "This URL is already a source on this series.");
  } else {
    clearControlValidity(input);
  }
}

// Match settings.ValidateOverrideDomain / NormalizeDomain (client-side for Add override modal).
function normalizeOverrideDomainClient(raw) {
  let s = String(raw || "").trim().toLowerCase();
  if (s.startsWith("www.")) s = s.slice(4);
  return s;
}

function overrideDomainValidationMessage(raw) {
  const domain = normalizeOverrideDomainClient(raw);
  if (!domain) return "Domain is required.";
  if (domain === "default" || domain === "unknown" || domain === "system") {
    return "Reserved domain name.";
  }
  if (/[:/\\@,#?&!\"'$;%^*()\[\]{}|=+ ]/.test(domain) || domain.includes(",")) {
    return "Enter a valid hostname (e.g. example.com).";
  }
  if (!domain.includes(".")) {
    return "Enter a valid hostname (e.g. example.com).";
  }
  if (domain.length > 253) {
    return "Enter a valid hostname (e.g. example.com).";
  }
  const labels = domain.split(".");
  for (const lab of labels) {
    if (!lab || lab.length > 63) {
      return "Enter a valid hostname (e.g. example.com).";
    }
    if (lab[0] === "-" || lab[lab.length - 1] === "-") {
      return "Enter a valid hostname (e.g. example.com).";
    }
    if (!/^[a-z0-9-]+$/.test(lab)) {
      return "Enter a valid hostname (e.g. example.com).";
    }
  }
  return "";
}

function syncDomainOverrideForm(form) {
  if (!form || !form.classList.contains("js-domain-override-form")) return;
  const input = form.querySelector(".js-domain-override-domain");
  if (!input) return;
  const msg = overrideDomainValidationMessage(input.value);
  if (msg) setControlValidity(input, msg);
  else clearControlValidity(input);
  return msg;
}

/** Flare XOR non-empty jar while editing (server also rejects both-set). daisyUI tips only (never native title). */
function syncDomainFlareCookieExclusive(form) {
  if (!form || !form.classList.contains("js-domain-override-form")) return;
  const cookies = form.querySelector(".js-domain-cookies");
  const flare = form.querySelector(".js-domain-flare");
  if (!cookies || !flare) return;
  const hasCookies = String(cookies.value || "").trim().length > 0;
  const flareOn = !!flare.checked;
  const flareUrlOk = form.getAttribute("data-flare-configured") === "1";
  const cookiesLockTip =
    form.getAttribute("data-cookies-lock-tip") ||
    "Turn off Use FlareSolverr first. Flare and account cookies cannot both be set: Flare merges anonymous cookies over the jar and would overwrite session cookies.";
  const flareCookieLockTip =
    form.getAttribute("data-flare-cookie-lock-tip") ||
    "Clear the cookie jar first. FlareSolverr and account cookies cannot both be set: Flare merges anonymous cookies over the jar and would overwrite session cookies.";

  function setCookiesLocked(locked) {
    const tipHost = cookies.closest(".js-domain-cookies-tip") || cookies;
    cookies.disabled = !!locked;
    cookies.readOnly = false;
    if (locked) {
      cookies.classList.add("pointer-events-none", "opacity-60");
      tipHost.classList.add("tooltip", "tooltip-top");
      tipHost.setAttribute("data-tip", cookiesLockTip);
    } else {
      cookies.classList.remove("pointer-events-none", "opacity-60");
      tipHost.classList.remove("tooltip", "tooltip-top");
      tipHost.removeAttribute("data-tip");
    }
  }

  function setFlareCookieLocked(locked) {
    // URL unset: server Disabled + tip; do not rewrite that wrap.
    if (!flareUrlOk) return;
    const label = flare.closest("label");
    const labelText = label && label.querySelector(".label-text");
    let tipHost = flare.closest(".js-domain-flare-tip");
    if (locked) {
      flare.disabled = true;
      if (labelText) labelText.classList.add("opacity-60");
      if (label) {
        label.classList.remove("cursor-pointer");
        label.classList.add("cursor-not-allowed");
      }
      if (!tipHost && label && label.parentNode) {
        tipHost = document.createElement("span");
        tipHost.className = "tooltip tooltip-top inline-flex w-fit max-w-full js-domain-flare-tip";
        label.parentNode.insertBefore(tipHost, label);
        tipHost.appendChild(label);
      }
      if (tipHost) {
        tipHost.classList.add("tooltip", "tooltip-top");
        tipHost.setAttribute("data-tip", flareCookieLockTip);
      }
    } else {
      flare.disabled = false;
      if (labelText) labelText.classList.remove("opacity-60");
      if (label) {
        label.classList.add("cursor-pointer");
        label.classList.remove("cursor-not-allowed");
      }
      if (tipHost) {
        tipHost.removeAttribute("data-tip");
        tipHost.classList.remove("tooltip", "tooltip-top");
        const parent = tipHost.parentNode;
        if (parent) {
          while (tipHost.firstChild) parent.insertBefore(tipHost.firstChild, tipHost);
          parent.removeChild(tipHost);
        }
      }
    }
  }

  // Legacy both-set: keep Flare editable; lock jar so Save drops cookies (disabled omit POST).
  if (hasCookies && flareOn) {
    setCookiesLocked(true);
    setFlareCookieLocked(false);
    return;
  }
  if (hasCookies) {
    setCookiesLocked(false);
    flare.checked = false;
    setFlareCookieLocked(true);
    return;
  }
  if (flareOn) {
    setCookiesLocked(true);
    setFlareCookieLocked(false);
    return;
  }
  setCookiesLocked(false);
  setFlareCookieLocked(false);
}

function syncAllDomainFlareCookieExclusive() {
  document.querySelectorAll("form.js-domain-override-form").forEach(syncDomainFlareCookieExclusive);
}

function utf8ByteLength(s) {
  try {
    return new TextEncoder().encode(String(s || "")).length;
  } catch (_) {
    return String(s || "").length;
  }
}

/** Client rules for Settings → General change-credentials modal (matches auth.ValidatePassword when set). */
function syncChangeCredentialsForm(form) {
  if (!form || !form.classList.contains("js-change-credentials-form")) return "";
  const user = form.querySelector(".js-auth-username");
  const pass = form.querySelector(".js-auth-password");
  const confirm = form.querySelector(".js-auth-password-confirm");
  if (user) clearControlValidity(user);
  if (pass) clearControlValidity(pass);
  if (confirm) clearControlValidity(confirm);
  if (user && !String(user.value || "").trim()) {
    setControlValidity(user, "Username is required");
    return "username";
  }
  const p = pass ? String(pass.value || "") : "";
  const c = confirm ? String(confirm.value || "") : "";
  if (!p && !c) return "";
  if (Array.from(p).length < 4) {
    setControlValidity(pass, "Password must be at least 4 characters");
    return "password";
  }
  if (utf8ByteLength(p) > 72) {
    setControlValidity(pass, "Password must be at most 72 bytes");
    return "password";
  }
  if (p !== c) {
    setControlValidity(confirm, "Passwords do not match");
    return "confirm";
  }
  return "";
}

export function bootSourceForms() {
  window.isValidSourceURLClient = isValidSourceURLClient;

  document.body.addEventListener("input", (ev) => {
    const input = ev.target.closest(".js-source-url");
    if (!input) return;
    syncSourceURLForm(input.closest("form"));
  });

  document.body.addEventListener("submit", (ev) => {
    const form = ev.target.closest(".js-source-url-form");
    if (!form) return;
    syncSourceURLForm(form);
    const submit = form.querySelector(".js-source-url-submit");
    if (submit && submit.disabled) ev.preventDefault();
  });

  document.body.addEventListener("input", (ev) => {
    const input = ev.target.closest(".js-domain-override-domain");
    if (input) syncDomainOverrideForm(input.closest("form"));
    const form = ev.target.closest(".js-domain-override-form");
    if (form && (ev.target.closest(".js-domain-cookies") || ev.target.closest(".js-domain-flare"))) {
      syncDomainFlareCookieExclusive(form);
    }
  });

  document.body.addEventListener("change", (ev) => {
    const t = ev.target;
    if (t && t.classList && t.classList.contains("modal-toggle") && t.checked) {
      const modal = t.nextElementSibling;
      if (modal && modal.classList.contains("modal")) {
        modal.querySelectorAll("form.js-domain-override-form").forEach(syncDomainFlareCookieExclusive);
      }
    }
    const form = ev.target.closest(".js-domain-override-form");
    if (form && (ev.target.closest(".js-domain-cookies") || ev.target.closest(".js-domain-flare"))) {
      syncDomainFlareCookieExclusive(form);
    }
  });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", syncAllDomainFlareCookieExclusive);
  } else {
    syncAllDomainFlareCookieExclusive();
  }

  document.body.addEventListener("submit", (ev) => {
    const form = ev.target.closest(".js-domain-override-form");
    if (!form) return;
    syncDomainFlareCookieExclusive(form);
    const domainMsg = syncDomainOverrideForm(form);
    if (domainMsg) {
      ev.preventDefault();
      const input = form.querySelector(".js-domain-override-domain");
      try {
        if (input) {
          input.focus();
          if (typeof input.select === "function") input.select();
        }
      } catch (_) {}
      return;
    }
    const cookies = form.querySelector(".js-domain-cookies");
    const flare = form.querySelector(".js-domain-flare");
    // Disabled jar is omitted from POST (clears cookies); only block when both editable.
    if (cookies && flare && !cookies.disabled && String(cookies.value || "").trim() && flare.checked && !flare.disabled) {
      ev.preventDefault();
      setControlValidity(
        cookies,
        "Cannot enable Use FlareSolverr with a non-empty cookie jar: Flare merges anonymous cookies over the jar and would overwrite session cookies. Clear the jar or turn Flare off."
      );
      try {
        cookies.focus();
      } catch (_) {}
      return;
    }
    const credMsg = syncCredentialsPasswordValidity(form);
    if (credMsg) {
      ev.preventDefault();
      const pass = form.querySelector('.js-credentials-override input[name="password"]');
      try {
        if (pass && !pass.disabled) pass.focus();
      } catch (_) {}
    }
  });

  document.body.addEventListener("input", (ev) => {
    const form = ev.target.closest(".js-change-credentials-form");
    if (!form) return;
    if (
      !ev.target.closest(".js-auth-username") &&
      !ev.target.closest(".js-auth-password") &&
      !ev.target.closest(".js-auth-password-confirm")
    ) {
      return;
    }
    syncChangeCredentialsForm(form);
  });

  document.body.addEventListener("submit", (ev) => {
    const form = ev.target.closest(".js-change-credentials-form");
    if (!form) return;
    const which = syncChangeCredentialsForm(form);
    if (!which) return;
    ev.preventDefault();
    const sel =
      which === "username"
        ? ".js-auth-username"
        : which === "confirm"
          ? ".js-auth-password-confirm"
          : ".js-auth-password";
    const el = form.querySelector(sel);
    try {
      if (el) el.focus();
    } catch (_) {}
  });

  document.body.addEventListener("change", (ev) => {
    const input = ev.target.closest("input[data-art-file]");
    if (!input || input.type !== "file") return;
    const slot = input.closest("[data-art-slot]");
    if (!slot) return;
    const img = slot.querySelector("[data-art-preview]");
    if (!img) return;
    const pref = slot.querySelector("input[data-art-prefetch]");
    const clear = slot.querySelector("input[data-art-clear]");
    const prev = img.dataset.objectUrl;
    if (prev) {
      URL.revokeObjectURL(prev);
      delete img.dataset.objectUrl;
    }
    const file = input.files && input.files[0];
    if (!file || !String(file.type || "").startsWith("image/")) {
      if (pref) pref.disabled = false;
      if (clear && clear.value === "1") {
        img.removeAttribute("src");
        setArtPreviewVisible(slot, false);
        return;
      }
      const orig = img.dataset.origSrc || "";
      if (orig) {
        img.src = orig;
        setArtPreviewVisible(slot, true);
      } else {
        img.removeAttribute("src");
        setArtPreviewVisible(slot, false);
      }
      return;
    }
    // Do not submit stale prefetch path when a new file was chosen.
    if (pref) pref.disabled = true;
    if (clear) clear.value = "";
    const url = URL.createObjectURL(file);
    img.dataset.objectUrl = url;
    img.src = url;
    setArtPreviewVisible(slot, true);
  });
}
