// Tests for BuildIterationDelta — the escalating recovery template for
// deploy retries. Tiers: 1–2 DIAGNOSE, 3–4 systematic check, 5+ STOP.
package workflow

import (
	"strings"
	"testing"
)

func TestBuildIterationDelta_RemainingUsesMaxIterations(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}
	result := BuildIterationDelta("deploy", 1, plan, "some failure")
	// maxIterations() defaults to 5 (aligned with 3-tier STOP at 5), so remaining = 4 at iteration 1.
	if !strings.Contains(result, "session remaining: 4") {
		t.Errorf("expected session remaining=4 (maxIterations()-1), got: %s", result)
	}
}

func TestBuildIterationDelta_NoForceGuide(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}
	result := BuildIterationDelta("deploy", 1, plan, "some failure")
	if strings.Contains(result, "forceGuide") {
		t.Error("output should not contain 'forceGuide'")
	}
}

func TestBuildIterationDelta_ZeroIteration(t *testing.T) {
	t.Parallel()
	result := BuildIterationDelta("deploy", 0, nil, "")
	if result != "" {
		t.Error("expected empty for iteration 0")
	}
}

func TestBuildIterationDelta_NonDeployStep(t *testing.T) {
	t.Parallel()
	result := BuildIterationDelta("verify", 1, nil, "failed")
	if result != "" {
		t.Error("expected empty for non-deploy step")
	}
}

func TestBuildIterationDelta_DeployWithIteration(t *testing.T) {
	t.Parallel()
	plan := &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}
	result := BuildIterationDelta("deploy", 1, plan, "timeout on /status")
	if result == "" {
		t.Fatal("expected non-empty delta for deploy iteration > 0")
	}
	if !strings.Contains(result, "ITERATION 1") {
		t.Error("delta should contain iteration number")
	}
	if !strings.Contains(result, "timeout on /status") {
		t.Error("delta should contain last attestation")
	}
	if !strings.Contains(result, "DIAGNOSE") {
		t.Error("delta should contain DIAGNOSE guidance for iteration 1")
	}
}

func TestBuildIterationDelta_Escalation_Tier1(t *testing.T) {
	t.Parallel()
	for _, iter := range []int{1, 2} {
		result := BuildIterationDelta("deploy", iter, nil, "some failure")
		if !strings.Contains(result, "DIAGNOSE") {
			t.Errorf("iteration %d should contain DIAGNOSE (tier 1)", iter)
		}
	}
}

func TestBuildIterationDelta_Escalation_Tier2(t *testing.T) {
	t.Parallel()
	for _, iter := range []int{3, 4} {
		result := BuildIterationDelta("deploy", iter, nil, "some failure")
		if !strings.Contains(result, "Systematic check") {
			t.Errorf("iteration %d should contain 'Systematic check' (tier 2)", iter)
		}
		if !strings.Contains(result, "0.0.0.0") {
			t.Errorf("iteration %d should reference 0.0.0.0 binding check", iter)
		}
	}
}

func TestBuildIterationDelta_Escalation_Tier3(t *testing.T) {
	t.Parallel()
	for _, iter := range []int{5, 8} {
		result := BuildIterationDelta("deploy", iter, nil, "some failure")
		if !strings.Contains(result, "STOP") {
			t.Errorf("iteration %d should contain STOP (tier 3)", iter)
		}
		if !strings.Contains(result, "user") {
			t.Errorf("iteration %d should mention user involvement", iter)
		}
	}
}
