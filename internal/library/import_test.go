package library_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestScanImportMatchesRemoteID(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "Demo Show",
		SourceURL:        "https://www.example.com/@demo",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'abc123', 'Cool Episode', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'abc123'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}

	media := filepath.Join(inbox, "Cool Episode [abc123].mkv")
	if err := os.WriteFile(media, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.ScanImportInbox()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("candidates=%d", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.Source != library.ImportSourceInbox {
		t.Fatalf("source=%q", c.Source)
	}
	if c.MatchType != "id" || c.SuggestedVideoID == nil || *c.SuggestedVideoID != videoID {
		t.Fatalf("match=%+v want video %d", c, videoID)
	}
	taskID, err := s.EnqueueImport(c.Path, *c.SuggestedVideoID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if taskID <= 0 {
		t.Fatal("task id")
	}
	_, err = s.EnqueueImport(c.Path, *c.SuggestedVideoID, false, false)
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestScanImportPrefersBracketIDOverSidecars(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "Demo Show",
		SourceURL:        "https://www.example.com/@demo",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'bracket1', 'Bracket Hit', 'wanted'),
		       (?, 'sidecar1', 'Sidecar Hit', 'wanted')
	`, ser.ID, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var bracketVID, sidecarVID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'bracket1'`).Scan(&bracketVID); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'sidecar1'`).Scan(&sidecarVID); err != nil {
		t.Fatal(err)
	}

	media := filepath.Join(inbox, "Ep [bracket1].mkv")
	if err := os.WriteFile(media, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(inbox, "Ep [bracket1].info.json")
	if err := os.WriteFile(info, []byte(`{"id":"sidecar1","title":"Sidecar Hit"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := filepath.Join(inbox, "Ep [bracket1].nfo")
	if err := os.WriteFile(nfo, []byte(`<episodedetails><uniqueid type="yt-dlp">sidecar1</uniqueid></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.ScanImportInbox()
	if err != nil {
		t.Fatal(err)
	}
	var c *library.ImportCandidate
	for i := range res.Candidates {
		if res.Candidates[i].Path == media {
			c = &res.Candidates[i]
			break
		}
	}
	if c == nil {
		t.Fatal("media candidate missing")
	}
	if c.MatchType != "id" || c.SuggestedVideoID == nil || *c.SuggestedVideoID != bracketVID {
		t.Fatalf("match=%+v want bracket video %d (not sidecar %d)", c, bracketVID, sidecarVID)
	}
	if len(c.IDs) < 2 || c.IDs[0].RemoteID != "bracket1" {
		t.Fatalf("IDs order=%+v want bracket1 first", c.IDs)
	}
}

func TestEnqueueImportCreateUnmatched(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Unmatched Show", SourceURL: "https://example.com/u", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(inbox, "Brand New Ep [zz99].mkv")
	if err := os.WriteFile(media, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(inbox, "Brand New Ep [zz99].info.json")
	if err := os.WriteFile(info, []byte(`{"id":"zz99","title":"Brand New Ep","webpage_url":"https://example.com/v/zz99","upload_date":"20240115"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.ScanImportInbox()
	if err != nil {
		t.Fatal(err)
	}
	var c *library.ImportCandidate
	for i := range res.Candidates {
		if res.Candidates[i].Role == library.ImportRoleVideo {
			c = &res.Candidates[i]
			break
		}
	}
	if c == nil {
		t.Fatalf("no video candidate in %+v", res.Candidates)
	}
	if c.SuggestedVideoID != nil {
		t.Fatal("expected no video match")
	}
	if c.SuggestedTitle == "" || c.SuggestedRemoteID != "zz99" {
		t.Fatalf("meta=%+v", c)
	}
	if c.SuggestedRemoteIDGenerated {
		t.Fatal("expected remote id from file, not generated")
	}
	if c.SuggestedUploadDateFromMtime || !strings.HasPrefix(c.SuggestedUploadDate, "2024-01-15") {
		t.Fatalf("upload=%q fromMtime=%v", c.SuggestedUploadDate, c.SuggestedUploadDateFromMtime)
	}

	taskID, videoID, err := s.EnqueueImportCreate(c.Path, library.CreateImportVideoParams{
		SeriesID: ser.ID,
		Title:    "Brand New Ep",
	})
	if err != nil {
		t.Fatal(err)
	}
	if taskID <= 0 || videoID <= 0 {
		t.Fatalf("task=%d video=%d", taskID, videoID)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.RemoteID != "zz99" || v.Title != "Brand New Ep" || v.Status != "wanted" {
		t.Fatalf("%+v", v)
	}
	if !v.UploadDate.Valid || !strings.HasPrefix(v.UploadDate.String, "2024-01-15") {
		t.Fatalf("upload_date=%v want 2024-01-15…", v.UploadDate)
	}
	if !v.SourceURL.Valid || v.SourceURL.String != "https://example.com/v/zz99" {
		t.Fatalf("source_url=%v", v.SourceURL)
	}
}

func TestScanImportSuggestsUploadDateFromMtime(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	media := filepath.Join(inbox, "plain-ep.mkv")
	if err := os.WriteFile(media, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantDay := time.Now().UTC().Format("2006-01-02")
	res, err := s.ScanImportInbox()
	if err != nil {
		t.Fatal(err)
	}
	var c *library.ImportCandidate
	for i := range res.Candidates {
		if res.Candidates[i].Role == library.ImportRoleVideo {
			c = &res.Candidates[i]
			break
		}
	}
	if c == nil {
		t.Fatal("no video candidate")
	}
	if !c.SuggestedUploadDateFromMtime || !strings.HasPrefix(c.SuggestedUploadDate, wantDay) {
		t.Fatalf("upload=%q fromMtime=%v want day %s", c.SuggestedUploadDate, c.SuggestedUploadDateFromMtime, wantDay)
	}
}

func TestScanImportSuggestsGeneratedRemoteID(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	media := filepath.Join(inbox, "plain-ep.mkv")
	if err := os.WriteFile(media, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := s.ScanImportInbox()
	if err != nil {
		t.Fatal(err)
	}
	var c *library.ImportCandidate
	for i := range res.Candidates {
		if res.Candidates[i].Role == library.ImportRoleVideo {
			c = &res.Candidates[i]
			break
		}
	}
	if c == nil {
		t.Fatal("no video candidate")
	}
	if !c.SuggestedRemoteIDGenerated || !strings.HasPrefix(c.SuggestedRemoteID, "import-") {
		t.Fatalf("want generated import-* remote, got id=%q generated=%v", c.SuggestedRemoteID, c.SuggestedRemoteIDGenerated)
	}
	if c.SuggestedUploadDate == "" {
		t.Fatal("expected suggested upload date from file mtime")
	}
}

func TestEnqueueImportCreateRequiresSeries(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	_ = os.MkdirAll(inbox, 0o755)
	s.ImportRoot = inbox
	media := filepath.Join(inbox, "x.mkv")
	_ = os.WriteFile(media, []byte("x"), 0o644)
	_, _, err := s.EnqueueImportCreate(media, library.CreateImportVideoParams{Title: "X"})
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want invalid, got %v", err)
	}
}

func TestEnqueueImportRejectsOutsideRoot(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	_ = os.MkdirAll(inbox, 0o755)
	s.ImportRoot = inbox
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "X", SourceURL: "https://example.com/a", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'r1', 'T', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var vid int64
	_ = s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'r1'`).Scan(&vid)
	outside := filepath.Join(t.TempDir(), "escape.mkv")
	_ = os.WriteFile(outside, []byte("x"), 0o644)
	_, err = s.EnqueueImport(outside, vid, false, false)
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want invalid, got %v", err)
	}
}

func TestScanImportLibraryOrphanBindInPlace(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	libRoot := t.TempDir()
	root, err := s.CreateRoot("archive", libRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Lib Show", SourceURL: "https://example.com/lib", RootID: root.ID, QualityProfileID: profile.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'lib99', 'Orphan Ep', 'deleted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'lib99'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}

	media := filepath.Join(libRoot, "Lib Show", "Orphan Ep [lib99].mkv")
	if err := os.MkdirAll(filepath.Dir(media), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, []byte("libfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := strings.TrimSuffix(media, filepath.Ext(media)) + ".nfo"
	if err := os.WriteFile(nfo, []byte("<episodedetails/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.ScanImport(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	var orphan *library.ImportCandidate
	for i := range res.Candidates {
		c := &res.Candidates[i]
		if c.Source == library.ImportSourceLibrary && c.Role == library.ImportRoleVideo {
			orphan = c
			break
		}
	}
	if orphan == nil {
		t.Fatalf("no library video candidate: %+v", res.Candidates)
	}
	if orphan.SuggestedVideoID == nil || *orphan.SuggestedVideoID != videoID {
		t.Fatalf("want video %d, got %+v", videoID, orphan)
	}

	taskID, err := s.EnqueueImport(orphan.Path, videoID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if taskID <= 0 {
		t.Fatal("task id")
	}
	var payload string
	if err := s.DB.SQL.QueryRow(`SELECT payload FROM tasks WHERE id = ?`, taskID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, `"in_place":true`) {
		t.Fatalf("payload=%s", payload)
	}

	nfoPath, infoPath := library.SidecarPathsBeside(orphan.Path)
	if nfoPath == "" {
		t.Fatal("expected nfo sidecar")
	}
	if err := s.CompleteImport(videoID, orphan.Path, nfoPath, infoPath, "", nil, library.MediaCompleteMeta{
		Tool: "import", InPlace: true, ImportSrc: orphan.Path,
	}, taskID); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "downloaded" {
		t.Fatalf("status=%s", v.Status)
	}
	path, ok, err := s.HasVideoFile(videoID)
	if err != nil || !ok || path != orphan.Path {
		t.Fatalf("file path=%q ok=%v err=%v", path, ok, err)
	}
	if _, err := os.Stat(orphan.Path); err != nil {
		t.Fatal("library file should still exist in place")
	}

	res2, err := s.ScanImport(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res2.Candidates {
		if c.Path == orphan.Path {
			t.Fatal("bound path should not appear as orphan")
		}
	}
}

func TestScanImportSkipsSeriesFolderMeta(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	libRoot := t.TempDir()
	root, err := s.CreateRoot("archive", libRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Meta Show", SourceURL: "https://example.com/meta", RootID: root.ID, QualityProfileID: profile.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	seriesDir := library.SeriesDir(libRoot, ser.Title)
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tvshow := filepath.Join(seriesDir, "tvshow.nfo")
	poster := filepath.Join(seriesDir, "poster.jpg")
	banner := filepath.Join(seriesDir, "banner.jpg")
	for _, p := range []string{tvshow, poster, banner} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	notes := filepath.Join(seriesDir, "notes.txt")
	if err := os.WriteFile(notes, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	loosePoster := filepath.Join(libRoot, "poster.jpg")
	if err := os.WriteFile(loosePoster, []byte("loose"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.ScanImport(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, c := range res.Candidates {
		paths[c.Path] = true
	}
	for _, skip := range []string{tvshow, poster, banner} {
		if paths[skip] {
			t.Fatalf("series folder meta should be skipped: %s", skip)
		}
	}
	if !paths[notes] {
		t.Fatal("unmanaged notes.txt under series folder should still list")
	}
	if !paths[loosePoster] {
		t.Fatal("poster.jpg outside a series folder should still list")
	}
}

func TestClassifyImportFile(t *testing.T) {
	cases := []struct {
		name, role, stem string
	}{
		{"Show S01E01.mkv", library.ImportRoleVideo, "Show S01E01"},
		{"Show S01E01.nfo", library.ImportRoleNFO, "Show S01E01"},
		{"Show S01E01.info.json", library.ImportRoleJSON, "Show S01E01"},
		{"Show S01E01-thumb.jpg", library.ImportRoleThumb, "Show S01E01"},
		{"Show S01E01.srt", library.ImportRoleSub, "Show S01E01"},
		{"readme.txt", library.ImportRoleOther, "readme"},
	}
	for _, tc := range cases {
		role, stem := library.ClassifyImportFile(tc.name)
		if role != tc.role || stem != tc.stem {
			t.Errorf("%s: got %s/%s want %s/%s", tc.name, role, stem, tc.role, tc.stem)
		}
	}
}

func TestScanImportSidecarStemAndOther(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	_ = os.MkdirAll(inbox, 0o755)
	s.ImportRoot = inbox
	libRoot := t.TempDir()
	root, err := s.CreateRoot("archive", libRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Side Show", SourceURL: "https://example.com/side", RootID: root.ID, QualityProfileID: profile.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'side1', 'Ep One', 'downloaded')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	_ = s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'side1'`).Scan(&videoID)

	dir := filepath.Join(libRoot, "Side Show")
	_ = os.MkdirAll(dir, 0o755)
	media := filepath.Join(dir, "Ep One [side1].mkv")
	_ = os.WriteFile(media, []byte("media"), 0o644)
	if err := s.CompleteImport(videoID, media, "", "", "", nil, library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}

	nfo := filepath.Join(dir, "Ep One [side1].nfo")
	_ = os.WriteFile(nfo, []byte("<episodedetails/>"), 0o644)
	info := filepath.Join(dir, "Ep One [side1].info.json")
	_ = os.WriteFile(info, []byte(`{"id":"side1"}`), 0o644)
	other := filepath.Join(dir, "notes.txt")
	_ = os.WriteFile(other, []byte("hi"), 0o644)

	res, err := s.ScanImport(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawNFO, sawJSON, sawOther bool
	for _, c := range res.Candidates {
		switch c.Path {
		case nfo:
			sawNFO = true
			if c.Role != library.ImportRoleNFO || c.MatchType != "sidecar_stem" || c.SuggestedVideoID == nil || *c.SuggestedVideoID != videoID {
				t.Fatalf("nfo candidate=%+v", c)
			}
		case info:
			sawJSON = true
			if c.Role != library.ImportRoleJSON || c.SuggestedVideoID != nil {
				t.Fatalf("json candidate=%+v want unmatched provenance", c)
			}
		case other:
			sawOther = true
			if c.Role != library.ImportRoleOther || c.SuggestedVideoID != nil {
				t.Fatalf("other candidate=%+v", c)
			}
		case media:
			t.Fatal("tracked media should not appear")
		}
	}
	if !sawNFO || !sawJSON || !sawOther {
		t.Fatalf("saw nfo=%v json=%v other=%v candidates=%d", sawNFO, sawJSON, sawOther, len(res.Candidates))
	}

	taskID, err := s.EnqueueAttachSidecars(videoID, []string{nfo})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(nfo, []byte(`<?xml version="1.0"?><episodedetails><title>Attached Title</title><plot>Attached plot</plot></episodedetails>`), 0o644)
	if err := s.AttachSidecarFiles(videoID, []string{nfo}, taskID); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "Attached Title" || v.Description != "Attached plot" {
		t.Fatalf("want metadata from nfo, got title=%q plot=%q", v.Title, v.Description)
	}
	if _, err := s.EnqueueAttachSidecars(videoID, []string{info}); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("attach info.json: want ErrInvalid, got %v", err)
	}
	wrongStem := filepath.Join(dir, "Other Title.nfo")
	_ = os.WriteFile(wrongStem, []byte(`<episodedetails/>`), 0o644)
	if _, err := s.EnqueueAttachSidecars(videoID, []string{wrongStem}); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("attach wrong-stem nfo: want ErrInvalid, got %v", err)
	}
	wrongThumb := filepath.Join(dir, "Other Title-thumb.jpg")
	_ = os.WriteFile(wrongThumb, []byte("x"), 0o644)
	if _, err := s.EnqueueAttachSidecars(videoID, []string{wrongThumb}); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("attach wrong-stem thumb: want ErrInvalid, got %v", err)
	}
	var nfoCount, jsonCount int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM files WHERE video_id = ? AND kind = 'nfo'`, videoID).Scan(&nfoCount)
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM files WHERE video_id = ? AND kind = 'json'`, videoID).Scan(&jsonCount)
	if nfoCount != 1 || jsonCount != 0 {
		t.Fatalf("nfo=%d json=%d", nfoCount, jsonCount)
	}

	res2, err := s.ScanImport(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res2.Candidates {
		if c.Path == nfo {
			t.Fatalf("attached sidecar still listed: %s", c.Path)
		}
	}
}

func TestAttachInboxSubtitleMovesBesideMedia(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	_ = os.MkdirAll(inbox, 0o755)
	s.ImportRoot = inbox
	libRoot := t.TempDir()
	root, err := s.CreateRoot("archive", libRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Sub Show", SourceURL: "https://example.com/sub", RootID: root.ID, QualityProfileID: profile.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'sub1', 'Ep Sub', 'downloaded')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	_ = s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'sub1'`).Scan(&videoID)

	dir := filepath.Join(libRoot, "Sub Show")
	_ = os.MkdirAll(dir, 0o755)
	media := filepath.Join(dir, "Ep Sub [sub1].mkv")
	_ = os.WriteFile(media, []byte("media"), 0o644)
	if err := s.CompleteImport(videoID, media, "", "", "", nil, library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}

	inboxSub := filepath.Join(inbox, "test.en.srt")
	_ = os.WriteFile(inboxSub, []byte("1\n00:00:01,000 --> 00:00:02,000\nhi\n"), 0o644)
	taskID, err := s.EnqueueAttachSidecars(videoID, []string{inboxSub})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSidecarFiles(videoID, []string{inboxSub}, taskID); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "Ep Sub [sub1].en.srt")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("want moved subtitle at %s: %v", want, err)
	}
	if _, err := os.Stat(inboxSub); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inbox subtitle should be gone, stat=%v", err)
	}
	var n int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM files WHERE video_id = ? AND kind = 'sub' AND path = ?`, videoID, want).Scan(&n)
	if n != 1 {
		t.Fatalf("want 1 sub file row, got %d", n)
	}
}

func TestEnqueueImportReplaceExistingMedia(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Replace Show", SourceURL: "https://example.com/replace",
		RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'rep1', 'Has Media', 'downloaded')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'rep1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	oldMedia := filepath.Join(t.TempDir(), "old.mkv")
	if err := os.WriteFile(oldMedia, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))
	`, videoID, oldMedia); err != nil {
		t.Fatal(err)
	}

	picker, err := s.ListImportPickerVideos()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range picker {
		if v.ID == videoID {
			found = true
			if !v.HasMedia {
				t.Fatal("want has_media true")
			}
		}
	}
	if !found {
		t.Fatal("picker missing video")
	}
	seriesPicker, err := s.ListImportPickerSeries()
	if err != nil {
		t.Fatal(err)
	}
	foundSeries := false
	for _, serRow := range seriesPicker {
		if serRow.ID == ser.ID && serRow.Title == "Replace Show" {
			foundSeries = true
			break
		}
	}
	if !foundSeries {
		t.Fatal("picker series missing")
	}

	media := filepath.Join(inbox, "Has Media [rep1].mkv")
	if err := os.WriteFile(media, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueImport(media, videoID, false, false); !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want conflict without replace, got %v", err)
	}
	taskID, err := s.EnqueueImport(media, videoID, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if taskID <= 0 {
		t.Fatal("task id")
	}
	var payload string
	if err := s.DB.SQL.QueryRow(`SELECT payload FROM tasks WHERE id = ?`, taskID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, `"replace":true`) {
		t.Fatalf("payload=%s", payload)
	}
}

func TestCompleteImportRegistersDashThumb(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "ThumbImp",
		SourceURL:        "https://www.example.com/@thumbimp",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        false,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "th1", Title: "T", WebpageURL: "https://www.example.com/watch?v=th1",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root.Path, "ThumbImp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "Show [th1].mkv")
	thumb := filepath.Join(dir, "Show-thumb.jpg")
	if err := os.WriteFile(media, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumb, []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, foundThumb, _ := library.FindDownloadSidecars(media)
	if foundThumb != thumb {
		t.Fatalf("FindDownloadSidecars thumb=%q", foundThumb)
	}
	if err := s.CompleteImport(res.VideoID, media, "", "", foundThumb, nil, library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.VideoThumbPath(res.VideoID)
	if err != nil || !ok || got != thumb {
		t.Fatalf("thumb path=%q ok=%v err=%v", got, ok, err)
	}
}

