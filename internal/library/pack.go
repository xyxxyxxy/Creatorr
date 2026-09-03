package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library/nametemplate"
)

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// SidecarPathsBeside returns existing .nfo / .info.json next to a media file.
func SidecarPathsBeside(mediaPath string) (nfoPath, infoPath string) {
	stem := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	for _, cand := range []string{stem + ".nfo"} {
		if fileExists(cand) {
			nfoPath = cand
			break
		}
	}
	for _, cand := range []string{stem + ".info.json", mediaPath + ".info.json"} {
		if fileExists(cand) {
			infoPath = cand
			break
		}
	}
	return nfoPath, infoPath
}

// FindDownloadSidecars locates info.json, thumbnail, and subtitle files written beside media
// (handler download should emit these in the same outdir / same stem).
// Missing files are soft-ok (empty strings / nil slice).
//
// Thumbnail preference: `{stem}-thumb.ext` / `{stem}.thumb.ext`, then `{stem}.ext`, then any
// same-directory image whose ClassifyImportFile stem matches the media stem after bracket
// strip (so `Show [id].mkv` pairs with `Show-thumb.jpg`).
func FindDownloadSidecars(mediaPath string) (infoPath, thumbPath string, subPaths []string) {
	if mediaPath == "" {
		return "", "", nil
	}
	stem := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	for _, cand := range []string{stem + ".info.json", mediaPath + ".info.json"} {
		if fileExists(cand) {
			infoPath = cand
			break
		}
	}
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		for _, cand := range []string{stem + "-thumb" + ext, stem + ".thumb" + ext} {
			if fileExists(cand) {
				thumbPath = cand
				break
			}
		}
		if thumbPath != "" {
			break
		}
	}
	if thumbPath == "" {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			cand := stem + ext
			if fileExists(cand) {
				thumbPath = cand
				break
			}
		}
	}
	dir := filepath.Dir(mediaPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return infoPath, thumbPath, nil
	}
	mediaBase := filepath.Base(mediaPath)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == mediaBase {
			continue
		}
		path := filepath.Join(dir, name)
		if thumbPath == "" {
			role, _ := ClassifyImportFile(name)
			if role == ImportRoleThumb && ImportSidecarStemMatchesMedia(path, mediaPath) {
				thumbPath = path
				continue
			}
		}
		if IsSubtitleFilename(name) {
			subPaths = append(subPaths, path)
		}
	}
	return infoPath, thumbPath, subPaths
}

// PackMedia moves media into the series folder and writes a full episode NFO
// (title, showtitle, S/E, plot, aired, uniqueid) via WriteEpisodeNFO.
// Copies optional info.json, thumbnail, and subtitle sidecars when sources exist (soft-ok if absent).
// Subtitle dest names keep the yt-dlp language suffix (e.g. .en.srt or .en.auto.srt).
func PackMedia(mediaSrc, root string, meta EpisodeNFO, cfg NamingConfig, infoSrc, thumbSrc string, subSrcs []string) (mediaPath, nfoPath, infoPath, thumbPath string, subPaths []string, err error) {
	paths, err := BuildEpisodePaths(root, meta, cfg)
	if err != nil {
		return "", "", "", "", nil, err
	}
	if err := os.MkdirAll(paths.EpisodeDir, 0o755); err != nil {
		return "", "", "", "", nil, err
	}
	ext := strings.ToLower(filepath.Ext(mediaSrc))
	if ext == "" {
		ext = ".mkv"
	}
	mediaPath = paths.PrimaryBase + ext
	if DestinationOccupied(mediaPath, nil) {
		return "", "", "", "", nil, fmt.Errorf("destination exists: %s", mediaPath)
	}
	if err := moveFile(mediaSrc, mediaPath); err != nil {
		return "", "", "", "", nil, err
	}
	nfoPath = paths.PrimaryBase + ".nfo"
	if err := WriteEpisodeNFO(nfoPath, meta); err != nil {
		return mediaPath, "", "", "", nil, err
	}
	if infoSrc != "" && fileExists(infoSrc) {
		infoPath = paths.PrimaryBase + ".info.json"
		if err := copyFile(infoSrc, infoPath); err != nil {
			return mediaPath, nfoPath, "", "", nil, err
		}
	}
	if thumbSrc != "" && fileExists(thumbSrc) {
		thumbExt := strings.ToLower(filepath.Ext(thumbSrc))
		if thumbExt == "" {
			thumbExt = ".jpg"
		}
		thumbPath = paths.PrimaryBase + "-thumb" + thumbExt
		if err := copyFile(thumbSrc, thumbPath); err != nil {
			// Soft-ok: media + NFO already installed.
			thumbPath = ""
		}
	}
	srcStem := strings.TrimSuffix(filepath.Base(mediaSrc), filepath.Ext(mediaSrc))
	for _, src := range subSrcs {
		if !fileExists(src) {
			continue
		}
		suffix := SubtitleLangAndExt(src, srcStem)
		if suffix == "" {
			continue
		}
		dest := paths.PrimaryBase + suffix
		if err := copyFile(src, dest); err != nil {
			continue
		}
		subPaths = append(subPaths, dest)
	}
	return mediaPath, nfoPath, infoPath, thumbPath, subPaths, nil
}

func sanitizeName(s string, max int) string {
	return nametemplate.SanitizeFilename(s, max)
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o644)
}
