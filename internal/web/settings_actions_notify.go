package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
)

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
