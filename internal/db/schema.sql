-- Creatorr fresh schema (Go). No migration from older databases.

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS root_folders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL UNIQUE,
  retention_ttl_seconds INTEGER
);

CREATE TABLE IF NOT EXISTS quality_profiles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  format_selector TEXT NOT NULL,
  maturity_redownload_hours INTEGER NOT NULL DEFAULT 0,
  maturity_sidecar_hours INTEGER NOT NULL DEFAULT 0,
  sponsorblock_mark TEXT NOT NULL DEFAULT '[]',
  sponsorblock_remove TEXT NOT NULL DEFAULT '[]',
  sponsorblock_reencode_cut INTEGER NOT NULL DEFAULT 0,
  sponsorblock_info_cards INTEGER NOT NULL DEFAULT 0,
  verify_media INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS series (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  root_id INTEGER NOT NULL REFERENCES root_folders(id),
  quality_profile_id INTEGER NOT NULL REFERENCES quality_profiles(id),
  monitored INTEGER NOT NULL DEFAULT 1,
  delivery_mode TEXT NOT NULL DEFAULT 'video',
  added_at TEXT NOT NULL,
  -- Show metadata for tvshow.nfo (art files live on disk only; year/status derived).
  plot TEXT NOT NULL DEFAULT '',
  sorttitle TEXT NOT NULL DEFAULT '',
  originaltitle TEXT NOT NULL DEFAULT '',
  studio TEXT NOT NULL DEFAULT '',
  genres TEXT NOT NULL DEFAULT '[]',
  tags TEXT NOT NULL DEFAULT '[]',
  uniqueid_type TEXT NOT NULL DEFAULT '',
  uniqueid_value TEXT NOT NULL DEFAULT '',
  actors TEXT NOT NULL DEFAULT '[]',
  tagline TEXT NOT NULL DEFAULT '',
  country TEXT NOT NULL DEFAULT '',
  mpaa TEXT NOT NULL DEFAULT '',
  premiered TEXT NOT NULL DEFAULT '',
  auto_ignore_media_types TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS sources (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  url TEXT NOT NULL,
  label TEXT,
  kind TEXT NOT NULL DEFAULT 'feed',
  scan_cron TEXT NOT NULL DEFAULT '0 3 * * 0',
  index_as_ignored INTEGER NOT NULL DEFAULT 0,
  title_regexp_include TEXT,
  title_regexp_exclude TEXT,
  scan_cutoff TEXT,
  full_scan_done INTEGER NOT NULL DEFAULT 0,
  UNIQUE(series_id, url)
);

CREATE TABLE IF NOT EXISTS videos (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  source_id INTEGER REFERENCES sources(id) ON DELETE SET NULL,
  remote_id TEXT NOT NULL,
  title TEXT NOT NULL,
  upload_date TEXT,
  source_url TEXT,
  status TEXT NOT NULL DEFAULT 'wanted',
  season INTEGER,
  episode INTEGER,
  description TEXT DEFAULT '',
  thumbnail_url TEXT,
  media_type TEXT NOT NULL DEFAULT '',
  duration_seconds INTEGER,
  width INTEGER,
  height INTEGER,
  fps REAL,
  download_format_selector TEXT,
  download_remux_container TEXT,
  tool TEXT,
  import_src TEXT,
  acquired_at TEXT,
  sidecars_acquired_at TEXT,
  -- Episode metadata for episodedetails NFO (description = plot).
  sorttitle TEXT NOT NULL DEFAULT '',
  originaltitle TEXT NOT NULL DEFAULT '',
  studio TEXT NOT NULL DEFAULT '',
  genres TEXT NOT NULL DEFAULT '[]',
  tags TEXT NOT NULL DEFAULT '[]',
  uniqueid_type TEXT NOT NULL DEFAULT '',
  uniqueid_value TEXT NOT NULL DEFAULT '',
  actors TEXT NOT NULL DEFAULT '[]',
  tagline TEXT NOT NULL DEFAULT '',
  country TEXT NOT NULL DEFAULT '',
  mpaa TEXT NOT NULL DEFAULT '',
  UNIQUE(series_id, remote_id)
);

CREATE TABLE IF NOT EXISTS files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  video_id INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  kind TEXT NOT NULL,
  acquired_at TEXT NOT NULL,
  size_bytes INTEGER
);

CREATE TABLE IF NOT EXISTS tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  series_id INTEGER REFERENCES series(id) ON DELETE SET NULL,
  video_id INTEGER REFERENCES videos(id) ON DELETE SET NULL,
  payload TEXT NOT NULL DEFAULT '{}',
  error_code TEXT,
  error_message TEXT,
  message TEXT,
  detail TEXT,
  commands TEXT NOT NULL DEFAULT '[]',
  progress REAL,
  domain TEXT NOT NULL DEFAULT 'unknown',
  priority INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);

CREATE TABLE IF NOT EXISTS cookies (
  domain TEXT PRIMARY KEY,
  content TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

-- Soft pause per hostname (claim stop). Missing row = not paused.
-- Never creates domains override rows; resume deletes the row.
CREATE TABLE IF NOT EXISTS domain_runtime (
  domain TEXT PRIMARY KEY,
  paused INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL
);

-- Known hostnames + reserved domain=default (global limit defaults).
-- Host rows: NULL task_cooldown_seconds / max_download_queue / max_parallel_tasks / download_rate_limit / sleep_requests / use_flaresolverr → use domain=default.
-- domain=default limit columns + use_flaresolverr must be non-NULL. Domains are never auto-deleted when sources go away.
CREATE TABLE IF NOT EXISTS domains (
  domain TEXT PRIMARY KEY,
  active INTEGER NOT NULL DEFAULT 1,
  task_cooldown_seconds INTEGER,
  max_download_queue INTEGER,
  max_parallel_tasks INTEGER,
  download_rate_limit TEXT,
  sleep_requests REAL,
  use_flaresolverr INTEGER,
  username TEXT,
  password TEXT,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Apprise notification channels (URL + subscribed event ids as JSON array).
-- Creatorr in-app delivery is a virtual channel (creatorr://in-app), not a row here.
CREATE TABLE IF NOT EXISTS notify_channels (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL,
  events TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

-- In-app notification log (written by the Creatorr channel; Apprise is optional fan-out).
CREATE TABLE IF NOT EXISTS notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at TEXT NOT NULL,
  event TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  task_id INTEGER REFERENCES tasks(id),
  external_ok INTEGER NOT NULL DEFAULT 0,
  read_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_notifications_id ON notifications(id DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_unread ON notifications(read_at, id DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_task ON notifications(task_id);

CREATE TABLE IF NOT EXISTS video_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  video_id INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  event TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '{}',
  task_id INTEGER NOT NULL REFERENCES tasks(id)
);

CREATE TABLE IF NOT EXISTS source_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  event TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '{}',
  task_id INTEGER NOT NULL REFERENCES tasks(id)
);
CREATE INDEX IF NOT EXISTS idx_source_history_source ON source_history(source_id, id DESC);

CREATE TABLE IF NOT EXISTS worker_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  heartbeat_at TEXT NOT NULL
);

-- Time-series samples for /stats (queue depth, video status, library size).
CREATE TABLE IF NOT EXISTS stats_samples (
  sampled_at TEXT NOT NULL,
  metric TEXT NOT NULL,
  value INTEGER NOT NULL,
  PRIMARY KEY (sampled_at, metric)
);
CREATE INDEX IF NOT EXISTS idx_stats_samples_at ON stats_samples(sampled_at);
