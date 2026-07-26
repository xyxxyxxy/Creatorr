package library_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestMaterializeThumbSrcPrefersExistingFile(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "have.webp")
	if err := os.WriteFile(existing, []byte("IMG"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, cleanup := library.MaterializeThumbSrc(existing, "https://example.com/nope.jpg")
	defer cleanup()
	if path != existing {
		t.Fatalf("path=%q want existing file", path)
	}
}

func TestMaterializeThumbSrcDownloadsURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("JPGDATA"))
	}))
	defer srv.Close()

	path, cleanup := library.MaterializeThumbSrc("", srv.URL+"/thumb.jpg")
	defer cleanup()
	if path == "" {
		t.Fatal("expected temp thumb path")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "JPGDATA" {
		t.Fatalf("body=%q", body)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cleanup should remove temp thumb")
	}
}

func TestMaterializeThumbSrcSoftOkOnHTTPFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	path, cleanup := library.MaterializeThumbSrc("", srv.URL+"/missing.jpg")
	defer cleanup()
	if path != "" {
		t.Fatalf("path=%q want empty on HTTP fail", path)
	}
}
