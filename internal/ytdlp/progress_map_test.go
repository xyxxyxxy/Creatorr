package ytdlp

import "testing"

func TestProgressMapperMonotonicAcrossReset(t *testing.T) {
	m := ProgressMapper{Lo: 0.05, Hi: 0.80}
	var prev float64
	for _, raw := range []float64{0.1, 0.5, 0.95, 1.0} {
		v := m.Map(raw)
		if v < prev {
			t.Fatalf("decreased on first format: %v -> %v", prev, v)
		}
		prev = v
	}
	// Second format restarts at ~0.
	for _, raw := range []float64{0.02, 0.3, 0.6, 0.99} {
		v := m.Map(raw)
		if v+1e-9 < prev {
			t.Fatalf("bar reset on second format: prev=%v got=%v (raw=%v)", prev, v, raw)
		}
		prev = v
	}
	if prev < 0.7 {
		t.Fatalf("expected to approach Hi, got %v", prev)
	}
	fin := m.Finish()
	if fin != 0.80 {
		t.Fatalf("Finish = %v", fin)
	}
}

func TestProgressMapperStaysInRange(t *testing.T) {
	m := ProgressMapper{Lo: 0.1, Hi: 0.5}
	for _, raw := range []float64{0, 0.5, 1, 0, 1} {
		v := m.Map(raw)
		if v < m.Lo-1e-9 || v > m.Hi+1e-9 {
			t.Fatalf("out of range: %v", v)
		}
	}
}
