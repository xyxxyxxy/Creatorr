package ytdlp

import (
	"testing"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

func TestClassifyMatchFilterReject(t *testing.T) {
	const liveOnly = "is_live!=?1"
	const mediaOnly = "media_type!=short"
	const both = "media_type!=short & is_live!=?1"

	cases := []struct {
		name, stderr, filter, wantCode string
	}{
		{"empty filter", "does not pass filter", "", ""},
		{"live stderr", "[download] … is_live … does not pass filter", liveOnly, apperrors.CodeLiveBroadcastSkipped},
		{"media stderr", "[download] media_type!=short does not pass filter", mediaOnly, apperrors.CodeMediaTypeExcluded},
		{"ambiguous both prefer live", "[download] does not pass filter", both, apperrors.CodeLiveBroadcastSkipped},
		{"media named in both filter", "media_type excluded", both, apperrors.CodeMediaTypeExcluded},
		{"live only generic", "[download] skipping", liveOnly, apperrors.CodeLiveBroadcastSkipped},
		{"not a skip", "HTTP Error 403", liveOnly, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := classifyMatchFilterReject(tc.stderr, tc.filter)
			if code != tc.wantCode {
				t.Fatalf("code=%q want %q", code, tc.wantCode)
			}
		})
	}
}

func TestBoolFieldIsLive(t *testing.T) {
	if !boolField(map[string]any{"is_live": true}, "is_live") {
		t.Fatal("bool true")
	}
	if boolField(map[string]any{"is_live": false}, "is_live") {
		t.Fatal("bool false")
	}
	if !boolField(map[string]any{"is_live": float64(1)}, "is_live") {
		t.Fatal("float 1")
	}
	if boolField(map[string]any{}, "is_live") {
		t.Fatal("missing")
	}
}
