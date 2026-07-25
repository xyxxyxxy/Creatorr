package settings

import "testing"

func TestParseStatsRetentionDays(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"90", 90},
		{"365", 365},
		{"-1", -1},
		{"0", 365},
		{"7", 365},
		{"30", 365},
		{"", 365},
		{"14", 365},
	}
	for _, tc := range cases {
		if got := ParseStatsRetentionDays(tc.in); got != tc.want {
			t.Errorf("ParseStatsRetentionDays(%q)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeStatsRetention(t *testing.T) {
	if got := NormalizeStatsRetention("30"); got != StatsRetentionYear {
		t.Fatalf("legacy 30 -> %q want %q", got, StatsRetentionYear)
	}
	if got := NormalizeStatsRetention("90"); got != StatsRetentionThreeMonths {
		t.Fatalf("90 -> %q", got)
	}
}

func TestValidateStatsRetention(t *testing.T) {
	for _, ok := range []string{"90", "365", "-1"} {
		if err := validateStatsRetention(ok); err != nil {
			t.Errorf("validateStatsRetention(%q): %v", ok, err)
		}
	}
	if err := validateStatsRetention("14"); err == nil {
		t.Fatal("expected error for 14")
	}
	if err := validateStatsRetention("0"); err == nil {
		t.Fatal("expected error for 0")
	}
}
