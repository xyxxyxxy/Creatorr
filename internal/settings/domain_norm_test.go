package settings_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestNormalizeDomain(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"Example.com", "example.com"},
		{"www.example.com", "example.com"},
		{"WWW.Example.com", "example.com"},
		{"m.example.com", "m.example.com"},
		{"  www.example.com  ", "example.com"},
	}
	for _, tc := range cases {
		if got := settings.NormalizeDomain(tc.in); got != tc.want {
			t.Fatalf("NormalizeDomain(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateOverrideDomain(t *testing.T) {
	ok := []string{"example.com", "www.Example.com", "a.b-c.example.com", "xn--bcher-kva.example"}
	for _, in := range ok {
		if err := settings.ValidateOverrideDomain(in); err != nil {
			t.Fatalf("ValidateOverrideDomain(%q): %v", in, err)
		}
	}
	bad := []struct {
		in, wantSub string
	}{
		{"", "domain required"},
		{"default", "reserved"},
		{"unknown", "reserved"},
		{"system", "reserved"},
		{"example,com", "invalid hostname"},
		{"example", "invalid hostname"},
		{"https://example.com", "invalid hostname"},
		{"example.com/path", "invalid hostname"},
		{"user@example.com", "invalid hostname"},
		{"example.com:443", "invalid hostname"},
		{"-example.com", "invalid hostname"},
		{"example-.com", "invalid hostname"},
		{".example.com", "invalid hostname"},
		{"example..com", "invalid hostname"},
	}
	for _, tc := range bad {
		err := settings.ValidateOverrideDomain(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
			t.Fatalf("ValidateOverrideDomain(%q)=%v want substring %q", tc.in, err, tc.wantSub)
		}
	}
}

func TestDefaultDomainRowSeeded(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	lim, err := settings.DefaultLimits(d)
	if err != nil {
		t.Fatal(err)
	}
	if lim.TaskCooldownSeconds != 30 || lim.DownloadRateLimit != "10M" || lim.SleepRequests != 1 {
		t.Fatalf("seeded default %+v", lim)
	}
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM domains WHERE domain = ?`, settings.DomainDefault).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("default row count %d", n)
	}
}

func TestSetDomainDefaultRejectsEmpty(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	if err := settings.SetDomainDefault(d, 10, 8, 1, "", "1", false); err == nil {
		t.Fatal("expected empty rate rejected")
	}
	if err := settings.SetDomainDefault(d, 10, 8, 1, "5M", "", false); err == nil {
		t.Fatal("expected empty sleep rejected")
	}
	if err := settings.SetDomainDefault(d, 45, 8, 1, "2M", "4", false); err != nil {
		t.Fatal(err)
	}
	lim, _ := settings.DefaultLimits(d)
	if lim.TaskCooldownSeconds != 45 || lim.DownloadRateLimit != "2M" || lim.SleepRequests != 4 {
		t.Fatalf("after set %+v", lim)
	}
}

func TestLimitsForDomainInheritsDefault(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 12, 8, 1, "3M", "2", false)
	lim, err := settings.LimitsForDomain(d, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if lim.TaskCooldownSeconds != 12 || lim.DownloadRateLimit != "3M" || lim.SleepRequests != 2 {
		t.Fatalf("no host row should use default %+v", lim)
	}
}
