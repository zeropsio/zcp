// Tests for: errwire.go — ErrorWire DTO + CheckWire/RecoveryHint
// composition + ErrorOption helpers.

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestConvertError_WithRecoveryStatus_AttachesHint(t *testing.T) {
	t.Parallel()
	pe := platform.NewPlatformError(platform.ErrSessionNotFound, "no session", "")

	result := convertError(pe, WithRecoveryStatus())

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getResultText(t, result)), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	rec, ok := parsed["recovery"].(map[string]any)
	if !ok {
		t.Fatalf("recovery field missing or wrong shape: %+v", parsed)
	}
	if rec["tool"] != "zerops_workflow" {
		t.Errorf("recovery.tool = %q, want zerops_workflow", rec["tool"])
	}
	if rec["action"] != "status" {
		t.Errorf("recovery.action = %q, want status", rec["action"])
	}
}

func TestConvertError_NoRecovery_OmitsField(t *testing.T) {
	t.Parallel()
	pe := platform.NewPlatformError(platform.ErrInvalidParameter, "bad input", "fix it")

	result := convertError(pe)

	text := getResultText(t, result)
	if strings.Contains(text, `"recovery"`) {
		t.Errorf("recovery key present without WithRecoveryStatus: %s", text)
	}
}

func TestConvertError_WithChecks_PreflightShape(t *testing.T) {
	t.Parallel()
	pe := platform.NewPlatformError(platform.ErrPreflightFailed, "preflight failed", "")
	checks := []workflow.StepCheck{
		{Name: "zerops_yml_exists", Status: "fail", Detail: "missing"},
	}

	result := convertError(pe, WithChecks("preflight", checks))

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getResultText(t, result)), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	checksAny, ok := parsed["checks"].([]any)
	if !ok {
		t.Fatalf("checks field missing or wrong shape: %+v", parsed)
	}
	if len(checksAny) != 1 {
		t.Fatalf("checks length = %d, want 1", len(checksAny))
	}
	c := checksAny[0].(map[string]any)
	if c["kind"] != "preflight" {
		t.Errorf("checks[0].kind = %q, want preflight", c["kind"])
	}
	if c["name"] != "zerops_yml_exists" {
		t.Errorf("checks[0].name = %q, want zerops_yml_exists", c["name"])
	}
	if c["status"] != "fail" {
		t.Errorf("checks[0].status = %q, want fail", c["status"])
	}
	if c["detail"] != "missing" {
		t.Errorf("checks[0].detail = %q, want missing", c["detail"])
	}
}

func TestCheckWire_PreservesRunnableContract(t *testing.T) {
	t.Parallel()
	pe := platform.NewPlatformError(platform.ErrPreflightFailed, "preflight failed", "")
	checks := []workflow.StepCheck{
		{
			Name:         "service_exists",
			Status:       "pass",
			Detail:       "appdev exists",
			PreAttestCmd: `zcli vpn up && curl -sf "$ZEROPS_API/projects/$PROJECT/services" | jq '.[].name'`,
			ExpectedExit: 0,
		},
	}

	result := convertError(pe, WithChecks("preflight", checks))

	text := getResultText(t, result)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c := parsed["checks"].([]any)[0].(map[string]any)
	if c["preAttestCmd"] == nil || c["preAttestCmd"] == "" {
		t.Errorf("preAttestCmd dropped — runnable contract lost: %s", text)
	}
	// expectedExit: 0 will be omitted by Go JSON omitempty for int zero,
	// which is correct (zero is the default). Pin a non-zero example via
	// a separate StepCheck to verify non-zero round-trips.
	checks2 := []workflow.StepCheck{{Name: "x", Status: "fail", PreAttestCmd: "false", ExpectedExit: 1}}
	result2 := convertError(pe, WithChecks("preflight", checks2))
	var parsed2 map[string]any
	_ = json.Unmarshal([]byte(getResultText(t, result2)), &parsed2)
	c2 := parsed2["checks"].([]any)[0].(map[string]any)
	if c2["expectedExit"] == nil {
		t.Errorf("expectedExit non-zero dropped: %s", getResultText(t, result2))
	}
}

func TestErrorWire_NeverCarriesEnvelopeOrPlan(t *testing.T) {
	t.Parallel()
	// Pin P4 contract: error responses NEVER carry envelope or plan,
	// only the optional recovery hint pointing at status. This test
	// catches accidental field additions during future refactors.
	pe := platform.NewPlatformError(platform.ErrSessionNotFound, "x", "y")
	result := convertError(pe, WithRecoveryStatus(), WithChecks("preflight", []workflow.StepCheck{
		{Name: "n", Status: "fail"},
	}))
	text := getResultText(t, result)

	for _, forbidden := range []string{`"envelope"`, `"plan"`, `"nextAtoms"`, `"guidance"`} {
		if strings.Contains(text, forbidden) {
			t.Errorf("ErrorWire must not carry %s — P4 contract violated:\n%s", forbidden, text)
		}
	}
}

func TestErrorWire_AlwaysHasCodeAndError(t *testing.T) {
	t.Parallel()
	// Schema invariant: code + error always present even when caller
	// passes a bare PlatformError with empty fields.
	pe := &platform.PlatformError{Code: platform.ErrAPIError, Message: "msg"}

	result := convertError(pe)

	var parsed map[string]any
	_ = json.Unmarshal([]byte(getResultText(t, result)), &parsed)
	if parsed["code"] == nil || parsed["code"] == "" {
		t.Errorf("code missing: %+v", parsed)
	}
	if parsed["error"] == nil || parsed["error"] == "" {
		t.Errorf("error missing: %+v", parsed)
	}
}

func TestConvertError_PreservesPlatformError(t *testing.T) {
	t.Parallel()
	// Typed PlatformError input survives untouched in code/error/suggestion.
	pe := platform.NewPlatformError("MY_CODE", "my message", "my hint")
	result := convertError(pe)

	var parsed map[string]any
	_ = json.Unmarshal([]byte(getResultText(t, result)), &parsed)
	if parsed["code"] != "MY_CODE" {
		t.Errorf("code drift: %v", parsed["code"])
	}
	if parsed["error"] != "my message" {
		t.Errorf("error drift: %v", parsed["error"])
	}
	if parsed["suggestion"] != "my hint" {
		t.Errorf("suggestion drift: %v", parsed["suggestion"])
	}
}
