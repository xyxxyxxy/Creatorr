package domains_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestSnapshotDomainAccessSkipsSystem(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	snap, err := domains.SnapshotDomainAccess(d, "system")
	if err != nil || snap != nil {
		t.Fatalf("system: snap=%v err=%v", snap, err)
	}
}

func TestSnapshotDomainAccessHost(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "http://flaresolverr.example.com")
	if err := domains.EnsureHost(d, "example.com"); err != nil {
		t.Fatal(err)
	}
	if err := domains.UpdateHostOverrides(d, "example.com", "", "", "", "5M", "2", "on"); err != nil {
		t.Fatal(err)
	}
	snap, err := domains.SnapshotDomainAccess(d, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if snap.RateLimit != "5M" || !snap.RateOverride {
		t.Fatalf("rate: %+v", snap)
	}
	if snap.SleepRequests != 2 || !snap.SleepOverride {
		t.Fatalf("sleep: %+v", snap)
	}
	if !snap.Flare || !snap.FlareOverride {
		t.Fatalf("flare: %+v", snap)
	}
}
