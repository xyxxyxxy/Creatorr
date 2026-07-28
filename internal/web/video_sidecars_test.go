package web

import "testing"

func TestPrettyJSON(t *testing.T) {
	got, ok := prettyJSON(`{"b":1,"a":[2,3]}`)
	if !ok {
		t.Fatal("expected ok")
	}
	want := "{\n  \"b\": 1,\n  \"a\": [\n    2,\n    3\n  ]\n}"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, ok := prettyJSON(`{broken`); ok {
		t.Fatal("expected fail on invalid JSON")
	}
}

func TestSidecarIsVideo(t *testing.T) {
	if !sidecarIsVideo("video", "ep.mkv") {
		t.Fatal("kind video")
	}
	if !sidecarIsVideo("", "clip.mp4") {
		t.Fatal("ext mp4")
	}
	if sidecarIsVideo("json", "x.info.json") {
		t.Fatal("json not video")
	}
}
