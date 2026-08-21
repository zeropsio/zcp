// Tests for: internal/platform/zerops_env.go — service userData writes
// under the platform's 2026-08 model (docs/spec-zerops-env-lifecycle.md
// §1/§7): sensitive is a REQUIRED write-side flag the pinned SDK body
// doesn't carry, so CreateServiceEnvVar hand-rolls the POST on the SDK's
// own authorized transport (sdkBase.Post) instead of the SDK's generated
// per-endpoint handler.
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zerops-go/dto/input/body"
)

// TestSDKUserDataBody_StillLacksSensitive is a drift guard, not a red/green
// feature test: it passes BY DESIGN today (the pinned zerops-go SDK's
// generated body.UserDataPost has no Sensitive field), and is expected to
// keep passing until zerops-go ships the field. Its RED was verified once,
// manually, by temporarily inverting the assertion to require Sensitive to
// be present — confirmed to fail for the right reason (the field really is
// absent) — then reverted to this, the real guard. If this test ever FAILS
// in CI, the SDK gained the field: retire the hand-rolled POST in
// zerops_env.go and call the SDK's generated per-endpoint handler directly
// instead.
func TestSDKUserDataBody_StillLacksSensitive(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[body.UserDataPost]()
	if _, ok := typ.FieldByName("Sensitive"); ok {
		t.Fatal("zerops-go now carries Sensitive on UserDataPost — retire the hand-rolled POST in zerops_env.go and use the SDK handler")
	}
}

// ---------------------------------------------------------------------------
// CreateServiceEnvVar (hand-rolled POST /service-stack/{id}/user-data)
// ---------------------------------------------------------------------------

// TestCreateServiceEnvVar_SendsSensitiveInBody pins the wire shape: the
// hand-rolled POST hits the real platform path with a bearer-authorized
// request and a JSON body carrying sensitive PRESENT (never omitted, even
// when false — the platform requires the field on every write, spec §1).
func TestCreateServiceEnvVar_SendsSensitiveInBody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		sensitive bool
	}{
		{name: "sensitive true", sensitive: true},
		{name: "sensitive false", sensitive: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath, gotAuth string
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotAuth = r.Header.Get("Authorization")
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"proc-1","status":"PENDING","actionName":"stack.updateUserData"}`))
			}))
			t.Cleanup(srv.Close)

			z, err := NewZeropsClient("fake-token", srv.URL)
			if err != nil {
				t.Fatalf("NewZeropsClient: %v", err)
			}

			proc, err := z.CreateServiceEnvVar(context.Background(), "svc-1", "NODE_ENV", "production", tt.sensitive)
			if err != nil {
				t.Fatalf("CreateServiceEnvVar: %v", err)
			}

			if gotMethod != http.MethodPost {
				t.Errorf("method = %q, want POST", gotMethod)
			}
			if gotPath != "/api/rest/public/service-stack/svc-1/user-data" {
				t.Errorf("path = %q, want /api/rest/public/service-stack/svc-1/user-data", gotPath)
			}
			if gotAuth != "Bearer fake-token" {
				t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer fake-token")
			}
			if gotBody["key"] != "NODE_ENV" {
				t.Errorf("body key = %v, want NODE_ENV", gotBody["key"])
			}
			if gotBody["content"] != "production" {
				t.Errorf("body content = %v, want production", gotBody["content"])
			}
			gotSensitive, present := gotBody["sensitive"]
			if !present {
				t.Fatal(`body missing "sensitive" field`)
			}
			if gotSensitive != tt.sensitive {
				t.Errorf("body sensitive = %v, want %v", gotSensitive, tt.sensitive)
			}
			if proc == nil || proc.ID != "proc-1" {
				t.Errorf("CreateServiceEnvVar Process = %+v, want ID=proc-1", proc)
			}
		})
	}
}

// TestCreateServiceEnvVar_DuplicateKey_PreservesAPICode pins that a
// userDataDuplicateKey 400 (the key already exists — e.g. it's owned by a
// yaml-baked run.envVariables record) maps to a *PlatformError carrying the
// raw APICode, so ops.EnvSet can translate it into actionable guidance.
func TestCreateServiceEnvVar_DuplicateKey_PreservesAPICode(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"userDataDuplicateKey","message":"UserData key 'NODE_ENV' is not unique in service stack frame of reference.","meta":[{"error":"UserData key 'NODE_ENV' is not unique in service stack frame of reference.","code":"userDataDuplicateKey","metadata":null}]}}`))
	}))
	t.Cleanup(srv.Close)

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}

	_, err = z.CreateServiceEnvVar(context.Background(), "svc-1", "NODE_ENV", "production", true)
	if err == nil {
		t.Fatal("expected error for duplicate key")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.APICode != "userDataDuplicateKey" {
		t.Errorf("APICode = %q, want %q", pe.APICode, "userDataDuplicateKey")
	}
}

// TestUserDataWrite_MalformedResponses_ArePlatformErrorsWithoutBody pins the
// "never echo the raw response" rule (CLAUDE.md "Credentials are
// user-owned" trap): a response that fails to decode as JSON — on either
// the success (<300) or error (>=300) branch — must map to a generic
// PlatformError whose Cause records the decode failure, never the response
// bytes. A submitted credential value could be echoed back verbatim in a
// broken/proxied error body, so the raw bytes must never reach any
// PlatformError field or err.Error().
func TestUserDataWrite_MalformedResponses_ArePlatformErrorsWithoutBody(t *testing.T) {
	t.Parallel()
	const sentinel = "SENTINEL-SECRET-42"
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "200 non-JSON body", status: http.StatusOK, body: "not json " + sentinel},
		{name: "502 HTML body", status: http.StatusBadGateway, body: "<html><body>Bad Gateway " + sentinel + "</body></html>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			z, err := NewZeropsClient("fake-token", srv.URL)
			if err != nil {
				t.Fatalf("NewZeropsClient: %v", err)
			}

			_, err = z.CreateServiceEnvVar(context.Background(), "svc-1", "NODE_ENV", "production", true)
			if err == nil {
				t.Fatal("expected error for malformed response")
			}
			var pe *PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("expected *PlatformError, got %T: %v", err, err)
			}
			if pe.Code != ErrAPIError {
				t.Errorf("Code = %q, want %q", pe.Code, ErrAPIError)
			}

			if strings.Contains(err.Error(), sentinel) {
				t.Errorf("err.Error() leaked the raw response body: %v", err)
			}
			if strings.Contains(pe.Message, sentinel) {
				t.Errorf("PlatformError.Message leaked the raw response body: %q", pe.Message)
			}
			if strings.Contains(pe.Suggestion, sentinel) {
				t.Errorf("PlatformError.Suggestion leaked the raw response body: %q", pe.Suggestion)
			}
			if strings.Contains(pe.Diagnostic, sentinel) {
				t.Errorf("PlatformError.Diagnostic leaked the raw response body: %q", pe.Diagnostic)
			}
			if pe.Cause != nil && strings.Contains(pe.Cause.Error(), sentinel) {
				t.Errorf("PlatformError.Cause leaked the raw response body: %v", pe.Cause)
			}
		})
	}
}
