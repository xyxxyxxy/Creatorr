package settings

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// DomainDefault is the reserved domains/cookies key for global defaults.
// Limit columns + use_flaresolverr on this row must always be non-NULL; cookies jar is optional.
const DomainDefault = "default"

// DefaultMaxParallelTasks is the seed / fallback for max_parallel_tasks.
const DefaultMaxParallelTasks = 1

// NormalizeDomain lowercases, trims, and strips a leading www. prefix.
// Used for cookies, domains table keys, handler hosts, and queue lanes.
func NormalizeDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	return strings.TrimPrefix(domain, "www.")
}

// ValidateOverrideDomain checks a hostname for creating/updating a host override.
// Expects raw or normalized input; normalizes before checks. Rejects empty, reserved
// names (default/unknown/system), and non-hostname junk (e.g. example,com, URLs).
func ValidateOverrideDomain(domain string) error {
	domain = NormalizeDomain(domain)
	if domain == "" {
		return fmt.Errorf("domain required")
	}
	if domain == DomainDefault || domain == "unknown" || domain == "system" {
		return fmt.Errorf("reserved domain name")
	}
	if strings.ContainsAny(domain, ":/\\@,#?&!\"'$;%^*()[]{}|=+ ") || strings.Contains(domain, ",") {
		return fmt.Errorf("invalid hostname")
	}
	if !strings.Contains(domain, ".") {
		return fmt.Errorf("invalid hostname")
	}
	for _, lab := range strings.Split(domain, ".") {
		if lab == "" || len(lab) > 63 {
			return fmt.Errorf("invalid hostname")
		}
		if lab[0] == '-' || lab[len(lab)-1] == '-' {
			return fmt.Errorf("invalid hostname")
		}
		for i := 0; i < len(lab); i++ {
			c := lab[i]
			ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
			if !ok {
				return fmt.Errorf("invalid hostname")
			}
		}
	}
	if len(domain) > 253 {
		return fmt.Errorf("invalid hostname")
	}
	return nil
}

// IsReservedDomain reports whether name is the default sentinel (not a host override).
func IsReservedDomain(domain string) bool {
	return NormalizeDomain(domain) == DomainDefault
}

// DomainLimits is queue/yt-dlp knobs (global default or effective resolved values).
type DomainLimits struct {
	TaskCooldownSeconds int
	MaxDownloadQueue    int     // pending+running download-family on this domain
	MaxParallelTasks    int     // concurrent running non-interactive tasks on this domain
	DownloadRateLimit   string  // yt-dlp --limit-rate for archive/scan (not interactive prefetch); "off"/"0"/"none" = unlimited
	SleepRequests       float64 // yt-dlp --sleep-requests + --sleep-subtitles + --sleep-interval; 0 = off; not interactive prefetch
	UseFlareSolverr     bool    // pre-solve via CREATORR_FLARESOLVERR_URL when effective
}

func defaultDomainLimits() DomainLimits {
	return DomainLimits{
		TaskCooldownSeconds: 30,
		MaxDownloadQueue:    DefaultMaxDownloadQueue,
		MaxParallelTasks:    DefaultMaxParallelTasks,
		DownloadRateLimit:   "10M",
		SleepRequests:       1,
		UseFlareSolverr:     false,
	}
}

// RateLimitOff reports whether the rate string means no --limit-rate flag.
func RateLimitOff(rate string) bool {
	s := strings.ToLower(strings.TrimSpace(rate))
	return s == "" || s == "0" || s == "off" || s == "none" || s == "unlimited"
}

func normalizeLimits(v DomainLimits) DomainLimits {
	d := v
	if d.TaskCooldownSeconds < 0 {
		d.TaskCooldownSeconds = defaultDomainLimits().TaskCooldownSeconds
	}
	if d.MaxDownloadQueue < 1 {
		d.MaxDownloadQueue = DefaultMaxDownloadQueue
	}
	if d.MaxParallelTasks < 1 {
		d.MaxParallelTasks = DefaultMaxParallelTasks
	}
	if d.MaxParallelTasks > d.MaxDownloadQueue {
		d.MaxParallelTasks = d.MaxDownloadQueue
	}
	if strings.TrimSpace(d.DownloadRateLimit) == "" {
		d.DownloadRateLimit = defaultDomainLimits().DownloadRateLimit
	}
	if d.SleepRequests < 0 {
		d.SleepRequests = defaultDomainLimits().SleepRequests
	}
	return d
}

// ValidateConcurrencyLimits checks max_parallel_tasks ≤ max_download_queue (both ≥ 1).
func ValidateConcurrencyLimits(maxQueue, maxParallel int) error {
	if maxQueue < 1 {
		return fmt.Errorf("max download queue must be an integer ≥ 1")
	}
	if maxParallel < 1 {
		return fmt.Errorf("max parallel tasks must be an integer ≥ 1")
	}
	if maxParallel > maxQueue {
		return fmt.Errorf("max parallel tasks cannot exceed max download queue")
	}
	return nil
}

// EnsureDefaultDomain inserts the domains row domain=default with non-NULL seeded
// limits when missing. Never overwrites an existing default row.
func EnsureDefaultDomain(database *db.DB) error {
	d := defaultDomainLimits()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.SQL.Exec(`
		INSERT INTO domains (domain, active, task_cooldown_seconds, max_download_queue,
			max_parallel_tasks, download_rate_limit, sleep_requests, use_flaresolverr,
			username, password, updated_at)
		VALUES (?, 1, ?, ?, ?, ?, ?, ?, '', '', ?)
		ON CONFLICT(domain) DO NOTHING
	`, DomainDefault, d.TaskCooldownSeconds, d.MaxDownloadQueue, d.MaxParallelTasks,
		d.DownloadRateLimit, d.SleepRequests, 0, now)
	return err
}

const domainLimitsSelect = `task_cooldown_seconds, max_download_queue, max_parallel_tasks,
	download_rate_limit, sleep_requests, use_flaresolverr`

func scanDomainLimits(delay, maxQ, maxP sql.NullInt64, rate sql.NullString, sleep sql.NullFloat64, flare sql.NullInt64, base DomainLimits) DomainLimits {
	out := base
	if delay.Valid && delay.Int64 >= 0 {
		out.TaskCooldownSeconds = int(delay.Int64)
	}
	if maxQ.Valid && maxQ.Int64 >= 1 {
		out.MaxDownloadQueue = int(maxQ.Int64)
	}
	if maxP.Valid && maxP.Int64 >= 1 {
		out.MaxParallelTasks = int(maxP.Int64)
	}
	if rate.Valid && strings.TrimSpace(rate.String) != "" {
		out.DownloadRateLimit = strings.TrimSpace(rate.String)
	}
	if sleep.Valid && sleep.Float64 >= 0 {
		out.SleepRequests = sleep.Float64
	}
	if flare.Valid {
		out.UseFlareSolverr = flare.Int64 != 0
	}
	return normalizeLimits(out)
}

func applyRateOverride(out *string, rate sql.NullString) {
	if !rate.Valid {
		return
	}
	s := strings.TrimSpace(rate.String)
	if s == "" {
		*out = "off"
	} else {
		*out = s
	}
}

// DefaultLimits returns limits from domains row domain=default (always non-NULL after seed).
func DefaultLimits(database *db.DB) (DomainLimits, error) {
	_ = EnsureDefaultDomain(database)
	var delay, maxQ, maxP sql.NullInt64
	var rate sql.NullString
	var sleep sql.NullFloat64
	var flare sql.NullInt64
	err := database.SQL.QueryRow(`
		SELECT `+domainLimitsSelect+`
		FROM domains WHERE domain = ?
	`, DomainDefault).Scan(&delay, &maxQ, &maxP, &rate, &sleep, &flare)
	if err == sql.ErrNoRows {
		return defaultDomainLimits(), nil
	}
	if err != nil {
		return DomainLimits{}, err
	}
	return scanDomainLimits(delay, maxQ, maxP, rate, sleep, flare, defaultDomainLimits()), nil
}

// LimitsForDomain resolves effective limits: host domains row overrides → default row.
func LimitsForDomain(database *db.DB, domain string) (DomainLimits, error) {
	def, err := DefaultLimits(database)
	if err != nil {
		return DomainLimits{}, err
	}
	domain = NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" || domain == DomainDefault {
		return def, nil
	}
	var delay, maxQ, maxP sql.NullInt64
	var rate sql.NullString
	var sleep sql.NullFloat64
	var flare sql.NullInt64
	err = database.SQL.QueryRow(`
		SELECT `+domainLimitsSelect+`
		FROM domains WHERE domain = ?
	`, domain).Scan(&delay, &maxQ, &maxP, &rate, &sleep, &flare)
	if err == sql.ErrNoRows {
		return def, nil
	}
	if err != nil {
		return def, nil
	}
	out := def
	if delay.Valid && delay.Int64 >= 0 {
		out.TaskCooldownSeconds = int(delay.Int64)
	}
	if maxQ.Valid && maxQ.Int64 >= 1 {
		out.MaxDownloadQueue = int(maxQ.Int64)
	}
	if maxP.Valid && maxP.Int64 >= 1 {
		out.MaxParallelTasks = int(maxP.Int64)
	}
	applyRateOverride(&out.DownloadRateLimit, rate)
	if sleep.Valid && sleep.Float64 >= 0 {
		out.SleepRequests = sleep.Float64
	}
	if flare.Valid {
		out.UseFlareSolverr = flare.Int64 != 0
	}
	return normalizeLimits(out), nil
}

// FlareSolverrConfigured reports whether CREATORR_FLARESOLVERR_URL is non-empty.
func FlareSolverrConfigured() bool {
	return FlareSolverrURL() != ""
}

// RequireFlareSolverrConfigured errors when Use FlareSolverr would be turned on
// without CREATORR_FLARESOLVERR_URL.
func RequireFlareSolverrConfigured() error {
	if !FlareSolverrConfigured() {
		return fmt.Errorf("set CREATORR_FLARESOLVERR_URL first")
	}
	return nil
}

// ClearUseFlareSolverr turns off Domain defaults Use FlareSolverr and clears host
// On overrides (NULL inherit). Call when the FlareSolverr env URL is emptied.
func ClearUseFlareSolverr(database *db.DB) error {
	if err := EnsureDefaultDomain(database); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.SQL.Exec(`
		UPDATE domains SET use_flaresolverr = 0, updated_at = ? WHERE domain = ?
	`, now, DomainDefault); err != nil {
		return err
	}
	_, err := database.SQL.Exec(`
		UPDATE domains SET use_flaresolverr = NULL, updated_at = ?
		WHERE domain != ? AND use_flaresolverr = 1
	`, now, DomainDefault)
	return err
}

// SetDomainDefault writes non-NULL limit values onto domains row domain=default.
func SetDomainDefault(database *db.DB, delay, maxQueue, maxParallel int, rate, sleepStr string, useFlare bool) error {
	if delay < 0 {
		return fmt.Errorf("invalid task_cooldown_seconds")
	}
	if err := ValidateConcurrencyLimits(maxQueue, maxParallel); err != nil {
		return err
	}
	rate = strings.TrimSpace(rate)
	if rate == "" {
		return fmt.Errorf("download_rate_limit required")
	}
	sleepStr = strings.TrimSpace(sleepStr)
	if sleepStr == "" {
		return fmt.Errorf("sleep_requests required")
	}
	sleep, err := strconv.ParseFloat(sleepStr, 64)
	if err != nil || sleep < 0 {
		return fmt.Errorf("invalid sleep_requests")
	}
	if useFlare {
		if err := RequireFlareSolverrConfigured(); err != nil {
			return err
		}
	}
	if err := EnsureDefaultDomain(database); err != nil {
		return err
	}
	flare := 0
	if useFlare {
		flare = 1
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = database.SQL.Exec(`
		UPDATE domains SET task_cooldown_seconds = ?, max_download_queue = ?, max_parallel_tasks = ?,
			download_rate_limit = ?, sleep_requests = ?, use_flaresolverr = ?, active = 1, updated_at = ?
		WHERE domain = ?
	`, delay, maxQueue, maxParallel, rate, sleep, flare, now, DomainDefault)
	return err
}
