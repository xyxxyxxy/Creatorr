package errors_test

import (
	"testing"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

func TestDetectPauseCode(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"", ""},
		{"ERROR: Unable to download", ""},
		{"ERROR: cookies are no longer valid", apperrors.CodeCookieInvalid},
		{"HTTP Error 403: Please sign in", apperrors.CodeCookieInvalid},
		{"ERROR: [mdetv] id: mde.tv stream sign HTTP 403: login required, or this video is outside your membership tier", apperrors.CodeCookieInvalid},
		{"ERROR: Unable to download webpage: HTTP Error 429: Too Many Requests", apperrors.CodeRateLimited},
		{"ERROR: [generic] xyz: This IP has been blocked", apperrors.CodeRateLimited},
		{"got rate-limited by the site", apperrors.CodeRateLimited},
		{"status code 429 from CDN", apperrors.CodeRateLimited},
		{"quota exceeded for this key", apperrors.CodeRateLimited},
	}
	for _, tc := range cases {
		if got := apperrors.DetectPauseCode(tc.msg); got != tc.want {
			t.Fatalf("DetectPauseCode(%q)=%q want %q", tc.msg, got, tc.want)
		}
	}
}

func TestUpgradeCode(t *testing.T) {
	got := apperrors.UpgradeCode(apperrors.CodeDownloadFailed, "HTTP Error 429: Too Many Requests")
	if got != apperrors.CodeRateLimited {
		t.Fatalf("%q", got)
	}
	got = apperrors.UpgradeCode(apperrors.CodeCookieInvalid, "HTTP Error 429")
	if got != apperrors.CodeCookieInvalid {
		t.Fatalf("must keep cookie code, got %q", got)
	}
	got = apperrors.UpgradeCode(apperrors.CodeRemuxFailed, "HTTP Error 429: Too Many Requests")
	if got != apperrors.CodeRemuxFailed {
		t.Fatalf("must keep RemuxFailed, got %q", got)
	}
	got = apperrors.UpgradeCode(apperrors.CodePackFailed, "sign in required")
	if got != apperrors.CodePackFailed {
		t.Fatalf("must keep PackFailed, got %q", got)
	}
}

func TestIsYtDlpPauseCode(t *testing.T) {
	if !apperrors.IsYtDlpPauseCode(apperrors.CodeDownloadFailed) ||
		!apperrors.IsYtDlpPauseCode(apperrors.CodeResolveFailed) ||
		!apperrors.IsYtDlpPauseCode(apperrors.CodeCookieInvalid) ||
		!apperrors.IsYtDlpPauseCode(apperrors.CodeRateLimited) {
		t.Fatal("expected yt-dlp pause codes")
	}
	if apperrors.IsYtDlpPauseCode(apperrors.CodeRemuxFailed) ||
		apperrors.IsYtDlpPauseCode(apperrors.CodePackFailed) ||
		apperrors.IsYtDlpPauseCode(apperrors.CodeMediaVerifyFailed) ||
		apperrors.IsYtDlpPauseCode(apperrors.CodeLiveBroadcastSkipped) ||
		apperrors.IsYtDlpPauseCode(apperrors.CodeMediaTypeExcluded) {
		t.Fatal("remux/pack/verify/live-skip/media-type must not pause")
	}
}

func TestDownloadFailStage(t *testing.T) {
	if got := apperrors.DownloadFailStage(apperrors.CodeRemuxFailed); got != "remux" {
		t.Fatalf("%q", got)
	}
	if got := apperrors.DownloadFailStage(apperrors.CodePackFailed); got != "pack" {
		t.Fatalf("%q", got)
	}
	if got := apperrors.DownloadFailStage(apperrors.CodeDownloadFailed); got != "fetch" {
		t.Fatalf("%q", got)
	}
}
