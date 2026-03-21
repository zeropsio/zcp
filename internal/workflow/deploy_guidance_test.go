package workflow

import (
	"testing"
)

// TestResolveDeployStepGuidance_DeployStep_WithStrategy tests that deploy step guidance includes strategy sections.
func TestResolveDeployStepGuidance_DeployStep_WithStrategy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		mode     string
		strategy string
		want     string
	}{
		{
			"standard_push_dev",
			PlanModeStandard,
			StrategyPushDev,
			"Push-Dev Deploy Strategy",
		},
		{
			"dev_ci_cd",
			PlanModeDev,
			StrategyCICD,
			"CI/CD Deploy Strategy",
		},
		{
			"simple_manual",
			PlanModeSimple,
			StrategyManual,
			"Manual Deploy Strategy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			guidance := resolveDeployStepGuidance(DeployStepDeploy, tt.mode, tt.strategy)

			if guidance == "" {
				t.Fatal("expected non-empty guidance")
			}
			// Should contain both mode-specific and strategy-specific content.
			if !containsStr(guidance, "deploy-execute") && !containsStr(guidance, "Deploy") {
				t.Errorf("guidance missing mode content, got: %s", guidance)
			}
			if !containsStr(guidance, tt.want) {
				t.Errorf("guidance missing strategy content %q, got: %s", tt.want, guidance)
			}
		})
	}
}

// TestResolveDeployStepGuidance_PrepareStep_IgnoresStrategy tests that prepare step doesn't use strategy.
func TestResolveDeployStepGuidance_PrepareStep_IgnoresStrategy(t *testing.T) {
	t.Parallel()
	guidance := resolveDeployStepGuidance(DeployStepPrepare, PlanModeStandard, StrategyPushDev)

	if guidance == "" {
		t.Fatal("expected non-empty guidance")
	}
	// Prepare step should not contain strategy-specific content.
	if containsStr(guidance, "Push-Dev Deploy Strategy") {
		t.Errorf("prepare step should not contain strategy content, got: %s", guidance)
	}
}

// containsStr is a helper to avoid importing strings in test.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsIndex(s, sub))
}

func containsIndex(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
