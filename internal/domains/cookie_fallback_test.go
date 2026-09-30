package domains_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

func TestInvokeWithCookieFallback_OffAlwaysJar(t *testing.T) {
	var calls []string
	out, st, err := domains.InvokeWithCookieFallback(
		false, true, "/jar.txt",
		func() (string, error) { t.Fatal("materialize"); return "", nil },
		func(path string) (string, error) {
			calls = append(calls, path)
			return "ok", nil
		},
		nil,
	)
	if err != nil || out != "ok" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if st.State != domains.CookieAttachCookies || st.AfterFail {
		t.Fatalf("status=%+v", st)
	}
	if len(calls) != 1 || calls[0] != "/jar.txt" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestInvokeWithCookieFallback_OnAgeRestrictedRetries(t *testing.T) {
	var calls []string
	var reason string
	out, st, err := domains.InvokeWithCookieFallback(
		true, true, "",
		func() (string, error) { return "/jar2.txt", nil },
		func(path string) (string, error) {
			calls = append(calls, path)
			if path == "" {
				return "", apperrors.New(apperrors.CodeAgeRestricted, "age")
			}
			return "ok", nil
		},
		func(r string) { reason = r },
	)
	if err != nil || out != "ok" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if st.State != domains.CookieAttachRetried || st.RetryReason != apperrors.CodeAgeRestricted {
		t.Fatalf("status=%+v", st)
	}
	if reason != apperrors.CodeAgeRestricted {
		t.Fatalf("onRetry=%q", reason)
	}
	if len(calls) != 2 || calls[0] != "" || calls[1] != "/jar2.txt" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestInvokeWithCookieFallback_OnCookieInvalidRetries(t *testing.T) {
	var calls []string
	_, st, err := domains.InvokeWithCookieFallback(
		true, true, "",
		func() (string, error) { return "/jar2.txt", nil },
		func(path string) (string, error) {
			calls = append(calls, path)
			if path == "" {
				return "", apperrors.New(apperrors.CodeCookieInvalid, "bad jar")
			}
			return "ok", nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != domains.CookieAttachRetried || len(calls) != 2 {
		t.Fatalf("status=%+v calls=%v", st, calls)
	}
}

func TestInvokeWithCookieFallback_OnGenericFailRetries(t *testing.T) {
	var calls []string
	_, st, err := domains.InvokeWithCookieFallback(
		true, true, "",
		func() (string, error) { return "/jar2.txt", nil },
		func(path string) (string, error) {
			calls = append(calls, path)
			if path == "" {
				return "", apperrors.New(apperrors.CodeDownloadFailed, "boom")
			}
			return "ok", nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != domains.CookieAttachRetried || len(calls) != 2 {
		t.Fatalf("status=%+v calls=%v", st, calls)
	}
}

func TestInvokeWithCookieFallback_OnRateLimitedNoRetry(t *testing.T) {
	var calls []string
	_, st, err := domains.InvokeWithCookieFallback(
		true, true, "",
		func() (string, error) { t.Fatal("materialize"); return "", nil },
		func(path string) (string, error) {
			calls = append(calls, path)
			return "", apperrors.New(apperrors.CodeRateLimited, "slow")
		},
		nil,
	)
	if apperrors.ErrorCode(err) != apperrors.CodeRateLimited {
		t.Fatalf("err=%v", err)
	}
	if st.State != domains.CookieAttachAnonymous || len(calls) != 1 || calls[0] != "" {
		t.Fatalf("status=%+v calls=%v", st, calls)
	}
}

func TestInvokeWithCookieFallback_OnUnavailableNoRetry(t *testing.T) {
	var calls []string
	_, st, err := domains.InvokeWithCookieFallback(
		true, true, "",
		func() (string, error) { t.Fatal("materialize"); return "", nil },
		func(path string) (string, error) {
			calls = append(calls, path)
			return "", apperrors.New(apperrors.CodeArchiveFallbackQueued, "gone")
		},
		nil,
	)
	if apperrors.ErrorCode(err) != apperrors.CodeArchiveFallbackQueued {
		t.Fatalf("err=%v", err)
	}
	if st.State != domains.CookieAttachAnonymous || len(calls) != 1 {
		t.Fatalf("status=%+v calls=%v", st, calls)
	}
}

func TestInvokeWithCookieFallback_NoStoredSingleCall(t *testing.T) {
	var calls []string
	_, st, err := domains.InvokeWithCookieFallback(
		true, false, "",
		func() (string, error) { t.Fatal("materialize"); return "", nil },
		func(path string) (string, error) {
			calls = append(calls, path)
			return "ok", nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != domains.CookieAttachOff || len(calls) != 1 || calls[0] != "" {
		t.Fatalf("status=%+v calls=%v", st, calls)
	}
}

func TestAllowStoredJar_RetryPass(t *testing.T) {
	if !domains.AllowStoredJar(false, false) {
		t.Fatal("off should allow")
	}
	if domains.AllowStoredJar(true, false) {
		t.Fatal("on first pass should omit")
	}
	if !domains.AllowStoredJar(true, true) {
		t.Fatal("on retry should allow")
	}
}
