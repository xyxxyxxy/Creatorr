package nametemplate

import (
	"strings"
	"testing"
)

func TestExpandPads(t *testing.T) {
	got, err := Expand("{series} - S{year:00}E{episode:000} - {title} [{id}]", Values{
		Series: "Show", Year: 1, Episode: 42, Title: "Hello", ID: "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Show - S01E042 - Hello [abc]"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestValidateUnknown(t *testing.T) {
	if err := Validate("{foo}"); err == nil {
		t.Fatal("expected error")
	}
	if err := Validate("{series} {year}"); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeFilename(t *testing.T) {
	got := SanitizeFilename("  Hello😀/World?.mkv  ", 80)
	if strings.Contains(got, "/") || strings.Contains(got, ":") || strings.Contains(got, "😀") {
		t.Fatalf("unsafe: %q", got)
	}
	empty := SanitizeFilename("!!!", 10)
	if empty != "untitled" {
		t.Fatalf("empty-ish got %q", empty)
	}
	long := SanitizeFilename(strings.Repeat("a", 100), 10)
	if len([]rune(long)) != 10 {
		t.Fatalf("truncate: %q len %d", long, len([]rune(long)))
	}
}

func TestExpandAndSanitize(t *testing.T) {
	got, err := ExpandAndSanitize("Season {year}", Values{Year: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Season 3" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandDateParts(t *testing.T) {
	v := Values{
		Year: 2024, Month: 3, Day: 15,
	}
	got, err := ExpandAndSanitize("{year}-{month:02}-{day:02}", v)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2024-03-15" {
		t.Fatalf("got %q", got)
	}
	got2, err := ExpandAndSanitize("{year}-{month}-{day}", Values{Year: 2026, Month: 1, Day: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got2 != "2026-1-5" {
		t.Fatalf("got %q", got2)
	}
	empty, err := ExpandAndSanitize("Y{year}M{month}D{day}", Values{})
	if err != nil {
		t.Fatal(err)
	}
	if empty != "Y0000MD" {
		t.Fatalf("zero year-season: %q", empty)
	}
}

func TestExpandYearUndated(t *testing.T) {
	got, err := Expand("S{year}", Values{Year: 0})
	if err != nil {
		t.Fatal(err)
	}
	if got != "S0000" {
		t.Fatalf("undated year: got %q want S0000", got)
	}
	got2, err := Expand("S{year}", Values{Year: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if got2 != "S2026" {
		t.Fatalf("dated year: got %q want S2026", got2)
	}
}

func TestExpandDateAndDomain(t *testing.T) {
	got, err := Expand("{date} [{domain}]", Values{Date: "2024-03-15", Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "2024-03-15 [example.com]" {
		t.Fatalf("got %q", got)
	}
	empty, err := ExpandAndSanitize("X{date}Y{domain}Z", Values{})
	if err != nil {
		t.Fatal(err)
	}
	if empty != "XYZ" {
		t.Fatalf("empty date/domain: %q", empty)
	}
}

func TestTitleMaxSuffix(t *testing.T) {
	long := strings.Repeat("a", 100)
	got, err := ExpandAndSanitize("{title:10}", Values{Title: long})
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(got)) != 10 {
		t.Fatalf("got %q len %d", got, len([]rune(got)))
	}
	got2, err := ExpandAndSanitize("{title}", Values{Title: long})
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(got2)) != 100 {
		t.Fatalf("bare title not truncated: %q len %d", got2, len([]rune(got2)))
	}
}

func TestSeriesMaxSuffix(t *testing.T) {
	long := strings.Repeat("b", 120)
	got, err := ExpandAndSanitize("{series:100}", Values{Series: long})
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(got)) != 100 {
		t.Fatalf("series:100 got %q len %d", got, len([]rune(got)))
	}
	got2, err := ExpandAndSanitize("{series}", Values{Series: long})
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(got2)) != 120 {
		t.Fatalf("bare series not truncated: %q len %d", got2, len([]rune(got2)))
	}
}
