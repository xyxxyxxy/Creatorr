package streamproxy

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// rewriteHLSPlaylist rewrites absolute/relative URI lines so the client fetches
// through Creatorr. pathPrefix is e.g. /stream/videos/1/hls/u/ and token is the
// sole query param (no '&' - Emby/ffmpeg HLS parsers choke on query ampersands).
func rewriteHLSPlaylist(body []byte, playlistURL, pathPrefix, token string) []byte {
	base, err := url.Parse(playlistURL)
	if err != nil {
		return body
	}
	var out strings.Builder
	sc := bufio.NewScanner(strings.NewReader(string(body)))
	// Allow long HLS lines.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			// Rewrite URI="..." inside EXT-X-KEY / EXT-X-MAP / EXT-X-MEDIA etc.
			if strings.Contains(trimmed, "URI=\"") {
				line = rewriteAttrURIs(line, base, pathPrefix, token)
			}
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		abs := resolveHLSURI(base, trimmed)
		out.WriteString(proxyURI(pathPrefix, token, abs))
		out.WriteByte('\n')
	}
	return []byte(out.String())
}

func proxyURI(pathPrefix, token, abs string) string {
	u := pathPrefix + encodeUpstream(abs)
	if token != "" {
		u += "?token=" + url.QueryEscape(token)
	}
	return u
}

func rewriteAttrURIs(line string, base *url.URL, pathPrefix, token string) string {
	const key = `URI="`
	var b strings.Builder
	rest := line
	for {
		i := strings.Index(rest, key)
		if i < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i+len(key)])
		rest = rest[i+len(key):]
		j := strings.Index(rest, `"`)
		if j < 0 {
			b.WriteString(rest)
			break
		}
		raw := rest[:j]
		abs := resolveHLSURI(base, raw)
		b.WriteString(proxyURI(pathPrefix, token, abs))
		b.WriteByte('"')
		rest = rest[j+1:]
	}
	return b.String()
}

func resolveHLSURI(base *url.URL, ref string) string {
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

// encodeUpstream packs an absolute URL for the path segment (base64url, no pad).
func encodeUpstream(abs string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(abs))
}

func decodeUpstream(enc string) (string, error) {
	// Accept raw base64url or query-unescaped form.
	b, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		b, err = base64.URLEncoding.DecodeString(enc)
		if err != nil {
			return "", fmt.Errorf("bad upstream encoding")
		}
	}
	s := string(b)
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return "", fmt.Errorf("upstream must be http(s)")
	}
	return s, nil
}

func proxyUpstream(w http.ResponseWriter, r *http.Request, mediaURL string, hdrs map[string]string, rewritePlaylist bool, playlistSelf, pathPrefix, token string) error {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, mediaURL, nil)
	if err != nil {
		http.Error(w, "bad upstream URL", http.StatusBadGateway)
		return err
	}
	for k, v := range hdrs {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	if rng := r.Header.Get("Range"); rng != "" && !rewritePlaylist {
		req.Header.Set("Range", rng)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "upstream fetch failed", http.StatusBadGateway)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return errUpstreamForbidden
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		http.Error(w, "upstream read failed", http.StatusBadGateway)
		return err
	}
	ct := resp.Header.Get("Content-Type")
	if rewritePlaylist || looksLikeM3U(ct, body) {
		body = rewriteHLSPlaylist(body, playlistSelf, pathPrefix, token)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return nil
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	}
	for _, k := range []string{"Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
	return nil
}

func looksLikeM3U(ct string, body []byte) bool {
	low := strings.ToLower(ct)
	if strings.Contains(low, "mpegurl") || strings.Contains(low, "m3u8") {
		return true
	}
	s := strings.TrimSpace(string(body))
	return strings.HasPrefix(s, "#EXTM3U")
}

// proxyUpstreamStream streams large segment bodies without buffering the whole file.
func proxyUpstreamStream(w http.ResponseWriter, r *http.Request, mediaURL string, hdrs map[string]string) error {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, mediaURL, nil)
	if err != nil {
		http.Error(w, "bad upstream URL", http.StatusBadGateway)
		return err
	}
	for k, v := range hdrs {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "upstream fetch failed", http.StatusBadGateway)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return errUpstreamForbidden
	}
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
	return nil
}
