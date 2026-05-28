package platform

import (
	"context"
	"testing"
)

// TestClassifyAppVersionUserData pins the RC1 Type-allowlist classifier (the
// single classifier shared by the real client + mock). Live-verified 2026-05-28:
// run.envVariables are Type ENV (+ SECRET for baked secrets); intrinsics are
// READ_ONLY/INTERNAL/EDITABLE (never ENV); ZEROPS_YAML is dropped by key.
func TestClassifyAppVersionUserData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		key           string
		typeStr       string
		wantKind      userDataKind
		wantSensitive bool
	}{
		{"ENV run var", "FOO", "ENV", kindRunEnvVariable, false},
		{"SECRET run var → sensitive", "API_KEY", "SECRET", kindRunEnvVariable, true},
		{"READ_ONLY intrinsic", "zeropsSubdomain", "READ_ONLY", kindIntrinsic, false},
		{"INTERNAL intrinsic", "workingDir", "INTERNAL", kindIntrinsic, false},
		{"EDITABLE intrinsic (PATH)", "PATH", "EDITABLE", kindIntrinsic, false},
		{"ZEROPS_YAML dropped by key even when ENV-typed", "ZEROPS_YAML", "ENV", kindZeropsYaml, false},
		{"empty Type → intrinsic (fail-safe)", "MYSTERY", "", kindIntrinsic, false},
		{"unknown Type → intrinsic (fail-safe)", "WHATEVER", "FUTURE_TYPE", kindIntrinsic, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kind, sensitive := classifyAppVersionUserData(tt.key, tt.typeStr)
			if kind != tt.wantKind {
				t.Errorf("kind = %d, want %d", kind, tt.wantKind)
			}
			if sensitive != tt.wantSensitive {
				t.Errorf("sensitive = %v, want %v", sensitive, tt.wantSensitive)
			}
		})
	}
}

// TestGetAppVersionUserData_MockFiltersAndDerives pins F7/E5/E6: the mock shares
// the real classifier, so it returns ONLY run.envVariables, derives Sensitive
// from Type (never a fabricated Sensitive the real client can't produce), and
// filters intrinsic-typed + ZEROPS_YAML records. A test cannot model a shape
// the real API can't produce.
func TestGetAppVersionUserData_MockFiltersAndDerives(t *testing.T) {
	t.Parallel()
	mock := NewMock().WithAppVersionUserData("av1", []ServiceEnvVar{
		{Key: "FOO", Content: "bar"},                                       // bare → ENV run var
		{Key: "API_KEY", Content: "s", Type: "SECRET"},                     // SECRET run var
		{Key: "zeropsSubdomain", Content: "https://x", Type: "READ_ONLY"},  // intrinsic → filtered
		{Key: "ZEROPS_YAML", Content: "build:\n  os: ubuntu", Type: "ENV"}, // blob → filtered by key
		{Key: "LIES", Content: "v", Sensitive: true},                       // fabricated Sensitive (no Type)
	})
	got, err := mock.GetAppVersionUserData(context.Background(), "av1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sens := map[string]bool{}
	for _, v := range got {
		sens[v.Key] = v.Sensitive
	}
	if _, ok := sens["zeropsSubdomain"]; ok {
		t.Error("intrinsic READ_ONLY var must be filtered out of yaml-baked")
	}
	if _, ok := sens["ZEROPS_YAML"]; ok {
		t.Error("ZEROPS_YAML blob must be filtered out of yaml-baked")
	}
	if len(got) != 3 {
		t.Fatalf("got %d run vars, want 3 (FOO, API_KEY, LIES): %+v", len(got), got)
	}
	if !sens["API_KEY"] {
		t.Error("SECRET-typed API_KEY must derive Sensitive=true")
	}
	if sens["FOO"] {
		t.Error("ENV FOO must be Sensitive=false")
	}
	if sens["LIES"] {
		t.Error("a hand-set Sensitive:true on a non-SECRET seed must be IGNORED (Sensitive derives from Type)")
	}
}
