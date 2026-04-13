package workflow

import (
	"context"
	"testing"
)

// TestValidateFeatureSubagent covers the new attestation-enforcing validator
// wired to SubStepSubagent at the deploy step. v11 shipped a scaffold-quality
// frontend because the main agent autonomously decided step 4b was "already
// done" and never dispatched the feature sub-agent; the validator removes
// that autonomy — the step can only be marked complete via an attestation
// describing what the feature sub-agent did.
//
// The validator must:
//   - pass when the attestation is a meaningful description (>= 40 chars)
//   - fail when the attestation is missing (empty string)
//   - fail when the attestation is boilerplate or too short to describe work
//
// Length alone is not a perfect proxy, but it is a sharp proxy: v11's skip
// would have attested "already done" or similar, which is under 40 chars.
// The threshold forces the agent to name what was produced, which also
// makes human review of session logs usable ("feature sub-agent added
// styled dispatch form, typed Task interface, refresh button, task-count
// status badge in JobsSection.svelte").
func TestValidateFeatureSubagent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		attestation string
		wantPassed  bool
	}{
		{
			name:        "empty attestation fails",
			attestation: "",
			wantPassed:  false,
		},
		{
			name:        "short attestation fails",
			attestation: "already done",
			wantPassed:  false,
		},
		{
			name:        "boilerplate attestation fails",
			attestation: "dispatched sub-agent",
			wantPassed:  false,
		},
		{
			name:        "meaningful attestation passes",
			attestation: "feature sub-agent added styled JobsSection.svelte with typed Task interface, dispatch form, refresh button, and pending-task badge",
			wantPassed:  true,
		},
	}

	plan := &RecipePlan{Tier: RecipeTierShowcase}
	state := &RecipeState{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := validateFeatureSubagent(context.Background(), plan, state, tt.attestation)
			if result == nil {
				t.Fatal("validator returned nil")
			}
			if result.Passed != tt.wantPassed {
				t.Errorf("attestation %q: passed=%v, want %v. Issues: %v", tt.attestation, result.Passed, tt.wantPassed, result.Issues)
			}
		})
	}
}

// TestGetSubStepValidator_FeatureSubagent locks in the wiring: the
// SubStepSubagent constant must resolve to the new validator. Without this,
// the validator exists but is never called — regression-proofs the
// "MANDATORY" enforcement the v11 failure revealed.
func TestGetSubStepValidator_FeatureSubagent(t *testing.T) {
	t.Parallel()
	v := getSubStepValidator(SubStepSubagent)
	if v == nil {
		t.Fatal("getSubStepValidator(SubStepSubagent) returned nil — validator not wired")
	}
}
