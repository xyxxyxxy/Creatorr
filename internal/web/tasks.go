package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

type taskView struct {
	ID          int64
	Position    int
	Status      string
	Kind        string
	Domain      string // set for cross-domain overview list
	SeriesID    int64
	SeriesTitle string
	VideoID     int64
	VideoTitle  string
	Message     string
	Progress    *float64
	LanePaused  bool // domain soft-pause: pending bars use warning
}

type laneView struct {
	Domain              string
	Paused              bool
	CanPause            bool // false for system / reserved lanes
	Page                PageInfo
	PendingCount        int
	DownloadCount       int
	ShowCancelPending   bool
	ShowCooldown        bool
	ShowBusy            bool // running >= max parallel (not paused / cooling)
	ShowActive          bool // running > 0 and below max parallel (not paused / cooling)
	RunningCount        int
	CooldownEndsAt      string  // RFC3339Nano for JS tick
	CooldownTotalSec    int     // configured task_cooldown_seconds (progress max)
	CooldownRemSec      int     // remaining seconds at render
	CooldownTip         string  // human wait tip for tooltip / aria-label
	TaskCooldownSeconds int     // effective cooldown setting (host lanes)
	MaxDownloadQueue    int     // effective max download queue (host lanes)
	RateLimit           string  // effective yt-dlp --limit-rate display (download)
	SleepRequests       float64 // effective yt-dlp --sleep-requests
	MaxParallelTasks    int     // effective max parallel (system = 1)
	UseFlareSolverr     bool    // effective Use FlareSolverr
	HasCookies          bool
	CookiesFromHost     bool // host jar (not Domain defaults fallback)
	CookiesTip          string
	HasCredentials      bool
	CredentialsFromHost bool
	CredentialsTip      string
	FlareWarm           bool
	FlareTip            string
	HasOverrideRow      bool
	CooldownOverride    string
	QueueOverride       string
	ParallelOverride    string
	RateOverride        string
	SleepOverride       string
	FlareOverride       string // default|on|off (empty = no row / inherit)
	CookieContent       string
	DefaultCooldown     int
	DefaultQueue        int
	DefaultParallel     int
	DefaultRate         string
	DefaultSleep        float64
	DefaultFlare        bool
	Tasks               []taskView          // pending + running (ListActive order)
	ScheduledTasks      []scheduledTaskView // system lane: upcoming scheduler jobs
}

func (h *Handler) tasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.Queue.ListActive()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	known, err := domains.List(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	knownMeta := map[string]domains.Domain{}
	for _, d := range known {
		knownMeta[d.Domain] = d
	}

	titles := map[int64]string{}
	videoTitles := map[int64]string{}
	lanesMap := map[string]*laneView{}
	var order []string

	defLim, _ := settings.DefaultLimits(h.Queue.DB)
	ensureLane := func(domain string) *laneView {
		lv, ok := lanesMap[domain]
		if ok {
			return lv
		}
		canPause := domain != queue.SystemDomain && domain != "unknown" && domain != settings.DomainDefault
		paused := false
		if canPause {
			if p, err := domains.IsPaused(h.Queue.DB, domain); err == nil {
				paused = p
			}
		}
		lv = &laneView{
			Domain:          domain,
			Paused:          paused,
			CanPause:        canPause,
			DefaultCooldown: defLim.TaskCooldownSeconds,
			DefaultQueue:    defLim.MaxDownloadQueue,
			DefaultParallel: defLim.MaxParallelTasks,
			DefaultRate:     defLim.DownloadRateLimit,
			DefaultSleep:    defLim.SleepRequests,
			DefaultFlare:    defLim.UseFlareSolverr,
		}
		if domain == queue.SystemDomain {
			lv.MaxParallelTasks = 1
		} else {
			// Effective Domain defaults ∪ host overrides (LimitsForDomain).
			lv.RateLimit = defLim.DownloadRateLimit
			lv.SleepRequests = defLim.SleepRequests
			lv.MaxParallelTasks = defLim.MaxParallelTasks
			lv.MaxDownloadQueue = defLim.MaxDownloadQueue
			lv.TaskCooldownSeconds = defLim.TaskCooldownSeconds
			lv.UseFlareSolverr = defLim.UseFlareSolverr
			if lim, err := settings.LimitsForDomain(h.Queue.DB, domain); err == nil {
				lv.RateLimit = lim.DownloadRateLimit
				lv.SleepRequests = lim.SleepRequests
				lv.MaxParallelTasks = lim.MaxParallelTasks
				lv.MaxDownloadQueue = lim.MaxDownloadQueue
				lv.TaskCooldownSeconds = lim.TaskCooldownSeconds
				lv.UseFlareSolverr = lim.UseFlareSolverr
			}
			if lv.UseFlareSolverr {
				lv.FlareTip = "FlareSolverr on (host Domain override)"
				if ytdlp.HasFlareSession(domain) {
					lv.FlareWarm = true
					lv.FlareTip = "FlareSolverr session warm"
				}
			} else {
				lv.FlareTip = "FlareSolverr off (host Domain override)"
			}
			if ok, _, err := domains.CookiesApply(h.Queue.DB, domain); err == nil && ok {
				lv.HasCookies = true
				lv.CookiesFromHost = true
				lv.CookiesTip = "Cookies set (host jar)"
			} else {
				lv.CookiesTip = "No cookies (host Domain override)"
			}
			if creds, err := settings.CredentialsForDomain(h.Queue.DB, domain); err == nil && strings.TrimSpace(creds.Username) != "" {
				lv.HasCredentials = true
				lv.CredentialsFromHost = true
				lv.CredentialsTip = "Credentials set (host override)"
			} else {
				lv.CredentialsTip = "No credentials (host Domain override)"
			}
			if meta, ok := knownMeta[domain]; ok {
				lv.HasOverrideRow = true
				lv.FlareOverride = domains.FlareOverrideLabel(meta.UseFlareSolverr)
				if meta.TaskCooldownSeconds.Valid {
					lv.CooldownOverride = strconv.FormatInt(meta.TaskCooldownSeconds.Int64, 10)
				}
				if meta.MaxDownloadQueue.Valid {
					lv.QueueOverride = strconv.FormatInt(meta.MaxDownloadQueue.Int64, 10)
				}
				if meta.MaxParallelTasks.Valid {
					lv.ParallelOverride = strconv.FormatInt(meta.MaxParallelTasks.Int64, 10)
				}
				if meta.DownloadRateLimit.Valid {
					s := strings.TrimSpace(meta.DownloadRateLimit.String)
					if s == "" {
						s = "off"
					}
					lv.RateOverride = s
				}
				if meta.SleepRequests.Valid {
					lv.SleepOverride = strconv.FormatFloat(meta.SleepRequests.Float64, 'f', -1, 64)
				}
			}
			if c, err := domains.GetCookies(h.Queue.DB, domain); err == nil {
				lv.CookieContent = c
			}
		}
		lanesMap[domain] = lv
		order = append(order, domain)
		return lv
	}

	// System lane always visible (maintenance + SponsorBlock cuts); pin first.
	ensureLane(queue.SystemDomain)
	for _, d := range known {
		if d.Domain == settings.DomainDefault {
			continue
		}
		ensureLane(d.Domain)
	}
	// Source hostnames always get a lane even without a domains override row.
	if sourceHosts, err := h.Library.ListSourceDomains(); err == nil {
		for _, host := range sourceHosts {
			ensureLane(host)
		}
	}
	// Soft-paused hosts always get a lane (auto-pause from failed prefetch before any
	// series/source exists would otherwise leave the nav badge with no Resume target).
	if pausedHosts, err := domains.ListPaused(h.Queue.DB); err == nil {
		for _, host := range pausedHosts {
			ensureLane(host)
		}
	}
	for _, t := range tasks {
		lv := ensureLane(t.Domain)
		tv := taskView{
			ID: t.ID, Position: t.QueuePos, Status: t.Status, Kind: t.Kind, Message: t.Message,
			LanePaused: lv.Paused,
		}
		if t.SeriesID.Valid {
			tv.SeriesID = t.SeriesID.Int64
			if title, ok := titles[tv.SeriesID]; ok {
				tv.SeriesTitle = title
			} else if ser, err := h.Library.GetSeries(tv.SeriesID, false); err == nil {
				titles[tv.SeriesID] = ser.Title
				tv.SeriesTitle = ser.Title
			}
		}
		if t.VideoID.Valid {
			tv.VideoID = t.VideoID.Int64
			if title, ok := videoTitles[tv.VideoID]; ok {
				tv.VideoTitle = title
			} else if v, err := h.Library.GetVideo(tv.VideoID); err == nil {
				videoTitles[tv.VideoID] = v.Title
				tv.VideoTitle = v.Title
				if tv.SeriesID == 0 {
					tv.SeriesID = v.SeriesID
				}
			}
		}
		if t.Progress.Valid {
			p := t.Progress.Float64
			tv.Progress = &p
		}
		lv.Tasks = append(lv.Tasks, tv)
		if t.Status == queue.StatusPending {
			lv.PendingCount++
		}
		if t.Status == queue.StatusRunning {
			lv.RunningCount++
		}
		if t.Kind == queue.KindDownload {
			lv.DownloadCount++
		}
	}

	// Stable order: system first, then known domains (alphabetical), then any extras from tasks.
	var rest []string
	for _, d := range order {
		if d == queue.SystemDomain {
			continue
		}
		rest = append(rest, d)
	}
	sort.Strings(rest)
	order = append([]string{queue.SystemDomain}, rest...)
	lanes := make([]laneView, 0, len(order))
	for _, d := range order {
		lv := lanesMap[d]
		if lv == nil {
			continue
		}
		lv.ShowCancelPending = lv.PendingCount > 0
		if d != queue.SystemDomain {
			if until := h.Queue.CooldownUntil(d); !until.IsZero() {
				rem := int(time.Until(until).Round(time.Second) / time.Second)
				if rem < 1 {
					rem = 1
				}
				total := lv.TaskCooldownSeconds
				if total < 1 {
					total = rem
				}
				if rem > total {
					total = rem
				}
				lv.ShowCooldown = true
				lv.CooldownEndsAt = until.UTC().Format(time.RFC3339Nano)
				lv.CooldownTotalSec = total
				lv.CooldownRemSec = rem
				lv.CooldownTip = cooldownWaitTip(rem)
			}
			if !lv.Paused && !lv.ShowCooldown && lv.MaxParallelTasks > 0 && lv.RunningCount >= lv.MaxParallelTasks {
				lv.ShowBusy = true
			} else if !lv.Paused && !lv.ShowCooldown && lv.RunningCount > 0 {
				lv.ShowActive = true
			}
		}
		pageTasks, pageInfo := SlicePageSize(r, lanePageParam(d), lv.Tasks, TaskPageSize)
		pageInfo.LiveTarget = "tasks-live"
		lv.Tasks = pageTasks
		lv.Page = pageInfo
		lanes = append(lanes, *lv)
	}
	flareOK := strings.TrimSpace(h.FlareSolverrURL) != ""

	now := time.Now().UTC()
	scheduled, err := buildScheduledTasks(h.Library, now)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	markScheduledBusy(h.Queue, scheduled)
	for i := range lanes {
		if lanes[i].Domain == queue.SystemDomain {
			lanes[i].ScheduledTasks = scheduled
			break
		}
	}

	render(w, "tasks", struct {
		pageBase
		Lanes           []laneView
		FlareConfigured bool
	}{
		pageBase:        newPage("Tasks", "tasks", flashFromQuery(r)),
		Lanes:           lanes,
		FlareConfigured: flareOK,
	})
}

func (h *Handler) actionRunScheduled(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	key := strings.TrimSpace(r.FormValue("key"))
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir != "/tasks" {
		redir = "/tasks"
	}
	if h.Library == nil {
		http.Redirect(w, r, redir+"?err="+urlQuery("library unavailable"), http.StatusSeeOther)
		return
	}

	switch key {
	case settings.KeyDownloadWantedCron:
		n, err := h.Library.EnqueueDownloadWanted()
		if err != nil {
			http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
			return
		}
		if _, _, err := h.Library.EnqueueMaturityDue(); err != nil {
			http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
			return
		}
		an, err := h.Library.EnqueueWantedArchiveBackfill(32)
		if err != nil {
			http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
			return
		}
		if n+an == 0 {
			http.Redirect(w, r, redir+"?ok=download-wanted-empty", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, redir+"?ok=download-wanted-queued", http.StatusSeeOther)
		return

	case settings.KeySyncFilesCron:
		if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindSyncFiles, queue.SystemDomain); busy {
			http.Redirect(w, r, redir+"?err="+urlQuery("File sync already queued"), http.StatusSeeOther)
			return
		}
		id, err := h.Library.EnqueueSyncFiles(queue.PrioritySyncFilesDue)
		if err != nil {
			http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
			return
		}
		if id == 0 {
			http.Redirect(w, r, redir+"?ok=sync-files-empty", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, redir+"?ok=sync-files-queued", http.StatusSeeOther)
		return

	case settings.KeyRetentionDeleteCron:
		if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindRetentionDelete, queue.SystemDomain); busy {
			http.Redirect(w, r, redir+"?err="+urlQuery("Retention delete already queued"), http.StatusSeeOther)
			return
		}
		id, err := h.Library.EnqueueRetentionDelete(queue.PriorityRetentionDeleteDue)
		if err != nil {
			http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
			return
		}
		if id == 0 {
			http.Redirect(w, r, redir+"?ok=retention-delete-empty", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, redir+"?ok=retention-delete-queued", http.StatusSeeOther)
		return

	case settings.KeyYtDlpUpdateCron:
		enabled, err := settings.YtDlpUpdatesEnabled(h.Queue.DB)
		if err != nil {
			http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
			return
		}
		if !enabled {
			http.Redirect(w, r, redir+"?err="+urlQuery("Automatic yt-dlp updates disabled"), http.StatusSeeOther)
			return
		}
		if busy, _ := h.Queue.HasPendingOrRunningKind(queue.KindYtDlpUpdate, queue.SystemDomain); busy {
			http.Redirect(w, r, redir+"?err="+urlQuery("yt-dlp update already queued or running"), http.StatusSeeOther)
			return
		}
		id, err := h.Library.EnqueueYtDlpUpdate(queue.PriorityYtDlpUpdateDue, "manual")
		if err != nil {
			http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
			return
		}
		if id == 0 {
			http.Redirect(w, r, redir+"?err="+urlQuery("yt-dlp update not enqueued"), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, redir+"?ok=ytdlp-update-queued", http.StatusSeeOther)
		return

	default:
		http.Redirect(w, r, redir+"?err="+urlQuery("unknown schedule"), http.StatusSeeOther)
		return
	}
}

// lanePageParam is the /tasks pager query key for one domain lane (p_example_com).
func lanePageParam(domain string) string {
	var b strings.Builder
	b.WriteString("p_")
	for _, r := range strings.ToLower(domain) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "p_" {
		return "p_unknown"
	}
	return s
}

func (h *Handler) actionCancelTask(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	_, _ = h.Queue.CancelWithMessage(id, "Cancelled")
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir == "" || !strings.HasPrefix(redir, "/") || strings.HasPrefix(redir, "//") {
		redir = "/tasks"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

func (h *Handler) actionCancelDomainTasks(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := settings.NormalizeDomain(r.FormValue("domain"))
	if domain != "" {
		_, _ = h.Queue.CancelPendingDomain(domain)
	}
	http.Redirect(w, r, "/tasks", http.StatusSeeOther)
}

func (h *Handler) actionSetDomainPaused(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := formDomain(r)
	paused := r.FormValue("paused") == "1"
	if err := domains.SetPaused(h.Queue.DB, domain, paused); err != nil {
		redir := r.FormValue("redirect")
		if redir == "" {
			redir = "/tasks"
		}
		http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = "/tasks"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}
