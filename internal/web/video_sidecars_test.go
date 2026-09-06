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

func TestVideoMediaPlay(t *testing.T) {
	files := []videoFileView{
		{ID: 0, Kind: "video", Path: "gone.mkv", Missing: true},
		{ID: 9, Kind: "nfo", Path: "ep.nfo"},
		{ID: 12, Kind: "video", Path: "/lib/ep.mkv"},
	}
	href, audio, ok := videoMediaPlay(files, 3, 7, false)
	if !ok || audio || href != "/series/3/videos/7/files/12/raw" {
		t.Fatalf("got %q audio=%v ok=%v", href, audio, ok)
	}
	_, audio, ok = videoMediaPlay([]videoFileView{{ID: 1, Kind: "video", Path: "a.mka"}}, 1, 2, false)
	if !ok || !audio {
		t.Fatalf("mka audio=%v ok=%v", audio, ok)
	}
	_, audio, ok = videoMediaPlay([]videoFileView{{ID: 1, Kind: "video", Path: "a.mkv"}}, 1, 2, true)
	if !ok || !audio {
		t.Fatalf("audio series=%v ok=%v", audio, ok)
	}
	if _, _, ok := videoMediaPlay(nil, 1, 2, false); ok {
		t.Fatal("empty")
	}
}
