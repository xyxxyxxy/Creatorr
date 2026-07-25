package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

const sponsorblockCutStageDir = "sponsorblock-cut"

// SponsorblockCutPayload is stored on sponsorblock_cut tasks.
type SponsorblockCutPayload struct {
	VideoID                int64    `json:"video_id"`
	StageDir               string   `json:"stage_dir"`
	MediaPath              string   `json:"media_path"`
	InfoPath               string   `json:"info_path,omitempty"`
	ThumbPath              string   `json:"thumb_path,omitempty"`
	SubPaths               []string `json:"sub_paths,omitempty"`
	PageURL                string   `json:"page_url,omitempty"`
	RemoteID               string   `json:"remote_id,omitempty"`
	Maturity               bool     `json:"maturity,omitempty"`
	FormatSelector         string   `json:"format_selector,omitempty"`
	SeriesTitle            string   `json:"series_title,omitempty"`
	VideoTitle             string   `json:"video_title,omitempty"`
	Description            string   `json:"description,omitempty"`
	Aired                  string   `json:"aired,omitempty"`
	Season                 int      `json:"season,omitempty"`
	Episode                int      `json:"episode,omitempty"`
	SeriesID               int64    `json:"series_id,omitempty"`
	RootPath               string   `json:"root_path,omitempty"`
	NamingDomain           string   `json:"naming_domain,omitempty"`
}

// SponsorblockCutStageDir returns {CacheDir}/sponsorblock-cut/{videoID}/.
func (s *Store) SponsorblockCutStageDir(videoID int64) string {
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join(os.TempDir(), "creatorr-cache")
	}
	return filepath.Join(root, sponsorblockCutStageDir, strconv.FormatInt(videoID, 10))
}

// RemoveSponsorblockCutStaging deletes staging for one video (best-effort).
func (s *Store) RemoveSponsorblockCutStaging(videoID int64) {
	if videoID <= 0 {
		return
	}
	_ = os.RemoveAll(s.SponsorblockCutStageDir(videoID))
}

// ParseSponsorblockCutPayload reads cut task payload JSON.
func ParseSponsorblockCutPayload(raw string) (SponsorblockCutPayload, error) {
	var p SponsorblockCutPayload
	if strings.TrimSpace(raw) == "" {
		return p, fmt.Errorf("%w: empty sponsorblock_cut payload", ErrInvalid)
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return p, fmt.Errorf("%w: sponsorblock_cut payload: %v", ErrInvalid, err)
	}
	if p.VideoID <= 0 || p.MediaPath == "" {
		return p, fmt.Errorf("%w: sponsorblock_cut payload missing video_id or media_path", ErrInvalid)
	}
	return p, nil
}

// StageSponsorblockCut moves media + sidecars into durable staging and returns payload paths.
func (s *Store) StageSponsorblockCut(videoID int64, media string, info, thumb string, subs []string) (SponsorblockCutPayload, error) {
	stage := s.SponsorblockCutStageDir(videoID)
	_ = os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return SponsorblockCutPayload{}, err
	}
	moveInto := func(src string) (string, error) {
		src = strings.TrimSpace(src)
		if src == "" {
			return "", nil
		}
		base := filepath.Base(src)
		dst := filepath.Join(stage, base)
		if err := os.Rename(src, dst); err != nil {
			// Cross-device: copy then remove.
			data, rerr := os.ReadFile(src)
			if rerr != nil {
				return "", err
			}
			if werr := os.WriteFile(dst, data, 0o644); werr != nil {
				return "", werr
			}
			_ = os.Remove(src)
		}
		return dst, nil
	}
	mediaDst, err := moveInto(media)
	if err != nil {
		_ = os.RemoveAll(stage)
		return SponsorblockCutPayload{}, err
	}
	infoDst, err := moveInto(info)
	if err != nil {
		_ = os.RemoveAll(stage)
		return SponsorblockCutPayload{}, err
	}
	thumbDst, err := moveInto(thumb)
	if err != nil {
		_ = os.RemoveAll(stage)
		return SponsorblockCutPayload{}, err
	}
	var subDsts []string
	for _, sp := range subs {
		d, err := moveInto(sp)
		if err != nil {
			_ = os.RemoveAll(stage)
			return SponsorblockCutPayload{}, err
		}
		if d != "" {
			subDsts = append(subDsts, d)
		}
	}
	return SponsorblockCutPayload{
		VideoID:   videoID,
		StageDir:  stage,
		MediaPath: mediaDst,
		InfoPath:  infoDst,
		ThumbPath: thumbDst,
		SubPaths:  subDsts,
	}, nil
}

// EnqueueSponsorblockCut queues a low-priority system-lane cut/pack task.
func (s *Store) EnqueueSponsorblockCut(p SponsorblockCutPayload) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	if p.VideoID <= 0 || p.MediaPath == "" {
		return 0, fmt.Errorf("%w: sponsorblock_cut requires video and media", ErrInvalid)
	}
	payload := map[string]any{
		"video_id":         p.VideoID,
		"stage_dir":        p.StageDir,
		"media_path":       p.MediaPath,
		"info_path":        p.InfoPath,
		"thumb_path":       p.ThumbPath,
		"sub_paths":        p.SubPaths,
		"page_url":         p.PageURL,
		"remote_id":        p.RemoteID,
		"maturity":         p.Maturity,
		"format_selector":  p.FormatSelector,
		"series_title":     p.SeriesTitle,
		"video_title":      p.VideoTitle,
		"description":      p.Description,
		"aired":            p.Aired,
		"season":           p.Season,
		"episode":          p.Episode,
		"series_id":        p.SeriesID,
		"root_path":        p.RootPath,
		"naming_domain":    p.NamingDomain,
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindSponsorblockCut,
		Domain:   queue.SystemDomain,
		SeriesID: p.SeriesID,
		VideoID:  p.VideoID,
		Priority: queue.PrioritySponsorblockCut,
		Message:  "SponsorBlock cut",
		Payload:  payload,
	})
}
