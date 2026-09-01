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
	"github.com/xyxxyxxy/Creatorr/internal/auth"
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
	"github.com/xyxxyxxy/Creatorr/internal/web"
	"github.com/xyxxyxxy/Creatorr/internal/worker"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg := config.Load()
	auth.SetTrustForwardedProto(cfg.TrustProxy)
	if err := os.MkdirAll(cfg.ImportRoot, 0o755); err != nil {
		log.Warn("mkdir import root", "path", cfg.ImportRoot, "err", err)
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer func() { _ = database.Close() }()

	if err := settings.SeedDefaults(database); err != nil {
		log.Error("seed settings", "err", err)
		os.Exit(1)
	}
	if err := settings.DropFlareSolverrURLSetting(database); err != nil {
		log.Error("drop legacy FlareSolverr URL setting", "err", err)
		os.Exit(1)
	}
	if err := settings.DisablePotFetchWhenUnset(database, cfg.PotProviderURL); err != nil {
		log.Error("disable pot_fetch when provider URL unset", "err", err)
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
	ytPaths := ytdlp.PathsForLayout(ytdlp.DataDirExists())
	updateChannel, _ := settings.Get(database, settings.KeyYtDlpUpdateChannel)
	ytVersion, err := ytdlp.PrepareManagedBin(context.Background(), ytdlp.PrepareOpts{
		Bootstrap: ytPaths.Bootstrap,
		Managed:   ytPaths.Managed,
		Channel:   settings.NormalizeYtDlpUpdateChannel(updateChannel),
	})
	if err != nil {
		log.Error("yt-dlp prepare", "err", err)
		os.Exit(1)
	}
	cfg.YtDlpBin = ytPaths.Managed
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

	hb := &worker.HeartbeatState{}
	go (&worker.Heartbeat{State: hb, Log: log}).Run(ctx)

	hub := events.NewHub()
	notify.SetEventsHub(hub)
	q := queue.NewStore(database)
	lib := library.NewStore(database, q)
	lib.ImportRoot = cfg.ImportRoot
	lib.CacheDir = cfg.CacheDir
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		log.Warn("mkdir cache dir", "path", cfg.CacheDir, "err", err)
	}
	if n, err := q.RequeueStaleRunning(); err != nil {
		log.Error("requeue stale tasks", "err", err)
	} else if n > 0 {
		log.Info("requeued interrupted tasks", "count", n)
	}

	if enabled, err := settings.YtDlpUpdatesEnabled(database); err != nil {
		log.Error("yt-dlp updates enabled check", "err", err)
	} else if enabled {
		if id, err := lib.EnqueueYtDlpUpdate(queue.PriorityYtDlpUpdateBoot, "boot"); err != nil {
			log.Warn("yt-dlp boot update enqueue", "err", err)
		} else if id > 0 {
			log.Info("yt-dlp boot update enqueued", "task", id)
		}
	} else {
		log.Info("yt-dlp automatic updates disabled; skipping boot update")
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

	healthChecker := &health.Checker{
		DB:  database,
		Cfg: cfg,
		WorkerAt: func() time.Time {
			return hb.At()
		},
	}
	srvImpl := &api.Server{
		Health:  healthChecker,
		Queue:   q,
		Library: lib,
		Events:  hub,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(auth.Middleware(database))

	// SSE must not use the global request timeout.
	r.Get("/api/events", srvImpl.EventsSSE)

	r.Group(func(r chi.Router) {
		// Import folder WalkDir can exceed 60s on slow disks; skip timeout for that route only.
		timeout := middleware.Timeout(60 * time.Second)
		r.Use(func(next http.Handler) http.Handler {
			timed := timeout(next)
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method == http.MethodPost && req.URL.Path == "/api/import/scan" {
					next.ServeHTTP(w, req)
					return
				}
				timed.ServeHTTP(w, req)
			})
		})

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
			FlareSolverrURL: cfg.FlareSolverrURL,
			PotProviderURL:  cfg.PotProviderURL,
			Health:          healthChecker,
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
	ytdlp.DestroyAllFlareSessions(context.Background())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	log.Info("creatorr stopped")
}
