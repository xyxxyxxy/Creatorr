package library_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestSeriesWarnLevels(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	q := queue.NewStore(d)
	s := library.NewStore(d, q)
	root, err := s.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := s.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "W", RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/c/w", Kind: library.SourceKindFeed,
	})
	if err != nil {
		t.Fatal(err)
	}
	// AddSource may enqueue a full scan; clear it so history is stalled.
	if _, err := d.SQL.Exec(`DELETE FROM tasks WHERE kind = 'scan'`); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetFullScan(src.ID); err != nil {
		t.Fatal(err)
	}
	// Default feed cron is non-empty; scheduled incomplete does not escalate.
	lvl, err := s.SeriesWarnLevel(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lvl != library.SeriesWarnNone {
		t.Fatalf("scheduled incomplete should not escalate, got %q", lvl)
	}
	if _, err := d.SQL.Exec(`UPDATE sources SET scan_cron = '' WHERE id = ?`, src.ID); err != nil {
		t.Fatal(err)
	}
	lvl, err = s.SeriesWarnLevel(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lvl != library.SeriesWarnIncomplete {
		t.Fatalf("want incomplete, got %q", lvl)
	}
	if err := s.MarkFullScanDone(src.ID); err != nil {
		t.Fatal(err)
	}
	lvl, err = s.SeriesWarnLevel(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lvl != library.SeriesWarnNone {
		t.Fatalf("want none after indexed, got %q", lvl)
	}
	// Video error overwrites to error level.
	if _, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, source_id, remote_id, title, status)
		VALUES (?, ?, 'e1', 'E', 'wanted_download_error')
	`, ser.ID, src.ID); err != nil {
		t.Fatal(err)
	}
	lvl, err = s.SeriesWarnLevel(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lvl != library.SeriesWarnError {
		t.Fatalf("want error, got %q", lvl)
	}
	flags, err := s.SeriesVideoErrorFlagsMap([]int64{ser.ID})
	if err != nil {
		t.Fatal(err)
	}
	f := flags[ser.ID]
	if !f.HasDownloadError || f.HasSourceError || f.DownloadErrorCount != 1 {
		t.Fatalf("flags=%+v", f)
	}
	if _, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, source_id, remote_id, title, status)
		VALUES (?, ?, 'e2', 'E2', 'wanted_download_error')
	`, ser.ID, src.ID); err != nil {
		t.Fatal(err)
	}
	flags, err = s.SeriesVideoErrorFlagsMap([]int64{ser.ID})
	if err != nil {
		t.Fatal(err)
	}
	f = flags[ser.ID]
	if !f.HasDownloadError || f.DownloadErrorCount != 2 {
		t.Fatalf("want 2 download errors, flags=%+v", f)
	}
	if _, err := d.SQL.Exec(`UPDATE videos SET status = 'wanted_source_error' WHERE remote_id = 'e1'`); err != nil {
		t.Fatal(err)
	}
	flags, err = s.SeriesVideoErrorFlagsMap([]int64{ser.ID})
	if err != nil {
		t.Fatal(err)
	}
	f = flags[ser.ID]
	if !f.HasSourceError || !f.HasDownloadError || f.SourceErrorCount != 1 || f.DownloadErrorCount != 1 {
		t.Fatalf("after source error flags=%+v", f)
	}
	n, err := s.CountSeriesWithError()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("CountSeriesWithError=%d want 1", n)
	}
}
