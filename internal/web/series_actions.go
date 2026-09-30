package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func (h *Handler) actionUpdateSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if err := h.errIfSeriesDeleting(sid); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	if _, err := h.Library.GetSeries(sid, false); err != nil {
		http.NotFound(w, r)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	rootID, _ := strconv.ParseInt(r.FormValue("root_id"), 10, 64)
	qpID, _ := strconv.ParseInt(r.FormValue("quality_profile_id"), 10, 64)
	dm := library.NormalizeDeliveryMode(r.FormValue("delivery_mode"))
	out, err := h.Library.UpdateSeriesDetailed(sid, library.UpdateSeriesParams{
		Title:            &title,
		RootID:           &rootID,
		QualityProfileID: &qpID,
		DeliveryMode:     &dm,
	})
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	monitored := r.FormValue("monitored") == "1"
	if err := h.Library.SetSeriesMonitored(sid, monitored); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	ok := "updated"
	if out.RenameQueued {
		ok = "series-rename"
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=%s", sid, ok), http.StatusSeeOther)
}

func (h *Handler) actionAddSource(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	kind := r.FormValue("kind")
	scanCron := ""
	if kind != library.SourceKindSingle {
		c, err := parseFeedScanCron(r, "@weekly")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
			return
		}
		scanCron = c
	}
	titleInclude := ""
	titleExclude := ""
	indexAsIgnored := false
	fullScanLimit := 0
	if kind != library.SourceKindSingle {
		titleInclude = strings.TrimSpace(r.FormValue("title_regexp_include"))
		titleExclude = strings.TrimSpace(r.FormValue("title_regexp_exclude"))
		indexAsIgnored = r.FormValue("index_as_ignored") == "1"
		var err error
		fullScanLimit, err = parseFullScanLimitForm(r)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
			return
		}
	}
	_, err := h.Library.AddSource(sid, library.AddSourceParams{
		URL:                strings.TrimSpace(r.FormValue("url")),
		Label:              strings.TrimSpace(r.FormValue("label")),
		Kind:               kind,
		ScanCron:           scanCron,
		IndexAsIgnored:     indexAsIgnored,
		TitleRegexpInclude: titleInclude,
		TitleRegexpExclude: titleExclude,
		FullScanLimit:      fullScanLimit,
	})
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=source", sid), http.StatusSeeOther)
}

func (h *Handler) actionUpdateSource(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	srcID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	label := strings.TrimSpace(r.FormValue("label"))
	redir := seriesSourceRedirect(r, sid, srcID)
	cur, err := h.Library.GetSource(sid, srcID)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	p := library.UpdateSourceParams{
		Label: &label,
	}
	if !cur.IsSingle() {
		limit, err := parseFullScanLimitForm(r)
		if err != nil {
			http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
			return
		}
		p.FullScanLimit = &limit
		if _, ok := r.Form["scan_cron"]; ok {
			cron, err := cronexpr.NormalizeScanCron(r.FormValue("scan_cron"))
			if err != nil {
				http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
				return
			}
			p.ScanCron = &cron
		} else if _, ok := r.Form["scan_cron_schedule"]; ok {
			cron, err := cronexpr.NormalizeScanCron(r.FormValue("scan_cron_schedule"))
			if err != nil {
				http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
				return
			}
			p.ScanCron = &cron
		}
	}
	idx := r.FormValue("index_as_ignored") == "1"
	if !cur.IsSingle() {
		p.IndexAsIgnored = &idx
		titleInclude := strings.TrimSpace(r.FormValue("title_regexp_include"))
		titleExclude := strings.TrimSpace(r.FormValue("title_regexp_exclude"))
		p.TitleRegexpInclude = &titleInclude
		p.TitleRegexpExclude = &titleExclude
	} else {
		off := false
		p.IndexAsIgnored = &off
	}
	_, err = h.Library.UpdateSource(sid, srcID, p)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendQuery(redir, "ok=source-updated"), http.StatusSeeOther)
}

func (h *Handler) actionDeleteSource(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	srcID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	if r.FormValue("confirm_delete") != "1" {
		http.Redirect(w, r, appendQuery(seriesSourceRedirect(r, sid, srcID), "err="+urlQuery("confirm delete to remove this source")), http.StatusSeeOther)
		return
	}
	if err := h.Library.DeleteSource(sid, srcID); err != nil {
		http.Redirect(w, r, appendQuery(seriesSourceRedirect(r, sid, srcID), "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=source-deleted", sid), http.StatusSeeOther)
}

func (h *Handler) actionDeleteSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if r.FormValue("delete_files") != "1" {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery("confirm delete to remove this series and its library files")), http.StatusSeeOther)
		return
	}
	if err := h.Library.DeleteSeries(sid, true); err != nil {
		http.Redirect(w, r, "/series?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/series?ok=delete-queued", http.StatusSeeOther)
}

func (h *Handler) actionScanSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if err := h.errIfSeriesDeleting(sid); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	n, _, err := h.Library.EnqueueScansForSeries(sid)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=scan-for-new-%d", sid, n), http.StatusSeeOther)
}

func (h *Handler) actionScanSource(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	srcID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	redir := seriesSourceRedirect(r, sid, srcID)
	src, err := h.Library.GetSourceByID(srcID)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	okFlash := "scan-for-new"
	if !src.FullScanDone {
		okFlash = "history-scan"
	}
	_, err = h.Library.EnqueueScanSource(srcID, queue.OriginManual)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendQuery(redir, "ok="+okFlash), http.StatusSeeOther)
}

func (h *Handler) actionFullRescanSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	n, _, err := h.Library.FullRescanSeries(sid)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=restart-history-%d", sid, n), http.StatusSeeOther)
}

func (h *Handler) actionFullRescanSource(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	srcID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	redir := seriesSourceRedirect(r, sid, srcID)
	_, err := h.Library.FullRescanSource(srcID)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendQuery(redir, "ok=restart-history"), http.StatusSeeOther)
}

func (h *Handler) actionMetadataRescanSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if err := h.errIfSeriesDeleting(sid); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	_, err := h.Library.EnqueueMetadataRescanSeries(sid)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=metadata-rescan", sid), http.StatusSeeOther)
}

func (h *Handler) actionMetadataRescanVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if err := h.errIfVideoDeleting(vid); err != nil {
		redir := r.FormValue("redirect")
		if redir == "" {
			redir = fmt.Sprintf("/series/%d/videos/%d", sid, vid)
		}
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	_, err := h.Library.EnqueueMetadataRescanVideo(vid)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d/videos/%d", sid, vid)
	}
	if err != nil {
		http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	sep := "?"
	if strings.Contains(redir, "?") {
		sep = "&"
	}
	http.Redirect(w, r, redir+sep+"ok=metadata-rescan", http.StatusSeeOther)
}

func (h *Handler) actionSetSourceMonitored(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "source monitored flag removed; use domain active and series monitored", http.StatusGone)
}

func (h *Handler) actionSetSeriesMonitored(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if err := h.errIfSeriesDeleting(sid); err != nil {
		redir := r.FormValue("redirect")
		if redir == "" {
			redir = "/series"
		}
		sep := "?"
		if strings.Contains(redir, "?") {
			sep = "&"
		}
		errURL := redir + sep + "err=" + urlQuery(err.Error())
		if hxRequest(r) {
			hxRedirect(w, errURL)
			return
		}
		http.Redirect(w, r, errURL, http.StatusSeeOther)
		return
	}
	monitored := r.FormValue("monitored") == "1"
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = "/series"
	}
	if err := h.Library.SetSeriesMonitored(sid, monitored); err != nil {
		sep := "?"
		if strings.Contains(redir, "?") {
			sep = "&"
		}
		errURL := redir + sep + "err=" + urlQuery(err.Error())
		if hxRequest(r) {
			hxRedirect(w, errURL)
			return
		}
		http.Redirect(w, r, errURL, http.StatusSeeOther)
		return
	}
	if hxRequest(r) {
		if h.tryRenderSeriesListLive(w, r) {
			return
		}
		// Detail (and other pages): monitored is on Edit form; refresh whole page.
		hxRedirect(w, redir)
		return
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

func (h *Handler) actionWantVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d", sid)
	}
	if err := h.errIfVideoDeleting(vid); err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	_, err := h.Library.WantVideo(vid)
	h.finishVideoAction(w, r, sid, redir, err)
}

func (h *Handler) actionDownloadVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d", sid)
	}
	if err := h.errIfVideoDeleting(vid); err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	_, err := h.Library.EnqueueDownloadNow(vid)
	if err == nil {
		redir = appendQuery(redir, "ok=download")
	}
	h.finishVideoAction(w, r, sid, redir, err)
}

func (h *Handler) actionRetrySourceErrors(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	srcID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	redir := seriesSourceRedirect(r, sid, srcID)
	n, err := h.Library.RetrySourceErrors(srcID)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendQuery(redir, "ok=retry&n="+strconv.FormatInt(int64(n), 10)), http.StatusSeeOther)
}

func (h *Handler) actionIgnoreVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d", sid)
	}
	if err := h.errIfVideoDeleting(vid); err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	_, err := h.Library.IgnoreVideo(vid)
	if err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	h.finishVideoAction(w, r, sid, redir, nil)
}

func (h *Handler) actionDeleteVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	_, err := h.Library.DeleteVideo(vid)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d", sid)
	}
	if err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	h.finishVideoAction(w, r, sid, appendQuery(redir, "ok=delete-queued"), nil)
}

func (h *Handler) actionDeleteVideoSidecar(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	fid, _ := strconv.ParseInt(r.FormValue("file_id"), 10, 64)
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir == "" {
		redir = fmt.Sprintf("/series/%d/videos/%d", sid, vid)
	}
	err := h.Library.DeleteVideoSidecar(vid, fid)
	if err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	h.finishVideoAction(w, r, sid, appendQuery(redir, "ok=sidecar-deleted"), nil)
}
