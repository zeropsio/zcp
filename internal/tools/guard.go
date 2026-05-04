package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// RecipeSessionProbe is satisfied by *recipe.Store. Abstracting keeps
// internal/tools free of a direct recipe-package import while letting
// the workflow-context guard accept an active v3 recipe session as
// valid context.
//
// CurrentSingleSession lets a v2-shaped tool (zerops_record_fact,
// zerops_workspace_manifest) route into the recipe's outputRoot when
// the agent is in a recipe-only context — ok=false when zero or >1
// recipe sessions are open, so ambiguity becomes an explicit error
// instead of a guessed write.
//
// CoversHost lets the deploy-adoption gate (requireAdoption) skip the
// bootstrap-adoption check when an open recipe session owns the deploy
// target. A recipe authoring session legitimately deploys its own
// `apistage` / `appdev` cross-targets before any bootstrap workflow
// exists; without this exemption, the gate would refuse every
// recipe cross-deploy.
type RecipeSessionProbe interface {
	HasAnySession() bool
	CurrentSingleSession() (slug, legacyFactsPath, manifestPath string, ok bool)
	CoversHost(host string) bool
}

// requireWorkflowContext checks that the agent is in an active workflow:
// either a bootstrap / develop session, an open work session for the
// current process, or a live v3 recipe session. Used by mount + import
// to ensure the agent has received knowledge before performing
// infrastructure operations.
//
// recipeProbe may be nil in tests that don't exercise the recipe path.
func requireWorkflowContext(engine *workflow.Engine, stateDir string, recipeProbe RecipeSessionProbe) *mcp.CallToolResult {
	if engine != nil && engine.HasActiveSession() {
		return nil
	}
	if ws, _ := workflow.CurrentWorkSession(stateDir); ws != nil && ws.ClosedAt == "" {
		return nil
	}
	if recipeProbe != nil && recipeProbe.HasAnySession() {
		return nil
	}
	return convertError(platform.NewPlatformError(
		platform.ErrWorkflowRequired,
		"No active workflow. This tool requires a workflow context.",
		"Start a workflow: zerops_recipe action=\"start\" (recipe authoring), "+
			"zerops_workflow action=\"start\" workflow=\"bootstrap\" (create/adopt infrastructure), "+
			"or zerops_workflow action=\"start\" workflow=\"develop\" (develop/deploy/fix).",
	))
}

// requireAdoption checks that all given hostnames are tracked by ServiceMeta
// (bootstrapped or adopted). Returns nil if all are known.
//
// Skips the check when:
//   - stateDir is empty (no state directory configured)
//   - services/ directory doesn't exist (no bootstrap ever ran — gate activates
//     once the first service is adopted, giving a clean migration path)
//   - recipeProbe.CoversHost(h) — an open recipe session's Plan owns the
//     hostname; recipe authoring legitimately deploys its own dev/stage slots
//     before any bootstrap workflow exists. recipeProbe may be nil; the
//     exemption is opt-in and narrow to this gate (requireWorkflowContext
//     uses the same probe but for a different purpose).
func requireAdoption(stateDir string, recipeProbe RecipeSessionProbe, hostnames ...string) *mcp.CallToolResult {
	if stateDir == "" {
		return nil
	}
	// Gate activates only after first bootstrap creates the services directory.
	servicesDir := filepath.Join(stateDir, "services")
	if _, err := os.Stat(servicesDir); os.IsNotExist(err) {
		return nil
	}
	for _, h := range hostnames {
		if h == "" {
			continue
		}
		if workflow.IsKnownService(stateDir, h) {
			continue
		}
		// Recipe-authoring exemption: an open recipe session whose Plan
		// owns this hostname satisfies the gate. Pre-fix, run-24
		// surfaced the failure mode — the recipe cross-deploy
		// `apidev → apistage` returned SERVICE_NOT_FOUND because no
		// bootstrap had ever adopted `apistage`.
		if recipeProbe != nil && recipeProbe.CoversHost(h) {
			continue
		}
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Service %q is not adopted by ZCP — deploy blocked", h),
			fmt.Sprintf("Adopt it first: zerops_workflow action=\"start\" workflow=\"bootstrap\" (with isExisting=true for %s)", h),
		))
	}
	return nil
}
