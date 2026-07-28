package ytdlp

import (
	"errors"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

// asAppError coerces any error into *apperrors.AppError, defaulting to fallbackCode
// when the error did not already carry an AppError.
func asAppError(err error, fallbackCode string) *apperrors.AppError {
	var ae *apperrors.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return apperrors.WithDetail(apperrors.New(fallbackCode, "yt-dlp command failed"), err.Error())
}

func appErr(code, message, detail string) *apperrors.AppError {
	e := apperrors.New(code, message)
	if detail != "" {
		return apperrors.WithDetail(e, detail)
	}
	return e
}
