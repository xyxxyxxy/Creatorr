package errors

import (
	"context"
	"errors"
)

// ErrorCode returns AppError.Code when err unwraps to *AppError; otherwise "".
func ErrorCode(err error) string {
	var ae *AppError
	if errors.As(err, &ae) && ae != nil {
		return ae.Code
	}
	return ""
}

// CookieRetryWorthless reports failures where attaching account cookies cannot
// change the outcome. Auto download cookie-retry skips these and fails as-is.
func CookieRetryWorthless(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	switch ErrorCode(err) {
	case CodeRateLimited, CodeLiveBroadcastSkipped, CodeArchiveFallbackQueued:
		return true
	}
	return DetectVideoUnavailable(err.Error())
}
