package ytdlp

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BinCandidates are preferred yt-dlp locations (Docker image path first, then PATH).
var BinCandidates = []string{
	"/usr/local/bin/yt-dlp",
	"yt-dlp",
}

// ResolveBin returns the yt-dlp binary to exec: /usr/local/bin/yt-dlp when present,
// else PATH lookup of "yt-dlp". Plugins are independent (--plugin-dirs under data).
func ResolveBin() (string, error) {
	for _, c := range BinCandidates {
		if filepath.IsAbs(c) {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c, nil
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("yt-dlp not found (tried %v)", BinCandidates)
}

// EnsurePluginsDir creates the plugins root when missing (no binary copy).
func EnsurePluginsDir(pluginsDir string) error {
	if d := filepath.Clean(pluginsDir); d != "" && d != "." {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir plugins dir: %w", err)
		}
	}
	return nil
}

// VerifyBinary checks that bin exists and runs successfully with --version
// (boot must fail if missing or broken). Returns the version string.
func VerifyBinary(bin string) (version string, err error) {
	bin = filepath.Clean(strings.TrimSpace(bin))
	if bin == "" || bin == "." {
		return "", fmt.Errorf("yt-dlp path empty")
	}
	st, err := os.Stat(bin)
	if err != nil {
		return "", fmt.Errorf("yt-dlp binary missing: %s: %w", bin, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("yt-dlp path is a directory: %s", bin)
	}
	cmd := exec.Command(bin, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg != "" {
			return "", fmt.Errorf("yt-dlp --version failed (%s): %w: %s", bin, err, msg)
		}
		return "", fmt.Errorf("yt-dlp --version failed (%s): %w", bin, err)
	}
	version = strings.TrimSpace(out.String())
	if i := strings.IndexByte(version, '\n'); i >= 0 {
		version = strings.TrimSpace(version[:i])
	}
	if version == "" {
		return "", fmt.Errorf("yt-dlp --version returned empty output (%s)", bin)
	}
	return version, nil
}
