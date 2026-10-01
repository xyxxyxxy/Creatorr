package web

import (
	"net/http"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/auth"
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
	}
	if r.FormValue("redirect") == "/settings/queue" {
		if r.FormValue("archive_fallback_settings") == "1" {
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
