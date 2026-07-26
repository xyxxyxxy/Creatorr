package streamproxy

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// buildHandoffPlaylist prepends durable prefix segments (beginning or progressive
// playback cache), then live session segments not already promoted into that prefix.
// skipLiveEntries is how many live playlist entries are already in the prefix
// (PlaybackMeta.LiveSegsCopied). Those must not be listed again or Emby plays the
// same content twice then hits expired live URIs.
// While the live mux is open (no ENDLIST): EVENT playlist, real segs only (no phantom
// pad). Emby re-polls EVENT and picks up new live segs.
// Once live writes ENDLIST: VOD + ENDLIST with real segs only (declared duration longer
// than mux output must not invent URIs).
// beginningBase/liveBase are absolute or path-form URI prefixes ending in /.
// Token is appended as ?token= only (no '&').
// durationSec is accepted for call-site compatibility; pad-to-duration was removed
// because VOD phantoms hang HLS clients that prefetch the whole list.
func buildHandoffPlaylist(beginningDir, liveDir, beginningBase, liveBase, token string, durationSec float64, skipLiveEntries int) []byte {
	_ = durationSec
	q := "?token=" + token
	liveIndex := filepath.Join(liveDir, "index.m3u8")
	beginEntries := parseMediaEntries(filepath.Join(beginningDir, "index.m3u8"))
	liveEntries := parseMediaEntries(liveIndex)
	if skipLiveEntries < 0 {
		skipLiveEntries = 0
	}
	if skipLiveEntries > len(liveEntries) {
		skipLiveEntries = len(liveEntries)
	}
	// New live segs continue the same mux PTS as already-promoted ones: no extra
	// discontinuity when skipLiveEntries > 0.
	appendLive := liveEntries[skipLiveEntries:]
	liveEnded := playlistHasEndlistFile(liveIndex)
	target := 4.0
	if t := playlistTargetDuration(filepath.Join(beginningDir, "index.m3u8")); t > 0 {
		target = float64(t)
	}
	if t := playlistTargetDuration(liveIndex); float64(t) > target {
		target = float64(t)
	}

	var out strings.Builder
	out.WriteString("#EXTM3U\n")
	out.WriteString("#EXT-X-VERSION:7\n")
	out.WriteString("#EXT-X-TARGETDURATION:" + strconv.Itoa(int(target+0.5)) + "\n")
	out.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	if liveEnded {
		out.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	} else {
		out.WriteString("#EXT-X-PLAYLIST-TYPE:EVENT\n")
	}
	out.WriteString("#EXT-X-START:TIME-OFFSET=0\n")

	for _, e := range beginEntries {
		if e.discontinuity {
			out.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		out.WriteString("#EXTINF:")
		out.WriteString(e.extinf)
		out.WriteByte('\n')
		out.WriteString(beginningBase)
		out.WriteString(e.uri)
		out.WriteString(q)
		out.WriteByte('\n')
	}

	if len(beginEntries) > 0 && len(appendLive) > 0 && skipLiveEntries == 0 {
		out.WriteString("#EXT-X-DISCONTINUITY\n")
	}
	for _, e := range appendLive {
		if e.discontinuity {
			out.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		out.WriteString("#EXTINF:")
		out.WriteString(e.extinf)
		out.WriteByte('\n')
		out.WriteString(liveBase)
		out.WriteString(e.uri)
		out.WriteString(q)
		out.WriteByte('\n')
	}

	if liveEnded {
		out.WriteString("#EXT-X-ENDLIST\n")
	}
	return []byte(out.String())
}

// playlistHasEndlist reports whether body contains an HLS end marker.
func playlistHasEndlist(body []byte) bool {
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "#EXT-X-ENDLIST" {
			return true
		}
	}
	return false
}

func playlistHasEndlistFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return playlistHasEndlist(data)
}

func extinfSeconds(extinf string) float64 {
	rest := strings.TrimSpace(extinf)
	if i := strings.IndexByte(rest, ','); i >= 0 {
		rest = rest[:i]
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
	if err != nil || f < 0 {
		return 0
	}
	return f
}

type mediaEntry struct {
	extinf        string // duration text including optional title after comma, e.g. "4.000," or "4.0,title"
	uri           string
	discontinuity bool // emit #EXT-X-DISCONTINUITY before this entry
}

func parseMediaEntries(path string) []mediaEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []mediaEntry
	pendingDisc := false
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "#EXT-X-DISCONTINUITY" {
			pendingDisc = true
			continue
		}
		if !strings.HasPrefix(trimmed, "#EXTINF:") {
			continue
		}
		extinf := strings.TrimPrefix(trimmed, "#EXTINF:")
		uri := ""
		for j := i + 1; j < len(lines); j++ {
			u := strings.TrimSpace(lines[j])
			if u == "" || strings.HasPrefix(u, "#") {
				continue
			}
			uri = filepath.Base(u)
			i = j
			break
		}
		if uri != "" && uri != "." && uri != ".." {
			out = append(out, mediaEntry{extinf: extinf, uri: uri, discontinuity: pendingDisc})
			pendingDisc = false
		}
	}
	return out
}

func playlistTargetDuration(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:")))
			if err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}
