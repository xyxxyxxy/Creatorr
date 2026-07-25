package notify_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	apprise "github.com/unraid/apprise-go"
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
	defer d.Close()
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
	if !items[0].ExternalOK || items[0].Unread() {
		t.Fatalf("cookie with channel should auto-read: %#v", items[0])
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
	defer d.Close()
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

func TestFormatDigestBody(t *testing.T) {
	body := notify.FormatDigestBody([]notify.DigestItem{
		{Series: "A", Title: "One", Kind: "archive"},
		{Series: "B", Title: "Two", Kind: "stream", Beginning: true},
		{Series: "C", Title: "Three", Kind: "stream"},
	})
	want := "- A / One (downloaded)\n- B / Two (stream, beginning cached)\n- C / Three (stream)"
	if body != want {
		t.Fatalf("got %q want %q", body, want)
	}
}

func TestSendEventNoChannelsStillRecords(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
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
	defer d.Close()
	if err := notify.SendEvent(context.Background(), d, notify.EventYtDlpFailed, "t", "b", 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestYtDlpFailedExternalFailStaysUnread(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
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

func TestDownloadDigestAlwaysRead(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "dig.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
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
	defer d.Close()
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
	defer d.Close()
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

func TestPOTProviderWarningUnread(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "pot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
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

