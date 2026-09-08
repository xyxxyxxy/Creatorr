package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/health"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func (h *Handler) ytdlpConnectControlsView() ytdlpConnectControlsView {
	updateBusy, _ := h.Queue.HasPendingOrRunningKind(queue.KindYtDlpUpdate, queue.SystemDomain)
	lastCheckedAt, _ := h.Queue.LastFinishedAt(queue.KindYtDlpUpdate, queue.SystemDomain, queue.StatusDone)
	return ytdlpConnectControlsView{
		YtDlpLastCheckedAt: lastCheckedAt,
		YtDlpUpdateBusy:    updateBusy,
		YtDlpUpdateBusyTip: "yt-dlp update already queued or running",
		YtDlpChannel:       h.ytdlpUpdateChannelRow(),
	}
}

type ytdlpConnectControlsView struct {
	YtDlpLastCheckedAt string
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

func (h *Handler) ytdlpInstalledVersionView() ytdlpInstalledVersionView {
	if installedVer, _ := settings.Get(h.Queue.DB, settings.KeyYtDlpInstalledVersion); strings.TrimSpace(installedVer) != "" {
		return ytdlpInstalledVersionView{Value: installedVer}
	}
	// Version is written on boot (PrepareManagedBin --version) and by ytdlp_update.
	// Never invoke yt-dlp from HTTP.
	return ytdlpInstalledVersionView{Value: "Unknown"}
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
		Key:    settings.KeyYtDlpUpdateChannel,
		Label:  settings.Labels[settings.KeyYtDlpUpdateChannel],
		Value:  settings.NormalizeYtDlpUpdateChannel(val),
		Help:   settings.Help[settings.KeyYtDlpUpdateChannel],
		Select: true,
	}
	for _, o := range settings.YtDlpUpdateChannelOptions() {
		row.Options = append(row.Options, PresetOption{Value: o.Value, Label: o.Label})
	}
	return row
}

func (h *Handler) ytdlpPlayerClientRow() settingsRowView {
	val, _ := settings.Get(h.Queue.DB, settings.KeyYoutubePlayerClient)
	return settingsRowView{
		Key:   settings.KeyYoutubePlayerClient,
		Label: settings.Labels[settings.KeyYoutubePlayerClient],
		Value: settings.NormalizeYoutubePlayerClient(val),
		Help:  settings.Help[settings.KeyYoutubePlayerClient],
	}
}

func (h *Handler) potFetchRow() settingsRowView {
	val, _ := settings.Get(h.Queue.DB, settings.KeyPotFetch)
	row := settingsRowView{
		Key:    settings.KeyPotFetch,
		Label:  settings.Labels[settings.KeyPotFetch],
		Value:  settings.NormalizePotFetch(val),
		Help:   settings.Help[settings.KeyPotFetch],
		Select: true,
	}
	for _, o := range settings.PotFetchOptions() {
		row.Options = append(row.Options, PresetOption{Value: o.Value, Label: o.Label})
	}
	if strings.TrimSpace(h.PotProviderURL) == "" {
		row.Disabled = true
		row.Label = row.Label + " (disabled)"
		row.Value = settings.PotFetchNever
		row.DisabledTitle = "Set CREATORR_POT_PROVIDER_URL first (Compose default http://creatorr-po-token:4416)."
	}
	return row
}

func (h *Handler) settingsConnect(w http.ResponseWriter, r *http.Request) {
	ytdlpControls := h.ytdlpConnectControlsView()
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
	plugins := h.YtDlp.ListPluginPackages()
	render(w, "settings_connect", struct {
		pageBase
		FlareService          externalServiceURLView
		PotService            externalServiceURLView
		NotifyChannels        []notifyChannelView
		EventGroups           []notifyEventGroupView
		DefaultEvents         []string
		YtDlpInstalledVersion ytdlpInstalledVersionView
		YtDlpControls         ytdlpConnectControlsView
		YtDlpPlayerClient     settingsRowView
		PotFetch              settingsRowView
		YtDlpPlugins          []ytdlp.PluginPackage
	}{
		pageBase:              newSettingsPage("Settings · Connect", "connect", flashFromQuery(r)),
		FlareService:          flareJoin,
		PotService:            potJoin,
		NotifyChannels:        chViews,
		EventGroups:           evGroups,
		DefaultEvents:         []string{notify.EventAll},
		YtDlpInstalledVersion: ytdlpInstalledVersionView{Pending: true},
		YtDlpControls:         ytdlpControls,
		YtDlpPlayerClient:     h.ytdlpPlayerClientRow(),
		PotFetch:              h.potFetchRow(),
		YtDlpPlugins:          plugins,
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
		Hint:  "Set CREATORR_POT_PROVIDER_URL and restart.\n'fetch_pot' is under 'plugins' above (forced to 'Never' while unset).",
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
