// Package buildinfo holds bake-time identity for supportability (footer, logs).
// Override via go build -ldflags "-X github.com/xyxxyxxy/Creatorr/internal/buildinfo.Version=…".
package buildinfo

import (
	"regexp"
	"strings"
)

// Defaults for local go test / unset ldflags. Image builds set Deployment=docker.
var (
	Version    = "dev"
	Revision   = "unknown"
	Deployment = "binary"
)

// GitHubURL is the public repository homepage.
const GitHubURL = "https://github.com/xyxxyxxy/Creatorr"

var shaRE = regexp.MustCompile(`(?i)^[0-9a-f]{7,40}$`)

// ShortRevision returns the first 7 characters of Revision when long enough.
func ShortRevision() string {
	r := strings.TrimSpace(Revision)
	if r == "" {
		return "unknown"
	}
	if len(r) > 7 && shaRE.MatchString(r) {
		return r[:7]
	}
	return r
}

// CommitURL returns a GitHub commit URL when Revision looks like a git sha, else "".
func CommitURL() string {
	r := strings.TrimSpace(Revision)
	if !shaRE.MatchString(r) {
		return ""
	}
	return GitHubURL + "/commit/" + r
}
