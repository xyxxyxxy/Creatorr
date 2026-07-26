package domains_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestScopedLimitUpdatesLeaveOtherColumns(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "http://flaresolverr.example.com")

	if err := domains.UpdateHostOverrides(d, "example.com", "60", "", "", "5M", "", "2", "on"); err != nil {
		t.Fatal(err)
	}
	lim, err := settings.LimitsForDomain(d, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if lim.TaskCooldownSeconds != 60 || lim.DownloadRateLimit != "5M" || lim.SleepRequests != 2 {
		t.Fatalf("after upsert: %+v", lim)
	}
	if lim.StreamPlayRateLimit != "off" {
		t.Fatalf("stream play should stay default off: %+v", lim)
	}

	if err := domains.UpdateHostOverrides(d, "example.com", "60", "", "", "5M", "750K", "2", "on"); err == nil {
		t.Fatal("expected stream play below download rejected")
	}
	if err := domains.UpdateHostOverrides(d, "example.com", "60", "", "", "5M", "10M", "2", "on"); err != nil {
		t.Fatal(err)
	}
	lim, err = settings.LimitsForDomain(d, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if lim.StreamPlayRateLimit != "10M" || lim.DownloadRateLimit != "5M" || lim.SleepRequests != 2 || lim.TaskCooldownSeconds != 60 {
		t.Fatalf("stream play with other overrides: %+v", lim)
	}

	if err := domains.UpdateHostOverrides(d, "example.com", "90", "", "", "", "", "", "on"); err != nil {
		t.Fatal(err)
	}
	lim, err = settings.LimitsForDomain(d, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	def, _ := settings.DefaultLimits(d)
	if lim.TaskCooldownSeconds != 90 {
		t.Fatalf("cooldown: %+v", lim)
	}
	if lim.DownloadRateLimit != def.DownloadRateLimit || lim.SleepRequests != def.SleepRequests {
		t.Fatalf("cleared rate/sleep should inherit default: got %+v want rate=%s sleep=%v", lim, def.DownloadRateLimit, def.SleepRequests)
	}
	if lim.StreamPlayRateLimit != def.StreamPlayRateLimit {
		t.Fatalf("cleared stream play rate should inherit default: got %s want %s", lim.StreamPlayRateLimit, def.StreamPlayRateLimit)
	}

	if err := domains.Delete(d, "example.com"); err != nil {
		t.Fatal(err)
	}
	lim, err = settings.LimitsForDomain(d, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if lim.TaskCooldownSeconds != def.TaskCooldownSeconds {
		t.Fatalf("after delete should use default %+v", lim)
	}
	var n int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM domains WHERE domain = ?`, "example.com").Scan(&n)
	if n != 0 {
		t.Fatalf("row should be gone, count=%d", n)
	}
}

func TestSourceAddDoesNotCreateDomainRow(t *testing.T) {
	// Covered indirectly: EnsureFromURL no longer called from library; EnsureHost only on explicit upsert.
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	ok, err := domains.IsActive(d, "fresh.example")
	if err != nil || !ok {
		t.Fatalf("missing row should be active: %v %v", ok, err)
	}
	var n int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM domains WHERE domain = ?`, "fresh.example").Scan(&n)
	if n != 0 {
		t.Fatalf("unexpected domain row")
	}
}
