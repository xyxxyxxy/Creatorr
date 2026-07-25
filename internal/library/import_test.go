package library_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	taskID, err := s.EnqueueImport(c.Path, *c.SuggestedVideoID, false)
	if err != nil {
		t.Fatal(err)
	}
	if taskID <= 0 {
		t.Fatal("task id")
	}
	_, err = s.EnqueueImport(c.Path, *c.SuggestedVideoID, false)
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
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
	if !v.SourceURL.Valid || v.SourceURL.String != "https://example.com/v/zz99" {
		t.Fatalf("source_url=%v", v.SourceURL)
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
	_, err = s.EnqueueImport(outside, vid, false)
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
	root, err := s.CreateRoot("archive", libRoot, nil)
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

	res, err := s.ScanImport()
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

	taskID, err := s.EnqueueImport(orphan.Path, videoID, false)
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
	if err := s.CompleteImport(videoID, orphan.Path, nfoPath, infoPath, library.MediaCompleteMeta{
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

	res2, err := s.ScanImport()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res2.Candidates {
		if c.Path == orphan.Path {
			t.Fatal("bound path should not appear as orphan")
		}
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
	root, err := s.CreateRoot("archive", libRoot, nil)
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
	if err := s.CompleteImport(videoID, media, "", "", library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}

	nfo := filepath.Join(dir, "Ep One [side1].nfo")
	_ = os.WriteFile(nfo, []byte("<episodedetails/>"), 0o644)
	info := filepath.Join(dir, "Ep One [side1].info.json")
	_ = os.WriteFile(info, []byte(`{"id":"side1"}`), 0o644)
	other := filepath.Join(dir, "notes.txt")
	_ = os.WriteFile(other, []byte("hi"), 0o644)

	res, err := s.ScanImport()
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
			if c.Role != library.ImportRoleJSON || c.SuggestedVideoID == nil || *c.SuggestedVideoID != videoID {
				t.Fatalf("json candidate=%+v", c)
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

	taskID, err := s.EnqueueAttachSidecars(videoID, []string{nfo, info})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSidecarFiles(videoID, []string{nfo, info}, taskID); err != nil {
		t.Fatal(err)
	}
	var nfoCount, jsonCount int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM files WHERE video_id = ? AND kind = 'nfo'`, videoID).Scan(&nfoCount)
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM files WHERE video_id = ? AND kind = 'json'`, videoID).Scan(&jsonCount)
	if nfoCount != 1 || jsonCount != 1 {
		t.Fatalf("nfo=%d json=%d", nfoCount, jsonCount)
	}

	res2, err := s.ScanImport()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res2.Candidates {
		if c.Path == nfo || c.Path == info {
			t.Fatalf("attached sidecar still listed: %s", c.Path)
		}
	}
}

