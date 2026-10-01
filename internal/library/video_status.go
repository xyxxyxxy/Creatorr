package library

// videos.status values.
const (
	StatusWanted                    = "wanted"
	StatusDownloaded                = "downloaded"
	StatusIgnored                   = "ignored"
	StatusDeleted                   = "deleted"
	StatusMissing                   = "missing"
	StatusWantedDownloadError       = "wanted_download_error"
	StatusDownloadedIntegrityFailed = "downloaded_integrity_failed"
	// StatusWantedArchive means live fetch was unavailable; eligible for archive.org lane download.
	StatusWantedArchive = "wanted_archive"
)

// sqlVideoErrorStatuses is a SQL IN list of video error statuses (series health).
const sqlVideoErrorStatuses = `('` + StatusWantedDownloadError + `', '` + StatusDownloadedIntegrityFailed + `')`
