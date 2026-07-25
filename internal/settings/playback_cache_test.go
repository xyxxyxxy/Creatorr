package settings

import "testing"

func TestNormalizeStreamPlaybackCacheMaxHours(t *testing.T) {
	if got := NormalizeStreamPlaybackCacheMaxHours("20"); got != "20" {
		t.Fatalf("got %s", got)
	}
	if got := NormalizeStreamPlaybackCacheMaxHours("15"); got != "20" {
		t.Fatalf("got %s want 20", got)
	}
	if got := NormalizeStreamPlaybackCacheMaxHours("999"); got != "100" {
		t.Fatalf("got %s", got)
	}
	if got := NormalizeStreamPlaybackCacheMaxHours("bad"); got != "20" {
		t.Fatalf("got %s", got)
	}
}

func TestValidateStreamPlaybackCacheMaxHours(t *testing.T) {
	if err := validateStreamPlaybackCacheMaxHours("20"); err != nil {
		t.Fatal(err)
	}
	if err := validateStreamPlaybackCacheMaxHours("15"); err == nil {
		t.Fatal("expected error")
	}
	if err := validateStreamPlaybackCache("1"); err != nil {
		t.Fatal(err)
	}
	if err := validateStreamPlaybackCache("2"); err == nil {
		t.Fatal("expected error")
	}
}
