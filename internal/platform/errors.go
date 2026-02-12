package platform

import (
	"errors"
	"net"
	"strings"
)

// Error codes for ZCP.
const (
	ErrAuthRequired           = "AUTH_REQUIRED"
	ErrAuthInvalidToken       = "AUTH_INVALID_TOKEN"
	ErrAuthTokenExpired       = "AUTH_TOKEN_EXPIRED"
	ErrAuthAPIError           = "AUTH_API_ERROR"
	ErrTokenNoProject         = "TOKEN_NO_PROJECT"
	ErrTokenMultiProject      = "TOKEN_MULTI_PROJECT"
	ErrServiceNotFound        = "SERVICE_NOT_FOUND"
	ErrServiceRequired        = "SERVICE_REQUIRED"
	ErrConfirmRequired        = "CONFIRM_REQUIRED"
	ErrFileNotFound           = "FILE_NOT_FOUND"
	ErrZeropsYmlNotFound      = "ZEROPS_YML_NOT_FOUND"
	ErrInvalidZeropsYml       = "INVALID_ZEROPS_YML"
	ErrInvalidImportYml       = "INVALID_IMPORT_YML"
	ErrImportHasProject       = "IMPORT_HAS_PROJECT"
	ErrInvalidScaling         = "INVALID_SCALING"
	ErrInvalidParameter       = "INVALID_PARAMETER"
	ErrInvalidEnvFormat       = "INVALID_ENV_FORMAT"
	ErrInvalidHostname        = "INVALID_HOSTNAME"
	ErrUnknownType            = "UNKNOWN_TYPE"
	ErrProcessNotFound        = "PROCESS_NOT_FOUND"
	ErrProcessAlreadyTerminal = "PROCESS_ALREADY_TERMINAL"
	ErrPermissionDenied       = "PERMISSION_DENIED"
	ErrAPIError               = "API_ERROR"
	ErrAPITimeout             = "API_TIMEOUT"
	ErrAPIRateLimited         = "API_RATE_LIMITED"
	ErrNetworkError           = "NETWORK_ERROR"
	ErrInvalidUsage           = "INVALID_USAGE"
	ErrMountFailed            = "MOUNT_FAILED"
	ErrUnmountFailed          = "UNMOUNT_FAILED"
	ErrNotImplemented         = "NOT_IMPLEMENTED"
)

// PlatformError carries a ZCP error code, message, and suggestion.
type PlatformError struct {
	Code       string
	Message    string
	Suggestion string
}

func (e *PlatformError) Error() string {
	return e.Message
}

// NewPlatformError creates a PlatformError with the given code, message, and suggestion.
func NewPlatformError(code, message, suggestion string) *PlatformError {
	return &PlatformError{
		Code:       code,
		Message:    message,
		Suggestion: suggestion,
	}
}

// MapNetworkError determines if an error is a network error and returns the appropriate code.
func MapNetworkError(err error) (code string, isNetwork bool) {
	if err == nil {
		return "", false
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return ErrNetworkError, true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ErrNetworkError, true
	}

	msg := err.Error()
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "i/o timeout") {
		return ErrNetworkError, true
	}

	if strings.Contains(msg, "context deadline exceeded") {
		return ErrAPITimeout, true
	}

	return "", false
}
