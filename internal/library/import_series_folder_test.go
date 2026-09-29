package library_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestScanImportSeriesFolderUnknownLocksMedia(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	inbox := filepath.Join(tmp, "import")
	libRoot := filepath.Join(tmp, "library")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(libRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	s := library.NewStore(d, queue.NewStore(d))
	s.ImportRoot = inbox
	if _, err := s.CreateRoot("lib", libRoot, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile("best", "bv*+ba/b"); err != nil {
		t.Fatal(err)
	}

	show := filepath.Join(inbox, "Cool Show")
	sub := filepath.Join(show, "S2020")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	nfo := `<?xml version="1.0"?><tvshow><title>Cool Show</title><status>Continuing</status><plot>Hi</plot></tvshow>`
	if err := os.WriteFile(filepath.Join(show, "tvshow.nfo"), []byte(nfo), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(show, "poster.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(sub, "ep [abc].mkv")
	if err := os.WriteFile(media, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.ScanImport()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SeriesFolders) != 1 {
		t.Fatalf("series_folders=%d want 1", len(res.SeriesFolders))
	}
	f := res.SeriesFolders[0]
	if !f.Unknown || f.Title != "Cool Show" || f.MediaCount != 1 {
		t.Fatalf("folder: %+v", f)
	}
	if len(f.ArtRoles) != 1 || f.ArtRoles[0] != "poster" {
		t.Fatalf("art: %v", f.ArtRoles)
	}
	var mediaCand *library.ImportCandidate
	for i := range res.Candidates {
		c := &res.Candidates[i]
		if c.Role == "video" {
			mediaCand = c
			break
		}
	}
	if mediaCand == nil {
		t.Fatal("missing video candidate")
	}
	if !mediaCand.SeriesFolderLocked || mediaCand.SeriesFolderDraftKey == "" {
		t.Fatalf("lock: %+v", mediaCand)
	}
	for _, c := range res.Candidates {
		if c.Filename == "tvshow.nfo" || c.Filename == "poster.jpg" {
			t.Fatalf("series meta should be skipped: %s", c.Filename)
		}
	}
}

func TestScanImportSeriesFolderKnown(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	libRoot := filepath.Join(tmp, "library")
	inbox := filepath.Join(tmp, "import")
	if err := os.MkdirAll(libRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	s := library.NewStore(d, queue.NewStore(d))
	s.ImportRoot = inbox
	root, err := s.CreateRoot("lib", libRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := s.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Known Show", RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Inbox tree matched by title to the existing series.
	showDir := filepath.Join(inbox, "Known Show")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte(`<tvshow><title>Known Show</title></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(showDir, "ep.mkv")
	if err := os.WriteFile(media, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.ScanImport()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SeriesFolders) != 1 || res.SeriesFolders[0].Unknown {
		t.Fatalf("folders: %+v", res.SeriesFolders)
	}
	if res.SeriesFolders[0].SeriesID == nil || *res.SeriesFolders[0].SeriesID != ser.ID {
		t.Fatalf("series_id: %+v", res.SeriesFolders[0].SeriesID)
	}
	found := false
	for _, c := range res.Candidates {
		if c.Role != "video" {
			continue
		}
		found = true
		if !c.SeriesFolderLocked || c.SuggestedSeriesID == nil || *c.SuggestedSeriesID != ser.ID {
			t.Fatalf("candidate lock: %+v", c)
		}
	}
	if !found {
		t.Fatal("no video candidate")
	}
}

func TestEnqueueImportAllowsRematchUnderUnknownSeriesFolder(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	libRoot := filepath.Join(tmp, "library")
	inbox := filepath.Join(tmp, "import")
	_ = os.MkdirAll(libRoot, 0o755)
	_ = os.MkdirAll(inbox, 0o755)
	s := library.NewStore(d, queue.NewStore(d))
	s.ImportRoot = inbox
	root, err := s.CreateRoot("lib", libRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := s.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Other", SourceURL: "https://example.com/o", RootID: root.ID, QualityProfileID: prof.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`INSERT INTO videos (series_id, remote_id, title, status) VALUES (?, 'v1', 'Ep', 'wanted')`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	_ = s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'v1'`).Scan(&videoID)
	folder := filepath.Join(inbox, "Locked Show")
	_ = os.MkdirAll(folder, 0o755)
	if err := os.WriteFile(filepath.Join(folder, "tvshow.nfo"), []byte(`<tvshow><title>Locked Show</title></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(folder, "ep.mkv")
	if err := os.WriteFile(media, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	media2 := filepath.Join(folder, "ep2.mkv")
	if err := os.WriteFile(media2, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Match may rematch under an unknown tvshow.nfo tree (auto-select only; joins stay editable).
	if _, err := s.EnqueueImport(media, videoID, false); err != nil {
		t.Fatalf("bind under unknown tvshow.nfo: %v", err)
	}
	if _, _, err := s.EnqueueImportCreate(media2, library.CreateImportVideoParams{
		SeriesID: ser.ID, Title: "Ep", UploadDate: "2020-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("create under unknown tvshow.nfo: %v", err)
	}
	if _, err := s.EnqueueImportPlan(library.ImportPlanPayload{
		Jobs: []library.ImportPlanJob{{
			Path: media, VideoID: videoID,
		}},
	}); err != nil {
		t.Fatalf("plan bind under unknown tvshow.nfo: %v", err)
	}
}

func TestImportSeriesTitleFromFolder(t *testing.T) {
	tmp := t.TempDir()
	withTitle := filepath.Join(tmp, "Named")
	_ = os.MkdirAll(withTitle, 0o755)
	if err := os.WriteFile(filepath.Join(withTitle, "tvshow.nfo"), []byte(`<tvshow><title>From NFO</title></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := library.ImportSeriesTitleFromFolder(withTitle); got != "From NFO" {
		t.Fatalf("nfo title=%q", got)
	}
	noTitle := filepath.Join(tmp, "Folder Fallback")
	_ = os.MkdirAll(noTitle, 0o755)
	if err := os.WriteFile(filepath.Join(noTitle, "tvshow.nfo"), []byte(`<tvshow><plot>x</plot></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := library.ImportSeriesTitleFromFolder(noTitle); got != "Folder Fallback" {
		t.Fatalf("folder fallback=%q", got)
	}
}

func TestEnqueueImportPlanKeepsClientTitle(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	libRoot := filepath.Join(tmp, "library")
	inbox := filepath.Join(tmp, "import")
	_ = os.MkdirAll(libRoot, 0o755)
	_ = os.MkdirAll(inbox, 0o755)
	s := library.NewStore(d, queue.NewStore(d))
	s.ImportRoot = inbox
	root, err := s.CreateRoot("lib", libRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := s.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(inbox, "Real Show")
	_ = os.MkdirAll(folder, 0o755)
	if err := os.WriteFile(filepath.Join(folder, "tvshow.nfo"), []byte(`<tvshow><title>Real Show</title></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(folder, "a.mkv")
	if err := os.WriteFile(media, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskID, err := s.EnqueueImportPlan(library.ImportPlanPayload{
		Series: []library.ImportPlanSeriesDraft{{
			DraftKey: folder, FolderPath: folder, Title: "Client Rename",
			RootID: root.ID, QualityProfileID: prof.ID, Monitored: true, DeliveryMode: "video",
		}},
		Jobs: []library.ImportPlanJob{{
			Path: media, SeriesDraftKey: folder, Title: "Ep", UploadDate: "2020-01-01T00:00:00Z",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := d.SQL.QueryRow(`SELECT payload FROM tasks WHERE id = ?`, taskID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, "Client Rename") {
		t.Fatalf("payload should keep operator title rename, got %s", payload)
	}
}

func TestEnqueueImportPlanNoSeriesRows(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	libRoot := filepath.Join(tmp, "library")
	inbox := filepath.Join(tmp, "import")
	_ = os.MkdirAll(libRoot, 0o755)
	_ = os.MkdirAll(inbox, 0o755)
	s := library.NewStore(d, queue.NewStore(d))
	s.ImportRoot = inbox
	root, err := s.CreateRoot("lib", libRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := s.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(inbox, "Show")
	_ = os.MkdirAll(folder, 0o755)
	if err := os.WriteFile(filepath.Join(folder, "tvshow.nfo"), []byte(`<tvshow><title>Show</title></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(folder, "a.mkv")
	if err := os.WriteFile(media, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskID, err := s.EnqueueImportPlan(library.ImportPlanPayload{
		Series: []library.ImportPlanSeriesDraft{{
			DraftKey: folder, FolderPath: folder, Title: "Show",
			RootID: root.ID, QualityProfileID: prof.ID, Monitored: true, DeliveryMode: "video",
		}},
		Jobs: []library.ImportPlanJob{{
			Path: media, SeriesDraftKey: folder, Title: "Ep", UploadDate: "2020-01-01T00:00:00Z",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if taskID <= 0 {
		t.Fatal("task id")
	}
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM series`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("series rows=%d want 0 before worker", n)
	}
	var kind string
	if err := d.SQL.QueryRow(`SELECT kind FROM tasks WHERE id = ?`, taskID).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != queue.KindImportPlan {
		t.Fatalf("kind=%s", kind)
	}
}

func TestRemoveImportSeriesFolderIfDrained(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	inbox := filepath.Join(tmp, "import")
	libRoot := filepath.Join(tmp, "library")
	_ = os.MkdirAll(inbox, 0o755)
	_ = os.MkdirAll(libRoot, 0o755)
	s := library.NewStore(d, queue.NewStore(d))
	s.ImportRoot = inbox

	folder := filepath.Join(inbox, "Cool Show")
	sub := filepath.Join(folder, "S2020")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(folder, "tvshow.nfo"), []byte(`<tvshow><title>Cool Show</title></tvshow>`), 0o644)
	_ = os.WriteFile(filepath.Join(folder, "poster.jpg"), []byte("p"), 0o644)
	media := filepath.Join(sub, "ep.mkv")
	_ = os.WriteFile(media, []byte("m"), 0o644)

	if got := s.NearestImportSeriesFolder(media); got != folder {
		t.Fatalf("nearest=%q want %q", got, folder)
	}
	if err := s.RemoveImportSeriesFolderIfDrained(folder); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("folder with media must stay: %v", err)
	}

	_ = os.Remove(media)
	if err := s.RemoveImportSeriesFolderIfDrained(folder); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("drained inbox series folder should be gone, stat=%v", err)
	}

	// Library series folders are never deleted by this helper.
	libShow := filepath.Join(libRoot, "Lib Show")
	_ = os.MkdirAll(libShow, 0o755)
	_ = os.WriteFile(filepath.Join(libShow, "tvshow.nfo"), []byte(`<tvshow><title>Lib Show</title></tvshow>`), 0o644)
	if err := s.RemoveImportSeriesFolderIfDrained(libShow); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(libShow); err != nil {
		t.Fatalf("library folder must stay: %v", err)
	}
}

func TestApplyImportSeriesFolderKeepsInboxTree(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	inbox := filepath.Join(tmp, "import")
	libRoot := filepath.Join(tmp, "library")
	_ = os.MkdirAll(inbox, 0o755)
	_ = os.MkdirAll(libRoot, 0o755)
	s := library.NewStore(d, queue.NewStore(d))
	s.ImportRoot = inbox
	root, err := s.CreateRoot("lib", libRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := s.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(inbox, "Cool Show")
	_ = os.MkdirAll(folder, 0o755)
	_ = os.WriteFile(filepath.Join(folder, "tvshow.nfo"), []byte(`<tvshow><title>Cool Show</title><plot>Hi</plot></tvshow>`), 0o644)
	_ = os.WriteFile(filepath.Join(folder, "poster.jpg"), []byte("p"), 0o644)
	media := filepath.Join(folder, "ep.mkv")
	_ = os.WriteFile(media, []byte("media"), 0o644)

	sid, err := s.ApplyImportSeriesFolder(library.ImportPlanSeriesDraft{
		DraftKey: folder, FolderPath: folder, Title: "Cool Show",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: true, DeliveryMode: "video",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sid <= 0 {
		t.Fatal("series id")
	}
	if _, err := os.Stat(media); err != nil {
		t.Fatalf("inbox media must remain until PackMedia: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, "tvshow.nfo")); err != nil {
		t.Fatalf("inbox tvshow.nfo must remain until drained: %v", err)
	}
	libPoster := filepath.Join(library.SeriesDir(libRoot, "Cool Show"), "poster.jpg")
	if _, err := os.Stat(libPoster); err != nil {
		t.Fatalf("library art should be copied: %v", err)
	}
}
