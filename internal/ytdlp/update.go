package ytdlp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

const (
	ChannelStable  = "stable"
	ChannelNightly = "nightly"

	sha256SumsFile = "SHA2-256SUMS"
)

// UpdateOpts controls a GitHub release fetch and managed-path install.
type UpdateOpts struct {
	ManagedPath string
	Channel     string
	Force       bool // install even when version matches (dev bootstrap)
	HTTPClient  *http.Client
	Progress    func(msg string)
}

// UpdateResult is the outcome of Update.
type UpdateResult struct {
	FromVersion string
	ToVersion   string
	Channel     string
	Skipped     bool
}

// AssetName returns the Linux release asset for the current GOARCH.
func AssetName() string {
	if runtime.GOARCH == "arm64" {
		return "yt-dlp_linux_aarch64"
	}
	return "yt-dlp_linux"
}

func releaseRepo(channel string) string {
	if strings.TrimSpace(channel) == ChannelNightly {
		return "yt-dlp/yt-dlp-nightly-builds"
	}
	return "yt-dlp/yt-dlp"
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

func versionsDiffer(current, latest string) bool {
	return normalizeVersion(current) != normalizeVersion(latest)
}

func (o UpdateOpts) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (o UpdateOpts) progress(msg string) {
	if o.Progress != nil {
		o.Progress(msg)
	}
}

// Update fetches the latest release for channel when newer than installed, verifies SHA2-256, and installs to ManagedPath.
func Update(ctx context.Context, opts UpdateOpts) (UpdateResult, error) {
	managed := filepath.Clean(strings.TrimSpace(opts.ManagedPath))
	if managed == "" || managed == "." {
		return UpdateResult{}, fmt.Errorf("yt-dlp managed path empty")
	}
	channel := strings.TrimSpace(opts.Channel)
	if channel == "" {
		channel = ChannelStable
	}
	if channel != ChannelStable && channel != ChannelNightly {
		return UpdateResult{}, fmt.Errorf("unknown yt-dlp update channel %q", channel)
	}

	fromVer := ""
	if st, err := os.Stat(managed); err == nil && !st.IsDir() {
		if v, verr := VerifyBinary(managed); verr == nil {
			fromVer = v
		}
	}

	repo := releaseRepo(channel)
	opts.progress("Checking for yt-dlp update…")
	tag, err := fetchLatestTag(ctx, opts.client(), repo)
	if err != nil {
		return UpdateResult{}, wrapUpdateErr(err)
	}

	res := UpdateResult{FromVersion: fromVer, ToVersion: tag, Channel: channel}
	if !opts.Force && fromVer != "" && !versionsDiffer(fromVer, tag) {
		res.Skipped = true
		res.ToVersion = normalizeVersion(fromVer)
		opts.progress("Already up to date")
		return res, nil
	}

	opts.progress("Downloading yt-dlp " + tag + "…")
	binURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, AssetName())
	sumsURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, sha256SumsFile)

	dir := filepath.Dir(managed)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return UpdateResult{}, wrapUpdateErr(fmt.Errorf("mkdir %s: %w", dir, err))
	}
	tmp, err := os.CreateTemp(dir, "yt-dlp-dl-*")
	if err != nil {
		return UpdateResult{}, wrapUpdateErr(err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	defer cleanup()

	if err := downloadURL(ctx, opts.client(), binURL, tmp); err != nil {
		_ = tmp.Close()
		return UpdateResult{}, wrapUpdateErr(err)
	}
	if err := tmp.Close(); err != nil {
		return UpdateResult{}, wrapUpdateErr(err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return UpdateResult{}, wrapUpdateErr(err)
	}

	opts.progress("Verifying SHA2-256…")
	sums, err := fetchBytes(ctx, opts.client(), sumsURL)
	if err != nil {
		return UpdateResult{}, wrapUpdateErr(err)
	}
	if err := verifyFileSHA256(tmpPath, sums, AssetName()); err != nil {
		return UpdateResult{}, wrapUpdateErr(err)
	}

	opts.progress("Verifying yt-dlp binary…")
	if _, err := VerifyBinary(tmpPath); err != nil {
		return UpdateResult{}, wrapUpdateErr(err)
	}

	if err := atomicReplace(tmpPath, managed); err != nil {
		return UpdateResult{}, wrapUpdateErr(err)
	}

	installed, err := VerifyBinary(managed)
	if err != nil {
		return UpdateResult{}, wrapUpdateErr(err)
	}
	res.ToVersion = installed
	return res, nil
}

func wrapUpdateErr(err error) error {
	if err == nil {
		return nil
	}
	if ae, ok := err.(*apperrors.AppError); ok {
		return ae
	}
	return apperrors.WithDetail(apperrors.New(apperrors.CodeDownloadFailed, "yt-dlp update failed"), err.Error())
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

func fetchLatestTag(ctx context.Context, client *http.Client, repo string) (string, error) {
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Creatorr")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("github releases API %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	tag := strings.TrimSpace(rel.TagName)
	if tag == "" {
		return "", fmt.Errorf("github release missing tag_name")
	}
	return tag, nil
}

func downloadURL(ctx context.Context, client *http.Client, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Creatorr")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func fetchBytes(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Creatorr")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(resp.Body)
}

func expectedSHA256(sums []byte, asset string) (string, bool) {
	for _, line := range strings.Split(string(sums), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		hash := strings.ToLower(parts[0])
		name := strings.TrimPrefix(parts[1], "*")
		if name == asset {
			return hash, true
		}
	}
	return "", false
}

func verifyFileSHA256(path string, sums []byte, asset string) error {
	want, ok := expectedSHA256(sums, asset)
	if !ok {
		return fmt.Errorf("SHA2-256SUMS missing entry for %s", asset)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("SHA256 mismatch for %s: expected %s got %s", asset, want, got)
	}
	return nil
}

func atomicReplace(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device fallback.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".yt-dlp-install-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	ok = true
	_ = os.Remove(src)
	return nil
}

func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "yt-dlp-bootstrap-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	ok = true
	return nil
}
