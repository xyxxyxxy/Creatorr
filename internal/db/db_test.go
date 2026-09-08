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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
	}
	assertColumn(t, d.SQL, "sources", "full_scan_limit", true)
	assertColumn(t, d.SQL, "sources", "scan_cutoff", false)
	assertColumn(t, d.SQL, "sources", "auto_ignore_media_types", false)
	assertColumn(t, d.SQL, "series", "auto_ignore_media_types", false)
	assertColumn(t, d.SQL, "videos", "acquired_via", true)
	assertColumnNotNull(t, d.SQL, "videos", "acquired_via", false)
	assertColumn(t, d.SQL, "videos", "tool", false)
	assertColumn(t, d.SQL, "root_folders", "episode_format", true)
	assertColumn(t, d.SQL, "files", "content_hash", true)
	assertColumn(t, d.SQL, "tasks", "logs", true)
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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
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
		if ver != 13 {
			t.Fatalf("schema_version=%d want 13", ver)
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
		if ver != 13 {
			t.Fatalf("schema_version=%d want 13", ver)
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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
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
	if ver != 13 {
		t.Fatalf("schema_version=%d want 13", ver)
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
