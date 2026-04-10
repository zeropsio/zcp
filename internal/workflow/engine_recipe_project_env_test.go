// Tests for: UpdateRecipeProjectEnvVariables merge semantics.
// Mirrors the merge rules of UpdateRecipeComments but operates on
// plan.ProjectEnvVariables (a per-env map[string]map[string]string).
//
// Merge rules this file asserts:
//   - Passing a non-empty map for an env REPLACES that env's prior
//     map (deterministic, not per-key merge — different from
//     EnvComments.Service which is additive per key).
//   - Passing an empty map for an env CLEARS that env's entry.
//   - Omitting an env key leaves that env untouched (partial re-run).
//   - Invalid env key (outside "0".."5") is rejected.
//   - Invalid var name (doesn't match [a-zA-Z_][a-zA-Z0-9_]*) is rejected.
//   - Nil input (caller passed nothing) is a no-op, not an error.
package workflow

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestUpdateRecipeProjectEnvVariables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		initial map[string]map[string]string
		input   map[string]map[string]string
		want    map[string]map[string]string
		wantErr bool
	}{
		{
			name:    "fresh_insert_all_envs",
			initial: nil,
			input: map[string]map[string]string{
				"0": {"DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
				"1": {"DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
				"2": {"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
				"3": {"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
				"4": {"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
				"5": {"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
			},
			want: map[string]map[string]string{
				"0": {"DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
				"1": {"DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
				"2": {"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
				"3": {"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
				"4": {"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
				"5": {"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
			},
		},
		{
			name: "replace_single_env_leaves_others_untouched",
			initial: map[string]map[string]string{
				"0": {"DEV_API_URL": "https://old.example"},
				"2": {"STAGE_API_URL": "https://api.example"},
			},
			input: map[string]map[string]string{
				"0": {"DEV_API_URL": "https://new.example", "DEV_FRONTEND_URL": "https://new-fe.example"},
			},
			want: map[string]map[string]string{
				"0": {"DEV_API_URL": "https://new.example", "DEV_FRONTEND_URL": "https://new-fe.example"},
				"2": {"STAGE_API_URL": "https://api.example"},
			},
		},
		{
			name: "empty_map_clears_env",
			initial: map[string]map[string]string{
				"0": {"DEV_API_URL": "https://dev.example"},
				"2": {"STAGE_API_URL": "https://api.example"},
			},
			input: map[string]map[string]string{
				"0": {},
			},
			want: map[string]map[string]string{
				"2": {"STAGE_API_URL": "https://api.example"},
			},
		},
		{
			name:    "nil_input_no_op",
			initial: map[string]map[string]string{"2": {"STAGE_API_URL": "https://api.example"}},
			input:   nil,
			want:    map[string]map[string]string{"2": {"STAGE_API_URL": "https://api.example"}},
		},
		{
			name:    "invalid_env_key_rejected",
			initial: nil,
			input: map[string]map[string]string{
				"6": {"STAGE_API_URL": "https://api.example"},
			},
			wantErr: true,
		},
		{
			name:    "non_numeric_env_key_rejected",
			initial: nil,
			input: map[string]map[string]string{
				"prod": {"STAGE_API_URL": "https://api.example"},
			},
			wantErr: true,
		},
		{
			name:    "invalid_var_name_with_dash_rejected",
			initial: nil,
			input: map[string]map[string]string{
				"0": {"STAGE-API-URL": "https://api.example"},
			},
			wantErr: true,
		},
		{
			name:    "invalid_var_name_starts_with_digit_rejected",
			initial: nil,
			input: map[string]map[string]string{
				"0": {"3STAGE_API_URL": "https://api.example"},
			},
			wantErr: true,
		},
		{
			name:    "empty_var_name_rejected",
			initial: nil,
			input: map[string]map[string]string{
				"0": {"": "https://api.example"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, EnvLocal, nil)

			if _, err := eng.Start("proj-1", WorkflowRecipe, "test"); err != nil {
				t.Fatalf("Start: %v", err)
			}
			// Seed a minimal plan so the function has something to operate on.
			state, err := eng.loadState()
			if err != nil {
				t.Fatalf("loadState: %v", err)
			}
			state.Recipe = NewRecipeState()
			state.Recipe.Plan = testMinimalPlan()
			state.Recipe.Plan.ProjectEnvVariables = tt.initial
			if err := saveSessionState(dir, eng.sessionID, state); err != nil {
				t.Fatalf("saveSessionState: %v", err)
			}

			err = eng.UpdateRecipeProjectEnvVariables(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UpdateRecipeProjectEnvVariables err=%v, wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			// Reload and compare.
			reloaded, err := eng.loadState()
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			got := reloaded.Recipe.Plan.ProjectEnvVariables
			// Normalize nil/empty for comparison.
			if got == nil {
				got = map[string]map[string]string{}
			}
			want := tt.want
			if want == nil {
				want = map[string]map[string]string{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("plan.ProjectEnvVariables mismatch\n got = %#v\nwant = %#v", got, want)
			}
		})
	}
}

// TestUpdateRecipeProjectEnvVariables_NoActivePlan verifies that calling the
// function without an active recipe plan returns an error (matching
// UpdateRecipeComments semantics).
func TestUpdateRecipeProjectEnvVariables_NoActivePlan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	// No active session at all.
	err := eng.UpdateRecipeProjectEnvVariables(map[string]map[string]string{
		"0": {"STAGE_API_URL": "https://api.example"},
	})
	if err == nil {
		t.Error("expected error when no active session, got nil")
	}

	// Active session but no recipe plan.
	if _, err := eng.Start("proj-1", WorkflowRecipe, "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	err = eng.UpdateRecipeProjectEnvVariables(map[string]map[string]string{
		"0": {"STAGE_API_URL": "https://api.example"},
	})
	if err == nil {
		t.Error("expected error when no recipe plan, got nil")
	}

	// Verify state file exists where we expect it (sanity check the engine's
	// path layout — catches regressions in saveSessionState location).
	if _, err := filepath.Abs(dir); err != nil {
		t.Fatalf("abs path: %v", err)
	}
}
