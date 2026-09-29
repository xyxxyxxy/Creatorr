package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestSpecialDisplayPlacementAndReindex(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@show",
	})
	if err != nil {
		t.Fatal(err)
	}
	r1, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "a", Title: "A", SourceID: src.ID,
		UploadDate: "2026-01-10T00:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "b", Title: "B", SourceID: src.ID,
		UploadDate: "2026-06-10T00:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReindexSeriesUTCYear(ser.ID, 2026); err != nil {
		t.Fatal(err)
	}
	sp, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "sp", Title: "Special", SourceID: src.ID,
		UploadDate: "2026-03-01T00:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVideoPackRole(sp.VideoID, library.PackRoleSpecialEpisode); err != nil {
		t.Fatal(err)
	}
	spv, err := s.GetVideo(sp.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if !spv.Season.Valid || spv.Season.Int64 != 0 || !spv.Episode.Valid || spv.Episode.Int64 != 1 {
		t.Fatalf("special S/E=%v/%v", spv.Season, spv.Episode)
	}
	meta := library.EpisodeMetaFromVideo(spv, "Show", 0, 1, spv.UploadDate.String, 0)
	if err := s.ApplySpecialDisplay(&meta, ser.ID, spv.ID, spv.UploadDate.String); err != nil {
		t.Fatal(err)
	}
	r2v, err := s.GetVideo(r2.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.DisplayEpisodeSet || meta.DisplaySeason != int(r2v.Season.Int64) || meta.DisplayEpisode != int(r2v.Episode.Int64) {
		t.Fatalf("display=%d/%d set=%v want before r2 S/E %d/%d",
			meta.DisplaySeason, meta.DisplayEpisode, meta.DisplayEpisodeSet,
			r2v.Season.Int64, r2v.Episode.Int64)
	}
	_ = r1
}

func TestPruneEmptyReservedFolder(t *testing.T) {
	root := t.TempDir()
	series := filepath.Join(root, "Show")
	specials := filepath.Join(series, "Specials")
	trailers := filepath.Join(series, "trailers")
	nested := filepath.Join(series, "S2026", "trailers")
	if err := os.MkdirAll(specials, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trailers, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if !library.PruneEmptyReservedFolder(series, specials) {
		t.Fatal("expected prune Specials")
	}
	if !library.PruneEmptyReservedFolder(series, trailers) {
		t.Fatal("expected prune trailers")
	}
	if library.PruneEmptyReservedFolder(series, nested) {
		t.Fatal("must not prune season-nested trailers")
	}
	if _, err := os.Stat(specials); !os.IsNotExist(err) {
		t.Fatal("Specials still present")
	}
}

func TestSpecialDisplayAfterAllRegulars(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "AfterAll", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@aa"})
	if err != nil {
		t.Fatal(err)
	}
	r1, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "a", Title: "A", SourceID: src.ID,
		UploadDate: "2026-01-10T00:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReindexSeriesUTCYear(ser.ID, 2026); err != nil {
		t.Fatal(err)
	}
	sp, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "sp", Title: "Late Special", SourceID: src.ID,
		UploadDate: "2026-12-31T00:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVideoPackRole(sp.VideoID, library.PackRoleSpecialEpisode); err != nil {
		t.Fatal(err)
	}
	spv, _ := s.GetVideo(sp.VideoID)
	meta := library.EpisodeMetaFromVideo(spv, "AfterAll", 0, 1, spv.UploadDate.String, 0)
	if err := s.ApplySpecialDisplay(&meta, ser.ID, spv.ID, spv.UploadDate.String); err != nil {
		t.Fatal(err)
	}
	r1v, _ := s.GetVideo(r1.VideoID)
	if meta.DisplayEpisodeSet {
		t.Fatal("after-all must omit displayepisode")
	}
	if meta.DisplaySeason != int(r1v.Season.Int64) {
		t.Fatalf("displayseason=%d want %d", meta.DisplaySeason, r1v.Season.Int64)
	}
}

func TestReindexYearExcludesSpecials(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Iso", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@iso"})
	if err != nil {
		t.Fatal(err)
	}
	r1, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "a", Title: "A", SourceID: src.ID, UploadDate: "2026-01-01T00:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "sp", Title: "Sp", SourceID: src.ID, UploadDate: "2026-02-01T00:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVideoPackRole(sp.VideoID, library.PackRoleSpecialEpisode); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReindexSeriesUTCYear(ser.ID, 2026); err != nil {
		t.Fatal(err)
	}
	rv, _ := s.GetVideo(r1.VideoID)
	spv, _ := s.GetVideo(sp.VideoID)
	if int(rv.Episode.Int64) != 1 {
		t.Fatalf("regular episode=%v want 1 (special excluded)", rv.Episode)
	}
	if !spv.Season.Valid || spv.Season.Int64 != 0 {
		t.Fatalf("special season=%v", spv.Season)
	}
}
