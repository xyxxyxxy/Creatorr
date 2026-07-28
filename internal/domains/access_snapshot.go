package domains

import (
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/cookies"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// DetailKeyDomainAccess is the tasks.detail JSON key for DomainAccessSnapshot.
const DetailKeyDomainAccess = "domain-access"

// DomainAccessSnapshot is the host-lane Access / yt-dlp throttle chip state
// captured when a task starts (rate, sleep, cookies, Flare, credentials).
// Queue / parallel / cooldown are omitted (task-detail chips skip those).
type DomainAccessSnapshot struct {
	RateLimit           string  `json:"rate_limit"`
	RateOverride        bool    `json:"rate_override"`
	SleepRequests       float64 `json:"sleep_requests"`
	SleepOverride       bool    `json:"sleep_override"`
	Cookies             bool    `json:"cookies"`
	CookiesOverride     bool    `json:"cookies_override"`
	Flare               bool    `json:"flare"`
	FlareOverride       bool    `json:"flare_override"`
	Credentials         bool    `json:"credentials"`
	CredentialsOverride bool    `json:"credentials_override"`
}

// SnapshotDomainAccess records effective host-lane settings for task detail.
// Returns nil for system / unknown / empty hosts (no Access chips).
func SnapshotDomainAccess(database *db.DB, domain string) (*DomainAccessSnapshot, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == queue.SystemDomain || domain == settings.DomainDefault {
		return nil, nil
	}
	lim, err := settings.LimitsForDomain(database, domain)
	if err != nil {
		return nil, err
	}
	snap := &DomainAccessSnapshot{
		RateLimit:     strings.TrimSpace(lim.DownloadRateLimit),
		SleepRequests: lim.SleepRequests,
		Flare:         lim.UseFlareSolverr,
	}
	if meta, ok, err := Get(database, domain); err != nil {
		return nil, err
	} else if ok {
		snap.RateOverride = meta.DownloadRateLimit.Valid
		snap.SleepOverride = meta.SleepRequests.Valid
		snap.FlareOverride = FlareOverrideLabel(meta.UseFlareSolverr) == FlareOn
	}
	if ok, _, err := cookies.Applies(database, domain); err != nil {
		return nil, err
	} else if ok {
		snap.Cookies = true
		snap.CookiesOverride = true
	}
	if creds, err := settings.CredentialsForDomain(database, domain); err != nil {
		return nil, err
	} else if strings.TrimSpace(creds.Username) != "" {
		snap.Credentials = true
		snap.CredentialsOverride = creds.FromHost
	}
	return snap, nil
}

// ShowRate reports whether the rate chip should render (hide unlimited/off).
func (s *DomainAccessSnapshot) ShowRate() bool {
	if s == nil {
		return false
	}
	return !settings.RateLimitOff(s.RateLimit)
}

// ShowSleep reports whether the sleep chip should render (hide 0).
func (s *DomainAccessSnapshot) ShowSleep() bool {
	return s != nil && s.SleepRequests > 0
}
