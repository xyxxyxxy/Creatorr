package notify_test

import (
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
)

func TestFormatDigestBodyIncludesVideoID(t *testing.T) {
	body := notify.FormatDigestBody([]notify.DigestItem{{
		VideoID: 6, Series: "Chud Logic", Title: "Episode", Kind: "archive",
	}})
	if !strings.Contains(body, "[#6]") {
		t.Fatalf("body=%q", body)
	}
	disp := notify.DigestBodyDisplay(body)
	if strings.Contains(disp, "[#6]") || !strings.Contains(disp, "Chud Logic / Episode (downloaded)") {
		t.Fatalf("display=%q", disp)
	}
	lines := notify.ParseDigestBodyLines(body)
	if len(lines) != 1 || lines[0].VideoID != 6 || lines[0].Series != "Chud Logic" || lines[0].Title != "Episode" {
		t.Fatalf("%#v", lines)
	}
}

func TestParseDigestBodyLinesLegacy(t *testing.T) {
	lines := notify.ParseDigestBodyLines("- Chud Logic / What the show (downloaded)")
	if len(lines) != 1 {
		t.Fatalf("%#v", lines)
	}
	if lines[0].VideoID != 0 || lines[0].Series != "Chud Logic" || lines[0].Title != "What the show" {
		t.Fatalf("%#v", lines[0])
	}
}
