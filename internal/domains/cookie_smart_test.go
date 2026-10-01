package domains_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestSmartCookiesDefaultOff(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := domains.EnsureHost(database, "example.com"); err != nil {
		t.Fatal(err)
	}
	on, err := domains.SmartCookies(database, "example.com")
	if err != nil || on {
		t.Fatalf("default smart_cookies: on=%v err=%v", on, err)
	}
	if err := domains.SetSmartCookies(database, "example.com", true); err != nil {
		t.Fatal(err)
	}
	on, err = domains.SmartCookies(database, "example.com")
	if err != nil || !on {
		t.Fatalf("after set: on=%v err=%v", on, err)
	}
}

func TestCookieAttachStatus_SplitAndUsed(t *testing.T) {
	st := domains.CookieAttachStatus{State: domains.CookieAttachRetried, RetryReason: "AgeRestricted", Smart: true}
	if !st.SplitDownloadAttempts() {
		t.Fatal("smart retried should split")
	}
	if st.CookieUsedNote() != "Cookies used" {
		t.Fatalf("note=%q", st.CookieUsedNote())
	}
	always := domains.CookieAttachStatus{State: domains.CookieAttachCookies, Smart: false}
	if always.SplitDownloadAttempts() {
		t.Fatal("always-jar should not split")
	}
	anon := domains.CookieAttachStatus{State: domains.CookieAttachAnonymous, Smart: true}
	if anon.CookieUsedNote() != "" {
		t.Fatal("anon should not show cookies used")
	}
}

func TestDecideAttach_ProbeEveryEighth(t *testing.T) {
	cf, probe, n := domains.DecideAttach(false, 0)
	if cf || probe || n != 0 {
		t.Fatalf("anon-first: cf=%v probe=%v n=%d", cf, probe, n)
	}
	cf, probe, n = domains.DecideAttach(true, 0)
	if !cf || probe || n != 1 {
		t.Fatalf("prefer first: cf=%v probe=%v n=%d", cf, probe, n)
	}
	cf, probe, n = domains.DecideAttach(true, 7)
	if cf || !probe || n != 8 {
		t.Fatalf("8th probe: cf=%v probe=%v n=%d", cf, probe, n)
	}
}

func TestApplyOutcome_PreferAndRecover(t *testing.T) {
	var st domains.CookieSmartState
	for i := 0; i < 4; i++ {
		st = domains.ApplyOutcome(st, domains.CookieOutcomeFallback)
	}
	if !st.Prefer {
		t.Fatal("want prefer after 4 fallbacks")
	}
	for i := 0; i < 8; i++ {
		st = domains.ApplyOutcome(st, domains.CookieOutcomeCookiesOK)
	}
	if !st.Prefer {
		t.Fatal("cookies_ok must not clear prefer")
	}
	st = domains.ApplyOutcome(st, domains.CookieOutcomeProbeAnonOK)
	st = domains.ApplyOutcome(st, domains.CookieOutcomeProbeAnonOK)
	if st.Prefer {
		t.Fatal("want prefer cleared after 2 proven-anon")
	}
}

func openLibWithSource(t *testing.T) (*db.DB, int64) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	q := queue.NewStore(database)
	store := library.NewStore(database, q)
	root, err := store.CreateRoot("r", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := store.CreateProfile("p", "bv*")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := store.CreateSeries(library.CreateSeriesParams{
		Title: "S", SourceURL: "https://example.com/c/x", RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ser.Sources) == 0 {
		t.Fatal("no source")
	}
	return database, ser.Sources[0].ID
}

func TestCookieSmartRecordAndWipe(t *testing.T) {
	database, srcID := openLibWithSource(t)
	if err := domains.EnsureHost(database, "example.com"); err != nil {
		t.Fatal(err)
	}
	if err := domains.SetCookies(database, "example.com", "# Netscape\n.example.com\tTRUE\t/\tFALSE\t0\ta\tb\n"); err != nil {
		t.Fatal(err)
	}
	if err := domains.SetSmartCookies(database, "example.com", true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := domains.RecordCookieSmartOutcome(database, srcID, domains.CookieOutcomeFallback); err != nil {
			t.Fatal(err)
		}
	}
	st, err := domains.LoadCookieSmart(database, srcID)
	if err != nil || !st.Prefer {
		t.Fatalf("prefer=%v err=%v ring=%v", st.Prefer, err, st.Ring)
	}
	if err := domains.SetSmartCookies(database, "example.com", false); err != nil {
		t.Fatal(err)
	}
	st, err = domains.LoadCookieSmart(database, srcID)
	if err != nil || st.Prefer || len(st.Ring) != 0 {
		t.Fatalf("after wipe: %+v err=%v", st, err)
	}
	if err := domains.SetSmartCookies(database, "example.com", true); err != nil {
		t.Fatal(err)
	}
	_ = domains.RecordCookieSmartOutcome(database, srcID, domains.CookieOutcomeFallback)
	if err := domains.SetCookies(database, "example.com", "# Netscape\n.example.com\tTRUE\t/\tFALSE\t0\ta\tchanged\n"); err != nil {
		t.Fatal(err)
	}
	st, err = domains.LoadCookieSmart(database, srcID)
	if err != nil || len(st.Ring) != 0 {
		t.Fatalf("jar overwrite wipe: %+v", st)
	}
}

func TestClaimCookieSmart_PreferCookies(t *testing.T) {
	database, srcID := openLibWithSource(t)
	st := domains.CookieSmartState{Prefer: true, N: 0}
	if err := domains.SaveCookieSmart(database, srcID, st); err != nil {
		t.Fatal(err)
	}
	cf, probe, prefer, err := domains.ClaimCookieSmart(database, srcID, true)
	if err != nil || !cf || probe || !prefer {
		t.Fatalf("cf=%v probe=%v prefer=%v err=%v", cf, probe, prefer, err)
	}
	st, _ = domains.LoadCookieSmart(database, srcID)
	if st.N != 1 {
		t.Fatalf("n=%d", st.N)
	}
}
