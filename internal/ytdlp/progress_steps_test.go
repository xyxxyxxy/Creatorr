package ytdlp

import (
	"strings"
	"testing"
)

func TestStepProgressDualFormatWithDest(t *testing.T) {
	var s StepProgress
	lines := []string{
		"[info] abc: Downloading 2 format(s): 96+95",
		"[download] Destination: /tmp/Title [abc].f96.mp4",
		"[download]  42.0% of 10.00MiB at 1.00MiB/s ETA 00:05",
		"[download] 100% of 10.00MiB in 00:10",
		"[download] Destination: /tmp/Title [abc].f95.m4a",
		"[download]   0.0% of 1.00MiB at 500.00KiB/s ETA 00:02",
		"[download]  50.0% of 1.00MiB at 500.00KiB/s ETA 00:01",
		"[Merger] Merging formats into \"/tmp/Title [abc].mp4\"",
	}
	var msgs []string
	var fracs []*float64
	for _, line := range lines {
		msg, frac, ok := s.Feed(line)
		if !ok {
			continue
		}
		msgs = append(msgs, msg)
		fracs = append(fracs, frac)
	}
	wantMsgs := []string{
		"Downloading video (1/2)",
		"Downloading video (1/2) 42%",
		"Downloading video (1/2) 100%",
		"Downloading audio (2/2)",
		"Downloading audio (2/2)",
		"Downloading audio (2/2) 50%",
		"Merging…",
	}
	if len(msgs) != len(wantMsgs) {
		t.Fatalf("got %d emits %v, want %d %v", len(msgs), msgs, len(wantMsgs), wantMsgs)
	}
	for i := range wantMsgs {
		if msgs[i] != wantMsgs[i] {
			t.Fatalf("msg[%d] = %q, want %q", i, msgs[i], wantMsgs[i])
		}
	}
	if fracs[0] != nil {
		t.Fatalf("Destination should be busy (nil frac), got %v", *fracs[0])
	}
	if fracs[3] != nil {
		t.Fatalf("step2 Destination should be nil, got %v", *fracs[3])
	}
	if fracs[4] != nil {
		t.Fatalf("step2 0%% should stay busy nil, got %v", *fracs[4])
	}
	if fracs[5] == nil || *fracs[5] < 0.49 || *fracs[5] > 0.51 {
		t.Fatalf("step2 mid = %v", fracs[5])
	}
	if fracs[6] != nil {
		t.Fatalf("Merging should be message-only, got %v", *fracs[6])
	}
}

func TestStepProgressPercentResetWithoutDest(t *testing.T) {
	var s StepProgress
	_, _, _ = s.Feed("[info] x: Downloading 2 format(s): a+b")
	_, f1, ok1 := s.Feed("[download] 100%")
	if !ok1 || f1 == nil || *f1 != 1 {
		t.Fatalf("first end: ok=%v frac=%v", ok1, f1)
	}
	msg, f2, ok2 := s.Feed("[download] 0%")
	if !ok2 {
		t.Fatal("expected second start")
	}
	// New step at 0%: busy (nil), not a determinate 0% flip.
	if f2 != nil {
		t.Fatalf("second start frac = %v, want nil busy", *f2)
	}
	if msg != "Downloading audio (2/2)" {
		t.Fatalf("msg = %q", msg)
	}
}

func TestStepProgressFragmentDoesNotRewind(t *testing.T) {
	var s StepProgress
	_, _, _ = s.Feed("[download] Destination: /tmp/x.mp4")
	_, f1, _ := s.Feed("[download]  40.0%")
	if f1 == nil || *f1 < 0.39 {
		t.Fatalf("peak = %v", f1)
	}
	msg, f2, ok := s.Feed("[download]   5.0%")
	if !ok || f2 == nil || *f2 < 0.39 {
		t.Fatalf("rewind emitted %v", f2)
	}
	if !strings.Contains(msg, "40%") {
		t.Fatalf("msg = %q, want peak %%", msg)
	}
}

func TestStepProgressSingleFormat(t *testing.T) {
	var s StepProgress
	msg, frac, ok := s.Feed("[download]  10.0% of ~100.00MiB")
	if !ok || frac == nil || *frac < 0.09 || *frac > 0.11 {
		t.Fatalf("ok=%v frac=%v", ok, frac)
	}
	if msg != "Downloading 10%" {
		t.Fatalf("msg = %q, want no step label for unknown single", msg)
	}
}

func TestStepProgressWrongCountUsesIdList(t *testing.T) {
	var s StepProgress
	_, _, _ = s.Feed("[info] abc: Downloading 1 format(s): 96+95")
	if s.total != 2 {
		t.Fatalf("total = %d, want 2 from id list", s.total)
	}
	msg, frac, ok := s.Feed("[download] Destination: /tmp/x.f96.mp4")
	if !ok || frac != nil || msg != "Downloading video (1/2)" {
		t.Fatalf("msg = %q frac=%v", msg, frac)
	}
}

func TestStepProgressSingleFormatDeclared(t *testing.T) {
	var s StepProgress
	_, _, _ = s.Feed("[info] abc: Downloading 1 format(s): 96")
	msg0, frac0, ok0 := s.Feed("[download] Destination: /tmp/Title [abc].f96.mp4")
	if !ok0 || frac0 != nil || msg0 != "Downloading…" {
		t.Fatalf("dest msg=%q frac=%v", msg0, frac0)
	}
	msg, frac, ok := s.Feed("[download]  72.0% of 10.00MiB")
	if !ok || frac == nil {
		t.Fatal("expected progress")
	}
	if msg != "Downloading 72%" {
		t.Fatalf("msg = %q, want no (1/1) and no video role", msg)
	}
}
