package library

import "testing"

func TestTitlePassesFilters(t *testing.T) {
	ok, reason := TitlePassesFilters(`(?i)show`, ``, "Show Trailer")
	if !ok || reason != "" {
		t.Fatalf("include only match: ok=%v reason=%q", ok, reason)
	}
	ok, reason = TitlePassesFilters(`(?i)show`, ``, "Vlog")
	if ok || reason != SkipReasonTitleRegexpInclude {
		t.Fatalf("include miss: ok=%v reason=%q", ok, reason)
	}
	ok, reason = TitlePassesFilters(``, `(?i)trailer`, "Show Trailer")
	if ok || reason != SkipReasonTitleRegexpExclude {
		t.Fatalf("exclude hit: ok=%v reason=%q", ok, reason)
	}
	ok, reason = TitlePassesFilters(`(?i)show`, `(?i)trailer`, "Show Trailer")
	if ok || reason != SkipReasonTitleRegexpExclude {
		t.Fatalf("both → exclude: ok=%v reason=%q", ok, reason)
	}
	ok, reason = TitlePassesFilters(``, ``, "Anything")
	if !ok || reason != "" {
		t.Fatalf("empty both: ok=%v reason=%q", ok, reason)
	}
}
