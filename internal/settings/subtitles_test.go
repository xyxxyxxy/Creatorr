package settings

import "testing"

func TestNormalizeSubtitleLangs(t *testing.T) {
	got := NormalizeSubtitleLangs([]string{" de ", "en", "en", "", "all"})
	want := []string{"all", "de", "en"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestSubtitleLangsJSONRoundTrip(t *testing.T) {
	raw := SubtitleLangsJSON([]string{"en.*", "en"})
	got := ParseSubtitleLangsJSON(raw)
	if len(got) != 2 || got[0] != "en" || got[1] != "en.*" {
		t.Fatalf("got %v from %q", got, raw)
	}
	if ParseSubtitleLangsJSON("not-json") != nil {
		t.Fatal("invalid json should be empty")
	}
}

func TestSubtitleLangSeedAllFirst(t *testing.T) {
	if len(SubtitleLangSeed) == 0 || SubtitleLangSeed[0] != "all" {
		t.Fatalf("seed[0]=%v", SubtitleLangSeed)
	}
}

func TestNormalizeSubtitleAuto(t *testing.T) {
	if NormalizeSubtitleAuto("1") != "1" || NormalizeSubtitleAuto("") != "0" {
		t.Fatal("auto normalize")
	}
}
