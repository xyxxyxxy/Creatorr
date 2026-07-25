package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/xyxxyxxy/Creatorr/internal/api"
	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/events"
	"github.com/xyxxyxxy/Creatorr/internal/health"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/scheduler"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/stats"
	"github.com/xyxxyxxy/Creatorr/internal/streamproxy"
	"github.com/xyxxyxxy/Creatorr/internal/web"
	"github.com/xyxxyxxy/Creatorr/internal/worker"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg := config.Load()
	if err := os.MkdirAll(cfg.ImportRoot, 0o755); err != nil {
		log.Warn("mkdir import root", "path", cfg.ImportRoot, "err", err)
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := settings.SeedDefaults(database); err != nil {
		log.Error("seed settings", "err", err)
		os.Exit(1)
	}
	if err := settings.MigrateExternalBaseURLFromEnv(database); err != nil {
		log.Error("migrate external base URL", "err", err)
		os.Exit(1)
	}
	if err := library.SeedDefaults(database, cfg); err != nil {
		log.Error("seed library defaults", "err", err)
		os.Exit(1)
	}
	if err := ytdlp.EnsurePluginsDir(cfg.YtDlpPluginsDir); err != nil {
		log.Error("yt-dlp plugins dir", "err", err)
		os.Exit(1)
	}
	ytBin, err := ytdlp.ResolveBin()
	if err != nil {
		log.Error("yt-dlp resolve", "err", err)
		os.Exit(1)
	}
	cfg.YtDlpBin = ytBin
	ytVersion, err := ytdlp.VerifyBinary(cfg.YtDlpBin)
	if err != nil {
		log.Error("yt-dlp verify", "err", err)
		os.Exit(1)
	}
	log.Info("yt-dlp ready", "bin", cfg.YtDlpBin, "version", ytVersion)
	ytClient := &ytdlp.Client{
		Bin:              cfg.YtDlpBin,
		PluginsDir:       cfg.YtDlpPluginsDir,
		SystemPluginsDir: cfg.YtDlpSystemPluginsDir,
		PotProviderURL:   cfg.PotProviderURL,
		PotFetch: func() string {
			mode, err := settings.EffectivePotFetch(database, cfg.PotProviderURL)
			if err != nil {
				return settings.PotFetchNever
			}
			return mode
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go (&worker.Heartbeat{DB: database, Log: log}).Run(ctx)

	hub := events.NewHub()
	notify.SetEventsHub(hub)
	q := queue.NewStore(database)
	lib := library.NewStore(database, q)
	lib.ImportRoot = cfg.ImportRoot
	lib.CacheDir = cfg.CacheDir
	if ext, err := settings.ExternalBaseURL(database); err == nil {
		lib.PublicBaseURL = ext
	}
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		log.Warn("mkdir cache dir", "path", cfg.CacheDir, "err", err)
	}
	if n, err := q.CancelStaleStreamPlay(); err != nil {
		log.Error("cancel stale stream_play", "err", err)
	} else if n > 0 {
		log.Info("cancelled orphaned stream_play tasks", "count", n)
	}
	if n, err := q.RequeueStaleRunning(); err != nil {
		log.Error("requeue stale tasks", "err", err)
	} else if n > 0 {
		log.Info("requeued interrupted tasks", "count", n)
	}

	go (&worker.Runner{
		Queue:   q,
		Library: lib,
		Log:     log,
		Events:  hub,
		Handlers: worker.DefaultHandlers(worker.Deps{
			Library: lib,
			Events:  hub,
			YtDlp:   ytClient,
		}),
	}).Run(ctx)
	go (&scheduler.Scheduler{Library: lib, Log: log}).Run(ctx)
	go (&stats.Sampler{DB: database, Log: log}).Run(ctx)

	if flare, err := settings.Get(database, settings.KeyFlareSolverrURL); err == nil {
		cfg.FlareSolverrURL = flare
	}

	srvImpl := &api.Server{
		Health:  &health.Checker{DB: database, Cfg: cfg},
		Queue:   q,
		Library: lib,
		Events:  hub,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	// SSE and stream proxy must not use the global request timeout.
	r.Get("/api/events", srvImpl.EventsSSE)
	streamH := &streamproxy.Handler{Library: lib, YtDlp: ytClient, Queue: q, Events: hub}
	streamH.Mount(r)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))

		r.Get("/api/openapi.json", func(w http.ResponseWriter, req *http.Request) {
			swagger, err := gen.GetSwagger()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(swagger)
		})

		gen.HandlerFromMux(srvImpl, r)

		r.Handle("/static/*", web.StaticHandler())
		ui := &web.Handler{
			Library: lib, Queue: q, YtDlp: ytClient,
			PotProviderURL: cfg.PotProviderURL,
		}
		ui.Mount(r)
	})

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpSrv := &http.Server{Addr: addr, Handler: r}

	go func() {
		if web.WebDev() {
			log.Info("web-dev UI reload enabled", "dir", web.WebDir())
		}
		log.Info("creatorr listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	log.Info("creatorr stopped")
}
