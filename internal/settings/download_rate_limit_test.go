package settings

import "testing"

func TestSplitDownloadRateLimit(t *testing.T) {
	tests := []struct {
		in, wantV, wantU string
	}{
		{"", "", ""},
		{"  ", "", ""},
		{"10M", "10", "M"},
		{"500K", "500", "K"},
		{"1.5G", "1.5", "G"},
		{"off", "", "off"},
		{"0", "", "off"},
		{"NONE", "", "off"},
		{"unlimited", "", "off"},
		{"bogus", "", "M"},
	}
	for _, tc := range tests {
		v, u := SplitDownloadRateLimit(tc.in)
		if v != tc.wantV || u != tc.wantU {
			t.Errorf("SplitDownloadRateLimit(%q) = (%q,%q), want (%q,%q)", tc.in, v, u, tc.wantV, tc.wantU)
		}
	}
}

func TestCombineDownloadRateLimit(t *testing.T) {
	tests := []struct {
		value, unit, want string
		wantErr           bool
	}{
		{"", "", "", false},
		{"", "off", "off", false},
		{"10", "M", "10M", false},
		{"500", "k", "500K", false},
		{"1.5", "G", "1.5G", false},
		{"", "M", "", true},
		{"10", "", "", true},
		{"0", "M", "", true},
		{"-1", "M", "", true},
		{"x", "M", "", true},
	}
	for _, tc := range tests {
		got, err := CombineDownloadRateLimit(tc.value, tc.unit)
		if tc.wantErr {
			if err == nil {
				t.Errorf("CombineDownloadRateLimit(%q,%q) err=nil, want error", tc.value, tc.unit)
			}
			continue
		}
		if err != nil {
			t.Errorf("CombineDownloadRateLimit(%q,%q) err=%v", tc.value, tc.unit, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CombineDownloadRateLimit(%q,%q) = %q, want %q", tc.value, tc.unit, got, tc.want)
		}
	}
}

func TestCombineDownloadRateLimitOverride(t *testing.T) {
	tests := []struct {
		value, unit, def, want string
		wantErr                bool
	}{
		{"", "M", "10M", "", false},
		{"", "off", "10M", "off", false},
		{"", "off", "off", "", false},
		{"", "K", "off", "", false},
		{"5", "M", "10M", "5M", false},
		{"0", "M", "10M", "", true},
	}
	for _, tc := range tests {
		got, err := CombineDownloadRateLimitOverride(tc.value, tc.unit, tc.def)
		if tc.wantErr {
			if err == nil {
				t.Errorf("CombineDownloadRateLimitOverride(%q,%q,%q) err=nil, want error", tc.value, tc.unit, tc.def)
			}
			continue
		}
		if err != nil {
			t.Errorf("CombineDownloadRateLimitOverride(%q,%q,%q) err=%v", tc.value, tc.unit, tc.def, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CombineDownloadRateLimitOverride(%q,%q,%q) = %q, want %q", tc.value, tc.unit, tc.def, got, tc.want)
		}
	}
}

