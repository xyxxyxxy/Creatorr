package library_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func TestFormatEpisodeNFOExtraFields(t *testing.T) {
	b := library.FormatEpisodeNFO(library.EpisodeNFO{
		Title: "Ep", SeriesTitle: "Show", Season: 2024, Episode: 1,
		Plot: "Hello", SortTitle: "Ep, The", OriginalTitle: "Orig",
		Tagline: "Tag", Studio: "StudioX", Genres: []string{"Tech"}, Tags: []string{"News"},
		Country: "US", MPAA: "TV-14", Aired: "2024-06-01",
		UniqueIDType: "site", UniqueID: "abc",
		Actors:         []library.SeriesActor{{Name: "Host", Role: "Creator"}},
		RuntimeSeconds: 125,
	})
	s := string(b)
	for _, want := range []string{
		"<sorttitle>Ep, The</sorttitle>",
		"<originaltitle>Orig</originaltitle>",
		"<tagline>Tag</tagline>",
		"<studio>StudioX</studio>",
		"<genre>Tech</genre>",
		"<tag>News</tag>",
		"<country>US</country>",
		"<mpaa>TV-14</mpaa>",
		`type="site"`,
		"<name>Host</name>",
		"<durationinseconds>125</durationinseconds>",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
}

func TestFormatEpisodeNFOOmitsTitleDupSortOriginal(t *testing.T) {
	b := library.FormatEpisodeNFO(library.EpisodeNFO{
		Title: "Same Title", SortTitle: "Same Title", OriginalTitle: "Same Title",
		Season: 1, Episode: 1,
	})
	s := string(b)
	if strings.Contains(s, "<sorttitle>") || strings.Contains(s, "<originaltitle>") {
		t.Fatalf("expected omit sort/original when equal title:\n%s", s)
	}
	b2 := library.FormatEpisodeNFO(library.EpisodeNFO{
		Title: "Same Title", SortTitle: "  Same Title  ", OriginalTitle: "Other",
		Season: 1, Episode: 1,
	})
	s2 := string(b2)
	if strings.Contains(s2, "<sorttitle>") {
		t.Fatalf("trimmed equal sorttitle should omit:\n%s", s2)
	}
	if !strings.Contains(s2, "<originaltitle>Other</originaltitle>") {
		t.Fatalf("distinct originaltitle should remain:\n%s", s2)
	}
}

func TestSaveVideoMetadataRewritesNFO(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "MetaEp", SourceURL: "https://www.example.com/@metaep",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "m1", Title: "Old Title", Description: "old plot",
		WebpageURL: "https://www.example.com/watch?v=m1", UploadDate: "2024-02-01",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root.Path, "MetaEp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "ep.mkv")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))`, res.VideoID, media); err != nil {
		t.Fatal(err)
	}

	err = s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "New Title", Plot: "new plot", Studio: "StudioY",
		UniqueIDType: "site", UniqueIDValue: "m1",
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "New Title" || v.Description != "new plot" || v.Studio != "StudioY" {
		t.Fatalf("db fields title=%q plot=%q studio=%q", v.Title, v.Description, v.Studio)
	}
	nfo := strings.TrimSuffix(media, filepath.Ext(media)) + ".nfo"
	body, err := os.ReadFile(nfo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "New Title") || !strings.Contains(string(body), "new plot") {
		t.Fatalf("nfo body=%s", body)
	}
	if _, err := os.Stat(media); err != nil {
		t.Fatal("media should remain", err)
	}
}

func TestBuildVideoPrefetchDraftFromEntry(t *testing.T) {
	d := library.BuildVideoPrefetchDraftFromEntry(ytdlp.Entry{
		ID: "abc", Title: "T", Description: "P", ThumbnailURL: "https://example.com/t.jpg",
	})
	if d.Title != "T" || d.Plot != "P" || d.UniqueIDValue != "abc" || d.UniqueIDType != "yt-dlp" {
		t.Fatalf("%+v", d)
	}
	if d.SortTitle != "" || d.OriginalTitle != "" {
		t.Fatalf("prefetch must not copy title into sort/original: %+v", d)
	}
}

func TestSaveVideoMetadataClearsTitleDupSortOriginal(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "DupEp", SourceURL: "https://www.example.com/@dupep",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "d1", Title: "Same", WebpageURL: "https://www.example.com/watch?v=d1",
		UploadDate: "2024-02-01", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root.Path, "DupEp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "ep.mkv")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))`, res.VideoID, media); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "Same", SortTitle: "Same", OriginalTitle: "Same",
	}); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.SortTitle != "" || v.OriginalTitle != "" {
		t.Fatalf("want cleared sort/original, got sort=%q orig=%q", v.SortTitle, v.OriginalTitle)
	}
	nfo := strings.TrimSuffix(media, filepath.Ext(media)) + ".nfo"
	body, err := os.ReadFile(nfo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "<sorttitle>") || strings.Contains(string(body), "<originaltitle>") {
		t.Fatalf("nfo should omit dups:\n%s", body)
	}
}

func TestListMetaSuggestionsPoolsSeriesAndVideos(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "SugShow", SourceURL: "https://www.example.com/@sugshow",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSeriesMetadata(ser.ID, library.SaveSeriesMetadataParams{
		Studio: "SeriesStudio", Genres: []string{"Tech"}, Tags: []string{"SeriesTag"},
		Country: "US", MPAA: "TV-PG",
		Actors: []library.SeriesActor{{Name: "SeriesHost", Role: "Host"}},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "sug1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=sug1",
		UploadDate: "2024-01-01", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "Ep", Studio: "VideoStudio", Genres: []string{"News"}, Tags: []string{"VideoTag"},
		Country: "CA", MPAA: "TV-14",
		Actors: []library.SeriesActor{{Name: "VideoGuest", Role: "Guest"}},
	}); err != nil {
		t.Fatal(err)
	}
	sug, err := s.ListMetaSuggestions()
	if err != nil {
		t.Fatal(err)
	}
	has := func(list []string, want string) bool {
		for _, v := range list {
			if v == want {
				return true
			}
		}
		return false
	}
	for _, check := range []struct {
		label string
		list  []string
		want  string
	}{
		{"studio", sug.Studios, "SeriesStudio"},
		{"studio", sug.Studios, "VideoStudio"},
		{"genre", sug.Genres, "Tech"},
		{"genre", sug.Genres, "News"},
		{"tag", sug.Tags, "SeriesTag"},
		{"tag", sug.Tags, "VideoTag"},
		{"country", sug.Countries, "US"},
		{"country", sug.Countries, "CA"},
		{"mpaa", sug.MPAAs, "TV-PG"},
		{"mpaa", sug.MPAAs, "TV-14"},
		{"actor", sug.ActorNames, "SeriesHost"},
		{"actor", sug.ActorNames, "VideoGuest"},
		{"role", sug.ActorRoles, "Host"},
		{"role", sug.ActorRoles, "Guest"},
	} {
		if !has(check.list, check.want) {
			t.Fatalf("missing %s %q in %v", check.label, check.want, check.list)
		}
	}
}

