package stats_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/stats"
)

func openStatsDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func seedSeries(t *testing.T, d *db.DB) {
	t.Helper()
	_, err := d.SQL.Exec(`
		INSERT INTO root_folders (id, name, path) VALUES (1, 'lib', '/tmp/lib');
		INSERT INTO quality_profiles (id, name, format_selector) VALUES (1, 'best', 'bv*+ba/b');
		INSERT INTO series (id, title, root_id, quality_profile_id, monitored, delivery_mode, added_at)
		VALUES (1, 'Show', 1, 1, 1, 'video', datetime('now'))
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSampleAndLoadChart(t *testing.T) {
	d := openStatsDB(t)
	seedSeries(t, d)
	_, err := d.SQL.Exec(`
		INSERT INTO videos (id, series_id, remote_id, title, status) VALUES
			(1, 1, 'a', 'A', 'wanted'),
			(2, 1, 'b', 'B', 'downloaded'),
			(3, 1, 'c', 'C', 'ignored');
		INSERT INTO tasks (kind, domain, status, message, created_at)
		VALUES (?, 'example.com', ?, 'x', datetime('now'))
	`, queue.KindDownload, queue.StatusPending)
	if err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	if err := stats.Sample(d, at); err != nil {
		t.Fatal(err)
	}
	chart, err := stats.LoadChartAt(d, at)
	if err != nil {
		t.Fatal(err)
	}
	if chart.SampleCount != 1 {
		t.Fatalf("samples=%d", chart.SampleCount)
	}
	wantMetrics := map[string]int{
		stats.MetricVideosWanted:     1,
		stats.MetricVideosDownloaded: 1,
		stats.MetricQueueDownload:    1,
		stats.MetricQueueScan:        0,
	}
	for _, sp := range chart.VideoStatus {
		if len(sp.Data) != 1 {
			t.Fatalf("%s data len %d", sp.Metric, len(sp.Data))
		}
		if want, ok := wantMetrics[sp.Metric]; ok && sp.Data[0] != want {
			t.Fatalf("%s=%d want %d", sp.Metric, sp.Data[0], want)
		}
	}
	for _, sp := range chart.Queue {
		if want, ok := wantMetrics[sp.Metric]; ok && sp.Data[0] != want {
			t.Fatalf("%s=%d want %d", sp.Metric, sp.Data[0], want)
		}
	}
}

func TestSamplerChangeOnly(t *testing.T) {
	d := openStatsDB(t)
	seedSeries(t, d)
	_, err := d.SQL.Exec(`INSERT INTO videos (id, series_id, remote_id, title, status) VALUES (1, 1, 'a', 'A', 'wanted')`)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	s := &stats.Sampler{DB: d}
	if err := s.Sample(at); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM stats_samples`).Scan(&n)
	if n != 4 {
		t.Fatalf("first sample rows=%d want 4", n)
	}
	if err := s.Sample(at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM stats_samples`).Scan(&n)
	if n != 4 {
		t.Fatalf("unchanged second sample rows=%d want 4", n)
	}
	_, err = d.SQL.Exec(`INSERT INTO videos (id, series_id, remote_id, title, status) VALUES (2, 1, 'b', 'B', 'wanted')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Sample(at.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM stats_samples`).Scan(&n)
	if n != 5 {
		t.Fatalf("after wanted bump rows=%d want 5 (one new videos_wanted)", n)
	}
}

func TestForwardFill(t *testing.T) {
	d := openStatsDB(t)
	t1 := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Minute)
	_, err := d.SQL.Exec(`
		INSERT INTO stats_samples (sampled_at, metric, value) VALUES
		(?, ?, 3),
		(?, ?, 5)
	`, t1.Format(time.RFC3339), stats.MetricVideosWanted,
		t2.Format(time.RFC3339), stats.MetricVideosWanted)
	if err != nil {
		t.Fatal(err)
	}
	// Midpoint change for downloaded only
	mid := t1.Add(time.Minute)
	_, err = d.SQL.Exec(`
		INSERT INTO stats_samples (sampled_at, metric, value) VALUES (?, ?, 1)
	`, mid.Format(time.RFC3339), stats.MetricVideosDownloaded)
	if err != nil {
		t.Fatal(err)
	}
	chart, err := stats.LoadChartAt(d, t2)
	if err != nil {
		t.Fatal(err)
	}
	if len(chart.Times) != 3 {
		t.Fatalf("times=%v", chart.Times)
	}
	var wanted []int
	for _, sp := range chart.VideoStatus {
		if sp.Metric == stats.MetricVideosWanted {
			wanted = sp.Data
		}
	}
	if len(wanted) != 3 || wanted[0] != 3 || wanted[1] != 3 || wanted[2] != 5 {
		t.Fatalf("wanted forward-fill=%v", wanted)
	}
}

func TestLoadChartAppendsNowTip(t *testing.T) {
	d := openStatsDB(t)
	t1 := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	now := t1.Add(3 * time.Hour)
	_, err := d.SQL.Exec(`
		INSERT INTO stats_samples (sampled_at, metric, value) VALUES (?, ?, 4)
	`, t1.Format(time.RFC3339), stats.MetricVideosWanted)
	if err != nil {
		t.Fatal(err)
	}
	chart, err := stats.LoadChartAt(d, now)
	if err != nil {
		t.Fatal(err)
	}
	wantTip := now.Truncate(time.Minute).Format(time.RFC3339)
	if len(chart.Times) != 2 || chart.Times[0] != t1.Format(time.RFC3339) || chart.Times[1] != wantTip {
		t.Fatalf("times=%v want [%s %s]", chart.Times, t1.Format(time.RFC3339), wantTip)
	}
	var wanted []int
	for _, sp := range chart.VideoStatus {
		if sp.Metric == stats.MetricVideosWanted {
			wanted = sp.Data
		}
	}
	if len(wanted) != 2 || wanted[0] != 4 || wanted[1] != 4 {
		t.Fatalf("wanted tip forward-fill=%v", wanted)
	}
	// Same-minute tip: no duplicate.
	chart, err = stats.LoadChartAt(d, t1.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(chart.Times) != 1 {
		t.Fatalf("same-minute times=%v", chart.Times)
	}
}

func TestPruneKeepsBaseline(t *testing.T) {
	d := openStatsDB(t)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	older := now.AddDate(0, 0, -100)
	baseline := now.AddDate(0, 0, -95) // newest at/before 90d cutoff
	inWindow := now.AddDate(0, 0, -50)
	recent := now.Add(-time.Hour)
	for i, at := range []time.Time{older, baseline, inWindow, recent} {
		_, err := d.SQL.Exec(`
			INSERT INTO stats_samples (sampled_at, metric, value) VALUES (?, ?, ?)
		`, at.Format(time.RFC3339), stats.MetricVideosWanted, i+1)
		if err != nil {
			t.Fatal(err)
		}
	}
	n, err := stats.Prune(d, now, 90)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 delete (older than baseline), got %d", n)
	}
	var oldest string
	_ = d.SQL.QueryRow(`SELECT MIN(sampled_at) FROM stats_samples WHERE metric = ?`, stats.MetricVideosWanted).Scan(&oldest)
	if oldest != baseline.Format(time.RFC3339) {
		t.Fatalf("oldest=%s want baseline %s", oldest, baseline.Format(time.RFC3339))
	}
	var cnt int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM stats_samples WHERE metric = ?`, stats.MetricVideosWanted).Scan(&cnt)
	if cnt != 3 {
		t.Fatalf("rows=%d want 3 (baseline + 2 in window)", cnt)
	}
}

func TestSampleLibrarySize(t *testing.T) {
	d := openStatsDB(t)
	seedSeries(t, d)
	_, err := d.SQL.Exec(`
		INSERT INTO videos (id, series_id, remote_id, title, status) VALUES (1, 1, 'a', 'A', 'downloaded');
		INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES
			(1, '/tmp/a.mkv', 'video', datetime('now'), 1500),
			(1, '/tmp/a.nfo', 'nfo', datetime('now'), NULL)
	`)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 24, 15, 30, 0, 0, time.UTC)
	if err := stats.SampleLibrarySize(d, at); err != nil {
		t.Fatal(err)
	}
	var ts string
	var v int
	err = d.SQL.QueryRow(`SELECT sampled_at, value FROM stats_samples WHERE metric = ?`, stats.MetricLibrarySizeBytes).Scan(&ts, &v)
	if err != nil {
		t.Fatal(err)
	}
	day := at.UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
	if ts != day {
		t.Fatalf("sampled_at=%s want %s", ts, day)
	}
	if v != 1500 {
		t.Fatalf("value=%d", v)
	}
	chart, err := stats.LoadChartAt(d, at)
	if err != nil {
		t.Fatal(err)
	}
	if chart.StorageSampleCnt != 1 || len(chart.Storage) != 1 || chart.Storage[0].Data[0] != 1500 {
		t.Fatalf("storage=%+v", chart.Storage)
	}
	nextDay := at.Add(24 * time.Hour)
	chart, err = stats.LoadChartAt(d, nextDay)
	if err != nil {
		t.Fatal(err)
	}
	if chart.StorageSampleCnt != 2 || chart.Storage[0].Data[1] != 1500 {
		t.Fatalf("storage tip=%+v times=%v", chart.Storage, chart.StorageTimes)
	}
}

func TestLoadLibrarySize(t *testing.T) {
	d := openStatsDB(t)
	_, err := d.SQL.Exec(`
		INSERT INTO root_folders (id, name, path) VALUES (1, 'A', '/a'), (2, 'B', '/b');
		INSERT INTO quality_profiles (id, name, format_selector) VALUES (1, 'best', 'bv*+ba/b');
		INSERT INTO series (id, title, root_id, quality_profile_id, monitored, delivery_mode, added_at) VALUES
			(1, 'S1', 1, 1, 1, 'video', datetime('now')),
			(2, 'S2', 1, 1, 1, 'video', datetime('now')),
			(3, 'S3', 2, 1, 1, 'video', datetime('now'));
		INSERT INTO videos (id, series_id, remote_id, title, status) VALUES
			(1, 1, 'a', 'A', 'downloaded'),
			(2, 2, 'b', 'B', 'downloaded'),
			(3, 3, 'c', 'C', 'downloaded');
		INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES
			(1, '/a/1.mkv', 'video', datetime('now'), 1000),
			(1, '/a/1.nfo', 'nfo', datetime('now'), NULL),
			(2, '/a/2.mkv', 'video', datetime('now'), 500),
			(3, '/b/3.mkv', 'video', datetime('now'), 2500)
	`)
	if err != nil {
		t.Fatal(err)
	}
	byRoot, err := stats.LoadLibrarySize(d, "root")
	if err != nil {
		t.Fatal(err)
	}
	if byRoot.TotalBytes != 4000 {
		t.Fatalf("total=%d", byRoot.TotalBytes)
	}
	bySeries, err := stats.LoadLibrarySize(d, "series")
	if err != nil {
		t.Fatal(err)
	}
	if len(bySeries.Slices) != 3 {
		t.Fatalf("slices=%d", len(bySeries.Slices))
	}
}

func TestApplyRetentionForever(t *testing.T) {
	d := openStatsDB(t)
	if err := settings.Set(d, settings.KeyStatsRetentionDays, settings.StatsRetentionForever); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = d.SQL.Exec(`INSERT INTO stats_samples (sampled_at, metric, value) VALUES (?, ?, 1)`,
		old.Format(time.RFC3339), stats.MetricVideosWanted)
	n, err := stats.ApplyRetention(d, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("forever prune deleted %d", n)
	}
}
