package sponsorblock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AppliedCutPlan is persisted next to media so sidecar refresh cannot drift.
type AppliedCutPlan struct {
	Version         int               `json:"version"`
	VideoID         string            `json:"video_id,omitempty"`
	FetchedAt       string            `json:"fetched_at,omitempty"`
	Segments        []AppliedSegment  `json:"segments"`
	InfoCards       bool              `json:"info_cards"`
	CardDurationSec float64           `json:"card_duration_sec"`
	SourceDuration  float64           `json:"source_duration_sec,omitempty"`
	Hash            string            `json:"hash"`
}

// AppliedSegment is one cut that was applied (or will be at play for stream).
type AppliedSegment struct {
	UUID     string  `json:"uuid,omitempty"`
	Category string  `json:"category"`
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
}

// PlanFromCuts builds an applied-cut plan from filtered remove segments.
func PlanFromCuts(videoID string, cuts []Segment, infoCards bool, cardDur, sourceDur float64) AppliedCutPlan {
	if cardDur <= 0 {
		cardDur = DefaultCardDurationSec
	}
	segs := make([]AppliedSegment, 0, len(cuts))
	for _, c := range cuts {
		segs = append(segs, AppliedSegment{
			UUID:     c.UUID,
			Category: c.Category,
			Start:    c.Start,
			End:      c.End,
		})
	}
	p := AppliedCutPlan{
		Version:         1,
		VideoID:         videoID,
		FetchedAt:       time.Now().UTC().Format(time.RFC3339),
		Segments:        segs,
		InfoCards:       infoCards,
		CardDurationSec: cardDur,
		SourceDuration:  sourceDur,
	}
	p.Hash = p.computeHash()
	return p
}

func (p AppliedCutPlan) computeHash() string {
	type wire struct {
		Segments        []AppliedSegment `json:"segments"`
		InfoCards       bool             `json:"info_cards"`
		CardDurationSec float64          `json:"card_duration_sec"`
	}
	b, _ := json.Marshal(wire{p.Segments, p.InfoCards, p.CardDurationSec})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// Cuts returns Segment slice for MapTime / KeepRanges.
func (p AppliedCutPlan) Cuts() []Segment {
	out := make([]Segment, 0, len(p.Segments))
	for _, s := range p.Segments {
		out = append(out, Segment{
			UUID:     s.UUID,
			Category: s.Category,
			Start:    s.Start,
			End:      s.End,
		})
	}
	return out
}

// WritePlan writes plan JSON next to mediaPath (basename.sponsorblock.json).
func WritePlan(mediaPath string, p AppliedCutPlan) (string, error) {
	path := PlanPath(mediaPath)
	if p.Hash == "" {
		p.Hash = p.computeHash()
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// PlanPath returns the sidecar path for a media basename.
func PlanPath(mediaPath string) string {
	ext := filepath.Ext(mediaPath)
	base := stringsTrimSuffix(mediaPath, ext)
	return base + ".sponsorblock.json"
}

func stringsTrimSuffix(s, suf string) string {
	if len(suf) > 0 && len(s) >= len(suf) && s[len(s)-len(suf):] == suf {
		return s[:len(s)-len(suf)]
	}
	return s
}

// ReadPlan loads an applied-cut plan if present.
func ReadPlan(mediaPath string) (AppliedCutPlan, bool, error) {
	path := PlanPath(mediaPath)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AppliedCutPlan{}, false, nil
		}
		return AppliedCutPlan{}, false, err
	}
	var p AppliedCutPlan
	if err := json.Unmarshal(b, &p); err != nil {
		return AppliedCutPlan{}, false, fmt.Errorf("sponsorblock plan: %w", err)
	}
	return p, true, nil
}

// HasCuts reports whether the plan removes any time.
func (p AppliedCutPlan) HasCuts() bool {
	return len(p.Segments) > 0
}
