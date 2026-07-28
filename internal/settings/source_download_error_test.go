package settings

import "testing"

func TestNormalizeSourceDownloadErrorThreshold(t *testing.T) {
	if got := NormalizeSourceDownloadErrorThreshold(""); got != "2" {
		t.Fatalf("empty: %q", got)
	}
	if got := NormalizeSourceDownloadErrorThreshold("0"); got != "1" {
		t.Fatalf("zero: %q", got)
	}
	if got := NormalizeSourceDownloadErrorThreshold("-3"); got != "1" {
		t.Fatalf("neg: %q", got)
	}
	if got := NormalizeSourceDownloadErrorThreshold("1"); got != "1" {
		t.Fatalf("one: %q", got)
	}
	if got := NormalizeSourceDownloadErrorThreshold(" 7 "); got != "7" {
		t.Fatalf("seven: %q", got)
	}
}

func TestValidateSourceDownloadErrorThreshold(t *testing.T) {
	if err := validateSourceDownloadErrorThreshold("1"); err != nil {
		t.Fatal(err)
	}
	if err := validateSourceDownloadErrorThreshold("0"); err == nil {
		t.Fatal("want reject 0")
	}
	if err := validateSourceDownloadErrorThreshold("x"); err == nil {
		t.Fatal("want reject non-int")
	}
}
