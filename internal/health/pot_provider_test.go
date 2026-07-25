package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/config"
)

func TestCheckPotProvider(t *testing.T) {
	c := &Checker{Cfg: config.Config{}}
	ch := c.checkPotProvider(context.Background())
	if ch.Status != StatusSkipped {
		t.Fatalf("unset URL: %#v", ch)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" {
			t.Fatalf("path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"server_uptime":1,"version":"1.3.1"}`))
	}))
	t.Cleanup(srv.Close)

	c.Cfg.PotProviderURL = srv.URL
	ch = c.checkPotProvider(context.Background())
	if ch.Status != StatusOK || ch.Name != "pot_provider" {
		t.Fatalf("ok ping: %#v", ch)
	}
}
