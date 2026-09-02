package web

import (
	"net/http"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func (h *Handler) isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func (h *Handler) respondSettingsSaveOK(w http.ResponseWriter, r *http.Request) {
	if h.isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		render(w, "flash_toast_oob", flashOK("Settings saved."))
		return
	}
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = "/settings/connect"
	}
	redirectSettings(w, r, redir, "ok=saved")
}

func invalidCronKeyFromForm(r *http.Request) string {
	for key := range settings.CronKeys {
		if _, ok := r.Form[key]; !ok {
			continue
		}
		if err := cronexpr.Validate(cronValueFromForm(r, key)); err != nil {
			return key
		}
	}
	return ""
}

func (h *Handler) respondSettingsSaveError(w http.ResponseWriter, r *http.Request, err error) {
	msg := err.Error()
	if h.isHTMX(r) {
		if key := invalidCronKeyFromForm(r); key != "" {
			h.writeCronFieldError(w, r, key, msg)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		render(w, "flash_toast_oob", flashErr(msg))
		return
	}
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = "/settings/general"
	}
	redirectSettings(w, r, redir, "err="+urlQuery(msg))
}

func (h *Handler) respondDomainDefaultsSaveOK(w http.ResponseWriter, r *http.Request) {
	if h.isHTMX(r) {
		defLim, err := settings.DefaultLimits(h.Queue.DB)
		if err != nil {
			h.respondDomainDefaultsSaveError(w, r, err)
			return
		}
		defLim.UseFlareSolverr = false
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		render(w, "domain_defaults_autosave_oob", map[string]any{
			"Flash":         flashOK("Domain defaults saved."),
			"DefaultLimits": defLim,
		})
		return
	}
	redirectSettings(w, r, "/settings/queue", "ok=domain-defaults")
}

func (h *Handler) respondDomainDefaultsSaveError(w http.ResponseWriter, r *http.Request, err error) {
	msg := err.Error()
	if h.isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		render(w, "flash_toast_oob", flashErr(msg))
		return
	}
	redirectSettings(w, r, "/settings/queue", "err="+urlQuery(msg))
}

func cronValueFromForm(r *http.Request, key string) string {
	return strings.TrimSpace(r.FormValue(key))
}

func (h *Handler) writeCronFieldError(w http.ResponseWriter, r *http.Request, key, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Settings-Cron-Error", "1")
	w.Header().Set("X-Settings-Cron-Key", key)
	w.WriteHeader(http.StatusUnprocessableEntity)
	render(w, "settings_cron_field", map[string]any{
		"Key":             key,
		"Label":           settings.Labels[key],
		"Help":            settings.Help[key],
		"Value":           cronValueFromForm(r, key),
		"Error":           msg,
		"CronDescriptors": cronexpr.Descriptors(),
	})
}
