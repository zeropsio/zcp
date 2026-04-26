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
	ErrAuthTokenExpired       = "AUTH_TOKEN_EXPIRED"
	ErrTokenNoProject         = "TOKEN_NO_PROJECT"
	ErrTokenMultiProject      = "TOKEN_MULTI_PROJECT"
	ErrServiceNotFound        = "SERVICE_NOT_FOUND"
	ErrServiceRequired        = "SERVICE_REQUIRED"
	ErrFileNotFound           = "FILE_NOT_FOUND"
	ErrInvalidZeropsYml       = "INVALID_ZEROPS_YML"
	ErrInvalidImportYml       = "INVALID_IMPORT_YML"
	ErrImportHasProject       = "IMPORT_HAS_PROJECT"
	ErrInvalidScaling         = "INVALID_SCALING"
	ErrInvalidParameter       = "INVALID_PARAMETER"
	ErrInvalidEnvFormat       = "INVALID_ENV_FORMAT"
	ErrInvalidHostname        = "INVALID_HOSTNAME"
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
	ErrBootstrapNotActive     = "BOOTSTRAP_NOT_ACTIVE"
	ErrSSHDeployFailed        = "SSH_DEPLOY_FAILED"
	ErrDeployFailed           = "DEPLOY_FAILED"
	ErrPrerequisiteMissing    = "PREREQUISITE_MISSING"
	ErrWorkflowRequired       = "WORKFLOW_REQUIRED"
	ErrSelfServiceBlocked     = "SELF_SERVICE_BLOCKED"
	ErrGitTokenMissing        = "GIT_TOKEN_MISSING"
	ErrSubagentMisuse         = "SUBAGENT_MISUSE"
	ErrExportBlocked          = "EXPORT_BLOCKED"
	ErrMissingEvidence        = "MISSING_EVIDENCE"
	ErrTopicEmpty             = "TOPIC_EMPTY"
	ErrWorkSessionCorrupt     = "WORK_SESSION_CORRUPT"
	// ErrPreflightFailed signals a deploy preflight check failure. Carried
	// alongside structured CheckWire entries so the agent can re-run the
	// failed check or fix the underlying issue. Replaces the legacy
	// preflight wire shapes (jsonResult of StepCheckResult, ad-hoc batch
	// {preFlightFailedFor, result} map).
	ErrPreflightFailed = "PREFLIGHT_FAILED"
	// ErrUnknown wraps generic Go errors at the tools/convert.go boundary
	// when the underlying error doesn't carry a typed PlatformError code.
	// Eliminates the plain-text wire shape — every tool error response is
	// typed JSON.
	ErrUnknown = "UNKNOWN"
)

// PlatformError carries a ZCP error code, message, and suggestion.
type PlatformError struct {
	Code       string
	Message    string
	Suggestion string
	APICode    string        // raw API error code, empty if not from API
	Diagnostic string        // raw command output for LLM debugging (SSH output, etc.)
	APIMeta    []APIMetaItem // server-provided field-level detail, empty when API did not send meta
}

// APIMetaItem mirrors one element of the Zerops API's `error.meta[]` array.
// The server emits this shape for every 4xx response; ZCP surfaces it to the
// LLM so the failing field and reason are visible without trial-and-error.
// Example from live probe: metadata={"storage.mode": ["mode not supported"]}.
type APIMetaItem struct {
	Code     string              `json:"code,omitempty"`
	Error    string              `json:"error,omitempty"`
	Metadata map[string][]string `json:"metadata,omitempty"`
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
