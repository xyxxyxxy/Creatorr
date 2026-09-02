package library

import (
	"net/url"
	"strings"
)

// DownloadURL returns the yt-dlp URL for a video row.
// sourceURL is videos.source_url; remoteID is videos.remote_id.
// Falls back to host-specific rules when the stored URL is a shared container page.
func DownloadURL(sourceURL, remoteID string) string {
	sourceURL = strings.TrimSpace(sourceURL)
	remoteID = strings.TrimSpace(remoteID)
	if sourceURL == "" {
		return ""
	}
	if u := archiveOrgDownloadURL(sourceURL, remoteID); u != "" {
		return u
	}
	return sourceURL
}

func archiveOrgDownloadURL(sourceURL, remoteID string) string {
	if remoteID == "" || !strings.Contains(remoteID, "/") {
		return ""
	}
	u, err := url.Parse(sourceURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host != "archive.org" && host != "web.archive.org" {
		return ""
	}
	return "https://archive.org/download/" + remoteID
}
