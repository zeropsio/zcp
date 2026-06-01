package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestRenderStatus_ProdIntent_SignpostsGitPushSetup pins RC-C: when the develop
// intent signals production AND a required service has git-push unconfigured
// (the dev/stage direct-push state), the status surfaces that launch needs
// git-push-setup — early, not as a reactive wall at the prod boundary.
func TestRenderStatus_ProdIntent_SignpostsGitPushSetup(t *testing.T) {
	t.Parallel()
	mk := func(intent string, gp topology.GitPushState) string {
		return RenderStatus(Response{Envelope: StateEnvelope{
			Phase: PhaseDevelopActive,
			Services: []ServiceSnapshot{
				{Hostname: "blogdev", TypeVersion: "php-nginx@8.4", Mode: topology.ModeStandard, RuntimeClass: topology.RuntimeImplicitWeb, GitPushState: gp},
			},
			WorkSession: &WorkSessionSummary{Intent: intent, Services: []string{"blogdev"}},
		}})
	}
	// Prod intent + unconfigured → signpost present.
	out := mk("Laravel blog with dev/stage and production environments", topology.GitPushUnconfigured)
	low := strings.ToLower(out)
	if !strings.Contains(low, "git-push-setup") {
		t.Fatalf("prod intent + unconfigured must signpost git-push-setup:\n%s", out)
	}
	// No prod intent → no signpost (no friction for non-prod sessions).
	if strings.Contains(strings.ToLower(mk("redesign the homepage", topology.GitPushUnconfigured)), "git-push-setup") {
		t.Fatalf("non-prod intent must NOT signpost")
	}
	// Prod intent but git-push already configured → no signpost (already set up).
	if strings.Contains(strings.ToLower(mk("ship to production", topology.GitPushConfigured)), "git-push-setup") {
		t.Fatalf("configured git-push must NOT signpost")
	}
}
