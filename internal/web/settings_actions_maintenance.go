package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func (h *Handler) actionRegenerateNFOs(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.Library == nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("library unavailable"))
		return
	}
	seriesIDs, videoIDs, err := parseMaintenanceScope(r)
	if err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRegenerateNFO, queue.SystemDomain); busy {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("NFO regenerate already queued"))
		return
	}
	if _, err := h.Library.EnqueueRegenerateNFOScoped(seriesIDs, videoIDs); err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/maintenance", "ok=nfo-regen-queued"+maintenanceScopeOKSuffix(seriesIDs, videoIDs))
}

func (h *Handler) actionVerifyAllMedia(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.Library == nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("library unavailable"))
		return
	}
	seriesIDs, videoIDs, err := parseMaintenanceScope(r)
	if err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindIntegrityCheck, queue.SystemDomain); busy {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("'Integrity check' already queued"))
		return
	}
	if _, err := h.Library.EnqueueVerifyAllMediaScoped(seriesIDs, videoIDs, queue.OriginManual); err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/maintenance", "ok=verify-all-queued"+maintenanceScopeOKSuffix(seriesIDs, videoIDs))
}

func (h *Handler) actionRefreshSidecarsScoped(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.Library == nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("library unavailable"))
		return
	}
	seriesIDs, videoIDs, err := parseMaintenanceScope(r)
	if err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	queued, skipped, err := h.Library.EnqueueRefreshSidecarsScoped(seriesIDs, videoIDs)
	if err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	q := "ok=refresh-sidecars-queued" + maintenanceScopeOKSuffix(seriesIDs, videoIDs) +
		"&queued=" + strconv.Itoa(queued) + "&skipped=" + strconv.Itoa(skipped)
	redirectSettings(w, r, "/settings/maintenance", q)
}

func (h *Handler) actionApplyEpisodeNaming(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.Library == nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("library unavailable"))
		return
	}
	seriesIDs, videoIDs, err := parseMaintenanceScope(r)
	if err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	var qerr error
	switch {
	case len(videoIDs) > 0:
		_, qerr = h.Library.EnqueueRenameEpisodesVideos(videoIDs)
	case len(seriesIDs) > 0:
		_, qerr = h.Library.EnqueueRenameEpisodesSeriesIDs(seriesIDs)
	default:
		_, qerr = h.Library.EnqueueRenameEpisodes()
	}
	if qerr != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(qerr.Error()))
		return
	}
	redirectSettings(w, r, "/settings/maintenance", "ok=apply-naming"+maintenanceScopeOKSuffix(seriesIDs, videoIDs))
}

func (h *Handler) actionPreviewApplyEpisodeNaming(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.Library == nil {
		render(w, "maintenance_rename_preview_body", map[string]any{"Err": "library unavailable"})
		return
	}
	seriesIDs, videoIDs, err := parseMaintenanceScope(r)
	if err != nil {
		render(w, "maintenance_rename_preview_body", map[string]any{"Err": err.Error()})
		return
	}
	prev, err := h.Library.PreviewApplyEpisodeNaming(seriesIDs, videoIDs)
	if err != nil {
		render(w, "maintenance_rename_preview_body", map[string]any{"Err": err.Error()})
		return
	}
	render(w, "maintenance_rename_preview_body", map[string]any{"Preview": prev})
}

func (h *Handler) actionMaintenanceRun(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.Library == nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("library unavailable"))
		return
	}
	seriesIDs, videoIDs, err := parseMaintenanceScope(r)
	if err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	wanted := map[string]bool{}
	for _, a := range r.Form["actions"] {
		a = strings.TrimSpace(a)
		if a != "" {
			wanted[a] = true
		}
	}
	order := []string{"reset_metadata_from_info", "apply_episode_naming", "regenerate_nfos", "sync_files", "integrity_check", "refresh_sidecars"}
	var queued []string
	var skipMsgs []string
	var firstErr string
	refreshQueued, refreshSkipped := 0, 0

	for _, key := range order {
		if !wanted[key] {
			continue
		}
		switch key {
		case "reset_metadata_from_info":
			if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindResetMetadataFromInfo, queue.SystemDomain); busy {
				skipMsgs = append(skipMsgs, "'Reset metadata from info.json' already queued")
				continue
			}
			if _, err := h.Library.EnqueueResetMetadataFromInfoScoped(seriesIDs, videoIDs); err != nil {
				if firstErr == "" {
					firstErr = err.Error()
				}
				continue
			}
			queued = append(queued, "reset-meta")
		case "apply_episode_naming":
			var qerr error
			switch {
			case len(videoIDs) > 0:
				_, qerr = h.Library.EnqueueRenameEpisodesVideos(videoIDs)
			case len(seriesIDs) > 0:
				_, qerr = h.Library.EnqueueRenameEpisodesSeriesIDs(seriesIDs)
			default:
				_, qerr = h.Library.EnqueueRenameEpisodes()
			}
			if qerr != nil {
				if firstErr == "" {
					firstErr = qerr.Error()
				}
				continue
			}
			queued = append(queued, "apply")
		case "regenerate_nfos":
			if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRegenerateNFO, queue.SystemDomain); busy {
				skipMsgs = append(skipMsgs, "'Regenerate all NFO files' already queued")
				continue
			}
			if _, err := h.Library.EnqueueRegenerateNFOScoped(seriesIDs, videoIDs); err != nil {
				if firstErr == "" {
					firstErr = err.Error()
				}
				continue
			}
			queued = append(queued, "nfo")
		case "integrity_check":
			if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindIntegrityCheck, queue.SystemDomain); busy {
				skipMsgs = append(skipMsgs, "'Integrity check' already queued")
				continue
			}
			if _, err := h.Library.EnqueueVerifyAllMediaScoped(seriesIDs, videoIDs, queue.OriginManual); err != nil {
				if firstErr == "" {
					firstErr = err.Error()
				}
				continue
			}
			queued = append(queued, "verify")
		case "sync_files":
			if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindSyncFiles, queue.SystemDomain); busy {
				skipMsgs = append(skipMsgs, "'File sync' already queued")
				continue
			}
			id, err := h.Library.EnqueueSyncFiles(queue.OriginManual)
			if err != nil {
				if firstErr == "" {
					firstErr = err.Error()
				}
				continue
			}
			if id == 0 {
				skipMsgs = append(skipMsgs, "No videos to sync")
				continue
			}
			queued = append(queued, "sync")
		case "refresh_sidecars":
			n, skipped, err := h.Library.EnqueueRefreshSidecarsScoped(seriesIDs, videoIDs)
			if err != nil {
				if firstErr == "" {
					firstErr = err.Error()
				}
				continue
			}
			refreshQueued, refreshSkipped = n, skipped
			queued = append(queued, "sidecars")
		}
	}

	if len(queued) == 0 {
		msg := firstErr
		if msg == "" && len(skipMsgs) > 0 {
			msg = strings.Join(skipMsgs, "; ")
		}
		if msg == "" {
			msg = "Select at least one action"
		}
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(msg))
		return
	}

	q := "ok=maintenance-run" + maintenanceScopeOKSuffix(seriesIDs, videoIDs) +
		"&actions=" + urlQuery(strings.Join(queued, ","))
	if refreshQueued > 0 || (wanted["refresh_sidecars"] && maintenanceQueuedHas(queued, "sidecars")) {
		q += "&queued=" + strconv.Itoa(refreshQueued) + "&skipped=" + strconv.Itoa(refreshSkipped)
	}
	if len(skipMsgs) > 0 || firstErr != "" {
		detail := append([]string{}, skipMsgs...)
		if firstErr != "" {
			detail = append(detail, firstErr)
		}
		q += "&partial=" + urlQuery(strings.Join(detail, "; "))
	}
	redirectSettings(w, r, "/settings/maintenance", q)
}

func (h *Handler) actionMaintenanceConfirmSummary(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if h.Library == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "library unavailable"})
		return
	}
	seriesIDs, videoIDs, err := parseMaintenanceScope(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	n, err := h.Library.CountPackedVideos(seriesIDs, videoIDs)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	contactsExternal := false
	for _, a := range r.Form["actions"] {
		if strings.TrimSpace(a) == "refresh_sidecars" {
			contactsExternal = true
			break
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"packed_videos":     n,
		"contacts_external": contactsExternal,
	})
}

func maintenanceQueuedHas(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// parseMaintenanceScope reads repeated series_ids / video_ids form fields (mutually exclusive).
func parseMaintenanceScope(r *http.Request) (seriesIDs, videoIDs []int64, err error) {
	for _, s := range r.Form["series_ids"] {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, perr := strconv.ParseInt(s, 10, 64)
		if perr != nil || id <= 0 {
			return nil, nil, fmt.Errorf("invalid series_ids")
		}
		seriesIDs = append(seriesIDs, id)
	}
	for _, s := range r.Form["video_ids"] {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, perr := strconv.ParseInt(s, 10, 64)
		if perr != nil || id <= 0 {
			return nil, nil, fmt.Errorf("invalid video_ids")
		}
		videoIDs = append(videoIDs, id)
	}
	if len(seriesIDs) > 0 && len(videoIDs) > 0 {
		return nil, nil, fmt.Errorf("series_ids and video_ids are mutually exclusive")
	}
	return seriesIDs, videoIDs, nil
}

func maintenanceScopeOKSuffix(seriesIDs, videoIDs []int64) string {
	switch {
	case len(videoIDs) > 0:
		return "&scope=videos"
	case len(seriesIDs) > 0:
		return "&scope=series"
	default:
		return ""
	}
}

func (h *Handler) actionYtDlpUpdate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.Library == nil {
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery("library unavailable"))
		return
	}
	if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindYtDlpUpdate, queue.SystemDomain); busy {
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery("yt-dlp update already queued or running"))
		return
	}
	id, err := h.Library.EnqueueYtDlpUpdate(queue.OriginManual)
	if err != nil {
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery(err.Error()))
		return
	}
	if id == 0 {
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery("yt-dlp update not enqueued"))
		return
	}
	redirectSettings(w, r, "/settings/connect", "ok=ytdlp-update-queued")
}
