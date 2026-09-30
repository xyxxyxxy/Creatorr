package domains

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// Smart cookie learning defaults (documented in docs/ytdlp.md).
const (
	CookieSmartRingSize        = 8
	CookieSmartPreferThreshold = 4
	CookieSmartRecoverAnon     = 2
	CookieSmartProbeEvery      = 8

	CookieOutcomeFallback    = "fallback"
	CookieOutcomeAnonOK      = "anon_ok"
	CookieOutcomeCookiesOK   = "cookies_ok"
	CookieOutcomeProbeAnonOK = "probe_anon_ok"
)

// CookieSmartState is persisted learning for one source.
type CookieSmartState struct {
	Prefer bool
	Ring   []string
	N      int
}

// CookieSmartDisplay is Status-panel fields for source detail.
type CookieSmartDisplay struct {
	Enabled     bool   // host smart_cookies on and jar present
	Prefer      bool
	PolicyLabel string // "prefer cookies" | "anon first"
	RingSummary string // e.g. "4 fallback / 8"
	NextProbe   string // e.g. "in 3 invokes" or "-" when not preferring
	FallbackN   int
	RingLen     int
}

// DecideAttach chooses cookies-first vs anon-first for a prefer_cookies source.
// When prefer is false, always anon-first (not a probe). When prefer is true,
// bump n and every CookieSmartProbeEvery-th invoke is an anonymous probe.
func DecideAttach(prefer bool, n int) (cookiesFirst, probe bool, nextN int) {
	if !prefer {
		return false, false, n
	}
	nextN = n + 1
	if nextN%CookieSmartProbeEvery == 0 {
		return false, true, nextN
	}
	return true, false, nextN
}

// ApplyOutcome pushes outcome onto ring and updates prefer.
// cookies_ok is neutral for prefer on/off.
// prefer turns on at ≥ CookieSmartPreferThreshold fallbacks.
// prefer clears only when proven-anonymous count ≥ CookieSmartRecoverAnon.
func ApplyOutcome(st CookieSmartState, outcome string) CookieSmartState {
	if outcome == "" {
		return st
	}
	st.Ring = append(st.Ring, outcome)
	if len(st.Ring) > CookieSmartRingSize {
		st.Ring = st.Ring[len(st.Ring)-CookieSmartRingSize:]
	}
	fallback := 0
	anon := 0
	for _, o := range st.Ring {
		switch o {
		case CookieOutcomeFallback:
			fallback++
		case CookieOutcomeAnonOK, CookieOutcomeProbeAnonOK:
			anon++
		}
	}
	if fallback >= CookieSmartPreferThreshold {
		st.Prefer = true
	}
	if anon >= CookieSmartRecoverAnon {
		st.Prefer = false
	}
	return st
}

// OutcomeFromAttach maps a successful cookie-attach status to a ring outcome.
// Empty means skip recording (off / omitted / worthless / failure).
func OutcomeFromAttach(st CookieAttachStatus, probe bool) string {
	switch st.State {
	case CookieAttachAnonymous:
		if probe {
			return CookieOutcomeProbeAnonOK
		}
		return CookieOutcomeAnonOK
	case CookieAttachRetried:
		return CookieOutcomeFallback
	case CookieAttachCookies:
		return CookieOutcomeCookiesOK
	default:
		return ""
	}
}

// LoadCookieSmart reads learning columns for a source (missing → empty).
func LoadCookieSmart(database *db.DB, sourceID int64) (CookieSmartState, error) {
	var st CookieSmartState
	if sourceID <= 0 {
		return st, nil
	}
	var prefer int
	var ringJSON string
	var n int
	err := database.SQL.QueryRow(`
		SELECT cookie_smart_prefer, cookie_smart_ring, cookie_smart_n FROM sources WHERE id = ?
	`, sourceID).Scan(&prefer, &ringJSON, &n)
	if err == sql.ErrNoRows {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Prefer = prefer != 0
	st.N = n
	if ringJSON != "" && ringJSON != "[]" {
		_ = json.Unmarshal([]byte(ringJSON), &st.Ring)
	}
	return st, nil
}

// SaveCookieSmart writes learning columns for a source.
func SaveCookieSmart(database *db.DB, sourceID int64, st CookieSmartState) error {
	if sourceID <= 0 {
		return nil
	}
	prefer := 0
	if st.Prefer {
		prefer = 1
	}
	ring, err := json.Marshal(st.Ring)
	if err != nil {
		return err
	}
	if ring == nil {
		ring = []byte("[]")
	}
	res, err := database.SQL.Exec(`
		UPDATE sources SET cookie_smart_prefer = ?, cookie_smart_ring = ?, cookie_smart_n = ? WHERE id = ?
	`, prefer, string(ring), st.N, sourceID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("source %d not found", sourceID)
	}
	return nil
}

// ResetCookieSmart clears learning for one source.
func ResetCookieSmart(database *db.DB, sourceID int64) error {
	return SaveCookieSmart(database, sourceID, CookieSmartState{Ring: nil, N: 0, Prefer: false})
}

// WipeCookieSmartForHost clears learning on all sources whose URL host matches domain.
func WipeCookieSmartForHost(database *db.DB, domain string) error {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" || domain == settings.DomainDefault {
		return nil
	}
	rows, err := database.SQL.Query(`SELECT id, url FROM sources WHERE url != ''`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		if queue.DomainFromURL(raw) == domain {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := ResetCookieSmart(database, id); err != nil {
			return err
		}
	}
	return nil
}

// ClaimCookieSmart decides attach policy under a short write transaction and
// bumps cookie_smart_n when preferring cookies (probe cadence).
// smartOn false → cookies first (always jar). sourceID 0 with smartOn → anon first, no persist.
func ClaimCookieSmart(database *db.DB, sourceID int64, smartOn bool) (cookiesFirst, probe, prefer bool, err error) {
	if !smartOn {
		return true, false, false, nil
	}
	if sourceID <= 0 {
		return false, false, false, nil
	}
	tx, err := database.SQL.Begin()
	if err != nil {
		return false, false, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var preferI, n int
	var ringJSON string
	err = tx.QueryRow(`
		SELECT cookie_smart_prefer, cookie_smart_ring, cookie_smart_n FROM sources WHERE id = ?
	`, sourceID).Scan(&preferI, &ringJSON, &n)
	if err == sql.ErrNoRows {
		return false, false, false, tx.Commit()
	}
	if err != nil {
		return false, false, false, err
	}
	prefer = preferI != 0
	cookiesFirst, probe, nextN := DecideAttach(prefer, n)
	if nextN != n {
		if _, err := tx.Exec(`UPDATE sources SET cookie_smart_n = ? WHERE id = ?`, nextN, sourceID); err != nil {
			return false, false, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, false, false, err
	}
	return cookiesFirst, probe, prefer, nil
}

// RecordCookieSmartOutcome appends an outcome and updates prefer under a short write transaction.
func RecordCookieSmartOutcome(database *db.DB, sourceID int64, outcome string) error {
	if sourceID <= 0 || outcome == "" {
		return nil
	}
	tx, err := database.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var preferI, n int
	var ringJSON string
	err = tx.QueryRow(`
		SELECT cookie_smart_prefer, cookie_smart_ring, cookie_smart_n FROM sources WHERE id = ?
	`, sourceID).Scan(&preferI, &ringJSON, &n)
	if err == sql.ErrNoRows {
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	st := CookieSmartState{Prefer: preferI != 0, N: n}
	if ringJSON != "" && ringJSON != "[]" {
		_ = json.Unmarshal([]byte(ringJSON), &st.Ring)
	}
	st = ApplyOutcome(st, outcome)
	prefer := 0
	if st.Prefer {
		prefer = 1
	}
	ring, err := json.Marshal(st.Ring)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE sources SET cookie_smart_prefer = ?, cookie_smart_ring = ?, cookie_smart_n = ? WHERE id = ?
	`, prefer, string(ring), st.N, sourceID); err != nil {
		return err
	}
	return tx.Commit()
}

// CookieSmartDisplayForSource builds Status fields when smart is on and a jar exists.
func CookieSmartDisplayForSource(database *db.DB, sourceID int64, sourceURL string) (CookieSmartDisplay, error) {
	var out CookieSmartDisplay
	smart, err := SmartCookiesForURL(database, sourceURL)
	if err != nil || !smart {
		return out, err
	}
	host := queue.DomainFromURL(sourceURL)
	content, err := ResolveCookies(database, host)
	if err != nil || content == "" {
		return out, err
	}
	st, err := LoadCookieSmart(database, sourceID)
	if err != nil {
		return out, err
	}
	out.Enabled = true
	out.Prefer = st.Prefer
	if st.Prefer {
		out.PolicyLabel = "prefer cookies"
	} else {
		out.PolicyLabel = "anon first"
	}
	fallback := 0
	for _, o := range st.Ring {
		if o == CookieOutcomeFallback {
			fallback++
		}
	}
	out.FallbackN = fallback
	out.RingLen = len(st.Ring)
	out.RingSummary = fmt.Sprintf("%d fallback / %d", fallback, CookieSmartRingSize)
	if !st.Prefer {
		out.NextProbe = "-"
	} else {
		rem := CookieSmartProbeEvery - (st.N % CookieSmartProbeEvery)
		if rem == CookieSmartProbeEvery {
			rem = CookieSmartProbeEvery
		}
		// After claim, n already counts current; display remaining until next probe boundary.
		out.NextProbe = fmt.Sprintf("in %d invokes", rem)
	}
	return out, nil
}
