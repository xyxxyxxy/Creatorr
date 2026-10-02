package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	_ "modernc.org/sqlite"
)

func TestOpenFreshSchema(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "creatorr.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "tasks", "queue_seq", true)
	assertColumn(t, d.SQL, "tasks", "interrupt_count", true)
	assertColumn(t, d.SQL, "tasks", "priority", false)
	assertColumn(t, d.SQL, "sources", "full_scan_limit", true)
	assertColumn(t, d.SQL, "sources", "scan_cutoff", false)
	assertColumn(t, d.SQL, "sources", "auto_ignore_media_types", false)
	assertColumn(t, d.SQL, "series", "auto_ignore_media_types", false)
	assertColumn(t, d.SQL, "videos", "acquired_via", true)
	assertColumnNotNull(t, d.SQL, "videos", "acquired_via", false)
	assertColumn(t, d.SQL, "videos", "tool", false)
	assertColumn(t, d.SQL, "root_folders", "episode_format", true)
	assertColumn(t, d.SQL, "domains", "smart_cookies", true)
	assertColumn(t, d.SQL, "domains", "cookies_after_fail", false)
	assertColumn(t, d.SQL, "sources", "cookie_smart_prefer", true)
	assertColumn(t, d.SQL, "files", "content_hash", true)
	assertColumn(t, d.SQL, "files", "content_hash_checked_at", true)
	assertColumn(t, d.SQL, "files", "content_hash_ok_at", true)
	assertColumn(t, d.SQL, "tasks", "logs", true)
	assertColumn(t, d.SQL, "series", "notes", true)
	assertColumn(t, d.SQL, "videos", "notes", true)
}

func TestMigrateV2AddsFullScanLimitDropsCutoff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (1);
		CREATE TABLE root_folders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL UNIQUE,
			retention_ttl_seconds INTEGER
		);
		CREATE TABLE quality_profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			format_selector TEXT NOT NULL
		);
		CREATE TABLE series (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			root_id INTEGER NOT NULL REFERENCES root_folders(id),
			quality_profile_id INTEGER NOT NULL REFERENCES quality_profiles(id),
			monitored INTEGER NOT NULL DEFAULT 1,
			added_at TEXT NOT NULL
		);
		CREATE TABLE sources (
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
		INSERT INTO root_folders (path) VALUES ('/tmp/root');
		INSERT INTO quality_profiles (name, format_selector) VALUES ('Default', 'bv*+ba/b');
		INSERT INTO series (title, root_id, quality_profile_id, added_at) VALUES ('S', 1, 1, '2026-01-01T00:00:00Z');
		INSERT INTO sources (series_id, url, scan_cutoff) VALUES (1, 'https://www.example.com/@x', '2020-01-01');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "sources", "full_scan_limit", true)
	assertColumn(t, d.SQL, "sources", "scan_cutoff", false)

	var limit int
	if err := d.SQL.QueryRow(`SELECT full_scan_limit FROM sources WHERE id = 1`).Scan(&limit); err != nil {
		t.Fatal(err)
	}
	if limit != 0 {
		t.Fatalf("full_scan_limit=%d want 0 default", limit)
	}
}

func TestMigrateV3ClearsSourceHold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hold.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (2);
		CREATE TABLE series (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			root_id INTEGER NOT NULL,
			quality_profile_id INTEGER NOT NULL,
			monitored INTEGER NOT NULL DEFAULT 1,
			added_at TEXT NOT NULL
		);
		CREATE TABLE sources (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL,
			url TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'feed',
			scan_cron TEXT NOT NULL DEFAULT '',
			index_as_ignored INTEGER NOT NULL DEFAULT 0,
			full_scan_limit INTEGER NOT NULL DEFAULT 0,
			full_scan_done INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL,
			source_id INTEGER,
			remote_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'wanted'
		);
		CREATE TABLE video_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			video_id INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			event TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '',
			task_id INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO series (id, title, root_id, quality_profile_id, added_at) VALUES (1, 'S', 1, 1, '2026-01-01T00:00:00Z');
		INSERT INTO sources (id, series_id, url) VALUES (1, 1, 'https://www.example.com/@x');
		INSERT INTO videos (id, series_id, source_id, remote_id, title, status) VALUES (1, 1, 1, 'h1', 'H1', 'wanted_source_error');
		INSERT INTO videos (id, series_id, source_id, remote_id, title, status) VALUES (2, 1, 1, 'h2', 'H2', 'wanted_download_error');
		INSERT INTO video_history (video_id, created_at, event, message, task_id) VALUES (1, '2026-01-01T00:00:00Z', 'source_failed', 'Held', 1);
		INSERT INTO video_history (video_id, created_at, event, message, task_id) VALUES (2, '2026-01-01T00:00:00Z', 'wanted_download_error', 'legacy', 2);
		INSERT INTO video_history (video_id, created_at, event, message, task_id) VALUES (2, '2026-01-01T00:00:00Z', 'download_failed', 'keep', 3);
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	var st string
	if err := d.SQL.QueryRow(`SELECT status FROM videos WHERE id = 1`).Scan(&st); err != nil {
		t.Fatal(err)
	}
	if st != "wanted" {
		t.Fatalf("video 1 status=%q want wanted", st)
	}
	var histN int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM video_history WHERE event IN ('source_failed', 'wanted_source_error')`).Scan(&histN); err != nil {
		t.Fatal(err)
	}
	if histN != 0 {
		t.Fatalf("hold history rows=%d want 0", histN)
	}
	var ev string
	if err := d.SQL.QueryRow(`SELECT event FROM video_history WHERE message = 'legacy'`).Scan(&ev); err != nil {
		t.Fatal(err)
	}
	if ev != "download_failed" {
		t.Fatalf("legacy event=%q want download_failed", ev)
	}
}

func TestMigrateV4AddsAcquiredVia(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acquired.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (3);
		CREATE TABLE videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL,
			source_id INTEGER,
			remote_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'wanted',
			import_src TEXT,
			acquired_at TEXT
		);
		INSERT INTO videos (id, series_id, remote_id, title, status, import_src, acquired_at)
			VALUES (1, 1, 'a', 'A', 'downloaded', '/import/a.mkv', '2026-01-01T00:00:00Z');
		INSERT INTO videos (id, series_id, remote_id, title, status, import_src, acquired_at)
			VALUES (2, 1, 'b', 'B', 'wanted', NULL, NULL);
		INSERT INTO videos (id, series_id, remote_id, title, status, import_src, acquired_at)
			VALUES (3, 1, 'c', 'C', 'downloaded', '', '2026-01-02T00:00:00Z');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "videos", "acquired_via", true)
	assertColumnNotNull(t, d.SQL, "videos", "acquired_via", false)

	var via1, via3 string
	var via2 sql.NullString
	if err := d.SQL.QueryRow(`SELECT acquired_via FROM videos WHERE id = 1`).Scan(&via1); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL.QueryRow(`SELECT acquired_via FROM videos WHERE id = 2`).Scan(&via2); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL.QueryRow(`SELECT acquired_via FROM videos WHERE id = 3`).Scan(&via3); err != nil {
		t.Fatal(err)
	}
	if via1 != "import" {
		t.Fatalf("video 1 acquired_via=%q want import", via1)
	}
	if via2.Valid {
		t.Fatalf("video 2 acquired_via=%q want NULL (never acquired; v8 clears v4 source default)", via2.String)
	}
	if via3 != "source" {
		t.Fatalf("video 3 acquired_via=%q want source", via3)
	}
}

func TestMigrateV5AddsRootEpisodeFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "epfmt.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (4);
		CREATE TABLE root_folders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL UNIQUE,
			retention_ttl_seconds INTEGER
		);
		CREATE TABLE settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO root_folders (name, path) VALUES ('A', '/a'), ('B', '/b');
		INSERT INTO settings (key, value) VALUES ('episode_format', '{date} [{id}]');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "root_folders", "episode_format", true)

	var fmtA, fmtB string
	if err := d.SQL.QueryRow(`SELECT episode_format FROM root_folders WHERE path = '/a'`).Scan(&fmtA); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL.QueryRow(`SELECT episode_format FROM root_folders WHERE path = '/b'`).Scan(&fmtB); err != nil {
		t.Fatal(err)
	}
	if fmtA != "{date} [{id}]" || fmtB != "{date} [{id}]" {
		t.Fatalf("episode_format A=%q B=%q want {date} [{id}]", fmtA, fmtB)
	}
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = 'episode_format'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("settings episode_format rows=%d want 0", n)
	}
}

func TestMigrateV6DropsAutoIgnoreColumns(t *testing.T) {
	t.Run("series column only", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "autoignore-series.db")
		sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
		if err != nil {
			t.Fatal(err)
		}
		_, err = sqlDB.Exec(`
			CREATE TABLE schema_version (version INTEGER NOT NULL);
			INSERT INTO schema_version (version) VALUES (5);
			CREATE TABLE root_folders (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL DEFAULT '',
				path TEXT NOT NULL UNIQUE,
				retention_ttl_seconds INTEGER,
				episode_format TEXT NOT NULL DEFAULT 'S{year}/S{year}E{episode} [{id}]'
			);
			CREATE TABLE quality_profiles (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL UNIQUE,
				format_selector TEXT NOT NULL
			);
			CREATE TABLE series (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				title TEXT NOT NULL,
				root_id INTEGER NOT NULL REFERENCES root_folders(id),
				quality_profile_id INTEGER NOT NULL REFERENCES quality_profiles(id),
				monitored INTEGER NOT NULL DEFAULT 1,
				added_at TEXT NOT NULL,
				auto_ignore_media_types TEXT NOT NULL DEFAULT '[]'
			);
			CREATE TABLE sources (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
				url TEXT NOT NULL,
				kind TEXT NOT NULL DEFAULT 'feed',
				scan_cron TEXT NOT NULL DEFAULT '',
				index_as_ignored INTEGER NOT NULL DEFAULT 0,
				full_scan_limit INTEGER NOT NULL DEFAULT 0,
				full_scan_done INTEGER NOT NULL DEFAULT 0,
				UNIQUE(series_id, url)
			);
			INSERT INTO root_folders (path) VALUES ('/tmp/root');
			INSERT INTO quality_profiles (name, format_selector) VALUES ('Default', 'bv*+ba/b');
			INSERT INTO series (id, title, root_id, quality_profile_id, added_at, auto_ignore_media_types)
				VALUES (1, 'S', 1, 1, '2026-01-01T00:00:00Z', '["short","clip"]');
			INSERT INTO sources (id, series_id, url, kind, index_as_ignored) VALUES
				(1, 1, 'https://www.example.com/@wanted', 'feed', 0);
		`)
		if err != nil {
			_ = sqlDB.Close()
			t.Fatal(err)
		}
		_ = sqlDB.Close()

		d, err := db.Open(path)
		if err != nil {
			t.Fatalf("open migrate: %v", err)
		}
		defer func() { _ = d.Close() }()

		var ver int
		if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
			t.Fatal(err)
		}
		if ver != 26 {
			t.Fatalf("schema_version=%d want 26", ver)
		}
		assertColumn(t, d.SQL, "sources", "auto_ignore_media_types", false)
		assertColumn(t, d.SQL, "series", "auto_ignore_media_types", false)
	})

	t.Run("both columns", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "autoignore-both.db")
		sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
		if err != nil {
			t.Fatal(err)
		}
		_, err = sqlDB.Exec(`
			CREATE TABLE schema_version (version INTEGER NOT NULL);
			INSERT INTO schema_version (version) VALUES (5);
			CREATE TABLE root_folders (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL DEFAULT '',
				path TEXT NOT NULL UNIQUE,
				retention_ttl_seconds INTEGER,
				episode_format TEXT NOT NULL DEFAULT 'S{year}/S{year}E{episode} [{id}]'
			);
			CREATE TABLE quality_profiles (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL UNIQUE,
				format_selector TEXT NOT NULL
			);
			CREATE TABLE series (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				title TEXT NOT NULL,
				root_id INTEGER NOT NULL REFERENCES root_folders(id),
				quality_profile_id INTEGER NOT NULL REFERENCES quality_profiles(id),
				monitored INTEGER NOT NULL DEFAULT 1,
				added_at TEXT NOT NULL,
				auto_ignore_media_types TEXT NOT NULL DEFAULT '[]'
			);
			CREATE TABLE sources (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
				url TEXT NOT NULL,
				kind TEXT NOT NULL DEFAULT 'feed',
				scan_cron TEXT NOT NULL DEFAULT '',
				index_as_ignored INTEGER NOT NULL DEFAULT 0,
				full_scan_limit INTEGER NOT NULL DEFAULT 0,
				full_scan_done INTEGER NOT NULL DEFAULT 0,
				auto_ignore_media_types TEXT NOT NULL DEFAULT '[]',
				UNIQUE(series_id, url)
			);
			INSERT INTO root_folders (path) VALUES ('/tmp/root');
			INSERT INTO quality_profiles (name, format_selector) VALUES ('Default', 'bv*+ba/b');
			INSERT INTO series (id, title, root_id, quality_profile_id, added_at, auto_ignore_media_types)
				VALUES (1, 'S', 1, 1, '2026-01-01T00:00:00Z', '["short"]');
			INSERT INTO sources (id, series_id, url, kind, index_as_ignored, auto_ignore_media_types) VALUES
				(1, 1, 'https://www.example.com/@wanted', 'feed', 0, '["clip"]');
		`)
		if err != nil {
			_ = sqlDB.Close()
			t.Fatal(err)
		}
		_ = sqlDB.Close()

		d, err := db.Open(path)
		if err != nil {
			t.Fatalf("open migrate: %v", err)
		}
		defer func() { _ = d.Close() }()

		var ver int
		if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
			t.Fatal(err)
		}
		if ver != 26 {
			t.Fatalf("schema_version=%d want 26", ver)
		}
		assertColumn(t, d.SQL, "sources", "auto_ignore_media_types", false)
		assertColumn(t, d.SQL, "series", "auto_ignore_media_types", false)
	})
}


func TestMigrateV7DropsVideosTool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (6);
		CREATE TABLE videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL,
			source_id INTEGER,
			remote_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'wanted',
			tool TEXT,
			acquired_via TEXT NOT NULL DEFAULT 'source'
		);
		INSERT INTO videos (id, series_id, remote_id, title, status, tool, acquired_via)
			VALUES (1, 1, 'a', 'A', 'downloaded', 'yt-dlp', 'source');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "videos", "tool", false)
	assertColumn(t, d.SQL, "videos", "acquired_via", true)
}

func TestMigrateV8NullsUnacquiredVia(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acquired-via.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (7);
		CREATE TABLE videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL,
			source_id INTEGER,
			remote_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'wanted',
			acquired_via TEXT NOT NULL DEFAULT 'source',
			acquired_at TEXT
		);
		INSERT INTO videos (id, series_id, remote_id, title, status, acquired_via, acquired_at)
			VALUES (1, 1, 'a', 'Wanted', 'wanted', 'source', NULL);
		INSERT INTO videos (id, series_id, remote_id, title, status, acquired_via, acquired_at)
			VALUES (2, 1, 'b', 'Got', 'downloaded', 'source', '2026-01-01T00:00:00Z');
		INSERT INTO videos (id, series_id, remote_id, title, status, acquired_via, acquired_at)
			VALUES (3, 1, 'c', 'Import', 'downloaded', 'import', '2026-01-02T00:00:00Z');
		INSERT INTO videos (id, series_id, remote_id, title, status, acquired_via, acquired_at)
			VALUES (4, 1, 'd', 'AcquiredNoVia', 'downloaded', '', '2026-01-03T00:00:00Z');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "videos", "acquired_via", true)
	assertColumnNotNull(t, d.SQL, "videos", "acquired_via", false)

	var via1 sql.NullString
	if err := d.SQL.QueryRow(`SELECT acquired_via FROM videos WHERE id = 1`).Scan(&via1); err != nil {
		t.Fatal(err)
	}
	if via1.Valid {
		t.Fatalf("video 1 acquired_via=%q want NULL", via1.String)
	}
	var via2, via3, via4 string
	if err := d.SQL.QueryRow(`SELECT acquired_via FROM videos WHERE id = 2`).Scan(&via2); err != nil {
		t.Fatal(err)
	}
	if via2 != "source" {
		t.Fatalf("video 2 acquired_via=%q want source", via2)
	}
	if err := d.SQL.QueryRow(`SELECT acquired_via FROM videos WHERE id = 3`).Scan(&via3); err != nil {
		t.Fatal(err)
	}
	if via3 != "import" {
		t.Fatalf("video 3 acquired_via=%q want import", via3)
	}
	if err := d.SQL.QueryRow(`SELECT acquired_via FROM videos WHERE id = 4`).Scan(&via4); err != nil {
		t.Fatal(err)
	}
	if via4 != "source" {
		t.Fatalf("video 4 acquired_via=%q want source (defensive backfill)", via4)
	}
}

func TestMigrateV9BumpsExactDefaultEpisodeFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "epfmt9.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (8);
		CREATE TABLE root_folders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL UNIQUE,
			retention_ttl_seconds INTEGER,
			episode_format TEXT NOT NULL DEFAULT 'S{year}/S{year}E{episode} [{id}]'
		);
		INSERT INTO root_folders (name, path, episode_format) VALUES
			('legacy', '/legacy', 'S{year}/S{year}E{episode} [{id}]'),
			('custom', '/custom', '{date} [{id}]');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	var legacy, custom string
	if err := d.SQL.QueryRow(`SELECT episode_format FROM root_folders WHERE path = '/legacy'`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if legacy != `S{year}/S{year}E{episode:04} [{id}]` {
		t.Fatalf("legacy format=%q", legacy)
	}
	if err := d.SQL.QueryRow(`SELECT episode_format FROM root_folders WHERE path = '/custom'`).Scan(&custom); err != nil {
		t.Fatal(err)
	}
	if custom != "{date} [{id}]" {
		t.Fatalf("custom must be untouched: %q", custom)
	}
}

func assertColumn(t *testing.T, sqlDB *sql.DB, table, column string, want bool) {
	t.Helper()
	rows, err := sqlDB.Query(`PRAGMA table_info("` + table + `")`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if found != want {
		t.Fatalf("column %s.%s present=%v want %v", table, column, found, want)
	}
}

func assertColumnNotNull(t *testing.T, sqlDB *sql.DB, table, column string, wantNotNull bool) {
	t.Helper()
	rows, err := sqlDB.Query(`PRAGMA table_info("` + table + `")`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			got := notnull != 0
			if got != wantNotNull {
				t.Fatalf("column %s.%s notnull=%v want %v", table, column, got, wantNotNull)
			}
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("column %s.%s missing", table, column)
}

func TestWorkerHeartbeat(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "creatorr.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()

	at, err := d.WorkerHeartbeat()
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if !at.IsZero() {
		t.Fatalf("expected zero heartbeat, got %v", at)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := d.TouchWorkerHeartbeat(now); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, err := d.WorkerHeartbeat()
	if err != nil {
		t.Fatalf("heartbeat after touch: %v", err)
	}
	if got.IsZero() {
		t.Fatal("expected non-zero heartbeat")
	}
}

func TestOpenBusyTimeoutOnPooledConns(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "creatorr.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()

	ctx := context.Background()
	c1, err := d.SQL.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c1.Close() }()
	c2, err := d.SQL.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()

	readTimeout := func(c *sql.Conn) int {
		t.Helper()
		var ms int
		if err := c.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&ms); err != nil {
			t.Fatal(err)
		}
		return ms
	}
	if got := readTimeout(c1); got < 10000 {
		t.Fatalf("conn1 busy_timeout=%d want >=10000", got)
	}
	if got := readTimeout(c2); got < 10000 {
		t.Fatalf("conn2 busy_timeout=%d want >=10000", got)
	}
	var mode string
	if err := c2.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode=%q want wal", mode)
	}
}

func TestMigrateV11OriginAndStripTrigger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "origin.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (10);
		CREATE TABLE tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			series_id INTEGER,
			video_id INTEGER,
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
		INSERT INTO tasks (id, kind, status, payload, detail, domain, created_at)
			VALUES (1, 'ytdlp_update', 'done', '{"trigger":"cron"}', '{"trigger":"boot","ok":true}', 'system', '2026-01-01T00:00:00Z');
		INSERT INTO tasks (id, kind, status, payload, detail, domain, created_at)
			VALUES (2, 'scan', 'done', '{}', NULL, 'example.com', '2026-01-02T00:00:00Z');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "tasks", "origin", true)
	assertColumn(t, d.SQL, "tasks", "parent_task_id", true)

	var o1, o2, p1, d1 string
	if err := d.SQL.QueryRow(`SELECT origin, payload, COALESCE(detail,'') FROM tasks WHERE id = 1`).Scan(&o1, &p1, &d1); err != nil {
		t.Fatal(err)
	}
	if o1 != "manual" {
		t.Fatalf("task1 origin=%q want manual", o1)
	}
	if strings.Contains(p1, "trigger") {
		t.Fatalf("payload still has trigger: %s", p1)
	}
	if strings.Contains(d1, `"trigger"`) {
		t.Fatalf("detail still has trigger: %s", d1)
	}
	if !strings.Contains(d1, `"ok"`) {
		t.Fatalf("detail lost unrelated keys: %s", d1)
	}
	if err := d.SQL.QueryRow(`SELECT origin FROM tasks WHERE id = 2`).Scan(&o2); err != nil {
		t.Fatal(err)
	}
	if o2 != "manual" {
		t.Fatalf("task2 origin=%q want manual", o2)
	}
}

func TestMigrateV11SkipsMalformedDetailJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "origin-badjson.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (10);
		CREATE TABLE tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			series_id INTEGER,
			video_id INTEGER,
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
		INSERT INTO tasks (id, kind, status, payload, detail, domain, created_at)
			VALUES (1, 'scan', 'done', '{"trigger":"cron"}', 'not-json {', 'example.com', '2026-01-01T00:00:00Z');
		INSERT INTO tasks (id, kind, status, payload, detail, domain, created_at)
			VALUES (2, 'ytdlp_update', 'done', '{}', '{"trigger":"boot","ok":true}', 'system', '2026-01-02T00:00:00Z');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	var detail1, detail2, payload1 string
	if err := d.SQL.QueryRow(`SELECT detail, payload FROM tasks WHERE id = 1`).Scan(&detail1, &payload1); err != nil {
		t.Fatal(err)
	}
	if detail1 != "not-json {" {
		t.Fatalf("malformed detail changed: %q", detail1)
	}
	if strings.Contains(payload1, "trigger") {
		t.Fatalf("payload trigger not stripped: %s", payload1)
	}
	if err := d.SQL.QueryRow(`SELECT detail FROM tasks WHERE id = 2`).Scan(&detail2); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detail2, `"trigger"`) {
		t.Fatalf("valid detail trigger not stripped: %s", detail2)
	}
}

func TestMigrateV12VideoHistoryEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hist-v12.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (11);
		CREATE TABLE video_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			video_id INTEGER NOT NULL,
			event TEXT NOT NULL,
			message TEXT,
			detail TEXT,
			created_at TEXT NOT NULL
		);
		INSERT INTO video_history (video_id, event, message, detail, created_at) VALUES
			(1, 'verified', 'ok', '{"kind":"media_verify"}', '2026-01-01T00:00:00Z'),
			(2, 'verify_failed', 'bad', '{"kind":"verify_all_media"}', '2026-01-02T00:00:00Z'),
			(3, 'integrity_checked', 'already', '{}', '2026-01-03T00:00:00Z');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}

	var e1, d1, e2, d2, e3 string
	if err := d.SQL.QueryRow(`SELECT event, detail FROM video_history WHERE video_id = 1`).Scan(&e1, &d1); err != nil {
		t.Fatal(err)
	}
	if e1 != "integrity_checked" {
		t.Fatalf("video 1 event=%q want integrity_checked", e1)
	}
	if !strings.Contains(d1, `"integrity_check_initial"`) {
		t.Fatalf("video 1 detail kind not rewritten: %s", d1)
	}
	if err := d.SQL.QueryRow(`SELECT event, detail FROM video_history WHERE video_id = 2`).Scan(&e2, &d2); err != nil {
		t.Fatal(err)
	}
	if e2 != "integrity_check_failed" {
		t.Fatalf("video 2 event=%q want integrity_check_failed", e2)
	}
	if !strings.Contains(d2, `"integrity_check"`) {
		t.Fatalf("video 2 detail kind not rewritten: %s", d2)
	}
	if err := d.SQL.QueryRow(`SELECT event FROM video_history WHERE video_id = 3`).Scan(&e3); err != nil {
		t.Fatal(err)
	}
	if e3 != "integrity_checked" {
		t.Fatalf("video 3 event=%q want integrity_checked", e3)
	}
}

func TestMigrateV14AddsCookiesAfterFail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m14.db")
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (13);
		CREATE TABLE domains (
		  domain TEXT PRIMARY KEY,
		  active INTEGER NOT NULL DEFAULT 1,
		  task_cooldown_seconds INTEGER,
		  max_download_queue INTEGER,
		  max_parallel_tasks INTEGER,
		  download_rate_limit TEXT,
		  sleep_requests REAL,
		  use_flaresolverr INTEGER,
		  cookies TEXT,
		  username TEXT,
		  password TEXT,
		  updated_at TEXT NOT NULL
		);
		INSERT INTO domains (domain, active, updated_at) VALUES ('example.com', 1, 't');
	`); err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	var v int
	if err := d.SQL.QueryRow(`SELECT smart_cookies FROM domains WHERE domain = 'example.com'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Fatalf("smart_cookies=%d want 0", v)
	}
}

func TestMigrateV15QueueSeqDropsPriority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (14);
		CREATE TABLE tasks (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT NOT NULL,
		  status TEXT NOT NULL DEFAULT 'pending',
		  series_id INTEGER,
		  video_id INTEGER,
		  payload TEXT NOT NULL DEFAULT '{}',
		  error_code TEXT,
		  error_message TEXT,
		  message TEXT,
		  detail TEXT,
		  commands TEXT NOT NULL DEFAULT '[]',
		  logs TEXT NOT NULL DEFAULT '[]',
		  progress REAL,
		  domain TEXT NOT NULL DEFAULT 'unknown',
		  priority INTEGER NOT NULL DEFAULT 0,
		  origin TEXT NOT NULL DEFAULT 'manual',
		  parent_task_id INTEGER,
		  created_at TEXT NOT NULL,
		  started_at TEXT,
		  finished_at TEXT
		);
		INSERT INTO tasks (kind, status, domain, priority, origin, created_at)
		VALUES ('download', 'pending', 'example.com', 100, 'manual', 't1'),
		       ('download', 'pending', 'example.com', 0, 'manual', 't2');
	`); err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "tasks", "queue_seq", true)
	assertColumn(t, d.SQL, "tasks", "priority", false)

	var seqs []int64
	rows, err := d.SQL.Query(`SELECT queue_seq FROM tasks WHERE status = 'pending' ORDER BY id ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var s int64
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, s)
	}
	if len(seqs) != 2 || seqs[0] != 1 || seqs[1] != 2 {
		// queue_seq = id (priority ignored)
		t.Fatalf("queue_seq=%v want [1 2]", seqs)
	}
}

func TestMigrateV16PackRoleAndSpecialFormats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v16.db")
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	// Minimal pre-v16 schema with root_folders + videos + schema_version 15.
	if _, err := sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (15);
		CREATE TABLE root_folders (
		  id INTEGER PRIMARY KEY,
		  name TEXT NOT NULL DEFAULT '',
		  path TEXT NOT NULL,
		  episode_format TEXT NOT NULL DEFAULT 'S{year}/S{year}E{episode:04} [{id}]',
		  retention_ttl_seconds INTEGER
		);
		INSERT INTO root_folders (id, name, path) VALUES (1, 'lib', '/library');
		CREATE TABLE series (
		  id INTEGER PRIMARY KEY,
		  title TEXT NOT NULL,
		  root_id INTEGER NOT NULL,
		  quality_profile_id INTEGER NOT NULL DEFAULT 1,
		  monitored INTEGER NOT NULL DEFAULT 1,
		  delivery_mode TEXT NOT NULL DEFAULT 'video'
		);
		INSERT INTO series (id, title, root_id) VALUES (1, 'Show', 1);
		CREATE TABLE videos (
		  id INTEGER PRIMARY KEY,
		  series_id INTEGER NOT NULL,
		  remote_id TEXT NOT NULL,
		  title TEXT NOT NULL,
		  status TEXT NOT NULL DEFAULT 'wanted'
		);
		INSERT INTO videos (id, series_id, remote_id, title) VALUES (1, 1, 'a', 'A');
	`); err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "videos", "special_feature", true)
	assertColumn(t, d.SQL, "root_folders", "special_episode_format", true)
	assertColumn(t, d.SQL, "root_folders", "special_feature_format", true)

	var packRole sql.NullString
	if err := d.SQL.QueryRow(`SELECT special_feature FROM videos WHERE id = 1`).Scan(&packRole); err != nil {
		t.Fatal(err)
	}
	if packRole.Valid {
		t.Fatalf("special_feature=%v want NULL (regular after v24)", packRole)
	}
	var seFmt, sfFmt string
	if err := d.SQL.QueryRow(`SELECT special_episode_format, special_feature_format FROM root_folders WHERE id = 1`).Scan(&seFmt, &sfFmt); err != nil {
		t.Fatal(err)
	}
	if seFmt != "[{id}]" {
		t.Fatalf("special_episode_format=%q", seFmt)
	}
	if sfFmt != "{episode:02} {title:100}" {
		t.Fatalf("special_feature_format=%q", sfFmt)
	}
}

func TestMigrateV17PackRoleEpisode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v17.db")
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (16);
		CREATE TABLE videos (
		  id INTEGER PRIMARY KEY,
		  series_id INTEGER NOT NULL DEFAULT 1,
		  remote_id TEXT NOT NULL,
		  title TEXT NOT NULL,
		  status TEXT NOT NULL DEFAULT 'wanted',
		  pack_role TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO videos (id, remote_id, title, pack_role) VALUES
		  (1, 'a', 'A', ''),
		  (2, 'b', 'B', 'special_episode'),
		  (3, 'c', 'C', 'trailers');
	`); err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "videos", "special_feature", true)
	assertColumn(t, d.SQL, "videos", "pack_role", false)
	var roles []string
	rows, err := d.SQL.Query(`SELECT special_feature FROM videos ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r sql.NullString
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		if r.Valid {
			roles = append(roles, r.String)
		} else {
			roles = append(roles, "")
		}
	}
	want := []string{"", "special_episode", "trailers"}
	if len(roles) != 3 || roles[0] != want[0] || roles[1] != want[1] || roles[2] != want[2] {
		t.Fatalf("special_feature=%v want %v", roles, want)
	}
}

func TestMigrateV19DropsImportSrc(t *testing.T) {
	path := filepath.Join(t.TempDir(), "import-src.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (18);
		CREATE TABLE videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL,
			remote_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'wanted',
			import_src TEXT,
			acquired_via TEXT,
			special_feature TEXT NOT NULL DEFAULT 'episode'
		);
		INSERT INTO videos (id, series_id, remote_id, title, status, import_src, acquired_via)
			VALUES (1, 1, 'a', 'A', 'downloaded', '/inbox/a.mp4', 'import');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "videos", "import_src", false)
	assertColumn(t, d.SQL, "videos", "acquired_via", true)
}

func TestMigrateV20SmartCookies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smart-cookies.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (19);
		CREATE TABLE domains (
			domain TEXT PRIMARY KEY,
			active INTEGER NOT NULL DEFAULT 1,
			cookies TEXT,
			cookies_after_fail INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		);
		INSERT INTO domains (domain, cookies_after_fail, updated_at) VALUES ('example.com', 1, '2020-01-01T00:00:00Z');
		CREATE TABLE series (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, root_id INTEGER NOT NULL, quality_profile_id INTEGER NOT NULL, monitored INTEGER NOT NULL DEFAULT 1, delivery_mode TEXT NOT NULL DEFAULT 'video', added_at TEXT NOT NULL DEFAULT '');
		CREATE TABLE sources (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'feed',
			scan_cron TEXT NOT NULL DEFAULT '',
			index_as_ignored INTEGER NOT NULL DEFAULT 0,
			full_scan_limit INTEGER NOT NULL DEFAULT 0,
			full_scan_done INTEGER NOT NULL DEFAULT 0,
			UNIQUE(series_id, url)
		);
		INSERT INTO series (id, title, root_id, quality_profile_id) VALUES (1, 'S', 1, 1);
		INSERT INTO sources (id, series_id, url) VALUES (1, 1, 'https://example.com/c');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "domains", "smart_cookies", true)
	assertColumn(t, d.SQL, "domains", "cookies_after_fail", false)
	assertColumn(t, d.SQL, "sources", "cookie_smart_prefer", true)
	assertColumn(t, d.SQL, "sources", "cookie_smart_ring", true)
	assertColumn(t, d.SQL, "sources", "cookie_smart_n", true)
	var v int
	if err := d.SQL.QueryRow(`SELECT smart_cookies FROM domains WHERE domain = 'example.com'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("smart_cookies=%d want 1 (preserved from cookies_after_fail)", v)
	}
}

func TestMigrateV21Notes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (20);
		CREATE TABLE series (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			root_id INTEGER NOT NULL,
			quality_profile_id INTEGER NOT NULL,
			monitored INTEGER NOT NULL DEFAULT 1,
			delivery_mode TEXT NOT NULL DEFAULT 'video',
			added_at TEXT NOT NULL DEFAULT '',
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
			premiered TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
			remote_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'wanted',
			UNIQUE(series_id, remote_id)
		);
		INSERT INTO series (id, title, root_id, quality_profile_id) VALUES (1, 'S', 1, 1);
		INSERT INTO videos (id, series_id, remote_id, title) VALUES (1, 1, 'r1', 'V');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "series", "notes", true)
	assertColumn(t, d.SQL, "videos", "notes", true)
	var sn, vn string
	if err := d.SQL.QueryRow(`SELECT notes FROM series WHERE id = 1`).Scan(&sn); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL.QueryRow(`SELECT notes FROM videos WHERE id = 1`).Scan(&vn); err != nil {
		t.Fatal(err)
	}
	if sn != "" || vn != "" {
		t.Fatalf("default notes series=%q video=%q", sn, vn)
	}
}

func TestMigrateV22PurgesDeleteSidecar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (21);
		CREATE TABLE series (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			root_id INTEGER NOT NULL,
			quality_profile_id INTEGER NOT NULL,
			monitored INTEGER NOT NULL DEFAULT 1,
			delivery_mode TEXT NOT NULL DEFAULT 'video',
			added_at TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
			remote_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'wanted',
			notes TEXT NOT NULL DEFAULT '',
			UNIQUE(series_id, remote_id)
		);
		CREATE TABLE tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'done',
			series_id INTEGER,
			video_id INTEGER,
			payload TEXT NOT NULL DEFAULT '{}',
			domain TEXT NOT NULL DEFAULT 'system',
			queue_seq INTEGER NOT NULL DEFAULT 0,
			origin TEXT NOT NULL DEFAULT 'manual',
			parent_task_id INTEGER,
			created_at TEXT NOT NULL,
			finished_at TEXT
		);
		CREATE TABLE video_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			video_id INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			event TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '',
			task_id INTEGER NOT NULL
		);
		CREATE TABLE notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at TEXT NOT NULL,
			event TEXT NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL DEFAULT '',
			task_id INTEGER,
			external_ok INTEGER NOT NULL DEFAULT 0,
			read_at TEXT
		);
		INSERT INTO series (id, title, root_id, quality_profile_id) VALUES (1, 'S', 1, 1);
		INSERT INTO videos (id, series_id, remote_id, title) VALUES (1, 1, 'r1', 'V');
		INSERT INTO tasks (id, kind, status, video_id, domain, origin, created_at, finished_at)
		VALUES (10, 'delete_sidecar', 'done', 1, 'system', 'manual', '2026-01-01T00:00:00Z', '2026-01-01T00:00:01Z');
		INSERT INTO tasks (id, kind, status, video_id, domain, origin, created_at, finished_at)
		VALUES (11, 'download', 'done', 1, 'example.com', 'manual', '2026-01-01T00:00:00Z', '2026-01-01T00:00:01Z');
		INSERT INTO video_history (video_id, created_at, event, message, task_id)
		VALUES (1, '2026-01-01T00:00:00Z', 'sidecar_deleted', 'Deleted sidecar', 10);
		INSERT INTO video_history (video_id, created_at, event, message, task_id)
		VALUES (1, '2026-01-01T00:00:00Z', 'downloaded', 'ok', 11);
		INSERT INTO notifications (created_at, event, title, body, task_id)
		VALUES ('2026-01-01T00:00:00Z', 'ytdlp_failed', 'x', 'y', 10);
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'delete_sidecar'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("delete_sidecar tasks remain=%d", n)
	}
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM video_history WHERE event = 'sidecar_deleted'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("sidecar_deleted history remain=%d", n)
	}
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM notifications WHERE task_id = 10`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("notifications remain=%d", n)
	}
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = 11`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("download task should remain")
	}
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM video_history WHERE event = 'downloaded'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("downloaded history should remain")
	}
}

func TestMigrateV24SpecialFeatureNullableAndSourcePreset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v22.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (23);
		CREATE TABLE series (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			root_id INTEGER NOT NULL,
			quality_profile_id INTEGER NOT NULL,
			monitored INTEGER NOT NULL DEFAULT 1,
			delivery_mode TEXT NOT NULL DEFAULT 'video',
			added_at TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO series (id, title, root_id, quality_profile_id) VALUES (1, 'Show', 1, 1);
		CREATE TABLE sources (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'feed',
			scan_cron TEXT NOT NULL DEFAULT '',
			index_as_ignored INTEGER NOT NULL DEFAULT 0,
			full_scan_limit INTEGER NOT NULL DEFAULT 0,
			full_scan_done INTEGER NOT NULL DEFAULT 0,
			UNIQUE(series_id, url)
		);
		INSERT INTO sources (id, series_id, url) VALUES (1, 1, 'https://www.example.com/@chan');
		CREATE TABLE videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
			remote_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'wanted',
			special_feature TEXT NOT NULL DEFAULT 'episode',
			notes TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO videos (id, series_id, remote_id, title, special_feature) VALUES
			(1, 1, 'a', 'Regular', 'episode'),
			(2, 1, 'b', 'Feature', 'extras'),
			(3, 1, 'c', 'Empty', '');
		CREATE TABLE files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			video_id INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
			path TEXT NOT NULL,
			kind TEXT NOT NULL,
			acquired_at TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO files (id, video_id, path, kind) VALUES (10, 1, '/tmp/a.mkv', 'video');
		CREATE TABLE tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			domain TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'queued',
			video_id INTEGER REFERENCES videos(id) ON DELETE SET NULL,
			parent_task_id INTEGER
		);
		INSERT INTO tasks (id, kind, video_id) VALUES (20, 'download', 1);
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 26 {
		t.Fatalf("schema_version=%d want 26", ver)
	}
	assertColumn(t, d.SQL, "sources", "studio", true)
	assertColumn(t, d.SQL, "sources", "tags", true)
	assertColumn(t, d.SQL, "sources", "special_feature", true)
	assertColumnNotNull(t, d.SQL, "videos", "special_feature", false)

	var sf1, sf2, sf3 sql.NullString
	if err := d.SQL.QueryRow(`SELECT special_feature FROM videos WHERE id = 1`).Scan(&sf1); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL.QueryRow(`SELECT special_feature FROM videos WHERE id = 2`).Scan(&sf2); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL.QueryRow(`SELECT special_feature FROM videos WHERE id = 3`).Scan(&sf3); err != nil {
		t.Fatal(err)
	}
	if sf1.Valid {
		t.Fatalf("regular special_feature=%v want NULL", sf1)
	}
	if !sf2.Valid || sf2.String != "extras" {
		t.Fatalf("feature special_feature=%v", sf2)
	}
	if sf3.Valid {
		t.Fatalf("empty special_feature=%v want NULL", sf3)
	}

	var fileVID, taskVID sql.NullInt64
	if err := d.SQL.QueryRow(`SELECT video_id FROM files WHERE id = 10`).Scan(&fileVID); err != nil {
		t.Fatal(err)
	}
	if !fileVID.Valid || fileVID.Int64 != 1 {
		t.Fatalf("files.video_id=%v want 1 (same id after column swap)", fileVID)
	}
	if err := d.SQL.QueryRow(`SELECT video_id FROM tasks WHERE id = 20`).Scan(&taskVID); err != nil {
		t.Fatal(err)
	}
	if !taskVID.Valid || taskVID.Int64 != 1 {
		t.Fatalf("tasks.video_id=%v want 1", taskVID)
	}

	var fk int
	if err := d.SQL.QueryRow(`PRAGMA foreign_key_check`).Scan(&fk); err != sql.ErrNoRows && err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}

	var tags string
	if err := d.SQL.QueryRow(`SELECT tags FROM sources WHERE id = 1`).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tags, "example.com") {
		t.Fatalf("source tags=%q want domain seed", tags)
	}
}
