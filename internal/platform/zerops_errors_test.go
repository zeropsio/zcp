// Tests for: plans/audit/04-error-translation-lossy.md § Fix 1
package platform

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zerops-go/apiError"
)

func TestMapAPIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		apiErr           apiError.Error
		entityType       string
		wantCode         string
		wantAPICode      string
		wantSuggContains string
	}{
		{
			name:        "401_Unauthorized",
			apiErr:      apiError.Error{HttpStatusCode: http.StatusUnauthorized, ErrorCode: "tokenExpired", Message: "token expired"},
			entityType:  "",
			wantCode:    ErrAuthTokenExpired,
			wantAPICode: "tokenExpired",
		},
		{
			name:        "403_Forbidden",
			apiErr:      apiError.Error{HttpStatusCode: http.StatusForbidden, ErrorCode: "noAccess", Message: "no access"},
			entityType:  "",
			wantCode:    ErrPermissionDenied,
			wantAPICode: "noAccess",
		},
		{
			name:        "404_Process",
			apiErr:      apiError.Error{HttpStatusCode: http.StatusNotFound, ErrorCode: "processNotFound", Message: "process not found"},
			entityType:  "process",
			wantCode:    ErrProcessNotFound,
			wantAPICode: "processNotFound",
		},
		{
			name:        "404_Service",
			apiErr:      apiError.Error{HttpStatusCode: http.StatusNotFound, ErrorCode: "serviceNotFound", Message: "not found"},
			entityType:  "service",
			wantCode:    ErrServiceNotFound,
			wantAPICode: "serviceNotFound",
		},
		{
			name:        "429_RateLimited",
			apiErr:      apiError.Error{HttpStatusCode: http.StatusTooManyRequests, ErrorCode: "rateLimited", Message: "too many requests"},
			entityType:  "",
			wantCode:    ErrAPIRateLimited,
			wantAPICode: "rateLimited",
		},
		{
			name:        "SubdomainAlreadyEnabled",
			apiErr:      apiError.Error{HttpStatusCode: 409, ErrorCode: "SubdomainAccessAlreadyEnabled", Message: "already enabled"},
			entityType:  "",
			wantCode:    "SUBDOMAIN_ALREADY_ENABLED",
			wantAPICode: "", // subdomain codes don't set APICode
		},
		{
			name:        "SubdomainAlreadyDisabled",
			apiErr:      apiError.Error{HttpStatusCode: 409, ErrorCode: "serviceStackSubdomainAccessAlreadyDisabled", Message: "already disabled"},
			entityType:  "",
			wantCode:    "SUBDOMAIN_ALREADY_DISABLED",
			wantAPICode: "",
		},
		{
			name:             "5xx_ServerError",
			apiErr:           apiError.Error{HttpStatusCode: 502, ErrorCode: "badGateway", Message: "bad gateway"},
			entityType:       "",
			wantCode:         ErrAPIError,
			wantAPICode:      "badGateway",
			wantSuggContains: "retry later",
		},
		{
			name:             "4xx_WithErrCode",
			apiErr:           apiError.Error{HttpStatusCode: 422, ErrorCode: "projectImportInvalidYaml", Message: "invalid yaml"},
			entityType:       "",
			wantCode:         ErrAPIError,
			wantAPICode:      "projectImportInvalidYaml",
			wantSuggContains: "projectImportInvalidYaml",
		},
		{
			name:             "4xx_WithoutErrCode",
			apiErr:           apiError.Error{HttpStatusCode: 400, ErrorCode: "", Message: "bad request"},
			entityType:       "",
			wantCode:         ErrAPIError,
			wantAPICode:      "",
			wantSuggContains: "Check the request parameters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := mapAPIError(tt.apiErr, tt.entityType)
			pe, ok := err.(*PlatformError)
			if !ok {
				t.Fatalf("expected *PlatformError, got %T: %v", err, err)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", pe.Code, tt.wantCode)
			}
			if pe.APICode != tt.wantAPICode {
				t.Errorf("APICode = %q, want %q", pe.APICode, tt.wantAPICode)
			}
			if tt.wantSuggContains != "" && !strings.Contains(pe.Suggestion, tt.wantSuggContains) {
				t.Errorf("Suggestion = %q, want it to contain %q", pe.Suggestion, tt.wantSuggContains)
			}
		})
	}
}

func TestMapSDKError_NonAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantCode    string
		wantAPICode string
	}{
		{
			name:        "ContextCanceled",
			err:         context.Canceled,
			wantCode:    ErrAPIError,
			wantAPICode: "",
		},
		{
			name:        "DeadlineExceeded",
			err:         context.DeadlineExceeded,
			wantCode:    ErrAPITimeout,
			wantAPICode: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := mapSDKError(tt.err, "")
			pe, ok := err.(*PlatformError)
			if !ok {
				t.Fatalf("expected *PlatformError, got %T: %v", err, err)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", pe.Code, tt.wantCode)
			}
			if pe.APICode != tt.wantAPICode {
				t.Errorf("APICode = %q, want %q (non-API errors should not have APICode)", pe.APICode, tt.wantAPICode)
			}
		})
	}
}
