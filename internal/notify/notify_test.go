package notify_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	apprise "github.com/unraid/apprise-go"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestNormalizeEvents(t *testing.T) {
	got, err := notify.NormalizeEvents([]string{notify.EventRateLimited, notify.EventCookieInvalid, notify.EventCookieInvalid, ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != notify.EventCookieInvalid || got[1] != notify.EventRateLimited {
		t.Fatalf("got %#v", got)
	}
	if _, err := notify.NormalizeEvents([]string{"nope"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAliasLegacyEvents(t *testing.T) {
	got, err := notify.NormalizeEvents([]string{"download_failed", "downloads_done"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != notify.EventDownloadDigest || got[1] != notify.EventYtDlpFailed {
		t.Fatalf("got %#v", got)
	}
}

func TestEventsSortedByLevel(t *testing.T) {
	got := notify.EventsSortedByLevel()
	if len(got) != len(notify.AllEvents) {
		t.Fatalf("len=%d want %d", len(got), len(notify.AllEvents))
	}
	seen := map[string]bool{}
	prevRank := -1
	for _, id := range got {
		if seen[id] {
			t.Fatalf("duplicate %q", id)
		}
		seen[id] = true
		rank := map[string]int{
			notify.LevelAlert: 0, notify.LevelWarning: 1, notify.LevelInfo: 2,
		}[notify.EventLevel(id)]
		if rank < prevRank {
			t.Fatalf("out of level order at %q", id)
		}
		prevRank = rank
	}
	for _, id := range notify.AllEvents {
		if !seen[id] {
			t.Fatalf("missing %q", id)
		}
	}
	// First block alerts; last is info (live_skipped / download_digest).
	if notify.EventLevel(got[0]) != notify.LevelAlert {
		t.Fatalf("first=%q want alert", got[0])
	}
	if notify.EventLevel(got[len(got)-1]) != notify.LevelInfo {
		t.Fatalf("last=%q want info", got[len(got)-1])
	}
}

func seedTask(t *testing.T, d *db.DB) int64 {
	t.Helper()
	id, err := queue.NewStore(d).Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestChannelCRUDAndSendEvent(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	taskID := seedTask(t, d)

	var calls atomic.Int32
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		n := calls.Add(1)
		if len(urls) != 1 || urls[0] == "" {
			t.Fatalf("urls=%v", urls)
		}
		if title == "" || body == "" {
			t.Fatal("empty title/body")
		}
		if n == 1 && nt != apprise.NotifyWarning {
			t.Fatalf("notify type %v want warning", nt)
		}
		if n == 2 && nt != apprise.NotifySuccess {
			t.Fatalf("notify type %v want success", nt)
		}
		return nil
	})
	defer notify.SetSendFnForTest(old)

	id, err := notify.Upsert(d, 0, "alerts", "discord://111111111111111111/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN012345", []string{notify.EventCookieInvalid})
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatal("id")
	}
	_, err = notify.Upsert(d, 0, "other", "discord://222222222222222222/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN012345", []string{notify.EventDownloadDigest})
	if err != nil {
		t.Fatal(err)
	}

	if err := notify.CookieInvalid(context.Background(), d, taskID, "example.com", "bad cookie"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d want 1 (only cookie channel)", calls.Load())
	}
	items, err := notify.ListNotifications(d, notify.ListFilter{}, 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if !items[0].ExternalOK || !items[0].Unread() {
		t.Fatalf("cookie with channel: want external_ok + still unread: %#v", items[0])
	}

	if err := notify.DownloadDigest(context.Background(), d, []notify.DigestItem{
		{Domain: "example.com", Series: "Show", Title: "Ep", Kind: "archive"},
	}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2", calls.Load())
	}

	list, err := notify.List(d)
	if err != nil || len(list) != 3 {
		t.Fatalf("list=%v err=%v (want in-app + 2 Apprise)", list, err)
	}
	if !notify.IsInAppChannel(list[0]) {
		t.Fatalf("first channel should be in-app: %#v", list[0])
	}
	if err := notify.Delete(d, id); err != nil {
		t.Fatal(err)
	}
	list, err = notify.List(d)
	if err != nil || len(list) != 2 {
		t.Fatalf("after delete list=%v err=%v", list, err)
	}
}

func TestInAppChannelReadOnly(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "inapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_, err = notify.Upsert(d, 0, "x", notify.InAppURL, notify.AllEvents)
	if err == nil {
		t.Fatal("expected upsert reject")
	}
	if err := notify.Delete(d, 0); err == nil {
		t.Fatal("expected delete reject")
	}
	list, err := notify.List(d)
	if err != nil || len(list) != 1 || !notify.IsInAppChannel(list[0]) {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if len(list[0].Events) != len(notify.AllEvents) {
		t.Fatalf("events=%v", list[0].Events)
	}
}

func TestFormatFileSyncIssuesBody(t *testing.T) {
	body := notify.FormatFileSyncIssuesBody(
		[]notify.FileSyncIssueItem{
			{Series: "S1", Title: "Gone"},
			{Series: "S1", Title: "Gone", Detail: "nfo: ep.nfo"},
		},
		[]notify.FileSyncIssueItem{
			{Series: "S2", Title: "Changed"},
			{Series: "S2", Title: "Changed", Detail: "thumb: ep-thumb.jpg"},
		},
	)
	if !strings.Contains(body, "Missing (2):") || !strings.Contains(body, "- S1 / Gone\n") {
		t.Fatalf("missing section: %q", body)
	}
	if !strings.Contains(body, "- S1 / Gone (nfo: ep.nfo)") {
		t.Fatalf("missing sidecar detail: %q", body)
	}
	if !strings.Contains(body, "Size changed (2):") || !strings.Contains(body, "- S2 / Changed\n") {
		t.Fatalf("changed section: %q", body)
	}
	if !strings.Contains(body, "- S2 / Changed (thumb: ep-thumb.jpg)") {
		t.Fatalf("changed sidecar detail: %q", body)
	}
	if !strings.Contains(body, "verify_failed") || !strings.Contains(body, "sidecar") {
		t.Fatalf("want verify_failed + sidecar hint: %q", body)
	}
}

func TestFileSyncIssuesEmptyNoop(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "fsi-empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		t.Fatal("should not send")
		return nil
	})
	defer notify.SetSendFnForTest(old)
	if err := notify.FileSyncIssues(context.Background(), d, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestFileSyncIssuesRecordsOnce(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "fsi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	taskID := seedTask(t, d)
	sends := 0
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		sends++
		return nil
	})
	defer notify.SetSendFnForTest(old)
	if err := notify.FileSyncIssues(context.Background(), d, taskID,
		[]notify.FileSyncIssueItem{{Series: "A", Title: "M"}},
		[]notify.FileSyncIssueItem{{Series: "B", Title: "C"}},
	); err != nil {
		t.Fatal(err)
	}
	items, err := notify.ListNotifications(d, notify.ListFilter{}, 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if items[0].Event != notify.EventFileSyncIssues {
		t.Fatalf("event=%s", items[0].Event)
	}
	if !items[0].Unread() {
		t.Fatal("want unread alert")
	}
	if sends != 0 {
		// no Apprise channels configured
		t.Fatalf("unexpected external sends=%d", sends)
	}
}

func TestSendEventNoChannelsStillRecords(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	taskID := seedTask(t, d)
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		t.Fatal("should not send")
		return nil
	})
	defer notify.SetSendFnForTest(old)
	if err := notify.SendEvent(context.Background(), d, notify.EventCookieInvalid, "t", "b", taskID); err != nil {
		t.Fatal(err)
	}
	items, err := notify.ListNotifications(d, notify.ListFilter{}, 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if !items[0].Unread() || items[0].ExternalOK {
		t.Fatalf("want unread no-external: %#v", items[0])
	}
	uc, err := notify.CountUnread(d)
	if err != nil || uc != 1 {
		t.Fatalf("unread=%d err=%v", uc, err)
	}
}

func TestSendEventErrorRequiresTaskID(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "req.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := notify.SendEvent(context.Background(), d, notify.EventYtDlpFailed, "t", "b", 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestYtDlpFailedExternalFailStaysUnread(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	taskID := seedTask(t, d)
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		return context.DeadlineExceeded
	})
	defer notify.SetSendFnForTest(old)
	if _, err := notify.Upsert(d, 0, "t", "discord://111111111111111111/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN012345", []string{notify.EventYtDlpFailed}); err != nil {
		t.Fatal(err)
	}
	_ = notify.YtDlpFailed(context.Background(), d, taskID, "example.com", "boom")
	items, err := notify.ListNotifications(d, notify.ListFilter{}, 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if !items[0].Unread() || items[0].ExternalOK {
		t.Fatalf("want unread: %#v", items[0])
	}
}

func TestYtDlpFailedKeepsDetailTail(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "tail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	taskID := seedTask(t, d)
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		return nil
	})
	defer notify.SetSendFnForTest(old)

	suffix := "ERROR: BitChute media API returned 403 Forbidden"
	detail := strings.Repeat("Downloading media API page\n", 200) + suffix
	if err := notify.YtDlpFailed(context.Background(), d, taskID, "example.com", detail); err != nil {
		t.Fatal(err)
	}
	items, err := notify.ListNotifications(d, notify.ListFilter{}, 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	body := items[0].Body
	if !strings.Contains(body, suffix) {
		t.Fatalf("want ERROR tail in body, got %q", body)
	}
	_, detailPart, ok := strings.Cut(body, "\n\n")
	if !ok {
		t.Fatalf("body missing detail split: %q", body)
	}
	if !strings.HasPrefix(detailPart, "…") {
		t.Fatalf("want ellipsis on truncated detail, got %q", detailPart)
	}
	if !strings.HasSuffix(detailPart, suffix) {
		t.Fatalf("want body detail to end with ERROR, got %q", detailPart)
	}
}

func TestDownloadDigestAlwaysRead(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "dig.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		return nil
	})
	defer notify.SetSendFnForTest(old)
	if err := notify.DownloadDigest(context.Background(), d, []notify.DigestItem{{Domain: "example.com", Title: "Ep"}}); err != nil {
		t.Fatal(err)
	}
	items, err := notify.ListNotifications(d, notify.ListFilter{}, 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if items[0].Unread() || items[0].TaskID.Valid {
		t.Fatalf("digest should be read, no task: %#v", items[0])
	}
}

func TestMarkRead(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "mr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	taskID := seedTask(t, d)
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		return nil
	})
	defer notify.SetSendFnForTest(old)
	if err := notify.SendEvent(context.Background(), d, notify.EventRateLimited, "t", "b", taskID); err != nil {
		t.Fatal(err)
	}
	if err := notify.MarkRead(d, 1); err != nil {
		t.Fatal(err)
	}
	if n, _ := notify.CountUnread(d); n != 0 {
		t.Fatalf("unread=%d", n)
	}
}

func TestListNotificationsRange(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "range.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	taskID := seedTask(t, d)
	_, err = notify.InsertNotification(d, notify.EventCookieInvalid, "early", "b", taskID, false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.SQL.Exec(`UPDATE notifications SET created_at = ? WHERE id = 1`, "2026-07-24T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	_, err = notify.InsertNotification(d, notify.EventRateLimited, "late", "b", taskID, false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.SQL.Exec(`UPDATE notifications SET created_at = ? WHERE id = 2`, "2026-07-25T15:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	n, err := notify.CountNotifications(d, notify.ListFilter{
		From: "2026-07-25T00:00:00Z",
		To:   "2026-07-25T23:59:59.999999999Z",
	})
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	items, err := notify.ListNotifications(d, notify.ListFilter{From: "2026-07-25T15:00:00Z"}, 10, 0)
	if err != nil || len(items) != 1 || items[0].Title != "late" {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestListNotificationsByLevel(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "nlevel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	taskID := seedTask(t, d)
	old := notify.SetSendFnForTest(func([]string, string, string, apprise.NotifyType) error { return nil })
	defer notify.SetSendFnForTest(old)
	if err := notify.DownloadDigest(context.Background(), d, []notify.DigestItem{{
		Domain: "example.com", Series: "S", Title: "V", Kind: "archive",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := notify.POTProvider(context.Background(), d, taskID, "example.com", "none"); err != nil {
		t.Fatal(err)
	}
	if err := notify.YtDlpFailed(context.Background(), d, taskID, "example.com", "boom"); err != nil {
		t.Fatal(err)
	}
	info, err := notify.ListNotifications(d, notify.ListFilter{Level: notify.LevelInfo}, 10, 0)
	if err != nil || len(info) != 1 || info[0].Event != notify.EventDownloadDigest {
		t.Fatalf("info=%v err=%v", info, err)
	}
	warn, err := notify.ListNotifications(d, notify.ListFilter{Level: notify.LevelWarning}, 10, 0)
	if err != nil || len(warn) != 1 || warn[0].Event != notify.EventPOTProvider {
		t.Fatalf("warn=%v err=%v", warn, err)
	}
	alert, err := notify.ListNotifications(d, notify.ListFilter{Level: notify.LevelAlert}, 10, 0)
	if err != nil || len(alert) != 1 || alert[0].Event != notify.EventYtDlpFailed {
		t.Fatalf("alert=%v err=%v", alert, err)
	}
}

func TestPOTProviderWarningUnread(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "pot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	taskID := seedTask(t, d)
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		if nt != apprise.NotifyWarning {
			t.Fatalf("notify type %v want warning", nt)
		}
		return nil
	})
	defer notify.SetSendFnForTest(old)

	if notify.EventLevel(notify.EventPOTProvider) != notify.LevelWarning {
		t.Fatal("pot_provider should be warning level")
	}
	if !notify.IsUnreadEvent(notify.EventPOTProvider) || notify.IsAlertEvent(notify.EventPOTProvider) {
		t.Fatal("pot_provider should be unread warning, not alert")
	}
	if err := notify.POTProvider(context.Background(), d, taskID, "example.com", "PO Token Providers: none"); err != nil {
		t.Fatal(err)
	}
	items, err := notify.ListNotifications(d, notify.ListFilter{UnreadOnly: true}, 10, 0)
	if err != nil || len(items) != 1 || items[0].Event != notify.EventPOTProvider {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if !items[0].Unread() {
		t.Fatal("want unread")
	}
	if _, err := notify.MarkAllRead(d); err != nil {
		t.Fatal(err)
	}
	if n, _ := notify.CountUnread(d); n != 0 {
		t.Fatalf("unread after mark-all=%d", n)
	}
}

func TestLiveSkippedInfoWithTaskID(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "live-skip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	taskID := seedTask(t, d)
	var gotType apprise.NotifyType
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		gotType = nt
		return nil
	})
	defer notify.SetSendFnForTest(old)
	if _, err := notify.Upsert(d, 0, "ap", "discord://111111111111111111/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN012345", []string{notify.EventLiveSkipped}); err != nil {
		t.Fatal(err)
	}

	if notify.EventLevel(notify.EventLiveSkipped) != notify.LevelInfo {
		t.Fatal("live_skipped should be info")
	}
	if notify.IsUnreadEvent(notify.EventLiveSkipped) {
		t.Fatal("live_skipped must not be unread")
	}
	if err := notify.LiveSkipped(context.Background(), d, taskID, "Series", "On air"); err != nil {
		t.Fatal(err)
	}
	if gotType != apprise.NotifyInfo {
		t.Fatalf("apprise type=%v want info", gotType)
	}
	items, err := notify.ListNotifications(d, notify.ListFilter{Event: notify.EventLiveSkipped}, 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if items[0].Unread() {
		t.Fatal("info should be stored read")
	}
	if !items[0].TaskID.Valid || items[0].TaskID.Int64 != taskID {
		t.Fatalf("task_id=%v want %d", items[0].TaskID, taskID)
	}
	if !strings.Contains(items[0].Body, "Series / On air") {
		t.Fatalf("body=%q", items[0].Body)
	}
}
