package library_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestUpdateVideoNotes(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "Notes Video",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'vid-notes', 'One', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	vidID, _ := res.LastInsertId()

	if err := s.UpdateVideoNotes(vidID, "hello notes"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetVideo(vidID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Notes != "hello notes" {
		t.Fatalf("notes = %q", got.Notes)
	}

	if err := s.UpdateVideoNotes(vidID, ""); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetVideo(vidID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Notes != "" {
		t.Fatalf("cleared notes = %q", got.Notes)
	}

	tooLong := strings.Repeat("a", library.NotesMaxBytes+1)
	if err := s.UpdateVideoNotes(vidID, tooLong); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("over-max err = %v", err)
	}
	if err := s.UpdateVideoNotes(999999, "x"); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestUpdateSeriesNotes(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "Notes Series",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateSeriesNotes(ser.ID, "series note"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSeries(ser.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Notes != "series note" {
		t.Fatalf("notes = %q", got.Notes)
	}

	tooLong := strings.Repeat("b", library.NotesMaxBytes+1)
	if err := s.UpdateSeriesNotes(ser.ID, tooLong); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("over-max err = %v", err)
	}
	if err := s.UpdateSeriesNotes(999999, "x"); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}
