package domains

import (
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

// AllowStoredJar reports whether the stored Netscape jar may be attached.
// When cookies_after_fail is off, always allow. When on, allow only on the
// cookie retry pass (retryPass true).
func AllowStoredJar(cookiesAfterFail, retryPass bool) bool {
	if !cookiesAfterFail {
		return true
	}
	return retryPass
}

// InvokeWithCookieFallback runs invoke with the jar policy for cookies_after_fail.
//
// Flag off (or no stored jar): invoke(initialJar) once.
// Flag on with a stored jar: invoke("") first; on failure that is not
// CookieRetryWorthless, materializeRetry and invoke once with that path.
// onRetry is optional and called with the first-failure code before the cookie pass.
//
// Soft-pause / alerts use the returned (final) error only.
func InvokeWithCookieFallback[T any](
	afterFail, hadStored bool,
	initialJar string,
	materializeRetry func() (string, error),
	invoke func(cookiesPath string) (T, error),
	onRetry func(reason string),
) (T, CookieAttachStatus, error) {
	var zero T
	st := CookieAttachStatus{AfterFail: afterFail}

	if !hadStored {
		st.State = CookieAttachOff
		out, err := invoke(initialJar)
		return out, st, err
	}

	if !afterFail {
		st.State = CookieAttachCookies
		st.Detail = "account cookies attached"
		out, err := invoke(initialJar)
		if err != nil {
			st.Detail = ""
		}
		return out, st, err
	}

	// afterFail && hadStored: anonymous first.
	out, err := invoke("")
	if err == nil {
		st.State = CookieAttachAnonymous
		st.Detail = "succeeded without account cookies"
		return out, st, nil
	}
	if apperrors.CookieRetryWorthless(err) {
		st.State = CookieAttachAnonymous
		st.Detail = "failed without account cookies"
		return zero, st, err
	}

	retryReason := apperrors.ErrorCode(err)
	if retryReason == "" {
		retryReason = apperrors.CodeDownloadFailed
	}
	if onRetry != nil {
		onRetry(retryReason)
	}
	jar2, merr := materializeRetry()
	if merr != nil {
		st.State = CookieAttachAnonymous
		st.Detail = "cookie jar failed on retry"
		return zero, st, merr
	}
	out2, err2 := invoke(jar2)
	st.State = CookieAttachRetried
	st.RetryReason = retryReason
	if err2 != nil {
		st.Detail = "cookie retry failed"
		return zero, st, err2
	}
	st.Detail = "succeeded after cookie retry"
	return out2, st, nil
}
