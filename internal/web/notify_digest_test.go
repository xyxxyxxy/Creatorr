package web

import "testing"

func TestDigestRelatedSectionsFromBody(t *testing.T) {
	body := "- Chud Logic / Episode (downloaded) [#6]\n- Other / Gone (downloaded)"
	secs := digestRelatedSectionsFromBody(body,
		func(id int64) notifyFileSyncIssueRef {
			return notifyFileSyncIssueRef{ID: id, SeriesID: 1, Series: "Chud Logic", Title: "Episode"}
		},
		func(series, title string) notifyFileSyncIssueRef {
			if series == "Other" && title == "Gone" {
				return notifyFileSyncIssueRef{ID: 9, SeriesID: 2, Series: series, Title: title}
			}
			return notifyFileSyncIssueRef{Series: series, Title: title, Missing: true}
		},
	)
	if len(secs) != 1 || secs[0].Heading != "Downloaded" || secs[0].Total != 2 {
		t.Fatalf("%#v", secs)
	}
	if secs[0].Items[0].ID != 6 || secs[0].Items[0].SeriesID != 1 {
		t.Fatalf("id row: %#v", secs[0].Items[0])
	}
	if secs[0].Items[1].ID != 9 {
		t.Fatalf("title row: %#v", secs[0].Items[1])
	}
}
