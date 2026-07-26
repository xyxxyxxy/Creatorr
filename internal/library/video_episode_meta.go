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

// ArtThumb is the video Metadata form art role (episode thumb beside pack).
const ArtThumb = "thumb"

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
	// ThumbSrc is a local path to copy as the episode thumb (upload or prefetch cache). Empty = leave.
	ThumbSrc string
	// ThumbClear deletes the registered thumb sidecar on disk.
	ThumbClear bool
}

// SaveVideoMetadata writes DB fields, applies optional thumb ops, and rewrites episode NFO.
// Does not rename media paths or change remote_id / source_url.
// Sidecar refresh (yt-dlp re-fetch) remains a separate task (EnqueueRefreshSidecarsVideo).
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
	if err := s.applyVideoThumbEdit(videoID, p.ThumbSrc, p.ThumbClear); err != nil {
		return err
	}
	_, err = s.RewriteVideoNFO(videoID, 0)
	return err
}

// applyVideoThumbEdit installs or clears the episode thumb beside the pack anchor.
// No-op when neither clear nor src is set. Requires a packed video/.strm.
func (s *Store) applyVideoThumbEdit(videoID int64, thumbSrc string, clear bool) error {
	thumbSrc = strings.TrimSpace(thumbSrc)
	if !clear && thumbSrc == "" {
		return nil
	}
	mediaPath, ok, err := s.HasPackAnchor(videoID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: pack media or stream before editing thumb", ErrInvalid)
	}
	if err := s.clearVideoThumbFiles(videoID, mediaPath); err != nil {
		return err
	}
	if clear {
		return nil
	}
	if !fileExists(thumbSrc) {
		return fmt.Errorf("%w: thumb source missing", ErrInvalid)
	}
	ext := strings.ToLower(filepath.Ext(thumbSrc))
	if ext == "" {
		ext = ".jpg"
	}
	stem := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	dest := stem + "-thumb" + ext
	if err := copyFile(thumbSrc, dest); err != nil {
		return fmt.Errorf("install thumb: %w", err)
	}
	return s.RegisterFileKind(videoID, dest, "thumb")
}

func (s *Store) clearVideoThumbFiles(videoID int64, mediaPath string) error {
	rows, err := s.DB.SQL.Query(`SELECT path FROM files WHERE video_id = ? AND kind = 'thumb'`, videoID)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil && strings.TrimSpace(p) != "" {
			paths = append(paths, p)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range paths {
		_ = os.Remove(p)
	}
	stem := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		_ = os.Remove(stem + "-thumb" + ext)
	}
	_, err = s.DB.SQL.Exec(`DELETE FROM files WHERE video_id = ? AND kind = 'thumb'`, videoID)
	return err
}

// VideoThumbMtime returns unix-nano mtime for the registered thumb (cache-bust query).
func (s *Store) VideoThumbMtime(videoID int64) int64 {
	path, ok, err := s.VideoThumbPath(videoID)
	if err != nil || !ok {
		return 0
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.ModTime().UnixNano()
}

// VideoPrefetchDraft is ephemeral form fill data (not persisted on video until Save).
type VideoPrefetchDraft struct {
	Title         string            `json:"title"`
	Plot          string            `json:"plot"`
	SortTitle     string            `json:"sorttitle"`
	OriginalTitle string            `json:"originaltitle"`
	Studio        string            `json:"studio"`
	UniqueIDType  string            `json:"uniqueid_type"`
	UniqueIDValue string            `json:"uniqueid_value"`
	Actors        []SeriesActor     `json:"actors"`
	Tagline       string            `json:"tagline"`
	Country       string            `json:"country"`
	MPAA          string            `json:"mpaa"`
	Genres        []string          `json:"genres"`
	Tags          []string          `json:"tags"`
	ThumbnailURL  string            `json:"thumbnail_url,omitempty"`
	ArtFiles      map[string]string `json:"art_files,omitempty"` // role → local path under cache
	Error         string            `json:"error,omitempty"`
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

// videoPrefetchArtDir is cache/video-meta/{vid}/art-{tid}/ for ephemeral thumb previews.
func (s *Store) videoPrefetchArtDir(videoID, taskID int64) string {
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("data", "cache")
	}
	return filepath.Join(root, "video-meta", strconv.FormatInt(videoID, 10),
		fmt.Sprintf("art-%d", taskID))
}

// PersistVideoPrefetchThumb downloads thumbnailURL into the video-meta art cache and
// sets draft.ArtFiles["thumb"]. Soft-ok when URL empty or download fails.
func (s *Store) PersistVideoPrefetchThumb(videoID, taskID int64, draft *VideoPrefetchDraft) {
	if draft == nil {
		return
	}
	url := strings.TrimSpace(draft.ThumbnailURL)
	if url == "" {
		return
	}
	artDir := s.videoPrefetchArtDir(videoID, taskID)
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		return
	}
	dest := filepath.Join(artDir, ArtThumb+".jpg")
	if err := downloadURLToFile(url, dest); err != nil {
		_ = os.Remove(dest)
		return
	}
	if draft.ArtFiles == nil {
		draft.ArtFiles = map[string]string{}
	}
	draft.ArtFiles[ArtThumb] = dest
}

// ClearVideoPrefetchDraft removes an ephemeral video-meta prefetch draft and its art dir.
func (s *Store) ClearVideoPrefetchDraft(videoID, taskID int64) error {
	if videoID <= 0 || taskID <= 0 {
		return nil
	}
	draft, err := s.ReadVideoPrefetchDraft(videoID, taskID)
	if err == nil {
		for _, p := range draft.ArtFiles {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			_ = os.Remove(p)
		}
	}
	_ = os.Remove(s.videoPrefetchDraftPath(videoID, taskID))
	videoCache := filepath.Dir(s.videoPrefetchDraftPath(videoID, taskID))
	_ = os.RemoveAll(filepath.Join(videoCache, fmt.Sprintf("art-%d", taskID)))
	entries, err := os.ReadDir(videoCache)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(videoCache)
	}
	return nil
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
		Genres:       ParseStringListFields(e.Categories),
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
