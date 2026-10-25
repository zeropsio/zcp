package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
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
	if ws, _ := workflow.CurrentWorkSession(stateDir); workflow.IsOpen(stateDir, ws) {
		return nil
	}
	if recipeProbe != nil && recipeProbe.HasAnySession() {
		return nil
	}
	return convertError(platform.NewPlatformError(
		platform.ErrWorkflowRequired,
		"No active workflow. This tool requires a workflow context.",
		"Start a workflow: zerops_workflow action=\"start\" workflow=\"bootstrap\" (create/adopt infrastructure), "+
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
//   - h is the alias of a lease zcp holds a record for (ops.LeaseTarget): a
//     pool container is no service to adopt (docs/spec-workflows.md §8 DP-1),
//     and the pool of such a lease named beside it is the deploy target of a
//     lease-to-pool deploy (PS-2 keeps a pool from being adopted)
//   - recipeProbe.CoversHost(h) — an open recipe session's Plan owns the
//     hostname; recipe authoring legitimately deploys its own dev/stage slots
//     before any bootstrap workflow exists. recipeProbe may be nil; the
//     exemption is opt-in and narrow to this gate (requireWorkflowContext
//     uses the same probe but for a different purpose).
//
// rt drives env-aware recovery: in local env the rejection points the
// agent at zerops_workflow action="adopt-local" (the local recovery
// primitive); in container env it keeps the bootstrap suggestion. Pre-
// Phase-12 the message hard-coded bootstrap, sending local-mode agents
// down the wrong recovery branch.
func requireAdoption(stateDir string, rt runtime.Info, recipeProbe RecipeSessionProbe, hostnames ...string) *mcp.CallToolResult {
	if stateDir == "" {
		return nil
	}
	// Gate activates only after first bootstrap creates the services directory.
	servicesDir := filepath.Join(stateDir, "services")
	if _, err := os.Stat(servicesDir); os.IsNotExist(err) {
		return nil
	}
	leasePools := leasePoolsAmong(stateDir, hostnames)
	for _, h := range hostnames {
		if h == "" {
			continue
		}
		if workflow.IsKnownService(stateDir, h) {
			continue
		}
		if _, isLease := leaseOf(stateDir, h); isLease || leasePools[h] {
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
		var (
			suggestion string
			recovery   *RecoveryHint
		)
		if rt.InContainer {
			suggestion = "Adopt it first: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\"."
			recovery = &topology.Recovery{
				Tool:   "zerops_workflow",
				Action: "start",
				Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
			}
		} else {
			suggestion = fmt.Sprintf("Local projects link a Zerops runtime via zerops_workflow action=\"adopt-local\" targetService=%q.", h)
			recovery = &topology.Recovery{
				Tool:   "zerops_workflow",
				Action: "adopt-local",
				Args:   map[string]string{"targetService": h},
			}
		}
		// ErrAdoptRequired (not ErrServiceNotFound): the hostname exists but
		// isn't adopted. discover's adoptable warning already promises
		// ADOPT_REQUIRED here, and SERVICE_NOT_FOUND routes the agent into
		// hostname-typo recovery instead of the adopt flow.
		return convertError(platform.NewPlatformError(
			platform.ErrAdoptRequired,
			fmt.Sprintf("Service %q is not adopted by ZCP — deploy blocked", h),
			suggestion,
		), WithRecovery(recovery))
	}
	return nil
}

// leaseOf is the lease host names, when host is the alias of a lease zcp holds
// a record for. An unreadable record set is no lease: the caller falls back to
// the service rules it had before leases.
// isLease reports whether host is an alias of a lease zcp holds.
func isLease(stateDir, host string) bool {
	_, ok := leaseOf(stateDir, host)
	return ok
}

func leaseOf(stateDir, host string) (ops.LeaseTargetInfo, bool) {
	lease, ok, err := ops.LeaseTarget(stateDir, host)
	if err != nil || !ok {
		return ops.LeaseTargetInfo{}, false
	}
	return lease, true
}

// leaseVouch asks a lease's pool orchestrator, once, whether the container
// the lease names is still the lease's (docs/spec-workflows.md §8 DP-2): the
// lease as it is to be reached (parked when its container is stopped), or the
// refusal of one that is no longer zcp's or that the orchestrator does not
// answer about.
type leaseVouch func(ctx context.Context, lease ops.LeaseTargetInfo, what string) (ops.LeaseTargetInfo, error)

// orchestratorVouch is the leaseVouch that asks through swarm
// (ops.LeaseTargetChecked); nil swarm vouches for no lease: the call is
// refused, zcp cannot tell whose the container is.
func orchestratorVouch(client platform.Client, projectID, stateDir string, swarm ops.SwarmFactory) leaseVouch {
	return func(ctx context.Context, lease ops.LeaseTargetInfo, what string) (ops.LeaseTargetInfo, error) {
		checked, ok, err := ops.LeaseTargetChecked(ctx, ops.LeaseDeps{Client: client, ProjectID: projectID, StateDir: stateDir, Swarm: swarm}, lease.Alias, what)
		if err == nil && !ok {
			// The record went between the two reads: nothing to vouch for.
			return lease, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				fmt.Sprintf("Lease %s was not %s: its record went while zcp read it", lease.Alias, what),
				"zerops_lease action=status lists the leases this zcp holds")
		}
		return checked, err
	}
}

// checkedLeaseOf is the lease host names, vouched for by its pool's
// orchestrator once (vouch) for a call that reaches its container over ssh or
// sshfs: err refuses a lease that is no longer zcp's — maybe another holder's
// — or that the orchestrator does not answer about, before anything reaches
// it; ok is false when host names no lease (an unreadable record set names
// none, as leaseOf reads it).
func checkedLeaseOf(ctx context.Context, stateDir string, vouch leaseVouch, host, what string) (ops.LeaseTargetInfo, bool, error) {
	lease, ok := leaseOf(stateDir, host)
	if !ok {
		return ops.LeaseTargetInfo{}, false, nil
	}
	if vouch == nil {
		vouch = orchestratorVouch(nil, "", stateDir, nil)
	}
	checked, err := vouch(ctx, lease, what)
	return checked, true, err
}

// deployListing is a deploy call's one listing of the project's services
// (an ES-backed search, docs/spec-workflows.md §3.5): its pool checks read
// service types from it, and the deploy resolves its source and target from
// it, so the call lists no more than it did before pools. A listing that
// fails reads nil: the pool checks keep every hostname (the gate or the
// deploy answers for it) and the deploy lists the services itself.
func deployListing(ctx context.Context, client platform.Client, projectID string) []platform.ServiceStack {
	if client == nil {
		return nil
	}
	services, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		return nil
	}
	return services
}

// lazyDeployListing is a single deploy's deployListing, read at most once and
// only when first asked (list): a deploy asks it only for what needs a
// service's type, so a call the gate or the delivery redirect answers from
// zcp's records lists nothing. listed is what was read, nil when nothing
// asked — the push then lists the services itself.
func lazyDeployListing(ctx context.Context, client platform.Client, projectID string) (list, listed func() []platform.ServiceStack) {
	var (
		services []platform.ServiceStack
		read     bool
	)
	list = func() []platform.ServiceStack {
		if !read {
			services, read = deployListing(ctx, client, projectID), true
		}
		return services
	}
	return list, func() []platform.ServiceStack { return services }
}

// requireDeployAdoption is a deploy's adoption gate, asked first from zcp's
// records alone; only once it refuses is the project listed (list), to exempt
// a pool among hostnames — a pool is never adopted (PS-5): a pool target is
// deployed to (DP-7), a pool source is refused after the gate (PS-3).
func requireDeployAdoption(stateDir string, rt runtime.Info, recipeProbe RecipeSessionProbe, list func() []platform.ServiceStack, hostnames ...string) *mcp.CallToolResult {
	if requireAdoption(stateDir, rt, recipeProbe, hostnames...) == nil {
		return nil
	}
	return requireAdoption(stateDir, rt, recipeProbe, withoutPoolHosts(list(), hostnames...)...)
}

// adoptedNonPool reports whether hostname is a service zcp adopted that is no
// pool: a pool is never adopted (PS-5), so only the dev half of a pair keyed by
// its dev pool is one among them. A deploy's pool-source check needs no
// listing for such a source.
func adoptedNonPool(stateDir, hostname string) bool {
	meta, err := workflow.FindServiceMeta(stateDir, hostname)
	if err != nil || meta == nil {
		return false
	}
	return !meta.DevIsPool() || meta.Hostname != hostname
}

// withoutPoolHosts drops the hostnames that are Swarm pools in services (a
// deployListing): a pool is never adopted (PS-5), so the adoption gate has
// nothing to ask of a deploy that targets one (DP-7). A hostname the listing
// does not hold stays, and the gate or the deploy's own lookup answers for
// it.
func withoutPoolHosts(services []platform.ServiceStack, hostnames ...string) []string {
	kept := make([]string, 0, len(hostnames))
	for _, h := range hostnames {
		if h != "" {
			if svc, err := ops.FindService(services, h); err == nil &&
				topology.IsPoolType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
				continue
			}
		}
		kept = append(kept, h)
	}
	return kept
}

// leasePoolsAmong is the set of pools whose lease is named in hostnames.
func leasePoolsAmong(stateDir string, hostnames []string) map[string]bool {
	var pools map[string]bool
	for _, h := range hostnames {
		if lease, ok := leaseOf(stateDir, h); ok {
			if pools == nil {
				pools = map[string]bool{}
			}
			pools[lease.Pool] = true
		}
	}
	return pools
}
