package library_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestFindDownloadSidecars(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "clip.mkv")
	info := filepath.Join(dir, "clip.info.json")
	thumb := filepath.Join(dir, "clip.webp")
	if err := os.WriteFile(media, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(info, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumb, []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}
	gotInfo, gotThumb, _ := library.FindDownloadSidecars(media)
	if gotInfo != info || gotThumb != thumb {
		t.Fatalf("info=%q thumb=%q", gotInfo, gotThumb)
	}
}

func TestFindDownloadSidecarsSubs(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "clip.mkv")
	sub := filepath.Join(dir, "clip.en.vtt")
	if err := os.WriteFile(media, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sub, []byte("WEBVTT"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, subs := library.FindDownloadSidecars(media)
	if len(subs) != 1 || subs[0] != sub {
		t.Fatalf("subs=%v", subs)
	}
}

func TestPackMediaCopiesSidecars(t *testing.T) {
	srcDir := t.TempDir()
	root := t.TempDir()
	media := filepath.Join(srcDir, "a.mkv")
	info := filepath.Join(srcDir, "a.info.json")
	thumb := filepath.Join(srcDir, "a.jpg")
	sub := filepath.Join(srcDir, "a.en.vtt")
	for path, body := range map[string]string{media: "m", info: "{}", thumb: "t", sub: "WEBVTT"} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mediaPath, nfoPath, infoPath, thumbPath, subPaths, err := library.PackMedia(
		media, root,
		library.EpisodeNFO{
			SeriesTitle: "Show", Title: "Ep", Season: 1, Episode: 2,
			Plot: "plot text", Aired: "2024-01-15T00:00:00Z", UniqueID: "rid", SourceSite: "yt-dlp",
		},
		library.NamingConfig{EpisodeFormat: library.DefaultEpisodeFormat},
		info, thumb, []string{sub},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !fileExists(mediaPath) || !fileExists(nfoPath) || !fileExists(infoPath) || !fileExists(thumbPath) {
		t.Fatalf("missing packed files: media=%v nfo=%v info=%v thumb=%v",
			fileExists(mediaPath), fileExists(nfoPath), fileExists(infoPath), fileExists(thumbPath))
	}
	if len(subPaths) != 1 || !strings.HasSuffix(subPaths[0], ".en.vtt") || !fileExists(subPaths[0]) {
		t.Fatalf("subPaths=%v", subPaths)
	}
	nfoBody, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(nfoBody)
	for _, want := range []string{"<plot>plot text</plot>", "<showtitle>Show</showtitle>", "<aired>2024-01-15</aired>", "Ep"} {
		if !strings.Contains(body, want) {
			t.Fatalf("nfo missing %q in %s", want, body)
		}
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
