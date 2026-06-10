// Tests for: plans/audit/04-error-translation-lossy.md § Fix 1
package platform

import (
	"context"
	"errors"
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
			wantSuggContains: "'storage.mode' (mode not supported)",
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
			wantSuggContains: "'db.mode' (missing)",
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
			wantSuggContains: "'build.base' (unknown base nodejs@99)",
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

// TestFormatAPIMetaActionable pins the contract that 4xx suggestion
// text expands apiMeta inline rather than pointing at the structured
// block. Pre-2026-05-06 the suggestion was "see apiMeta..." and an
// out-of-band atom (`develop-api-error-meta`) taught the apiMeta
// shape; the atom was deleted in the same change that introduced
// this expansion. Wire shape (apiMeta JSON array) is preserved for
// programmatic consumers; suggestion is now the actionable summary.
func TestFormatAPIMetaActionable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta []APIMetaItem
		want string
	}{
		{
			name: "single_field_single_reason",
			meta: []APIMetaItem{
				{Code: "x", Metadata: map[string][]string{"storage.mode": {"mode not supported"}}},
			},
			want: "Field 'storage.mode' (mode not supported) rejected. Fix in YAML and retry.",
		},
		{
			name: "parameter_special_case_flips_to_field_with_missing_reason",
			meta: []APIMetaItem{
				{Code: "x", Metadata: map[string][]string{"parameter": {"db.mode"}}},
			},
			want: "Field 'db.mode' (missing) rejected. Fix in YAML and retry.",
		},
		{
			name: "multi_field_sorted_deterministic",
			meta: []APIMetaItem{
				{Code: "x", Metadata: map[string][]string{
					"build.base": {"unknown base nodejs@99"},
					"build.os":   {"unknown os "},
				}},
			},
			want: "Rejected fields: 'build.base' (unknown base nodejs@99), 'build.os' (unknown os ). Fix in YAML and retry.",
		},
		{
			name: "empty_reason_omits_parens",
			meta: []APIMetaItem{
				{Code: "x", Metadata: map[string][]string{"run.os": {""}}},
			},
			want: "Field 'run.os' rejected. Fix in YAML and retry.",
		},
		{
			name: "multi_reason_joined_semicolon",
			meta: []APIMetaItem{
				{Code: "x", Metadata: map[string][]string{"port": {"required", "must be > 0"}}},
			},
			want: "Field 'port' (required; must be > 0) rejected. Fix in YAML and retry.",
		},
		{
			name: "no_metadata_falls_back_to_pointer",
			meta: []APIMetaItem{{Code: "x", Error: "y"}},
			want: "The platform flagged specific fields — see apiMeta for each field's failure reason.",
		},
		{
			name: "over_cap_falls_back_with_count",
			meta: []APIMetaItem{
				{Metadata: map[string][]string{
					"a": {"r"}, "b": {"r"}, "c": {"r"},
					"d": {"r"}, "e": {"r"}, "f": {"r"},
				}},
			},
			want: "The platform flagged 6 fields — see apiMeta for each field's failure reason.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatAPIMetaActionable(tt.meta)
			if got != tt.want {
				t.Errorf("formatAPIMetaActionable\n got:  %q\n want: %q", got, tt.want)
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

// TestMapSDKError_PreservesCause pins C10: non-API errors must remain
// distinguishable through the PlatformError wrap via errors.Is. Without
// Unwrap()/Cause a user-initiated context.Canceled mid-poll was
// indistinguishable by code from a real platform failure (both ErrAPIError).
func TestMapSDKError_PreservesCause(t *testing.T) {
	t.Parallel()

	if mapped := mapSDKError(context.Canceled, ""); !errors.Is(mapped, context.Canceled) {
		t.Errorf("errors.Is(mapped, context.Canceled) = false; want true (Unwrap/Cause lost)")
	}
	if mapped := mapSDKError(context.DeadlineExceeded, ""); !errors.Is(mapped, context.DeadlineExceeded) {
		t.Errorf("errors.Is(mapped, context.DeadlineExceeded) = false; want true")
	}
}
