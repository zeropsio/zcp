package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/zeropsio/zerops-go/apiError"
)

// mapSDKError converts SDK/API errors to ZCP platform errors.
func mapSDKError(err error, entityType string) error {
	if err == nil {
		return nil
	}

	var apiErr apiError.Error
	if errors.As(err, &apiErr) {
		return mapAPIError(apiErr, entityType)
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return NewPlatformError(ErrNetworkError, err.Error(), "Check network connectivity")
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return NewPlatformError(ErrNetworkError, err.Error(), "Check API host DNS")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewPlatformError(ErrAPITimeout, "API request timed out", "Retry the operation")
	}
	if errors.Is(err, context.Canceled) {
		return NewPlatformError(ErrAPIError, "request canceled", "")
	}

	errStr := err.Error()
	if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "no such host") {
		return NewPlatformError(ErrNetworkError, errStr, "Check API host and network")
	}

	return NewPlatformError(ErrAPIError, errStr, "")
}

// withAPICode sets the APICode field on a PlatformError and returns it.
func withAPICode(pe *PlatformError, apiCode string) *PlatformError {
	pe.APICode = apiCode
	return pe
}

func mapAPIError(apiErr apiError.Error, entityType string) error {
	code := apiErr.GetHttpStatusCode()
	errCode := apiErr.GetErrorCode()
	msg := apiErr.GetMessage()

	switch code {
	case http.StatusUnauthorized:
		return withAPICode(NewPlatformError(ErrAuthTokenExpired, msg, "Check token validity"), errCode)
	case http.StatusForbidden:
		return withAPICode(NewPlatformError(ErrPermissionDenied, msg, "Check token permissions"), errCode)
	case http.StatusNotFound:
		switch entityType {
		case "process":
			return withAPICode(NewPlatformError(ErrProcessNotFound, msg, "Check process ID"), errCode)
		default:
			return withAPICode(NewPlatformError(ErrServiceNotFound, msg, "Check service hostname"), errCode)
		}
	case http.StatusTooManyRequests:
		return withAPICode(NewPlatformError(ErrAPIRateLimited, msg, "Wait and retry"), errCode)
	}

	if code >= 500 {
		return withAPICode(NewPlatformError(ErrAPIError, msg, "Zerops API server error — retry later"), errCode)
	}

	// Client error (4xx) — tell LLM to fix input
	suggestion := "Check the request parameters"
	if errCode != "" {
		suggestion = fmt.Sprintf("API rejected the request (code: %s) — check the input parameters", errCode)
	}
	return withAPICode(NewPlatformError(ErrAPIError, msg, suggestion), errCode)
}
