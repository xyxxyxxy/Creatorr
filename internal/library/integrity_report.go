package library

import "github.com/xyxxyxxy/Creatorr/internal/library/integrity"

const (
	IntegrityCheckNullDecode      = integrity.IntegrityCheckNullDecode
	IntegrityCheckMediaChecksum   = integrity.IntegrityCheckMediaChecksum
	IntegrityCheckSidecarChecksum = integrity.IntegrityCheckSidecarChecksum
	IntegrityCheckNFO             = integrity.IntegrityCheckNFO

	IntegrityResultOK      = integrity.IntegrityResultOK
	IntegrityResultFilled  = integrity.IntegrityResultFilled
	IntegrityResultFailed  = integrity.IntegrityResultFailed
	IntegrityResultPartial = integrity.IntegrityResultPartial
	IntegrityResultSkipped = integrity.IntegrityResultSkipped

	IntegrityOutcomeOK      = integrity.IntegrityOutcomeOK
	IntegrityOutcomePartial = integrity.IntegrityOutcomePartial
	IntegrityOutcomeFailed  = integrity.IntegrityOutcomeFailed
	IntegrityOutcomeSkipped = integrity.IntegrityOutcomeSkipped

	IntegrityCheckDetailCap = integrity.IntegrityCheckDetailCap
)

type IntegrityCheckItem = integrity.IntegrityCheckItem
type IntegrityCheckReport = integrity.IntegrityCheckReport
type VerifyAllMediaResult = integrity.VerifyAllMediaResult

func bumpCheckAgg(agg map[string]map[string]int, report *IntegrityCheckReport) {
	integrity.BumpCheckAgg(agg, report)
}

func appendCappedID(dst []int64, id int64) []int64 {
	return integrity.AppendCappedID(dst, id)
}

func integrityFailDetail(err error) string {
	return integrity.FailDetail(err)
}
