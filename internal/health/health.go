package health

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// Status is overall or per-check health.
type Status string

const (
	StatusOK       Status = "ok"
	StatusDegraded Status = "degraded"
	StatusDown     Status = "down"
	StatusSkipped  Status = "skipped" // mapped to ok with message for OpenAPI enum
)

// Check is one dependency probe result.
type Check struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Report is the /api/health payload.
type Report struct {
	Status Status  `json:"status"`
	Checks []Check `json:"checks"`
}

// Checker runs dependency checks.
type Checker struct {
	DB  *db.DB
	Cfg config.Config
}

// Run executes all checks and rolls up overall status.
func (c *Checker) Run(ctx context.Context) Report {
	checks := []Check{
		c.checkDB(ctx),
		c.checkWorker(ctx),
		c.checkYtDlp(ctx),
		c.checkDisk(),
		c.checkFlareSolverr(ctx),
		c.checkPotProvider(ctx),
	}
	overall := StatusOK
	for _, ch := range checks {
		st := ch.Status
		if st == StatusSkipped {
			continue
		}
		if st == StatusDown {
			overall = StatusDown
			break
		}
		if st == StatusDegraded && overall == StatusOK {
			overall = StatusDegraded
		}
	}
	// OpenAPI HealthStatus enum is ok|degraded|down - map skipped checks to ok.
	out := make([]Check, len(checks))
	for i, ch := range checks {
		out[i] = ch
		if ch.Status == StatusSkipped {
			out[i].Status = StatusOK
			if out[i].Message == "" {
				out[i].Message = "skipped"
			}
		}
	}
	return Report{Status: overall, Checks: out}
}

func (c *Checker) checkDB(ctx context.Context) Check {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// PingContext so timeout cancels the wait - plain Ping() ignores cancel and
	// can leave a waiter stuck if the pool is saturated.
	if err := c.DB.SQL.PingContext(ctx); err != nil {
		if ctx.Err() != nil {
			return Check{Name: "db", Status: StatusDown, Message: "ping timeout"}
		}
		return Check{Name: "db", Status: StatusDown, Message: err.Error()}
	}
	return Check{Name: "db", Status: StatusOK}
}

func (c *Checker) checkWorker(ctx context.Context) Check {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	at, err := c.DB.WorkerHeartbeatContext(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return Check{Name: "worker", Status: StatusDegraded, Message: "heartbeat timeout"}
		}
		return Check{Name: "worker", Status: StatusDegraded, Message: err.Error()}
	}
	if at.IsZero() {
		return Check{Name: "worker", Status: StatusDegraded, Message: "no heartbeat yet"}
	}
	if time.Since(at) > 30*time.Second {
		return Check{Name: "worker", Status: StatusDegraded, Message: "heartbeat stale"}
	}
	return Check{Name: "worker", Status: StatusOK, Message: at.UTC().Format(time.RFC3339)}
}

func (c *Checker) checkYtDlp(ctx context.Context) Check {
	_ = ctx
	bin := strings.TrimSpace(c.Cfg.YtDlpBin)
	if bin == "" {
		bin = "/usr/local/bin/yt-dlp"
	}
	fi, err := os.Stat(bin)
	if err != nil {
		return Check{Name: "ytdlp", Status: StatusDegraded, Message: "yt-dlp binary missing: " + bin}
	}
	if fi.IsDir() {
		return Check{Name: "ytdlp", Status: StatusDegraded, Message: "yt-dlp path is a directory: " + bin}
	}
	return Check{Name: "ytdlp", Status: StatusOK, Message: bin}
}

func (c *Checker) checkDisk() Check {
	roots := []string{c.Cfg.LibraryRoot, c.Cfg.ImportRoot}
	for _, root := range roots {
		if root == "" {
			continue
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			return Check{Name: "disk", Status: StatusDegraded, Message: err.Error()}
		}
		dir := root
		if !filepath.IsAbs(dir) {
			wd, _ := os.Getwd()
			dir = filepath.Join(wd, dir)
		}
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() {
			return Check{Name: "disk", Status: StatusDegraded, Message: "root not usable: " + root}
		}
	}
	return Check{Name: "disk", Status: StatusOK}
}

// ExternalServices probes FlareSolverr and the PO token provider and returns raw
// statuses (including skipped when the URL is unset). Unlike Run, skipped is not
// mapped to ok.
func (c *Checker) ExternalServices(ctx context.Context) (flare, pot Check) {
	return c.checkFlareSolverr(ctx), c.checkPotProvider(ctx)
}

func (c *Checker) checkFlareSolverr(ctx context.Context) Check {
	url := c.Cfg.FlareSolverrURL
	if url == "" {
		return Check{Name: "flaresolverr", Status: StatusSkipped, Message: "URL unset"}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Check{Name: "flaresolverr", Status: StatusDegraded, Message: err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Check{Name: "flaresolverr", Status: StatusDegraded, Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return Check{Name: "flaresolverr", Status: StatusDegraded, Message: resp.Status}
	}
	return Check{Name: "flaresolverr", Status: StatusOK}
}

func (c *Checker) checkPotProvider(ctx context.Context) Check {
	base := strings.TrimSpace(c.Cfg.PotProviderURL)
	if base == "" {
		return Check{Name: "pot_provider", Status: StatusSkipped, Message: "URL unset"}
	}
	pingURL := strings.TrimRight(base, "/") + "/ping"
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pingURL, nil)
	if err != nil {
		return Check{Name: "pot_provider", Status: StatusDegraded, Message: err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Check{Name: "pot_provider", Status: StatusDegraded, Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return Check{Name: "pot_provider", Status: StatusDegraded, Message: resp.Status}
	}
	return Check{Name: "pot_provider", Status: StatusOK}
}
