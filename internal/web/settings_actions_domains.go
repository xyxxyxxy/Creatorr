package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

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
	flareOn := false
	if strings.TrimSpace(h.FlareSolverrURL) != "" {
		if v := strings.TrimSpace(r.FormValue("use_flaresolverr")); v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true") {
			flareStr = "on"
			flareOn = true
		}
	}
	cookieContent := r.FormValue("cookies")
	if err := domains.RejectFlareWithCookies(flareOn, cookieContent); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
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
	if err := h.saveDomainCookies(domain, cookieContent); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	if err := domains.SetSmartCookies(h.Queue.DB, domain, domains.ValidateSmartCookiesForm(r.FormValue("smart_cookies"))); err != nil {
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
