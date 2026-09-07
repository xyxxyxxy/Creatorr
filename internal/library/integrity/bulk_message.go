package integrity

import "fmt"

type VerifyAllMediaFail struct {
	VideoID     int64
	SeriesTitle string
	VideoTitle  string
	Detail      string
}

func VerifyAllMediaMessage(checked, partial, skipped, failed int) string {
	return fmt.Sprintf("Integrity checked %d, partial %d, skipped %d, failed %d", checked, partial, skipped, failed)
}
