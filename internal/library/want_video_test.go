package library_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestWantVideoStatusMatrix(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Want", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@want"})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		from    string
		wantOK  bool
		wantErr error
	}{
		{"ignored", true, nil},
		{"deleted", true, nil},
		{"missing", true, nil},
		{"downloaded_integrity_failed", true, nil},
		{"downloaded", false, library.ErrInvalid},
		{"wanted", false, library.ErrInvalid},
	}
	for i, tc := range cases {
		t.Run(tc.from, func(t *testing.T) {
			res, err := s.UpsertListed(ser.ID, library.ListedVideo{
				RemoteID:   fmt.Sprintf("w%d", i),
				Title:      "Ep",
				WebpageURL: fmt.Sprintf("https://www.example.com/watch?v=w%d", i),
				SourceID:   src.ID,
			}, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, tc.from, res.VideoID); err != nil {
				t.Fatal(err)
			}
			v, err := s.WantVideo(res.VideoID)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("WantVideo: %v", err)
				}
				if v.Status != "wanted" {
					t.Fatalf("status=%q", v.Status)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v want %v", err, tc.wantErr)
			}
		})
	}
}

func TestWantVideosBulkSkipCounts(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "BulkWant", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@bw"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i, st := range []string{"ignored", "downloaded", "missing"} {
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID:   fmt.Sprintf("bw%d", i),
			Title:      "Ep",
			WebpageURL: fmt.Sprintf("https://www.example.com/watch?v=bw%d", i),
			SourceID:   src.ID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, st, res.VideoID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.VideoID)
	}
	ids = append(ids, 999999) // not found
	updated, skipped, err := s.WantVideosBulk(ids)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 2 || skipped != 2 {
		t.Fatalf("updated=%d skipped=%d want 2/2", updated, skipped)
	}
}

func TestIgnoreVideosBulkSkipCounts(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "BulkIgn", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@bi"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i, st := range []string{"wanted", "downloaded", "ignored"} {
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID:   fmt.Sprintf("bi%d", i),
			Title:      "Ep",
			WebpageURL: fmt.Sprintf("https://www.example.com/watch?v=bi%d", i),
			SourceID:   src.ID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, st, res.VideoID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.VideoID)
	}
	updated, skipped, err := s.IgnoreVideosBulk(ids)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 2 || skipped != 1 {
		t.Fatalf("updated=%d skipped=%d want 2/1", updated, skipped)
	}
}
