package library

import (
	"fmt"
)

// NotesMaxBytes is the server-side cap for series/video operator notes (64 KiB).
const NotesMaxBytes = 64 << 10

func validateNotes(notes string) error {
	if len(notes) > NotesMaxBytes {
		return fmt.Errorf("%w: notes too long (max %d bytes)", ErrInvalid, NotesMaxBytes)
	}
	return nil
}

// UpdateVideoNotes sets operator-only notes on a video (not NFO / packed files).
func (s *Store) UpdateVideoNotes(id int64, notes string) error {
	if id <= 0 {
		return fmt.Errorf("%w: video id", ErrInvalid)
	}
	if err := validateNotes(notes); err != nil {
		return err
	}
	res, err := s.DB.SQL.Exec(`UPDATE videos SET notes = ? WHERE id = ?`, notes, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateSeriesNotes sets operator-only notes on a series (not NFO / packed files).
func (s *Store) UpdateSeriesNotes(id int64, notes string) error {
	if id <= 0 {
		return fmt.Errorf("%w: series id", ErrInvalid)
	}
	if err := validateNotes(notes); err != nil {
		return err
	}
	res, err := s.DB.SQL.Exec(`UPDATE series SET notes = ? WHERE id = ?`, notes, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
