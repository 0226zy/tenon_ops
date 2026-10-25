package services

import (
	"fmt"
	"net/http"
	"time"
)

type Error struct {
	Code       string
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *Error) Error() string { return e.Message }

func E(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

func BadRequest(code, msg string) *Error { return E(http.StatusBadRequest, code, msg) }
func NotFound(msg string) *Error         { return E(http.StatusNotFound, "NOT_FOUND", msg) }
func Conflict(code, msg string) *Error   { return E(http.StatusConflict, code, msg) }
func Internal(msg string) *Error         { return E(http.StatusInternalServerError, "INTERNAL", msg) }

func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	return Internal("internal error")
}

var (
	ErrPasswordNotManaged = E(http.StatusConflict, "PASSWORD_NOT_MANAGED", "password is not managed; reset it first")
	ErrStepUpRequired     = E(http.StatusForbidden, "STEP_UP_REQUIRED", "re-authentication required")
	ErrSchemaMismatch     = E(http.StatusServiceUnavailable, "TENON_SCHEMA_MISMATCH", "Tenon schema is not compatible")
)

func Wrap(err error, msg string) *Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	return Internal(fmt.Sprintf("%s: %v", msg, err))
}
