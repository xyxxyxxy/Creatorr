package worker

import (
	"github.com/xyxxyxxy/Creatorr/internal/events"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// Deps wires real task handlers.
type Deps struct {
	Library *library.Store
	TmpRoot string        // optional parent for cookie/work temps
	YtDlp   *ytdlp.Client // required
	Events  *events.Hub
}

// DefaultHandlers returns scan/download implementations plus stubs for other kinds.
func DefaultHandlers(d Deps) map[string]TaskHandler {
	stubs := StubHandlers()
	out := make(map[string]TaskHandler, len(stubs))
	for k, v := range stubs {
		out[k] = v
	}
	out[queue.KindScan] = ScanHandler(d)
	out[queue.KindDownload] = DownloadHandler(d)
	out[queue.KindRescanMetadata] = RescanMetadataHandler(d)
	out[queue.KindRefreshSidecars] = RefreshSidecarsHandler(d)
	out[queue.KindImport] = ImportHandler(d)
	out[queue.KindPrefetchSeriesMeta] = PrefetchSeriesMetaHandler(d)
	out[queue.KindPrefetchVideoMeta] = PrefetchVideoMetaHandler(d)
	out[queue.KindPrefetchAddSeries] = PrefetchAddSeriesHandler(d)
	out[queue.KindPrefetchAddVideo] = PrefetchAddVideoHandler(d)
	out[queue.KindProbeSourceTitle] = ProbeSourceTitleHandler(d)
	out[queue.KindSyncFiles] = SyncFilesHandler(d)
	out[queue.KindRetentionDelete] = RetentionDeleteHandler(d)
	out[queue.KindRenameEpisodes] = RenameEpisodesHandler(d)
	out[queue.KindRegenerateNFO] = RegenerateNFOHandler(d)
	out[queue.KindIntegrityCheck] = VerifyAllMediaHandler(d)
	out[queue.KindDeleteFiles] = DeleteFilesHandler(d)
	out[queue.KindSponsorblockCut] = SponsorblockCutHandler(d)
	out[queue.KindIntegrityCheckInitial] = MediaVerifyHandler(d)
	out[queue.KindYtDlpUpdate] = YtDlpUpdateHandler(d)
	out[queue.KindBulkEditSeries] = BulkEditSeriesHandler(d)
	out[queue.KindBulkEditVideos] = BulkEditVideosHandler(d)
	return out
}
