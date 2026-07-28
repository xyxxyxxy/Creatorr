package notify

import (
	"fmt"
	"strings"
)

// In-app Creatorr channel (virtual; not stored in notification_channels).
// Always subscribed to AllEvents; not editable or deletable in Settings.
const (
	InAppURL  = "creatorr://in-app"
	InAppName = "Creatorr"
)

// ErrInAppChannelReadOnly is returned when Upsert/Delete targets the in-app channel.
var ErrInAppChannelReadOnly = fmt.Errorf("in-app Creatorr channel cannot be edited or deleted")

// IsInAppURL reports whether raw is the fixed in-app channel URL.
func IsInAppURL(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), InAppURL)
}

// IsInAppChannel reports whether c is the fixed in-app channel.
func IsInAppChannel(c Channel) bool {
	return IsInAppURL(c.URL)
}

// InAppChannel returns the fixed in-app delivery target (all events).
func InAppChannel() Channel {
	return Channel{
		ID:     0,
		Name:   InAppName,
		URL:    InAppURL,
		Events: append([]string(nil), AllEvents...),
	}
}
