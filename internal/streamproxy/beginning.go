package streamproxy

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// buildHandoffPlaylist prepends all cached beginning segments, then live session
// segments with #EXT-X-DISCONTINUITY. Framed as VOD + START:0; when durationSec > 0,
// pads future live seg%05d.ts entries and ENDLIST so Emby reports full length.
// beginningBase/liveBase are absolute or path-form URI prefixes ending in /.
// Token is appended as ?token= only (no '&').
func buildHandoffPlaylist(beginningDir, liveDir, beginningBase, liveBase, token string, durationSec float64) []byte {
	q := "?token=" + token
	beginEntries := parseMediaEntries(filepath.Join(beginningDir, "index.m3u8"))
	liveEntries := parseMediaEntries(filepath.Join(liveDir, "index.m3u8"))
	target := 4.0
	if t := playlistTargetDuration(filepath.Join(beginningDir, "index.m3u8")); t > 0 {
		target = float64(t)
	}
	if t := playlistTargetDuration(filepath.Join(liveDir, "index.m3u8")); float64(t) > target {
		target = float64(t)
	}

	var out strings.Builder
	out.WriteString("#EXTM3U\n")
	out.WriteString("#EXT-X-VERSION:7\n")
	out.WriteString("#EXT-X-TARGETDURATION:" + strconv.Itoa(int(target+0.5)) + "\n")
	out.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	out.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	out.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	out.WriteString("#EXT-X-START:TIME-OFFSET=0\n")

	var sum float64
	for _, e := range beginEntries {
		out.WriteString("#EXTINF:")
		out.WriteString(e.extinf)
		out.WriteByte('\n')
		out.WriteString(beginningBase)
		out.WriteString(e.uri)
		out.WriteString(q)
		out.WriteByte('\n')
		sum += extinfSeconds(e.extinf)
	}

	lastSeg := -1
	needLive := len(liveEntries) > 0 || (durationSec > 0 && sum+0.05 < durationSec)
	if len(beginEntries) > 0 && needLive {
		out.WriteString("#EXT-X-DISCONTINUITY\n")
	}
	for _, e := range liveEntries {
		out.WriteString("#EXTINF:")
		out.WriteString(e.extinf)
		out.WriteByte('\n')
		out.WriteString(liveBase)
		out.WriteString(e.uri)
		out.WriteString(q)
		out.WriteByte('\n')
		sum += extinfSeconds(e.extinf)
		if n := parseSegIndex(e.uri); n > lastSeg {
			lastSeg = n
		}
	}

	if durationSec > 0 {
		segDur := target
		if segDur < 1 {
			segDur = 4
		}
		next := lastSeg + 1
		if next < 0 {
			next = 0
		}
		for sum+0.05 < durationSec {
			remain := durationSec - sum
			d := segDur
			if remain < d {
				d = remain
			}
			out.WriteString("#EXTINF:")
			out.WriteString(strconv.FormatFloat(d, 'f', 6, 64))
			out.WriteString(",\n")
			out.WriteString(liveBase)
			out.WriteString("seg")
			out.WriteString(padSegIndex(next))
			out.WriteString(".ts")
			out.WriteString(q)
			out.WriteByte('\n')
			sum += d
			next++
		}
		out.WriteString("#EXT-X-ENDLIST\n")
	}
	return []byte(out.String())
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
	extinf string // duration text including optional title after comma, e.g. "4.000," or "4.0,title"
	uri    string
}

func parseMediaEntries(path string) []mediaEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []mediaEntry
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
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
			out = append(out, mediaEntry{extinf: extinf, uri: uri})
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
