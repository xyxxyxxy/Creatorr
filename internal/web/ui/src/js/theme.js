const THEME_KEY = "creatorr-theme";

const THEME_LIGHT = "emerald"; // OS light fallback

const THEME_DARK = "dark"; // OS dark fallback

const THEME_GROUPS = {
  dark: ["dark", "synthwave", "forest", "black", "dracula", "coffee", "dim", "sunset", "abyss"],
  light: ["cupcake", "emerald", "corporate", "garden", "fantasy", "autumn"],
  special: ["cyberpunk", "valentine", "halloween", "aqua"],
};

const THEMES = [].concat(THEME_GROUPS.dark, THEME_GROUPS.light, THEME_GROUPS.special);

const LEGACY_THEMES = {
  light: "emerald",
  night: "dark",
  bumblebee: "cupcake",
  retro: "cupcake",
  lofi: "cupcake",
  pastel: "cupcake",
  wireframe: "cupcake",
  luxury: "dracula",
  cmyk: "cupcake",
  business: "corporate",
  acid: "cupcake",
  lemonade: "cupcake",
  winter: "cupcake",
  nord: "dark",
  caramellatte: "cupcake",
  silk: "cupcake",
};

function osTheme() {
  try {
    return matchMedia("(prefers-color-scheme: dark)").matches ? THEME_DARK : THEME_LIGHT;
  } catch (_) {
    return THEME_DARK;
  }
}

function normalizeTheme(t) {
  if (typeof t !== "string") return null;
  if (THEMES.includes(t)) return t;
  if (LEGACY_THEMES[t]) return LEGACY_THEMES[t];
  return null;
}

function isTheme(t) {
  return normalizeTheme(t) != null;
}

function storedTheme() {
  try {
    return normalizeTheme(localStorage.getItem(THEME_KEY));
  } catch (_) {}
  return null;
}

function resolveTheme() {
  return storedTheme() || osTheme();
}

function syncThemeControls(theme) {
  document.querySelectorAll('input[name="theme-picker"]').forEach((el) => {
    el.checked = el.value === theme;
  });
}

function applyTheme(theme, opts) {
  const t = normalizeTheme(theme) || osTheme();
  document.documentElement.setAttribute("data-theme", t);
  syncThemeControls(t);
  if (opts && opts.persist) {
    try {
      localStorage.setItem(THEME_KEY, t);
    } catch (_) {}
  }
  window.dispatchEvent(new CustomEvent("creatorr:theme", { detail: { theme: t } }));
}

function initTheme() {
  applyTheme(resolveTheme(), { persist: false });
  document.querySelectorAll('input[name="theme-picker"]').forEach((el) => {
    el.addEventListener("change", () => {
      if (el.checked) applyTheme(el.value, { persist: true });
    });
  });
  try {
    matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
      if (storedTheme()) return;
      applyTheme(osTheme(), { persist: false });
    });
  } catch (_) {}
}

export function bootTheme() {
  initTheme();
}
