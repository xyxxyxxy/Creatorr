package library

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// EnqueueSeriesMetaPrefetch queues an ephemeral yt-dlp metadata fetch for the form.
func (s *Store) EnqueueSeriesMetaPrefetch(seriesID int64, fetchURL string) (int64, error) {
	if _, err := s.GetSeries(seriesID, false); err != nil {
		return 0, err
	}
	fetchURL = strings.TrimSpace(fetchURL)
	if fetchURL == "" {
		return 0, fmt.Errorf("%w: url required", ErrInvalid)
	}
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	domain := "unknown"
	if u, err := url.Parse(fetchURL); err == nil && u.Hostname() != "" {
		domain = settings.NormalizeDomain(u.Hostname())
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindPrefetchSeriesMeta,
		Domain:   domain,
		SeriesID: seriesID,
		Payload: map[string]any{
			"url": fetchURL,
		},
		Message: "Fetch series metadata",
	})
}

// EnqueueAddSeriesPrefetch queues yt-dlp metadata for Add series (no series row yet).
// draftToken must be a non-empty opaque id for cache/add-series/{token}/.
func (s *Store) EnqueueAddSeriesPrefetch(sourceURL, draftToken string) (int64, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	draftToken = strings.TrimSpace(draftToken)
	if sourceURL == "" {
		return 0, fmt.Errorf("%w: url required", ErrInvalid)
	}
	if draftToken == "" || strings.Contains(draftToken, "/") || strings.Contains(draftToken, "..") {
		return 0, fmt.Errorf("%w: draft token", ErrInvalid)
	}
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	domain := "unknown"
	if u, err := url.Parse(sourceURL); err == nil && u.Hostname() != "" {
		domain = settings.NormalizeDomain(u.Hostname())
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:   queue.KindPrefetchAddSeries,
		Domain: domain,
		Payload: map[string]any{
			"url":         sourceURL,
			"draft_token": draftToken,
		},
		Message: "Fetch add-series metadata",
	})
}

// SeriesTitleFromDraft picks a display title from prefetch draft fields.
// Empty when the dump had no usable name (caller should fail the create).
func SeriesTitleFromDraft(d PrefetchDraft) string {
	// Title is preferred; OriginalTitle/SortTitle remain as fallbacks for older drafts.
	for _, t := range []string{d.Title, d.OriginalTitle, d.SortTitle, d.Studio} {
		if s := strings.TrimSpace(t); s != "" {
			return s
		}
	}
	return ""
}

// BuildPrefetchDraftFromInfo maps a yt-dlp playlist/channel dump to a form draft.
// Sets draft.Title for Add series naming; does not copy that name into sorttitle /
// originaltitle (redundant with series.title). Playlist URLs skip images.
func BuildPrefetchDraftFromInfo(info map[string]any, artDir string) PrefetchDraft {
	draft := PrefetchDraft{ArtFiles: map[string]string{}}
	if info == nil {
		return draft
	}
	title := firstString(info, "title", "playlist_title", "channel", "uploader")
	desc := firstString(info, "description")
	channelID := firstString(info, "channel_id", "uploader_id", "id")

	draft.Plot = desc
	draft.PlaylistOnly = isPlaylistOnlyInfo(info)
	if title != "" {
		draft.Title = title
	}
	if channelID != "" {
		draft.UniqueIDType = "yt-dlp"
		if ek := strings.ToLower(firstString(info, "extractor_key", "extractor")); ek != "" {
			if base, _, ok := strings.Cut(ek, ":"); ok && base != "" {
				draft.UniqueIDType = base
			} else {
				draft.UniqueIDType = ek
			}
		}
		draft.UniqueIDValue = channelID
	}
	if draft.PlaylistOnly {
		return draft
	}
	posterURL, bannerURL := pickChannelArtURLs(info)
	if posterURL != "" && artDir != "" {
		dest := filepath.Join(artDir, "poster.jpg")
		if err := downloadURLToCache(posterURL, dest); err == nil {
			draft.ArtFiles[ArtPoster] = dest
		}
	}
	if bannerURL != "" && artDir != "" {
		dest := filepath.Join(artDir, "banner.jpg")
		if err := downloadURLToCache(bannerURL, dest); err == nil {
			draft.ArtFiles[ArtBanner] = dest
		}
	}
	return draft
}

func isPlaylistOnlyInfo(info map[string]any) bool {
	ek := strings.ToLower(firstString(info, "extractor_key", "extractor"))
	if strings.Contains(ek, "playlist") {
		return true
	}
	u := firstString(info, "webpage_url", "original_url")
	if strings.Contains(u, "/playlist") {
		return true
	}
	return false
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func pickChannelArtURLs(info map[string]any) (poster, banner string) {
	thumbs, _ := info["thumbnails"].([]any)
	var bestPoster, bestBanner, anyThumb string
	var bestPosterArea, bestBannerArea float64
	for _, raw := range thumbs {
		t, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		u, _ := t["url"].(string)
		if u == "" {
			continue
		}
		if anyThumb == "" {
			anyThumb = u
		}
		id, _ := t["id"].(string)
		w, _ := toFloat(t["width"])
		h, _ := toFloat(t["height"])
		area := w * h
		lid := strings.ToLower(id)
		lurl := strings.ToLower(u)
		isBanner := strings.Contains(lid, "banner") || strings.Contains(lurl, "banner")
		isAvatar := strings.Contains(lid, "avatar") || strings.Contains(lurl, "avatar") ||
			strings.Contains(lid, "poster") || strings.Contains(lid, "channel")
		if isBanner {
			if bestBanner == "" || area >= bestBannerArea {
				bestBannerArea = area
				bestBanner = u
			}
			continue
		}
		if isAvatar {
			if bestPoster == "" || area >= bestPosterArea {
				bestPosterArea = area
				bestPoster = u
			}
			continue
		}
		// Prefer squarer images as poster fallback; wide as banner.
		if w > 0 && h > 0 {
			ratio := w / h
			if ratio > 0.7 && ratio < 1.4 && (bestPoster == "" || area >= bestPosterArea) {
				bestPosterArea = area
				bestPoster = u
			} else if ratio >= 2 && (bestBanner == "" || area >= bestBannerArea) {
				bestBannerArea = area
				bestBanner = u
			}
		}
	}
	if bestPoster == "" {
		bestPoster = firstString(info, "thumbnail")
	}
	if bestPoster == "" {
		bestPoster = anyThumb
	}
	return bestPoster, bestBanner
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
