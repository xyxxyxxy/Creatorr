package library

import "strings"

// AcquiredVia is how packed media was obtained (videos.acquired_via).
const (
	AcquiredViaSource  = "source"
	AcquiredViaArchive = "archive"
	AcquiredViaImport  = "import"
)

// StatusWantedArchive means live fetch was unavailable; eligible for archive.org lane download.
const StatusWantedArchive = "wanted_archive"

// ArchiveOrgDomain is the queue lane hostname for ytarchive: fallback downloads.
const ArchiveOrgDomain = "archive.org"

// NormalizeAcquiredVia returns a closed-set value; empty → source.
func NormalizeAcquiredVia(raw string) string {
	switch raw {
	case AcquiredViaArchive, AcquiredViaImport, AcquiredViaSource:
		return raw
	default:
		return AcquiredViaSource
	}
}

// TitleIsRemoteIDPlaceholder reports whether title is only the remote id (index stub).
func TitleIsRemoteIDPlaceholder(title, remoteID string) bool {
	t := strings.TrimSpace(title)
	id := strings.TrimSpace(remoteID)
	return id != "" && t == id
}
