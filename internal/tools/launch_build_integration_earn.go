package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// earnProbeDeps bundles the live-read dependencies the build-integration
// earn probe needs. The gate threads its own client/sshDeployer/rt/projectID;
// tests stub the whole probe via buildIntegrationEarnProbe instead of
// faking these.
type earnProbeDeps struct {
	client      platform.Client
	sshDeployer ops.SSHDeployer
	rt          runtime.Info
	projectID   string
}

// actionsWorkflowFilePath is the repo-relative path of the GitHub Actions
// workflow file the build-integration handler instructs the agent to
// commit. The earn probe stats exactly this path — single owner with the
// handler's workflowFile.path response field.
const actionsWorkflowFilePath = ".github/workflows/zerops.yml"

// buildIntegrationEarnProbe is the live earn check for a DECLARED
// build-integration. Returns (earned, evidence). Indirected so gate tests
// stub it without SSH/API fakes; production default is
// readBuildIntegrationEarn.
//
//nolint:gochecknoglobals // test-injection point, initialized var
var buildIntegrationEarnProbe = readBuildIntegrationEarn

// readBuildIntegrationEarn verifies the declared integration against a
// checkable signal:
//
//   - actions: the workflow file exists in the push-source working tree
//     (container: SSH test -f under /var/www; local: stat under CWD).
//     The caller MUST sequence this after the push-proof checks (clean
//     tree + local HEAD == remote HEAD) — only then does file-present
//     mean file-on-the-pushed-HEAD. Secrets presence is NOT checkable
//     from here; evidence wording keeps that honest.
//   - webhook: the platform integration-status read on the build target
//     returns configured (the dashboard OAuth leaves no git artifact;
//     the platform IS the checkable source).
//
// Returns earned=false with a short reason when the signal is absent or
// unreadable — the gate then renders declared-but-unverified (warn),
// never an error.
func readBuildIntegrationEarn(ctx context.Context, deps earnProbeDeps, pushHost string, meta *workflow.ServiceMeta) (bool, string) {
	switch meta.BuildIntegration {
	case topology.BuildIntegrationActions:
		return earnActionsWorkflowFile(ctx, deps, pushHost)
	case topology.BuildIntegrationWebhook:
		return earnWebhookIntegrationStatus(ctx, deps, meta)
	case topology.BuildIntegrationNone:
		return false, "no integration declared"
	default:
		return false, "no integration declared"
	}
}

// earnActionsWorkflowFile stats the Actions workflow file in the push
// source's working tree. Trustworthy only after push-proof (caller's
// sequencing responsibility).
func earnActionsWorkflowFile(ctx context.Context, deps earnProbeDeps, pushHost string) (bool, string) {
	if deps.rt.InContainer {
		if deps.sshDeployer == nil {
			return false, "workflow-file check unavailable (no SSH deployer)"
		}
		cmd := fmt.Sprintf(`test -f %s && echo present || echo absent`,
			quoteShellLiteral("/var/www/"+actionsWorkflowFilePath))
		out, err := deps.sshDeployer.ExecSSH(ctx, pushHost, cmd)
		if err != nil {
			return false, fmt.Sprintf("workflow-file check failed (ssh: %v)", err)
		}
		if strings.TrimSpace(string(out)) == "present" {
			return true, "workflow file " + actionsWorkflowFilePath + " present at the pushed HEAD (secrets are verified only by the first CI run)"
		}
		return false, "workflow file " + actionsWorkflowFilePath + " not found in the push-source working tree"
	}
	wd, err := os.Getwd()
	if err != nil {
		return false, fmt.Sprintf("workflow-file check failed (getwd: %v)", err)
	}
	if _, statErr := os.Stat(filepath.Join(wd, actionsWorkflowFilePath)); statErr == nil {
		return true, "workflow file " + actionsWorkflowFilePath + " present at the pushed HEAD (secrets are verified only by the first CI run)"
	}
	return false, "workflow file " + actionsWorkflowFilePath + " not found in the working directory"
}

// earnWebhookIntegrationStatus reads the platform's integration status on
// the BUILD TARGET (the stage half for a standard pair — the service the
// dashboard webhook was wired to).
func earnWebhookIntegrationStatus(ctx context.Context, deps earnProbeDeps, meta *workflow.ServiceMeta) (bool, string) {
	if deps.client == nil || deps.projectID == "" {
		return false, "integration-status check unavailable (no platform client)"
	}
	buildHost, _ := anticipatedBuildTarget(meta)
	if buildHost == "" {
		buildHost = meta.Hostname
	}
	svc, err := ops.LookupService(ctx, deps.client, deps.projectID, buildHost)
	if err != nil {
		return false, fmt.Sprintf("integration-status check failed (lookup %s: %v)", buildHost, err)
	}
	status, err := deps.client.GetServiceStackIntegrationStatus(ctx, svc.ID)
	if err != nil {
		return false, fmt.Sprintf("integration-status check failed (%v)", err)
	}
	if status.State == platform.IntegrationConfigured {
		return true, fmt.Sprintf("platform reports a configured %s integration on %s", status.Provider, buildHost)
	}
	return false, "platform reports no repository integration on " + buildHost + " — the dashboard OAuth step has not completed"
}

// buildIntegrationVerificationNote renders the agent-facing verified-state
// line for the build-integration confirm/noop responses. Single owner of
// the declared-vs-verified wording on the handler surface.
func buildIntegrationVerificationNote(meta *workflow.ServiceMeta) string {
	if meta.BuildIntegrationVerifiedAt != "" {
		return "verified at " + meta.BuildIntegrationVerifiedAt + " — a checkable signal confirmed the integration exists (actions: workflow file on the pushed HEAD; webhook: platform integration status). Secrets are exercised only by the first CI run."
	}
	return "DECLARED ONLY — the choice is recorded, but ZCP has not yet confirmed the integration exists. It verifies at launch time (publish-side gate): actions = workflow file present on the pushed HEAD; webhook = platform integration-status read on the build target. Finish the steps in nextStep."
}
