package streamproxy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func TestHLSStartCompatible(t *testing.T) {
	cases := []struct {
		name       string
		sess, want float64
		ok         bool
	}{
		{"equal", 22, 22, true},
		{"growing_handoff", 22, 73, true},
		{"earlier_want", 73, 22, false},
		{"from_zero", 0, 0, true},
		{"near_equal", 22.02, 22.00, true},
		{"grow_from_zero", 0, 40, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hlsStartCompatible(tc.sess, tc.want); got != tc.ok {
				t.Fatalf("hlsStartCompatible(%v,%v)=%v want %v", tc.sess, tc.want, got, tc.ok)
			}
		})
	}
}

func TestEnsureHLSSessionReusesGrowingHandoff(t *testing.T) {
	clearHLSSessions(t)
	done := make(chan error)
	dir := t.TempDir()
	existing := &hlsSession{
		id: "sid-a", videoID: 1, token: "tok",
		dir: dir, startSec: 22,
		cancel: func() {}, done: done,
		lastUse: time.Now(),
	}
	key := hlsSessionKey(1, "tok")
	hlsMu.Lock()
	hlsSessions[key] = existing
	hlsMu.Unlock()

	h := &Handler{YtDlp: nil} // must not start a new mux
	got, err := h.ensureHLSSession(playCtx{
		videoID: 1, token: "tok", seriesID: 1, domain: "example.com",
	}, ytdlp.UrlsResult{DurationSeconds: 90}, "", 73)
	if err != nil {
		t.Fatal(err)
	}
	if got.id != "sid-a" {
		t.Fatalf("sid=%s want sid-a (reuse)", got.id)
	}
	if got.startSec != 22 {
		t.Fatalf("startSec=%v want 22 (keep original)", got.startSec)
	}
}

func TestEnsureHLSSessionReplacesEarlierWant(t *testing.T) {
	clearHLSSessions(t)
	t.Setenv("CREATORR_FAKE_URLS_KIND", "pipe")

	cancelled := false
	oldDir := t.TempDir()
	done := make(chan error)
	old := &hlsSession{
		id: "sid-old", videoID: 2, token: "tok2",
		dir: oldDir, startSec: 73,
		cancel: func() { cancelled = true }, done: done,
		lastUse: time.Now(),
	}
	key := hlsSessionKey(2, "tok2")
	hlsMu.Lock()
	hlsSessions[key] = old
	hlsMu.Unlock()

	h := &Handler{
		YtDlp:   &ytdlp.Client{},
		TmpRoot: t.TempDir(),
	}
	got, err := h.ensureHLSSession(playCtx{
		videoID: 2, token: "tok2", seriesID: 1, domain: "example.com",
	}, ytdlp.UrlsResult{}, "", 22)
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("expected old session cancel")
	}
	if got.id == "sid-old" {
		t.Fatal("expected new session for earlier handoff")
	}
	if !approxStartSec(got.startSec, 22) {
		t.Fatalf("startSec=%v want 22", got.startSec)
	}
	got.cancel()
}

func TestEnsureSessionSegmentFailFastAfterEndlist(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:2.0,
seg00000.ts
#EXT-X-ENDLIST
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seg00000.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	s := &hlsSession{dir: dir, done: done}
	if err := s.ensureSessionSegment("seg00000.ts"); err != nil {
		t.Fatalf("listed seg: %v", err)
	}
	start := time.Now()
	err := s.ensureSessionSegment("seg00003.ts")
	if err == nil {
		t.Fatal("expected miss for phantom seg")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("fail-fast took too long: %v", time.Since(start))
	}
}

func clearHLSSessions(t *testing.T) {
	t.Helper()
	hlsMu.Lock()
	hlsSessions = map[string]*hlsSession{}
	hlsMu.Unlock()
	t.Cleanup(func() {
		hlsMu.Lock()
		for _, s := range hlsSessions {
			if s.cancel != nil {
				s.cancel()
			}
			if s.dir != "" {
				_ = os.RemoveAll(s.dir)
			}
		}
		hlsSessions = map[string]*hlsSession{}
		hlsMu.Unlock()
	})
}
