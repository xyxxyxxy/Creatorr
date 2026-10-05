package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestListFilesFilteredDBOnlyScopes(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "FilesList", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root.Path, "FilesList")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode)
		VALUES (?, 'fl1', 'Ep', 'downloaded', 2026, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'fl1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "ep.mkv")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterFileKind(videoID, media, "video"); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(dir, "poster.jpg")
	if err := os.WriteFile(poster, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterSeriesMetaFile(ser.ID, poster, library.ArtPoster); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListFilesFiltered(library.FileListFilter{SeriesID: ser.ID}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 2 {
		t.Fatalf("series scope got %d want >= 2", len(all))
	}
	vidOnly, err := s.ListFilesFiltered(library.FileListFilter{VideoID: videoID}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(vidOnly) != 1 || vidOnly[0].Kind != "video" {
		t.Fatalf("video scope=%+v", vidOnly)
	}
	art, err := s.ListFilesFiltered(library.FileListFilter{SeriesID: ser.ID, Kinds: []string{library.ArtPoster}}, 50, 0)
	if err != nil || len(art) != 1 {
		t.Fatalf("poster filter: %v %#v", err, art)
	}
	if !art[0].IsSeriesMeta() {
		t.Fatal("poster should be series-meta")
	}
}

func TestListFilesFilteredStatusMatchesIntegrityColumn(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "StatusFilt", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root.Path, "StatusFilt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode)
		VALUES (?, 'sf1', 'Ep', 'downloaded', 2026, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'sf1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "ep.mkv")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterFileKind(videoID, media, "video"); err != nil {
		t.Fatal(err)
	}
	nfo := filepath.Join(dir, "ep.nfo")
	if err := os.WriteFile(nfo, []byte("<episodedetails/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterFileKind(videoID, nfo, "nfo"); err != nil {
		t.Fatal(err)
	}
	thumb := filepath.Join(dir, "ep-thumb.jpg")
	if err := os.WriteFile(thumb, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterFileKind(videoID, thumb, "thumb"); err != nil {
		t.Fatal(err)
	}
	var videoFileID, thumbID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM files WHERE video_id = ? AND kind = 'video'`, videoID).Scan(&videoFileID); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.SQL.QueryRow(`SELECT id FROM files WHERE video_id = ? AND kind = 'thumb'`, videoID).Scan(&thumbID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFileHashOK(videoFileID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFileHashAttempted(thumbID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE files SET size_bytes = -1 WHERE video_id = ? AND kind = 'thumb'`, videoID); err != nil {
		t.Fatal(err)
	}

	mustCount := func(status string, want int) {
		t.Helper()
		n, err := s.CountFilesFiltered(library.FileListFilter{VideoID: videoID, Statuses: []string{status}})
		if err != nil || n != want {
			t.Fatalf("status=%s count=%d err=%v want %d", status, n, err, want)
		}
	}
	mustCount(library.FileStatusOK, 1)        // video
	mustCount(library.FileStatusFailed, 1)    // thumb (failed stamps)
	mustCount(library.FileStatusNA, 1)        // nfo
	mustCount(library.FileStatusUnchecked, 0) // none present without stamps
	mustCount(library.FileStatusInactive, 0)  // failed missing thumb is Failed, not Inactive

	// Present unchecked sidecar.
	sub := filepath.Join(dir, "ep.en.srt")
	if err := os.WriteFile(sub, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterFileKind(videoID, sub, "sub"); err != nil {
		t.Fatal(err)
	}
	mustCount(library.FileStatusUnchecked, 1)

	// Missing without fail stamps → Inactive.
	if _, err := s.DB.SQL.Exec(`
		UPDATE files SET size_bytes = -1,
		  content_hash_checked_at = NULL, content_hash_ok_at = NULL
		WHERE video_id = ? AND kind = 'sub'
	`, videoID); err != nil {
		t.Fatal(err)
	}
	mustCount(library.FileStatusInactive, 1)
	mustCount(library.FileStatusUnchecked, 0)
}

func TestSeriesHasFailedIntegrityFileHardRollup(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Rollup", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root.Path, "Rollup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(dir, "poster.jpg")
	if err := os.WriteFile(poster, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterSeriesMetaFile(ser.ID, poster, library.ArtPoster); err != nil {
		t.Fatal(err)
	}
	var fileID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM files WHERE series_id = ? AND kind = ?`, ser.ID, library.ArtPoster).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	ok, err := s.SeriesHasFailedIntegrityFile(ser.ID)
	if err != nil || ok {
		t.Fatalf("before fail: ok=%v err=%v", ok, err)
	}
	if err := s.MarkFileHashAttempted(fileID); err != nil {
		t.Fatal(err)
	}
	ok, err = s.SeriesHasFailedIntegrityFile(ser.ID)
	if err != nil || !ok {
		t.Fatalf("after fail: ok=%v err=%v", ok, err)
	}
	flags, err := s.SeriesVideoErrorFlagsMap([]int64{ser.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !flags[ser.ID].HasVerifyFailed {
		t.Fatal("series health should raise integrity failed from series-meta")
	}
}
