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

// Cookie attach outcome states for download task detail / Stages.
const (
	CookieAttachOff       = "off"       // no stored jar
	CookieAttachAnonymous = "anonymous" // download succeeded without account jar
	CookieAttachCookies   = "cookies"   // jar used (flag off, or always-attach path)
	CookieAttachRetried   = "retried"   // download succeeded after cookie retry
	CookieAttachOmitted   = "omitted"   // flag on; jar available but not used this invoke
)

// CookieAttachStatus is stored under tasks.detail cookie-attach.
type CookieAttachStatus struct {
	State       string `json:"state"`                  // off|anonymous|cookies|retried|omitted
	RetryReason string `json:"retry_reason,omitempty"` // first-failure code when retried
	Detail      string `json:"detail,omitempty"`
	AfterFail   bool   `json:"after_fail,omitempty"` // domains.cookies_after_fail at task time
}

// CookiesAfterFail reports domains.cookies_after_fail for a hostname.
// Missing row → false (today's always-attach).
func CookiesAfterFail(database *db.DB, domain string) (bool, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" || domain == settings.DomainDefault {
		return false, nil
	}
	var v sql.NullInt64
	err := database.SQL.QueryRow(`SELECT cookies_after_fail FROM domains WHERE domain = ?`, domain).Scan(&v)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return v.Valid && v.Int64 != 0, nil
}

// CookiesAfterFailForURL resolves the host from rawURL then CookiesAfterFail.
func CookiesAfterFailForURL(database *db.DB, rawURL string) (bool, error) {
	return CookiesAfterFail(database, queue.DomainFromURL(rawURL))
}

// SetCookiesAfterFail sets domains.cookies_after_fail on a host override.
func SetCookiesAfterFail(database *db.DB, domain string, on bool) error {
	if err := settings.ValidateOverrideDomain(domain); err != nil {
		return err
	}
	domain = settings.NormalizeDomain(domain)
	if err := EnsureHost(database, domain); err != nil {
		return err
	}
	val := 0
	if on {
		val = 1
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.SQL.Exec(`
		UPDATE domains SET cookies_after_fail = ?, updated_at = ? WHERE domain = ?
	`, val, now, domain)
	return err
}

// AllowStoredJar reports whether the stored Netscape jar may be attached.
// When cookies_after_fail is off, always allow. When on, allow only on the
// download retry pass (downloadRetryPass true).
func AllowStoredJar(cookiesAfterFail, downloadRetryPass bool) bool {
	if !cookiesAfterFail {
		return true
	}
	return downloadRetryPass
}

// CookieAttachStage is one Stages note derived from cookie-attach detail.
type CookieAttachStage struct {
	Event    string
	Message  string
	HasError bool
}

// SplitDownloadAttempts reports whether Stages should show two download nodes
// (anonymous failure, then cookie retry) under cookies-after-fail.
func (s CookieAttachStatus) SplitDownloadAttempts() bool {
	return s.AfterFail && s.State == CookieAttachRetried
}

// CookieUsedNote is the Stages line when cookies were actually attached.
// Empty for anonymous / omitted / off (unused). Setting (after-fail vs always) does not matter.
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

// ValidateCookiesAfterFailForm maps checkbox form values to bool.
func ValidateCookiesAfterFailForm(raw string) bool {
	switch raw {
	case "1", "on", "true", "True", "ON":
		return true
	default:
		return false
	}
}