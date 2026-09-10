// Package fakemedia provides local-only fake ffmpeg/ffprobe binaries for tests.
// PrependPATH installs them ahead of the real tools so CI never needs ffmpeg.
package fakemedia

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// PrependPATH puts this package's bin/ (fake ffmpeg + ffprobe) first on PATH.
// Safe for hermetic tests: scripts never contact the network.
func PrependPATH(t *testing.T) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	bin := filepath.Join(filepath.Dir(file), "bin")
	ffmpeg := filepath.Join(bin, "ffmpeg")
	ffprobe := filepath.Join(bin, "ffprobe")
	if st, err := os.Stat(ffmpeg); err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("fake ffmpeg missing or not executable: %s", ffmpeg)
	}
	if st, err := os.Stat(ffprobe); err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("fake ffprobe missing or not executable: %s", ffprobe)
	}
	path := bin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", path)
}

// WriteDummyMedia writes a tiny placeholder media file (fake ffmpeg/ffprobe treat it as valid).
func WriteDummyMedia(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fake-media\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
