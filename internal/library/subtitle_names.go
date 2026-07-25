package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

var subtitleExts = map[string]bool{
	".vtt": true, ".srt": true, ".ass": true, ".ssa": true, ".sub": true,
}

const autoSubtitleMarker = "auto"

// IsSubtitleFilename reports whether name looks like a subtitle sidecar.
func IsSubtitleFilename(name string) bool {
	lower := strings.ToLower(name)
	for ext := range subtitleExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// SubtitleLangAndExt returns the language+ext suffix for a yt-dlp subtitle file
// relative to workStem (e.g. "Title [id]" + "Title [id].en.vtt" → ".en.vtt",
// or "Title [id].en.auto.srt" → ".en.auto.srt").
// Falls back to filepath.Ext when the stem does not match.
func SubtitleLangAndExt(srcPath, workStem string) string {
	base := filepath.Base(srcPath)
	stem := filepath.Base(workStem)
	if stem != "" && strings.HasPrefix(base, stem+".") {
		return base[len(stem):]
	}
	return filepath.Ext(base)
}

// MarkAutoSubtitleFiles renames auto-only subtitle sidecars to .lang.auto.ext
// using info.json caption maps. Manual langs (present in subtitles) are left as-is.
// Soft-ok when info.json is missing or unreadable: returns paths unchanged.
func MarkAutoSubtitleFiles(subPaths []string, infoJSONPath string) []string {
	if len(subPaths) == 0 {
		return subPaths
	}
	manual, auto := captionLangSets(infoJSONPath)
	if len(auto) == 0 {
		return subPaths
	}
	out := make([]string, 0, len(subPaths))
	for _, p := range subPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		stem := guessSubtitleWorkStem(p)
		lang := subtitleLangFromPath(p, stem)
		if lang == "" || manual[lang] || !auto[lang] {
			out = append(out, p)
			continue
		}
		if subtitleHasAutoMarker(p, stem) {
			out = append(out, p)
			continue
		}
		ext := filepath.Ext(p)
		dest := filepath.Join(filepath.Dir(p), filepath.Base(stem)+"."+lang+"."+autoSubtitleMarker+ext)
		if dest == p {
			out = append(out, p)
			continue
		}
		if err := os.Rename(p, dest); err != nil {
			out = append(out, p)
			continue
		}
		out = append(out, dest)
	}
	return out
}

func captionLangSets(infoJSONPath string) (manual, auto map[string]bool) {
	manual = map[string]bool{}
	auto = map[string]bool{}
	infoJSONPath = strings.TrimSpace(infoJSONPath)
	if infoJSONPath == "" {
		return manual, auto
	}
	b, err := os.ReadFile(infoJSONPath)
	if err != nil {
		return manual, auto
	}
	var raw struct {
		Subtitles         map[string]json.RawMessage `json:"subtitles"`
		AutomaticCaptions map[string]json.RawMessage `json:"automatic_captions"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return manual, auto
	}
	for lang := range raw.Subtitles {
		manual[lang] = true
	}
	for lang := range raw.AutomaticCaptions {
		auto[lang] = true
	}
	return manual, auto
}

func subtitleLangFromPath(srcPath, workStem string) string {
	suffix := strings.TrimPrefix(SubtitleLangAndExt(srcPath, workStem), ".")
	if suffix == "" {
		return ""
	}
	lower := strings.ToLower(suffix)
	for ext := range subtitleExts {
		if strings.HasSuffix(lower, ext) {
			suffix = suffix[:len(suffix)-len(ext)]
			lower = strings.ToLower(suffix)
			break
		}
	}
	suffix = strings.TrimSuffix(suffix, ".")
	lower = strings.ToLower(suffix)
	if strings.HasSuffix(lower, "."+autoSubtitleMarker) {
		suffix = suffix[:len(suffix)-len("."+autoSubtitleMarker)]
	} else if lower == autoSubtitleMarker {
		return ""
	}
	return suffix
}

func subtitleHasAutoMarker(srcPath, workStem string) bool {
	suffix := strings.TrimPrefix(SubtitleLangAndExt(srcPath, workStem), ".")
	lower := strings.ToLower(suffix)
	for ext := range subtitleExts {
		if strings.HasSuffix(lower, ext) {
			suffix = suffix[:len(suffix)-len(ext)]
			lower = strings.ToLower(suffix)
			break
		}
	}
	suffix = strings.TrimSuffix(suffix, ".")
	lower = strings.ToLower(suffix)
	return strings.HasSuffix(lower, "."+autoSubtitleMarker) || lower == autoSubtitleMarker
}
