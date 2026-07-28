package errors

// AppError is the structured error used across API, worker, and Activity.
type AppError struct {
	Code    string
	Message string
	Detail  string
	Cause   error
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail != "" {
		return e.Message + ": " + e.Detail
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func New(code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

func Wrap(code, message string, cause error) *AppError {
	return &AppError{Code: code, Message: message, Cause: cause}
}

func WithDetail(err *AppError, detail string) *AppError {
	if err == nil {
		return nil
	}
	cp := *err
	cp.Detail = detail
	return &cp
}

// Stable codes (extend as handlers grow).
const (
	CodeInternal       = "Internal"
	CodeNotFound       = "NotFound"
	CodeConflict       = "Conflict"
	CodeCookieInvalid  = "CookieInvalid"
	CodeCookieMissing  = "CookieMissing"
	CodeRateLimited    = "RateLimited"
	CodeDownloadFailed = "DownloadFailed"
	CodeRemuxFailed    = "RemuxFailed"
	CodePackFailed     = "PackFailed"
	CodeResolveFailed  = "ResolveFailed"
	CodeScanFailed     = "ScanFailed"
	CodeImportFailed   = "ImportFailed"
	CodeFlareSolverrRequired = "FlareSolverrRequired"
	CodeHandlerCapabilityMissing = "HandlerCapabilityMissing"
	CodeMediaTypeExcluded = "MediaTypeExcluded"
	CodeLiveBroadcastSkipped = "LiveBroadcastSkipped"
	CodeMediaVerifyFailed = "MediaVerifyFailed"
	CodeUnauthorized = "Unauthorized"
	CodeSetupRequired = "SetupRequired"
)

// DownloadFailStage maps a download-task failure code to a pipeline stage for
// video activity detail: fetch (handler), remux (ffmpeg), or pack (library install).
func DownloadFailStage(code string) string {
	switch code {
	case CodeRemuxFailed:
		return "remux"
	case CodePackFailed:
		return "pack"
	case CodeMediaVerifyFailed:
		return "verify"
	default:
		return "fetch"
	}
}
