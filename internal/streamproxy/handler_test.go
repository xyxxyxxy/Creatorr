package streamproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/sponsorblock"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func openStreamLib(t *testing.T) *library.Store {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)
	q := queue.NewStore(d)
	s := library.NewStore(d, q)
	s.PublicBaseURL = "http://creatorr.example.com:8787"
	return s
}

func mountHandler(t *testing.T, lib *library.Store) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	h := &Handler{Library: lib, YtDlp: &ytdlp.Client{Bin: filepath.Join(t.TempDir(), "missing")}}
	h.Mount(r)
	return r
}

func TestServeVideoUnauthorized(t *testing.T) {
	lib := openStreamLib(t)
	h := mountHandler(t, lib)

	req := httptest.NewRequest(http.MethodGet, "/stream/videos/1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/stream/videos/1?token=bad", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status=%d", rec.Code)
	}
}

func TestServeVideoInvalidID(t *testing.T) {
	lib := openStreamLib(t)
	tok, err := library.EnsureStreamToken(lib.DB)
	if err != nil {
		t.Fatal(err)
	}
	h := mountHandler(t, lib)

	for _, path := range []string{
		"/stream/videos/abc?token=" + tok,
		"/stream/videos/0?token=" + tok,
		"/stream/videos/99999?token=" + tok,
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestProxyProgressiveForwardsRange(t *testing.T) {
	var gotRange string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-99/1000")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("segment"))
	}))
	t.Cleanup(upstream.Close)

	req := httptest.NewRequest(http.MethodGet, "/stream/videos/1?token=x", nil)
	req.Header.Set("Range", "bytes=0-99")
	rec := httptest.NewRecorder()
	if err := proxyProgressive(rec, req, upstream.URL, map[string]string{"User-Agent": "test"}); err != nil {
		t.Fatal(err)
	}
	if gotRange != "bytes=0-99" {
		t.Fatalf("upstream Range=%q", gotRange)
	}
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status=%d", rec.Code)
	}
	if rec.Header().Get("Content-Range") != "bytes 0-99/1000" {
		t.Fatalf("Content-Range=%q", rec.Header().Get("Content-Range"))
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "segment" {
		t.Fatalf("body=%q", body)
	}
}

func TestRewriteHLSPlaylist(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000
https://cdn.example.com/720.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=400000
rel/360.m3u8
`
	baseProxy := "/stream/videos/1/hls?token=tok&u="
	got := string(rewriteHLSPlaylist([]byte(master), "https://cdn.example.com/master.m3u8", baseProxy))
	if !strings.Contains(got, baseProxy) {
		t.Fatalf("missing proxy prefix: %s", got)
	}
	if strings.Contains(got, "https://cdn.example.com/720.m3u8\n") {
		t.Fatalf("absolute URI not rewritten: %s", got)
	}
	if strings.Contains(got, "\nrel/360.m3u8\n") {
		t.Fatalf("relative URI not rewritten: %s", got)
	}
	enc720 := encodeUpstream("https://cdn.example.com/720.m3u8")
	if !strings.Contains(got, enc720) {
		t.Fatalf("missing encoded 720: %s", got)
	}
	dec, err := decodeUpstream(enc720)
	if err != nil || dec != "https://cdn.example.com/720.m3u8" {
		t.Fatalf("roundtrip %q %v", dec, err)
	}
}

func TestServeVideoHead(t *testing.T) {
	t.Setenv("CREATORR_FAKE_URLS_KIND", "pipe")
	lib := openStreamLib(t)
	root, err := lib.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Head", SourceURL: "https://www.example.com/@head",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
		DeliveryMode: library.DeliveryStream,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := lib.DB.SQL.Exec(`
		INSERT INTO videos (series_id, source_id, remote_id, title, source_url, status)
		VALUES (?, ?, 'v1', 'Ep', 'https://www.example.com/watch?v=v1', 'streamable')`,
		ser.ID, ser.Sources[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	vid, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	tok, err := library.EnsureStreamToken(lib.DB)
	if err != nil {
		t.Fatal(err)
	}
	h := mountHandler(t, lib)
	for _, path := range []string{
		"/stream/videos/" + strconv.FormatInt(vid, 10) + "?token=" + tok,
		"/stream/videos/" + strconv.FormatInt(vid, 10) + "/master.m3u8?token=" + tok,
	} {
		req := httptest.NewRequest(http.MethodHead, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
			t.Fatalf("%s Content-Type=%q", path, ct)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("%s HEAD wrote body", path)
		}
	}
}

func TestServeVideoPipe(t *testing.T) {
	t.Setenv("CREATORR_FAKE_URLS_KIND", "pipe")
	lib := openStreamLib(t)
	root, err := lib.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Pipe", SourceURL: "https://www.example.com/@pipe",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
		DeliveryMode: library.DeliveryStream,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := lib.DB.SQL.Exec(`
		INSERT INTO videos (series_id, source_id, remote_id, title, source_url, status)
		VALUES (?, ?, 'v1', 'Ep', 'https://www.example.com/watch?v=v1', 'streamable')`,
		ser.ID, ser.Sources[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	vid, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	tok, err := library.EnsureStreamToken(lib.DB)
	if err != nil {
		t.Fatal(err)
	}
	h := mountHandler(t, lib)
	req := httptest.NewRequest(http.MethodGet, "/stream/videos/"+strconv.FormatInt(vid, 10)+"?token="+tok, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Fatalf("Content-Type=%q body=%s", ct, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "#EXT-X-STREAM-INF") || !strings.Contains(body, "/hls/local/") || !strings.Contains(body, "index.m3u8?token=") {
		t.Fatalf("master playlist=%q", body)
	}
	// Follow master → media playlist, then segment.
	var mediaPath string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "index.m3u8") && !strings.HasPrefix(line, "#") {
			mediaPath = line
			break
		}
	}
	if mediaPath == "" {
		t.Fatalf("no media URI in %q", body)
	}
	if u, err := url.Parse(mediaPath); err == nil && u.IsAbs() {
		mediaPath = u.RequestURI()
	}
	reqM := httptest.NewRequest(http.MethodGet, mediaPath, nil)
	recM := httptest.NewRecorder()
	h.ServeHTTP(recM, reqM)
	if recM.Code != http.StatusOK {
		t.Fatalf("media status=%d body=%s", recM.Code, recM.Body.String())
	}
	mediaBody := recM.Body.String()
	if strings.Contains(mediaBody, "&sid=") || strings.Contains(mediaBody, "&f=") {
		t.Fatalf("media URI still uses multi-query form: %q", mediaBody)
	}
	if !strings.Contains(mediaBody, "seg00000.ts") {
		t.Fatalf("expected .ts segment in %q", mediaBody)
	}
	var segPath string
	for _, line := range strings.Split(mediaBody, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "seg00000.ts") && !strings.HasPrefix(line, "#") {
			segPath = line
			break
		}
	}
	if segPath == "" {
		t.Fatalf("no segment URI in %q", mediaBody)
	}
	if u, err := url.Parse(segPath); err == nil && u.IsAbs() {
		segPath = u.RequestURI()
	}
	req2 := httptest.NewRequest(http.MethodGet, segPath, nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK || rec2.Body.String() != "seg" {
		t.Fatalf("segment status=%d body=%q", rec2.Code, rec2.Body.String())
	}
}

func TestServeVideoPipeFallsBackToMatroska(t *testing.T) {
	t.Setenv("CREATORR_FAKE_URLS_KIND", "pipe")
	t.Setenv("CREATORR_FAKE_HLS_FAIL", "1")
	oldWait := hlsPlaylistWait
	hlsPlaylistWait = 400 * time.Millisecond
	t.Cleanup(func() { hlsPlaylistWait = oldWait })

	lib := openStreamLib(t)
	root, err := lib.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "PipeFB", SourceURL: "https://www.example.com/@pipefb",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
		DeliveryMode: library.DeliveryStream,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := lib.DB.SQL.Exec(`
		INSERT INTO videos (series_id, source_id, remote_id, title, source_url, status)
		VALUES (?, ?, 'v1', 'Ep', 'https://www.example.com/watch?v=v1', 'streamable')`,
		ser.ID, ser.Sources[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	vid, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	tok, err := library.EnsureStreamToken(lib.DB)
	if err != nil {
		t.Fatal(err)
	}
	h := mountHandler(t, lib)
	req := httptest.NewRequest(http.MethodGet, "/stream/videos/"+strconv.FormatInt(vid, 10)+"?token="+tok, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/x-matroska" {
		t.Fatalf("Content-Type=%q body=%s", ct, rec.Body.String())
	}
	if rec.Body.String() != "fake-matroska" {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestRewriteLocalHLSPlaylist(t *testing.T) {
	in := `#EXTM3U
#EXTINF:2.0,
seg00000.ts
`
	got := string(rewriteLocalHLSPlaylist([]byte(in), "http://creatorr.example.com:8787/stream/videos/1/hls/local/s/", "t", 0))
	if !strings.Contains(got, "http://creatorr.example.com:8787/stream/videos/1/hls/local/s/seg00000.ts?token=t") {
		t.Fatalf("seg rewrite: %s", got)
	}
	if !strings.Contains(got, "#EXT-X-PLAYLIST-TYPE:VOD") || !strings.Contains(got, "#EXT-X-START:TIME-OFFSET=0") {
		t.Fatalf("missing VOD/START: %s", got)
	}
	if strings.Contains(got, "ENDLIST") {
		t.Fatalf("no pad without duration: %s", got)
	}
	if strings.Contains(got, "&") {
		t.Fatalf("unexpected ampersand in media URI: %s", got)
	}
}

func TestRewriteLocalHLSPlaylistStripsEVENT(t *testing.T) {
	in := `#EXTM3U
#EXT-X-TARGETDURATION:4
#EXT-X-PLAYLIST-TYPE:EVENT
#EXTINF:2.0,
seg00000.ts
`
	got := string(rewriteLocalHLSPlaylist([]byte(in), "/stream/videos/1/hls/local/s/", "t", 0))
	if strings.Contains(got, "EVENT") {
		t.Fatalf("EVENT leaked: %s", got)
	}
	if !strings.Contains(got, "#EXT-X-PLAYLIST-TYPE:VOD") {
		t.Fatalf("missing VOD type: %s", got)
	}
	if !strings.Contains(got, "#EXT-X-START:TIME-OFFSET=0") {
		t.Fatalf("missing START: %s", got)
	}
	vodAt := strings.Index(got, "#EXT-X-PLAYLIST-TYPE:VOD")
	infAt := strings.Index(got, "#EXTINF:")
	uriAt := strings.Index(got, "seg00000.ts?token=t")
	if vodAt < 0 || infAt < 0 || uriAt < 0 || !(vodAt < infAt && infAt < uriAt) {
		t.Fatalf("bad header order: %s", got)
	}
	between := got[infAt:uriAt]
	if strings.Contains(between, "PLAYLIST-TYPE") || strings.Contains(between, "EXT-X-START") {
		t.Fatalf("tags between EXTINF and URI: %s", got)
	}
	if strings.Contains(got, "seg00001.ts") {
		t.Fatalf("must not invent future segments without duration: %s", got)
	}
	if strings.Contains(got, "ENDLIST") {
		t.Fatalf("must not force ENDLIST without duration: %s", got)
	}
}

func TestRewriteLocalHLSPlaylistPadsDuration(t *testing.T) {
	in := `#EXTM3U
#EXT-X-TARGETDURATION:4
#EXT-X-PLAYLIST-TYPE:EVENT
#EXTINF:2.0,
seg00000.ts
`
	got := string(rewriteLocalHLSPlaylist([]byte(in), "/stream/videos/1/hls/local/s/", "t", 10))
	if !strings.Contains(got, "#EXT-X-ENDLIST") {
		t.Fatalf("missing ENDLIST: %s", got)
	}
	if !strings.Contains(got, "seg00001.ts?token=t") || !strings.Contains(got, "seg00002.ts?token=t") {
		t.Fatalf("missing padded segs: %s", got)
	}
	if strings.Contains(got, "EVENT") {
		t.Fatalf("EVENT leaked: %s", got)
	}
	sum := 0.0
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "#EXTINF:") {
			sum += extinfSeconds(strings.TrimPrefix(line, "#EXTINF:"))
		}
	}
	if sum < 9.9 || sum > 10.1 {
		t.Fatalf("padded duration sum=%v want ~10", sum)
	}
}

func TestSafeSessionFileRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if _, ok := safeSessionFile(dir, "../etc/passwd"); ok {
		t.Fatal("expected reject")
	}
	if _, ok := safeSessionFile(dir, "seg00000.m4s"); !ok {
		t.Fatal("expected allow basename")
	}
}

func TestPlayDurationPrefersPlanSource(t *testing.T) {
	lib := openStreamLib(t)
	root, err := lib.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "SB", SourceURL: "https://www.example.com/@sb",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
		DeliveryMode: library.DeliveryStream,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := lib.DB.SQL.Exec(`
		INSERT INTO videos (series_id, source_id, remote_id, title, source_url, status, duration_seconds)
		VALUES (?, ?, 'v1', 'Ep', 'https://www.example.com/watch?v=v1', 'streamable', 975)`,
		ser.ID, ser.Sources[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	vid, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	strm := filepath.Join(t.TempDir(), "ep.strm")
	if err := os.WriteFile(strm, []byte("http://creatorr.example.com/stream/videos/1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'strm', datetime('now'))`, vid, strm); err != nil {
		t.Fatal(err)
	}
	plan := sponsorblock.PlanFromCuts("v1", []sponsorblock.Segment{
		{Category: "sponsor", Start: 949.767, End: 1038.997},
	}, true, 2, 1062)
	if _, err := sponsorblock.WritePlan(strm, plan); err != nil {
		t.Fatal(err)
	}
	h := &Handler{Library: lib}
	want := sponsorblock.PlaybackDuration(1062, plan)
	// Pass already-play DB length (975): must not subtract cuts again.
	got := h.playDuration(vid, 975)
	if got < want-0.5 || got > want+0.5 {
		t.Fatalf("playDuration(playSecs)=%v want ~%v", got, want)
	}
	got2 := h.playDuration(vid, 1062)
	if got2 < want-0.5 || got2 > want+0.5 {
		t.Fatalf("playDuration(source)=%v want ~%v", got2, want)
	}
}
