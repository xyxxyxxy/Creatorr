package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/auth"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func (h *Handler) actionSaveSettings(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.FormValue("auth_settings") == "1" {
		h.actionSaveAuthSettings(w, r)
		return
	}
	vals := map[string]string{}
	for _, e := range []string{
		settings.KeyPotFetch,
		settings.KeyYoutubePlayerClient,
		settings.KeyDownloadWantedCron,
		settings.KeySyncFilesCron,
		settings.KeyIntegrityCheckCron,
		settings.KeyRetentionDeleteCron,
		settings.KeyYtDlpUpdateCron,
		settings.KeyYtDlpUpdateChannel,
	} {
		if _, ok := r.Form[e]; !ok {
			continue
		}
		vals[e] = r.FormValue(e)
	}
	if raw, ok := vals[settings.KeyPotFetch]; ok {
		if strings.TrimSpace(h.PotProviderURL) == "" {
			vals[settings.KeyPotFetch] = settings.PotFetchNever
		} else {
			vals[settings.KeyPotFetch] = settings.NormalizePotFetch(raw)
		}
	}
	if raw, ok := vals[settings.KeyYoutubePlayerClient]; ok {
		vals[settings.KeyYoutubePlayerClient] = settings.NormalizeYoutubePlayerClient(raw)
	}
	if r.FormValue("redirect") == "/settings/library" {
		if r.FormValue("subtitle_settings") == "1" {
			vals[settings.KeySubtitleLangs] = settings.SubtitleLangsJSON(r.Form["subtitle_langs"])
			if r.FormValue(settings.KeySubtitleAuto) == "1" {
				vals[settings.KeySubtitleAuto] = "1"
			} else {
				vals[settings.KeySubtitleAuto] = "0"
			}
		}
		if r.FormValue("metadata_settings") == "1" {
			if r.FormValue(settings.KeyMetadataDomainTag) == "1" {
				vals[settings.KeyMetadataDomainTag] = "1"
			} else {
				vals[settings.KeyMetadataDomainTag] = "0"
			}
			if r.FormValue(settings.KeyMetadataGenresFromCategories) == "1" {
				vals[settings.KeyMetadataGenresFromCategories] = "1"
			} else {
				vals[settings.KeyMetadataGenresFromCategories] = "0"
			}
			if r.FormValue(settings.KeyArchiveFallback) == "1" {
				vals[settings.KeyArchiveFallback] = "1"
			} else {
				vals[settings.KeyArchiveFallback] = "0"
			}
		}
	}
	if err := settings.SetMany(h.Queue.DB, vals); err != nil {
		h.respondSettingsSaveError(w, r, err)
		return
	}
	h.respondSettingsSaveOK(w, r)
}

func (h *Handler) actionSaveAuthSettings(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("auth_username"))
	password := r.FormValue("auth_password")
	confirm := r.FormValue("auth_password_confirm")
	if username == "" {
		redirectSettings(w, r, "/settings/general", "err="+urlQuery("username required"))
		return
	}
	var hash string
	if password != "" || confirm != "" {
		if err := auth.ValidatePassword(password, confirm); err != nil {
			redirectSettings(w, r, "/settings/general", "err="+urlQuery(err.Error()))
			return
		}
		var err error
		hash, err = auth.HashPassword(password)
		if err != nil {
			redirectSettings(w, r, "/settings/general", "err="+urlQuery(err.Error()))
			return
		}
	}
	if _, err := settings.UpdateAuthCredentials(h.Queue.DB, username, hash, true); err != nil {
		redirectSettings(w, r, "/settings/general", "err="+urlQuery(err.Error()))
		return
	}
	if err := auth.IssueSession(w, r, h.Queue.DB, username); err != nil {
		redirectSettings(w, r, "/settings/general", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/general", "ok=saved")
}

func (h *Handler) actionRegenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if _, err := settings.RegenerateAPIKey(h.Queue.DB); err != nil {
		redirectSettings(w, r, "/settings/general", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/general", "ok="+urlQuery("API key regenerated"))
}

func (h *Handler) actionUpsertNotifyChannel(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	htmx := r.Header.Get("HX-Request") == "true"
	var id int64
	if raw := strings.TrimSpace(r.FormValue("id")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			if htmx {
				h.writeNotifyURLFieldError(w, r, "invalid channel id")
				return
			}
			redirectSettings(w, r, "/settings/connect", "err="+urlQuery("invalid channel id"))
			return
		}
		id = n
	}
	if notify.IsInAppURL(r.FormValue("url")) {
		msg := notify.ErrInAppChannelReadOnly.Error()
		if htmx {
			h.writeNotifyURLFieldError(w, r, msg)
			return
		}
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery(msg))
		return
	}
	events := r.Form["events"]
	_, err := notify.Upsert(h.Queue.DB, id, r.FormValue("name"), r.FormValue("url"), events)
	if err != nil {
		if htmx {
			h.writeNotifyURLFieldError(w, r, err.Error())
			return
		}
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery(err.Error()))
		return
	}
	ok := "notify-channel"
	if id > 0 {
		ok = "notify-channel-saved"
	}
	redir := settingsFormRedirect(r, "/settings/connect")
	sep := "?"
	if strings.Contains(redir, "?") {
		sep = "&"
	}
	target := redir + sep + "ok=" + ok
	if htmx {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *Handler) writeNotifyURLFieldError(w http.ResponseWriter, r *http.Request, msg string) {
	fieldID := "notify-url-field-add"
	formID := "modal-add-notify-channel-form"
	if idRaw := strings.TrimSpace(r.FormValue("id")); idRaw != "" {
		if id, err := strconv.ParseInt(idRaw, 10, 64); err == nil && id > 0 {
			fieldID = fmt.Sprintf("notify-url-field-%d", id)
			formID = fmt.Sprintf("modal-edit-notify-%d-form", id)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 200 so HTMX swaps the field fragment (4xx skips swap by default).
	render(w, "notify_url_field", map[string]any{
		"FieldID":  fieldID,
		"FormID":   formID,
		"URL":      r.FormValue("url"),
		"URLError": msg,
	})
}

func (h *Handler) actionDeleteNotifyChannel(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil || id <= 0 {
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery("invalid channel id"))
		return
	}
	if err := notify.Delete(h.Queue.DB, id); err != nil {
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/connect", "ok=notify-channel-deleted")
}

func (h *Handler) actionTestNotifyChannel(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	rawURL := strings.TrimSpace(r.FormValue("url"))
	if rawURL == "" {
		if idRaw := strings.TrimSpace(r.FormValue("id")); idRaw != "" {
			id, err := strconv.ParseInt(idRaw, 10, 64)
			if err == nil && id > 0 {
				if c, err := notify.Get(h.Queue.DB, id); err == nil {
					rawURL = c.URL
				}
			}
		}
	}
	if rawURL == "" {
		render(w, "flash_toast_oob", flashErr("Add an Apprise URL first."))
		return
	}
	if err := notify.ValidateURL(rawURL); err != nil {
		render(w, "flash_toast_oob", flashErr(err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := notify.Send(ctx, []string{rawURL}, "Creatorr", "test notification from creatorr"); err != nil {
		render(w, "flash_toast_oob", flashErr(err.Error()))
		return
	}
	render(w, "flash_toast_oob", flashOK("Test notification sent."))
}

func (h *Handler) actionSaveDomainDefault(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	delay, err := strconv.Atoi(r.FormValue("task_cooldown_seconds"))
	if err != nil || delay < 0 {
		h.respondDomainDefaultsSaveError(w, r, fmt.Errorf("invalid task_cooldown_seconds"))
		return
	}
	maxQueue, err := settings.ParsePositiveInt(r.FormValue("max_download_queue"), "max download tasks")
	if err != nil {
		h.respondDomainDefaultsSaveError(w, r, err)
		return
	}
	maxParallel, err := settings.ParsePositiveInt(r.FormValue("max_parallel_tasks"), "max parallel tasks")
	if err != nil {
		h.respondDomainDefaultsSaveError(w, r, err)
		return
	}
	rate, err := settings.CombineDownloadRateLimit(r.FormValue("download_rate_limit_value"), r.FormValue("download_rate_limit_unit"))
	if err != nil {
		h.respondDomainDefaultsSaveError(w, r, err)
		return
	}
	if err := settings.SetDomainDefault(h.Queue.DB, delay, maxQueue, maxParallel, rate, r.FormValue("sleep_requests"), false); err != nil {
		h.respondDomainDefaultsSaveError(w, r, err)
		return
	}
	// Access (Flare / cookies / credentials) is override-only; clear any legacy defaults jar/creds.
	_ = domains.ClearCookies(h.Queue.DB, settings.DomainDefault)
	if err := settings.SaveDefaultCredentials(h.Queue.DB, "", "", false); err != nil {
		h.respondDomainDefaultsSaveError(w, r, err)
		return
	}
	h.respondDomainDefaultsSaveOK(w, r)
}

func (h *Handler) actionUpsertDomainOverride(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := formDomain(r)
	if err := settings.ValidateOverrideDomain(domain); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	domain = settings.NormalizeDomain(domain)
	defLim, err := settings.DefaultLimits(h.Queue.DB)
	if err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	rate, err := settings.CombineDownloadRateLimitOverride(
		r.FormValue("download_rate_limit_value"),
		r.FormValue("download_rate_limit_unit"),
		defLim.DownloadRateLimit,
	)
	if err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	flareStr := "default"
	if strings.TrimSpace(h.FlareSolverrURL) != "" {
		if v := strings.TrimSpace(r.FormValue("use_flaresolverr")); v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true") {
			flareStr = "on"
		}
	}
	if err := domains.UpdateHostOverrides(h.Queue.DB, domain,
		r.FormValue("task_cooldown_seconds"),
		r.FormValue("max_download_queue"),
		r.FormValue("max_parallel_tasks"),
		rate,
		r.FormValue("sleep_requests"),
		flareStr,
	); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	if err := h.saveDomainCookies(domain, r.FormValue("cookies")); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	if err := domains.SetCookiesAfterFail(h.Queue.DB, domain, domains.ValidateCookiesAfterFailForm(r.FormValue("cookies_after_fail"))); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	inheritCreds := r.FormValue("credentials_inherit") == "1"
	credUser := strings.TrimSpace(r.FormValue("username"))
	credPass := r.FormValue("password")
	touchCreds := inheritCreds || credUser != "" || credPass != ""
	if !touchCreds {
		if meta, ok, err := domains.Get(h.Queue.DB, domain); err == nil && ok && meta.Username.Valid {
			touchCreds = true
		}
	}
	if touchCreds {
		keepPassword := false
		if !inheritCreds && credUser != "" {
			hasStored, _ := settings.HostHasStoredPassword(h.Queue.DB, domain)
			keepPassword = hasStored && credPass == "" && r.FormValue("password_keep") == "1"
		}
		if err := settings.SaveHostCredentials(h.Queue.DB, domain, credUser, credPass, inheritCreds, keepPassword); err != nil {
			redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
			return
		}
	}
	redirectSettings(w, r, "/settings/queue", "ok=domain")
}

func (h *Handler) actionDeleteDomainOverride(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := settings.NormalizeDomain(r.FormValue("domain"))
	if err := domains.Delete(h.Queue.DB, domain); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/queue", "ok=domain-deleted")
}

func (h *Handler) actionSaveCookie(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := formDomain(r)
	if err := h.saveDomainCookies(domain, r.FormValue("content")); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/queue", "ok=cookie")
}

func (h *Handler) saveDomainCookies(domain, content string) error {
	domain = settings.NormalizeDomain(domain)
	if domain == "" {
		return fmt.Errorf("domain required")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return domains.ClearCookies(h.Queue.DB, domain)
	}
	return domains.SetCookies(h.Queue.DB, domain, content)
}

func (h *Handler) actionDeleteCookie(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_ = domains.ClearCookies(h.Queue.DB, strings.TrimSpace(r.FormValue("domain")))
	redirectSettings(w, r, "/settings/queue", "ok=cookie-deleted")
}

func (h *Handler) actionAddRoot(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ttl, err := parseRetentionTTLDays(r.FormValue("retention_ttl_days"))
	if err != nil {
		redirectOrJSONRootErr(w, r, err)
		return
	}
	epFmt := strings.TrimSpace(r.FormValue("episode_format"))
	_, err = h.Library.CreateRoot(strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("path")), epFmt, ttl)
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
	if _, ok := r.Form["episode_format"]; ok {
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
	var epPtr *string
	if _, ok := r.Form["episode_format"]; ok {
		epPtr = &epFmt
	}
	_, err := h.Library.UpdateRoot(id, &name, &path, epPtr, retention, clearRetention)
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
	mediaPreset, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("maturity_media_preset")))
	hours := library.MaturityMediaHoursForPreset(mediaPreset)
	sidecarPreset, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("maturity_sidecar_preset")))
	days := library.MaturitySidecarDaysForPreset(sidecarPreset)
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
	_, err := h.Library.CreateProfileFull(
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
	mediaPreset, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("maturity_media_preset")))
	hours := library.MaturityMediaHoursForPreset(mediaPreset)
	sidecarPreset, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("maturity_sidecar_preset")))
	days := library.MaturitySidecarDaysForPreset(sidecarPreset)
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
	_, err := h.Library.UpdateProfileParams(id, library.UpdateProfileParams{
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
	order := []string{"apply_episode_naming", "regenerate_nfos", "sync_files", "integrity_check", "refresh_sidecars"}
	var queued []string
	var skipMsgs []string
	var firstErr string
	refreshQueued, refreshSkipped := 0, 0

	for _, key := range order {
		if !wanted[key] {
			continue
		}
		switch key {
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
			id, err := h.Library.EnqueueSyncFiles(queue.PrioritySyncFilesDue, queue.OriginManual)
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
	id, err := h.Library.EnqueueYtDlpUpdate(queue.PriorityYtDlpUpdateDue, queue.OriginManual)
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
