// Tests for: deploy_strategy_gate.go — shared validation and mode gating
// between the SSH and local deploy variants.
package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
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
		{"retired push-dev alias → reject", "push-dev", true, platform.ErrInvalidParameter},
		{"manual → reject (declaration, not a dispatch value)", "manual", true, platform.ErrInvalidParameter},
		{"zcli → reject (internal label, not a tool argument)", "zcli", true, platform.ErrInvalidParameter},
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
			// O5 fix (round-3 audit): the "zcli" rejection must redirect to
			// "omit the strategy parameter" rather than handing back a
			// generic "Invalid strategy" — atom prose and DeployAttempt
			// records both surface "zcli" so an agent guessing this value
			// must learn the actual mapping. Assert on Message + Suggestion
			// since the redirect lives in Suggestion (PlatformError.Error()
			// is Message-only).
			if tt.strategy == "zcli" {
				if !strings.Contains(pe.Message, "internal label") {
					t.Errorf("zcli-rejection Message missing 'internal label'; got: %s", pe.Message)
				}
				for _, want := range []string{"Omit the strategy parameter", `Strategy: "zcli"`} {
					if !strings.Contains(pe.Suggestion, want) {
						t.Errorf("zcli-rejection Suggestion missing %q; got: %s", want, pe.Suggestion)
					}
				}
			}
			// "gopher" is the unknown-value path; the redirect must mention
			// "zcli" so the same agent who tries `strategy="zcli"` from a
			// typo'd path also gets pointed at the omit-the-parameter form.
			if tt.strategy == "gopher" && !strings.Contains(pe.Suggestion, `"zcli" is the internal label`) {
				t.Errorf("unknown-value rejection Suggestion should mention zcli internal label; got: %s", pe.Suggestion)
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
			name: "local-only + empty strategy (default zcli push) → reject",
			meta: &workflow.ServiceMeta{
				Hostname: "myproject", Mode: topology.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
			},
			strategy: "",
			wantErr:  true,
		},
		{
			name: "local-only + git-push → accept (git doesn't need a stage)",
			meta: &workflow.ServiceMeta{
				Hostname: "myproject", Mode: topology.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
			},
			strategy: "git-push",
			wantErr:  false,
		},
		{
			name: "local-stage + default → accept",
			meta: &workflow.ServiceMeta{
				Hostname: "myproject", StageHostname: "apistage", Mode: topology.PlanModeLocalStage, BootstrappedAt: "2026-04-01",
			},
			strategy: "",
			wantErr:  false,
		},
		{
			name: "container dev + default → accept",
			meta: &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-04-01",
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
