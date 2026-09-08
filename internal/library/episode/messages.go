package episode

import (
	"fmt"
	"strings"
)

// RenamedHistoryMessage is the video History message for a successful episode rename.
// When triggerVideoID is another video (peer-move after its pack/download), the message
// names that trigger so History is not mistaken for an Apply-only rename.
func RenamedHistoryMessage(thisVideoID, triggerVideoID int64, triggerTitle string) string {
	const base = "Episode files renamed"
	if triggerVideoID <= 0 || triggerVideoID == thisVideoID {
		return base
	}
	title := strings.TrimSpace(triggerTitle)
	if title == "" {
		return base + " (peer move after another video)"
	}
	return base + " (peer move after '" + title + "')"
}

func ApplyNamingMessage(renamed, skippedBusy, failed int) string {
	return fmt.Sprintf("Renamed %d, skipped busy %d, failed %d", renamed, skippedBusy, failed)
}
