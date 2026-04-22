// Tests for: BootstrapTarget types, StageHostname derivation, and ValidateBootstrapTargets.
package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestValidatePlanHostname(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		hostname string
		wantErr  string // empty = no error expected
	}{
		{"valid_lowercase", "appdev", ""},
		{"valid_with_digits", "app1dev2", ""},
		{"single_char", "a", ""},
		{"max_length_25", strings.Repeat("a", 25), ""},
		{"leading_digit", "3test", "invalid hostname"},
		{"all_digits", "123", "invalid hostname"},
		{"has_hyphen", "my-app", "invalid hostname"},
		{"has_underscore", "my_app", "invalid hostname"},
		{"has_uppercase", "AppDev", "invalid hostname"},
		{"too_long", strings.Repeat("a", 41), "invalid hostname"},
		{"empty", "", "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePlanHostname(tt.hostname)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestStageHostname_Standard(t *testing.T) {
	t.Parallel()
	// Phase B.4: ExplicitStage is the only source of a stage hostname —
	// the previous `{base}dev → {base}stage` auto-derivation was deleted
	// along with the broader hostname-suffix heuristic. Standard mode
	// without ExplicitStage returns empty (caught by
	// ValidateBootstrapTargets as a hard error).
	rt := RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"}
	if got := rt.StageHostname(); got != "appstage" {
		t.Errorf("StageHostname with ExplicitStage: want %q, got %q", "appstage", got)
	}

	rtMissing := RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}
	if got := rtMissing.StageHostname(); got != "" {
		t.Errorf("StageHostname without ExplicitStage: want empty (no heuristic), got %q", got)
	}
}

func TestStageHostname_Simple(t *testing.T) {
	t.Parallel()
	rt := RuntimeTarget{DevHostname: "myapp", Type: "nodejs@22", BootstrapMode: "simple"}
	got := rt.StageHostname()
	if got != "" {
		t.Errorf("StageHostname for simple mode: want empty, got %q", got)
	}
}

func TestStageHostname_Dev(t *testing.T) {
	t.Parallel()
	rt := RuntimeTarget{DevHostname: "myappdev", Type: "nodejs@22", BootstrapMode: "dev"}
	got := rt.StageHostname()
	if got != "" {
		t.Errorf("StageHostname for dev mode: want empty, got %q", got)
	}
}

func TestEffectiveMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mode Mode
		want Mode
	}{
		{"empty_defaults_to_standard", "", "standard"},
		{"explicit_standard", "standard", "standard"},
		{"dev_mode", "dev", "dev"},
		{"simple_mode", "simple", "simple"},
		{"invalid_mode_passes_through", "foobar", "foobar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := RuntimeTarget{DevHostname: "app", Type: "nodejs@22", BootstrapMode: tt.mode}
			if got := rt.EffectiveMode(); got != tt.want {
				t.Errorf("EffectiveMode: want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestValidateBootstrapTargets_InvalidBootstrapMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mode    Mode
		wantErr bool
	}{
		{"valid_empty", "", false},
		{"valid_standard", "standard", false},
		{"valid_dev", "dev", false},
		{"valid_simple", "simple", false},
		{"invalid_foobar", "foobar", true},
		{"invalid_STANDARD_uppercase", "STANDARD", true},
		{"invalid_mixed_case", "Dev", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hostname := "appdev"
			explicitStage := "appstage"
			if tt.mode == "simple" || tt.mode == "dev" {
				hostname = "myapp"
				explicitStage = "" // non-standard modes ignore ExplicitStage
			}
			targets := []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: hostname, Type: "nodejs@22", BootstrapMode: tt.mode, ExplicitStage: explicitStage}},
			}
			_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for mode %q, got nil", tt.mode)
				}
				if !strings.Contains(err.Error(), "invalid bootstrapMode") {
					t.Errorf("error %q should contain 'invalid bootstrapMode'", err.Error())
				}
			} else if err != nil {
				t.Fatalf("unexpected error for mode %q: %v", tt.mode, err)
			}
		})
	}
}

func TestStageHostname_NoExplicit(t *testing.T) {
	t.Parallel()
	// Standard mode without ExplicitStage returns empty — the suffix
	// heuristic is gone (phase B.4).
	rt := RuntimeTarget{DevHostname: "myapp", Type: "nodejs@22"}
	got := rt.StageHostname()
	if got != "" {
		t.Errorf("StageHostname without ExplicitStage: want empty, got %q", got)
	}
}

func TestStageHostname_ExplicitOverride(t *testing.T) {
	t.Parallel()
	rt := RuntimeTarget{DevHostname: "zmon", Type: "go@1", ExplicitStage: "zmonstage"}
	got := rt.StageHostname()
	if got != "zmonstage" {
		t.Errorf("StageHostname with explicit override: want %q, got %q", "zmonstage", got)
	}
}

func TestStageHostname_ExplicitUsedVerbatim(t *testing.T) {
	t.Parallel()
	// ExplicitStage is used as-is; no suffix heuristic interferes.
	rt := RuntimeTarget{DevHostname: "apidev", Type: "go@1", ExplicitStage: "apipreview"}
	got := rt.StageHostname()
	if got != "apipreview" {
		t.Errorf("ExplicitStage: want %q, got %q", "apipreview", got)
	}
}

func TestStageHostname_ExplicitIgnoredForNonStandard(t *testing.T) {
	t.Parallel()
	rt := RuntimeTarget{DevHostname: "app", Type: "go@1", BootstrapMode: "simple", ExplicitStage: "appstage"}
	got := rt.StageHostname()
	if got != "" {
		t.Errorf("explicit stage should be ignored for simple mode: want empty, got %q", got)
	}
}

func TestRuntimeBase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan *ServicePlan
		want string
	}{
		{"nil_plan", nil, ""},
		{"empty_targets", &ServicePlan{}, ""},
		{"nodejs_version", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"}},
		}}, "nodejs"},
		{"bun_no_version", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun"}},
		}}, "bun"},
		{"first_target_wins", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "go@1"}},
			{Runtime: RuntimeTarget{DevHostname: "apidev", Type: "nodejs@22"}},
		}}, "go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.plan.RuntimeBase(); got != tt.want {
				t.Errorf("RuntimeBase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDependencyTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan *ServicePlan
		want []string
	}{
		{"nil_plan", nil, nil},
		{"no_deps", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "go@1"}},
		}}, nil},
		{"single_dep", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "go@1"}, Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			}},
		}}, []string{"postgresql@16"}},
		{"deduplicates", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "go@1"}, Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			}},
			{Runtime: RuntimeTarget{DevHostname: "apidev", Type: "bun@1.2", ExplicitStage: "apistage"}, Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "SHARED"},
				{Hostname: "cache", Type: "valkey@7.2", Resolution: "CREATE"},
			}},
		}}, []string{"postgresql@16", "valkey@7.2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.plan.DependencyTypes()
			if len(got) != len(tt.want) {
				t.Fatalf("DependencyTypes() = %v, want %v", got, tt.want)
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("DependencyTypes()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

var testLiveTypes = []platform.ServiceStackType{
	{Name: "Node.js", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
		{Name: "nodejs@22", Status: "ACTIVE"},
	}},
	{Name: "Bun", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
		{Name: "bun@1.2", Status: "ACTIVE"},
	}},
	{Name: "PostgreSQL", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
		{Name: "postgresql@16", Status: "ACTIVE"},
	}},
	{Name: "Valkey", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
		{Name: "valkey@7.2", Status: "ACTIVE"},
	}},
	{Name: "Shared Storage", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
		{Name: "shared-storage", Status: "ACTIVE"},
	}},
	{Name: "Object Storage", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
		{Name: "object-storage", Status: "ACTIVE"},
	}},
}

func TestValidateBootstrapTargets_SingleTarget_Success(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			},
		},
	}
	defaulted, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// db should be defaulted to NON_HA
	if len(defaulted) != 1 || defaulted[0] != "db" {
		t.Errorf("defaulted: want [db], got %v", defaulted)
	}
}

func TestValidateBootstrapTargets_EmptyTargets_Allowed(t *testing.T) {
	t.Parallel()
	defaulted, err := ValidateBootstrapTargets(nil, testLiveTypes, nil)
	if err != nil {
		t.Fatalf("empty targets should be allowed (managed-only): %v", err)
	}
	if len(defaulted) != 0 {
		t.Errorf("defaulted should be empty for nil targets, got %v", defaulted)
	}
}

func TestValidateBootstrapTargets_InvalidHostname_Error(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "my-app", Type: "nodejs@22"}},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err == nil {
		t.Fatal("expected error for invalid hostname")
	}
	if !strings.Contains(err.Error(), "invalid hostname") {
		t.Errorf("error %q should contain 'invalid hostname'", err.Error())
	}
}

func TestValidateBootstrapTargets_StageHostnameOverflow_Error(t *testing.T) {
	t.Parallel()
	// Explicit stage hostname of 41 chars exceeds the 40-char platform limit.
	over := strings.Repeat("a", 41)
	targets := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: over}},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err == nil {
		t.Fatal("expected error for stage hostname overflow")
	}
	if !strings.Contains(err.Error(), "stageHostname") {
		t.Errorf("error %q should mention stageHostname", err.Error())
	}
}

func TestValidateBootstrapTargets_ExplicitStage_NoDevSuffix(t *testing.T) {
	t.Parallel()
	// Hostname "zmon" doesn't end in "dev" but explicit stageHostname is provided.
	targets := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "zmon", Type: "nodejs@22", ExplicitStage: "zmonstage"}},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err != nil {
		t.Fatalf("explicit stageHostname should allow non-dev hostnames: %v", err)
	}
}

func TestValidateBootstrapTargets_ExplicitStage_InvalidHostname(t *testing.T) {
	t.Parallel()
	// Explicit stage with invalid hostname should fail validation.
	targets := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "zmon", Type: "nodejs@22", ExplicitStage: "INVALID-STAGE"}},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err == nil {
		t.Fatal("expected error for invalid explicit stage hostname")
	}
}

func TestValidateBootstrapTargets_NoDevSuffix_NoExplicit_Error(t *testing.T) {
	t.Parallel()
	// Standard mode without dev suffix AND without explicit stage should fail.
	targets := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "zmon", Type: "nodejs@22"}},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err == nil {
		t.Fatal("expected error: no dev suffix and no explicit stage")
	}
	if !strings.Contains(err.Error(), "stageHostname") {
		t.Errorf("error should mention stageHostname field: %v", err)
	}
}

func TestValidateBootstrapTargets_StorageExcluded_FromEnvCheck(t *testing.T) {
	t.Parallel()
	// shared-storage is a managed storage type — should not require env var checks.
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
			Dependencies: []Dependency{
				{Hostname: "files", Type: "shared-storage", Resolution: "CREATE"},
			},
		},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBootstrapTargets_SharedResolution_Success(t *testing.T) {
	t.Parallel()
	// Two targets both reference "db" — the second with SHARED resolution.
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			},
		},
		{
			Runtime: RuntimeTarget{DevHostname: "apidev", Type: "bun@1.2", ExplicitStage: "apistage"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "SHARED"},
			},
		},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBootstrapTargets_SharedResolution_NoCreate_Error(t *testing.T) {
	t.Parallel()
	// SHARED dep references "db" but no target has CREATE for it.
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "SHARED"},
			},
		},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err == nil {
		t.Fatal("expected error for SHARED without CREATE")
	}
	if !strings.Contains(err.Error(), "SHARED") {
		t.Errorf("error %q should mention SHARED", err.Error())
	}
}

func TestValidateBootstrapTargets_CreateServiceExists_Error(t *testing.T) {
	t.Parallel()
	liveServices := []platform.ServiceStack{
		{Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	}
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			},
		},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, liveServices)
	if err == nil {
		t.Fatal("expected error for CREATE on existing service")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error %q should mention 'already exists'", err.Error())
	}
}

func TestValidateBootstrapTargets_ExistsServiceMissing_Error(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "EXISTS"},
			},
		},
	}
	// Empty live services — db doesn't exist.
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, []platform.ServiceStack{})
	if err == nil {
		t.Fatal("expected error for EXISTS on missing service")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should mention 'not found'", err.Error())
	}
}

func TestValidateBootstrapTargets_SimpleMode_NoStage(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "myapp", Type: "nodejs@22", BootstrapMode: "simple"},
		},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err != nil {
		t.Fatalf("unexpected error for simple mode: %v", err)
	}
}

func TestValidateBootstrapTargets_DevMode_NoStage(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "myappdev", Type: "nodejs@22", BootstrapMode: "dev"},
		},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err != nil {
		t.Fatalf("unexpected error for dev mode: %v", err)
	}
}

func TestValidateBootstrapTargets_MixedModes_Valid(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"}}, // standard (default)
		{Runtime: RuntimeTarget{DevHostname: "frontend", Type: "bun@1.2", BootstrapMode: "simple"}},   // simple
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err != nil {
		t.Fatalf("unexpected error for mixed modes: %v", err)
	}
}

func TestValidateBootstrapTargets_DuplicateHostname_Error(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
				{Hostname: "db", Type: "valkey@7.2", Resolution: "CREATE"},
			},
		},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err == nil {
		t.Fatal("expected error for duplicate hostname in dependencies")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q should mention 'duplicate'", err.Error())
	}
}

func TestValidateBootstrapTargets_UnknownType_Error(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "python@3.12"}},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should mention 'not found'", err.Error())
	}
}

func TestValidateBootstrapTargets_CaseInsensitiveResolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		resolution string
		wantUpper  string
	}{
		{"lowercase_create", "create", "CREATE"},
		{"mixed_case_exists", "Exists", "EXISTS"},
		{"lowercase_shared", "shared", "SHARED"},
		{"already_uppercase", "CREATE", "CREATE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Two targets so SHARED has a CREATE to reference.
			targets := []BootstrapTarget{
				{
					Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
					Dependencies: []Dependency{
						{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
					},
				},
				{
					Runtime: RuntimeTarget{DevHostname: "apidev", Type: "bun@1.2", ExplicitStage: "apistage"},
					Dependencies: []Dependency{
						{Hostname: "db", Type: "postgresql@16", Resolution: tt.resolution},
					},
				},
			}
			_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := targets[1].Dependencies[0].Resolution
			if got != tt.wantUpper {
				t.Errorf("resolution: want %q, got %q", tt.wantUpper, got)
			}
		})
	}
}

func TestValidateBootstrapTargets_CaseInsensitiveMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		mode      string
		wantUpper string
	}{
		{"lowercase_ha", "ha", "HA"},
		{"mixed_case_non_ha", "non_ha", "NON_HA"},
		{"already_uppercase", "HA", "HA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			targets := []BootstrapTarget{
				{
					Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
					Dependencies: []Dependency{
						{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE", Mode: tt.mode},
					},
				},
			}
			_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := targets[0].Dependencies[0].Mode
			if got != tt.wantUpper {
				t.Errorf("mode: want %q, got %q", tt.wantUpper, got)
			}
		})
	}
}

func TestValidateBootstrapTargets_ManagedModeDefault_NON_HA(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
				{Hostname: "cache", Type: "valkey@7.2", Mode: "HA", Resolution: "CREATE"},
			},
		},
	}
	defaulted, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// db should be defaulted, cache should not (already has HA).
	if len(defaulted) != 1 || defaulted[0] != "db" {
		t.Errorf("defaulted: want [db], got %v", defaulted)
	}
	// Verify mode was set.
	if targets[0].Dependencies[0].Mode != "NON_HA" {
		t.Errorf("db mode: want NON_HA, got %q", targets[0].Dependencies[0].Mode)
	}
}

func TestIsAllExisting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan *ServicePlan
		want bool
	}{
		{"nil_plan", nil, false},
		{"empty_targets", &ServicePlan{}, false},
		{"all_existing_all_exists", &ServicePlan{Targets: []BootstrapTarget{
			{
				Runtime:      RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true},
				Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@16", Resolution: "EXISTS"}},
			},
		}}, true},
		{"mixed_existing_and_new", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true}},
			{Runtime: RuntimeTarget{DevHostname: "apidev", Type: "go@1", IsExisting: false}},
		}}, false},
		{"existing_runtime_create_dep", &ServicePlan{Targets: []BootstrapTarget{
			{
				Runtime:      RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true},
				Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"}},
			},
		}}, false},
		{"existing_runtime_shared_dep", &ServicePlan{Targets: []BootstrapTarget{
			{
				Runtime:      RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true},
				Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@16", Resolution: "SHARED"}},
			},
		}}, false},
		{"existing_no_deps", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true}},
		}}, true},
		{"multiple_existing_multiple_exists_deps", &ServicePlan{Targets: []BootstrapTarget{
			{
				Runtime:      RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true},
				Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@16", Resolution: "EXISTS"}},
			},
			{
				Runtime:      RuntimeTarget{DevHostname: "apidev", Type: "go@1", IsExisting: true},
				Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@16", Resolution: "EXISTS"}},
			},
		}}, true},
		{"new_runtime_no_deps", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: false}},
		}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.plan.IsAllExisting()
			if got != tt.want {
				t.Errorf("IsAllExisting() = %v, want %v", got, tt.want)
			}
		})
	}
}

// RED phase test: ValidateBootstrapTargets should allow empty targets (managed-only projects)
func TestValidateBootstrapTargets_ManagedOnlyEmptyTargets(t *testing.T) {
	t.Parallel()
	// Managed-only project: zero runtime targets, only managed dependencies
	targets := []BootstrapTarget{}
	defaulted, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)

	if err != nil {
		t.Fatalf("ValidateBootstrapTargets with empty targets should not error: %v", err)
	}
	if len(defaulted) != 0 {
		t.Errorf("defaulted list should be empty for empty targets, got %d entries", len(defaulted))
	}
}

// P5': classic plan (isExisting=false) naming an already-live runtime
// hostname must fail early at plan-commit with a diagnostic that points
// at the adopt alternative.
func TestValidateBootstrapTargets_ClassicWithLiveRuntime_Rejected(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{
				DevHostname:   "fizzydev",
				Type:          "nodejs@22",
				BootstrapMode: "standard",
				ExplicitStage: "fizzystage",
			},
		},
	}
	live := []platform.ServiceStack{
		{Name: "fizzydev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, live)
	if err == nil {
		t.Fatal("expected error when classic plan hostname collides with live service")
	}
	for _, needle := range []string{"fizzydev", "exists", "adopt"} {
		if !strings.Contains(err.Error(), needle) {
			t.Errorf("error missing %q: %v", needle, err)
		}
	}
}

// P5': adopt plan (isExisting=true) referring to a non-existent runtime
// is rejected symmetrically.
func TestValidateBootstrapTargets_AdoptWithMissingRuntime_Rejected(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{
				DevHostname:   "fizzydev",
				Type:          "nodejs@22",
				BootstrapMode: "standard",
				IsExisting:    true,
				ExplicitStage: "fizzystage",
			},
		},
	}
	// Non-nil live set with a different hostname — triggers the
	// "isExisting but not found" branch. A nil live set would skip the
	// collision check entirely (used by test fixtures that don't care).
	live := []platform.ServiceStack{
		{Name: "other", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, live)
	if err == nil {
		t.Fatal("expected error when adopt plan hostname is not live")
	}
	for _, needle := range []string{"fizzydev", "isExisting", "not found"} {
		if !strings.Contains(err.Error(), needle) {
			t.Errorf("error missing %q: %v", needle, err)
		}
	}
}

// P5': stage hostname collision in a classic standard-mode plan follows
// the same rule as the dev hostname.
func TestValidateBootstrapTargets_ClassicWithLiveStage_Rejected(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{
				DevHostname:   "appdev",
				Type:          "nodejs@22",
				BootstrapMode: "standard",
				ExplicitStage: "appstage",
			},
		},
	}
	live := []platform.ServiceStack{
		{Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, live)
	if err == nil {
		t.Fatal("expected error when classic plan stage hostname collides with live service")
	}
	for _, needle := range []string{"appstage", "exists", "adopt"} {
		if !strings.Contains(err.Error(), needle) {
			t.Errorf("error missing %q: %v", needle, err)
		}
	}
}

// P5': happy path — classic plan, hostnames not live, validation passes.
func TestValidateBootstrapTargets_ClassicGreenfield_Success(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{
				DevHostname:   "appdev",
				Type:          "nodejs@22",
				BootstrapMode: "standard",
				ExplicitStage: "appstage",
			},
		},
	}
	live := []platform.ServiceStack{} // empty — greenfield
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, live)
	if err != nil {
		t.Fatalf("greenfield classic plan must pass: %v", err)
	}
}
