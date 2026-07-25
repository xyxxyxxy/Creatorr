package library_test

import (
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestShouldVerifyMedia(t *testing.T) {
	upload := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	uploadStr := upload.Format(time.RFC3339)

	cases := []struct {
		name         string
		verify       bool
		hours        int
		maturityPack bool
		acquired     time.Time
		want         bool
	}{
		{"off", false, 0, false, upload.Add(time.Hour), false},
		{"hours0_always", true, 0, false, upload.Add(time.Hour), true},
		{"young_skip", true, 48, false, upload.Add(time.Hour), false},
		{"young_maturity_pack", true, 48, true, upload.Add(time.Hour), true},
		{"already_mature", true, 48, false, upload.Add(72 * time.Hour), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := library.QualityProfile{VerifyMedia: tc.verify, MaturityRedownloadHours: tc.hours}
			got := library.ShouldVerifyMedia(p, tc.maturityPack, uploadStr, tc.acquired)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	if !library.ShouldVerifyMedia(library.QualityProfile{VerifyMedia: true, MaturityRedownloadHours: 48}, false, "", time.Now()) {
		t.Fatal("empty upload_date should verify (maturity never schedules)")
	}
}
