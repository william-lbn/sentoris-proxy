package errors

import (
	"fmt"
	"net/http"
)

type ErrorCode string

const (
	ErrInvalidConstraint          ErrorCode = "SENTORIS_INVALID_CONSTRAINT"
	ErrBadConstraintCombination    ErrorCode = "SENTORIS_BAD_CONSTRAINT_COMBINATION"
	ErrBudgetExceeded             ErrorCode = "SENTORIS_BUDGET_EXCEEDED"
	ErrMissingFocusFields         ErrorCode = "SENTORIS_MISSING_FOCUS_FIELDS"
	ErrJCSCanonicalizationFailed ErrorCode = "SENTORIS_JCS_CANONICALIZATION_FAILED"
	ErrSchemaViolation            ErrorCode = "SENTORIS_SCHEMA_VIOLATION"
	ErrStateTransitionInvalid     ErrorCode = "SENTORIS_STATE_TRANSITION_INVALID"
	ErrUpstreamTimeout            ErrorCode = "SENTORIS_UPSTREAM_TIMEOUT"
	ErrUpstreamDisconnect         ErrorCode = "SENTORIS_UPSTREAM_DISCONNECT"
	ErrUpstreamError              ErrorCode = "SENTORIS_UPSTREAM_ERROR"
	ErrAuthRequired               ErrorCode = "SENTORIS_AUTH_REQUIRED"
	ErrPermissionDenied           ErrorCode = "SENTORIS_PERMISSION_DENIED"
	ErrBaselineNotFound           ErrorCode = "SENTORIS_BASELINE_NOT_FOUND"
	ErrRateLimited                ErrorCode = "SENTORIS_RATE_LIMITED"
	ErrStateUnavailable           ErrorCode = "SENTORIS_STATE_UNAVAILABLE"
	ErrInternalError              ErrorCode = "SENTORIS_INTERNAL_ERROR"
	ErrVersionMismatch            ErrorCode = "SENTORIS_VERSION_MISMATCH"
	ErrInvalidMethod              ErrorCode = "SENTORIS_INVALID_METHOD"
	ErrProviderNotFound           ErrorCode = "SENTORIS_PROVIDER_NOT_FOUND"
	ErrInvalidInput               ErrorCode = "SENTORIS_INVALID_INPUT"
)

func (e ErrorCode) Error() string {
	return string(e)
}

type SentorisError struct {
	Code    ErrorCode          `json:"code"`
	Message string             `json:"message"`
	Param   string             `json:"param,omitempty"`
	Type    string             `json:"type"`
	Details map[string]any     `json:"details,omitempty"`
}

func (e *SentorisError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewSentorisError(code ErrorCode, message string) *SentorisError {
	return &SentorisError{
		Code:    code,
		Message: message,
		Type:    "sentoris_error",
	}
}

func (e *SentorisError) WithParam(param string) *SentorisError {
	e.Param = param
	return e
}

func (e *SentorisError) WithDetails(details map[string]any) *SentorisError {
	e.Details = details
	return e
}

func (e *SentorisError) ToMap() map[string]any {
	m := map[string]any{
		"error": map[string]any{
			"message": e.Message,
			"type":    e.Type,
			"code":    e.Code,
		},
	}
	if e.Param != "" {
		m["error"].(map[string]any)["param"] = e.Param
	}
	if e.Details != nil {
		m["error"].(map[string]any)["details"] = e.Details
	}
	return m
}

func (e *SentorisError) HTTPStatusCode() int {
	switch e.Code {
	case ErrInvalidConstraint, ErrBadConstraintCombination, ErrMissingFocusFields, ErrVersionMismatch:
		return http.StatusBadRequest
	case ErrAuthRequired:
		return http.StatusUnauthorized
	case ErrPermissionDenied:
		return http.StatusForbidden
	case ErrBaselineNotFound:
		return http.StatusNotFound
	case ErrSchemaViolation:
		return http.StatusUnprocessableEntity
	case ErrBudgetExceeded, ErrRateLimited:
		return http.StatusTooManyRequests
	case ErrInternalError, ErrStateTransitionInvalid:
		return http.StatusInternalServerError
	case ErrUpstreamDisconnect:
		return http.StatusBadGateway
	case ErrStateUnavailable:
		return http.StatusServiceUnavailable
	case ErrUpstreamTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

func (e *SentorisError) ShouldRetry() bool {
	switch e.Code {
	case ErrBudgetExceeded:
		return false
	case ErrRateLimited, ErrUpstreamTimeout, ErrUpstreamDisconnect, ErrStateUnavailable, ErrInternalError:
		return true
	default:
		return false
	}
}

func WrapError(err error, code ErrorCode, message string) *SentorisError {
	sentorisErr := NewSentorisError(code, message)
	if err != nil {
		sentorisErr.Details = map[string]any{
			"underlying_error": err.Error(),
		}
	}
	return sentorisErr
}
