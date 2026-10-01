function syncConfirmSubmit(form) {
  if (!form || !form.classList.contains("js-confirm-submit")) return;
  const check = form.querySelector(".js-confirm-submit-check");
  const btn = form.querySelector('button[type="submit"]');
  if (!check || !btn) return;
  btn.disabled = !check.checked;
}

function syncNotifyEventsExclusive(root, changed) {
  if (!root) return;
  const allBox = root.querySelector("input[data-notify-events-all]");
  const ones = root.querySelectorAll("input[data-notify-events-one]");
  if (!allBox) return;
  if (changed && changed.hasAttribute("data-notify-events-all")) {
    if (allBox.checked) {
      ones.forEach((b) => {
        b.checked = true;
        b.disabled = true;
      });
    } else {
      ones.forEach((b) => {
        b.checked = false;
        b.disabled = false;
      });
    }
    return;
  }
  if (changed && changed.hasAttribute("data-notify-events-one") && changed.checked) {
    allBox.checked = false;
    ones.forEach((b) => {
      b.disabled = false;
    });
    return;
  }
  // Init / after reset: All checked → show specifics checked + disabled.
  if (allBox.checked) {
    ones.forEach((b) => {
      b.checked = true;
      b.disabled = true;
    });
  } else {
    ones.forEach((b) => {
      b.disabled = false;
    });
  }
}

export function bootConfirmNotify() {
  document.body.addEventListener("change", (ev) => {
    const t = ev.target;
    if (!(t instanceof HTMLInputElement)) return;
    if (t.classList.contains("js-confirm-submit-check")) {
      syncConfirmSubmit(t.closest("form"));
      return;
    }
    if (t.matches("input[data-notify-events-all], input[data-notify-events-one]")) {
      syncNotifyEventsExclusive(t.closest("[data-notify-events]"), t);
      return;
    }
    if (t.classList.contains("modal-toggle") && !t.checked) {
      const modal = t.nextElementSibling;
      if (modal && modal.classList.contains("modal")) {
        modal.querySelectorAll("form.js-confirm-submit").forEach((form) => {
          form.reset();
          syncConfirmSubmit(form);
        });
        modal.querySelectorAll("[data-notify-events]").forEach((root) => {
          const form = root.closest("form");
          if (form) form.reset();
          syncNotifyEventsExclusive(root, null);
        });
      }
    }
  });

  document.querySelectorAll("[data-notify-events]").forEach((root) => {
    syncNotifyEventsExclusive(root, null);
  });

  document.querySelectorAll("form.js-confirm-submit").forEach(syncConfirmSubmit);
}
