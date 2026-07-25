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
	// beginning 8s + live 4s; pad to 20s
	body := string(buildHandoffPlaylist(begin, live, "/b/", "/l/", "tok", 20))
	if !strings.Contains(body, "/b/seg00000.ts?token=tok") {
		t.Fatalf("missing beginning uri: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-DISCONTINUITY") {
		t.Fatalf("missing discontinuity: %s", body)
	}
	if !strings.Contains(body, "/l/seg00000.ts?token=tok") {
		t.Fatalf("missing live uri: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-PLAYLIST-TYPE:VOD") {
		t.Fatalf("missing VOD: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-START:TIME-OFFSET=0") {
		t.Fatalf("missing start offset 0: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Fatalf("expected ENDLIST when duration known: %s", body)
	}
	if !strings.Contains(body, "/l/seg00001.ts?token=tok") {
		t.Fatalf("expected padded live segs: %s", body)
	}
	sum := 0.0
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#EXTINF:") {
			sum += extinfSeconds(strings.TrimPrefix(line, "#EXTINF:"))
		}
	}
	if sum < 19.9 || sum > 20.1 {
		t.Fatalf("duration sum=%v want ~20", sum)
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

	body := string(buildHandoffPlaylist(begin, live, "/b/", "/l/", "t", 0))
	if !strings.Contains(body, "seg00000.ts") || !strings.Contains(body, "seg00002.ts") {
		t.Fatalf("expected full beginning dumped: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-DISCONTINUITY") || !strings.Contains(body, "/l/seg00000.ts") {
		t.Fatalf("expected live after beginning: %s", body)
	}
	if strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Fatalf("no ENDLIST without durationSec: %s", body)
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
	body := string(buildHandoffPlaylist(begin, live, "/b/", "/l/", "t", 10))
	if !strings.Contains(body, "#EXT-X-DISCONTINUITY") {
		t.Fatalf("expected discontinuity before padded live: %s", body)
	}
	if !strings.Contains(body, "/b/a.ts?token=t") {
		t.Fatalf("missing beginning: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Fatalf("expected ENDLIST with duration: %s", body)
	}
	// pad uses live base when no live entries yet
	if !strings.Contains(body, "/l/seg00000.ts?token=t") {
		t.Fatalf("expected padded live segs after beginning-only: %s", body)
	}
}
