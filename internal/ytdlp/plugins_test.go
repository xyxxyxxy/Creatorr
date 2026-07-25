package ytdlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandPluginDirsParentOfPackages(t *testing.T) {
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
	if len(dirs) != 1 || dirs[0] != root {
		t.Fatalf("want parent search root %q, got %v", root, dirs)
	}
}

func TestExpandPluginDirsPackageRootUsesParent(t *testing.T) {
	parent := t.TempDir()
	pkg := filepath.Join(parent, "bgutil")
	if err := os.MkdirAll(filepath.Join(pkg, "yt_dlp_plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirs := expandPluginDirs(pkg)
	if len(dirs) != 1 || dirs[0] != parent {
		t.Fatalf("want parent %q for package root, got %v", parent, dirs)
	}
}

func TestWithPluginDirsRepeatsFlag(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootA, "mde", "yt_dlp_plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rootB, "bgutil", "yt_dlp_plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	// SystemPluginsDir style: pass package folder; expand lifts to parent.
	pkgB := filepath.Join(rootB, "bgutil")
	args := withPluginDirs([]string{"-J", "https://example.com/v"}, rootA, pkgB)
	var pluginArgs []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--plugin-dirs" {
			pluginArgs = append(pluginArgs, args[i+1])
			if strings.Contains(args[i+1], string(os.PathListSeparator)) {
				t.Fatalf("plugin dir must not be PATH-joined: %q", args[i+1])
			}
		}
	}
	if len(pluginArgs) != 2 {
		t.Fatalf("want 2 --plugin-dirs (operator + system parents), got %v", args)
	}
	if pluginArgs[0] != rootA || pluginArgs[1] != rootB {
		t.Fatalf("want parents %q and %q, got %v", rootA, rootB, pluginArgs)
	}
}
