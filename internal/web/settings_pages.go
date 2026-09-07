package web

import (
	"net/http"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

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
