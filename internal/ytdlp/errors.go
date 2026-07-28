package ytdlp

import (
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

func appErr(code, message, detail string) *apperrors.AppError {
	e := apperrors.New(code, message)
	if detail != "" {
		return apperrors.WithDetail(e, detail)
	}
	return e
}
