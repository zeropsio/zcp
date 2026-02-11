package platform

import (
	"context"
	"errors"
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

func mapAPIError(apiErr apiError.Error, entityType string) error {
	code := apiErr.GetHttpStatusCode()
	errCode := apiErr.GetErrorCode()
	msg := apiErr.GetMessage()

	switch code {
	case http.StatusUnauthorized:
		return NewPlatformError(ErrAuthTokenExpired, msg, "Check token validity")
	case http.StatusForbidden:
		return NewPlatformError(ErrPermissionDenied, msg, "Check token permissions")
	case http.StatusNotFound:
		switch entityType {
		case "process":
			return NewPlatformError(ErrProcessNotFound, msg, "Check process ID")
		default:
			return NewPlatformError(ErrServiceNotFound, msg, "Check service hostname")
		}
	case http.StatusTooManyRequests:
		return NewPlatformError(ErrAPIRateLimited, msg, "Wait and retry")
	}

	switch {
	case strings.Contains(errCode, "SubdomainAccessAlreadyEnabled") ||
		strings.Contains(errCode, "subdomainAccessAlreadyEnabled"):
		return NewPlatformError("SUBDOMAIN_ALREADY_ENABLED", msg, "")
	case strings.Contains(errCode, "serviceStackSubdomainAccessAlreadyDisabled") ||
		strings.Contains(errCode, "ServiceStackSubdomainAccessAlreadyDisabled"):
		return NewPlatformError("SUBDOMAIN_ALREADY_DISABLED", msg, "")
	}

	if code >= 500 {
		return NewPlatformError(ErrAPIError, msg, "Zerops API error -- retry later")
	}

	return NewPlatformError(ErrAPIError, msg, "")
}
