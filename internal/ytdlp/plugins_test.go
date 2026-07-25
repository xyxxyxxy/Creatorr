package ytdlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandPluginDirs(t *testing.T) {
	root := t.TempDir()
	mde := filepath.Join(root, "mde")
	tyler := filepath.Join(root, "tyler")
	if err := os.MkdirAll(filepath.Join(mde, "yt_dlp_plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tyler, "yt_dlp_plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirs := expandPluginDirs(root)
	joined := strings.Join(dirs, ",")
	if !strings.Contains(joined, mde) || !strings.Contains(joined, tyler) {
		t.Fatalf("expected child plugin dirs, got %v", dirs)
	}
}

func TestWithPluginDirsRepeatsFlag(t *testing.T) {
	root := t.TempDir()
	mde := filepath.Join(root, "mde")
	tyler := filepath.Join(root, "tyler")
	if err := os.MkdirAll(filepath.Join(mde, "yt_dlp_plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tyler, "yt_dlp_plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := withPluginDirs([]string{"-J", "https://example.com/v"}, root)
	var pluginArgs []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--plugin-dirs" {
			pluginArgs = append(pluginArgs, args[i+1])
			if strings.Contains(args[i+1], string(os.PathListSeparator)) {
				t.Fatalf("plugin dir must not be PATH-joined: %q", args[i+1])
			}
		}
	}
	if len(pluginArgs) < 2 {
		t.Fatalf("want repeated --plugin-dirs, got %v", args)
	}
}
