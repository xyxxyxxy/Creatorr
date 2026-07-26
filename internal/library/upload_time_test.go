package library

import "testing"

func TestParseUploadTime(t *testing.T) {
	if got := NormalizeUploadTime("2024-01-15T14:30:00Z"); got != "2024-01-15T14:30:00Z" {
		t.Fatalf("got %q", got)
	}
	// Reject date-only / unix - handlers must send RFC3339.
	for _, bad := range []string{"", "20240115", "2024-01-15", "1705329000"} {
		if got := NormalizeUploadTime(bad); got != "" {
			t.Fatalf("%q: want empty, got %q", bad, got)
		}
	}
}

func TestBeforeCutoff(t *testing.T) {
	if BeforeCutoff("2024-01-15T23:59:59Z", "2024-01-15") {
		t.Fatal("same calendar day must be indexed (not before cutoff)")
	}
	if BeforeCutoff("2024-01-15T00:00:00Z", "2024-01-15") {
		t.Fatal("midnight on cutoff day must be indexed")
	}
	if !BeforeCutoff("2024-01-14T23:59:59Z", "2024-01-15") {
		t.Fatal("previous day should be before cutoff")
	}
	if BeforeCutoff("2024-01-16T00:00:00Z", "2024-01-15") {
		t.Fatal("next day must not be before cutoff")
	}
	if BeforeCutoff("2024-01-15T12:00:00Z", "") {
		t.Fatal("empty cutoff never matches")
	}
}

func TestCutoffExpanded(t *testing.T) {
	cases := []struct {
		old, neu string
		want     bool
	}{
		{"2024-01-01", "2020-01-01", true},
		{"2024-01-01", "", true},
		{"2024-01-01", "2024-01-01", false},
		{"2020-01-01", "2024-01-01", false},
		{"", "2024-01-01", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := CutoffExpanded(tc.old, tc.neu); got != tc.want {
			t.Fatalf("CutoffExpanded(%q,%q)=%v want %v", tc.old, tc.neu, got, tc.want)
		}
	}
}
