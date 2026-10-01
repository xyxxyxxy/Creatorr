package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func (h *Handler) actionAddRoot(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ttl, err := parseRetentionTTLDays(r.FormValue("retention_ttl_days"))
	if err != nil {
		redirectOrJSONRootErr(w, r, err)
		return
	}
	epFmt := strings.TrimSpace(r.FormValue("episode_format"))
	seFmt := strings.TrimSpace(r.FormValue("special_episode_format"))
	sfFmt := strings.TrimSpace(r.FormValue("special_feature_format"))
	_, err = h.Library.CreateRootWithFormats(strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("path")), epFmt, seFmt, sfFmt, ttl)
	if err != nil {
		redirectOrJSONRootErr(w, r, err)
		return
	}
	redirectOrJSONRootOK(w, r, "ok=root")
}

func (h *Handler) actionUpdateRoot(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	name := strings.TrimSpace(r.FormValue("name"))
	path := strings.TrimSpace(r.FormValue("path"))
	epFmt := strings.TrimSpace(r.FormValue("episode_format"))
	seFmt := strings.TrimSpace(r.FormValue("special_episode_format"))
	sfFmt := strings.TrimSpace(r.FormValue("special_feature_format"))
	ttlRaw := strings.TrimSpace(r.FormValue("retention_ttl_days"))
	clearRetention := ttlRaw == ""
	var retention *int64
	if !clearRetention {
		ttl, err := parseRetentionTTLDays(ttlRaw)
		if err != nil {
			redirectOrJSONRootErr(w, r, err)
			return
		}
		if ttl == nil {
			clearRetention = true
		} else {
			retention = ttl
		}
	}
	formatBusy := false
	for _, key := range []string{"episode_format", "special_episode_format", "special_feature_format"} {
		if _, ok := r.Form[key]; ok {
			formatBusy = true
			break
		}
	}
	if formatBusy {
		if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRenameEpisodes, queue.SystemDomain); busy {
			msg := "Cancel or wait for 'Apply episode format' before changing formats"
			if wantsJSON(r) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg, "field": "episode_format"})
				return
			}
			redirectSettings(w, r, "/settings/library", "err="+urlQuery(msg))
			return
		}
	}
	var epPtr, sePtr, sfPtr *string
	if _, ok := r.Form["episode_format"]; ok {
		epPtr = &epFmt
	}
	if _, ok := r.Form["special_episode_format"]; ok {
		sePtr = &seFmt
	}
	if _, ok := r.Form["special_feature_format"]; ok {
		sfPtr = &sfFmt
	}
	_, err := h.Library.UpdateRootFormats(id, &name, &path, epPtr, sePtr, sfPtr, retention, clearRetention)
	if err != nil {
		redirectOrJSONRootErr(w, r, err)
		return
	}
	redirectOrJSONRootOK(w, r, "ok=root-updated")
}

func (h *Handler) actionDeleteRoot(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err := h.Library.DeleteRoot(id); err != nil {
		redirectSettings(w, r, "/settings/library", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/library", "ok=root-deleted")
}

// parseRetentionTTLDays reads UI days and returns stored seconds (nil = keep forever).
func parseRetentionTTLDays(raw string) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	days, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid retention")
	}
	if days < 0 {
		return nil, fmt.Errorf("invalid retention")
	}
	if days == 0 {
		return nil, nil
	}
	sec := library.RetentionSecondsFromDays(days)
	return &sec, nil
}

func (h *Handler) actionAddProfile(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	hours, err := library.ParseMaturityInt(r.FormValue("maturity_media_hours"), library.MaxMaturityRedownloadHours)
	if err != nil {
		redirectSettings(w, r, "/settings/library", "err="+urlQuery("invalid media maturity hours"))
		return
	}
	days, err := library.ParseMaturityInt(r.FormValue("maturity_sidecar_days"), library.MaxMaturitySidecarDays)
	if err != nil {
		redirectSettings(w, r, "/settings/library", "err="+urlQuery("invalid sidecar maturity days"))
		return
	}
	mark := r.Form["sponsorblock_mark"]
	remove := r.Form["sponsorblock_remove"]
	reencode := false
	for _, v := range r.Form["sponsorblock_reencode_cut"] {
		if v == "1" {
			reencode = true
			break
		}
	}
	infoCards := false
	for _, v := range r.Form["sponsorblock_info_cards"] {
		if v == "1" {
			infoCards = true
			break
		}
	}
	verifyMedia := r.FormValue("verify_media") == "1"
	_, err = h.Library.CreateProfileFull(
		strings.TrimSpace(r.FormValue("name")),
		strings.TrimSpace(r.FormValue("format_selector")),
		hours,
		library.MaturitySidecarDaysToHours(days),
		mark,
		remove,
		reencode,
		infoCards,
		verifyMedia,
	)
	if err != nil {
		redirectSettings(w, r, "/settings/library", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/library", "ok=profile")
}

func (h *Handler) actionUpdateProfile(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	name := strings.TrimSpace(r.FormValue("name"))
	format := strings.TrimSpace(r.FormValue("format_selector"))
	hours, err := library.ParseMaturityInt(r.FormValue("maturity_media_hours"), library.MaxMaturityRedownloadHours)
	if err != nil {
		redirectSettings(w, r, "/settings/library", "err="+urlQuery("invalid media maturity hours"))
		return
	}
	days, err := library.ParseMaturityInt(r.FormValue("maturity_sidecar_days"), library.MaxMaturitySidecarDays)
	if err != nil {
		redirectSettings(w, r, "/settings/library", "err="+urlQuery("invalid sidecar maturity days"))
		return
	}
	sidecarHours := library.MaturitySidecarDaysToHours(days)
	mark := r.Form["sponsorblock_mark"]
	remove := r.Form["sponsorblock_remove"]
	reencode := false
	for _, v := range r.Form["sponsorblock_reencode_cut"] {
		if v == "1" {
			reencode = true
			break
		}
	}
	infoCards := false
	for _, v := range r.Form["sponsorblock_info_cards"] {
		if v == "1" {
			infoCards = true
			break
		}
	}
	verifyMedia := r.FormValue("verify_media") == "1"
	_, err = h.Library.UpdateProfileParams(id, library.UpdateProfileParams{
		Name:                    &name,
		FormatSelector:          &format,
		MaturityRedownloadHours: &hours,
		MaturitySidecarHours:    &sidecarHours,
		SponsorBlockMark:        &mark,
		SponsorBlockRemove:      &remove,
		SponsorBlockReencodeCut: &reencode,
		SponsorBlockInfoCards:   &infoCards,
		VerifyMedia:             &verifyMedia,
	})
	if err != nil {
		redirectSettings(w, r, "/settings/library", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/library", "ok=profile-updated")
}

func (h *Handler) actionDeleteProfile(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err := h.Library.DeleteProfile(id); err != nil {
		redirectSettings(w, r, "/settings/library", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/library", "ok=profile-deleted")
}
