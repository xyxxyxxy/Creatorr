package web_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVideoDetailFilesIntegrityFailedUsesTextError(t *testing.T) {
	// String-guard: derived-failed rows paint kind/name/size with text-error.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	path := filepath.Join(filepath.Dir(thisFile), "templates", "video_detail.html")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if !strings.Contains(body, "IntegrityFailed") {
		t.Fatal("video_detail Files table missing IntegrityFailed branch")
	}
	if !strings.Contains(body, "text-error") {
		t.Fatal("video_detail Files table missing text-error class")
	}
}
