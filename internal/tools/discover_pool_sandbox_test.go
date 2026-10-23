package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

// A Swarm pool and a Zerops Box are neither adoptable nor managed
// dependencies (spec-workflows.md §8 PS-2): discover reports adoption state
// `pool` / `sandbox` with a reason saying it is not an app runtime, and no
// adopt warning is raised for them. Both are NOT infrastructure.
func TestClassifyAdoptionState_PoolAndSandbox(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		hostname   string
		typ        string
		wantState  ops.AdoptionState
		wantStateS string
		wantReason string
	}{
		{"pool", "pool", "swarm@1", ops.AdoptionPool, "pool", "a Swarm pool — its orchestrator hands out containers; deploy its image, never adopt it as an app's dev or stage"},
		{"pool vm", "poolvm", "swarm-vm@1", ops.AdoptionPool, "pool", "a Swarm pool — its orchestrator hands out containers; deploy its image, never adopt it as an app's dev or stage"},
		{"box", "box", "ubuntu/zbox@1", ops.AdoptionSandbox, "sandbox", "a Zerops Box — hosts sandboxes; not an app runtime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
			result := &ops.DiscoverResult{Services: []ops.ServiceInfo{
				{Hostname: tt.hostname, Type: tt.typ, IsInfrastructure: false},
				{Hostname: "app", Type: "nodejs@22", IsInfrastructure: false},
			}}

			enrichWithMetaStatus(result, stateDir, nil)

			svc := result.Services[0]
			if svc.AdoptionState != tt.wantState || string(svc.AdoptionState) != tt.wantStateS {
				t.Errorf("AdoptionState = %q, want %q", svc.AdoptionState, tt.wantStateS)
			}
			if svc.AdoptionReason != tt.wantReason {
				t.Errorf("AdoptionReason = %q, want %q", svc.AdoptionReason, tt.wantReason)
			}
			if svc.IsInfrastructure {
				t.Error("IsInfrastructure must stay false for a pool or Box")
			}
			if got := result.Services[1]; got.AdoptionState != ops.AdoptionAdoptable || got.AdoptionReason != "" {
				t.Errorf("app: state %q reason %q, want adoptable with no reason", got.AdoptionState, got.AdoptionReason)
			}
			for _, w := range result.Warnings {
				if strings.Contains(w, tt.hostname+",") || strings.HasSuffix(w, tt.hostname) || strings.Contains(w, ": "+tt.hostname+".") {
					t.Errorf("warning must not offer %s for adoption: %s", tt.hostname, w)
				}
			}
		})
	}

	// The zcp-self case keeps precedence over the new cases.
	if got, _ := classifyAdoptionState(&ops.ServiceInfo{Hostname: "zcp", Type: "zcp@1"}, nil, nil); got != ops.AdoptionZCPSelf {
		t.Errorf("zcp@1: got %q, want zcp-self", got)
	}
}
