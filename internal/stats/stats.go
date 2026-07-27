// Package stats samples queue and library metrics for the Stats page.
// Independent of the main scheduler - samples every minute (change-only writes).
package stats

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// SampleCron is the fixed schedule for minute metrics (UTC).
const SampleCron = "* * * * *"

// DailySampleCron is the fixed schedule for library size samples (UTC midnight).
const DailySampleCron = "0 0 * * *"

// StorageChartMaxDays caps the Storage development chart display window.
const StorageChartMaxDays = 365

// Metric names stored in stats_samples.metric.
const (
	MetricQueueDownload    = "queue_download"
	MetricQueueScan        = "queue_scan"
	MetricVideosWanted     = "videos_wanted"
	MetricVideosDownloaded = "videos_downloaded"
	MetricLibrarySizeBytes = "library_size_bytes"
)

var minuteMetrics = []string{
	MetricQueueDownload,
	MetricQueueScan,
	MetricVideosWanted,
	MetricVideosDownloaded,
}

// Sampler runs sampling independently of other Creatorr schedules.
type Sampler struct {
	DB   *db.DB
	Log  *slog.Logger
	Tick time.Duration
	Now  func() time.Time

	lastSample      time.Time
	lastDailySample time.Time

	mu        sync.Mutex
	lastValue map[string]int // metric -> last written value
	cacheOK   bool
}

// Run blocks until ctx is cancelled.
func (s *Sampler) Run(ctx context.Context) {
	if s.Tick <= 0 {
		s.Tick = 15 * time.Second
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	t := time.NewTicker(s.Tick)
	defer t.Stop()
	s.TickOnce(log)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.TickOnce(log)
		}
	}
}

// TickOnce samples + prunes when minute and/or daily schedules are due.
func (s *Sampler) TickOnce(log *slog.Logger) {
	if s.DB == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	now := s.Now().UTC()
	s.ensureLastCache(log)

	minuteDue, err := cronexpr.Due(SampleCron, s.lastSample, now)
	if err != nil {
		log.Error("stats cron", "err", err)
		return
	}
	dailyDue, err := cronexpr.Due(DailySampleCron, s.lastDailySample, now)
	if err != nil {
		log.Error("stats daily cron", "err", err)
		return
	}
	if !minuteDue && !dailyDue {
		return
	}

	if minuteDue {
		if err := s.Sample(now); err != nil {
			log.Error("stats sample", "err", err)
		} else {
			s.lastSample = now
		}
	}
	if dailyDue {
		if err := s.SampleLibrarySize(now); err != nil {
			log.Error("stats library size sample", "err", err)
		} else {
			s.lastDailySample = now
		}
	}

	days := retentionDays(s.DB)
	if days >= 0 {
		if n, err := Prune(s.DB, now, days); err != nil {
			log.Error("stats prune", "err", err)
		} else if n > 0 {
			log.Info("stats prune", "deleted", n, "retention_days", days)
		}
	}
}

func (s *Sampler) ensureLastCache(log *slog.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cacheOK {
		return
	}
	s.lastValue = map[string]int{}
	rows, err := s.DB.SQL.Query(`
		SELECT metric, value FROM stats_samples s
		WHERE sampled_at = (
			SELECT MAX(sampled_at) FROM stats_samples s2 WHERE s2.metric = s.metric
		)
	`)
	if err != nil {
		log.Warn("stats last-value cache load", "err", err)
		s.cacheOK = true
		return
	}
	defer rows.Close()
	for rows.Next() {
		var metric string
		var value int
		if err := rows.Scan(&metric, &value); err != nil {
			log.Warn("stats last-value cache scan", "err", err)
			break
		}
		s.lastValue[metric] = value
	}
	s.cacheOK = true
}

func (s *Sampler) shouldWrite(metric string, value int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastValue == nil {
		s.lastValue = map[string]int{}
	}
	prev, ok := s.lastValue[metric]
	if ok && prev == value {
		return false
	}
	s.lastValue[metric] = value
	return true
}

func retentionDays(database *db.DB) int {
	raw, _ := settings.Get(database, settings.KeyStatsRetentionDays)
	return settings.ParseStatsRetentionDays(raw)
}

// Sample records global queue + video status counts (change-only writes).
func (s *Sampler) Sample(at time.Time) error {
	return SampleWithWriter(s.DB, at, s.shouldWrite)
}

// Sample is a package-level helper for tests (always writes).
func Sample(database *db.DB, at time.Time) error {
	return SampleWithWriter(database, at, func(string, int) bool { return true })
}

// SampleWithWriter records minute metrics; writeFn returns false to skip unchanged values.
func SampleWithWriter(database *db.DB, at time.Time, writeFn func(metric string, value int) bool) error {
	at = at.UTC().Truncate(time.Minute)
	ts := at.Format(time.RFC3339)

	var qd, qs int
	rows, err := database.SQL.Query(`
		SELECT kind, COUNT(*) FROM tasks
		WHERE status IN (?, ?) AND kind IN (?, ?)
		GROUP BY kind
	`, queue.StatusPending, queue.StatusRunning, queue.KindDownload, queue.KindScan)
	if err != nil {
		return err
	}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			_ = rows.Close()
			return err
		}
		switch kind {
		case queue.KindDownload:
			qd = n
		case queue.KindScan:
			qs = n
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	var wanted, downloaded int
	vrows, err := database.SQL.Query(`
		SELECT status, COUNT(*) FROM videos
		WHERE status IN ('wanted', 'downloaded')
		GROUP BY status
	`)
	if err != nil {
		return err
	}
	for vrows.Next() {
		var status string
		var n int
		if err := vrows.Scan(&status, &n); err != nil {
			_ = vrows.Close()
			return err
		}
		switch status {
		case "wanted":
			wanted = n
		case "downloaded":
			downloaded = n
		}
	}
	if err := vrows.Err(); err != nil {
		_ = vrows.Close()
		return err
	}
	if err := vrows.Close(); err != nil {
		return err
	}

	values := map[string]int{
		MetricQueueDownload:    qd,
		MetricQueueScan:        qs,
		MetricVideosWanted:     wanted,
		MetricVideosDownloaded: downloaded,
	}

	tx, err := database.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, metric := range minuteMetrics {
		v := values[metric]
		if writeFn != nil && !writeFn(metric, v) {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO stats_samples (sampled_at, metric, value)
			VALUES (?, ?, ?)
			ON CONFLICT(sampled_at, metric) DO UPDATE SET value = excluded.value
		`, ts, metric, v); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit stats sample: %w", err)
	}
	return nil
}

// SampleLibrarySize records global sum of kind=video size_bytes (change-only when via Sampler).
func (s *Sampler) SampleLibrarySize(at time.Time) error {
	return SampleLibrarySizeWithWriter(s.DB, at, s.shouldWrite)
}

// SampleLibrarySize always writes (tests).
func SampleLibrarySize(database *db.DB, at time.Time) error {
	return SampleLibrarySizeWithWriter(database, at, func(string, int) bool { return true })
}

// SampleLibrarySizeWithWriter records daily library size; writeFn gates unchanged values.
func SampleLibrarySizeWithWriter(database *db.DB, at time.Time, writeFn func(metric string, value int) bool) error {
	day := at.UTC().Truncate(24 * time.Hour)
	ts := day.Format(time.RFC3339)

	var total int64
	err := database.SQL.QueryRow(`
		SELECT COALESCE(SUM(size_bytes), 0) FROM files
		WHERE kind = 'video' AND size_bytes IS NOT NULL
	`).Scan(&total)
	if err != nil {
		return err
	}
	v := int(total)
	if writeFn != nil && !writeFn(MetricLibrarySizeBytes, v) {
		return nil
	}
	_, err = database.SQL.Exec(`
		INSERT INTO stats_samples (sampled_at, metric, value)
		VALUES (?, ?, ?)
		ON CONFLICT(sampled_at, metric) DO UPDATE SET value = excluded.value
	`, ts, MetricLibrarySizeBytes, v)
	return err
}

// ApplyRetention deletes samples outside the current stats_retention_days window.
func ApplyRetention(database *db.DB, now time.Time) (int64, error) {
	return Prune(database, now, retentionDays(database))
}

// Prune deletes samples per retention policy, keeping one baseline row per metric
// at or before the cutoff so forward-fill still has a starting value.
// retentionDays: -1 = forever (no-op), >0 = keep last N days (+ baselines).
func Prune(database *db.DB, now time.Time, retentionDays int) (int64, error) {
	if retentionDays < 0 {
		return 0, nil
	}
	if retentionDays == 0 {
		res, err := database.SQL.Exec(`DELETE FROM stats_samples`)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	cutoff := now.UTC().AddDate(0, 0, -retentionDays).Truncate(time.Minute).Format(time.RFC3339)

	tx, err := database.SQL.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	baselines := map[string]string{}
	brows, err := tx.Query(`
		SELECT metric, MAX(sampled_at) FROM stats_samples
		WHERE sampled_at <= ?
		GROUP BY metric
	`, cutoff)
	if err != nil {
		return 0, err
	}
	for brows.Next() {
		var metric, at string
		if err := brows.Scan(&metric, &at); err != nil {
			_ = brows.Close()
			return 0, err
		}
		baselines[metric] = at
	}
	if err := brows.Err(); err != nil {
		_ = brows.Close()
		return 0, err
	}
	if err := brows.Close(); err != nil {
		return 0, err
	}

	var deleted int64
	metrics := append(append([]string{}, minuteMetrics...), MetricLibrarySizeBytes)
	for _, metric := range metrics {
		base, ok := baselines[metric]
		var res interface{ RowsAffected() (int64, error) }
		var execErr error
		if ok {
			// Keep newest sample at/before cutoff as forward-fill baseline; drop older.
			res, execErr = tx.Exec(`
				DELETE FROM stats_samples
				WHERE metric = ? AND sampled_at < ? AND sampled_at != ?
			`, metric, cutoff, base)
		} else {
			res, execErr = tx.Exec(`
				DELETE FROM stats_samples
				WHERE metric = ? AND sampled_at < ?
			`, metric, cutoff)
		}
		if execErr != nil {
			return 0, execErr
		}
		n, _ := res.RowsAffected()
		deleted += n
	}
	res, err := tx.Exec(`
		DELETE FROM stats_samples
		WHERE sampled_at < ?
		  AND metric NOT IN (?, ?, ?, ?, ?)
	`, cutoff,
		MetricQueueDownload, MetricQueueScan,
		MetricVideosWanted, MetricVideosDownloaded,
		MetricLibrarySizeBytes)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	deleted += n

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

// SeriesPoint is one chart series.
type SeriesPoint struct {
	Name   string `json:"name"`
	Metric string `json:"metric"`
	Stack  string `json:"stack,omitempty"`
	Data   []int  `json:"data"`
}

// ChartPayload is JSON for the Stats page charts.
type ChartPayload struct {
	Times            []string      `json:"times"`
	Queue            []SeriesPoint `json:"queue"`
	VideoStatus      []SeriesPoint `json:"video_status"`
	StorageTimes     []string      `json:"storage_times"`
	Storage          []SeriesPoint `json:"storage"`
	RetentionDays    int           `json:"retention_days"`
	SampleCount      int           `json:"sample_count"`
	StorageSampleCnt int           `json:"storage_sample_count"`
}

// LoadChart builds forward-filled series for queue, video status, and storage charts.
// Extends the series to the current minute/day so change-only samples still reach "now".
func LoadChart(database *db.DB) (ChartPayload, error) {
	return LoadChartAt(database, time.Now().UTC())
}

// LoadChartAt is LoadChart with an explicit "now" (tests and callers that need a fixed tip).
func LoadChartAt(database *db.DB, now time.Time) (ChartPayload, error) {
	now = now.UTC()
	out := ChartPayload{
		RetentionDays: retentionDays(database),
	}

	rows, err := database.SQL.Query(`
		SELECT sampled_at, metric, value
		FROM stats_samples
		ORDER BY sampled_at ASC, metric ASC
	`)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	byTime := map[string]map[string]int{}
	var allTimes []string
	timeSeen := map[string]struct{}{}

	for rows.Next() {
		var at, metric string
		var value int
		if err := rows.Scan(&at, &metric, &value); err != nil {
			return out, err
		}
		if _, ok := timeSeen[at]; !ok {
			timeSeen[at] = struct{}{}
			allTimes = append(allTimes, at)
		}
		if byTime[at] == nil {
			byTime[at] = map[string]int{}
		}
		byTime[at][metric] = value
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	minuteTimes := timesForMetrics(allTimes, byTime, minuteMetrics)
	minuteTimes = appendNowTip(minuteTimes, now.Truncate(time.Minute).Format(time.RFC3339))
	out.Times = minuteTimes
	out.SampleCount = len(minuteTimes)

	out.Queue = forwardFillSeries(minuteTimes, byTime, []struct{ metric, name, stack string }{
		{MetricQueueDownload, "download", "queue"},
		{MetricQueueScan, "scan", "queue"},
	})
	out.VideoStatus = forwardFillSeries(minuteTimes, byTime, []struct{ metric, name, stack string }{
		{MetricVideosWanted, "wanted", "video_status"},
		{MetricVideosDownloaded, "downloaded", "video_status"},
	})

	storageCutoff := now.AddDate(0, 0, -StorageChartMaxDays).Truncate(24 * time.Hour).Format(time.RFC3339)
	storageTimes := timesForMetrics(allTimes, byTime, []string{MetricLibrarySizeBytes})
	filtered := storageTimes[:0]
	for _, t := range storageTimes {
		if t >= storageCutoff {
			filtered = append(filtered, t)
		}
	}
	storageTimes = filtered
	storageTimes = appendNowTip(storageTimes, now.Truncate(24*time.Hour).Format(time.RFC3339))
	out.StorageTimes = storageTimes
	out.StorageSampleCnt = len(storageTimes)
	out.Storage = forwardFillSeries(storageTimes, byTime, []struct{ metric, name, stack string }{
		{MetricLibrarySizeBytes, "library size", "storage"},
	})

	return out, nil
}

// appendNowTip adds a synthetic tip timestamp when stored samples stop before now.
// Empty series stay empty (nothing to forward-fill). Does not write to the DB.
func appendNowTip(times []string, nowTS string) []string {
	if len(times) == 0 || nowTS == "" {
		return times
	}
	if times[len(times)-1] >= nowTS {
		return times
	}
	return append(times, nowTS)
}

func timesForMetrics(allTimes []string, byTime map[string]map[string]int, metrics []string) []string {
	want := map[string]struct{}{}
	for _, m := range metrics {
		want[m] = struct{}{}
	}
	var out []string
	for _, t := range allTimes {
		m := byTime[t]
		if m == nil {
			continue
		}
		for metric := range m {
			if _, ok := want[metric]; ok {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

func forwardFillSeries(times []string, byTime map[string]map[string]int, specs []struct{ metric, name, stack string }) []SeriesPoint {
	out := make([]SeriesPoint, 0, len(specs))
	for _, spec := range specs {
		sp := SeriesPoint{
			Name:   spec.name,
			Metric: spec.metric,
			Stack:  spec.stack,
			Data:   make([]int, len(times)),
		}
		last := 0
		have := false
		if len(times) > 0 {
			var bestAt string
			bestVal := 0
			for t, m := range byTime {
				if t >= times[0] {
					continue
				}
				v, ok := m[spec.metric]
				if !ok {
					continue
				}
				if bestAt == "" || t > bestAt {
					bestAt = t
					bestVal = v
				}
			}
			if bestAt != "" {
				last = bestVal
				have = true
			}
		}
		for i, t := range times {
			if v, ok := byTime[t][spec.metric]; ok {
				last = v
				have = true
			}
			if have {
				sp.Data[i] = last
			}
		}
		out = append(out, sp)
	}
	return out
}

// Library size group modes for the live pie chart.
const (
	LibrarySizeGroupRoot   = "root"
	LibrarySizeGroupSeries = "series"
)

// LibrarySizeSlice is one pie segment.
type LibrarySizeSlice struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
	Bytes int64  `json:"bytes"`
}

// LibrarySizePayload is live (non-sampled) library media size for the Stats pie.
type LibrarySizePayload struct {
	TotalBytes int64              `json:"total_bytes"`
	Group      string             `json:"group"`
	Slices     []LibrarySizeSlice `json:"slices"`
}

// LoadLibrarySize sums kind=video size_bytes grouped by root folder or series.
func LoadLibrarySize(database *db.DB, group string) (LibrarySizePayload, error) {
	group = strings.TrimSpace(strings.ToLower(group))
	if group != LibrarySizeGroupSeries {
		group = LibrarySizeGroupRoot
	}
	out := LibrarySizePayload{Group: group, Slices: []LibrarySizeSlice{}}

	rows, err := database.SQL.Query(`
		SELECT s.id, s.title, r.id, r.name, COALESCE(f.size_bytes, 0)
		FROM files f
		JOIN videos v ON v.id = f.video_id
		JOIN series s ON s.id = v.series_id
		JOIN root_folders r ON r.id = s.root_id
		WHERE f.kind = 'video'
	`)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	type agg struct {
		id    int64
		label string
		bytes int64
	}
	byID := map[int64]*agg{}
	for rows.Next() {
		var seriesID, rootID int64
		var seriesTitle, rootName string
		var size int64
		if err := rows.Scan(&seriesID, &seriesTitle, &rootID, &rootName, &size); err != nil {
			return out, err
		}
		id, label := rootID, rootName
		if group == LibrarySizeGroupSeries {
			id, label = seriesID, seriesTitle
		}
		a := byID[id]
		if a == nil {
			a = &agg{id: id, label: label}
			byID[id] = a
		}
		a.bytes += size
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	out.Slices = make([]LibrarySizeSlice, 0, len(byID))
	for _, a := range byID {
		out.Slices = append(out.Slices, LibrarySizeSlice{ID: a.id, Label: a.label, Bytes: a.bytes})
		out.TotalBytes += a.bytes
	}
	sort.Slice(out.Slices, func(i, j int) bool {
		if out.Slices[i].Bytes != out.Slices[j].Bytes {
			return out.Slices[i].Bytes > out.Slices[j].Bytes
		}
		return out.Slices[i].Label < out.Slices[j].Label
	})
	return out, nil
}
