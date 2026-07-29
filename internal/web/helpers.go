package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

type pageBase struct {
	Title        string
	Nav          string
	Icon         string // lucide icon for page title headers
	SettingsTab  string // set when Nav == "settings" (general|library|connect|queue|scheduler|maintenance)
	Flash        *flash
	AuthUsername string // operator account; for navbar account menu
}

// usernameForPage returns the operator username for shell chrome (set from Handler.Mount).
var usernameForPage func() string

// SetUsernameForPage wires the operator username lookup used by newPage.
func SetUsernameForPage(fn func() string) {
	usernameForPage = fn
}

// EpisodeLucideIcon is the Lucide name for indexed episodes (library videos).
// Packed media files (kind=video) use "film" via sidecarKindIcon instead.
const EpisodeLucideIcon = "square-play"

// newPage builds pageBase with a nav-matched lucide icon.
func newPage(title, nav string, flash *flash) pageBase {
	p := pageBase{Title: title, Nav: nav, Icon: pageIcon(nav), Flash: flash}
	if usernameForPage != nil {
		p.AuthUsername = usernameForPage()
	}
	return p
}

func newSettingsPage(title, tab string, flash *flash) pageBase {
	p := newPage(title, "settings", flash)
	p.SettingsTab = tab
	p.Icon = settingsTabIcon(tab)
	return p
}

// pageIcon is the Lucide name for top-level nav / page titles (keep in sync with nav.html).
func pageIcon(nav string) string {
	switch nav {
	case "overview":
		return "layout-dashboard"
	case "series":
		return "tv"
	case "import":
		return "folder-input"
	case "tasks":
		return "list-todo"
	case "stats":
		return "chart-column"
	case "history":
		return "history"
	case "settings":
		return "settings"
	default:
		return ""
	}
}

// settingsTabIcon is the Lucide name for Settings sub-page titles (keep in sync with nav.html).
func settingsTabIcon(tab string) string {
	switch tab {
	case "general":
		return "sliders-horizontal"
	case "connect":
		return "plug"
	case "library":
		return "folder"
	case "maintenance":
		return "wrench"
	case "scheduler":
		return "calendar-clock"
	case "queue":
		return "list-ordered"
	case "domains":
		return "globe"
	default:
		return "settings"
	}
}

type flash struct {
	Message string
	Warning bool
	Error   bool
}

func flashOK(msg string) *flash   { return &flash{Message: msg} }
func flashWarn(msg string) *flash { return &flash{Message: msg, Warning: true} }
func flashErr(msg string) *flash  { return &flash{Message: msg, Error: true} }

// DisplayURL formats a URL for UI text: strip https:// and leading www. (any case);
// keep other schemes (still strip www. after ://).
func DisplayURL(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= len("https://") && strings.EqualFold(s[:len("https://")], "https://") {
		return stripWWWHost(s[len("https://"):])
	}
	if i := strings.Index(s, "://"); i >= 0 {
		return s[:i+3] + stripWWWHost(s[i+3:])
	}
	return stripWWWHost(s)
}

func stripWWWHost(s string) string {
	if len(s) >= 4 && strings.EqualFold(s[:4], "www.") {
		return s[4:]
	}
	return s
}

func flashFromQuery(r *http.Request) *flash {
	if e := r.URL.Query().Get("err"); e != "" {
		e = strings.TrimPrefix(e, "conflict: ")
		e = strings.TrimPrefix(e, "invalid: ")
		return flashErr(e)
	}
	ok := r.URL.Query().Get("ok")
	switch {
	case ok == "scan-for-new":
		return flashOK("Scan enqueued.")
	case ok == "history-scan":
		return flashOK("Full scan enqueued.")
	case strings.HasPrefix(ok, "scan-for-new-"):
		n := strings.TrimPrefix(ok, "scan-for-new-")
		return flashOK("Scan enqueued (" + n + ").")
	case ok == "restart-history":
		return flashOK("Full scan enqueued.")
	case strings.HasPrefix(ok, "restart-history-"):
		n := strings.TrimPrefix(ok, "restart-history-")
		return flashOK("Full scan enqueued (" + n + ").")
	case strings.HasPrefix(ok, "scan-"):
		n := strings.TrimPrefix(ok, "scan-")
		return flashOK("Scan enqueued (" + n + ").")
	}
	switch ok {
	case "updated":
		return flashOK("Series updated.")
	case "source":
		return flashOK("Source added.")
	case "source-updated":
		return flashOK("Source updated.")
	case "scan":
		return flashOK("Scan enqueued.")
	case "metadata-rescan":
		return flashOK("Metadata rescan enqueued.")
	case "refresh-sidecars":
		return flashOK("Sidecar refresh enqueued.")
	case "metadata":
		return flashOK("Series metadata saved (tvshow.nfo + art).")
	case "video-metadata":
		return flashOK("Episode metadata saved.")
	case "video-metadata-busy":
		return flashOK("Episode metadata saved. Rename skipped while a download or pack task is busy - run Apply episode format later.")
	case "download":
		return flashOK("Download now enqueued.")
	case "video-deleted":
		return flashOK("Video delete queued - files remove in the background.")
	case "sidecar-deleted":
		return flashOK("Sidecar deleted.")
	case "retry":
		return flashOK("Source errors cleared; videos set to wanted.")
	case "deleted":
		return flashOK("Series deleted.")
	case "saved":
		return flashOK("Settings saved.")
	case "cookie":
		return flashOK("Cookies saved.")
	case "cookie-deleted":
		return flashOK("Cookies deleted.")
	case "root":
		return flashOK("Root folder added.")
	case "root-updated":
		return flashOK("Root folder updated.")
	case "profile":
		return flashOK("Quality profile added.")
	case "profile-updated":
		return flashOK("Quality profile updated.")
	case "profile-deleted":
		return flashOK("Quality profile deleted.")
	case "nfo-regen-queued":
		return flashOK("NFO regenerate queued.")
	case "sync-files-queued":
		return flashOK("File sync queued.")
	case "sync-files-empty":
		return flashWarn("No videos to sync.")
	case "nfo-regen":
		rewrote := r.URL.Query().Get("rewrote")
		failed := r.URL.Query().Get("failed")
		msg := "Regenerated " + rewrote + " NFO file(s)."
		if failed != "" && failed != "0" {
			msg = "Regenerated " + rewrote + " NFO file(s); " + failed + " failed."
			return flashWarn(msg)
		}
		return flashOK(msg)
	case "apply-naming":
		return flashOK("'Apply episode format' queued.")
	case "delete-queued":
		return flashOK("Delete queued - files remove in the background.")
	case "domain-queue":
		return flashOK("Domain queue limits saved.")
	case "handler":
		return flashOK("Domain handler saved.")
	case "handler-deleted":
		return flashOK("Domain handler deleted.")
	case "handler-updated":
		return flashOK("Domain handler updated.")
	case "handler-cron":
		return flashOK("Handler update schedule saved.")
	case "default-handler":
		return flashOK("Default handler saved.")
	}
	return nil
}

// formDomain reads the Domain text field (preset select only fills this input).
func formDomain(r *http.Request) string {
	return settings.NormalizeDomain(r.FormValue("domain"))
}

// clampPastDate keeps YYYY-MM-DD on or before today UTC; empty stays empty.
func clampPastDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	today := time.Now().UTC().Format("2006-01-02")
	if s > today {
		return today
	}
	return s
}

func urlQuery(s string) string {
	return url.QueryEscape(s)
}

func scanCronDescriptors() []string {
	return cronexpr.ScanDescriptors()
}

// parseFeedScanCron reads scan_cron (or legacy scan_cron_schedule). emptyDefault used when both empty (add flows).
func parseFeedScanCron(r *http.Request, emptyDefault string) (string, error) {
	raw := strings.TrimSpace(r.FormValue("scan_cron"))
	if raw == "" {
		raw = strings.TrimSpace(r.FormValue("scan_cron_schedule"))
	}
	if raw == "" {
		raw = emptyDefault
	}
	return cronexpr.NormalizeScanCron(raw)
}

// seriesSourceRedirect returns a safe post-action path for source forms.
// Honors redirect when it is under /series/; otherwise the series page.
func seriesSourceRedirect(r *http.Request, seriesID, _ int64) string {
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if strings.HasPrefix(redir, "/series/") && !strings.Contains(redir, "://") {
		return redir
	}
	return fmt.Sprintf("/series/%d", seriesID)
}

func appendQuery(path, query string) string {
	if query == "" {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + query
}

func hxRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func hxRedirect(w http.ResponseWriter, url string) {
	w.Header().Set("HX-Redirect", url)
	w.WriteHeader(http.StatusOK)
}

// finishVideoAction redirects after a video action, or when HTMX targeted
// #series-videos-live, returns that partial (no full-page reload).
func (h *Handler) finishVideoAction(w http.ResponseWriter, r *http.Request, sid int64, redir string, err error) {
	if redir == "" {
		redir = fmt.Sprintf("/series/%d", sid)
	}
	if err != nil {
		errURL := appendQuery(redir, "err="+urlQuery(err.Error()))
		if hxRequest(r) {
			if h.tryRenderSeriesVideosLive(w, r, sid) {
				return
			}
			hxRedirect(w, errURL)
			return
		}
		http.Redirect(w, r, errURL, http.StatusSeeOther)
		return
	}
	if hxRequest(r) {
		if h.tryRenderSeriesVideosLive(w, r, sid) {
			return
		}
		hxRedirect(w, redir)
		return
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

// tryRenderSeriesVideosLive renders the series video list partial when the
// HTMX request targeted #series-videos-live. Uses HX-Current-URL for filters/page.
func (h *Handler) tryRenderSeriesVideosLive(w http.ResponseWriter, r *http.Request, sid int64) bool {
	if r.Header.Get("HX-Target") != "series-videos-live" {
		return false
	}
	if sid < 1 {
		return false
	}
	ser, err := h.Library.GetSeries(sid, false)
	if err != nil {
		return false
	}
	req := r
	if cur := strings.TrimSpace(r.Header.Get("HX-Current-URL")); cur != "" {
		if u, perr := url.Parse(cur); perr == nil && u != nil {
			clone := r.Clone(r.Context())
			clone.URL = u
			clone.RequestURI = u.RequestURI()
			req = clone
		}
	}
	activeTasks, _ := h.Queue.ListActiveForSeries(sid)
	seriesTasks, _, byVideo := seriesActivityMaps(activeTasks)
	h.mergeFileDeleteForSeries(sid, &seriesTasks, byVideo)
	data, err := h.loadSeriesVideosLive(req, ser, byVideo)
	if err != nil {
		return false
	}
	render(w, "series_videos_live", data)
	return true
}

func renderMonitorToggle(w http.ResponseWriter, data map[string]any) {
	render(w, "monitor_toggle", data)
}
