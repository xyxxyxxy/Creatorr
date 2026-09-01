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
