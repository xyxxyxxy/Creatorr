package library_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

	_, err = s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
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
		Categories: []string{"Education", " Tech "},
	})
	if d.Title != "T" || d.Plot != "P" || d.UniqueIDValue != "abc" || d.UniqueIDType != "yt-dlp" {
		t.Fatalf("%+v", d)
	}
	if d.SortTitle != "" || d.OriginalTitle != "" {
		t.Fatalf("prefetch must not copy title into sort/original: %+v", d)
	}
	if len(d.Genres) != 2 || d.Genres[0] != "Education" || d.Genres[1] != "Tech" {
		t.Fatalf("genres=%v", d.Genres)
	}
}

func TestSaveVideoMetadataInstallsAndClearsThumb(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ThumbEp", SourceURL: "https://www.example.com/@thumbep",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "th1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=th1",
		UploadDate: "2024-02-01", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root.Path, "ThumbEp")
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
	src := filepath.Join(t.TempDir(), "up.jpg")
	if err := os.WriteFile(src, []byte("JPG"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "Ep", ThumbSrc: src,
	}); err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(media, filepath.Ext(media)) + "-thumb.jpg"
	if _, err := os.Stat(want); err != nil {
		t.Fatal("thumb missing after install", err)
	}
	path, ok, err := s.VideoThumbPath(res.VideoID)
	if err != nil || !ok || path != want {
		t.Fatalf("VideoThumbPath path=%q ok=%v err=%v", path, ok, err)
	}
	if _, err := s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "Ep", ThumbClear: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("thumb should be cleared")
	}
	if _, ok, err := s.VideoThumbPath(res.VideoID); err != nil || ok {
		t.Fatalf("thumb row should be gone ok=%v err=%v", ok, err)
	}
}

func TestSaveVideoMetadataThumbRequiresPack(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "NoPack", SourceURL: "https://www.example.com/@nopack",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "np1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=np1",
		UploadDate: "2024-02-01", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "up.jpg")
	if err := os.WriteFile(src, []byte("JPG"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "Ep", ThumbSrc: src,
	})
	if err == nil {
		t.Fatal("expected error without pack")
	}
}

func TestPersistVideoPrefetchThumb(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("THUMB"))
	}))
	defer srv.Close()

	draft := library.VideoPrefetchDraft{ThumbnailURL: srv.URL + "/t.jpg"}
	s.PersistVideoPrefetchThumb(42, 7, &draft)
	path := draft.ArtFiles[library.ArtThumb]
	if path == "" {
		t.Fatal("expected art file path")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "THUMB" {
		t.Fatalf("body=%q", body)
	}
	if err := s.ClearVideoPrefetchDraft(42, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("clear should remove art file")
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
	if _, err := s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
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
	if _, err := s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
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

func TestSaveVideoMetadataUploadDateReindexesAndRenames(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "DateEdit", SourceURL: "https://www.example.com/@dateedit",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "d1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=d1",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seriesDir := filepath.Join(root.Path, "DateEdit")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldMedia := filepath.Join(seriesDir, "old.mkv")
	if err := os.WriteFile(oldMedia, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded' WHERE id = ?`, res.VideoID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, datetime('now'))`, res.VideoID, oldMedia); err != nil {
		t.Fatal(err)
	}

	_, err = s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "Ep", UploadDate: "2024-03-15",
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.UploadDate.Valid || library.UploadCalendarDate(v.UploadDate.String) != "2024-03-15" {
		t.Fatalf("upload_date=%v", v.UploadDate)
	}
	if !v.Season.Valid || int(v.Season.Int64) != 2024 {
		t.Fatalf("season=%v", v.Season)
	}
	if !v.Episode.Valid || int(v.Episode.Int64) != 31500 {
		t.Fatalf("episode=%v want 31500", v.Episode)
	}
	var newPath string
	_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, res.VideoID).Scan(&newPath)
	if newPath == "" || newPath == oldMedia {
		t.Fatalf("expected rename, path=%q", newPath)
	}
	if !strings.Contains(newPath, "S2024") {
		t.Fatalf("path should nest under S2024: %q", newPath)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatal(err)
	}
}

func TestSaveVideoMetadataClearsUploadDate(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ClearDate", SourceURL: "https://www.example.com/@cleardate",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "c1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=c1",
		UploadDate: "2024-03-15T12:00:00Z", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	v0, _ := s.GetVideo(res.VideoID)
	seriesDir := filepath.Join(root.Path, "ClearDate")
	seasonDir := filepath.Join(seriesDir, "S2024")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stem := fmt.Sprintf("S2024E%06d [c1]", int(v0.Episode.Int64))
	oldMedia := filepath.Join(seasonDir, stem+".mkv")
	if err := os.WriteFile(oldMedia, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded' WHERE id = ?`, res.VideoID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, datetime('now'))`, res.VideoID, oldMedia); err != nil {
		t.Fatal(err)
	}

	_, err = s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "Ep", UploadDate: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.UploadDate.Valid {
		t.Fatalf("upload_date should clear, got %v", v.UploadDate)
	}
	if v.Season.Valid || v.Episode.Valid {
		t.Fatalf("season/episode should clear, season=%v episode=%v", v.Season, v.Episode)
	}
	var newPath string
	_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, res.VideoID).Scan(&newPath)
	if newPath == "" || newPath == oldMedia {
		t.Fatalf("expected rename toward undated path, path=%q", newPath)
	}
	if !strings.Contains(newPath, "S0000") {
		t.Fatalf("path should nest under S0000: %q", newPath)
	}
}

func TestSaveVideoMetadataUploadDateWithTime(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "TimeEdit", SourceURL: "https://www.example.com/@timeedit",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "t1", Title: "Ep", SourceID: ser.Sources[0].ID,
		UploadDate: "2024-03-15T08:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "Ep", UploadDate: "2024-03-15T18:30",
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.UploadDate.Valid || v.UploadDate.String != "2024-03-15T18:30:00Z" {
		t.Fatalf("upload_date=%q want 2024-03-15T18:30:00Z", v.UploadDate.String)
	}
	// Date-only same day preserves the explicit time.
	_, err = s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "Ep", UploadDate: "2024-03-15",
	})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if !v2.UploadDate.Valid || v2.UploadDate.String != "2024-03-15T18:30:00Z" {
		t.Fatalf("date-only same day should keep time, got %q", v2.UploadDate.String)
	}
}

func TestUploadFormValue(t *testing.T) {
	if got := library.UploadFormValue("2024-03-15T00:00:00Z"); got != "2024-03-15" {
		t.Fatalf("midnight=%q", got)
	}
	if got := library.UploadFormValue("2024-03-15T18:30:00Z"); got != "2024-03-15T18:30" {
		t.Fatalf("timed=%q", got)
	}
	day, clock := library.UploadFormParts("2024-03-15T18:30:00Z")
	if day != "2024-03-15" || clock != "18:30" {
		t.Fatalf("parts day=%q clock=%q", day, clock)
	}
	day, clock = library.UploadFormParts("2024-03-15T00:00:00Z")
	if day != "2024-03-15" || clock != "" {
		t.Fatalf("midnight parts day=%q clock=%q", day, clock)
	}
	if got := library.CombineUploadFormDateTime("2024-03-15", "18:30"); got != "2024-03-15T18:30" {
		t.Fatalf("combine=%q", got)
	}
	if got := library.CombineUploadFormDateTime("2024-03-15", ""); got != "2024-03-15" {
		t.Fatalf("combine date-only=%q", got)
	}
	if got := library.CombineUploadFormDateTime("", "18:30"); got != "" {
		t.Fatalf("combine empty day=%q", got)
	}
}

