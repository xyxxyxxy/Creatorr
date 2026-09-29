package library_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestEnqueueImportPlanBareDraft(t *testing.T) {
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
	media := filepath.Join(inbox, "ep.mkv")
	if err := os.WriteFile(media, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskID, err := s.EnqueueImportPlan(library.ImportPlanPayload{
		Series: []library.ImportPlanSeriesDraft{{
			DraftKey: "local:bare1", Kind: library.ImportSeriesDraftBare, Title: "Bare Show",
			RootID: root.ID, QualityProfileID: prof.ID, Monitored: true, DeliveryMode: "video",
		}},
		Jobs: []library.ImportPlanJob{{
			Path: media, SeriesDraftKey: "local:bare1", Title: "Ep", UploadDate: "2020-01-01T00:00:00Z",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := d.SQL.QueryRow(`SELECT payload FROM tasks WHERE id = ?`, taskID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, `"kind":"bare"`) || !strings.Contains(payload, "Bare Show") {
		t.Fatalf("payload=%s", payload)
	}
	sid, err := s.ApplyImportSeriesDraft(library.ImportPlanSeriesDraft{
		DraftKey: "local:bare1", Kind: library.ImportSeriesDraftBare, Title: "Bare Show",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: true, DeliveryMode: "video",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sid <= 0 {
		t.Fatal("expected series id")
	}
}

func TestEnqueueImportPlanEmptySeriesCreateUnderExisting(t *testing.T) {
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
		Title: "Existing", RootID: root.ID, QualityProfileID: prof.ID, Monitored: true, DeliveryMode: "video",
	})
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(inbox, "ep.mkv")
	if err := os.WriteFile(media, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.EnqueueImportPlan(library.ImportPlanPayload{
		Jobs: []library.ImportPlanJob{{
			Path: media, SeriesID: ser.ID, Title: "Ep", UploadDate: "2020-01-01T00:00:00Z",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueImportPlanRejectsDupRemoteAndTitle(t *testing.T) {
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
	m1 := filepath.Join(inbox, "a.mkv")
	m2 := filepath.Join(inbox, "b.mkv")
	_ = os.WriteFile(m1, []byte("m"), 0o644)
	_ = os.WriteFile(m2, []byte("m"), 0o644)
	_, err = s.EnqueueImportPlan(library.ImportPlanPayload{
		Series: []library.ImportPlanSeriesDraft{{
			DraftKey: "local:a", Kind: library.ImportSeriesDraftBare, Title: "Same",
			RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
		}, {
			DraftKey: "local:b", Kind: library.ImportSeriesDraftBare, Title: "Same",
			RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
		}},
		Jobs: []library.ImportPlanJob{
			{Path: m1, SeriesDraftKey: "local:a", Title: "E1", UploadDate: "2020-01-01T00:00:00Z"},
			{Path: m2, SeriesDraftKey: "local:b", Title: "E2", UploadDate: "2020-01-02T00:00:00Z"},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate title reject")
	}
	_, err = s.EnqueueImportPlan(library.ImportPlanPayload{
		Series: []library.ImportPlanSeriesDraft{{
			DraftKey: "local:c", Kind: library.ImportSeriesDraftBare, Title: "One",
			RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
		}},
		Jobs: []library.ImportPlanJob{
			{Path: m1, SeriesDraftKey: "local:c", Title: "E1", RemoteID: "dup", UploadDate: "2020-01-01T00:00:00Z"},
			{Path: m2, SeriesDraftKey: "local:c", Title: "E2", RemoteID: "dup", UploadDate: "2020-01-02T00:00:00Z"},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate remote_id reject")
	}
}

func TestVideoSourceImportFilter(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	libRoot := filepath.Join(tmp, "library")
	_ = os.MkdirAll(libRoot, 0o755)
	s := library.NewStore(d, queue.NewStore(d))
	root, err := s.CreateRoot("lib", libRoot, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := s.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.SQL.Exec(`
		INSERT INTO videos (series_id, source_id, remote_id, title, status, special_feature)
		VALUES (?, NULL, '1', 'Imported', 'wanted', 'episode')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CountVideosWithNullSource(ser.ID)
	if err != nil || n != 1 {
		t.Fatalf("null count=%d err=%v", n, err)
	}
	list, err := s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{SourceID: library.VideoSourceImport}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d videos", len(list))
	}
}

func TestBuildImportPlanUnitsOrder(t *testing.T) {
	p := library.ImportPlanPayload{
		Series: []library.ImportPlanSeriesDraft{
			{DraftKey: "d1", Title: "A"},
			{DraftKey: "d2", Title: "B"},
		},
		Jobs: []library.ImportPlanJob{
			{SeriesDraftKey: "d2", Path: "/b"},
			{SeriesDraftKey: "d1", Path: "/a1"},
			{SeriesDraftKey: "d1", Path: "/a2"},
			{SeriesID: 9, Path: "/x"},
		},
	}
	units := library.BuildImportPlanUnits(p)
	if len(units) != 3 {
		b, _ := json.Marshal(units)
		t.Fatalf("units=%s", b)
	}
	if units[0].DraftKey != "d1" || len(units[0].Jobs) != 2 {
		t.Fatalf("unit0=%+v", units[0])
	}
	if units[1].DraftKey != "d2" || len(units[1].Jobs) != 1 {
		t.Fatalf("unit1=%+v", units[1])
	}
	if units[2].SeriesID != 9 || len(units[2].Jobs) != 1 {
		t.Fatalf("unit2=%+v", units[2])
	}
}
