package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestVideoListFilterPackRole(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "KindFilt", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@kindfilt"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := seedTaskID(t, s)
	for _, li := range []library.ListedVideo{
		{RemoteID: "ep", Title: "Regular", SourceID: src.ID, UploadDate: "2025-01-01T00:00:00Z"},
		{RemoteID: "sp", Title: "Special", SourceID: src.ID, UploadDate: "2025-01-02T00:00:00Z"},
		{RemoteID: "tr", Title: "Trailer", SourceID: src.ID, UploadDate: "2025-01-03T00:00:00Z"},
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
	if err := s.SetVideoPackRole(byRemote["sp"], library.PackRoleSpecialEpisode); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVideoPackRole(byRemote["tr"], "trailers"); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{PackRoles: []string{library.PackRoleRegular}}, 50, 0)
	if err != nil || len(got) != 1 || got[0].RemoteID != "ep" {
		t.Fatalf("regular: %+v err=%v", got, err)
	}
	got, err = s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{PackRoles: []string{library.VideoPackRoleAnySpecial}}, 50, 0)
	if err != nil || len(got) != 2 {
		t.Fatalf("any special: %+v err=%v", got, err)
	}
	got, err = s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{PackRoles: []string{library.PackRoleSpecialEpisode}}, 50, 0)
	if err != nil || len(got) != 1 || got[0].RemoteID != "sp" {
		t.Fatalf("special episode: %+v err=%v", got, err)
	}
	got, err = s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{PackRoles: []string{"trailers"}}, 50, 0)
	if err != nil || len(got) != 1 || got[0].RemoteID != "tr" {
		t.Fatalf("trailers: %+v err=%v", got, err)
	}
	n, err := s.CountVideosFiltered(ser.ID, library.VideoListFilter{PackRoles: []string{library.VideoPackRoleAnySpecial}})
	if err != nil || n != 2 {
		t.Fatalf("count any special=%d err=%v", n, err)
	}
}
