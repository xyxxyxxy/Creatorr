package domains_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// gatedInvokeFailAnon counts yt-dlp-like invokes: empty cookies fail, jar succeeds.
func gatedInvokeFailAnon(calls *[]string) func(string) (string, error) {
	return func(path string) (string, error) {
		*calls = append(*calls, path)
		if path == "" {
			return "", apperrors.New(apperrors.CodeAgeRestricted, "age")
		}
		return "ok", nil
	}
}

func TestSmartCookieLearnThenPreferInvokeCount(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
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
	srcID := ser.Sources[0].ID
	if err := domains.EnsureHost(database, "example.com"); err != nil {
		t.Fatal(err)
	}
	jarText := "# Netscape\n.example.com\tTRUE\t/\tFALSE\t0\ta\tb\n"
	if err := domains.SetCookies(database, "example.com", jarText); err != nil {
		t.Fatal(err)
	}
	if err := domains.SetSmartCookies(database, "example.com", true); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	var totalCalls int
	runOnce := func() {
		var calls []string
		smart, err := domains.SmartCookiesForURL(database, "https://example.com/v")
		if err != nil {
			t.Fatal(err)
		}
		cookiesFirst, probe, prefer, err := domains.ClaimCookieSmart(database, srcID, smart)
		if err != nil {
			t.Fatal(err)
		}
		anonFirst := smart && !cookiesFirst
		jar, hadStored, err := domains.StoredJarForURL(database, work, "https://example.com/v", domains.AllowStoredJar(anonFirst, false))
		if err != nil {
			t.Fatal(err)
		}
		_, attach, err := domains.InvokeWithCookieFallback(anonFirst, hadStored, jar, func() (string, error) {
			path, _, e := domains.StoredJarForURL(database, work, "https://example.com/v", true)
			return path, e
		}, gatedInvokeFailAnon(&calls), nil)
		if err != nil {
			t.Fatal(err)
		}
		attach.Smart = smart
		attach.PreferCookies = prefer
		attach.Probe = probe
		if outcome := domains.OutcomeFromAttach(attach, probe); outcome != "" {
			_ = domains.RecordCookieSmartOutcome(database, srcID, outcome)
		}
		totalCalls += len(calls)
	}

	// Learn: 4× anon fail + cookie success = 8 invokes
	for i := 0; i < 4; i++ {
		runOnce()
	}
	if totalCalls != 8 {
		t.Fatalf("learn invokes=%d want 8", totalCalls)
	}
	st, _ := domains.LoadCookieSmart(database, srcID)
	if !st.Prefer {
		t.Fatal("want prefer after learn")
	}
	// Prefer cookies: single invoke
	before := totalCalls
	runOnce()
	if totalCalls-before != 1 {
		t.Fatalf("prefer cookies invokes=%d want 1 (detail %s)", totalCalls-before, fmt.Sprint(st))
	}
}
