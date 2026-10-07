package library_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/testutil"
)

func TestListCatalogValuesCaseFoldAndCounts(t *testing.T) {
	_, _, s := testutil.OpenStores(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "CatA", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSeriesMetadata(ser.ID, library.SaveSeriesMetadataParams{Tags: []string{"News", "Politics"}}); err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "ep", WebpageURL: "https://example.com/v1",
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "ep", Tags: []string{"news", "Drama"},
	})
	if err != nil {
		t.Fatal(err)
	}
	vals, err := s.ListCatalogValues(library.CatalogFieldTags)
	if err != nil {
		t.Fatal(err)
	}
	var news *library.CatalogValue
	for i := range vals {
		if vals[i].Value == "News" || vals[i].Value == "news" {
			news = &vals[i]
			break
		}
	}
	if news == nil {
		t.Fatalf("news missing: %+v", vals)
	}
	if news.SeriesCount != 1 || news.VideoCount != 1 {
		t.Fatalf("counts series=%d videos=%d value=%q", news.SeriesCount, news.VideoCount, news.Value)
	}
}

func TestCatalogRemoveAndBlockSoftFill(t *testing.T) {
	d, q, s := testutil.OpenStores(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "CatB", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSeriesMetadata(ser.ID, library.SaveSeriesMetadataParams{Tags: []string{"tea"}}); err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v2", Title: "ep2", WebpageURL: "https://example.com/v2",
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	vid := res.VideoID
	_, err = s.SaveVideoMetadata(vid, library.SaveVideoMetadataParams{Title: "ep2", Tags: []string{"tea", "keep"}})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := s.EnqueueRewriteCatalogMeta(library.RewriteCatalogMetaParams{
		Field: library.CatalogFieldTags,
		Op:    library.CatalogOpRemoveBlock,
		From:  "Tea",
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked, _ := settings.IsSoftFillBlocked(d, settings.CatalogFieldTags, "tea")
	if !blocked {
		t.Fatal("expected SoftFill block before task")
	}
	task, err := q.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RewriteCatalogMetaPass(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(vid)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Tags) != 1 || v.Tags[0] != "keep" {
		t.Fatalf("video tags=%v", v.Tags)
	}
	ok, err := s.EnsureVideoTagsFromInfo(vid, []string{"tea", "newtag"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected softfill of newtag")
	}
	v, _ = s.GetVideo(vid)
	for _, tag := range v.Tags {
		if tag == "tea" || tag == "Tea" {
			t.Fatalf("blocked tag returned: %v", v.Tags)
		}
	}
}

func TestSoftFillTagsToggleOff(t *testing.T) {
	d, _, s := testutil.OpenStores(t)
	if err := settings.Set(d, settings.KeySoftFillTags, "0"); err != nil {
		t.Fatal(err)
	}
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "CatC", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v3", Title: "ep3", WebpageURL: "https://example.com/v3",
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.EnsureVideoTagsFromInfo(res.VideoID, []string{"junk"})
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestSoftFillDomainTagToggleOff(t *testing.T) {
	d, _, s := testutil.OpenStores(t)
	if err := settings.Set(d, settings.KeySoftFillDomainTag, "0"); err != nil {
		t.Fatal(err)
	}
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "NoDomain",
		SourceURL:        "https://www.example.com/@x",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ser.Sources[0].Tags) != 0 {
		t.Fatalf("expected no domain seed, got %v", ser.Sources[0].Tags)
	}
}

func TestResetFromInfoRespectsSoftFillToggle(t *testing.T) {
	d, _, s := testutil.OpenStores(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ResetSoft", SourceURL: "https://www.example.com/@rs",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vr", Title: "old", WebpageURL: "https://example.com/vr",
		SourceID: ser.Sources[0].ID,
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	vid := res.VideoID
	_, err = s.SaveVideoMetadata(vid, library.SaveVideoMetadataParams{Title: "old", Tags: []string{"keep-me"}})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root.Path, "ResetSoft")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "ep.mkv")
	info := filepath.Join(dir, "ep.info.json")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(info, []byte(`{"id":"vr","title":"from-info","tags":["site-tag"],"categories":["News"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))`, vid, media); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'json', datetime('now'))`, vid, info); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(d, settings.KeySoftFillTags, "0"); err != nil {
		t.Fatal(err)
	}
	out, err := s.ResetVideoMetadataFromInfoJSON(vid, 0)
	if err != nil || out.Skipped {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	v, err := s.GetVideo(vid)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Tags) != 1 || v.Tags[0] != "keep-me" {
		t.Fatalf("tags=%v want keep-me (SoftFill tags off)", v.Tags)
	}
	if v.Title != "from-info" {
		t.Fatalf("title=%q", v.Title)
	}
}

func TestRewriteCatalogMetaBusyConflict(t *testing.T) {
	_, q, s := testutil.OpenStores(t)
	_, err := q.Enqueue(queue.EnqueueParams{
		Origin:  queue.OriginManual,
		Kind:    queue.KindRewriteCatalogMeta,
		Domain:  queue.SystemDomain,
		Payload: map[string]any{"field": "tags", "op": "remove", "from": "x"},
		Message: "busy",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.EnqueueRewriteCatalogMeta(library.RewriteCatalogMetaParams{
		Field: library.CatalogFieldTags, Op: library.CatalogOpRemove, From: "y",
	})
	if err == nil {
		t.Fatal("expected conflict")
	}
}
