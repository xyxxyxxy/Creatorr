package web

import (
	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/health"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// Handler serves HTML admin pages and form actions.
type Handler struct {
	Library         *library.Store
	Queue           *queue.Store
	YtDlp           *ytdlp.Client
	FlareSolverrURL string // CREATORR_FLARESOLVERR_URL; display-only on Settings → Connect
	PotProviderURL  string // CREATORR_POT_PROVIDER_URL; empty disables pot_fetch UI
	Health          *health.Checker
}

// Mount registers UI routes on r.
func (h *Handler) Mount(r chi.Router) {
	SetUsernameForPage(func() string {
		if h.Queue == nil || h.Queue.DB == nil {
			return ""
		}
		u, _ := settings.AuthUsername(h.Queue.DB)
		return u
	})

	r.Get("/setup", h.setupGet)
	r.Post("/setup", h.setupPost)
	r.Get("/login", h.loginGet)
	r.Post("/login", h.loginPost)
	r.Post("/logout", h.logoutPost)

	r.Get("/", h.overview)
	r.Get("/series", h.seriesList)
	r.Get("/series/list-live", h.seriesListLive)
	r.Get("/series/error-count.json", h.seriesErrorCountJSON)
	r.Get("/series/add", h.seriesAdd)
	r.Get("/series/{id}", h.seriesDetail)
	r.Get("/series/{id}/art/{role}", h.seriesArtFile)
	r.Get("/series/{id}/files/{role}", h.seriesMetaFileViewPage)
	r.Get("/series/{id}/files/{role}/raw", h.seriesMetaFileRaw)
	r.Get("/series/{id}/metadata/prefetch/{tid}", h.seriesMetadataPrefetchStatus)
	r.Get("/series/{id}/metadata/prefetch/{tid}/art/{role}", h.seriesPrefetchArtFile)
	r.Get("/series/{id}/task-indicators", h.seriesTaskIndicators)
	r.Get("/series/{id}/videos-live", h.seriesVideosLive)
	r.Get("/series/{id}/sources/{sid}", h.sourceDetail)
	r.Get("/series/{id}/videos/{vid}", h.videoDetail)
	r.Get("/series/{id}/videos/{vid}/thumb", h.videoThumb)
	r.Get("/series/{id}/videos/{vid}/metadata/prefetch/{tid}", h.videoMetadataPrefetchStatus)
	r.Get("/series/{id}/videos/{vid}/metadata/prefetch/{tid}/art/{role}", h.videoPrefetchArtFile)
	r.Get("/series/{id}/videos/{vid}/files/{fid}", h.videoSidecarViewPage)
	r.Get("/series/{id}/videos/{vid}/files/{fid}/raw", h.videoSidecarFile)
	r.Get("/series/{id}/videos/{vid}/task-indicator", h.videoTaskIndicator)
	r.Get("/tasks", h.tasks)
	r.Get("/stats", h.statsPage)
	r.Get("/stats/series.json", h.statsSeriesJSON)
	r.Get("/stats/library-size.json", h.statsLibrarySizeJSON)
	r.Get("/history", h.historyPage)
	r.Get("/notification/{id}", h.notificationDetail)
	r.Get("/task/{id}", h.taskDetail)
	r.Get("/task/{id}/logs", h.taskLogs)
	r.Get("/settings", h.settingsRedirect)
	r.Get("/settings/general", h.settingsGeneral)
	r.Get("/settings/library", h.settingsLibrary)
	r.Get("/settings/connect", h.settingsConnect)
	r.Get("/settings/queue", h.settingsQueue)
	r.Get("/settings/scheduler", h.settingsScheduler)
	r.Get("/settings/maintenance", h.settingsMaintenance)
	r.Get("/settings/domains", h.settingsDomains)
	r.Get("/import", h.importPage)
	r.Get("/actions/import-full-scan-status", h.importFullScanStatus)
	r.Get("/actions/probe-source-title", h.actionProbeSourceTitle)
	r.Get("/actions/add-series-prefetch/{tid}", h.addSeriesPrefetchStatus)
	r.Post("/actions/fetch-add-series", h.actionFetchAddSeries)
	r.Get("/actions/add-video-prefetch/{tid}", h.addVideoPrefetchStatus)
	r.Post("/actions/fetch-add-video", h.actionFetchAddVideo)

	r.Post("/actions/add-series", h.actionAddSeries)
	r.Post("/actions/update-series", h.actionUpdateSeries)
	r.Post("/actions/save-series-metadata", h.actionSaveSeriesMetadata)
	r.Post("/actions/series-metadata-prefetch", h.actionSeriesMetadataPrefetch)
	r.Post("/actions/save-video-metadata", h.actionSaveVideoMetadata)
	r.Post("/actions/video-metadata-prefetch", h.actionVideoMetadataPrefetch)
	r.Post("/actions/add-source", h.actionAddSource)
	r.Post("/actions/update-source", h.actionUpdateSource)
	r.Post("/actions/delete-source", h.actionDeleteSource)
	r.Post("/actions/delete-series", h.actionDeleteSeries)
	r.Post("/actions/scan-series", h.actionScanSeries)
	r.Post("/actions/scan-source", h.actionScanSource)
	r.Post("/actions/full-rescan-series", h.actionFullRescanSeries)
	r.Post("/actions/full-rescan-source", h.actionFullRescanSource)
	r.Post("/actions/metadata-rescan-series", h.actionMetadataRescanSeries)
	r.Post("/actions/metadata-rescan-video", h.actionMetadataRescanVideo)
	r.Post("/actions/refresh-sidecars-video", h.actionRefreshSidecarsVideo)
	r.Post("/actions/want-video", h.actionWantVideo)
	r.Post("/actions/set-source-monitored", h.actionSetSourceMonitored)
	r.Post("/actions/set-series-monitored", h.actionSetSeriesMonitored)
	r.Post("/actions/download-video", h.actionDownloadVideo)
	r.Post("/actions/retry-source-errors", h.actionRetrySourceErrors)
	r.Post("/actions/ignore-video", h.actionIgnoreVideo)
	r.Post("/actions/delete-video", h.actionDeleteVideo)
	r.Post("/actions/delete-video-sidecar", h.actionDeleteVideoSidecar)
	r.Post("/actions/cancel-task", h.actionCancelTask)
	r.Post("/actions/cancel-domain-tasks", h.actionCancelDomainTasks)
	r.Post("/actions/save-settings", h.actionSaveSettings)
	r.Post("/actions/regenerate-api-key", h.actionRegenerateAPIKey)
	r.Post("/actions/upsert-notify-channel", h.actionUpsertNotifyChannel)
	r.Post("/actions/delete-notify-channel", h.actionDeleteNotifyChannel)
	r.Post("/actions/test-notify-channel", h.actionTestNotifyChannel)
	r.Post("/actions/mark-notification-read", h.actionMarkNotificationRead)
	r.Post("/actions/mark-all-notifications-read", h.actionMarkAllNotificationsRead)
	r.Post("/actions/set-domain-active", h.actionSetDomainActive)
	r.Post("/actions/set-domain-paused", h.actionSetDomainPaused)
	r.Post("/actions/save-domain-default", h.actionSaveDomainDefault)
	r.Post("/actions/upsert-domain-override", h.actionUpsertDomainOverride)
	r.Post("/actions/delete-domain-override", h.actionDeleteDomainOverride)
	r.Post("/actions/save-cookie", h.actionSaveCookie)
	r.Post("/actions/delete-cookie", h.actionDeleteCookie)
	r.Post("/actions/add-root", h.actionAddRoot)
	r.Post("/actions/update-root", h.actionUpdateRoot)
	r.Post("/actions/add-profile", h.actionAddProfile)
	r.Post("/actions/update-profile", h.actionUpdateProfile)
	r.Post("/actions/delete-profile", h.actionDeleteProfile)
	r.Post("/actions/regenerate-nfos", h.actionRegenerateNFOs)
	r.Post("/actions/apply-episode-naming", h.actionApplyEpisodeNaming)
	r.Post("/actions/sync-files", h.actionSyncFiles)
}
