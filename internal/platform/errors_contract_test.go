package platform

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/zeropsio/zerops-go/apiError"
)

// TA-06: the invariant this contract pins is simple — for any 4xx/5xx
// apiError.Error that carries a non-nil Meta, mapAPIError MUST expose
// that meta on the resulting PlatformError. The field-level detail the
// server provides is the only actionable information the LLM has to fix
// invalid input; dropping it on any branch re-opens F#7.
//
// The test covers every HTTP-status branch in mapAPIError (401, 403,
// 404/service, 404/process, 429, 5xx, 4xx-with-code, 4xx-without-code).
// A new branch that doesn't propagate Meta fails this test by
// construction. AST-level enforcement (as in TestNoInlineManagedRuntimeIndex)
// would be stricter but adds scaffolding cost; the runtime contract is
// sufficient because every apiError branch funnels through withAPICode.
func TestTA06_MapAPIError_PreservesMetaAcrossAllBranches(t *testing.T) {
	t.Parallel()

	// Sample meta reused by every case — assertion is "Meta came through"
	// not "Meta has the right content" (that's TestMapAPIError's job).
	sampleMeta := []interface{}{
		map[string]interface{}{
			"code":  "someError",
			"error": "Some error.",
			"metadata": map[string]interface{}{
				"field.path": []interface{}{"reason"},
			},
		},
	}
	wantDecoded := []APIMetaItem{
		{
			Code:  "someError",
			Error: "Some error.",
			Metadata: map[string][]string{
				"field.path": {"reason"},
			},
		},
	}

	branches := []struct {
		name       string
		status     int
		errCode    string
		entityType string
		wantCode   string
	}{
		{"401_auth", http.StatusUnauthorized, "tokenExpired", "", ErrAuthTokenExpired},
		{"403_permission", http.StatusForbidden, "noAccess", "", ErrPermissionDenied},
		{"404_process", http.StatusNotFound, "processNotFound", "process", ErrProcessNotFound},
		{"404_service", http.StatusNotFound, "serviceNotFound", "service", ErrServiceNotFound},
		{"429_rate_limited", http.StatusTooManyRequests, "rateLimited", "", ErrAPIRateLimited},
		{"500_server", 500, "internalError", "", ErrAPIError},
		{"502_gateway", 502, "badGateway", "", ErrAPIError},
		{"400_with_code", 400, "projectImportInvalidParameter", "", ErrAPIError},
		{"422_with_code", 422, "zeropsYamlInvalidParameter", "", ErrAPIError},
		{"400_without_code", 400, "", "", ErrAPIError},
	}

	for _, tc := range branches {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ae := apiError.Error{
				HttpStatusCode: tc.status,
				ErrorCode:      tc.errCode,
				Message:        "fixture",
				Meta:           sampleMeta,
			}
			err := mapAPIError(ae, tc.entityType)
			pe, ok := err.(*PlatformError)
			if !ok {
				t.Fatalf("expected *PlatformError, got %T", err)
			}
			if pe.Code != tc.wantCode {
				t.Errorf("branch routed wrong: Code = %q, want %q", pe.Code, tc.wantCode)
			}
			if !reflect.DeepEqual(pe.APIMeta, wantDecoded) {
				t.Errorf(
					"branch %q dropped APIMeta — F#7 regression\ngot:  %+v\nwant: %+v",
					tc.name, pe.APIMeta, wantDecoded,
				)
			}
		})
	}
}

// TA-06 corollary: branches with nil server meta MUST yield nil APIMeta
// (not an empty slice). Consumers rely on len(APIMeta) > 0 to decide
// whether to surface apiMeta in the MCP JSON response.
func TestTA06_MapAPIError_NilMetaStaysNil(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"401", http.StatusUnauthorized},
		{"403", http.StatusForbidden},
		{"404", http.StatusNotFound},
		{"429", http.StatusTooManyRequests},
		{"500", 500},
		{"400", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := mapAPIError(apiError.Error{
				HttpStatusCode: tc.status,
				ErrorCode:      "someCode",
				Message:        "msg",
				Meta:           nil,
			}, "")
			pe := err.(*PlatformError)
			if pe.APIMeta != nil {
				t.Errorf("APIMeta should be nil when server sent no meta, got %+v", pe.APIMeta)
			}
		})
	}
}
