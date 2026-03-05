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
		{"too_long", strings.Repeat("a", 26), "invalid hostname"},
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
	rt := RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}
	got := rt.StageHostname()
	if got != "appstage" {
		t.Errorf("StageHostname: want %q, got %q", "appstage", got)
	}
}

func TestStageHostname_Simple(t *testing.T) {
	t.Parallel()
	rt := RuntimeTarget{DevHostname: "myapp", Type: "nodejs@22", Simple: true}
	got := rt.StageHostname()
	if got != "" {
		t.Errorf("StageHostname for Simple: want empty, got %q", got)
	}
}

func TestStageHostname_NoDevSuffix(t *testing.T) {
	t.Parallel()
	rt := RuntimeTarget{DevHostname: "myapp", Type: "nodejs@22"}
	got := rt.StageHostname()
	if got != "" {
		t.Errorf("StageHostname without 'dev' suffix: want empty, got %q", got)
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
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
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

func TestValidateBootstrapTargets_EmptyTargets_Error(t *testing.T) {
	t.Parallel()
	_, err := ValidateBootstrapTargets(nil, testLiveTypes, nil)
	if err == nil {
		t.Fatal("expected error for empty targets")
	}
	if !strings.Contains(err.Error(), "at least one target") {
		t.Errorf("error %q should mention 'at least one target'", err.Error())
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
	// "dev" suffix = 3 chars, stage suffix = 5 chars. Base needs to be 21 chars to make stage overflow (21+5=26>25).
	base := strings.Repeat("a", 21)
	targets := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: base + "dev", Type: "nodejs@22"}},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err == nil {
		t.Fatal("expected error for stage hostname overflow")
	}
	if !strings.Contains(err.Error(), "stage hostname") {
		t.Errorf("error %q should mention 'stage hostname'", err.Error())
	}
}

func TestValidateBootstrapTargets_StorageExcluded_FromEnvCheck(t *testing.T) {
	t.Parallel()
	// shared-storage is a managed storage type — should not require env var checks.
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
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
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			},
		},
		{
			Runtime: RuntimeTarget{DevHostname: "apidev", Type: "bun@1.2"},
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
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
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
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
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
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
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
			Runtime: RuntimeTarget{DevHostname: "myapp", Type: "nodejs@22", Simple: true},
		},
	}
	_, err := ValidateBootstrapTargets(targets, testLiveTypes, nil)
	if err != nil {
		t.Fatalf("unexpected error for simple mode: %v", err)
	}
}

func TestValidateBootstrapTargets_DuplicateHostname_Error(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
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

func TestValidateBootstrapTargets_ManagedModeDefault_NON_HA(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
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
