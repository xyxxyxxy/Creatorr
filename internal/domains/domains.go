package domains

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// Domain is one known hostname (queue lane + optional limit overrides).
type Domain struct {
	Domain              string
	Active              bool
	TaskCooldownSeconds sql.NullInt64   // Valid=false → use default
	MaxDownloadQueue    sql.NullInt64   // Valid=false → use default
	MaxParallelTasks    sql.NullInt64   // Valid=false → use default
	DownloadRateLimit   sql.NullString  // Valid=false → use default
	SleepRequests       sql.NullFloat64 // Valid=false → use default
	UseFlareSolverr     sql.NullBool    // Valid=false → inherit default; Bool = on/off override
	Username            sql.NullString
	Password            sql.NullString
	UpdatedAt           string
}

// FlareOverrideValue is the form/storage state for use_flaresolverr on host rows.
// Empty / "default" / "off" → SQL NULL (inherit Domain defaults, always off for Access).
// "on" → 1. Host rows do not store 0 (explicit off); Off is indistinguishable from inherit.
const (
	FlareDefault = "default"
	FlareOn      = "on"
	FlareOff     = "off" // accepted on write; stored as NULL
)

// ParseFlareOverride maps form value to SQL bind: nil=NULL inherit, or 1 for On.
// "off" / 0 / false map to NULL (hosts do not store explicit Off).
func ParseFlareOverride(s string) (any, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", FlareDefault, "0", FlareOff, "false":
		return nil, nil
	case "1", FlareOn, "true":
		return 1, nil
	default:
		return nil, fmt.Errorf("invalid use_flaresolverr")
	}
}

// FlareOverrideLabel returns on|default for UI from a scanned NullBool.
// Valid false (legacy 0) is treated as default/NULL for display.
func FlareOverrideLabel(v sql.NullBool) string {
	if v.Valid && v.Bool {
		return FlareOn
	}
	return FlareDefault
}

// EnsureFromURL extracts the hostname from a source URL and inserts an active
// row if missing. Existing rows are left unchanged. Domains are never deleted
// automatically when sources go away.
func EnsureFromURL(database *db.DB, rawURL string) error {
	host := queue.DomainFromURL(rawURL)
	return EnsureHost(database, host)
}

// EnsureHost inserts an active domain row if missing (NULL limits + flare = inherit).
func EnsureHost(database *db.DB, host string) error {
	host = settings.NormalizeDomain(host)
	if host == "" || host == "unknown" || host == "system" || host == settings.DomainDefault {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.SQL.Exec(`
		INSERT INTO domains (domain, active, task_cooldown_seconds, max_download_queue,
			max_parallel_tasks, download_rate_limit, sleep_requests, use_flaresolverr,
			cookies, username, password, updated_at)
		VALUES (?, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, ?)
		ON CONFLICT(domain) DO NOTHING
	`, host, now)
	return err
}

// Get returns one domain row.
func Get(database *db.DB, domain string) (Domain, bool, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" {
		return Domain{}, false, nil
	}
	row := database.SQL.QueryRow(`
		SELECT domain, active, task_cooldown_seconds, max_download_queue, max_parallel_tasks,
			download_rate_limit, sleep_requests, use_flaresolverr,
			username, password, updated_at
		FROM domains WHERE domain = ?
	`, domain)
	d, err := scanDomain(row)
	if err == sql.ErrNoRows {
		return Domain{}, false, nil
	}
	if err != nil {
		return Domain{}, false, err
	}
	return d, true, nil
}

// List returns all domains sorted by name.
func List(database *db.DB) ([]Domain, error) {
	rows, err := database.SQL.Query(`
		SELECT domain, active, task_cooldown_seconds, max_download_queue, max_parallel_tasks,
			download_rate_limit, sleep_requests, use_flaresolverr,
			username, password, updated_at
		FROM domains ORDER BY domain
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListInactive returns domains with active=0.
func ListInactive(database *db.DB) ([]Domain, error) {
	rows, err := database.SQL.Query(`
		SELECT domain, active, task_cooldown_seconds, max_download_queue, max_parallel_tasks,
			download_rate_limit, sleep_requests, use_flaresolverr,
			username, password, updated_at
		FROM domains WHERE active = 0 ORDER BY domain
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// IsActive reports whether tasks for this hostname may run.
// Missing row is treated as active (call EnsureHost for known hosts).
func IsActive(database *db.DB, domain string) (bool, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" {
		return true, nil
	}
	var active int
	err := database.SQL.QueryRow(`SELECT active FROM domains WHERE domain = ?`, domain).Scan(&active)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return active != 0, nil
}

// SetActive updates the active flag (operator toggle only; never auto-changed).
func SetActive(database *db.DB, domain string, active bool) error {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" || domain == settings.DomainDefault {
		return fmt.Errorf("invalid domain")
	}
	if err := EnsureHost(database, domain); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	val := 0
	if active {
		val = 1
	}
	_, err := database.SQL.Exec(`
		UPDATE domains SET active = ?, updated_at = ?
		WHERE domain = ?
	`, val, now, domain)
	return err
}

// Deactivate sets active=0.
func Deactivate(database *db.DB, domain string) error {
	return SetActive(database, domain, false)
}

// UpdateLimits sets optional per-domain overrides and Use FlareSolverr.
// Empty string fields mean NULL (use defaults). flareStr is default|on|off (or empty=default).
// Deprecated for split UI: prefer UpdateCooldown and UpdateSiteLimits.
func UpdateLimits(database *db.DB, domain string, delayStr, rateStr, sleepStr, flareStr string) error {
	if err := UpdateCooldown(database, domain, delayStr); err != nil {
		return err
	}
	return UpdateSiteLimits(database, domain, rateStr, sleepStr, flareStr)
}

// UpdateCooldown sets only task_cooldown_seconds (empty = NULL / use default).
func UpdateCooldown(database *db.DB, domain string, delayStr string) error {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == settings.DomainDefault {
		return fmt.Errorf("invalid domain")
	}
	if err := EnsureHost(database, domain); err != nil {
		return err
	}
	var delay any
	if s := strings.TrimSpace(delayStr); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid task_cooldown_seconds")
		}
		delay = n
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.SQL.Exec(`
		UPDATE domains SET task_cooldown_seconds = ?, updated_at = ?
		WHERE domain = ?
	`, delay, now, domain)
	return err
}

// UpdateSiteLimits sets rate/sleep/Flare only (empty rate/sleep = NULL). Does not touch cooldown.
func UpdateSiteLimits(database *db.DB, domain string, rateStr, sleepStr, flareStr string) error {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == settings.DomainDefault {
		return fmt.Errorf("invalid domain")
	}
	if err := EnsureHost(database, domain); err != nil {
		return err
	}
	var rate, sleep any
	if s := strings.TrimSpace(rateStr); s != "" {
		rate = s
	}
	if s := strings.TrimSpace(sleepStr); s != "" {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f < 0 {
			return fmt.Errorf("invalid sleep_requests")
		}
		sleep = f
	}
	flare, err := ParseFlareOverride(flareStr)
	if err != nil {
		return err
	}
	if flare == 1 {
		if err := settings.RequireFlareSolverrConfigured(); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = database.SQL.Exec(`
		UPDATE domains SET download_rate_limit = ?, sleep_requests = ?, use_flaresolverr = ?, updated_at = ?
		WHERE domain = ?
	`, rate, sleep, flare, now, domain)
	return err
}

// UpdateHostOverrides sets cooldown/queue/parallel/rate/sleep/Flare on a host row (empty = NULL inherit).
// Creates the row if missing. Rejects reserved domain=default.
// flareStr: default|on|off (empty = default / inherit).
// When queue or parallel is set, effective pair must satisfy parallel ≤ queue (resolved vs defaults).
func UpdateHostOverrides(database *db.DB, domain, delayStr, queueStr, parallelStr, rateStr, sleepStr, flareStr string) error {
	if err := settings.ValidateOverrideDomain(domain); err != nil {
		return err
	}
	domain = settings.NormalizeDomain(domain)
	if err := EnsureHost(database, domain); err != nil {
		return err
	}
	def, err := settings.DefaultLimits(database)
	if err != nil {
		return err
	}
	effQueue := def.MaxDownloadQueue
	effParallel := def.MaxParallelTasks
	var delay, maxQ, maxP, rate, sleep any
	if s := strings.TrimSpace(delayStr); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid task_cooldown_seconds")
		}
		delay = n
	}
	if s := strings.TrimSpace(queueStr); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return fmt.Errorf("max download queue must be an integer ≥ 1")
		}
		maxQ = n
		effQueue = n
	}
	if s := strings.TrimSpace(parallelStr); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return fmt.Errorf("max parallel tasks must be an integer ≥ 1")
		}
		maxP = n
		effParallel = n
	}
	if err := settings.ValidateConcurrencyLimits(effQueue, effParallel); err != nil {
		return err
	}
	if s := strings.TrimSpace(rateStr); s != "" {
		rate = s
	}
	if s := strings.TrimSpace(sleepStr); s != "" {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f < 0 {
			return fmt.Errorf("invalid sleep_requests")
		}
		sleep = f
	}
	flare, err := ParseFlareOverride(flareStr)
	if err != nil {
		return err
	}
	if flare == 1 {
		if err := settings.RequireFlareSolverrConfigured(); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = database.SQL.Exec(`
		UPDATE domains SET task_cooldown_seconds = ?, max_download_queue = ?, max_parallel_tasks = ?,
			download_rate_limit = ?, sleep_requests = ?, use_flaresolverr = ?, updated_at = ?
		WHERE domain = ?
	`, delay, maxQ, maxP, rate, sleep, flare, now, domain)
	return err
}

// Delete removes a host domains row (never default).
func Delete(database *db.DB, domain string) error {
	return settings.DeleteDomainOverride(database, domain)
}

// ListHosts returns domain rows excluding the reserved default profile.
func ListHosts(database *db.DB) ([]Domain, error) {
	all, err := List(database)
	if err != nil {
		return nil, err
	}
	out := make([]Domain, 0, len(all))
	for _, d := range all {
		if d.Domain == settings.DomainDefault {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// FlareSolverrURL returns CREATORR_FLARESOLVERR_URL when this hostname has
// Use FlareSolverr enabled on a host override (Domain defaults Flare is always off).
// Empty string means skip FlareSolverr pre-solve. Opt-in without env URL → FlareSolverrRequired.
func FlareSolverrURL(database *db.DB, host string) (string, error) {
	host = settings.NormalizeDomain(host)
	if host == "" || host == "unknown" || host == "system" || host == settings.DomainDefault {
		return "", nil
	}
	lim, err := settings.LimitsForDomain(database, host)
	if err != nil {
		return "", err
	}
	if !lim.UseFlareSolverr {
		return "", nil
	}
	url := settings.FlareSolverrURL()
	if url == "" {
		return "", apperrors.WithDetail(
			apperrors.New(apperrors.CodeFlareSolverrRequired, "FlareSolverr required for this domain"),
			"enable Use FlareSolverr On a host Domain override and set CREATORR_FLARESOLVERR_URL",
		)
	}
	return url, nil
}

// EffectiveLimits resolves delay/rate for a hostname.
func EffectiveLimits(database *db.DB, domain string) (settings.DomainLimits, error) {
	return settings.LimitsForDomain(database, domain)
}

func scanDomain(row interface{ Scan(dest ...any) error }) (Domain, error) {
	var d Domain
	var active int
	var flare sql.NullInt64
	err := row.Scan(&d.Domain, &active, &d.TaskCooldownSeconds, &d.MaxDownloadQueue, &d.MaxParallelTasks,
		&d.DownloadRateLimit, &d.SleepRequests, &flare, &d.Username, &d.Password, &d.UpdatedAt)
	d.Active = active != 0
	if flare.Valid {
		d.UseFlareSolverr = sql.NullBool{Bool: flare.Int64 != 0, Valid: true}
	}
	return d, err
}
