package web

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNotificationsBulkConfirmPattern(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	live, err := os.ReadFile(filepath.Join(dir, "partials", "notifications_list_live.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(live), `{{template "notifications_bulk_modals" .}}`) {
		t.Fatal("notifications_list_live must host bulk confirm modals")
	}
	modals, err := os.ReadFile(filepath.Join(dir, "partials", "notifications_bulk_modals.html"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(modals)
	if !strings.Contains(body, `id="modal-bulk-notification-read"`) ||
		!strings.Contains(body, `id="form-bulk-notification-read-confirm"`) ||
		!strings.Contains(body, `id="modal-bulk-notification-unread"`) ||
		!strings.Contains(body, `id="form-bulk-notification-unread-confirm"`) {
		t.Fatal("notification bulk modals missing read/unread confirm forms")
	}
	js, err := os.ReadFile(filepath.Join(dir, "ui", "src", "js", "notifications_bulk.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(js)
	if !strings.Contains(src, "NOTIFICATIONS_BULK_CONFIRM_AFTER = 5") {
		t.Fatal("notifications bulk must use confirm-after 5")
	}
	if !strings.Contains(src, `runNotificationsBulkAction("read")`) ||
		!strings.Contains(src, `runNotificationsBulkAction("unread")`) {
		t.Fatal("mark read/unread must go through runNotificationsBulkAction")
	}
}
