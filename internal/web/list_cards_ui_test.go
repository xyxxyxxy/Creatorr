package web

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVideoCardLayoutBottomTextStatusTopRight(t *testing.T) {
	// String-guard: video cards match series_card chrome (status top-right, text bottom).
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "partials", "list_cards.html"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	start := strings.Index(body, `{{define "video_card"}}`)
	end := strings.Index(body, `{{define "video_gallery_card"}}`)
	if start < 0 || end < 0 || end <= start {
		t.Fatal("video_card define missing")
	}
	card := body[start:end]
	if !strings.Contains(card, `absolute top-1 right-1`) {
		t.Fatal("video_card must place status top-right")
	}
	if !strings.Contains(card, `card-body justify-end`) {
		t.Fatal("video_card text overlay must sit at bottom")
	}
	if strings.Contains(card, `bottom-2 left-2`) || strings.Contains(card, `bottom-2 right-2`) {
		t.Fatal("video_card must not pin duration/quality as corner badges")
	}
	if !strings.Contains(card, `.DurationLabel`) || !strings.Contains(card, `.ResolutionLabel`) {
		t.Fatal("video_card must still render duration and quality")
	}
}
