// Tests for: deploy_strategy_gate.go — shared validation and mode gating
// between the SSH and local deploy variants.
package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestValidateDeployStrategyParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		strategy string
		wantErr  bool
		wantCode string
	}{
		{"empty → accept (default push)", "", false, ""},
		{"git-push → accept", "git-push", false, ""},
		{"push-dev → accept", "push-dev", false, ""},
		{"manual → reject (declaration, not a dispatch value)", "manual", true, platform.ErrInvalidParameter},
		{"unknown → reject", "gopher", true, platform.ErrInvalidParameter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateDeployStrategyParam(tt.strategy)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantErr {
				return
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("error is not *platform.PlatformError: %T", err)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", pe.Code, tt.wantCode)
			}
			if tt.strategy == "manual" && !strings.Contains(pe.Error(), "ServiceMeta declaration") {
				t.Errorf("manual-rejection message should explain ServiceMeta scope; got: %s", pe.Error())
			}
		})
	}
}

func TestCheckLocalOnlyGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		meta     *workflow.ServiceMeta
		strategy string
		wantErr  bool
	}{
		{
			name: "local-only + empty strategy (default push-dev) → reject",
			meta: &workflow.ServiceMeta{
				Hostname: "myproject", Mode: workflow.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
			},
			strategy: "",
			wantErr:  true,
		},
		{
			name: "local-only + explicit push-dev → reject",
			meta: &workflow.ServiceMeta{
				Hostname: "myproject", Mode: workflow.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
			},
			strategy: "push-dev",
			wantErr:  true,
		},
		{
			name: "local-only + git-push → accept (git doesn't need a stage)",
			meta: &workflow.ServiceMeta{
				Hostname: "myproject", Mode: workflow.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
			},
			strategy: "git-push",
			wantErr:  false,
		},
		{
			name: "local-stage + push-dev → accept",
			meta: &workflow.ServiceMeta{
				Hostname: "myproject", StageHostname: "apistage", Mode: workflow.PlanModeLocalStage, BootstrappedAt: "2026-04-01",
			},
			strategy: "",
			wantErr:  false,
		},
		{
			name: "container dev + push-dev → accept",
			meta: &workflow.ServiceMeta{
				Hostname: "appdev", Mode: workflow.PlanModeDev, BootstrappedAt: "2026-04-01",
			},
			strategy: "",
			wantErr:  false,
		},
		{
			name:     "no meta → no opinion (earlier gates handle it)",
			meta:     nil,
			strategy: "",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			target := "target"
			if tt.meta != nil {
				target = tt.meta.Hostname
				if err := workflow.WriteServiceMeta(dir, tt.meta); err != nil {
					t.Fatalf("WriteServiceMeta: %v", err)
				}
			}
			err := checkLocalOnlyGate(dir, target, tt.strategy)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantErr {
				return
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("error is not *platform.PlatformError: %T", err)
			}
			if !strings.Contains(pe.Message, "local-only") {
				t.Errorf("message should mention local-only; got: %s", pe.Message)
			}
			// Suggestion carries the resolution paths.
			if !strings.Contains(pe.Suggestion, "adopt-local") {
				t.Errorf("suggestion should point at adopt-local subaction as one of the options; got: %s", pe.Suggestion)
			}
			if !strings.Contains(pe.Suggestion, "git-push") {
				t.Errorf("suggestion should offer git-push as the other option; got: %s", pe.Suggestion)
			}
		})
	}
}
