package ytdlp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PrepareOpts configures boot-time managed yt-dlp setup.
type PrepareOpts struct {
	Bootstrap string // image-internal copy; empty in local dev without bootstrap
	Managed   string
	Channel   string // stable|nightly for dev GitHub fallback
}

// PrepareManagedBin ensures the managed binary exists and passes VerifyBinary.
// Copies from bootstrap when missing or corrupt; local dev may GitHub-download once when bootstrap is absent.
func PrepareManagedBin(ctx context.Context, opts PrepareOpts) (version string, err error) {
	managed := filepath.Clean(strings.TrimSpace(opts.Managed))
	if managed == "" || managed == "." {
		return "", fmt.Errorf("yt-dlp managed path empty")
	}
	dir := filepath.Dir(managed)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir yt-dlp bin dir: %w", err)
	}

	if ver, ok, err := managedOK(managed); err != nil {
		return "", err
	} else if ok {
		return ver, nil
	}

	bootstrap := filepath.Clean(strings.TrimSpace(opts.Bootstrap))
	if bootstrap != "" && bootstrap != "." {
		if st, err := os.Stat(bootstrap); err == nil && !st.IsDir() {
			if err := copyFileAtomic(bootstrap, managed); err != nil {
				return "", fmt.Errorf("copy bootstrap yt-dlp: %w", err)
			}
			return verifyManaged(managed)
		}
	}

	channel := strings.TrimSpace(opts.Channel)
	if channel == "" {
		channel = ChannelStable
	}
	if _, err := Update(ctx, UpdateOpts{
		ManagedPath: managed,
		Channel:     channel,
		Force:       true,
		Progress:    func(string) {},
	}); err != nil {
		return "", fmt.Errorf("bootstrap yt-dlp from GitHub: %w", err)
	}
	return verifyManaged(managed)
}

func managedOK(path string) (version string, ok bool, err error) {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if st.IsDir() {
		return "", false, nil
	}
	ver, verr := VerifyBinary(path)
	if verr != nil {
		return "", false, nil
	}
	return ver, true, nil
}

func verifyManaged(path string) (string, error) {
	ver, err := VerifyBinary(path)
	if err != nil {
		return "", err
	}
	return ver, nil
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
