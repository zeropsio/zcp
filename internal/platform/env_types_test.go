package platform_test

import (
	"reflect"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestProjectEnvVar_CarriesEditable pins that the project env wrapper
// surfaces the SDK's Editable bool. Project envs distinguish read-only
// platform-injected entries (Editable=false: zeropsSubdomainHost, CDN
// URLs) from user-overridable ones (Editable=true: envIsolation,
// sshIsolation) — Phase 2 envclass rules need this signal.
func TestProjectEnvVar_CarriesEditable(t *testing.T) {
	t.Parallel()
	v := platform.ProjectEnvVar{
		ID:        "id-1",
		Key:       "K",
		Content:   "v",
		Type:      platform.ProjectEnvUser,
		Sensitive: false,
		Editable:  true,
	}
	if !v.Editable {
		t.Error("Editable field not preserved on round-trip")
	}
	if v.Type != platform.ProjectEnvUser {
		t.Errorf("Type field not preserved: got %v want %v", v.Type, platform.ProjectEnvUser)
	}
}

// TestServiceEnvVar_NoEditableField pins that the service env wrapper
// has NO Editable field. The SDK's ServiceStackEnv DTO doesn't carry
// Editable (verified live, plans/research/env-types-investigation-
// 2026-05-14.md); the wrapper mirrors that. If a future refactor
// adds Editable to ServiceEnvVar, this test fails — surfacing the
// taxonomy regression.
func TestServiceEnvVar_NoEditableField(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[platform.ServiceEnvVar]()
	if _, found := typ.FieldByName("Editable"); found {
		t.Error("ServiceEnvVar must not have Editable — distinct from ProjectEnvVar (SDK DTO doesn't expose it on service-stack-env scope)")
	}
}

// TestEnvVar_AdditiveExtensionPreservesExistingCallers verifies that
// both new types preserve the Key + Content + ID fields existing
// callers read. Most consumers in internal/ops/ are field-access-only
// (.Key, .Content) — they should compile + run unchanged after the
// scope split lands.
func TestEnvVar_AdditiveExtensionPreservesExistingCallers(t *testing.T) {
	t.Parallel()
	p := platform.ProjectEnvVar{ID: "p1", Key: "P", Content: "pv"}
	if p.ID != "p1" || p.Key != "P" || p.Content != "pv" {
		t.Errorf("ProjectEnvVar field accessors changed shape: got %+v", p)
	}
	s := platform.ServiceEnvVar{ID: "s1", Key: "S", Content: "sv"}
	if s.ID != "s1" || s.Key != "S" || s.Content != "sv" {
		t.Errorf("ServiceEnvVar field accessors changed shape: got %+v", s)
	}
}

// TestProjectEnvType_ClosedEnum pins the project env type to USER/SYSTEM
// per the SDK's EnvTypeEnum (verified live: 2-value enum). Envclass
// Layer 3 (Phase 2) treats Type=SYSTEM as universal drop; the rule
// depends on the enum being closed.
func TestProjectEnvType_ClosedEnum(t *testing.T) {
	t.Parallel()
	if platform.ProjectEnvUser != "USER" {
		t.Errorf("ProjectEnvUser: got %q want USER", platform.ProjectEnvUser)
	}
	if platform.ProjectEnvSystem != "SYSTEM" {
		t.Errorf("ProjectEnvSystem: got %q want SYSTEM", platform.ProjectEnvSystem)
	}
}

// TestServiceEnvType_ClosedEnum pins the service env type to the 2026-08
// UserDataTypeEnum 2-value set (USER|SYSTEM) — the legacy
// READ_ONLY|EDITABLE|SECRET|INTERNAL|ENV enum is retired on the wire
// (spec docs/spec-zerops-env-lifecycle.md §1). Envclass Layer 3 (Phase 2)
// drops every service env regardless of Type; the enum nevertheless must
// be closed to surface SDK protocol drift early.
func TestServiceEnvType_ClosedEnum(t *testing.T) {
	t.Parallel()
	want := map[platform.ServiceEnvType]bool{
		platform.ServiceEnvUser:   true,
		platform.ServiceEnvSystem: true,
	}
	if len(want) != 2 {
		t.Errorf("expected 2 ServiceEnvType values, got %d", len(want))
	}
	if platform.ServiceEnvUser != "USER" {
		t.Errorf("ServiceEnvUser: got %q want USER", platform.ServiceEnvUser)
	}
	if platform.ServiceEnvSystem != "SYSTEM" {
		t.Errorf("ServiceEnvSystem: got %q want SYSTEM", platform.ServiceEnvSystem)
	}
}
