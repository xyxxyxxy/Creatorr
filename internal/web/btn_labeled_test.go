package web

import (
	"html/template"
	"strings"
	"testing"
)

func TestBoolDataAttr(t *testing.T) {
	if got := boolDataAttr("video-bulk-refresh"); got != template.HTMLAttr("data-video-bulk-refresh") {
		t.Fatalf("got %q", got)
	}
	if got := boolDataAttr("Bad_Attr"); got != "" {
		t.Fatalf("reject unsafe suffix: %q", got)
	}
	if got := boolDataAttr(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func TestBtnLabeledRendersDataAttr(t *testing.T) {
	tmpl, err := parseTemplates(embedded)
	if err != nil {
		t.Fatal(err)
	}
	wrap := `{{define "x"}}{{template "btn_labeled" (dict "Icon" "pencil" "Label" "Edit" "Data" "series-bulk-edit" "Disabled" true)}}{{end}}`
	tmpl, err = tmpl.New("x").Parse(wrap)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "x", nil); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.Contains(got, `data-series-bulk-edit`) || strings.Contains(got, "ZgotmplZ") {
		t.Fatalf("expected trusted data attr: %s", got)
	}
	if !strings.Contains(got, `class="btn" disabled`) {
		t.Fatalf("expected default btn + disabled: %s", got)
	}
	if strings.Contains(got, "btn-outline") || strings.Contains(got, "btn-primary") || strings.Contains(got, "btn-secondary") {
		t.Fatalf("labeled action must stay default btn: %s", got)
	}
}
