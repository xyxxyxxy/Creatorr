package streamproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildHandoffPlaylist(t *testing.T) {
	begin := t.TempDir()
	live := t.TempDir()
	if err := os.WriteFile(filepath.Join(begin, "index.m3u8"), []byte(`#EXTM3U
#EXT-X-TARGETDURATION:4
#EXTINF:4.0,
seg00000.ts
#EXTINF:4.0,
seg00001.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "index.m3u8"), []byte(`#EXTM3U
#EXT-X-TARGETDURATION:4
#EXTINF:4.0,
seg00000.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Live still open: EVENT, real segs only (no phantom pad even when durationSec > sum).
	body := string(buildHandoffPlaylist(begin, live, "/b/", "/l/", "tok", 20, 0))
	if !strings.Contains(body, "/b/seg00000.ts?token=tok") {
		t.Fatalf("missing beginning uri: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-DISCONTINUITY") {
		t.Fatalf("missing discontinuity: %s", body)
	}
	if !strings.Contains(body, "/l/seg00000.ts?token=tok") {
		t.Fatalf("missing live uri: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-PLAYLIST-TYPE:EVENT") {
		t.Fatalf("expected EVENT while mux open: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-START:TIME-OFFSET=0") {
		t.Fatalf("missing start offset 0: %s", body)
	}
	if strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Fatalf("no ENDLIST while mux open: %s", body)
	}
	if strings.Contains(body, "/l/seg00001.ts") {
		t.Fatalf("must not pad phantom live segs: %s", body)
	}
}

func TestBuildHandoffPlaylistFullBeginningNoReveal(t *testing.T) {
	begin := t.TempDir()
	live := t.TempDir()
	if err := os.WriteFile(filepath.Join(begin, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:4.0,
seg00000.ts
#EXTINF:4.0,
seg00001.ts
#EXTINF:4.0,
seg00002.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:4.0,
seg00000.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}

	body := string(buildHandoffPlaylist(begin, live, "/b/", "/l/", "t", 0, 0))
	if !strings.Contains(body, "seg00000.ts") || !strings.Contains(body, "seg00002.ts") {
		t.Fatalf("expected full beginning dumped: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-DISCONTINUITY") || !strings.Contains(body, "/l/seg00000.ts") {
		t.Fatalf("expected live after beginning: %s", body)
	}
	if strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Fatalf("no ENDLIST without live ENDLIST: %s", body)
	}
}

func TestBuildHandoffPlaylistBeginningOnly(t *testing.T) {
	begin := t.TempDir()
	live := t.TempDir()
	if err := os.WriteFile(filepath.Join(begin, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:2.0,
a.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := string(buildHandoffPlaylist(begin, live, "/b/", "/l/", "t", 10, 0))
	if strings.Contains(body, "#EXT-X-DISCONTINUITY") {
		t.Fatalf("no discontinuity without live segs: %s", body)
	}
	if !strings.Contains(body, "/b/a.ts?token=t") {
		t.Fatalf("missing beginning: %s", body)
	}
	if strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Fatalf("no ENDLIST while mux open / empty live: %s", body)
	}
	if strings.Contains(body, "/l/seg00000.ts") {
		t.Fatalf("must not invent live pad segs: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-PLAYLIST-TYPE:EVENT") {
		t.Fatalf("expected EVENT: %s", body)
	}
}

func TestBuildHandoffPlaylistPreservesPrefixDiscontinuity(t *testing.T) {
	begin := t.TempDir()
	live := t.TempDir()
	if err := os.WriteFile(filepath.Join(begin, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:4.0,
seg00000.ts
#EXTINF:4.0,
seg00001.ts
#EXT-X-DISCONTINUITY
#EXTINF:4.0,
seg00002.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:4.0,
seg00000.ts
#EXT-X-ENDLIST
`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := string(buildHandoffPlaylist(begin, live, "/b/", "/l/", "t", 0, 0))
	// Prefix discontinuity before seg00002, plus handoff discontinuity before live.
	if !strings.Contains(body, "/b/seg00001.ts?token=t\n#EXT-X-DISCONTINUITY\n#EXTINF:4.0,\n/b/seg00002.ts?token=t") {
		t.Fatalf("missing prefix discontinuity: %s", body)
	}
	if !strings.Contains(body, "/b/seg00002.ts?token=t\n#EXT-X-DISCONTINUITY\n#EXTINF:4.0,\n/l/seg00000.ts?token=t") {
		t.Fatalf("missing live discontinuity: %s", body)
	}
}

func TestBuildHandoffPlaylistSkipsPromotedLive(t *testing.T) {
	prefix := t.TempDir()
	live := t.TempDir()
	// Progressive cache already holds beginning + first live seg.
	if err := os.WriteFile(filepath.Join(prefix, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:4.0,
seg00000.ts
#EXT-X-DISCONTINUITY
#EXTINF:4.0,
seg00001.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:4.0,
seg00000.ts
#EXTINF:4.0,
seg00001.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// LiveSegsCopied=1: only the second live seg is new.
	body := string(buildHandoffPlaylist(prefix, live, "/p/", "/l/", "t", 0, 1))
	if strings.Contains(body, "/l/seg00000.ts") {
		t.Fatalf("must not re-list promoted live seg: %s", body)
	}
	if !strings.Contains(body, "/p/seg00001.ts?token=t\n#EXTINF:4.0,\n/l/seg00001.ts?token=t") {
		t.Fatalf("expected new live after prefix without extra discontinuity: %s", body)
	}
	if strings.Count(body, "#EXT-X-DISCONTINUITY") != 1 {
		t.Fatalf("want only prefix discontinuity, got: %s", body)
	}
}

func TestBuildHandoffPlaylistNoPadAfterLiveEndlist(t *testing.T) {
	begin := t.TempDir()
	live := t.TempDir()
	if err := os.WriteFile(filepath.Join(begin, "index.m3u8"), []byte(`#EXTM3U
#EXT-X-TARGETDURATION:4
#EXTINF:4.0,
seg00000.ts
#EXTINF:4.0,
seg00001.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Live finished short of declared duration (8+4=12 < 20).
	if err := os.WriteFile(filepath.Join(live, "index.m3u8"), []byte(`#EXTM3U
#EXT-X-TARGETDURATION:4
#EXTINF:4.0,
seg00000.ts
#EXT-X-ENDLIST
`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := string(buildHandoffPlaylist(begin, live, "/b/", "/l/", "tok", 20, 0))
	if !strings.Contains(body, "/l/seg00000.ts?token=tok") {
		t.Fatalf("missing live uri: %s", body)
	}
	if strings.Contains(body, "/l/seg00001.ts") {
		t.Fatalf("must not pad phantom segs after live ENDLIST: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-PLAYLIST-TYPE:VOD") {
		t.Fatalf("expected VOD after live ENDLIST: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Fatalf("expected ENDLIST: %s", body)
	}
	sum := 0.0
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#EXTINF:") {
			sum += extinfSeconds(strings.TrimPrefix(line, "#EXTINF:"))
		}
	}
	if sum < 11.9 || sum > 12.1 {
		t.Fatalf("sum=%v want ~12 (no pad)", sum)
	}
}
