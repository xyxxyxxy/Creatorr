package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

type settingsRowView struct {
	Key           string
	Label         string
	Value         string
	Help          string
	Cron          bool
	CronDefault   string
	Checkbox      bool
	Checked       bool
	Select        bool // closed-set dropdown
	Options       []PresetOption
	Textarea      bool
	Wide          bool
	Disabled      bool
	DisabledTitle string
}

// profileSettingsRow is a quality profile plus series usage for Settings → Library.
type profileSettingsRow struct {
	library.QualityProfile
	SeriesCount int
}

// rootSettingsRow is a root folder plus series usage for Settings → Library.
type rootSettingsRow struct {
	library.RootFolder
	SeriesCount int
}

type notifyChannelView struct {
	ID          int64
	Name        string
	URL         string
	URLMasked   string
	Events      []string
	EventLabels []string
	InApp       bool // fixed Creatorr channel: no edit/delete
}

type notifyEventOption struct {
	ID    string
	Label string
}

type notifyEventGroupView struct {
	Level     string
	Label     string
	Icon      string
	IconClass string
	Options   []notifyEventOption
}

func notifyEventGroups() []notifyEventGroupView {
	groups := []notifyEventGroupView{
		{Level: notify.LevelAlert, Label: "Alert", Icon: "megaphone", IconClass: "text-error"},
		{Level: notify.LevelWarning, Label: "Warning", Icon: "siren", IconClass: "text-warning"},
		{Level: notify.LevelInfo, Label: "Info", Icon: "bell", IconClass: "opacity-70"},
	}
	byLevel := map[string]*notifyEventGroupView{
		notify.LevelAlert:   &groups[0],
		notify.LevelWarning: &groups[1],
		notify.LevelInfo:    &groups[2],
	}
	for _, id := range notify.EventsSortedByLevel() {
		g := byLevel[notify.EventLevel(id)]
		if g == nil {
			continue
		}
		g.Options = append(g.Options, notifyEventOption{ID: id, Label: notify.EventLabels[id]})
	}
	return groups
}

func maskAppriseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= 48 {
		return raw
	}
	return raw[:28] + "…" + raw[len(raw)-12:]
}

func notifyEventsAll(events []string) bool {
	return notify.HasAllSubscription(events)
}

func (h *Handler) settingsRedirect(w http.ResponseWriter, r *http.Request) {
	q := r.URL.RawQuery
	target := "/settings/general"
	if q != "" {
		target += "?" + q
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func settingsFormRedirect(r *http.Request, defaultPath string) string {
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if strings.HasPrefix(redir, "/settings/") || redir == "/tasks" {
		return redir
	}
	return defaultPath
}

func redirectSettings(w http.ResponseWriter, r *http.Request, defaultPath, query string) {
	base := settingsFormRedirect(r, defaultPath)
	if query == "" {
		http.Redirect(w, r, base, http.StatusSeeOther)
		return
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	http.Redirect(w, r, base+sep+query, http.StatusSeeOther)
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json") ||
		r.FormValue("response") == "json" ||
		r.URL.Query().Get("response") == "json"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func operatorErrMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "invalid: ")
	msg = strings.TrimPrefix(msg, "conflict: ")
	return msg
}

// rootFormFieldFromErr maps CreateRoot/UpdateRoot errors to a form control name.
func rootFormFieldFromErr(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "path"):
		return "path"
	case strings.Contains(msg, "episode"):
		return "episode_format"
	case strings.Contains(msg, "retention"):
		return "retention_ttl_days"
	default:
		return ""
	}
}

func redirectOrJSONRootErr(w http.ResponseWriter, r *http.Request, err error) {
	msg := operatorErrMessage(err)
	if wantsJSON(r) {
		out := map[string]string{"error": msg}
		if f := rootFormFieldFromErr(err); f != "" {
			out["field"] = f
		}
		writeJSON(w, http.StatusBadRequest, out)
		return
	}
	redirectSettings(w, r, "/settings/library", "err="+urlQuery(err.Error()))
}

func redirectOrJSONRootOK(w http.ResponseWriter, r *http.Request, okQuery string) {
	base := settingsFormRedirect(r, "/settings/library")
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	loc := base + sep + okQuery
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "1", "redirect": loc})
		return
	}
	http.Redirect(w, r, loc, http.StatusSeeOther)
}

func (h *Handler) settingsGeneral(w http.ResponseWriter, r *http.Request) {
	authUser, _ := settings.AuthUsername(h.Queue.DB)
	apiKey, _ := settings.APIKey(h.Queue.DB)
	render(w, "settings_general", struct {
		pageBase
		AuthUsername string
		APIKey       string
	}{
		pageBase:     newSettingsPage("Settings · General", "general", flashFromQuery(r)),
		AuthUsername: authUser,
		APIKey:       apiKey,
	})
}
