package library

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// SaveVideoMetadataParams updates editable episode metadata.
type SaveVideoMetadataParams struct {
	Title         string
	Plot          string // stored in description
	SortTitle     string
	OriginalTitle string
	Studio        string
	Genres        []string
	Tags          []string
	UniqueIDType  string
	UniqueIDValue string
	Actors        []SeriesActor
	Tagline       string
	Country       string
	MPAA          string
}

// SaveVideoMetadata writes DB fields and rewrites episode NFO.
// Does not rename media paths or change remote_id / source_url.
// Sidecar refresh is a separate task (EnqueueRefreshSidecarsVideo).
func (s *Store) SaveVideoMetadata(videoID int64, p SaveVideoMetadataParams) error {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = v.Title
	}
	sortTitle := omitWhenEqualTitle(p.SortTitle, title)
	origTitle := omitWhenEqualTitle(p.OriginalTitle, title)
	uidType, uidVal := coalesceUniqueID(p.UniqueIDType, p.UniqueIDValue, v.UniqueIDType, v.UniqueIDValue)
	_, err = s.DB.SQL.Exec(`
		UPDATE videos SET
		  title = ?, description = ?,
		  sorttitle = ?, originaltitle = ?, studio = ?,
		  genres = ?, tags = ?, uniqueid_type = ?, uniqueid_value = ?,
		  actors = ?, tagline = ?, country = ?, mpaa = ?
		WHERE id = ?
	`, title, strings.TrimSpace(p.Plot),
		sortTitle, origTitle, strings.TrimSpace(p.Studio),
		encodeStringSlice(p.Genres), encodeStringSlice(p.Tags),
		uidType, uidVal,
		encodeActors(p.Actors), strings.TrimSpace(p.Tagline), strings.TrimSpace(p.Country),
		strings.TrimSpace(p.MPAA), videoID)
	if err != nil {
		return err
	}
	_, err = s.RewriteVideoNFO(videoID, 0)
	return err
}

// VideoPrefetchDraft is ephemeral form fill data (not persisted on video until Save).
type VideoPrefetchDraft struct {
	Title         string        `json:"title"`
	Plot          string        `json:"plot"`
	SortTitle     string        `json:"sorttitle"`
	OriginalTitle string        `json:"originaltitle"`
	Studio        string        `json:"studio"`
	UniqueIDType  string        `json:"uniqueid_type"`
	UniqueIDValue string        `json:"uniqueid_value"`
	Actors        []SeriesActor `json:"actors"`
	Tagline       string        `json:"tagline"`
	Country       string        `json:"country"`
	MPAA          string        `json:"mpaa"`
	Genres        []string      `json:"genres"`
	Tags          []string      `json:"tags"`
	ThumbnailURL  string        `json:"thumbnail_url,omitempty"`
	Error         string        `json:"error,omitempty"`
}

func (s *Store) videoPrefetchDraftPath(videoID, taskID int64) string {
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("data", "cache")
	}
	return filepath.Join(root, "video-meta", strconv.FormatInt(videoID, 10),
		fmt.Sprintf("prefetch-%d.json", taskID))
}

// WriteVideoPrefetchDraft stores a draft JSON under cache.
func (s *Store) WriteVideoPrefetchDraft(videoID, taskID int64, draft VideoPrefetchDraft) error {
	path := s.videoPrefetchDraftPath(videoID, taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ReadVideoPrefetchDraft loads a draft if present.
func (s *Store) ReadVideoPrefetchDraft(videoID, taskID int64) (VideoPrefetchDraft, error) {
	var draft VideoPrefetchDraft
	b, err := os.ReadFile(s.videoPrefetchDraftPath(videoID, taskID))
	if err != nil {
		return draft, err
	}
	err = json.Unmarshal(b, &draft)
	return draft, err
}

// EnqueueVideoMetaPrefetch queues an ephemeral resolve for the metadata form.
func (s *Store) EnqueueVideoMetaPrefetch(videoID int64, fetchURL string) (int64, error) {
	v, err := s.GetVideo(videoID)
	if err != nil {
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
		Kind:     queue.KindPrefetchVideoMeta,
		Domain:   domain,
		SeriesID: v.SeriesID,
		VideoID:  videoID,
		Payload: map[string]any{
			"url": fetchURL,
		},
		Message: "Fetch video metadata",
	})
}

// BuildVideoPrefetchDraftFromEntry maps a resolve entry into form draft fields.
func BuildVideoPrefetchDraftFromEntry(e ytdlp.Entry) VideoPrefetchDraft {
	draft := VideoPrefetchDraft{
		Title:        strings.TrimSpace(e.Title),
		Plot:         e.Description,
		ThumbnailURL: strings.TrimSpace(e.ThumbnailURL),
	}
	if e.ID != "" {
		draft.UniqueIDType = "yt-dlp"
		draft.UniqueIDValue = e.ID
	}
	return draft
}

// LatestVideoMetaPrefetchTaskID returns the newest prefetch_video_meta task for a video, if any.
func (s *Store) LatestVideoMetaPrefetchTaskID(videoID int64) (int64, bool, error) {
	var id int64
	err := s.DB.SQL.QueryRow(`
		SELECT id FROM tasks
		WHERE kind = ? AND video_id = ?
		ORDER BY id DESC LIMIT 1
	`, queue.KindPrefetchVideoMeta, videoID).Scan(&id)
	if err != nil {
		return 0, false, nil
	}
	return id, true, nil
}
