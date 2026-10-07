package web

import "testing"

func TestBuildSourceListMeta(t *testing.T) {
	cases := []struct {
		name    string
		ind     sourceStatusView
		summary string
		ignored bool
		want    string
	}{
		{
			name:    "queued ignored",
			ind:     sourceStatusView{SourceID: 1, Kind: "pending", Label: "queued", Href: "/task/9"},
			ignored: true,
			want:    "scan queued|new videos added as ignored|/task/9",
		},
		{
			name:    "last scan wanted",
			ind:     sourceStatusView{SourceID: 2, Kind: "scheduled", Label: "2h ago (1 new)"},
			summary: "2h ago (1 new)",
			want:    "last scan 2h ago (1 new)|new videos added as wanted|",
		},
		{
			name: "schedule off",
			ind:  sourceStatusView{SourceID: 3, Kind: "unscheduled", Label: "disabled"},
			want: "scan schedule off|new videos added as wanted|",
		},
		{
			name: "incomplete",
			ind:  sourceStatusView{SourceID: 4, Kind: "incomplete", Label: "incomplete"},
			want: "full scan incomplete|new videos added as wanted|",
		},
		{
			name:    "never scanned",
			ind:     sourceStatusView{SourceID: 5, Kind: "scheduled", Label: "indexed"},
			summary: "never",
			want:    "never scanned|new videos added as wanted|",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildSourceListMeta(tc.ind, tc.summary, tc.ignored)
			got := m.Scan + "|" + m.Discovered + "|" + m.Href
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			if m.SourceID != tc.ind.SourceID {
				t.Fatalf("SourceID %d", m.SourceID)
			}
		})
	}
}
