package platform

import (
	"context"
	"testing"
)

// TestClassifyAppVersionUserData_NewModel_UserIsRunEnvSystemIsIntrinsic pins the 2026-08 classifier (the
// single classifier shared by the real client + mock) against the model
// docs/spec-zerops-env-lifecycle.md §1 describes: app-version userDataList
// records are typed USER (yaml-baked run.envVariables, editable:false) or
// SYSTEM (intrinsics) — the legacy READ_ONLY|EDITABLE|SECRET|INTERNAL|ENV
// enum is retired on the wire. ZEROPS_YAML is dropped by key regardless of
// Type; unknown/empty Type is fail-safe intrinsic (never admitted as a
// yaml-baked ref target).
func TestClassifyAppVersionUserData_NewModel_UserIsRunEnvSystemIsIntrinsic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		key      string
		typeStr  string
		wantKind userDataKind
	}{
		{"USER run var", "FOO", "USER", kindRunEnvVariable},
		{"SYSTEM intrinsic", "zeropsSubdomain", "SYSTEM", kindIntrinsic},
		{"empty Type → intrinsic (fail-safe)", "MYSTERY", "", kindIntrinsic},
		{"unknown Type → intrinsic (fail-safe)", "WHATEVER", "FUTURE_TYPE", kindIntrinsic},
		{"ZEROPS_YAML dropped by key even when USER-typed", "ZEROPS_YAML", "USER", kindZeropsYaml},
		{"ZEROPS_YAML dropped by key even when SYSTEM-typed", "ZEROPS_YAML", "SYSTEM", kindZeropsYaml},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kind := classifyAppVersionUserData(tt.key, tt.typeStr)
			if kind != tt.wantKind {
				t.Errorf("kind = %d, want %d", kind, tt.wantKind)
			}
		})
	}
}

// TestGetAppVersionUserData_NewModel_ReturnsUserOnly pins F7/E5/E6 under the
// new model: the mock shares the real classifier, so it returns ONLY
// USER-typed yaml-baked records (SYSTEM intrinsics + the ZEROPS_YAML blob
// filtered out), with Sensitive always false — the SDK's AppVersionUserData
// DTO carries no Sensitive field at all (spec §1), so nothing derives it —
// and the returned Type is the closed ServiceEnvUser value. A test cannot
// model a shape the real API can't produce.
func TestGetAppVersionUserData_NewModel_ReturnsUserOnly(t *testing.T) {
	t.Parallel()
	mock := NewMock().WithAppVersionUserData("av1", []ServiceEnvVar{
		{Key: "FOO", Content: "bar", Type: ServiceEnvUser},                          // yaml-baked run var
		{Key: "hostname", Content: "api", Type: ServiceEnvSystem},                   // intrinsic → filtered
		{Key: "ZEROPS_YAML", Content: "build:\n  os: ubuntu", Type: ServiceEnvUser}, // blob → filtered by key
	})
	got, err := mock.GetAppVersionUserData(context.Background(), "av1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d run vars, want 1 (FOO): %+v", len(got), got)
	}
	if got[0].Key != "FOO" {
		t.Errorf("Key = %q, want FOO", got[0].Key)
	}
	if got[0].Type != ServiceEnvUser {
		t.Errorf("Type = %q, want %q", got[0].Type, ServiceEnvUser)
	}
	if got[0].Sensitive {
		t.Error("Sensitive must be false — the app-version DTO carries no Sensitive field to derive it from")
	}
}
