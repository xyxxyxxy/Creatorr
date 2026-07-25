package settings_test

import (
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestNormalizeEpisodeFormatDefaults(t *testing.T) {
	if got := settings.NormalizeEpisodeFormat(""); got != settings.DefaultEpisodeFormat {
		t.Fatalf("empty: %q", got)
	}
	if got := settings.NormalizeEpisodeFormat("  {title} [{id}]  "); got != "{title} [{id}]" {
		t.Fatalf("trim: %q", got)
	}
}

func TestValidateEpisodeFormat(t *testing.T) {
	if err := settings.ValidateEpisodeFormat("{title} [{id}]"); err != nil {
		t.Fatal(err)
	}
	if err := settings.ValidateEpisodeFormat("{foo}"); err == nil {
		t.Fatal("expected unknown token error")
	}
	if err := settings.ValidateEpisodeFormat(""); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultEpisodeFormat(t *testing.T) {
	if settings.DefaultEpisodeFormat == "" {
		t.Fatal("empty default")
	}
	if strings.Contains(settings.DefaultEpisodeFormat, "{series") {
		t.Fatalf("default must not include series token: %q", settings.DefaultEpisodeFormat)
	}
	if !strings.Contains(settings.DefaultEpisodeFormat, "{episode:000000}") {
		t.Fatalf("default should zero-pad episode: %q", settings.DefaultEpisodeFormat)
	}
	if err := settings.ValidateEpisodeFormat(settings.DefaultEpisodeFormat); err != nil {
		t.Fatal(err)
	}
}
