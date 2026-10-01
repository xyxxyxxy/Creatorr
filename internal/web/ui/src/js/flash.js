export function scheduleFlashToasts() {
  document.querySelectorAll("[data-flash-toast]").forEach((el) => {
    if (el.dataset.flashScheduled) return;
    el.dataset.flashScheduled = "1";
    window.setTimeout(() => {
      el.remove();
    }, 3500);
  });
}

export async function copyInputValue(inputId) {
  const el = document.getElementById(inputId);
  const text =
    el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement
      ? el.value
      : el instanceof HTMLElement
        ? el.textContent || ""
        : "";
  try {
    if (!navigator.clipboard || typeof navigator.clipboard.writeText !== "function") {
      throw new Error("clipboard unavailable");
    }
    await navigator.clipboard.writeText(String(text));
    window.showFlashToast("Copied.");
  } catch (_) {
    window.showFlashToast("Copying failed.", { error: true });
  }
}

export function initFlashToasts() {
  const el = document.querySelector("[data-flash-toast]");
  if (!el) return;
  try {
    const u = new URL(location.href);
    let dirty = false;
    for (const k of ["ok", "err", "rewrote", "failed"]) {
      if (u.searchParams.has(k)) {
        u.searchParams.delete(k);
        dirty = true;
      }
    }
    if (dirty) {
      const q = u.searchParams.toString();
      history.replaceState({}, "", u.pathname + (q ? "?" + q : "") + u.hash);
    }
  } catch (_) {}
  scheduleFlashToasts();
}

export function bootFlash() {
  /** Client-side flash toast (same markup as partials/flash_toast). opts: { error, warning }. */
  window.showFlashToast = function (message, opts) {
    opts = opts || {};
    const toast = document.createElement("div");
    toast.className = "toast toast-top toast-end z-[1300]";
    toast.setAttribute("data-flash-toast", "");
    const alert = document.createElement("div");
    alert.setAttribute("role", "status");
    let kind = "alert-success";
    if (opts.error) kind = "alert-error";
    else if (opts.warning) kind = "alert-warning";
    alert.className = "alert " + kind + " shadow-lg";
    const span = document.createElement("span");
    span.textContent = String(message || "");
    alert.appendChild(span);
    toast.appendChild(alert);
    document.body.appendChild(toast);
    scheduleFlashToasts();
  };
}
