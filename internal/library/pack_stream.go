package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PackStream writes .strm + full episode NFO (+ optional thumb and subtitle sidecars)
// under the series folder. No remux and no media file.
// allowPaths are existing episode paths that may be overwritten (re-pack).
// Subtitle dest names keep the yt-dlp language suffix (e.g. .en.srt or .en.auto.srt).
func PackStream(streamURL, root string, meta EpisodeNFO, cfg NamingConfig, thumbSrc string, subSrcs, allowPaths []string) (strmPath, nfoPath, thumbPath string, subPaths []string, err error) {
	streamURL = strings.TrimSpace(streamURL)
	if streamURL == "" {
		return "", "", "", nil, fmt.Errorf("stream URL required")
	}
	paths, err := BuildEpisodePaths(root, meta, cfg)
	if err != nil {
		return "", "", "", nil, err
	}
	if err := os.MkdirAll(paths.EpisodeDir, 0o755); err != nil {
		return "", "", "", nil, err
	}
	strmPath = paths.PrimaryBase + ".strm"
	if DestinationOccupied(strmPath, allowPaths) {
		return "", "", "", nil, fmt.Errorf("destination exists: %s", strmPath)
	}
	if err := os.WriteFile(strmPath, []byte(streamURL+"\n"), 0o644); err != nil {
		return "", "", "", nil, err
	}
	nfoPath = paths.PrimaryBase + ".nfo"
	if err := WriteEpisodeNFO(nfoPath, meta); err != nil {
		return strmPath, "", "", nil, err
	}
	if thumbSrc != "" && fileExists(thumbSrc) {
		thumbExt := filepath.Ext(thumbSrc)
		if thumbExt == "" {
			thumbExt = ".jpg"
		}
		thumbPath = paths.PrimaryBase + "-thumb" + thumbExt
		if err := copyFile(thumbSrc, thumbPath); err != nil {
			thumbPath = ""
		}
	}
	for _, src := range subSrcs {
		if !fileExists(src) {
			continue
		}
		suffix := SubtitleLangAndExt(src, guessSubtitleWorkStem(src))
		if suffix == "" {
			continue
		}
		dest := paths.PrimaryBase + suffix
		if err := copyFile(src, dest); err != nil {
			continue
		}
		subPaths = append(subPaths, dest)
	}
	return strmPath, nfoPath, thumbPath, subPaths, nil
}
