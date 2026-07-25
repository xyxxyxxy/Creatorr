package ytdlp

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

// Test hooks via env (streamproxy unit tests). Not for production.
const (
	envFakeUrlsKind = "CREATORR_FAKE_URLS_KIND"
	envFakeHLSFail  = "CREATORR_FAKE_HLS_FAIL"
)

func fakeUrlsFromEnv() (UrlsResult, bool) {
	kind := strings.TrimSpace(os.Getenv(envFakeUrlsKind))
	if kind == "" {
		return UrlsResult{}, false
	}
	switch kind {
	case UrlsKindPipe:
		return UrlsResult{Kind: UrlsKindPipe}, true
	case UrlsKindHLS:
		return UrlsResult{
			Kind: UrlsKindHLS,
			URL:  "https://cdn.example.com/master.m3u8",
		}, true
	default:
		return UrlsResult{
			Kind: UrlsKindProgressive,
			URL:  "https://cdn.example.com/v.mp4",
		}, true
	}
}

func fakeStartHLS(ctx context.Context, dir string) (<-chan error, error) {
	if os.Getenv(envFakeHLSFail) == "1" {
		return nil, appErr(apperrors.CodeDownloadFailed, "fake hls fail", "")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	index := filepath.Join(dir, "index.m3u8")
	seg := filepath.Join(dir, "seg00000.ts")
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-PLAYLIST-TYPE:EVENT\n#EXTINF:2.0,\nseg00000.ts\n"
	if err := os.WriteFile(index, []byte(playlist), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(seg, []byte("seg"), 0o644); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		<-ctx.Done()
		done <- nil
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	return done, nil
}

func fakePipeStream(dst io.Writer) error {
	_, err := io.WriteString(dst, "fake-matroska")
	return err
}
