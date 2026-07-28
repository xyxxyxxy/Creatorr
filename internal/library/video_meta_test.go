package library

import "testing"

func TestResolutionLabel(t *testing.T) {
	cases := []struct {
		w, h int
		want string
	}{
		{0, 0, ""},
		{100, 0, ""},
		{256, 144, "240p"},
		{426, 240, "240p"},
		{640, 360, "360p"},
		{854, 480, "480p"},
		{1280, 720, "720p"},
		{1920, 1080, "1080p"},
		{1080, 1920, "1080p"}, // portrait
		{2560, 1440, "1080p"}, // QHD → 1080p bucket
		{1440, 2560, "1080p"},
		{3840, 2160, "4K"},
		{2160, 3840, "4K"},
		{191, 340, "240p"},
		{299, 340, "240p"},
		{300, 340, "360p"},
		{899, 1600, "720p"},
		{900, 1600, "1080p"},
		{1799, 3200, "1080p"},
		{1800, 3200, "4K"},
	}
	for _, tc := range cases {
		got := ResolutionLabel(tc.w, tc.h)
		if got != tc.want {
			t.Fatalf("ResolutionLabel(%d,%d)=%q want %q", tc.w, tc.h, got, tc.want)
		}
	}
}

func TestVideoResolutionLabel(t *testing.T) {
	v := Video{}
	if v.ResolutionLabel() != "" {
		t.Fatal("empty columns")
	}
	v.Width.Valid, v.Width.Int64 = true, 1920
	v.Height.Valid, v.Height.Int64 = true, 1080
	if got := v.ResolutionLabel(); got != "1080p" {
		t.Fatalf("got %q", got)
	}
}
