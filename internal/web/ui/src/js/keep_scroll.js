// Full-page nav that should not jump to top:
// - mark link/form with js-keep-scroll, or
// - POST forms with hidden redirect back to the current pathname (Settings Save, etc.)
// Opt out with js-no-keep-scroll. Live panels use HTMX + dataset restore instead.
const SCROLL_KEY = "creatorr:keep-scroll";

export function saveKeepScroll() {
  sessionStorage.setItem(
    SCROLL_KEY,
    JSON.stringify({ path: location.pathname, y: window.scrollY })
  );
}

function restoreKeepScroll() {
  const raw = sessionStorage.getItem(SCROLL_KEY);
  if (raw == null) return;
  sessionStorage.removeItem(SCROLL_KEY);
  let data;
  try {
    data = JSON.parse(raw);
  } catch {
    return;
  }
  if (!data || data.path !== location.pathname) return;
  const top = Number(data.y);
  if (!Number.isFinite(top)) return;
  const go = () => window.scrollTo(0, top);
  requestAnimationFrame(() => requestAnimationFrame(go));
}

function formRedirectPathname(form) {
  const redir = form.querySelector('input[name="redirect"]');
  if (!redir) return "";
  const raw = String(redir.value || "").trim();
  if (!raw) return "";
  try {
    return new URL(raw, location.origin).pathname;
  } catch {
    return "";
  }
}

/** Path+search for Tasks action redirects; drop flash ok/err so they do not stack. */
export function currentKeepScrollRedirect() {
  const u = new URL(location.href);
  u.searchParams.delete("ok");
  u.searchParams.delete("err");
  const q = u.searchParams.toString();
  return u.pathname + (q ? "?" + q : "");
}

export function syncKeepScrollRedirect(form) {
  if (!(form instanceof HTMLFormElement)) return;
  form.querySelectorAll("input[name='redirect'][data-keep-scroll-redirect]").forEach((el) => {
    el.value = currentKeepScrollRedirect();
  });
}

export function shouldKeepScrollForm(form) {
  if (!(form instanceof HTMLFormElement)) return false;
  if (form.classList.contains("js-no-keep-scroll")) return false;
  if (form.hasAttribute("hx-get") || form.hasAttribute("hx-post")) return false;
  if (form.classList.contains("js-keep-scroll")) return true;
  // Creatorr actions: hidden redirect back to this page → restore after reload.
  const dest = formRedirectPathname(form);
  if (dest !== "" && dest === location.pathname) return true;
  // Tasks page: POST /actions/* always returns to /tasks (full reload).
  if (document.getElementById("tasks-live") && String(form.method || "").toLowerCase() === "post") {
    const action = form.getAttribute("action") || "";
    if (action.startsWith("/actions/")) return true;
  }
  return false;
}

function saveSeriesScroll() {
  saveKeepScroll();
}

export function restoreSeriesScroll() {
  restoreKeepScroll();
}

/** Series detail: Import source link uses #series-videos-live (after keep-scroll restore). */
export function scrollSeriesVideosAnchor() {
  if (location.hash !== "#series-videos-live") return;
  const el = document.getElementById("series-videos-live");
  if (!el) return;
  requestAnimationFrame(() => {
    el.scrollIntoView({ block: "start" });
  });
}
