package web

import "testing"

func TestVideoStatusLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"wanted", "wanted"},
		{"wanted_download_error", "wanted (download error)"},
		{"wanted_archive", "wanted (Web Archive)"},
		{"integrity_check_failed", "verify failed"},
		{"downloaded", "downloaded"},
		{"missing", "missing"},
		{"deleted", "deleted"},
		{"ignored", "ignored"},
		{"custom_status", "custom_status"},
	}
	for _, tc := range cases {
		if got := videoStatusLabel(tc.in); got != tc.want {
			t.Fatalf("videoStatusLabel(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
