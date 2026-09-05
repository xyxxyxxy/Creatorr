package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library/nametemplate"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// DefaultEpisodeFormat is the relative path stem (no extension) under the series folder.
const DefaultEpisodeFormat = settings.DefaultEpisodeFormat

// NamingConfig holds pack path templates.
type NamingConfig struct {
	EpisodeFormat string // relative path under series folder (may include /); required non-empty after Load
}

// NamingConfigFromRoot builds NamingConfig from a root folder row.
func NamingConfigFromRoot(root *RootFolder) NamingConfig {
	cfg := NamingConfig{EpisodeFormat: DefaultEpisodeFormat}
	if root == nil {
		return cfg
	}
	cfg.EpisodeFormat = settings.NormalizeEpisodeFormat(root.EpisodeFormat)
	return cfg
}

// LoadNamingConfigForRoot loads pack naming from the root folder row.
func (s *Store) LoadNamingConfigForRoot(rootID int64) NamingConfig {
	root, err := s.GetRoot(rootID)
	if err != nil {
		return NamingConfig{EpisodeFormat: DefaultEpisodeFormat}
	}
	return NamingConfigFromRoot(root)
}

// EpisodePaths is the on-disk layout for one packed episode (stem without ext).
type EpisodePaths struct {
	SeriesDir   string // {root}/{series_dir}
	SeasonDir   string // series dir or series/season (episode parent)
	Stem        string // filename stem without extension
	EpisodeDir  string // directory that holds the episode files (= SeasonDir)
	PrimaryBase string // full path without extension: EpisodeDir/Stem
}

// BuildEpisodePaths computes pack destinations under root for meta.
// EpisodeFormat is a relative path under the series folder (each segment expanded/sanitized).
func BuildEpisodePaths(root string, meta EpisodeNFO, cfg NamingConfig) (EpisodePaths, error) {
	epFmt := strings.TrimSpace(cfg.EpisodeFormat)
	if epFmt == "" {
		epFmt = DefaultEpisodeFormat
	}
	if err := nametemplate.Validate(epFmt); err != nil {
		return EpisodePaths{}, err
	}

	vals := nametemplate.Values{
		Series:  meta.SeriesTitle,
		Year:    meta.Season,
		Episode: meta.Episode,
		Title:   meta.Title,
		ID:      meta.UniqueID,
		Date:    UploadCalendarDate(meta.Aired),
		Domain:  namingDomain(meta.Domain),
	}
	if t, ok := ParseUploadTime(meta.Aired); ok {
		t = t.UTC()
		vals.Month = int(t.Month())
		vals.Day = t.Day()
	}

	seriesDir := SeriesDir(root, meta.SeriesTitle)
	return buildEpisodePathsRel(seriesDir, epFmt, vals)
}

func buildEpisodePathsRel(seriesDir, epFmt string, vals nametemplate.Values) (EpisodePaths, error) {
	epFmt = strings.ReplaceAll(epFmt, `\`, "/")
	rawParts := strings.Split(epFmt, "/")
	var parts []string
	for _, p := range rawParts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		seg, err := nametemplate.ExpandAndSanitize(p, vals)
		if err != nil {
			return EpisodePaths{}, err
		}
		if seg == "" || seg == "." || seg == ".." {
			return EpisodePaths{}, fmt.Errorf("invalid path segment in episode format")
		}
		parts = append(parts, seg)
	}
	if len(parts) == 0 {
		return EpisodePaths{}, fmt.Errorf("empty episode stem")
	}
	stem := parts[len(parts)-1]
	dirParts := parts[:len(parts)-1]
	episodeDir := seriesDir
	if len(dirParts) > 0 {
		episodeDir = filepath.Join(append([]string{seriesDir}, dirParts...)...)
	}
	return EpisodePaths{
		SeriesDir:   seriesDir,
		SeasonDir:   episodeDir,
		Stem:        stem,
		EpisodeDir:  episodeDir,
		PrimaryBase: filepath.Join(episodeDir, stem),
	}, nil
}

// PruneEmptyDir removes path if it exists and is an empty directory.
// Never errors on non-empty or missing; returns whether removed.
func PruneEmptyDir(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || path == "." || path == string(filepath.Separator) {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) > 0 {
		return false
	}
	if err := os.Remove(path); err != nil {
		return false
	}
	return true
}

// DestinationOccupied reports whether dst exists and is not one of the video's current paths.
func DestinationOccupied(dst string, currentPaths []string) bool {
	if !fileExists(dst) {
		return false
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		absDst = dst
	}
	for _, p := range currentPaths {
		if p == "" {
			continue
		}
		absP, err := filepath.Abs(p)
		if err != nil {
			absP = p
		}
		if absP == absDst {
			return false
		}
	}
	return true
}
