package web_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFilesExplorerIntegrityFailedUsesTextError(t *testing.T) {
	// String-guard: derived-failed rows paint with text-error in Files Explorer.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	path := filepath.Join(filepath.Dir(thisFile), "partials", "files_list_live.html")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if !strings.Contains(body, "IntegrityFailed") {
		t.Fatal("files_list_live missing IntegrityFailed branch")
	}
	if !strings.Contains(body, "text-error") {
		t.Fatal("files_list_live missing text-error class")
	}
	if !strings.Contains(body, `file_integrity_indicator`) {
		t.Fatal("files_list_live missing file_integrity_indicator")
	}
	if !strings.Contains(body, `1fr 7rem max-content`) {
		t.Fatal("files list row must fix integrity track width so shield icons stack")
	}
	indPath := filepath.Join(filepath.Dir(thisFile), "partials", "file_integrity_indicator.html")
	ind, err := os.ReadFile(indPath)
	if err != nil {
		t.Fatal(err)
	}
	indBody := string(ind)
	for _, icon := range []string{"shield-check", "shield-x", "shield-question", "shield-off", "shield-ban"} {
		if !strings.Contains(indBody, `data-lucide="`+icon+`"`) {
			t.Fatalf("file_integrity_indicator missing icon %s", icon)
		}
	}
	for _, st := range []string{"OK", "Failed", "Unchecked", "N/A", "Inactive"} {
		if !strings.Contains(indBody, st) {
			t.Fatalf("file_integrity_indicator missing status %s", st)
		}
	}
	// Tip hosts must not carry opacity (fades daisyUI bubble).
	if strings.Contains(indBody, "opacity-60 tooltip") || strings.Contains(indBody, "tooltip tooltip-top opacity") {
		t.Fatal("file_integrity_indicator must mute icon/label, not tip host")
	}
	if !strings.Contains(indBody, "CheckedAgo") {
		t.Fatal("file_integrity_indicator tip must use CheckedAgo (since / ISO>7d)")
	}
	if strings.Contains(body, "tooltip tooltip-left cursor-not-allowed opacity-50") {
		t.Fatal("files row actions must mute icon, not tip host")
	}
	defs := `Key: "integrity"`
	expPath := filepath.Join(filepath.Dir(thisFile), "explorer_files.go")
	exp, err := os.ReadFile(expPath)
	if err != nil {
		t.Fatal(err)
	}
	expBody := string(exp)
	iInt := strings.Index(expBody, `{Key: "integrity", Label: "Integrity", Default: true}`)
	iSize := strings.Index(expBody, `{Key: "size", Label: "Size", Default: true}`)
	if iInt < 0 || iSize < 0 || iInt > iSize {
		t.Fatalf("files table must default Integrity before Size: %s", defs)
	}
	sidecarPath := filepath.Join(filepath.Dir(thisFile), "templates", "video_sidecar_view.html")
	sidecar, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	sc := string(sidecar)
	if !strings.Contains(sc, ">Last checked<") || !strings.Contains(sc, ">Last OK<") {
		t.Fatal("file detail Details must list Last checked / Last OK rows")
	}
	if !strings.Contains(body, "Check integrity") {
		t.Fatal("files_list_live missing Check integrity action")
	}
	if !strings.Contains(body, "check-file-hash") {
		t.Fatal("files_list_live missing check-file-hash form action")
	}
	if !strings.Contains(body, "data-files-bulk-mode") || !strings.Contains(body, "js-file-select") {
		t.Fatal("files_list_live missing multi-select hooks")
	}
	bulkPath := filepath.Join(filepath.Dir(thisFile), "partials", "files_bulk_modals.html")
	bulk, err := os.ReadFile(bulkPath)
	if err != nil {
		t.Fatal(err)
	}
	bulkBody := string(bulk)
	if !strings.Contains(bulkBody, "bulk-check-file-hash") || !strings.Contains(bulkBody, "bulk-delete-video-sidecar") {
		t.Fatal("files bulk modals missing check/delete actions")
	}
	if !strings.Contains(body, "DeleteDisabledTip") {
		t.Fatal("files_list_live missing disabled Delete tip branch")
	}
	if !strings.Contains(body, `data-tip="Delete"`) {
		t.Fatal("files_list_live missing enabled Delete tip")
	}
	if !strings.Contains(body, `tooltip tooltip-left" data-tip="Check integrity"`) ||
		!strings.Contains(body, `tooltip tooltip-left" data-tip="Delete"`) {
		t.Fatal("files row actions must use tooltip-left (table wrap clips top tips)")
	}
	if !strings.Contains(body, `eq .Key "filepath"`) || !strings.Contains(body, `eq .Key "acquired"`) {
		t.Fatal("files_list_live missing Path/Acquired table columns")
	}
}

func TestFilesListLiveSSERefreshPin(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	path := filepath.Join(filepath.Dir(thisFile), "ui", "src", "js", "files_live.js")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, kind := range []string{"file_hash_check", "integrity_check", "integrity_check_initial", "sync_files"} {
		if !strings.Contains(body, kind) {
			t.Fatalf("files_live.js missing kind %s", kind)
		}
	}
	if !strings.Contains(body, "files-list-live") {
		t.Fatal("files_live.js must target files-list-live")
	}
	ssePath := filepath.Join(filepath.Dir(thisFile), "ui", "src", "js", "sse.js")
	sse, err := os.ReadFile(ssePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sse), "maybeRefreshFilesList") {
		t.Fatal("sse.js must call maybeRefreshFilesList")
	}
}

func TestBrowserHasFilesTypeJoin(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	path := filepath.Join(filepath.Dir(thisFile), "templates", "browser.html")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if !strings.Contains(body, `type=files`) {
		t.Fatal("browser missing Files type join")
	}
	if !strings.Contains(body, `files_list_live`) {
		t.Fatal("browser missing files_list_live embed")
	}
}
