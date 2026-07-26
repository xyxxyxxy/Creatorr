package settings

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// Keys stored in the settings table (SQLite + UI). Process bootstrap env is config.Load only - never Settings.
const (
	KeyPotFetch                     = "pot_fetch"
	KeyEpisodeFormat                = "episode_format"
	KeyDownloadWantedCron           = "download_wanted_cron"
	KeyDownloadWantedOrder          = "download_wanted_order"
	KeySyncFilesCron                = "sync_files_cron"
	KeyRetentionDeleteCron          = "retention_delete_cron"
	KeyStatsRetentionDays           = "stats_retention_days"
	KeySourceDownloadErrorThreshold = "source_download_error_threshold"
	KeyCacheBeginningSeconds        = "cache_beginning_seconds"
	KeyStreamPlaybackCache          = "stream_playback_cache"
	KeyStreamPlaybackCacheMaxHours  = "stream_playback_cache_max_hours"
	KeyExternalBaseURL              = "external_base_url"
)

// Help is one-line UI help text per key.
var Help = map[string]string{
	KeyPotFetch:                     "",
	KeyEpisodeFormat:                "Relative path under the series folder for packed episodes (no extension). Saving does not rename existing files - use Apply episode format.",
	KeyDownloadWantedCron:           "Schedule to enqueue wanted videos for monitored series.",
	KeyDownloadWantedOrder:          "Within each series, enqueue wanted downloads oldest-first or newest-first (upload date; undated by id). Series are fair-shared (round-robin, least-loaded first).",
	KeySyncFilesCron:                "Schedule for library file sync: missing/restore, packed media size vs DB (mismatch → verify_failed), and beginning-cache reconcile.",
	KeyRetentionDeleteCron:          "Schedule for deleting files past root retention TTL.",
	KeyStatsRetentionDays:           "",
	KeySourceDownloadErrorThreshold: "When this many videos on a source are wanted_download_error, other wanted videos become wanted_source_error (auto-download held). Minimum 1 (hold after the first error). Default 2.",
	KeyCacheBeginningSeconds:        "Piped streams need a few seconds before playback can start. Caching the beginning beforehand enables instant playback while the rest of the stream loads in the background. Changing this does not alter beginnings already cached.",
	KeyStreamPlaybackCache:          "Enables later plays to stream without re-fetching via yt-dlp.",
	KeyStreamPlaybackCacheMaxHours:  "Rolling total hours of playback cache kept. When over budget, least-recently-played whole-video caches are removed. Does not apply to beginning caches.",
	KeyExternalBaseURL:              "Essential for Creatorr streaming: the external media server (Emby/Jellyfin/Kodi/etc.) plays .strm entries by streaming through Creatorr’s proxy. Absolute origin clients can reach (scheme+host+port, no trailing slash). Empty disables stream delivery. Changing this requires Regenerate all .strm files under Settings → Maintenance.",
	KeySubtitleLangs:                "Supports all, regex (en.*), and -TAG exclusions. Saving does not re-fetch existing episodes.",
	KeySubtitleAuto:                 "Also download auto-generated captions when no custom track exists for that language. Auto-only files are packed as .lang.auto.srt (e.g. .en.auto.srt).",
}

// Labels are human-readable Settings titles (DB/API keys are snake_case).
var Labels = map[string]string{
	KeyPotFetch:                     "PO token fetch",
	KeyEpisodeFormat:                "Episode format",
	KeyDownloadWantedCron:           "Download wanted schedule",
	KeyDownloadWantedOrder:          "Download wanted order",
	KeySyncFilesCron:                "File sync schedule",
	KeyRetentionDeleteCron:          "Retention delete schedule",
	KeyStatsRetentionDays:           "Stats retention",
	KeySourceDownloadErrorThreshold: "Source download error threshold",
	KeyCacheBeginningSeconds:        "Cache beginning of streams",
	KeyStreamPlaybackCache:          "Build cache on playback",
	KeyStreamPlaybackCacheMaxHours:  "Max playback cache",
	KeyExternalBaseURL:              "External Creatorr URL",
	KeySubtitleLangs:                "Subtitle languages",
	KeySubtitleAuto:                 "Include auto-generated captions",
}

// generalOrder is Settings → General (not schedules).
var generalOrder = []string{
	KeyPotFetch,
	KeyStatsRetentionDays,
}

// schedulerOrder is Settings → Scheduler (cron schedules).
var schedulerOrder = []string{
	KeyDownloadWantedCron,
	KeySyncFilesCron,
	KeyRetentionDeleteCron,
}

// queueOrder is Settings → Queue (order, source error threshold).
var queueOrder = []string{
	KeyDownloadWantedOrder,
	KeySourceDownloadErrorThreshold,
}

// libraryOrder is Settings → Library (stream + subtitles; episode_format is separate).
var libraryOrder = []string{
	KeyExternalBaseURL,
	KeyCacheBeginningSeconds,
	KeyStreamPlaybackCache,
	KeyStreamPlaybackCacheMaxHours,
	KeySubtitleLangs,
	KeySubtitleAuto,
}

// CronKeys are schedule settings stored as cron (validated).
var CronKeys = map[string]bool{
	KeyDownloadWantedCron:  true,
	KeySyncFilesCron:       true,
	KeyRetentionDeleteCron: true,
}

// Entry is one settings row for API/UI.
type Entry struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
	Help  string `json:"help"`
}

// SeedDefaults inserts missing Settings keys with hardcoded defaults (first boot). Not from env.
func SeedDefaults(database *db.DB) error {
	if err := migrateLegacySettingKeys(database); err != nil {
		return err
	}
	if err := migrateLegacyTaskKinds(database); err != nil {
		return err
	}
	defaults := map[string]string{
		KeyPotFetch:                     PotFetchAuto,
		KeyEpisodeFormat:                DefaultEpisodeFormat,
		KeyDownloadWantedCron:           "@hourly",
		KeyDownloadWantedOrder:          DownloadWantedOrderOldest,
		KeySyncFilesCron:                "@daily",
		KeyRetentionDeleteCron:          "@daily",
		KeyStatsRetentionDays:           "365",
		KeySourceDownloadErrorThreshold: strconv.Itoa(DefaultSourceDownloadErrorThreshold),
		KeyCacheBeginningSeconds:        strconv.Itoa(DefaultCacheBeginningSeconds),
		KeyStreamPlaybackCache:          DefaultStreamPlaybackCache,
		KeyStreamPlaybackCacheMaxHours:  strconv.Itoa(DefaultStreamPlaybackCacheMaxHours),
		KeyExternalBaseURL:              "",
		KeySubtitleLangs:                DefaultSubtitleLangs,
		KeySubtitleAuto:                 DefaultSubtitleAuto,
	}
	allKeys := append([]string{}, generalOrder...)
	allKeys = append(allKeys, schedulerOrder...)
	allKeys = append(allKeys, queueOrder...)
	allKeys = append(allKeys, libraryOrder...)
	allKeys = append(allKeys, KeyEpisodeFormat)
	for _, key := range allKeys {
		val := defaults[key]
		_, err := database.SQL.Exec(`
			INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO NOTHING
		`, key, val)
		if err != nil {
			return fmt.Errorf("seed setting %s: %w", key, err)
		}
	}
	if err := EnsureDefaultDomain(database); err != nil {
		return fmt.Errorf("seed default domain: %w", err)
	}
	return nil
}

// migrateLegacySettingKeys renames settings keys after task-kind renames.
func migrateLegacySettingKeys(database *db.DB) error {
	renames := []struct{ old, neu string }{
		{"file_sync_cron", KeySyncFilesCron},
		{"retention_purge_cron", KeyRetentionDeleteCron},
		{"download_beginning_seconds", KeyCacheBeginningSeconds},
	}
	for _, r := range renames {
		var n int
		if err := database.SQL.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, r.neu).Scan(&n); err != nil {
			return fmt.Errorf("migrate setting %s: %w", r.neu, err)
		}
		if n > 0 {
			_, _ = database.SQL.Exec(`DELETE FROM settings WHERE key = ?`, r.old)
			continue
		}
		if _, err := database.SQL.Exec(`UPDATE settings SET key = ? WHERE key = ?`, r.neu, r.old); err != nil {
			return fmt.Errorf("rename setting %s→%s: %w", r.old, r.neu, err)
		}
	}
	// Drop removed tip-scan auto-download toggle (no longer used).
	_, _ = database.SQL.Exec(`DELETE FROM settings WHERE key = ?`, "download_new_on_scan")
	return nil
}

// migrateLegacyTaskKinds rewrites queued/history task kind strings to current names.
func migrateLegacyTaskKinds(database *db.DB) error {
	renames := []struct{ old, neu string }{
		{"download_beginning", "cache_beginning"},
		{"metadata_rescan", "rescan_metadata"},
		{"sidecar_refresh", "refresh_sidecars"},
		{"file_sync", "sync_files"},
		{"retention_purge", "retention_delete"},
		{"apply_episode_naming", "rename_episodes"},
		{"nfo_regenerate", "regenerate_nfo"},
		{"strm_regenerate", "regenerate_strm"},
		{"file_delete", "delete_files"},
		{"series_meta_prefetch", "prefetch_series_meta"},
		{"video_meta_prefetch", "prefetch_video_meta"},
		{"add_series_prefetch", "prefetch_add_series"},
		{"add_video_prefetch", "prefetch_add_video"},
	}
	for _, r := range renames {
		if _, err := database.SQL.Exec(`UPDATE tasks SET kind = ? WHERE kind = ?`, r.neu, r.old); err != nil {
			return fmt.Errorf("rename task kind %s→%s: %w", r.old, r.neu, err)
		}
	}
	return nil
}

// All returns General + Scheduler + Queue + Library keys (API listing).
func All(database *db.DB) ([]Entry, error) {
	keys := append(append([]string{}, generalOrder...), schedulerOrder...)
	keys = append(keys, queueOrder...)
	keys = append(keys, libraryOrder...)
	keys = append(keys, KeyEpisodeFormat)
	return entriesFor(database, keys)
}

// General returns Settings → General rows.
func General(database *db.DB) ([]Entry, error) {
	return entriesFor(database, generalOrder)
}

// Scheduler returns Settings → Scheduler rows.
func Scheduler(database *db.DB) ([]Entry, error) {
	return entriesFor(database, schedulerOrder)
}

// Queue returns Settings → Queue rows.
func Queue(database *db.DB) ([]Entry, error) {
	return entriesFor(database, queueOrder)
}

// DomainsSettings is deprecated: threshold lives under Queue.
func DomainsSettings(database *db.DB) ([]Entry, error) {
	return entriesFor(database, []string{KeySourceDownloadErrorThreshold})
}

// LibrarySettings returns Settings → Library global knobs (not roots/profiles tables).
func LibrarySettings(database *db.DB) ([]Entry, error) {
	return entriesFor(database, libraryOrder)
}

func entriesFor(database *db.DB, keys []string) ([]Entry, error) {
	out := make([]Entry, 0, len(keys))
	for _, key := range keys {
		val, err := Get(database, key)
		if err != nil {
			return nil, err
		}
		out = append(out, Entry{Key: key, Label: Labels[key], Value: val, Help: Help[key]})
	}
	return out, nil
}

// Get returns a setting value or empty if missing.
func Get(database *db.DB, key string) (string, error) {
	var v string
	err := database.SQL.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

// Set upserts a known editable key.
func Set(database *db.DB, key, value string) error {
	if _, ok := Help[key]; !ok {
		return fmt.Errorf("unknown setting key %q", key)
	}
	if key == KeyExternalBaseURL {
		value = NormalizeExternalBaseURL(value)
	}
	if key == KeySubtitleLangs {
		value = SubtitleLangsJSON(ParseSubtitleLangsJSON(value))
	}
	if key == KeySubtitleAuto {
		value = NormalizeSubtitleAuto(value)
	}
	if key == KeySourceDownloadErrorThreshold {
		value = NormalizeSourceDownloadErrorThreshold(value)
	}
	if err := validateValue(key, value); err != nil {
		return err
	}
	_, err := database.SQL.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// SetMany upserts multiple keys (all must be known).
func SetMany(database *db.DB, values map[string]string) error {
	tx, err := database.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for k, v := range values {
		if _, ok := Help[k]; !ok {
			return fmt.Errorf("unknown setting key %q", k)
		}
		if k == KeyExternalBaseURL {
			v = NormalizeExternalBaseURL(v)
			values[k] = v
		}
		if k == KeySubtitleLangs {
			v = SubtitleLangsJSON(ParseSubtitleLangsJSON(v))
			values[k] = v
		}
		if k == KeySubtitleAuto {
			v = NormalizeSubtitleAuto(v)
			values[k] = v
		}
		if k == KeySourceDownloadErrorThreshold {
			v = NormalizeSourceDownloadErrorThreshold(v)
			values[k] = v
		}
		if err := validateValue(k, v); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DomainQueueRow is one host override row for Settings → Queue (domain != default).
type DomainQueueRow struct {
	Domain               string
	TaskCooldownSeconds  int     // effective value for display
	MaxDownloadQueue     int     // effective
	MaxParallelTasks     int     // effective
	DownloadRateLimit    string  // effective value for display
	StreamPlayRateLimit  string  // effective value for display
	SleepRequests        float64 // effective value for display
	CooldownOverride      string  // empty = using default
	QueueOverride        string  // empty = using default
	ParallelOverride     string  // empty = using default
	RateOverride         string  // empty = using default
	StreamPlayRateOverride string // empty = using default
	SleepOverride        string  // empty = using default
	FlareOverride        string  // "", "on", "off" - empty = inherit Domain defaults
	Active               bool
	UseFlareSolverr      bool // effective resolved flare (defaults + override)
	HasCookies           bool
	CookieContent        string // Netscape jar text for edit modal
	HasRow               bool   // true when domains row exists
}

// DomainOverrideRows returns host override rows (excludes domain=default).
func DomainOverrideRows(database *db.DB) ([]DomainQueueRow, error) {
	def, err := DefaultLimits(database)
	if err != nil {
		return nil, err
	}
	cookieByDomain := map[string]string{}
	if crows, err := database.SQL.Query(`SELECT domain, content FROM cookies`); err == nil {
		for crows.Next() {
			var d, content string
			if err := crows.Scan(&d, &content); err != nil {
				_ = crows.Close()
				return nil, err
			}
			cookieByDomain[NormalizeDomain(d)] = content
		}
		if err := crows.Err(); err != nil {
			_ = crows.Close()
			return nil, err
		}
		if err := crows.Close(); err != nil {
			return nil, err
		}
	}
	rows, err := database.SQL.Query(`
		SELECT domain, active, task_cooldown_seconds, max_download_queue, max_parallel_tasks,
		       download_rate_limit, stream_play_rate_limit, sleep_requests, use_flaresolverr
		FROM domains WHERE domain != ? ORDER BY domain
	`, DomainDefault)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DomainQueueRow
	seen := map[string]bool{}
	for rows.Next() {
		var host string
		var active int
		var delay, maxQ, maxP sql.NullInt64
		var rate, streamRate sql.NullString
		var sleep sql.NullFloat64
		var flare sql.NullInt64
		if err := rows.Scan(&host, &active, &delay, &maxQ, &maxP, &rate, &streamRate, &sleep, &flare); err != nil {
			return nil, err
		}
		r := DomainQueueRow{
			Domain: host, Active: active != 0, HasRow: true,
			TaskCooldownSeconds: def.TaskCooldownSeconds,
			MaxDownloadQueue:    def.MaxDownloadQueue,
			MaxParallelTasks:    def.MaxParallelTasks,
			DownloadRateLimit:   def.DownloadRateLimit,
			StreamPlayRateLimit: def.StreamPlayRateLimit,
			SleepRequests:       def.SleepRequests,
			UseFlareSolverr:     def.UseFlareSolverr,
			CookieContent:       cookieByDomain[NormalizeDomain(host)],
		}
		r.HasCookies = strings.TrimSpace(r.CookieContent) != ""
		if delay.Valid && delay.Int64 >= 0 {
			r.TaskCooldownSeconds = int(delay.Int64)
			r.CooldownOverride = strconv.FormatInt(delay.Int64, 10)
		}
		if maxQ.Valid && maxQ.Int64 >= 1 {
			r.MaxDownloadQueue = int(maxQ.Int64)
			r.QueueOverride = strconv.FormatInt(maxQ.Int64, 10)
		}
		if maxP.Valid && maxP.Int64 >= 1 {
			r.MaxParallelTasks = int(maxP.Int64)
			r.ParallelOverride = strconv.FormatInt(maxP.Int64, 10)
		}
		if rate.Valid {
			s := strings.TrimSpace(rate.String)
			if s == "" {
				s = "off"
			}
			r.DownloadRateLimit = s
			r.RateOverride = s
		}
		if streamRate.Valid {
			s := strings.TrimSpace(streamRate.String)
			if s == "" {
				s = "off"
			}
			r.StreamPlayRateLimit = s
			r.StreamPlayRateOverride = s
		}
		if sleep.Valid && sleep.Float64 >= 0 {
			r.SleepRequests = sleep.Float64
			r.SleepOverride = strconv.FormatFloat(sleep.Float64, 'f', -1, 64)
		}
		if flare.Valid {
			if flare.Int64 != 0 {
				r.FlareOverride = "on"
				r.UseFlareSolverr = true
			} else {
				r.FlareOverride = "off"
				r.UseFlareSolverr = false
			}
		}
		out = append(out, r)
		seen[NormalizeDomain(host)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Host cookie jars without a domains row still appear (editable via override modal).
	for host, content := range cookieByDomain {
		host = NormalizeDomain(host)
		if host == "" || host == DomainDefault || seen[host] {
			continue
		}
		out = append(out, DomainQueueRow{
			Domain: host, Active: true, HasRow: false,
			TaskCooldownSeconds: def.TaskCooldownSeconds,
			MaxDownloadQueue:    def.MaxDownloadQueue,
			MaxParallelTasks:    def.MaxParallelTasks,
			DownloadRateLimit:   def.DownloadRateLimit,
			StreamPlayRateLimit: def.StreamPlayRateLimit,
			SleepRequests:       def.SleepRequests,
			UseFlareSolverr:     def.UseFlareSolverr,
			CookieContent:       content,
			HasCookies:          strings.TrimSpace(content) != "",
		})
	}
	return out, nil
}

// DomainQueueRows is an alias for DomainOverrideRows (legacy name).
func DomainQueueRows(database *db.DB) ([]DomainQueueRow, error) {
	return DomainOverrideRows(database)
}

// DeleteDomainOverride removes a host domains row (never default). Caller should also clear host cookies.
func DeleteDomainOverride(database *db.DB, domain string) error {
	domain = NormalizeDomain(domain)
	if domain == "" || domain == DomainDefault {
		return fmt.Errorf("cannot delete default domain")
	}
	res, err := database.SQL.Exec(`DELETE FROM domains WHERE domain = ?`, domain)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("domain not found")
	}
	return nil
}
