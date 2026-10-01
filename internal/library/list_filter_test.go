package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestVideoListFilterPresenceAndGenre(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Filter Show", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@filtershow"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := seedTaskID(t, s)
	for _, li := range []library.ListedVideo{
		{RemoteID: "a", Title: "Has Genre", SourceID: src.ID, UploadDate: "2024-01-01T00:00:00Z"},
		{RemoteID: "b", Title: "No Genre", SourceID: src.ID, UploadDate: "2024-02-01T00:00:00Z"},
	} {
		if _, err := s.UpsertListed(ser.ID, li, taskID); err != nil {
			t.Fatal(err)
		}
	}
	vids, err := s.ListVideos(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	byRemote := map[string]int64{}
	for _, v := range vids {
		byRemote[v.RemoteID] = v.ID
	}
	if _, err := s.SaveVideoMetadata(byRemote["a"], library.SaveVideoMetadataParams{
		Title: "Has Genre", Genres: []string{"Comedy", "Drama"}, UploadDate: "2024-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveVideoMetadata(byRemote["b"], library.SaveVideoMetadataParams{
		Title: "No Genre", Plot: "plot text here", UploadDate: "2024-02-01",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{Genres: []string{"Comedy"}}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RemoteID != "a" {
		t.Fatalf("genre filter: %+v", got)
	}

	got, err = s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{Empty: []string{library.PresenceGenres}}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RemoteID != "b" {
		t.Fatalf("empty genres: %+v", got)
	}

	got, err = s.ListVideosPageFiltered(0, library.VideoListFilter{
		Title: "plot", QField: library.QFieldPlot, SeriesID: ser.ID,
	}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RemoteID != "b" {
		t.Fatalf("plot search library-wide: %+v", got)
	}

	got, err = s.ListVideosPageFiltered(0, library.VideoListFilter{Sort: library.SortAdded}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("library list: got %d", len(got))
	}
}

func TestNormalizeSortDir(t *testing.T) {
	t.Parallel()
	if got := library.NormalizeSortDir(library.SortTitle, ""); got != library.SortDirAsc {
		t.Fatalf("title default: %q", got)
	}
	if got := library.NormalizeSortDir(library.SortAdded, ""); got != library.SortDirDesc {
		t.Fatalf("added default: %q", got)
	}
	if got := library.NormalizeSortDir(library.SortTitle, "desc"); got != library.SortDirDesc {
		t.Fatalf("title desc: %q", got)
	}
	if got := library.NormalizeSortDir(library.SortUpload, "asc"); got != library.SortDirAsc {
		t.Fatalf("upload asc: %q", got)
	}
}

func TestOrderByClauseDirection(t *testing.T) {
	t.Parallel()
	// Exported via listing: ensure asc title and desc title differ by flipping.
	asc := library.NormalizeSortDir(library.SortTitle, library.SortDirAsc)
	desc := library.NormalizeSortDir(library.SortTitle, library.SortDirDesc)
	if asc == desc {
		t.Fatal("asc and desc should differ")
	}
	if library.DefaultSortDir(library.SortTitle) != library.SortDirAsc {
		t.Fatal("title default asc")
	}
	if library.DefaultSortDir(library.SortUpload) != library.SortDirDesc {
		t.Fatal("upload default desc")
	}
	if library.DefaultSortDir(library.SortDuration) != library.SortDirDesc {
		t.Fatal("duration default desc")
	}
	if library.DefaultSortDir(library.SortDownloaded) != library.SortDirDesc {
		t.Fatal("downloaded default desc")
	}
}

func TestSeriesListSortDownloadedAndLastUpload(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	few, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Few DL", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	many, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Many DL", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcFew, err := s.AddSource(few.ID, library.AddSourceParams{URL: "https://www.example.com/@fewdl"})
	if err != nil {
		t.Fatal(err)
	}
	srcMany, err := s.AddSource(many.ID, library.AddSourceParams{URL: "https://www.example.com/@manydl"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := seedTaskID(t, s)
	add := func(sid, sourceID int64, remote, upload string) int64 {
		t.Helper()
		res, err := s.UpsertListed(sid, library.ListedVideo{
			RemoteID: remote, Title: remote, SourceID: sourceID, UploadDate: upload,
		}, taskID)
		if err != nil {
			t.Fatal(err)
		}
		return res.VideoID
	}
	v1 := add(few.ID, srcFew.ID, "f1", "2024-01-01T00:00:00Z")
	_ = add(many.ID, srcMany.ID, "m1", "2023-01-01T00:00:00Z")
	v2 := add(many.ID, srcMany.ID, "m2", "2025-06-01T00:00:00Z")
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded' WHERE id IN (?, ?)`, v1, v2); err != nil {
		t.Fatal(err)
	}
	// many: 1 downloaded; few: 1 downloaded - bump many with another download
	v3 := add(many.ID, srcMany.ID, "m3", "2022-01-01T00:00:00Z")
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded' WHERE id = ?`, v3); err != nil {
		t.Fatal(err)
	}

	byDL, err := s.ListSeriesFiltered(library.SeriesListFilter{Sort: library.SortDownloaded}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(byDL) < 2 || byDL[0].ID != many.ID {
		t.Fatalf("downloaded desc: first=%v want Many DL", byDL)
	}

	byUpload, err := s.ListSeriesFiltered(library.SeriesListFilter{Sort: library.SortLastUpload}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(byUpload) < 2 || byUpload[0].ID != many.ID {
		t.Fatalf("last_upload desc: first=%v want Many DL (2025)", byUpload)
	}
}

func TestVideoListSortDuration(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "DurSort", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@dursort"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := seedTaskID(t, s)
	short, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "short", Title: "Short", SourceID: src.ID, UploadDate: "2024-01-01T00:00:00Z",
	}, taskID)
	if err != nil {
		t.Fatal(err)
	}
	long, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "long", Title: "Long", SourceID: src.ID, UploadDate: "2024-02-01T00:00:00Z",
	}, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDurationSeconds(short.VideoID, 30); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDurationSeconds(long.VideoID, 600); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{Sort: library.SortDuration}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].RemoteID != "long" {
		t.Fatalf("duration desc: %+v", got)
	}
}

func TestSeriesListFilterPresenceAndStudio(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	a, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Alpha", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Beta", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSeriesMetadata(a.ID, library.SaveSeriesMetadataParams{
		Studio: "Studio A", Genres: []string{"Comedy"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSeriesMetadata(b.ID, library.SaveSeriesMetadataParams{
		Plot: "has plot",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListSeriesFiltered(library.SeriesListFilter{Studio: "Studio A"}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("studio filter: %+v", got)
	}

	got, err = s.ListSeriesFiltered(library.SeriesListFilter{Empty: []string{library.PresenceGenres}}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("empty genres: %+v", got)
	}

	got, err = s.ListSeriesFiltered(library.SeriesListFilter{
		Title: "plot", QField: library.QFieldPlot, Sort: library.SortAdded,
	}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("plot search: %+v", got)
	}

	if !(library.SeriesListFilter{NotEmpty: []string{library.PresenceStudio}}).Active() {
		t.Fatal("not_empty should be active")
	}
	if (library.SeriesListFilter{Sort: library.SortAdded}).Active() {
		t.Fatal("sort alone should not be active")
	}
}

func TestDistinctVideoFacetsSeriesScoped(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Facet Show", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Other", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@facet"})
	if err != nil {
		t.Fatal(err)
	}
	src2, err := s.AddSource(other.ID, library.AddSourceParams{URL: "https://www.example.com/@other"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := seedTaskID(t, s)
	for _, li := range []library.ListedVideo{
		{RemoteID: "f1", Title: "One", SourceID: src.ID, UploadDate: "2024-01-01T00:00:00Z"},
		{RemoteID: "f2", Title: "Two", SourceID: src2.ID, UploadDate: "2023-01-01T00:00:00Z"},
	} {
		sid := ser.ID
		if li.SourceID == src2.ID {
			sid = other.ID
		}
		if _, err := s.UpsertListed(sid, li, taskID); err != nil {
			t.Fatal(err)
		}
	}
	vids, err := s.ListVideos(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(vids) != 1 {
		t.Fatalf("ser videos: %d", len(vids))
	}
	if _, err := s.SaveVideoMetadata(vids[0].ID, library.SaveVideoMetadataParams{
		Title: "One", Studio: "OnlyHere", UploadDate: "2024-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	scoped, err := s.DistinctVideoScalar(ser.ID, "studio")
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 1 || scoped[0] != "OnlyHere" {
		t.Fatalf("scoped studios: %+v", scoped)
	}
	wide, err := s.DistinctVideoScalar(0, "studio")
	if err != nil {
		t.Fatal(err)
	}
	if len(wide) < 1 {
		t.Fatalf("library studios empty")
	}
}
