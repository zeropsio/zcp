// Tests for: deploy guidance resolver — extracts strategy-specific sections from deploy.md.
package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDeployGuidance_Strategy_ReturnsSection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		strategy     string
		wantContains string
	}{
		{
			"push_dev_strategy",
			StrategyPushDev,
			"Push-Dev Deploy Strategy",
		},
		{
			"ci_cd_strategy",
			StrategyCICD,
			"CI/CD Deploy Strategy",
		},
		{
			"manual_strategy",
			StrategyManual,
			"Manual Deploy Strategy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeTestMeta(t, stateDir, "myapp", tt.strategy)

			got := ResolveDeployGuidance(stateDir, "myapp")
			if got == "" {
				t.Fatal("expected non-empty guidance")
			}
			if !containsStr(got, tt.wantContains) {
				t.Errorf("guidance should contain %q, got: %s", tt.wantContains, got)
			}
		})
	}
}

func TestResolveDeployGuidance_NoMeta_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()

	got := ResolveDeployGuidance(stateDir, "nonexistent")
	if got != "" {
		t.Errorf("expected empty string for missing meta, got: %q", got)
	}
}

func TestResolveDeployGuidance_NoStrategy_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	// Write meta without deploy strategy decision.
	writeTestMeta(t, stateDir, "myapp", "")

	got := ResolveDeployGuidance(stateDir, "myapp")
	if got != "" {
		t.Errorf("expected empty string for missing strategy, got: %q", got)
	}
}

func TestResolveDeployGuidance_UnknownStrategy_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeTestMeta(t, stateDir, "myapp", "unknown-strategy")

	got := ResolveDeployGuidance(stateDir, "myapp")
	if got != "" {
		t.Errorf("expected empty string for unknown strategy, got: %q", got)
	}
}

// writeTestMeta creates a service meta file with the given strategy.
func writeTestMeta(t *testing.T, stateDir, hostname, strategy string) {
	t.Helper()
	meta := &ServiceMeta{
		Hostname:         hostname,
		DeployStrategy:   strategy,
		BootstrapSession: "test-session",
		BootstrappedAt:   "2026-01-01T00:00:00Z",
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "services"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write service meta: %v", err)
	}
}

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
