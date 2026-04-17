package platform

import (
	"errors"
	"fmt"
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
	ErrWorkflowActive         = "WORKFLOW_ACTIVE"
	ErrSessionNotFound        = "SESSION_NOT_FOUND"
	ErrBootstrapActive        = "BOOTSTRAP_ACTIVE"
	ErrBootstrapNotActive     = "BOOTSTRAP_NOT_ACTIVE"
	ErrDeployNotActive        = "DEPLOY_NOT_ACTIVE"
	ErrSSHDeployFailed        = "SSH_DEPLOY_FAILED"
	ErrDeployFailed           = "DEPLOY_FAILED"
	ErrPrerequisiteMissing    = "PREREQUISITE_MISSING"
	ErrWorkflowRequired       = "WORKFLOW_REQUIRED"
	ErrSelfServiceBlocked     = "SELF_SERVICE_BLOCKED"
	ErrGitTokenMissing        = "GIT_TOKEN_MISSING"
	ErrGitHistoryConflict     = "GIT_HISTORY_CONFLICT"
	ErrGitPushRejected        = "GIT_PUSH_REJECTED"
	ErrGitAuthFailed          = "GIT_AUTH_FAILED"
	ErrSubagentMisuse         = "SUBAGENT_MISUSE"
)

// PlatformError carries a ZCP error code, message, and suggestion.
type PlatformError struct {
	Code       string
	Message    string
	Suggestion string
	APICode    string // raw API error code, empty if not from API
	Diagnostic string // raw command output for LLM debugging (SSH output, etc.)
}

// SSHExecError carries structured SSH execution failure data.
// Separates OS-level exit error from remote command output so classifiers
// can distinguish progress messages from actual errors.
type SSHExecError struct {
	Hostname string
	Output   string // combined stdout+stderr from remote command
	Err      error  // underlying exec error (exit code, signal, etc.)
}

func (e *SSHExecError) Error() string {
	return fmt.Sprintf("ssh %s: %s", e.Hostname, e.Err.Error())
}

func (e *SSHExecError) Unwrap() error { return e.Err }

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
