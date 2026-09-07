package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/auth"
	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/health"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
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

func (h *Handler) ytdlpConnectControlsView() ytdlpConnectControlsView {
	updatesEnabled, _ := settings.YtDlpUpdatesEnabled(h.Queue.DB)
	updateBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindYtDlpUpdate, queue.SystemDomain)
	lastCheckedAt, _ := h.Queue.LastFinishedAt(queue.KindYtDlpUpdate, queue.SystemDomain, queue.StatusDone)
	return ytdlpConnectControlsView{
		YtDlpLastCheckedAt: lastCheckedAt,
		YtDlpUpdatesOn:     updatesEnabled,
		YtDlpUpdateBusy:    updateBusy,
		YtDlpUpdateBusyTip: "yt-dlp update already queued or running",
		YtDlpChannel:       h.ytdlpUpdateChannelRow(),
	}
}

type ytdlpConnectControlsView struct {
	YtDlpLastCheckedAt string
	YtDlpUpdatesOn     bool
	YtDlpUpdateBusy    bool
	YtDlpUpdateBusyTip string
	YtDlpChannel       settingsRowView
}

type ytdlpInstalledVersionView struct {
	Pending     bool
	Value       string
	Error       bool
	ErrorDetail string
}

func ytdlpInstalledVersionErrorLabel(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "missing") {
		return "Binary not found"
	}
	if strings.Contains(msg, "--version failed") {
		return "Binary check failed"
	}
	return "Binary unavailable"
}

func (h *Handler) ytdlpInstalledVersionView() ytdlpInstalledVersionView {
	if installedVer, _ := settings.Get(h.Queue.DB, settings.KeyYtDlpInstalledVersion); strings.TrimSpace(installedVer) != "" {
		return ytdlpInstalledVersionView{Value: installedVer}
	}
	if h.YtDlp == nil {
		return ytdlpInstalledVersionView{
			Error:       true,
			Value:       "Binary not found",
			ErrorDetail: "yt-dlp client unavailable",
		}
	}
	ver, err := ytdlp.VerifyBinary(h.YtDlp.BinPath())
	if err != nil {
		return ytdlpInstalledVersionView{
			Error:       true,
			Value:       ytdlpInstalledVersionErrorLabel(err),
			ErrorDetail: err.Error(),
		}
	}
	return ytdlpInstalledVersionView{Value: ver}
}

func (h *Handler) settingsConnectYtDlpInstalledVersion(w http.ResponseWriter, r *http.Request) {
	render(w, "ytdlp_connect_installed_version", h.ytdlpInstalledVersionView())
}

func (h *Handler) settingsConnectYtDlpLastChecked(w http.ResponseWriter, r *http.Request) {
	render(w, "ytdlp_connect_last_checked", h.ytdlpConnectControlsView())
}

func (h *Handler) ytdlpUpdateChannelRow() settingsRowView {
	val, _ := settings.Get(h.Queue.DB, settings.KeyYtDlpUpdateChannel)
	row := settingsRowView{
		Key:   settings.KeyYtDlpUpdateChannel,
		Label: settings.Labels[settings.KeyYtDlpUpdateChannel],
		Value: settings.NormalizeYtDlpUpdateChannel(val),
		Help:  settings.Help[settings.KeyYtDlpUpdateChannel],
		Select: true,
	}
	for _, o := range settings.YtDlpUpdateChannelOptions() {
		row.Options = append(row.Options, PresetOption{Value: o.Value, Label: o.Label})
	}
	return row
}

func (h *Handler) settingsConnect(w http.ResponseWriter, r *http.Request) {
	entries, err := settings.Connect(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	updatesEnabled, _ := settings.YtDlpUpdatesEnabled(h.Queue.DB)
	ytdlpControls := h.ytdlpConnectControlsView()
	rows := make([]settingsRowView, 0, len(entries))
	potURLSet := strings.TrimSpace(h.PotProviderURL) != ""
	for _, e := range entries {
		if e.Key == settings.KeyYtDlpUpdateChannel {
			continue
		}
		row := settingsRowView{
			Key: e.Key, Label: e.Label, Value: e.Value, Help: e.Help,
			Cron: settings.CronKeys[e.Key],
		}
		if e.Key == settings.KeyPotFetch {
			row.Select = true
			row.Value = settings.NormalizePotFetch(e.Value)
			for _, o := range settings.PotFetchOptions() {
				row.Options = append(row.Options, PresetOption{Value: o.Value, Label: o.Label})
			}
			if !potURLSet {
				row.Disabled = true
				row.Label = row.Label + " (disabled)"
				row.Value = settings.PotFetchNever
				row.DisabledTitle = "Set CREATORR_POT_PROVIDER_URL first (Compose default http://creatorr-po-token:4416)."
			}
		}
		rows = append(rows, row)
	}
	potJoin := externalServiceURLViewPending(h, false)
	flareJoin := externalServiceURLViewPending(h, true)
	channels, err := notify.List(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	chViews := make([]notifyChannelView, 0, len(channels))
	for _, c := range channels {
		var labels []string
		if notifyEventsAll(c.Events) {
			labels = []string{"all"}
		} else {
			labels = make([]string, 0, len(c.Events))
			for _, ev := range c.Events {
				if l, ok := notify.EventLabels[ev]; ok {
					labels = append(labels, l)
				} else {
					labels = append(labels, ev)
				}
			}
		}
		chViews = append(chViews, notifyChannelView{
			ID: c.ID, Name: c.Name, URL: c.URL, URLMasked: maskAppriseURL(c.URL),
			Events: c.Events, EventLabels: labels, InApp: notify.IsInAppChannel(c),
		})
	}
	evGroups := notifyEventGroups()
	render(w, "settings_connect", struct {
		pageBase
		FlareService          externalServiceURLView
		PotService            externalServiceURLView
		Settings              []settingsRowView
		NotifyChannels        []notifyChannelView
		EventGroups           []notifyEventGroupView
		DefaultEvents         []string
		YtDlpUpdatesOn        bool
		YtDlpInstalledVersion ytdlpInstalledVersionView
		YtDlpControls         ytdlpConnectControlsView
	}{
		pageBase:              newSettingsPage("Settings · Connect", "connect", flashFromQuery(r)),
		FlareService:          flareJoin,
		PotService:            potJoin,
		Settings:              rows,
		NotifyChannels:        chViews,
		EventGroups:           evGroups,
		DefaultEvents:         []string{notify.EventAll},
		YtDlpUpdatesOn:        updatesEnabled,
		YtDlpInstalledVersion: ytdlpInstalledVersionView{Pending: true},
		YtDlpControls:         ytdlpControls,
	})
}

type externalServiceURLView struct {
	Label          string
	Value          string
	Hint           string
	Status         string
	StatusLabel    string
	StatusTip      string
	HealthTargetID string
	HealthURL      string
}

func externalServiceURLViewBase(h *Handler, flare bool) externalServiceURLView {
	if flare {
		return externalServiceURLView{
			Label: "FlareSolverr URL",
			Value: strings.TrimSpace(h.FlareSolverrURL),
			Hint:  "Set CREATORR_FLARESOLVERR_URL and restart.\nEnable 'Use FlareSolverr' On a host 'Domain override' ('Settings → Queue / Domains').",
		}
	}
	return externalServiceURLView{
		Label: "PO token provider URL",
		Value: strings.TrimSpace(h.PotProviderURL),
		Hint:  "Set CREATORR_POT_PROVIDER_URL and restart.\nEnable 'PO token fetch' below when the URL is set (forced to 'Never' while unset).",
	}
}

func externalServiceURLViewPending(h *Handler, flare bool) externalServiceURLView {
	v := externalServiceURLViewBase(h, flare)
	v.Status = "pending"
	v.StatusLabel = "Checking"
	v.StatusTip = "Probing service health"
	if flare {
		v.HealthTargetID = "connect-flare-service-health"
		v.HealthURL = "/settings/connect/external-services/flare"
	} else {
		v.HealthTargetID = "connect-pot-service-health"
		v.HealthURL = "/settings/connect/external-services/pot"
	}
	return v
}

func (h *Handler) settingsConnectExternalServiceHealth(w http.ResponseWriter, r *http.Request) {
	service := strings.TrimSpace(chi.URLParam(r, "service"))
	flare := service == "flare"
	if service != "pot" && service != "flare" {
		http.NotFound(w, r)
		return
	}
	v := externalServiceURLViewBase(h, flare)
	if flare {
		v.HealthTargetID = "connect-flare-service-health"
	} else {
		v.HealthTargetID = "connect-pot-service-health"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	var ch health.Check
	if h.Health == nil {
		ch = health.Check{Status: health.StatusSkipped, Message: "URL unset"}
		if v.Value != "" {
			ch = health.Check{Status: health.StatusDegraded, Message: "Health checker unavailable"}
		}
	} else if flare {
		ch = h.Health.ProbeFlareSolverr(ctx)
	} else {
		ch = h.Health.ProbePotProvider(ctx)
	}
	v.Status, v.StatusLabel, v.StatusTip = externalServiceStatusFromCheck(ch)
	render(w, "external_service_status_slot", v)
}

func externalServiceStatusFromCheck(ch health.Check) (status, label, tip string) {
	switch ch.Status {
	case health.StatusOK:
		return string(health.StatusOK), "Healthy", "Ready to use"
	case health.StatusDegraded, health.StatusDown:
		tip = strings.TrimSpace(ch.Message)
		if tip == "" {
			tip = "Probe failed"
		}
		return string(health.StatusDegraded), "Unreachable", tip
	default:
		tip = "Set the environment variable and restart"
		if m := strings.TrimSpace(ch.Message); m != "" && m != "URL unset" {
			tip = m
		}
		return string(health.StatusSkipped), "Not configured", tip
	}
}

func (h *Handler) settingsScheduler(w http.ResponseWriter, r *http.Request) {
	entries, err := settings.Scheduler(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	rows := make([]settingsRowView, 0, len(entries))
	for _, e := range entries {
		row := settingsRowView{
			Key: e.Key, Label: e.Label, Value: e.Value, Help: e.Help,
			Cron: settings.CronKeys[e.Key], CronDefault: settings.CronSeedDefault(e.Key),
		}
		rows = append(rows, row)
	}
	render(w, "settings_scheduler", struct {
		pageBase
		Settings        []settingsRowView
		CronDescriptors []string
	}{
		pageBase:        newSettingsPage("Settings · Scheduler", "scheduler", flashFromQuery(r)),
		Settings:        rows,
		CronDescriptors: cronexpr.Descriptors(),
	})
}

func (h *Handler) settingsQueue(w http.ResponseWriter, r *http.Request) {
	defLim, _ := settings.DefaultLimits(h.Queue.DB)
	dqRows, _ := settings.DomainOverrideRows(h.Queue.DB)
	pageRows, pageInfo := SlicePage(r, "page", dqRows)
	sourceDomains, _ := h.Library.ListSourceDomains()
	// Match Handler.FlareSolverrURL (process bootstrap), not a live env re-read.
	flareOK := strings.TrimSpace(h.FlareSolverrURL) != ""
	// Access is override-only; Domain defaults Flare is always off for inherit display.
	defLim.UseFlareSolverr = false
	render(w, "settings_queue", struct {
		pageBase
		DefaultLimits   settings.DomainLimits
		DefaultUsername string
		DomainOverrides []settings.DomainQueueRow
		Page            PageInfo
		DomainDatalist  []string
		FlareConfigured bool
	}{
		pageBase:        newSettingsPage("Settings · Queue / Domains", "queue", flashFromQuery(r)),
		DefaultLimits:   defLim,
		DefaultUsername: "",
		DomainOverrides: pageRows,
		Page:            pageInfo,
		DomainDatalist:  sourceDomains,
		FlareConfigured: flareOK,
	})
}

func (h *Handler) settingsLibrary(w http.ResponseWriter, r *http.Request) {
	applyBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRenameEpisodes, queue.SystemDomain)
	roots, _ := h.Library.ListRoots()
	profiles, _ := h.Library.ListProfiles()
	seriesByRoot, _ := h.Library.SeriesCountsByRoot()
	seriesByProfile, _ := h.Library.SeriesCountsByProfile()
	pageRoots, rootsPage := SlicePage(r, "page", roots)
	pageProfiles, profilesPage := SlicePage(r, "profiles_page", profiles)
	rootRows := make([]rootSettingsRow, 0, len(pageRoots))
	for _, root := range pageRoots {
		rootRows = append(rootRows, rootSettingsRow{
			RootFolder:  root,
			SeriesCount: seriesByRoot[root.ID],
		})
	}
	profileRows := make([]profileSettingsRow, 0, len(pageProfiles))
	for _, p := range pageProfiles {
		profileRows = append(profileRows, profileSettingsRow{
			QualityProfile: p,
			SeriesCount:    seriesByProfile[p.ID],
		})
	}
	entries, err := settings.LibrarySettings(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	metadataDomainTag, _ := settings.MetadataDomainTagEnabled(h.Queue.DB)
	metadataGenresFromCategories, _ := settings.MetadataGenresFromCategoriesEnabled(h.Queue.DB)
	archiveFallback, _ := settings.ArchiveFallbackEnabled(h.Queue.DB)
	settingRows := make([]settingsRowView, 0, len(entries))
	subtitleLangs := settings.ParseSubtitleLangsJSON(settings.DefaultSubtitleLangs)
	subtitleAuto := false
	for _, e := range entries {
		switch e.Key {
		case settings.KeySubtitleLangs:
			subtitleLangs = settings.ParseSubtitleLangsJSON(e.Value)
			if e.Value == "" {
				subtitleLangs = settings.ParseSubtitleLangsJSON(settings.DefaultSubtitleLangs)
			}
			continue
		case settings.KeySubtitleAuto:
			subtitleAuto = settings.NormalizeSubtitleAuto(e.Value) == "1"
			continue
		}
		row := settingsRowView{
			Key: e.Key, Label: e.Label, Value: e.Value, Help: e.Help,
		}
		settingRows = append(settingRows, row)
	}
	render(w, "settings_library", struct {
		pageBase
		Settings                     []settingsRowView
		NamingLocked                 bool
		DefaultEpisodeFormat         string
		Roots                        []rootSettingsRow
		RootsPage                    PageInfo
		Profiles                     []profileSettingsRow
		ProfilesPage                 PageInfo
		SubtitleLangs                []string
		SubtitleLangOptions          []string
		SubtitleAuto                 bool
		MetadataDomainTag            bool
		MetadataGenresFromCategories bool
		ArchiveFallback              bool
	}{
		pageBase:                     newSettingsPage("Settings · Library", "library", flashFromQuery(r)),
		Settings:                     settingRows,
		NamingLocked:                 applyBusy,
		DefaultEpisodeFormat:         library.DefaultEpisodeFormat,
		Roots:                        rootRows,
		RootsPage:                    rootsPage,
		Profiles:                     profileRows,
		ProfilesPage:                 profilesPage,
		SubtitleLangs:                subtitleLangs,
		SubtitleLangOptions:          settings.SubtitleLangSeed,
		SubtitleAuto:                 subtitleAuto,
		MetadataDomainTag:            metadataDomainTag,
		MetadataGenresFromCategories: metadataGenresFromCategories,
		ArchiveFallback:              archiveFallback,
	})
}

func (h *Handler) settingsMaintenance(w http.ResponseWriter, r *http.Request) {
	data := h.maintenancePageData(r)
	if r.Header.Get("HX-Target") == "maintenance-live" {
		data.OOB = false
		render(w, "maintenance_live", data)
		return
	}
	render(w, "settings_maintenance", data)
}

type maintenancePageData struct {
	pageBase
	OOB                bool
	ApplyNamingBusy    bool
	NFORegenBusy       bool
	VerifyAllMediaBusy bool
	SyncFilesBusy      bool
}

func (h *Handler) maintenancePageData(r *http.Request) maintenancePageData {
	applyBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRenameEpisodes, queue.SystemDomain)
	nfoBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRegenerateNFO, queue.SystemDomain)
	verifyBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindIntegrityCheck, queue.SystemDomain)
	syncBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindSyncFiles, queue.SystemDomain)
	return maintenancePageData{
		pageBase:           newSettingsPage("Settings · Maintenance", "maintenance", flashFromQuery(r)),
		ApplyNamingBusy:    applyBusy,
		NFORegenBusy:       nfoBusy,
		VerifyAllMediaBusy: verifyBusy,
		SyncFilesBusy:      syncBusy,
	}
}

func (h *Handler) settingsDomains(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/settings/queue", http.StatusSeeOther)
}

func (h *Handler) actionSetDomainActive(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := settings.NormalizeDomain(r.FormValue("domain"))
	active := r.FormValue("active") == "1"
	if err := domains.SetActive(h.Queue.DB, domain, active); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	if !active {
		_, _ = h.Queue.CancelDomain(domain, queue.CancelReasonDomainDeactivated)
	}
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = "/settings/queue"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

func (h *Handler) actionSaveSettings(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.FormValue("auth_settings") == "1" {
		h.actionSaveAuthSettings(w, r)
		return
	}
	vals := map[string]string{}
	for _, e := range []string{
		settings.KeyPotFetch,
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
