package ytdlp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareManagedBinCopiesBootstrap(t *testing.T) {
	dir := t.TempDir()
	bootstrap := filepath.Join(dir, "bootstrap")
	managed := filepath.Join(dir, "bin", "yt-dlp")
	if err := os.WriteFile(bootstrap, []byte("#!/bin/sh\necho 2024.01.01\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ver, err := PrepareManagedBin(context.Background(), PrepareOpts{
		Bootstrap: bootstrap,
		Managed:   managed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ver != "2024.01.01" {
		t.Fatalf("version %q", ver)
	}
	if _, err := os.Stat(managed); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareManagedBinKeepsValidManaged(t *testing.T) {
	dir := t.TempDir()
	bootstrap := filepath.Join(dir, "bootstrap")
	managed := filepath.Join(dir, "managed")
	for name, body := range map[string]string{
		bootstrap: "#!/bin/sh\necho old\n",
		managed:   "#!/bin/sh\necho kept\n",
	} {
		if err := os.WriteFile(name, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ver, err := PrepareManagedBin(context.Background(), PrepareOpts{
		Bootstrap: bootstrap,
		Managed:   managed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ver != "kept" {
		t.Fatalf("got %q want kept", ver)
	}
}

func TestUpdateSkipWhenCurrent(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(managed, []byte("#!/bin/sh\necho 2025.01.01\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := managedPayload(t, "2025.01.01")
	var assetHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/repos/"):
			_, _ = fmt.Fprint(w, `{"tag_name":"2025.01.01"}`)
		case strings.HasSuffix(r.URL.Path, "/"+AssetName()):
			assetHits++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := mockGitHubClient(srv)
	_ = payload

	res, err := Update(context.Background(), UpdateOpts{
		ManagedPath: managed,
		Channel:     ChannelStable,
		HTTPClient:  client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Fatal("expected skip")
	}
	if assetHits != 0 {
		t.Fatalf("expected no asset download, got %d requests", assetHits)
	}
}

func TestUpdateSHA256MismatchKeepsPriorBinary(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(dir, "yt-dlp")
	orig := []byte("#!/bin/sh\necho prior\n")
	if err := os.WriteFile(managed, orig, 0o755); err != nil {
		t.Fatal(err)
	}
	badPayload := []byte("#!/bin/sh\necho tampered\n")
	goodPayload := []byte("#!/bin/sh\necho other\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/repos/"):
			_, _ = fmt.Fprint(w, `{"tag_name":"2025.02.01"}`)
		case strings.HasSuffix(r.URL.Path, "/SHA2-256SUMS"):
			sum := sha256.Sum256(goodPayload)
			_, _ = fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), AssetName())
		case strings.HasSuffix(r.URL.Path, "/"+AssetName()):
			_, _ = w.Write(badPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := mockGitHubClient(srv)

	_, err := Update(context.Background(), UpdateOpts{
		ManagedPath: managed,
		Channel:     ChannelStable,
		HTTPClient:  client,
	})
	if err == nil {
		t.Fatal("expected SHA256 mismatch error")
	}
	got, err := os.ReadFile(managed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(orig) {
		t.Fatal("managed binary changed on failed update")
	}
}

func TestUpdateInstallsNewer(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(managed, []byte("#!/bin/sh\necho 2024.01.01\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := managedPayload(t, "2025.03.01")
	srv, client := mockGitHubRelease(t, "2025.03.01", payload)
	defer srv.Close()

	res, err := Update(context.Background(), UpdateOpts{
		ManagedPath: managed,
		Channel:     ChannelStable,
		HTTPClient:  client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Fatal("expected install")
	}
	ver, err := VerifyBinary(managed)
	if err != nil {
		t.Fatal(err)
	}
	if ver != "2025.03.01" {
		t.Fatalf("installed %q", ver)
	}
}

func managedPayload(t *testing.T, version string) []byte {
	t.Helper()
	body := fmt.Sprintf("#!/bin/sh\necho %s\n", version)
	return []byte(body)
}

func mockGitHubRelease(t *testing.T, tag string, payload []byte) (*httptest.Server, *http.Client) {
	t.Helper()
	sum := sha256.Sum256(payload)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/repos/"):
			_, _ = fmt.Fprintf(w, `{"tag_name":"%s"}`, tag)
		case strings.HasSuffix(r.URL.Path, "/SHA2-256SUMS"):
			_, _ = fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), AssetName())
		case strings.HasSuffix(r.URL.Path, "/"+AssetName()):
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, mockGitHubClient(srv)
}

func mockGitHubClient(srv *httptest.Server) *http.Client {
	base, _ := url.Parse(srv.URL)
	return &http.Client{
		Transport: &rewriteHostTransport{base: base},
	}
}

type rewriteHostTransport struct {
	base *url.URL
}

func (t *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	return http.DefaultTransport.RoundTrip(req)
}
