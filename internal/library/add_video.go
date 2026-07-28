package library

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// CreateIndexedVideoParams creates an ignored indexed video (no media, no source row).
type CreateIndexedVideoParams struct {
	SeriesID    int64
	Title       string
	RemoteID    string // empty → generate
	UploadDate  string // required RFC3339 UTC (date-only adapted)
	SourceURL   string // optional webpage / fetch URL
	Description string
}

// CreateIndexedVideo inserts an ignored video under a series for Import Match / manual index.
func (s *Store) CreateIndexedVideo(p CreateIndexedVideoParams) (*Video, error) {
	if p.SeriesID <= 0 {
		return nil, fmt.Errorf("%w: series_id required", ErrInvalid)
	}
	if _, err := s.GetSeries(p.SeriesID, false); err != nil {
		return nil, err
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: title required", ErrInvalid)
	}
	upload := sidecarUploadTime(strings.TrimSpace(p.UploadDate))
	if upload == "" {
		return nil, fmt.Errorf("%w: upload_date required", ErrInvalid)
	}
	remoteID := strings.TrimSpace(p.RemoteID)
	if remoteID == "" {
		var err error
		remoteID, err = generateIndexedRemoteID()
		if err != nil {
			return nil, err
		}
	}
	sourceURL := strings.TrimSpace(p.SourceURL)
	desc := strings.TrimSpace(p.Description)

	season, episode, err := s.AssignSeasonEpisode(p.SeriesID, upload, 0, 0)
	if err != nil {
		return nil, err
	}
	var webpage any
	if sourceURL != "" {
		webpage = sourceURL
	}

	res, err := s.DB.SQL.Exec(`
		INSERT INTO videos (
		  series_id, source_id, remote_id, title, upload_date,
		  source_url, status, season, episode, description, thumbnail_url
		) VALUES (?, NULL, ?, ?, ?, ?, 'ignored', ?, ?, ?, NULL)
	`, p.SeriesID, remoteID, title, upload, webpage, season, episode, desc)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("%w: video with this remote_id already exists in series", ErrConflict)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := s.ReindexSeriesUTCDay(p.SeriesID, UploadCalendarDate(upload)); err != nil {
		_, _ = s.DB.SQL.Exec(`DELETE FROM videos WHERE id = ?`, id)
		return nil, err
	}
	return s.GetVideo(id)
}

// CreateIndexedVideoFromAddDraft creates from a successful add-video prefetch draft.
// Title/upload may be overridden by the operator; remote ID and source URL come from the draft.
func (s *Store) CreateIndexedVideoFromAddDraft(seriesID int64, token, title, uploadDate string) (*Video, error) {
	draft, err := s.ReadAddVideoDraft(token)
	if err != nil {
		return nil, fmt.Errorf("%w: draft not found", ErrInvalid)
	}
	if strings.TrimSpace(draft.Error) != "" {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, draft.Error)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = strings.TrimSpace(draft.Title)
	}
	upload := strings.TrimSpace(uploadDate)
	if upload == "" {
		upload = strings.TrimSpace(draft.UploadDate)
	}
	return s.CreateIndexedVideo(CreateIndexedVideoParams{
		SeriesID:    seriesID,
		Title:       title,
		RemoteID:    draft.RemoteID,
		UploadDate:  upload,
		SourceURL:   draft.SourceURL,
		Description: draft.Description,
	})
}

func generateIndexedRemoteID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "video-" + hex.EncodeToString(b), nil
}

// AddVideoDraft is ephemeral metadata for Add video (before the video row exists).
type AddVideoDraft struct {
	Title       string `json:"title"`
	RemoteID    string `json:"remote_id"`
	UploadDate  string `json:"upload_date"` // RFC3339 UTC
	SourceURL   string `json:"source_url"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (s *Store) addVideoDraftDir(token string) string {
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("data", "cache")
	}
	return filepath.Join(root, "add-video", token)
}

func (s *Store) addVideoDraftPath(token string) string {
	return filepath.Join(s.addVideoDraftDir(token), "draft.json")
}

// WriteAddVideoDraft stores a pre-create video metadata draft under cache/add-video/{token}/.
func (s *Store) WriteAddVideoDraft(token string, draft AddVideoDraft) error {
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") || strings.Contains(token, "..") {
		return fmt.Errorf("%w: draft token", ErrInvalid)
	}
	dir := s.addVideoDraftDir(token)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.addVideoDraftPath(token), b, 0o644)
}

// ReadAddVideoDraft loads a pre-create video metadata draft.
func (s *Store) ReadAddVideoDraft(token string) (AddVideoDraft, error) {
	var draft AddVideoDraft
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") || strings.Contains(token, "..") {
		return draft, fmt.Errorf("%w: draft token", ErrInvalid)
	}
	b, err := os.ReadFile(s.addVideoDraftPath(token))
	if err != nil {
		return draft, err
	}
	err = json.Unmarshal(b, &draft)
	return draft, err
}

// ClearAddVideoDraft removes an add-video draft dir.
func (s *Store) ClearAddVideoDraft(token string) error {
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") || strings.Contains(token, "..") {
		return nil
	}
	return os.RemoveAll(s.addVideoDraftDir(token))
}

// EnqueueAddVideoPrefetch queues yt-dlp resolve for Add video (no video row yet).
func (s *Store) EnqueueAddVideoPrefetch(sourceURL, draftToken string, seriesID int64) (int64, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	draftToken = strings.TrimSpace(draftToken)
	if sourceURL == "" {
		return 0, fmt.Errorf("%w: url required", ErrInvalid)
	}
	if draftToken == "" || strings.Contains(draftToken, "/") || strings.Contains(draftToken, "..") {
		return 0, fmt.Errorf("%w: draft token", ErrInvalid)
	}
	if seriesID > 0 {
		if _, err := s.GetSeries(seriesID, false); err != nil {
			return 0, err
		}
	}
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	domain := "unknown"
	if u, err := url.Parse(sourceURL); err == nil && u.Hostname() != "" {
		domain = settings.NormalizeDomain(u.Hostname())
	}
	p := queue.EnqueueParams{
		Kind:   queue.KindPrefetchAddVideo,
		Domain: domain,
		Payload: map[string]any{
			"url":         sourceURL,
			"draft_token": draftToken,
		},
		Message: "Fetch add-video metadata",
	}
	if seriesID > 0 {
		p.SeriesID = seriesID
	}
	return s.Queue.Enqueue(p)
}

// BuildAddVideoDraftFromEntry maps a resolve entry into an add-video form draft.
func BuildAddVideoDraftFromEntry(e ytdlp.Entry, sourceURL string) AddVideoDraft {
	draft := AddVideoDraft{
		Title:       strings.TrimSpace(e.Title),
		RemoteID:    strings.TrimSpace(e.ID),
		Description: strings.TrimSpace(e.Description),
		SourceURL:   strings.TrimSpace(sourceURL),
	}
	if draft.SourceURL == "" {
		draft.SourceURL = strings.TrimSpace(e.WebpageURL)
	}
	if ud := strings.TrimSpace(e.UploadDate); ud != "" {
		draft.UploadDate = ud
	}
	return draft
}

// EnsureAddVideoDraftUploadDate fills upload date from now UTC when still empty (last resort).
func EnsureAddVideoDraftUploadDate(d *AddVideoDraft) {
	if d == nil {
		return
	}
	if strings.TrimSpace(d.UploadDate) != "" {
		return
	}
	d.UploadDate = time.Now().UTC().Format(time.RFC3339)
}
