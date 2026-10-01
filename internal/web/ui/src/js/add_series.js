import { syncQualityProfileGate } from "./controls.js";
import { syncAllScanCronJoins } from "./joins.js";
import { isValidSourceURLClient } from "./source_forms.js";
import { clearControlValidity, clearFormControlValidity, clearPanelValues, isAbsolutePathClient, setControlValidity, setPanelControls, setRootFolderFormErr } from "./validity.js";

function addSeriesURLInvalid(form) {
  if (!form) return false;
  const urlEl = form.querySelector("#add-series-url");
  const raw = String((urlEl && urlEl.value) || "").trim();
  return raw !== "" && !isValidSourceURLClient(raw);
}

export function syncAddSeriesSourceNav(form) {
  const urlEl = form.querySelector("#add-series-url");
  const cont = form.querySelector(".js-add-series-fetch");
  if (!urlEl) return;
  const has = String(urlEl.value || "").trim() !== "";
  const invalid = addSeriesURLInvalid(form);
  const blocked = form.querySelector("[data-add-series-submit]")?.getAttribute("data-blocked") === "1";
  if (cont) cont.disabled = blocked || !has || invalid || cont.dataset.busy === "1";
  if (invalid) {
    setControlValidity(urlEl, "Enter a valid http(s) URL with a host.");
  } else {
    clearControlValidity(urlEl);
  }
}

function setAddSeriesAlert(form, msg) {
  const errEl = form.querySelector(".js-add-series-fetch-err");
  if (!errEl) return;
  const span = errEl.querySelector("span") || errEl;
  span.textContent = String(msg || "").replace(/^conflict:\s*/i, "").trim();
  // Visibility is owned by syncAddSeriesForm (needs step + message).
}

/** Route create/fetch errors to daisyUI validators when the message maps to a field. */
export function setAddSeriesFetchErr(form, msg) {
  if (!form) return;
  clearFormControlValidity(form);
  const text = String(msg || "").replace(/^conflict:\s*/i, "").trim();
  if (!text) {
    setAddSeriesAlert(form, "");
    return;
  }
  const lower = text.toLowerCase();
  let field = null;
  if (/\btitle\b/.test(lower) && /required|already exists|same root/.test(lower)) {
    field = form.querySelector("#add-series-title");
  } else if (/source url|url already|valid http|with a host/.test(lower)) {
    field = form.querySelector("#add-series-url");
  } else if (/\broot\b/.test(lower)) {
    field = form.querySelector('select[name="root_id"]');
  } else if (/quality|profile/.test(lower)) {
    field = form.querySelector("[data-quality-profile-select]") || form.querySelector('select[name="quality_profile_id"]');
  }
  // yt-dlp / prefetch failures stay in the alert (not URL field validators).
  if (field) {
    if (field.id === "add-series-url") {
      form.dataset.addSeriesStep = "source";
    } else {
      form.dataset.addSeriesStep = "series";
    }
    setControlValidity(field, text);
    setAddSeriesAlert(form, "");
    try {
      field.focus();
      if (typeof field.select === "function") field.select();
    } catch (_) {}
    return;
  }
  setAddSeriesAlert(form, text);
}

/** Snapshot form as urlencoded body (matches server ParseForm / tests). Skips disabled controls. */
function serializeAddSeriesForm(form) {
  const params = new URLSearchParams();
  form.querySelectorAll("input, select, textarea").forEach((el) => {
    if (!el.name || el.disabled || el.type === "submit" || el.type === "button" || el.type === "file" || el.type === "reset") {
      return;
    }
    if (el.type === "checkbox") {
      if (el.checked) params.append(el.name, el.value || "1");
      return;
    }
    if (el.type === "radio") {
      if (!el.checked) return;
      params.append(el.name, el.value);
      return;
    }
    params.append(el.name, el.value);
  });
  // Title must always win over any earlier empty same-name control.
  const titleEl = form.querySelector("#add-series-title") || form.querySelector('input[name="title"]');
  if (titleEl) params.set("title", String(titleEl.value || ""));
  // Manual path: drop empty source_url so the handler takes the manual branch cleanly.
  if ((form.dataset.addSeriesMode || "") !== "url") {
    const su = String(params.get("source_url") || "").trim();
    if (!su) params.delete("source_url");
  }
  return params;
}

// Steps: "" (choice) | "source" | "fetching" | "series". Mode: "url" | "manual".
export function syncAddSeriesForm(form) {
  if (!form || !form.classList.contains("js-add-series-form")) return;
  const mode = form.dataset.addSeriesMode || "";
  const step = form.dataset.addSeriesStep || "";
  const choice = form.querySelector("[data-add-series-choice]");
  const stepsBar = form.querySelector("[data-add-series-steps]");
  const source = form.querySelector('[data-add-series-step="source"]');
  const fetching = form.querySelector('[data-add-series-step="fetching"]');
  const series = form.querySelector('[data-add-series-step="series"]');
  const submit = form.querySelector("[data-add-series-submit]");
  const titleEl = form.querySelector("#add-series-title");
  const errEl = form.querySelector(".js-add-series-fetch-err");
  const titleWrap = titleEl && (titleEl.closest(".flex.items-center") || titleEl.closest(".flex"));
  const titleInfo = titleWrap
    ? titleWrap.querySelector(".tooltip-content p")
    : form.querySelector(".js-add-series-title-help");


  if (choice) choice.classList.toggle("hidden", step !== "");
  if (stepsBar) {
    const showSteps = mode === "url" && step !== "";
    stepsBar.classList.toggle("hidden", !showSteps);
    const order = ["source", "fetching", "series"];
    const cur = order.indexOf(step);
    stepsBar.querySelectorAll("[data-add-series-step-ind]").forEach((li) => {
      const i = order.indexOf(li.getAttribute("data-add-series-step-ind"));
      li.classList.toggle("step-primary", showSteps && i >= 0 && i <= cur);
    });
  }
  // Keep alert under steps; show on source or series when it has text (title conflicts stay editable).
  if (errEl) {
    const hasMsg = !!(errEl.querySelector("span") || errEl).textContent.trim();
    errEl.classList.toggle("hidden", !hasMsg || step === "" || step === "fetching");
  }
  if (source) {
    // Keep source fields enabled on later URL steps so they still submit; only hide.
    source.classList.toggle("hidden", step !== "source");
    source.querySelectorAll("input, select, textarea").forEach((el) => {
      if (el.type === "hidden") return;
      el.disabled = mode !== "url";
    });
  }
  if (fetching) fetching.classList.toggle("hidden", step !== "fetching");
  if (series) {
    const showSeries = step === "series";
    series.classList.toggle("hidden", !showSeries);
    // Keep series fields enabled for the whole URL/manual flow (hide-only when off-step).
    // Soft-disable made title look filled while submit omitted it (browser skips disabled).
    setPanelControls(series, mode === "manual" || mode === "url");
    syncQualityProfileGate(form);
  }
  if (titleInfo) {
    titleInfo.textContent = mode === "manual"
      ? "Display name for this series. Add sources after creation from the series page."
      : "Filled from the channel/playlist when available. Edit if needed.";
  }
  if (titleEl) titleEl.required = step === "series";
  const cont = form.querySelector("[data-add-series-continue]");
  if (cont) cont.classList.toggle("hidden", step !== "source");
  if (submit) {
    const blocked = submit.getAttribute("data-blocked") === "1";
    const onSeries = step === "series";
    submit.classList.toggle("hidden", !onSeries);
    submit.disabled = blocked || !onSeries;
  }
  if (step === "source") syncAddSeriesSourceNav(form);
  syncImportAddSeriesIgnored(form);
}

/** On 'Import', force discovered status to 'Ignored' + disable join (tooltip); hidden keeps value. */
function syncImportAddSeriesIgnored(form) {
  if (!form) return;
  const join = form.querySelector("[data-index-as-ignored-join]");
  if (!join) return;
  const radios = join.querySelectorAll('input[type="radio"][name="index_as_ignored"]');
  if (!radios.length) return;
  const lock =
    form.dataset.importMatchLock === "1" ||
    location.pathname === "/import" ||
    location.pathname.endsWith("/import");
  const tip =
    "Required on 'Import' so new videos are indexed for matching without downloading.";
  let tipWrap = join.parentElement;
  if (tipWrap && !tipWrap.classList.contains("tooltip")) tipWrap = null;
  let hidden = form.querySelector('input[type="hidden"][name="index_as_ignored"][data-import-force]');

  if (lock) {
    radios.forEach((r) => {
      r.checked = r.value === "1";
      r.disabled = true;
    });
    if (!hidden) {
      hidden = document.createElement("input");
      hidden.type = "hidden";
      hidden.name = "index_as_ignored";
      hidden.value = "1";
      hidden.dataset.importForce = "1";
      join.insertAdjacentElement("beforebegin", hidden);
    } else {
      hidden.value = "1";
    }
    join.classList.add("opacity-60", "pointer-events-none");
    if (!tipWrap) {
      tipWrap = document.createElement("span");
      tipWrap.className = "tooltip tooltip-top block w-full";
      join.parentNode.insertBefore(tipWrap, join);
      tipWrap.appendChild(join);
    }
    tipWrap.setAttribute("data-tip", tip);
    return;
  }

  if (hidden) hidden.remove();
  radios.forEach((r) => {
    r.disabled = false;
  });
  join.classList.remove("opacity-60", "pointer-events-none");
  if (tipWrap) {
    tipWrap.removeAttribute("data-tip");
    const parent = tipWrap.parentNode;
    if (parent) {
      parent.insertBefore(join, tipWrap);
      tipWrap.remove();
    }
  }
}

async function pollAddSeriesPrefetch(taskId) {
  const deadline = Date.now() + 120000;
  while (Date.now() < deadline) {
    const res = await fetch("/actions/add-series-prefetch/" + encodeURIComponent(taskId), {
      headers: { Accept: "application/json" },
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      throw new Error(data.error || ("Status failed (" + res.status + ")"));
    }
    const st = data.status;
    if (st === "pending" || st === "running") {
      await new Promise((r) => setTimeout(r, 500));
      continue;
    }
    return data;
  }
  throw new Error("Fetch timed out");
}

export function bootAddSeries() {
  // AJAX root folder save: keep modal open and invalidate Path (etc.) on error.
  document.body.addEventListener("submit", async (ev) => {
    const form = ev.target.closest("form.js-root-folder-form");
    if (!form) return;
    ev.preventDefault();
    clearFormControlValidity(form);
    const pathEl = form.querySelector('input[name="path"]');
    const pathVal = String((pathEl && pathEl.value) || "").trim();
    if (!pathVal) {
      setControlValidity(pathEl, "path required");
      try {
        pathEl && pathEl.focus();
      } catch (_) {}
      return;
    }
    if (!isAbsolutePathClient(pathVal)) {
      setControlValidity(pathEl, "path must be absolute");
      try {
        pathEl.focus();
        if (typeof pathEl.select === "function") pathEl.select();
      } catch (_) {}
      return;
    }
    const submitBtn = form.querySelector('button[type="submit"]');
    if (submitBtn) submitBtn.disabled = true;
    const body = new URLSearchParams();
    form.querySelectorAll("input, select, textarea").forEach((el) => {
      if (!el.name || el.disabled || el.type === "submit" || el.type === "button") return;
      if (el.type === "checkbox" || el.type === "radio") {
        if (el.checked) body.append(el.name, el.value || "1");
        return;
      }
      body.append(el.name, el.value);
    });
    try {
      const res = await fetch(form.getAttribute("action") || "/actions/add-root", {
        method: "POST",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
        },
        body: body.toString(),
        credentials: "same-origin",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setRootFolderFormErr(form, data);
        return;
      }
      const loc = (data && data.redirect) || "/settings/library";
      location.assign(loc);
    } catch (e) {
      setRootFolderFormErr(form, { error: e && e.message ? e.message : "Save failed" });
    } finally {
      if (submitBtn) submitBtn.disabled = false;
    }
  });

  window.setAddSeriesFetchErr = setAddSeriesFetchErr;

  // AJAX create: keep modal + draft on error so the operator can fix the title.
  document.body.addEventListener("submit", async (ev) => {
    const form = ev.target.closest("form.js-add-series-form");
    if (!form) return;
    ev.preventDefault();
    const submitBtn = form.querySelector("[data-add-series-submit]");
    if (submitBtn) submitBtn.disabled = true;
    setAddSeriesFetchErr(form, "");
    // Enable series controls before read so soft-disable cannot drop title.
    const seriesPanel = form.querySelector('[data-add-series-step="series"]');
    if (seriesPanel) setPanelControls(seriesPanel, true);
    syncQualityProfileGate(form);
    const titleEl = form.querySelector("#add-series-title") || form.querySelector('input[name="title"]');
    const titleVal = String((titleEl && titleEl.value) || "").trim();
    if (!titleVal) {
      const mode = form.dataset.addSeriesMode || "";
      setAddSeriesFetchErr(
        form,
        mode === "manual"
          ? "title is required when creating manually"
          : "title is required - fetch metadata again or enter a title"
      );
      form.dataset.addSeriesStep = "series";
      syncAddSeriesForm(form);
      return;
    }
    const body = serializeAddSeriesForm(form);
    // Belt: ensure title is present after snapshot (defends against empty append races).
    body.set("title", titleVal);
    syncAddSeriesForm(form);
    try {
      const res = await fetch("/actions/add-series", {
        method: "POST",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
        },
        body: body.toString(),
        credentials: "same-origin",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        const msg = (data && (data.error || data.message)) || ("Create series failed (" + res.status + ")");
        setAddSeriesFetchErr(form, msg);
        if (!form.dataset.addSeriesStep || form.dataset.addSeriesStep === "fetching") {
          form.dataset.addSeriesStep = "series";
        }
        syncAddSeriesForm(form);
        return;
      }
      if (!data || data.id == null) {
        setAddSeriesFetchErr(form, "Create series failed: invalid response");
        form.dataset.addSeriesStep = "series";
        syncAddSeriesForm(form);
        return;
      }
      if (form.dataset.importMatchLock === "1") {
        form.dispatchEvent(new CustomEvent("creatorr:series-created", {
          bubbles: true,
          detail: { id: data.id, title: data.title || "", warning: data.warning || "" },
        }));
        return;
      }
      if (data.warning) {
        location.assign("/series/" + data.id + "?err=" + encodeURIComponent(data.warning));
        return;
      }
      location.assign("/series/" + data.id);
    } catch (e) {
      setAddSeriesFetchErr(form, e && e.message ? e.message : "Create series failed");
      form.dataset.addSeriesStep = "series";
      syncAddSeriesForm(form);
    } finally {
      syncAddSeriesForm(form);
    }
  });

  window.syncAddSeriesForm = syncAddSeriesForm;

  document.body.addEventListener("click", (ev) => {
    const pick = ev.target.closest(".js-add-series-pick");
    if (!pick) return;
    const form = pick.closest("form.js-add-series-form");
    if (!form) return;
    const mode = pick.getAttribute("data-mode");
    if (!mode) return;
    const source = form.querySelector('[data-add-series-step="source"]');
    const series = form.querySelector('[data-add-series-step="series"]');
    clearPanelValues(source);
    clearPanelValues(series);
    const tok = form.querySelector("#add-series-draft-token");
    if (tok) tok.value = "";
    setAddSeriesFetchErr(form, "");
    form.dataset.addSeriesMode = mode;
    form.dataset.addSeriesStep = mode === "url" ? "source" : "series";
    syncAllScanCronJoins(form);
    syncAddSeriesForm(form);
    if (mode === "url") form.querySelector("#add-series-url")?.focus();
    else form.querySelector("#add-series-title")?.focus();
  });

  document.body.addEventListener("click", async (ev) => {
    const btn = ev.target.closest(".js-add-series-fetch");
    if (!btn) return;
    const form = btn.closest("form.js-add-series-form");
    if (!form) return;
    const urlEl = form.querySelector("#add-series-url");
    if (!urlEl || !String(urlEl.value || "").trim() || addSeriesURLInvalid(form)) {
      syncAddSeriesSourceNav(form);
      return;
    }
    setAddSeriesFetchErr(form, "");
    form.dataset.addSeriesStep = "fetching";
    syncAddSeriesForm(form);
    try {
      const body = new URLSearchParams();
      body.set("source_url", String(urlEl.value || "").trim());
      const res = await fetch("/actions/fetch-add-series", {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: body.toString(),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        throw new Error(data.error || ("Fetch failed (" + res.status + ")"));
      }
      const tid = data.task_id;
      const draftToken = data.draft_token || "";
      if (!tid) {
        throw new Error("missing task id");
      }
      const result = await pollAddSeriesPrefetch(tid);
      if (result.error) {
        throw new Error(result.error);
      }
      const titleEl = form.querySelector("#add-series-title");
      if (titleEl) titleEl.value = result.title || "";
      const tok = form.querySelector("#add-series-draft-token");
      if (tok) tok.value = draftToken || result.draft_token || "";
      form.dataset.addSeriesStep = "series";
      syncAddSeriesForm(form);
      titleEl?.focus();
    } catch (e) {
      form.dataset.addSeriesStep = "source";
      setAddSeriesFetchErr(form, e && e.message ? e.message : "Fetch failed");
      syncAddSeriesForm(form);
      syncAddSeriesSourceNav(form);
    }
  });

  // Filter bar × clears query params for that field and reloads.
  document.body.addEventListener("click", (ev) => {
    const btn = ev.target.closest("[data-filter-clear]");
    if (!btn) return;
    ev.preventDefault();
    const name = btn.getAttribute("data-filter-clear");
    if (!name) return;
    const u = new URL(location.href);
    u.searchParams.delete(name);
    u.searchParams.delete("page");
    location.assign(u.pathname + u.search);
  });
}
