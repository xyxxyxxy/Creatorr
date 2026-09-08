package errors_test

import (
	"context"
	"errors"
	"testing"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

func TestCookieRetryWorthless(t *testing.T) {
	if !apperrors.CookieRetryWorthless(nil) {
		t.Fatal("nil")
	}
	if !apperrors.CookieRetryWorthless(context.Canceled) {
		t.Fatal("canceled")
	}
	if !apperrors.CookieRetryWorthless(apperrors.New(apperrors.CodeRateLimited, "slow")) {
		t.Fatal("rate")
	}
	if !apperrors.CookieRetryWorthless(apperrors.New(apperrors.CodeLiveBroadcastSkipped, "live")) {
		t.Fatal("live")
	}
	if !apperrors.CookieRetryWorthless(apperrors.New(apperrors.CodeArchiveFallbackQueued, "gone")) {
		t.Fatal("archive")
	}
	if !apperrors.CookieRetryWorthless(errors.New("This video has been removed by the uploader")) {
		t.Fatal("unavailable text")
	}
	if apperrors.CookieRetryWorthless(apperrors.New(apperrors.CodeDownloadFailed, "boom")) {
		t.Fatal("download failed should retry")
	}
	if apperrors.CookieRetryWorthless(apperrors.New(apperrors.CodeAgeRestricted, "age")) {
		t.Fatal("age should retry")
	}
	if apperrors.CookieRetryWorthless(apperrors.New(apperrors.CodeCookieInvalid, "sign in")) {
		t.Fatal("cookie invalid should retry")
	}
	if apperrors.CookieRetryWorthless(apperrors.New(apperrors.CodeResolveFailed, "nope")) {
		t.Fatal("resolve should retry")
	}
}
