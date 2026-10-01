package domains

import (
	"database/sql"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// DetailKeyCookieAttach is the tasks.detail JSON key for CookieAttachStatus.
const DetailKeyCookieAttach = "cookie-attach"

// Cookie attach outcome states for task detail / Stages.
const (
	CookieAttachOff       = "off"       // no stored jar
	CookieAttachAnonymous = "anonymous" // succeeded without account jar
	CookieAttachCookies   = "cookies"   // jar used (smart off, or prefer-cookies path)
	CookieAttachRetried   = "retried"   // succeeded after cookie retry
	CookieAttachOmitted   = "omitted"   // smart on; jar available but not used this invoke
)

// CookieAttachStatus is stored under tasks.detail cookie-attach.
type CookieAttachStatus struct {
	State         string `json:"state"`                    // off|anonymous|cookies|retried|omitted
	RetryReason   string `json:"retry_reason,omitempty"`   // first-failure code when retried
	Detail        string `json:"detail,omitempty"`
	Smart         bool   `json:"smart,omitempty"`          // domains.smart_cookies at task time
	PreferCookies bool   `json:"prefer_cookies,omitempty"` // source preferred jar first
	Probe         bool   `json:"probe,omitempty"`          // anonymous pass was a recover probe
}

// SmartCookies reports domains.smart_cookies for a hostname.
// Missing row → false (always-attach jar).
func SmartCookies(database *db.DB, domain string) (bool, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" || domain == settings.DomainDefault {
		return false, nil
	}
	var v sql.NullInt64
	err := database.SQL.QueryRow(`SELECT smart_cookies FROM domains WHERE domain = ?`, domain).Scan(&v)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return v.Valid && v.Int64 != 0, nil
}

// SmartCookiesForURL resolves the host from rawURL then SmartCookies.
func SmartCookiesForURL(database *db.DB, rawURL string) (bool, error) {
	return SmartCookies(database, queue.DomainFromURL(rawURL))
}

// SetSmartCookies sets domains.smart_cookies on a host override.
// Wipes that host's source learning when the flag value changes.
func SetSmartCookies(database *db.DB, domain string, on bool) error {
	if err := settings.ValidateOverrideDomain(domain); err != nil {
		return err
	}
	domain = settings.NormalizeDomain(domain)
	if err := EnsureHost(database, domain); err != nil {
		return err
	}
	prev, err := SmartCookies(database, domain)
	if err != nil {
		return err
	}
	val := 0
	if on {
		val = 1
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = database.SQL.Exec(`
		UPDATE domains SET smart_cookies = ?, updated_at = ? WHERE domain = ?
	`, val, now, domain)
	if err != nil {
		return err
	}
	if prev != on {
		return WipeCookieSmartForHost(database, domain)
	}
	return nil
}

// CookieAttachStage is one Stages note derived from cookie-attach detail.
type CookieAttachStage struct {
	Event    string
	Message  string
	HasError bool
}

// SplitDownloadAttempts reports whether Stages should show two attempt nodes
// (anonymous failure, then cookie retry) under smart cookie anon-first.
func (s CookieAttachStatus) SplitDownloadAttempts() bool {
	return s.Smart && s.State == CookieAttachRetried
}

// CookieUsedNote is the Stages line when cookies were actually attached.
func (s CookieAttachStatus) CookieUsedNote() string {
	switch s.State {
	case CookieAttachCookies, CookieAttachRetried:
		return "Cookies used"
	default:
		return ""
	}
}

// StageEntries returns the cookie note for nesting under the final download node.
func (s CookieAttachStatus) StageEntries(taskFailed bool) []CookieAttachStage {
	_ = taskFailed
	if note := s.CookieUsedNote(); note != "" {
		return []CookieAttachStage{{Event: "cookies", Message: note, HasError: false}}
	}
	return nil
}

// StageMessage returns the Stages timeline message for a cookie-attach state.
// Deprecated for multi-node stages; prefer StageEntries / CookieUsedNote.
func (s CookieAttachStatus) StageMessage() string {
	return s.CookieUsedNote()
}

// ShowStage reports whether Stages should mention cookies under download.
func (s CookieAttachStatus) ShowStage() bool {
	return s.CookieUsedNote() != ""
}

// ValidateSmartCookiesForm maps checkbox form values to bool.
func ValidateSmartCookiesForm(raw string) bool {
	switch raw {
	case "1", "on", "true", "True", "ON":
		return true
	default:
		return false
	}
}
