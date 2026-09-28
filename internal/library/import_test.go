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

	res, err := s.ScanImport()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("candidates=%d", len(res.Candidates))
	}
	c := res.Candidates[0]
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

func TestScanImportPrefersNFOUniqueIDOverBracketAndInfoJSON(t *testing.T) {
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
		       (?, 'nfo1', 'NFO Hit', 'wanted'),
		       (?, 'info1', 'Info Hit', 'wanted')
	`, ser.ID, ser.ID, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var nfoVID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'nfo1'`).Scan(&nfoVID); err != nil {
		t.Fatal(err)
	}

	media := filepath.Join(inbox, "Ep [bracket1].mkv")
	if err := os.WriteFile(media, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(inbox, "Ep [bracket1].info.json")
	if err := os.WriteFile(info, []byte(`{"id":"info1","title":"Info Title","description":"Info plot","upload_date":"20200101"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := filepath.Join(inbox, "Ep [bracket1].nfo")
	if err := os.WriteFile(nfo, []byte(`<?xml version="1.0"?><episodedetails>
  <title>NFO Title</title>
  <plot>NFO plot</plot>
  <aired>2024-06-15</aired>
  <uniqueid type="yt-dlp" default="true">nfo1</uniqueid>
</episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.ScanImport()
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
	if c.MatchType != "id" || c.SuggestedVideoID == nil || *c.SuggestedVideoID != nfoVID {
		t.Fatalf("match=%+v want NFO video %d", c, nfoVID)
	}
	if len(c.IDs) < 1 || c.IDs[0].RemoteID != "nfo1" {
		t.Fatalf("IDs order=%+v want nfo1 first", c.IDs)
	}
	if c.SuggestedTitle != "NFO Title" {
		t.Fatalf("SuggestedTitle=%q want NFO Title", c.SuggestedTitle)
	}
	if c.SuggestedUploadDateFromMtime || !strings.HasPrefix(c.SuggestedUploadDate, "2024-06-15") {
		t.Fatalf("upload=%q fromMtime=%v want NFO aired", c.SuggestedUploadDate, c.SuggestedUploadDateFromMtime)
	}
}

func TestScanImportPrefersNFOTitlePlotOverInfoJSON(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	rootID, profileID := seedRootProfile(t, s)

	media := filepath.Join(inbox, "Loose Ep.mkv")
	if err := os.WriteFile(media, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(inbox, "Loose Ep.info.json")
	if err := os.WriteFile(info, []byte(`{"id":"loose1","title":"JSON Title","description":"JSON plot","upload_date":"20200101","webpage_url":"https://example.com/v/loose1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := filepath.Join(inbox, "Loose Ep.nfo")
	if err := os.WriteFile(nfo, []byte(`<?xml version="1.0"?><episodedetails>
  <title>NFO Title</title>
  <plot>NFO plot text</plot>
  <aired>2023-12-01</aired>
  <uniqueid type="yt-dlp" default="true">loose1</uniqueid>
</episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.ScanImport()
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
	if c.SuggestedTitle != "NFO Title" {
		t.Fatalf("SuggestedTitle=%q", c.SuggestedTitle)
	}
	if !strings.HasPrefix(c.SuggestedUploadDate, "2023-12-01") {
		t.Fatalf("SuggestedUploadDate=%q", c.SuggestedUploadDate)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Loose Show", SourceURL: "https://example.com/loose", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, videoID, err := s.EnqueueImportCreate(c.Path, library.CreateImportVideoParams{
		SeriesID: ser.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "NFO Title" || v.Description != "NFO plot text" {
		t.Fatalf("title=%q desc=%q", v.Title, v.Description)
	}
	if !v.UploadDate.Valid || !strings.HasPrefix(v.UploadDate.String, "2023-12-01") {
		t.Fatalf("upload_date=%v", v.UploadDate)
	}
}

func TestScanImportBracketBeatsInfoJSONWhenNoNFOUniqueID(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Demo Show", SourceURL: "https://www.example.com/@demo", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'bracket1', 'Bracket Hit', 'wanted'),
		       (?, 'info1', 'Info Hit', 'wanted')
	`, ser.ID, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var bracketVID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'bracket1'`).Scan(&bracketVID); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(inbox, "Ep [bracket1].mkv")
	if err := os.WriteFile(media, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(inbox, "Ep [bracket1].info.json")
	if err := os.WriteFile(info, []byte(`{"id":"info1","title":"Info"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// NFO without uniqueid (title only).
	nfo := filepath.Join(inbox, "Ep [bracket1].nfo")
	if err := os.WriteFile(nfo, []byte(`<?xml version="1.0"?><episodedetails><title>T</title></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := s.ScanImport()
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
	if c.SuggestedVideoID == nil || *c.SuggestedVideoID != bracketVID {
		t.Fatalf("match=%+v want bracket %d", c, bracketVID)
	}
	if len(c.IDs) < 1 || c.IDs[0].RemoteID != "bracket1" {
		t.Fatalf("IDs=%+v want bracket1 first", c.IDs)
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

	res, err := s.ScanImport()
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
	res, err := s.ScanImport()
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

func TestScanImportNoSyntheticRemoteID(t *testing.T) {
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
	res, err := s.ScanImport()
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
	if c.SuggestedRemoteID != "" || c.SuggestedRemoteIDGenerated {
		t.Fatalf("want empty suggested remote (assign videos.id on create), got id=%q generated=%v", c.SuggestedRemoteID, c.SuggestedRemoteIDGenerated)
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
	_, err = s.EnqueueImport(outside, vid, false)
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want invalid, got %v", err)
	}
}

func TestEnqueueImportRejectsLibraryPath(t *testing.T) {
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

	if _, err := s.ValidateImportMediaPath(media); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("ValidateImportMediaPath: want ErrInvalid, got %v", err)
	}
	if _, err := s.EnqueueImport(media, videoID, false); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("EnqueueImport: want ErrInvalid, got %v", err)
	}
}

func TestScanImportSkipsSeriesFolderMeta(t *testing.T) {
	s := openLib(t)
	inbox := filepath.Join(t.TempDir(), "import")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s.ImportRoot = inbox
	show := filepath.Join(inbox, "Meta Show")
	if err := os.MkdirAll(show, 0o755); err != nil {
		t.Fatal(err)
	}
	tvshow := filepath.Join(show, "tvshow.nfo")
	poster := filepath.Join(show, "poster.jpg")
	banner := filepath.Join(show, "banner.jpg")
	for _, p := range []string{tvshow, poster, banner} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	notes := filepath.Join(show, "notes.txt")
	if err := os.WriteFile(notes, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	loosePoster := filepath.Join(inbox, "poster.jpg")
	if err := os.WriteFile(loosePoster, []byte("loose"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.ScanImport()
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
		{"Show S01E01.en.srt", library.ImportRoleSub, "Show S01E01"},
		{"Show S01E01.eng.srt", library.ImportRoleSub, "Show S01E01"},
		{"Show S01E01.en-US.vtt", library.ImportRoleSub, "Show S01E01"},
		{"Show S01E01.en.auto.srt", library.ImportRoleSub, "Show S01E01"},
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
	if err := s.CompleteImport(videoID, media, "", "", "", nil, library.MediaCompleteMeta{}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}

	nfo := filepath.Join(inbox, "Ep One [side1].nfo")
	_ = os.WriteFile(nfo, []byte("<episodedetails/>"), 0o644)
	info := filepath.Join(inbox, "Ep One [side1].info.json")
	_ = os.WriteFile(info, []byte(`{"id":"side1"}`), 0o644)
	other := filepath.Join(inbox, "notes.txt")
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
			if c.Role != library.ImportRoleNFO {
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
		}
	}
	if !sawNFO || !sawJSON || !sawOther {
		t.Fatalf("saw nfo=%v json=%v other=%v candidates=%d", sawNFO, sawJSON, sawOther, len(res.Candidates))
	}

	// NFO already beside packed media can still be applied (inbox NFO attach is rejected).
	besideNFO := filepath.Join(dir, "Ep One [side1].nfo")
	_ = os.WriteFile(besideNFO, []byte(`<?xml version="1.0"?><episodedetails><title>Attached Title</title><plot>Attached plot</plot></episodedetails>`), 0o644)
	taskID := seedTaskID(t, s)
	if err := s.AttachSidecarFiles(videoID, []string{besideNFO}, taskID); err != nil {
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
	if _, err := s.EnqueueAttachSidecars(videoID, []string{besideNFO}); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("attach library-path nfo: want ErrInvalid, got %v", err)
	}
	wrongStem := filepath.Join(inbox, "Other Title.nfo")
	_ = os.WriteFile(wrongStem, []byte(`<episodedetails/>`), 0o644)
	if _, err := s.EnqueueAttachSidecars(videoID, []string{wrongStem}); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("attach wrong-stem nfo: want ErrInvalid, got %v", err)
	}
	wrongThumb := filepath.Join(inbox, "Other Title-thumb.jpg")
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
	if err := s.CompleteImport(videoID, media, "", "", "", nil, library.MediaCompleteMeta{}, seedTaskID(t, s)); err != nil {
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

	picker, err := s.ListImportPickerVideos(library.ImportPickerVideoQuery{SeriesID: &ser.ID})
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
	if _, err := s.EnqueueImport(media, videoID, false); !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want conflict without replace, got %v", err)
	}
	taskID, err := s.EnqueueImport(media, videoID, true)
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
	if err := s.CompleteImport(res.VideoID, media, "", "", foundThumb, nil, library.MediaCompleteMeta{}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.VideoThumbPath(res.VideoID)
	if err != nil || !ok || got != thumb {
		t.Fatalf("thumb path=%q ok=%v err=%v", got, ok, err)
	}
}

