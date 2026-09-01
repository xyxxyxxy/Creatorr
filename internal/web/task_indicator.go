package web

import (
	"fmt"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// taskIndicatorView is data for partials/task_indicator.html.
type taskIndicatorView struct {
	ID          string
	Status      string // pending | running | ""
	Progress    *float64
	Title       string
	OOB         bool
	VideoStatus string // when set (video list), idle shows status icon; busy replaces it
}

func indicatorFromTask(id string, t *queue.Task, title string) taskIndicatorView {
	v := taskIndicatorView{ID: id, Title: title}
	if t == nil {
		return v
	}
	v.Status = t.Status
	if t.Progress.Valid {
		p := t.Progress.Float64
		v.Progress = &p
	}
	if title == "" {
		v.Title = t.Kind + " · " + t.Status
		if t.Message != "" {
			v.Title = t.Message
		}
	}
	return v
}

// videoStatusIndicator builds a list-cell indicator: status icon, or list-ordered/spinner/radial when busy.
func videoStatusIndicator(videoID int64, t *queue.Task, videoStatus string) taskIndicatorView {
	label := videoStatusLabel(videoStatus)
	title := ""
	if t != nil && t.Kind == queue.KindDeleteFiles {
		if t.Status == queue.StatusRunning {
			title = "Deleting…"
		} else {
			title = "Queued for deletion"
		}
	}
	v := indicatorFromTask(videoIndicatorID(videoID), t, title)
	v.VideoStatus = videoStatus
	if v.Status == "" {
		v.Title = label
	} else if title != "" {
		v.Title = title
	} else if label != "" {
		if v.Title != "" {
			v.Title = label + " · " + v.Title
		} else {
			v.Title = label
		}
	}
	return v
}

func videoStatusLabel(status string) string {
	switch status {
	case "wanted":
		return "wanted"
	case "wanted_source_error":
		return "wanted (source error)"
	case "wanted_download_error":
		return "wanted (download error)"
	case "verify_failed":
		return "verify failed"
	case "downloaded":
		return "downloaded"
	case "missing":
		return "missing"
	case "deleted":
		return "deleted"
	case "ignored":
		return "ignored"
	default:
		return status
	}
}

func pickBestTask(tasks []queue.Task) *queue.Task {
	var best *queue.Task
	var fileDel *queue.Task
	for i := range tasks {
		t := &tasks[i]
		if t.Kind == queue.KindDeleteFiles {
			if t.Status == queue.StatusRunning {
				return t
			}
			if fileDel == nil {
				fileDel = t
			}
			continue
		}
		if t.Status == queue.StatusRunning {
			return t
		}
		if best == nil {
			best = t
		}
	}
	if fileDel != nil {
		return fileDel
	}
	return best
}

func taskIsFileDelete(t *queue.Task) bool {
	return t != nil && t.Kind == queue.KindDeleteFiles
}

func (h *Handler) errIfSeriesDeleting(seriesID int64) error {
	if h.Library == nil || seriesID < 1 {
		return nil
	}
	ok, err := h.Library.SeriesQueuedForDelete(seriesID)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("%w: series queued for deletion", library.ErrConflict)
	}
	return nil
}

func (h *Handler) errIfVideoDeleting(videoID int64) error {
	if h.Library == nil || videoID < 1 {
		return nil
	}
	ok, err := h.Library.VideoQueuedForDelete(videoID)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("%w: video queued for deletion", library.ErrConflict)
	}
	return nil
}

// seriesActivityMaps groups active tasks for series/source/video indicators.
func seriesActivityMaps(tasks []queue.Task) (seriesTasks []queue.Task, bySource map[int64][]queue.Task, byVideo map[int64][]queue.Task) {
	bySource = map[int64][]queue.Task{}
	byVideo = map[int64][]queue.Task{}
	for _, t := range tasks {
		seriesTasks = append(seriesTasks, t)
		if t.VideoID.Valid && t.VideoID.Int64 > 0 {
			byVideo[t.VideoID.Int64] = append(byVideo[t.VideoID.Int64], t)
		}
		if t.Kind == queue.KindScan {
			if sid := queue.SourceIDFromPayload(t.Payload); sid > 0 {
				bySource[sid] = append(bySource[sid], t)
			}
		}
		if t.Kind == queue.KindDeleteFiles {
			_, vids := queue.FileDeleteIDsFromPayload(t.Payload)
			for _, vid := range vids {
				if vid > 0 {
					byVideo[vid] = append(byVideo[vid], t)
				}
			}
		}
	}
	return seriesTasks, bySource, byVideo
}

// mergeFileDeleteForSeries attaches system-lane delete_files tasks that target this series.
func (h *Handler) mergeFileDeleteForSeries(seriesID int64, seriesTasks *[]queue.Task, byVideo map[int64][]queue.Task) {
	if h.Queue == nil || h.Library == nil {
		return
	}
	tasks, err := h.Queue.ListActiveFileDelete()
	if err != nil {
		return
	}
	seenTask := map[int64]struct{}{}
	for i := range tasks {
		t := &tasks[i]
		sids, vids := queue.FileDeleteIDsFromPayload(t.Payload)
		hit := false
		for _, sid := range sids {
			if sid == seriesID {
				hit = true
				break
			}
		}
		if hit {
			if seriesTasks != nil {
				if _, ok := seenTask[t.ID]; !ok {
					*seriesTasks = append(*seriesTasks, *t)
					seenTask[t.ID] = struct{}{}
				}
			}
			vrows, err := h.Library.DB.SQL.Query(`SELECT id FROM videos WHERE series_id = ?`, seriesID)
			if err != nil {
				continue
			}
			for vrows.Next() {
				var vid int64
				if vrows.Scan(&vid) == nil && byVideo != nil {
					byVideo[vid] = append(byVideo[vid], *t)
				}
			}
			_ = vrows.Close()
			continue
		}
		for _, vid := range vids {
			if v, err := h.Library.GetVideo(vid); err == nil && v.SeriesID == seriesID {
				if byVideo != nil {
					byVideo[vid] = append(byVideo[vid], *t)
				}
				if seriesTasks != nil {
					if _, ok := seenTask[t.ID]; !ok {
						*seriesTasks = append(*seriesTasks, *t)
						seenTask[t.ID] = struct{}{}
					}
				}
			}
		}
	}
}

// mergeFileDeleteIntoSeriesMap attaches active delete_files tasks onto series list maps.
func (h *Handler) mergeFileDeleteIntoSeriesMap(bySeries map[int64][]queue.Task) {
	if h.Queue == nil || h.Library == nil || bySeries == nil {
		return
	}
	tasks, err := h.Queue.ListActiveFileDelete()
	if err != nil {
		return
	}
	seen := map[int64]map[int64]struct{}{} // seriesID -> taskID
	add := func(sid int64, t queue.Task) {
		if sid <= 0 {
			return
		}
		if seen[sid] == nil {
			seen[sid] = map[int64]struct{}{}
		}
		if _, ok := seen[sid][t.ID]; ok {
			return
		}
		seen[sid][t.ID] = struct{}{}
		bySeries[sid] = append(bySeries[sid], t)
	}
	for i := range tasks {
		t := tasks[i]
		sids, vids := queue.FileDeleteIDsFromPayload(t.Payload)
		for _, sid := range sids {
			add(sid, t)
		}
		for _, vid := range vids {
			if v, err := h.Library.GetVideo(vid); err == nil {
				add(v.SeriesID, t)
			}
		}
	}
}

func seriesIndicatorID(seriesID int64) string {
	return fmt.Sprintf("task-ind-series-%d", seriesID)
}

func videoIndicatorID(videoID int64) string {
	return fmt.Sprintf("task-ind-video-%d", videoID)
}

// seriesStatusView drives partials/series_status_indicator.html and series_monitor_indicator.html.
type seriesStatusView struct {
	Kind  string // wanted_source_error | wanted_download_error | verify_failed | scan_error | incomplete | monitored | unmonitored
	Title string
	Count int // video error count for health badge; 0 = icon only
}

// buildSeriesMonitorStatus is left of series list title: always monitored | unmonitored.
func buildSeriesMonitorStatus(monitored bool) seriesStatusView {
	if monitored {
		return seriesStatusView{Kind: "monitored", Title: "Monitored"}
	}
	return seriesStatusView{Kind: "unmonitored", Title: "Unmonitored"}
}

func seriesHealthTitle(label string, count int) string {
	if count > 0 {
		return fmt.Sprintf("%s (%d)", label, count)
	}
	return label
}

// buildSeriesHealthStatus is poster health badge only (errors/warnings; no task busy). ok false = idle healthy.
func buildSeriesHealthStatus(errs library.SeriesVideoErrorFlags, warn library.SeriesWarnLevel) (seriesStatusView, bool) {
	if errs.HasSourceError {
		return seriesStatusView{
			Kind:  "wanted_source_error",
			Title: seriesHealthTitle("Source error", errs.SourceErrorCount),
			Count: errs.SourceErrorCount,
		}, true
	}
	if errs.HasDownloadError {
		return seriesStatusView{
			Kind:  "wanted_download_error",
			Title: seriesHealthTitle("Download error", errs.DownloadErrorCount),
			Count: errs.DownloadErrorCount,
		}, true
	}
	if errs.HasVerifyFailed {
		return seriesStatusView{
			Kind:  "verify_failed",
			Title: seriesHealthTitle("Verify failed", errs.VerifyFailedCount),
			Count: errs.VerifyFailedCount,
		}, true
	}
	if warn == library.SeriesWarnError {
		return seriesStatusView{Kind: "scan_error", Title: "Source scan error"}, true
	}
	if warn == library.SeriesWarnIncomplete {
		return seriesStatusView{Kind: "incomplete", Title: "Full scan incomplete and no scan schedule"}, true
	}
	return seriesStatusView{}, false
}

// sourceStatusView drives the combined source Status cell (icon + short label).
// Kind priority: running | pending | scan_error | stalled | incomplete | scheduled | indexed | unscheduled.
type sourceStatusView struct {
	SourceID int64
	Kind     string
	Title    string // tooltip (schedule, full-scan limit, last scan, guidance)
	Label    string // short visible text
	Href     string // optional /task/{id}
	Progress *float64
	OOB      bool
}

type sourceStatusParams struct {
	Src                 library.Source
	Best                *queue.Task
	HasError            bool
	ErrMsg              string
	ErrCode             string
	Stalled             bool
	SeriesMonitored     bool
	DomainActive        bool
	DomainDisabledTitle string
	ScanCronLabel       string
	Summary             string
	HasScanned          bool
	HistoryID           int64
	Now                 time.Time
	LastTipScannedAt    time.Time // tip scan (mode=scan) success; zero if none
}

func joinStatusTip(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// joinStatusSentences joins non-empty parts with ".\n" (sentence-style multiline tooltip).
func joinStatusSentences(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, strings.TrimRight(p, "."))
		}
	}
	return strings.Join(out, ".\n")
}

// titleFiltersTip notes that title include and/or exclude regexp is set (no which/detail).
func titleFiltersTip(src library.Source) string {
	if strings.TrimSpace(src.TitleRegexpInclude) != "" || strings.TrimSpace(src.TitleRegexpExclude) != "" {
		return "Regexp filters apply"
	}
	return ""
}

func truncateStatusLabel(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

func buildSourceStatus(p sourceStatusParams) sourceStatusView {
	src := p.Src
	v := sourceStatusView{SourceID: src.ID}

	cronLabel := p.ScanCronLabel
	if cronLabel == "" {
		cronLabel = src.ScanCron
	}
	scheduleTip := ""
	switch {
	case src.IsSingle():
		scheduleTip = "Single source - no scan schedule"
	case src.ScanCronNever():
		scheduleTip = "Scan schedule off"
	default:
		scheduleTip = "Scan scheduled: " + cronLabel
		if !p.SeriesMonitored {
			scheduleTip += " (series unmonitored - scan paused)"
		} else if !p.DomainActive {
			scheduleTip += " (domain inactive)"
		}
	}
	limitTip := ""
	if src.FullScanLimit > 0 {
		limitTip = fmt.Sprintf("full scan limit %d", src.FullScanLimit)
	}
	lastTip := ""
	if p.HasScanned && p.Summary != "" {
		lastTip = "Last scan: " + p.Summary
	}

	if p.Best != nil {
		title := p.Best.Kind + " · " + p.Best.Status
		if p.Best.Message != "" {
			title = p.Best.Message
		}
		if p.Best.Status == queue.StatusRunning {
			v.Kind = "running"
			v.Label = "scanning"
			if p.Best.Message != "" {
				v.Label = truncateStatusLabel(p.Best.Message, 40)
			}
			v.Title = joinStatusTip(title, scheduleTip, limitTip)
			if p.Best.Progress.Valid {
				prog := p.Best.Progress.Float64
				v.Progress = &prog
			}
			v.Href = fmt.Sprintf("/task/%d", p.Best.ID)
			return v
		}
		if p.Best.Status == queue.StatusPending {
			v.Kind = "pending"
			v.Label = "queued"
			v.Title = joinStatusTip(title, scheduleTip, limitTip)
			v.Href = fmt.Sprintf("/task/%d", p.Best.ID)
			return v
		}
	}

	if p.HasError {
		v.Kind = "scan_error"
		v.Label = truncateStatusLabel(p.ErrMsg, 40)
		if v.Label == "" {
			v.Label = "error"
		}
		errTip := strings.TrimSpace(p.ErrCode)
		if errTip == "" {
			errTip = "Scan error"
		}
		v.Title = joinStatusTip(errTip, scheduleTip, limitTip, lastTip)
		if p.HistoryID > 0 {
			v.Href = fmt.Sprintf("/task/%d", p.HistoryID)
		}
		return v
	}

	if p.Stalled {
		hasSchedule := !src.IsSingle() && !src.ScanCronNever()
		if !hasSchedule {
			v.Kind = "incomplete" // full scan incomplete, no tip schedule
		} else {
			v.Kind = "stalled"
		}
		line1 := "Full scan incomplete"
		line2 := ""
		if !p.DomainActive {
			v.Label = "pending"
			line2 = p.DomainDisabledTitle
			if line2 == "" {
				line2 = "Domain is inactive - activate it under 'Settings → Queue / Domains', then use 'Full scan'"
			}
		} else {
			v.Label = "incomplete"
			if hasSchedule {
				line2 = nextScanTip(p)
			} else {
				line2 = "No scan scheduled"
			}
		}
		v.Title = joinStatusSentences(line1, titleFiltersTip(src), line2)
		if v.Title == "" {
			v.Title = line1
		}
		return v
	}

	// Idle: full scan done or not yet started without stall (active scan covered above).
	if src.FullScanDone {
		if src.IsSingle() {
			// Single: success icon + "complete"; tip is only relative scanned time.
			v.Kind = "indexed"
			v.Label = "complete"
			v.Title = "Indexed"
			if p.HasScanned && p.Summary != "" && p.Summary != "never" {
				ago := p.Summary
				if i := strings.Index(ago, " ("); i >= 0 {
					ago = ago[:i]
				}
				v.Title = "Scanned " + ago
			}
			return v
		}
		if src.ScanCronNever() {
			v.Kind = "unscheduled"
		} else {
			v.Kind = "scheduled"
		}
		if p.HasScanned && p.Summary != "" && p.Summary != "never" {
			v.Label = p.Summary
		} else {
			v.Label = "indexed"
		}
		// Full scan complete: optional limit tip + next scan.
		untilTip := ""
		if src.FullScanLimit > 0 {
			untilTip = fmt.Sprintf("Full scan limited to %d entries", src.FullScanLimit)
		}
		timeTip := lastTip
		if !src.ScanCronNever() {
			if next := nextScanTip(p); next != "" {
				timeTip = next
			}
		}
		v.Title = joinStatusSentences(untilTip, titleFiltersTip(src), timeTip)
		if v.Title == "" {
			v.Title = "Indexed"
		}
		if p.HistoryID > 0 {
			v.Href = fmt.Sprintf("/task/%d", p.HistoryID)
		}
		return v
	}

	// Full scan not done, not stalled, no active task: treat as scanning edge (rare).
	if src.IsSingle() || src.ScanCronNever() {
		v.Kind = "incomplete"
		v.Label = "scanning"
		v.Title = joinStatusTip("Full scan in progress", scheduleTip, limitTip, lastTip)
		return v
	}
	v.Kind = "scheduled"
	v.Label = "scanning"
	v.Title = joinStatusTip("Full scan in progress", scheduleTip, limitTip, lastTip)
	return v
}

// nextScanTip is the tooltip countdown to the next tip/full scan for a scheduled feed.
// Empty when schedule is off/unknown so callers can fall back to last-scan tip.
// Missed fires (next after last tip already past) use the next cron after now - same
// idea as scheduler boot anchoring - not "due now".
func nextScanTip(p sourceStatusParams) string {
	src := p.Src
	if src.IsSingle() || src.ScanCronNever() {
		return ""
	}
	if !p.SeriesMonitored {
		return "Next scan paused (series unmonitored)"
	}
	if !p.DomainActive {
		return "Next scan paused (domain inactive)"
	}
	now := p.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	after := p.LastTipScannedAt
	if !after.IsZero() {
		nextAfterLast, err := cronexpr.Next(src.ScanCron, after)
		if err != nil {
			return ""
		}
		if nextAfterLast.After(now) {
			return formatNextScanIn(now, nextAfterLast)
		}
		// Schedule slot after last tip was missed - wait for next fire after now.
	}
	next, err := cronexpr.Next(src.ScanCron, now)
	if err != nil || next.IsZero() {
		return ""
	}
	return formatNextScanIn(now, next)
}

func formatNextScanIn(now, next time.Time) string {
	span := formatInShort(now, next)
	if span == "now" {
		return "Next scan soon"
	}
	return "Next scan in " + span
}

func (h *Handler) videoIndicator(videoID int64, t *queue.Task, status string) taskIndicatorView {
	return videoStatusIndicator(videoID, t, status)
}
