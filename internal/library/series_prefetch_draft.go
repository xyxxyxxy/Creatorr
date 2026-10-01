package library

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func downloadURLToCache(rawURL, dest string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Creatorr/1.0)")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	return err
}

// PrefetchDraft is ephemeral form fill data (not persisted on series until Save).
type PrefetchDraft struct {
	Title         string            `json:"title"` // display name for Add series; not a tvshow.nfo alt title
	Plot          string            `json:"plot"`
	SortTitle     string            `json:"sorttitle"`
	OriginalTitle string            `json:"originaltitle"`
	Studio        string            `json:"studio"`
	UniqueIDType  string            `json:"uniqueid_type"`
	UniqueIDValue string            `json:"uniqueid_value"`
	Actors        []SeriesActor     `json:"actors"`
	ArtFiles      map[string]string `json:"art_files"` // role → local path under cache
	PlaylistOnly  bool              `json:"playlist_only"`
	Error         string            `json:"error,omitempty"`
}

func (s *Store) prefetchDraftPath(seriesID, taskID int64) string {
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("data", "cache")
	}
	return filepath.Join(root, "series-meta", strconv.FormatInt(seriesID, 10),
		fmt.Sprintf("prefetch-%d.json", taskID))
}

func (s *Store) addSeriesDraftDir(token string) string {
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("data", "cache")
	}
	return filepath.Join(root, "add-series", token)
}

func (s *Store) addSeriesDraftPath(token string) string {
	return filepath.Join(s.addSeriesDraftDir(token), "draft.json")
}

// WriteAddSeriesDraft stores a pre-create metadata draft under cache/add-series/{token}/.
func (s *Store) WriteAddSeriesDraft(token string, draft PrefetchDraft) error {
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") || strings.Contains(token, "..") {
		return fmt.Errorf("%w: draft token", ErrInvalid)
	}
	dir := s.addSeriesDraftDir(token)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Persist art into the draft dir so temp work dirs can be removed.
	persisted := map[string]string{}
	for role, src := range draft.ArtFiles {
		if src == "" {
			continue
		}
		ext := filepath.Ext(src)
		if ext == "" {
			ext = ".jpg"
		}
		dest := filepath.Join(dir, role+ext)
		b, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dest, b, 0o644); err != nil {
			continue
		}
		persisted[role] = dest
	}
	draft.ArtFiles = persisted
	b, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.addSeriesDraftPath(token), b, 0o644)
}

// ReadAddSeriesDraft loads a pre-create metadata draft.
func (s *Store) ReadAddSeriesDraft(token string) (PrefetchDraft, error) {
	var draft PrefetchDraft
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") || strings.Contains(token, "..") {
		return draft, fmt.Errorf("%w: draft token", ErrInvalid)
	}
	b, err := os.ReadFile(s.addSeriesDraftPath(token))
	if err != nil {
		return draft, err
	}
	err = json.Unmarshal(b, &draft)
	return draft, err
}

// WritePrefetchDraft stores a draft JSON under cache.
func (s *Store) WritePrefetchDraft(seriesID, taskID int64, draft PrefetchDraft) error {
	path := s.prefetchDraftPath(seriesID, taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ReadPrefetchDraft loads a draft if present.
func (s *Store) ReadPrefetchDraft(seriesID, taskID int64) (PrefetchDraft, error) {
	var draft PrefetchDraft
	b, err := os.ReadFile(s.prefetchDraftPath(seriesID, taskID))
	if err != nil {
		return draft, err
	}
	err = json.Unmarshal(b, &draft)
	return draft, err
}

// ClearPrefetchDraft removes an ephemeral series-meta prefetch draft and its art dir
// under cache (library art is the lasting copy after Save).
func (s *Store) ClearPrefetchDraft(seriesID, taskID int64) error {
	if seriesID <= 0 || taskID <= 0 {
		return nil
	}
	draft, err := s.ReadPrefetchDraft(seriesID, taskID)
	if err == nil {
		for _, p := range draft.ArtFiles {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			_ = os.Remove(p)
		}
	}
	_ = os.Remove(s.prefetchDraftPath(seriesID, taskID))
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("data", "cache")
	}
	seriesCache := filepath.Join(root, "series-meta", strconv.FormatInt(seriesID, 10))
	_ = os.RemoveAll(filepath.Join(seriesCache, fmt.Sprintf("art-%d", taskID)))
	entries, err := os.ReadDir(seriesCache)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(seriesCache)
	}
	return nil
}

// ClearAddSeriesDraft removes cache/add-series/{token}/ after series create copies art into the library.
func (s *Store) ClearAddSeriesDraft(token string) error {
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") || strings.Contains(token, "..") {
		return fmt.Errorf("%w: draft token", ErrInvalid)
	}
	return os.RemoveAll(s.addSeriesDraftDir(token))
}
