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
	KeyMetadataDomainTag            = "metadata_domain_tag"
	KeyMetadataGenresFromCategories = "metadata_genres_from_categories"
)

// Help is one-line UI help text per key.
var Help = map[string]string{
	KeyPotFetch:                     "",
	KeyEpisodeFormat:                "Relative path under the series folder for packed episodes (no extension). Saving does not rename existing files - use Apply episode format.",
	KeyDownloadWantedCron:           "Schedule to enqueue wanted videos for monitored series.",
	KeyDownloadWantedOrder:          "Which wanted videos to download first inside each series (by upload date; no date uses id). Series take turns so one series does not fill the whole queue.",
	KeySyncFilesCron:                "Library scan will detect changed files in the root folders and cache directories.",
	KeyRetentionDeleteCron:          "Deleting old data according to root folder retention ('Settings → Library').",
	KeyStatsRetentionDays:           "",
	KeySourceDownloadErrorThreshold: "When this many videos of a source enter an error state, other videos from that source are held until the issue is resolved.\nSet to 1 so the first error stops further downloads from that source.",
	KeySubtitleLangs:                "Supports all, regex (en.*), and -TAG exclusions. Saving does not re-fetch existing episodes.",
	KeySubtitleAuto:                 "Also download auto-generated subtitles when no custom track exists for that language. Auto-only files are packed as .lang.auto.srt (e.g. .en.auto.srt).",
	KeyMetadataDomainTag:            "On download and metadata rescan, prepend the source domain to video tags when source_url is known.",
	KeyMetadataGenresFromCategories: "On download and metadata rescan, add yt-dlp categories as video genres when categories are known.",
}

// Labels are human-readable Settings titles (DB/API keys are snake_case).
var Labels = map[string]string{
	KeyPotFetch:                     "PO token fetch",
	KeyEpisodeFormat:                "Episode format",
	KeyDownloadWantedCron:           "Download wanted schedule",
	KeyDownloadWantedOrder:          "Download wanted order",
	KeySyncFilesCron:                "File sync schedule",
	KeyRetentionDeleteCron:          "Retention delete schedule",
	KeyStatsRetentionDays:           "Retention",
	KeySourceDownloadErrorThreshold: "Source download error threshold",
	KeySubtitleLangs:                "Subtitle languages",
	KeySubtitleAuto:                 "Include auto-generated subtitles",
	KeyMetadataDomainTag:            "Add source domain as video tag",
	KeyMetadataGenresFromCategories: "Add genres from yt-dlp categories",
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

// queueOrder is Settings → Queue / Domains (order, source error threshold).
var queueOrder = []string{
	KeyDownloadWantedOrder,
	KeySourceDownloadErrorThreshold,
}

// libraryOrder is Settings → Library (subtitles; episode_format is separate).
var libraryOrder = []string{
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
		KeySubtitleLangs:                DefaultSubtitleLangs,
		KeySubtitleAuto:                 DefaultSubtitleAuto,
		KeyMetadataDomainTag:            DefaultMetadataDomainTag,
		KeyMetadataGenresFromCategories: DefaultMetadataGenresFromCategories,
	}
	allKeys := append([]string{}, generalOrder...)
	allKeys = append(allKeys, schedulerOrder...)
	allKeys = append(allKeys, queueOrder...)
	allKeys = append(allKeys, libraryOrder...)
	allKeys = append(allKeys, KeyEpisodeFormat)
	allKeys = append(allKeys, KeyMetadataDomainTag, KeyMetadataGenresFromCategories)
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
	// Drop removed settings no longer used.
	for _, key := range []string{"download_new_on_scan"} {
		_, _ = database.SQL.Exec(`DELETE FROM settings WHERE key = ?`, key)
	}
	return nil
}

// migrateLegacyTaskKinds rewrites queued/history task kind strings to current names.
func migrateLegacyTaskKinds(database *db.DB) error {
	renames := []struct{ old, neu string }{
		{"metadata_rescan", "rescan_metadata"},
		{"sidecar_refresh", "refresh_sidecars"},
		{"file_sync", "sync_files"},
		{"retention_purge", "retention_delete"},
		{"apply_episode_naming", "rename_episodes"},
		{"nfo_regenerate", "regenerate_nfo"},
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

// Queue returns Settings → Queue / Domains rows.
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
	if key == KeySubtitleLangs {
		value = SubtitleLangsJSON(ParseSubtitleLangsJSON(value))
	}
	if key == KeySubtitleAuto {
		value = NormalizeSubtitleAuto(value)
	}
	if key == KeyMetadataDomainTag || key == KeyMetadataGenresFromCategories {
		value = NormalizeMetadataFlag(value)
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
		if k == KeySubtitleLangs {
			v = SubtitleLangsJSON(ParseSubtitleLangsJSON(v))
			values[k] = v
		}
		if k == KeySubtitleAuto {
			v = NormalizeSubtitleAuto(v)
			values[k] = v
		}
		if k == KeyMetadataDomainTag || k == KeyMetadataGenresFromCategories {
			v = NormalizeMetadataFlag(v)
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

// DomainQueueRow is one host override row for Settings → Queue / Domains (domain != default).
type DomainQueueRow struct {
	Domain              string
	TaskCooldownSeconds int     // effective value for display
	MaxDownloadQueue    int     // effective
	MaxParallelTasks    int     // effective
	DownloadRateLimit   string  // effective value for display
	SleepRequests       float64 // effective value for display
	CooldownOverride    string  // empty = using default
	QueueOverride       string  // empty = using default
	ParallelOverride    string  // empty = using default
	RateOverride        string  // empty = using default
	SleepOverride       string  // empty = using default
	FlareOverride       string  // "", "on", "off" - empty = inherit Domain defaults
	Active              bool
	UseFlareSolverr     bool // effective resolved flare (defaults + override)
	HasCookies          bool
	CookieContent       string // Netscape jar text for edit modal
	HasCredentials              bool   // host row sets non-empty username
	CredentialsUsername         string // host override username for edit modal
	CredentialsInherit          bool   // host row username NULL (inherit default)
	CredentialsHasStoredPassword bool  // host row has stored password (edit modal hint)
	HasRow                      bool   // true when domains row exists
}

// DomainOverrideRows returns host override rows (excludes domain=default).
func DomainOverrideRows(database *db.DB) ([]DomainQueueRow, error) {
	def, err := DefaultLimits(database)
	if err != nil {
		return nil, err
	}
	rows, err := database.SQL.Query(`
		SELECT domain, active, task_cooldown_seconds, max_download_queue, max_parallel_tasks,
		       download_rate_limit, sleep_requests, use_flaresolverr, cookies, username, password
		FROM domains WHERE domain != ? ORDER BY domain
	`, DomainDefault)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DomainQueueRow
	for rows.Next() {
		var host string
		var active int
		var delay, maxQ, maxP sql.NullInt64
		var rate sql.NullString
		var sleep sql.NullFloat64
		var flare sql.NullInt64
		var jar, credUser, credPass sql.NullString
		if err := rows.Scan(&host, &active, &delay, &maxQ, &maxP, &rate, &sleep, &flare, &jar, &credUser, &credPass); err != nil {
			return nil, err
		}
		r := DomainQueueRow{
			Domain: host, Active: active != 0, HasRow: true,
			TaskCooldownSeconds: def.TaskCooldownSeconds,
			MaxDownloadQueue:    def.MaxDownloadQueue,
			MaxParallelTasks:    def.MaxParallelTasks,
			DownloadRateLimit:   def.DownloadRateLimit,
			SleepRequests:       def.SleepRequests,
			UseFlareSolverr:     def.UseFlareSolverr,
		}
		if jar.Valid {
			r.CookieContent = jar.String
			r.HasCookies = strings.TrimSpace(jar.String) != ""
		}
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
		if sleep.Valid && sleep.Float64 >= 0 {
			r.SleepRequests = sleep.Float64
			r.SleepOverride = strconv.FormatFloat(sleep.Float64, 'f', -1, 64)
		}
		if flare.Valid {
			if flare.Int64 != 0 {
				r.FlareOverride = "on"
				r.UseFlareSolverr = true
			} else {
				// Legacy host 0: treat as NULL inherit for UI (do not show muted "off").
				r.UseFlareSolverr = false
			}
		}
		if credUser.Valid {
			u := strings.TrimSpace(credUser.String)
			r.CredentialsUsername = u
			r.HasCredentials = u != ""
		} else {
			r.CredentialsInherit = true
		}
		r.CredentialsHasStoredPassword = credPass.Valid && strings.TrimSpace(credPass.String) != ""
		out = append(out, r)
	}
	return out, rows.Err()
}

// DomainQueueRows is an alias for DomainOverrideRows (legacy name).
func DomainQueueRows(database *db.DB) ([]DomainQueueRow, error) {
	return DomainOverrideRows(database)
}

// DeleteDomainOverride removes a host domains row (never default). Jar text lives on the row and is deleted with it.
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
