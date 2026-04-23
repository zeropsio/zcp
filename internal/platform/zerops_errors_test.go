// Tests for: plans/audit/04-error-translation-lossy.md § Fix 1
package platform

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zerops-go/apiError"
)

// TestMapAPIError is intentionally large — it pins the full
// HTTP-status × meta-shape truth table. Splitting it into per-branch
// functions loses the "one fixture set, one decoder" invariant the
// test enforces. Lint exception for maintainability index is
// warranted.
//
//nolint:maintidx // broad-coverage table is the point
func TestMapAPIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		apiErr           apiError.Error
		entityType       string
		wantCode         string
		wantAPICode      string
		wantSuggContains string
		wantAPIMeta      []APIMetaItem
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
		// --- APIMeta plumbing: field-level detail from API reaches PlatformError ---
		// Live API shape captured in plans/api-validation-plumbing.md §1.2/§1.3.
		{
			name: "APIMeta_projectImportInvalidParameter_single_field",
			apiErr: apiError.Error{
				HttpStatusCode: 400,
				ErrorCode:      "projectImportInvalidParameter",
				Message:        "Invalid parameter provided.",
				Meta: []any{
					map[string]any{
						"code":  "projectImportInvalidParameter",
						"error": "Invalid parameter provided.",
						"metadata": map[string]any{
							"storage.mode": []any{"mode not supported"},
						},
					},
				},
			},
			wantCode:         ErrAPIError,
			wantAPICode:      "projectImportInvalidParameter",
			wantSuggContains: "apiMeta",
			wantAPIMeta: []APIMetaItem{
				{
					Code:  "projectImportInvalidParameter",
					Error: "Invalid parameter provided.",
					Metadata: map[string][]string{
						"storage.mode": {"mode not supported"},
					},
				},
			},
		},
		{
			name: "APIMeta_projectImportMissingParameter",
			apiErr: apiError.Error{
				HttpStatusCode: 400,
				ErrorCode:      "projectImportMissingParameter",
				Message:        "Mandatory parameter is missing.",
				Meta: []any{
					map[string]any{
						"code":  "projectImportMissingParameter",
						"error": "Mandatory parameter is missing.",
						"metadata": map[string]any{
							"parameter": []any{"db.mode"},
						},
					},
				},
			},
			wantCode:         ErrAPIError,
			wantAPICode:      "projectImportMissingParameter",
			wantSuggContains: "apiMeta",
			wantAPIMeta: []APIMetaItem{
				{
					Code:  "projectImportMissingParameter",
					Error: "Mandatory parameter is missing.",
					Metadata: map[string][]string{
						"parameter": {"db.mode"},
					},
				},
			},
		},
		{
			name: "APIMeta_errorList_multi_item",
			apiErr: apiError.Error{
				HttpStatusCode: 400,
				ErrorCode:      "errorList",
				Message:        "See metadata",
				Meta: []any{
					map[string]any{
						"code":  "zeropsYamlInvalidParameter",
						"error": "Invalid parameter provided.",
						"metadata": map[string]any{
							"build.base": []any{"unknown base nodejs@99"},
							"build.os":   []any{"unknown os "},
						},
					},
					map[string]any{
						"code":  "zeropsYamlInvalidParameter",
						"error": "Invalid parameter provided.",
						"metadata": map[string]any{
							"run.base": []any{"nodejs@99"},
							"run.os":   []any{""},
						},
					},
				},
			},
			wantCode:         ErrAPIError,
			wantAPICode:      "errorList",
			wantSuggContains: "apiMeta",
			wantAPIMeta: []APIMetaItem{
				{
					Code:  "zeropsYamlInvalidParameter",
					Error: "Invalid parameter provided.",
					Metadata: map[string][]string{
						"build.base": {"unknown base nodejs@99"},
						"build.os":   {"unknown os "},
					},
				},
				{
					Code:  "zeropsYamlInvalidParameter",
					Error: "Invalid parameter provided.",
					Metadata: map[string][]string{
						"run.base": {"nodejs@99"},
						"run.os":   {""},
					},
				},
			},
		},
		{
			name: "APIMeta_serviceStackTypeNotFound",
			apiErr: apiError.Error{
				HttpStatusCode: 400,
				ErrorCode:      "serviceStackTypeNotFound",
				Message:        "Service stack Type not found.",
				Meta: []any{
					map[string]any{
						"code":  "serviceStackTypeNotFound",
						"error": "Service stack Type not found.",
						"metadata": map[string]any{
							"serviceStackTypeVersion": []any{"nodejs@99"},
						},
					},
				},
			},
			wantCode:    ErrAPIError,
			wantAPICode: "serviceStackTypeNotFound",
			wantAPIMeta: []APIMetaItem{
				{
					Code:  "serviceStackTypeNotFound",
					Error: "Service stack Type not found.",
					Metadata: map[string][]string{
						"serviceStackTypeVersion": {"nodejs@99"},
					},
				},
			},
		},
		{
			name: "APIMeta_nil_meta_preserved_as_nil",
			apiErr: apiError.Error{
				HttpStatusCode: 400,
				ErrorCode:      "projectImportInvalidParameter",
				Message:        "Invalid parameter provided.",
				Meta:           nil,
			},
			wantCode:    ErrAPIError,
			wantAPICode: "projectImportInvalidParameter",
			wantAPIMeta: nil,
		},
		{
			name: "APIMeta_item_without_metadata_map",
			apiErr: apiError.Error{
				HttpStatusCode: 400,
				ErrorCode:      "serviceStackNameInvalid",
				Message:        "Service stack name is invalid.",
				Meta: []any{
					map[string]any{
						"code":     "serviceStackNameInvalid",
						"error":    "Service stack name is invalid.",
						"metadata": nil,
					},
				},
			},
			wantCode:    ErrAPIError,
			wantAPICode: "serviceStackNameInvalid",
			wantAPIMeta: []APIMetaItem{
				{
					Code:  "serviceStackNameInvalid",
					Error: "Service stack name is invalid.",
				},
			},
		},
		{
			name: "APIMeta_malformed_shape_returns_nil_not_panic",
			apiErr: apiError.Error{
				HttpStatusCode: 400,
				ErrorCode:      "projectImportInvalidParameter",
				Message:        "Invalid parameter provided.",
				Meta:           "unexpected string instead of array", // garbage
			},
			wantCode:    ErrAPIError,
			wantAPICode: "projectImportInvalidParameter",
			wantAPIMeta: nil,
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
			if !reflect.DeepEqual(pe.APIMeta, tt.wantAPIMeta) {
				t.Errorf("APIMeta = %+v, want %+v", pe.APIMeta, tt.wantAPIMeta)
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
