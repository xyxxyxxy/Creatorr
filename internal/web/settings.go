package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/auth"
	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/health"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/stats"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

type settingsRowView struct {
	Key           string
	Label         string
	Value         string
	Help          string
	Cron          bool
	Checkbox      bool
	Checked       bool
	Select        bool // closed-set dropdown (scan schedule, stats retention)
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

func maskAppriseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= 48 {
		return raw
	}
	return raw[:28] + "…" + raw[len(raw)-12:]
}

func notifyEventsAll(events []string) bool {
	if len(events) != len(notify.AllEvents) {
		return false
	}
	have := map[string]bool{}
	for _, e := range events {
		have[e] = true
	}
	for _, id := range notify.AllEvents {
		if !have[id] {
			return false
		}
	}
	return true
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

func (h *Handler) settingsGeneral(w http.ResponseWriter, r *http.Request) {
	entries, err := settings.General(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	rows := make([]settingsRowView, 0, len(entries))
	for _, e := range entries {
		row := settingsRowView{
			Key: e.Key, Label: e.Label, Value: e.Value, Help: e.Help,
			Cron: settings.CronKeys[e.Key],
		}
		if e.Key == settings.KeyStatsRetentionDays {
			row.Select = true
			row.Value = settings.NormalizeStatsRetention(e.Value)
			for _, o := range settings.StatsRetentionOptions() {
				row.Options = append(row.Options, PresetOption{Value: o.Value, Label: o.Label})
			}
		}
		rows = append(rows, row)
	}
	authUser, _ := settings.AuthUsername(h.Queue.DB)
	apiKey, _ := settings.APIKey(h.Queue.DB)
	render(w, "settings_general", struct {
		pageBase
		Settings     []settingsRowView
		AuthUsername string
		APIKey       string
	}{
		pageBase:     newSettingsPage("Settings · General", "general", flashFromQuery(r)),
		Settings:     rows,
		AuthUsername: authUser,
		APIKey:       apiKey,
	})
}

func (h *Handler) ytdlpConnectPageData() (managedPath, installedVer, lastCheckedAt string, updatesEnabled, updateBusy bool) {
	updatesEnabled, _ = settings.YtDlpUpdatesEnabled(h.Queue.DB)
	updateBusy, _ = h.Queue.HasPendingOrRunningKind(queue.KindYtDlpUpdate, queue.SystemDomain)
	installedVer, _ = settings.Get(h.Queue.DB, settings.KeyYtDlpInstalledVersion)
	lastCheckedAt, _ = h.Queue.LastFinishedAt(queue.KindYtDlpUpdate, queue.SystemDomain, queue.StatusDone)
	if h.YtDlp != nil {
		managedPath = h.YtDlp.BinPath()
		if installedVer == "" {
			installedVer, _ = ytdlp.VerifyBinary(managedPath)
		}
	}
	return managedPath, installedVer, lastCheckedAt, updatesEnabled, updateBusy
}

func (h *Handler) settingsConnect(w http.ResponseWriter, r *http.Request) {
	entries, err := settings.Connect(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	managedPath, installedVer, lastCheckedAt, updatesEnabled, updateBusy := h.ytdlpConnectPageData()
	rows := make([]settingsRowView, 0, len(entries))
	potURLSet := strings.TrimSpace(h.PotProviderURL) != ""
	for _, e := range entries {
		row := settingsRowView{
			Key: e.Key, Label: e.Label, Value: e.Value, Help: e.Help,
			Cron: settings.CronKeys[e.Key],
		}
		if e.Key == settings.KeyYtDlpUpdateChannel {
			row.Select = true
			row.Value = settings.NormalizeYtDlpUpdateChannel(e.Value)
			for _, o := range settings.YtDlpUpdateChannelOptions() {
				row.Options = append(row.Options, PresetOption{Value: o.Value, Label: o.Label})
			}
			if !updatesEnabled {
				row.Disabled = true
				row.DisabledTitle = "Set yt-dlp update schedule under Settings → Scheduler first."
			}
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
	flareJoin, potJoin := externalServiceJoinViews(h)
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
	evOpts := make([]notifyEventOption, 0, len(notify.AllEvents))
	for _, id := range notify.EventsSortedByLevel() {
		evOpts = append(evOpts, notifyEventOption{ID: id, Label: notify.EventLabels[id]})
	}
	render(w, "settings_connect", struct {
		pageBase
		FlareService       externalServiceURLView
		PotService         externalServiceURLView
		Settings           []settingsRowView
		NotifyChannels     []notifyChannelView
		EventOptions       []notifyEventOption
		DefaultEvents      []string
		YtDlpManagedPath   string
		YtDlpInstalledVer  string
		YtDlpLastCheckedAt string
		YtDlpUpdatesOn     bool
		YtDlpUpdateBusy    bool
		YtDlpUpdateOffTip  string
		YtDlpUpdateBusyTip string
	}{
		pageBase:           newSettingsPage("Settings · Connect", "connect", flashFromQuery(r)),
		FlareService:       flareJoin,
		PotService:         potJoin,
		Settings:           rows,
		NotifyChannels:     chViews,
		EventOptions:       evOpts,
		DefaultEvents:      append([]string(nil), notify.AllEvents...),
		YtDlpManagedPath:   managedPath,
		YtDlpInstalledVer:  installedVer,
		YtDlpLastCheckedAt: lastCheckedAt,
		YtDlpUpdatesOn:     updatesEnabled,
		YtDlpUpdateBusy:    updateBusy,
		YtDlpUpdateOffTip:  "Automatic updates disabled. Set yt-dlp update schedule under Settings → Scheduler.",
		YtDlpUpdateBusyTip: "yt-dlp update already queued or running",
	})
}

type externalServiceURLView struct {
	Label       string
	Value       string
	Hint        string
	Status      string
	StatusLabel string
	StatusTip   string
}

func externalServiceJoinViews(h *Handler) (flare, pot externalServiceURLView) {
	flare = externalServiceURLView{
		Label: "FlareSolverr URL",
		Value: strings.TrimSpace(h.FlareSolverrURL),
		Hint:  "Set CREATORR_FLARESOLVERR_URL and restart.\nEnable 'Use FlareSolverr' On a host 'Domain override' ('Settings → Queue / Domains').",
	}
	pot = externalServiceURLView{
		Label: "PO token provider URL",
		Value: strings.TrimSpace(h.PotProviderURL),
		Hint:  "Set CREATORR_POT_PROVIDER_URL and restart.\nEnable 'PO token fetch' below when the URL is set (forced to 'Never' while unset).",
	}
	if h.Health == nil {
		flare.Status, flare.StatusLabel, flare.StatusTip = externalServiceStatusFromCheck(health.Check{Status: health.StatusSkipped, Message: "URL unset"})
		pot.Status, pot.StatusLabel, pot.StatusTip = externalServiceStatusFromCheck(health.Check{Status: health.StatusSkipped, Message: "URL unset"})
		if flare.Value != "" {
			flare.Status, flare.StatusLabel, flare.StatusTip = string(health.StatusDegraded), "Unreachable", "Health checker unavailable"
		}
		if pot.Value != "" {
			pot.Status, pot.StatusLabel, pot.StatusTip = string(health.StatusDegraded), "Unreachable", "Health checker unavailable"
		}
		return flare, pot
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	flareCheck, potCheck := h.Health.ExternalServices(ctx)
	flare.Status, flare.StatusLabel, flare.StatusTip = externalServiceStatusFromCheck(flareCheck)
	pot.Status, pot.StatusLabel, pot.StatusTip = externalServiceStatusFromCheck(potCheck)
	return flare, pot
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
			Cron: settings.CronKeys[e.Key],
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
	entries, err := settings.Queue(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	rows := make([]settingsRowView, 0, len(entries))
	for _, e := range entries {
		row := settingsRowView{
			Key: e.Key, Label: e.Label, Value: e.Value, Help: e.Help,
		}
		if e.Key == settings.KeyDownloadWantedOrder {
			row.Select = true
			row.Value = settings.NormalizeDownloadWantedOrder(e.Value)
			for _, o := range settings.DownloadWantedOrderOptions() {
				row.Options = append(row.Options, PresetOption{Value: o.Value, Label: o.Label})
			}
		}
		rows = append(rows, row)
	}
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
		Settings        []settingsRowView
		DefaultLimits   settings.DomainLimits
		DefaultUsername string
		DomainOverrides []settings.DomainQueueRow
		Page            PageInfo
		DomainDatalist  []string
		FlareConfigured bool
	}{
		pageBase:        newSettingsPage("Settings · Queue / Domains", "queue", flashFromQuery(r)),
		Settings:        rows,
		DefaultLimits:   defLim,
		DefaultUsername: "",
		DomainOverrides: pageRows,
		Page:            pageInfo,
		DomainDatalist:  sourceDomains,
		FlareConfigured: flareOK,
	})
}

func (h *Handler) settingsLibrary(w http.ResponseWriter, r *http.Request) {
	episodeFormat, _ := settings.GetEpisodeFormat(h.Queue.DB)
	applyBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRenameEpisodes, queue.SystemDomain)
	roots, _ := h.Library.ListRoots()
	profiles, _ := h.Library.ListProfiles()
	seriesByProfile, _ := h.Library.SeriesCountsByProfile()
	pageRoots, rootsPage := SlicePage(r, "page", roots)
	pageProfiles, profilesPage := SlicePage(r, "profiles_page", profiles)
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
		EpisodeFormat                string
		NamingLocked                 bool
		Roots                        []library.RootFolder
		RootsPage                    PageInfo
		Profiles                     []profileSettingsRow
		ProfilesPage                 PageInfo
		SubtitleLangs                []string
		SubtitleLangOptions          []string
		SubtitleAuto                 bool
		MetadataDomainTag            bool
		MetadataGenresFromCategories bool
	}{
		pageBase:                     newSettingsPage("Settings · Library", "library", flashFromQuery(r)),
		Settings:                     settingRows,
		EpisodeFormat:                episodeFormat,
		NamingLocked:                 applyBusy,
		Roots:                        pageRoots,
		RootsPage:                    rootsPage,
		Profiles:                     profileRows,
		ProfilesPage:                 profilesPage,
		SubtitleLangs:                subtitleLangs,
		SubtitleLangOptions:          settings.SubtitleLangSeed,
		SubtitleAuto:                 subtitleAuto,
		MetadataDomainTag:            metadataDomainTag,
		MetadataGenresFromCategories: metadataGenresFromCategories,
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
	OOB             bool
	ApplyNamingBusy bool
	NFORegenBusy    bool
	SyncFilesBusy   bool
}

func (h *Handler) maintenancePageData(r *http.Request) maintenancePageData {
	applyBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRenameEpisodes, queue.SystemDomain)
	nfoBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRegenerateNFO, queue.SystemDomain)
	syncBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindSyncFiles, queue.SystemDomain)
	return maintenancePageData{
		pageBase:        newSettingsPage("Settings · Maintenance", "maintenance", flashFromQuery(r)),
		ApplyNamingBusy: applyBusy,
		NFORegenBusy:    nfoBusy,
		SyncFilesBusy:   syncBusy,
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
		_, _ = h.Queue.CancelDomain(domain, "Domain deactivated")
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
		settings.KeyDownloadWantedOrder,
		settings.KeySyncFilesCron,
		settings.KeyRetentionDeleteCron,
		settings.KeyStatsRetentionDays,
		settings.KeyYtDlpUpdateCron,
		settings.KeyYtDlpUpdateChannel,
		settings.KeySourceDownloadErrorThreshold,
		settings.KeyEpisodeFormat,
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
	namingPosted := false
	if r.FormValue("redirect") == "/settings/library" {
		if _, ok := r.Form[settings.KeyEpisodeFormat]; ok {
			namingPosted = true
			vals[settings.KeyEpisodeFormat] = strings.TrimSpace(r.FormValue(settings.KeyEpisodeFormat))
		}
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
		}
	}
	// Reject naming changes while Apply rename is pending/running.
	if namingPosted {
		if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRenameEpisodes, queue.SystemDomain); busy {
			redirectSettings(w, r, "/settings/library", "err="+urlQuery("Cancel or wait for 'Apply episode format' before changing formats"))
			return
		}
	}
	if err := settings.SetMany(h.Queue.DB, vals); err != nil {
		redirectSettings(w, r, "/settings/general", "err="+urlQuery(err.Error()))
		return
	}
	// Shorter/disabled retention must drop old samples immediately (not wait for next sample tick).
	if _, ok := vals[settings.KeyStatsRetentionDays]; ok {
		if _, err := stats.ApplyRetention(h.Queue.DB, time.Now().UTC()); err != nil {
			redirectSettings(w, r, "/settings/general", "err="+urlQuery(err.Error()))
			return
		}
	}
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = "/settings/general"
	}
	redirectSettings(w, r, redir, "ok=saved")
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
	if idRaw := strings.TrimSpace(r.FormValue("id")); idRaw != "" {
		if id, err := strconv.ParseInt(idRaw, 10, 64); err == nil && id > 0 {
			fieldID = fmt.Sprintf("notify-url-field-%d", id)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 200 so HTMX swaps the field fragment (4xx skips swap by default).
	render(w, "notify_url_field", map[string]any{
		"FieldID":  fieldID,
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
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery("invalid task_cooldown_seconds"))
		return
	}
	maxQueue, err := settings.ParsePositiveInt(r.FormValue("max_download_queue"), "max download tasks")
	if err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	maxParallel, err := settings.ParsePositiveInt(r.FormValue("max_parallel_tasks"), "max parallel tasks")
	if err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	rate, err := settings.CombineDownloadRateLimit(r.FormValue("download_rate_limit_value"), r.FormValue("download_rate_limit_unit"))
	if err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	if err := settings.SetDomainDefault(h.Queue.DB, delay, maxQueue, maxParallel, rate, r.FormValue("sleep_requests"), false); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	// Access (Flare / cookies / credentials) is override-only; clear any legacy defaults jar/creds.
	_ = domains.ClearCookies(h.Queue.DB, settings.DomainDefault)
	if err := settings.SaveDefaultCredentials(h.Queue.DB, "", "", false); err != nil {
		redirectSettings(w, r, "/settings/queue", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/queue", "ok=domain-defaults")
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
		redirectSettings(w, r, "/settings/library", "err="+urlQuery(err.Error()))
		return
	}
	_, err = h.Library.CreateRoot(strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("path")), ttl)
	if err != nil {
		redirectSettings(w, r, "/settings/library", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/library", "ok=root")
}

func (h *Handler) actionUpdateRoot(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	name := strings.TrimSpace(r.FormValue("name"))
	path := strings.TrimSpace(r.FormValue("path"))
	ttlRaw := strings.TrimSpace(r.FormValue("retention_ttl_days"))
	clearRetention := ttlRaw == ""
	var retention *int64
	if !clearRetention {
		ttl, err := parseRetentionTTLDays(ttlRaw)
		if err != nil {
			redirectSettings(w, r, "/settings/library", "err="+urlQuery(err.Error()))
			return
		}
		if ttl == nil {
			clearRetention = true
		} else {
			retention = ttl
		}
	}
	_, err := h.Library.UpdateRoot(id, &name, &path, retention, clearRetention)
	if err != nil {
		redirectSettings(w, r, "/settings/library", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/library", "ok=root-updated")
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
	if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRegenerateNFO, queue.SystemDomain); busy {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("NFO regenerate already queued"))
		return
	}
	if _, err := h.Library.EnqueueRegenerateNFO(); err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/maintenance", "ok=nfo-regen-queued")
}

func (h *Handler) actionApplyEpisodeNaming(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.Library == nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("library unavailable"))
		return
	}
	_, err := h.Library.EnqueueRenameEpisodes()
	if err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, "/settings/maintenance", "ok=apply-naming")
}

func (h *Handler) actionSyncFiles(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.Library == nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("library unavailable"))
		return
	}
	if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindSyncFiles, queue.SystemDomain); busy {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery("File sync already queued"))
		return
	}
	id, err := h.Library.EnqueueSyncFiles(queue.PrioritySyncFilesDue)
	if err != nil {
		redirectSettings(w, r, "/settings/maintenance", "err="+urlQuery(err.Error()))
		return
	}
	if id == 0 {
		redirectSettings(w, r, "/settings/maintenance", "ok=sync-files-empty")
		return
	}
	redirectSettings(w, r, "/settings/maintenance", "ok=sync-files-queued")
}

func (h *Handler) actionYtDlpUpdate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.Library == nil {
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery("library unavailable"))
		return
	}
	enabled, err := settings.YtDlpUpdatesEnabled(h.Queue.DB)
	if err != nil {
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery(err.Error()))
		return
	}
	if !enabled {
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery("Automatic yt-dlp updates disabled"))
		return
	}
	if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindYtDlpUpdate, queue.SystemDomain); busy {
		redirectSettings(w, r, "/settings/connect", "err="+urlQuery("yt-dlp update already queued or running"))
		return
	}
	id, err := h.Library.EnqueueYtDlpUpdate(queue.PriorityYtDlpUpdateDue, "manual")
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
