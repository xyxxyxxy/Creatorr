/**
 * Shared library picker (Import single + Maintenance multi).
 * Modal markup: partials/library_picker_modal.html
 *
 * window.openLibraryPicker(opts)
 *   mode: 'single' | 'multi'
 *   Single (Import-driven): kind, subtitle, draft, onPick, onCreateSeries, onCreateVideo,
 *     allowExisting, getAllowExisting, setAllowExisting, series, videos, videoFilter,
 *     createSeries, createVideo, seriesOnly
 *   Multi: initialSeriesIds, initialVideoIds, packedOnly (default true), onConfirm({seriesIds, videoIds})
 */
(function () {
  const CREATE = "__create__";
  let ctx = null;
  let catalog = { series: [], videos: [] };
  let catalogPromise = null;

  function $(id) {
    return document.getElementById(id);
  }

  function escapeHtml(s) {
    return String(s || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function lucideRefresh(root) {
    if (typeof lucide !== "undefined" && lucide.createIcons) {
      lucide.createIcons({ root: root || document });
    }
  }

  function seriesPosterHTML(s) {
    const fallback =
      '<div class="bg-base-200 size-10 rounded-full flex items-center justify-center" aria-hidden="true"><i data-lucide="tv" class="size-5 opacity-40"></i></div>';
    if (!s || !s.poster_url) return fallback;
    return (
      '<img class="size-10 rounded-full object-cover" src="' +
      escapeHtml(s.poster_url) +
      '" alt="" width="40" height="40" loading="lazy" onerror="this.onerror=null;this.classList.add(\'hidden\');this.nextElementSibling.classList.remove(\'hidden\')" /><div class="hidden bg-base-200 size-10 rounded-full flex items-center justify-center" aria-hidden="true"><i data-lucide="tv" class="size-5 opacity-40"></i></div>'
    );
  }

  function videoThumbURL(v) {
    return v && v.thumb_url ? v.thumb_url : "";
  }

  async function ensureCatalog() {
    if (catalog.series.length || catalog.videos.length) return catalog;
    if (catalogPromise) return catalogPromise;
    catalogPromise = fetch("/api/import/picker", {
      headers: { Accept: "application/json" },
      credentials: "same-origin",
    })
      .then((res) => {
        if (!res.ok) throw new Error("picker " + res.status);
        return res.json();
      })
      .then((data) => {
        catalog = {
          series: Array.isArray(data.series) ? data.series : [],
          videos: Array.isArray(data.videos) ? data.videos : [],
        };
        return catalog;
      })
      .finally(() => {
        catalogPromise = null;
      });
    return catalogPromise;
  }

  function setCatalog(series, videos) {
    catalog = {
      series: Array.isArray(series) ? series : [],
      videos: Array.isArray(videos) ? videos : [],
    };
  }

  function filterSeries(q) {
    const needle = String(q || "")
      .trim()
      .toLowerCase();
    const list = ctx && ctx.series ? ctx.series : catalog.series;
    if (!needle) return list.slice();
    return list.filter((s) => String(s.title || "").toLowerCase().includes(needle));
  }

  function filterVideos(seriesId, q, packedOnly) {
    const needle = String(q || "")
      .trim()
      .toLowerCase();
    let list = ctx && ctx.videos ? ctx.videos : catalog.videos;
    if (packedOnly) list = list.filter((v) => !!v.has_media);
    if (seriesId) list = list.filter((v) => Number(v.series_id) === Number(seriesId));
    if (needle) {
      list = list.filter(
        (v) =>
          String(v.title || "")
            .toLowerCase()
            .includes(needle) ||
          String(v.series_title || "")
            .toLowerCase()
            .includes(needle)
      );
    }
    return list;
  }

  function closePicker() {
    const toggle = $("modal-library-picker");
    if (toggle) toggle.checked = false;
    ctx = null;
  }

  function multiResult() {
    if (!ctx || ctx.mode !== "multi") {
      return { seriesIds: [], videoIds: [], seriesTitle: "", seriesTitles: [], videoTitles: [] };
    }
    const videoIds = Array.from(ctx.videoIds || []).map(Number).filter((n) => n > 0);
    if (videoIds.length) {
      const sid = Array.from(ctx.seriesIds || [])[0];
      const s = catalog.series.find((x) => Number(x.id) === Number(sid));
      const byId = new Map(catalog.videos.map((v) => [Number(v.id), v]));
      const videoTitles = videoIds.map((id) => {
        const v = byId.get(id);
        return v && v.title ? String(v.title) : "Video #" + id;
      });
      return {
        seriesIds: [],
        videoIds,
        seriesTitle: s ? s.title : "",
        seriesTitles: [],
        videoTitles,
      };
    }
    const seriesIds = Array.from(ctx.seriesIds || []).map(Number).filter((n) => n > 0);
    const byId = new Map(catalog.series.map((s) => [Number(s.id), s]));
    const seriesTitles = seriesIds.map((id) => {
      const s = byId.get(id);
      return s && s.title ? String(s.title) : "Series #" + id;
    });
    return { seriesIds, videoIds: [], seriesTitle: "", seriesTitles, videoTitles: [] };
  }

  function renderMultiChrome() {
    if (!ctx || ctx.mode !== "multi") return;
    const videosCol = $("library-picker-videos-col");
    const grid = $("library-picker-grid");
    const box = $("library-picker-box");
    const confirmBtn = $("library-picker-confirm");
    if (box) {
      box.classList.add("max-w-4xl");
      box.classList.remove("max-w-2xl");
    }
    if (grid) grid.classList.add("md:grid-cols-2");
    if (videosCol) videosCol.classList.remove("hidden");
    if (confirmBtn) confirmBtn.classList.remove("hidden");
  }

  function renderMultiSeriesList() {
    if (!ctx || ctx.mode !== "multi") return;
    const qEl = $("library-picker-q");
    const q = qEl ? qEl.value : "";
    const seriesList = $("library-picker-series");
    if (!seriesList) return;
    const seriesIds = ctx.seriesIds;
    const videoIds = ctx.videoIds;
    const videoMode = videoIds.size > 0;
    const oneSeries = seriesIds.size === 1;
    const lockedSeriesId = videoMode || oneSeries ? Array.from(seriesIds)[0] : null;

    if (videoMode && lockedSeriesId) {
      const s = catalog.series.find((x) => Number(x.id) === Number(lockedSeriesId));
      seriesList.innerHTML =
        '<li class="list-row bg-base-200" aria-selected="true">' +
        '<div class="relative shrink-0">' +
        seriesPosterHTML(s) +
        "</div>" +
        '<div class="list-col-grow min-w-0 font-medium truncate">' +
        escapeHtml(s ? s.title : "Series") +
        "</div>" +
        '<button type="button" class="btn btn-ghost btn-sm btn-square shrink-0 tooltip tooltip-left" data-lp-unlock-series aria-label="Change series" data-tip="Change series">' +
        '<i data-lucide="x" class="size-4"></i></button></li>';
    } else {
      const series = filterSeries(q);
      let html = "";
      if (!series.length) {
        html =
          '<li class="list-row"><span class="opacity-60 text-sm px-2">' +
          (String(q || "").trim() ? "No series match" : "No series") +
          "</span></li>";
      } else {
        html = series
          .map((s) => {
            const checked = seriesIds.has(Number(s.id));
            return (
              '<li class="list-row cursor-pointer' +
              (checked ? " bg-base-200" : "") +
              '" data-lp-series-id="' +
              s.id +
              '" role="option" aria-selected="' +
              (checked ? "true" : "false") +
              '">' +
              '<div class="self-center">' +
              '<input type="checkbox" class="checkbox checkbox-sm pointer-events-none" tabindex="-1" data-lp-series-check value="' +
              s.id +
              '"' +
              (checked ? " checked" : "") +
              ' aria-hidden="true" />' +
              "</div>" +
              '<div class="relative shrink-0 self-center">' +
              seriesPosterHTML(s) +
              "</div>" +
              '<div class="list-col-grow min-w-0 font-medium truncate self-center">' +
              escapeHtml(s.title) +
              "</div></li>"
            );
          })
          .join("");
      }
      seriesList.innerHTML = html;
    }
    lucideRefresh(seriesList);
  }

  function renderMultiVideosList() {
    if (!ctx || ctx.mode !== "multi") return;
    const qEl = $("library-picker-q");
    const q = qEl ? qEl.value : "";
    const videoList = $("library-picker-videos");
    if (!videoList) return;
    const seriesIds = ctx.seriesIds;
    const videoIds = ctx.videoIds;
    const videoMode = videoIds.size > 0;
    const oneSeries = seriesIds.size === 1;
    const lockedSeriesId = videoMode || oneSeries ? Array.from(seriesIds)[0] : null;
    const multiSeries = seriesIds.size > 1;
    const listVideos = !multiSeries;

    if (listVideos) {
      const videos = filterVideos(lockedSeriesId || null, q, ctx.packedOnly !== false);
      let html = "";
      if (!videos.length) {
        html =
          '<li class="list-row"><span class="opacity-60 text-sm px-2">' +
          (String(q || "").trim() ? "No videos match" : "No videos with packed media") +
          "</span></li>";
      } else {
        html = videos
          .map((v) => {
            const checked = videoIds.has(Number(v.id));
            const thumb = videoThumbURL(v);
            const img = thumb
              ? '<img class="size-full rounded object-cover" src="' +
                escapeHtml(thumb) +
                '" alt="" width="80" height="45" loading="lazy" decoding="async" />'
              : '<div class="bg-base-200 w-full h-full rounded flex items-center justify-center" aria-hidden="true"><i data-lucide="square-play" class="size-5 opacity-40"></i></div>';
            return (
              '<li class="list-row cursor-pointer' +
              (checked ? " bg-base-200" : "") +
              '" data-lp-video-id="' +
              v.id +
              '" role="option" aria-selected="' +
              (checked ? "true" : "false") +
              '">' +
              '<div class="self-center">' +
              '<input type="checkbox" class="checkbox checkbox-sm pointer-events-none" tabindex="-1" data-lp-video-check value="' +
              v.id +
              '"' +
              (checked ? " checked" : "") +
              ' aria-hidden="true" />' +
              "</div>" +
              '<div class="relative shrink-0 w-20 aspect-video self-center">' +
              img +
              "</div>" +
              '<div class="list-col-grow min-w-0 self-center"><div class="font-medium truncate">' +
              escapeHtml(v.title) +
              '</div><div class="text-xs opacity-60 truncate">' +
              escapeHtml(v.series_title || "") +
              "</div></div></li>"
            );
          })
          .join("");
      }
      videoList.innerHTML = html;
    } else {
      videoList.innerHTML =
        '<li class="list-row"><span class="opacity-60 text-sm px-2">Select one series to choose videos</span></li>';
    }
    lucideRefresh(videoList);
  }

  // Update checkbox/highlight on existing series rows without replacing posters (avoids flicker).
  function patchMultiSeriesSelection() {
    if (!ctx || ctx.mode !== "multi" || ctx.videoIds.size > 0) return false;
    const seriesList = $("library-picker-series");
    if (!seriesList) return false;
    const rows = seriesList.querySelectorAll("[data-lp-series-id]");
    if (!rows.length) return false;
    rows.forEach((row) => {
      const id = Number(row.getAttribute("data-lp-series-id"));
      const on = ctx.seriesIds.has(id);
      row.classList.toggle("bg-base-200", on);
      row.setAttribute("aria-selected", on ? "true" : "false");
      const cb = row.querySelector("[data-lp-series-check]");
      if (cb) cb.checked = on;
    });
    return true;
  }

  function renderMulti() {
    if (!ctx || ctx.mode !== "multi") return;
    renderMultiChrome();
    renderMultiSeriesList();
    renderMultiVideosList();
  }

  async function openMulti(opts) {
    await ensureCatalog();
    ctx = {
      mode: "multi",
      packedOnly: opts.packedOnly !== false,
      seriesIds: new Set((opts.initialSeriesIds || []).map(Number).filter((n) => n > 0)),
      videoIds: new Set((opts.initialVideoIds || []).map(Number).filter((n) => n > 0)),
      onConfirm: opts.onConfirm,
    };
    // Video scope implies locking that series.
    if (ctx.videoIds.size) {
      const first = catalog.videos.find((v) => ctx.videoIds.has(Number(v.id)));
      if (first) {
        ctx.seriesIds = new Set([Number(first.series_id)]);
      }
    }
    const title = $("library-picker-title");
    if (title) title.textContent = "Select series/video";
    const sub = $("library-picker-subtitle");
    if (sub) sub.textContent = opts.subtitle || "Choose series or videos for Maintenance tasks.";
    const allowWrap = $("library-picker-allow-existing-wrap");
    if (allowWrap) allowWrap.classList.add("hidden");
    const confirmBtn = $("library-picker-confirm");
    if (confirmBtn) confirmBtn.classList.remove("hidden");
    const qEl = $("library-picker-q");
    if (qEl) {
      qEl.value = "";
      qEl.disabled = false;
      qEl.placeholder = "Filter series and videos";
    }
    renderMulti();
    const toggle = $("modal-library-picker");
    if (toggle) toggle.checked = true;
    requestAnimationFrame(() => {
      qEl?.focus();
    });
  }

  /**
   * Import single-mode open is driven by Import page (render + callbacks).
   * This only shows the modal shell / chrome for single mode.
   */
  function openSingleShell(opts) {
    opts = opts || {};
    ctx = { mode: "single", importDriven: true, ...opts };
    const title = $("library-picker-title");
    if (title) title.textContent = "Select series/video";
    const sub = $("library-picker-subtitle");
    if (sub) sub.textContent = opts.subtitle || "";
    const allowWrap = $("library-picker-allow-existing-wrap");
    if (allowWrap) allowWrap.classList.toggle("hidden", !opts.showAllowExisting);
    const confirmBtn = $("library-picker-confirm");
    if (confirmBtn) confirmBtn.classList.add("hidden");
    const toggle = $("modal-library-picker");
    if (toggle) toggle.checked = true;
  }

  window.openLibraryPicker = async function (opts) {
    opts = opts || {};
    if (opts.mode === "multi") {
      return openMulti(opts);
    }
    openSingleShell(opts);
  };

  window.closeLibraryPicker = closePicker;
  window.setLibraryPickerCatalog = setCatalog;
  window.libraryPickerCREATE = CREATE;

  function wire() {
    const qEl = $("library-picker-q");
    if (qEl && !qEl.dataset.lpWired) {
      qEl.dataset.lpWired = "1";
      qEl.addEventListener("input", () => {
        if (ctx && ctx.mode === "multi") renderMulti();
      });
    }
    // Document delegation so HTMX/page swaps keep working.
    if (document.documentElement.dataset.lpDelegated) return;
    document.documentElement.dataset.lpDelegated = "1";
    document.addEventListener("click", (ev) => {
      if (ev.target.closest("#library-picker-cancel")) {
        closePicker();
        return;
      }
      if (ev.target.closest("#library-picker-confirm")) {
        if (!ctx || ctx.mode !== "multi") return;
        const result = multiResult();
        const cb = ctx.onConfirm;
        closePicker();
        if (typeof cb === "function") cb(result);
        return;
      }
      if (!ctx || ctx.mode !== "multi") return;
      if (ev.target.closest("#library-picker-series [data-lp-unlock-series]")) {
        ctx.videoIds.clear();
        ctx.seriesIds.clear();
        renderMulti();
        return;
      }
      const seriesRow = ev.target.closest("#library-picker-series [data-lp-series-id]");
      if (seriesRow) {
        const id = Number(seriesRow.getAttribute("data-lp-series-id"));
        if (!id) return;
        if (ctx.videoIds.size) ctx.videoIds.clear();
        if (ctx.seriesIds.has(id)) ctx.seriesIds.delete(id);
        else ctx.seriesIds.add(id);
        if (ctx.seriesIds.size > 1) ctx.videoIds.clear();
        // Keep series posters in DOM; only refresh video column.
        if (patchMultiSeriesSelection()) {
          renderMultiChrome();
          renderMultiVideosList();
        } else {
          renderMulti();
        }
        return;
      }
      const videoRow = ev.target.closest("#library-picker-videos [data-lp-video-id]");
      if (videoRow) {
        const id = Number(videoRow.getAttribute("data-lp-video-id"));
        if (!id) return;
        const v = catalog.videos.find((x) => Number(x.id) === id);
        if (!v) return;
        const vidSeries = Number(v.series_id);
        if (ctx.seriesIds.size > 1) return;
        if (ctx.seriesIds.size === 0) {
          ctx.seriesIds = new Set([vidSeries]);
          ctx.videoIds.add(id);
        } else {
          const sid = Array.from(ctx.seriesIds)[0];
          if (Number(sid) !== vidSeries) {
            // Switch video scope to this video's series.
            ctx.seriesIds = new Set([vidSeries]);
            ctx.videoIds = new Set([id]);
          } else if (ctx.videoIds.has(id)) {
            ctx.videoIds.delete(id);
          } else {
            ctx.videoIds.add(id);
          }
        }
        renderMulti();
      }
    });
    document.addEventListener("input", (ev) => {
      if (ev.target && ev.target.id === "library-picker-q" && ctx && ctx.mode === "multi") {
        renderMulti();
      }
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", wire);
  } else {
    wire();
  }
})();
