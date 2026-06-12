package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/inventory"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// deployPreFlight validates zerops.yaml configuration BEFORE deploy execution.
// This is the harness: it catches config errors that would cause silent deploy failures.
// Returns nil when stateDir is empty (no state directory = skip validation).
//
// Asymmetric source/target semantics mirror `ops.deploySSH` (the deploy that
// fires after pre-flight): yaml + working-tree live on the SOURCE service's
// mount; the role/mode/setup-resolution rules apply to the TARGET. This
// distinction is load-bearing for cross-deploy (`appdev → appstage`),
// where the canonical yaml is at `<projectRoot>/<sourceHostname>/zerops.yaml`
// while role-driven setup matching keys off the target's pair role.
//
// Pass sourceHostname == "" for local-environment deploys (no per-service
// SSHFS mount; yaml lives at workingDir or projectRoot).
//
// workingDir is the dev-machine path the agent passed to zerops_deploy
// (local mode). When set on a local-env deploy, it overrides the
// state-derived projectRoot for yaml lookup — honoring the advertised
// `workingDir` contract end-to-end (preflight checks the same yaml that
// ops.DeployLocal will deploy from). Container-env callers pass "" because
// workingDir there is a CONTAINER path and not relevant for dev-side
// yaml lookup.
func deployPreFlight(ctx context.Context, client platform.Client, projectID, stateDir, sourceHostname, targetHostname, setup, workingDir string, sourceMountReadable bool) (resolvedSetup string, result *workflow.StepCheckResult, err error) {
	if stateDir == "" {
		return setup, nil, nil
	}

	// Resolve target ServiceMeta via the pair-aware lookup so a stage
	// hostname (the secondary half of a standard pair) finds the dev meta
	// it shares — spec-workflows.md §8 E8 ("runtime meta is pair-keyed";
	// keying by m.Hostname alone violates the invariant).
	meta, err := workflow.FindServiceMeta(stateDir, targetHostname)
	if err != nil {
		return setup, nil, fmt.Errorf("preflight find meta: %w", err)
	}
	// No meta = not adopted, but requireAdoption handles that gate.
	// If meta is nil, skip pre-flight (permissive).
	if meta == nil {
		return setup, nil, nil
	}

	// Local env + container source: the source service's zerops.yaml lives
	// ON THE CONTAINER, not on a local SSHFS mount — the container-env
	// mount lookup below would fail with a false "source mount
	// <cwd>/<host> missing" for every SSH deploy issued from a local-mode
	// server. Defer yaml + env validation to deploy time (ops.DeploySSH
	// reads the yaml in-container; the platform validates live) and still
	// resolve the setup from the meta cache so zcli gets an explicit
	// --setup when one is recorded.
	if sourceHostname != "" && !sourceMountReadable {
		resolvedSetup = setup
		if resolvedSetup == "" {
			resolvedSetup = meta.SetupNameFor(targetHostname)
		}
		return resolvedSetup, &workflow.StepCheckResult{
			Passed: true,
			Checks: []workflow.StepCheck{{
				Name: "zerops_yml_exists", Status: statusPass,
				Detail: fmt.Sprintf("local env, container source: zerops.yaml is validated at deploy time on %q", sourceHostname),
			}},
			Summary: "pre-flight deferred to deploy time (local env, container source)",
		}, nil
	}

	projectRoot := projectRootFromState(stateDir)
	var checks []workflow.StepCheck

	// Find and parse zerops.yaml from the source mount (container env) or
	// workingDir / project root (local env). See findAndParseZeropsYml's
	// contract for the workingDir-vs-projectRoot precedence.
	doc, parseErr := findAndParseZeropsYml(projectRoot, sourceHostname, workingDir)
	if parseErr != nil {
		checks = append(checks, workflow.StepCheck{
			Name: "zerops_yml_exists", Status: statusFail,
			Detail: fmt.Sprintf("zerops.yaml not found or invalid: %v", parseErr),
		})
		return setup, &workflow.StepCheckResult{
			Passed: false, Checks: checks, Summary: "zerops.yaml not found or invalid",
		}, nil
	}
	checks = append(checks, workflow.StepCheck{
		Name: "zerops_yml_exists", Status: statusPass,
	})

	// Gate B — first-deploy cache hit. When the input `setup` is empty AND
	// the per-pair ServiceMeta has a canonical PrimarySetupName /
	// StageSetupName recorded (from a prior Gate R / Gate A / earlier
	// Gate B write-back), use it directly. Skips the role+hostname
	// fallback entirely so a setup-name the user explicitly chose
	// (set-default-setup, P6) survives recipe-template drift.
	if setup == "" {
		if cached := meta.SetupNameFor(targetHostname); cached != "" {
			setup = cached
		}
	}

	// Resolve setup entry: explicit setup param → role name → hostname.
	// v8.85 — when the input `setup` is empty and pre-flight resolves one
	// via role or hostname fallback, the resolved name is propagated back
	// to the caller so `zcli push --setup <name>` is invoked explicitly.
	// Without this, pre-flight silently matched the right setup but zcli
	// received an empty flag and failed with "Cannot find corresponding
	// setup in zerops.yaml" — the exact failure in session-log-16 (L145).
	//
	// Role keys off the TARGET — stage half of a pair returns
	// DeployRoleStage (→ "prod" setup), dev half returns DeployRoleDev
	// (→ "dev" setup). PrimaryRole() would mis-classify a stage target as
	// dev because it ignores StageHostname.
	role := meta.RoleFor(targetHostname)
	if role == "" {
		role = meta.PrimaryRole()
	}
	entry := resolveSetupEntry(doc, setup, role, targetHostname)
	if entry == nil {
		// Gate B — multi-setup ambiguity surfaces as the structured
		// requiresSetupInput blocker so the agent can branch into
		// set-default-setup (P6). Pre-Gate-B path returned a free-text
		// "no setup entry %q found" error that agents had to parse
		// from prose; the typed blocker carries availableSetups +
		// recovery in the wire shape.
		availableSetups := doc.SetupNames()
		if len(availableSetups) > 1 {
			return setup, nil, &workflow.ErrRequiresSetupInput{
				Service:         targetHostname,
				TargetHostname:  targetHostname,
				AvailableSetups: availableSetups,
				Reason:          "no setup matched hostname / suffix conventions and yaml has multiple blocks",
			}
		}
		tried := targetHostname
		if setup != "" {
			tried = setup
		}
		checks = append(checks, workflow.StepCheck{
			Name: targetHostname + "_setup", Status: statusFail,
			Detail: fmt.Sprintf("no setup entry %q found in zerops.yaml — available setups: [%s]. Pass one explicitly via the `setup` parameter; in recipes setup names differ from hostnames (e.g. hostname=%s → setup=dev), the deploy tool cannot guess when multiple setups are declared.", tried, strings.Join(availableSetups, ", "), targetHostname),
		})
		return setup, &workflow.StepCheckResult{
			Passed: false, Checks: checks,
			Summary: fmt.Sprintf("no matching setup entry for %s", targetHostname),
		}, nil
	}
	// Entry resolved. The actual setup name to pass to zcli is entry.Setup —
	// even when the input was empty and role/hostname fallback found it.
	resolvedSetup = entry.Setup

	// Gate B — first-deploy write-back. Persist the resolved setup
	// onto the meta regardless of whether `setup` arrived explicit or
	// got role-resolved here. Recipe-bootstrap (the dominant adopt-side
	// path) hands the agent a setup name in the bootstrap-guide prose,
	// so the agent typically passes setup= explicitly on every deploy;
	// gating write-back on input being empty meant the cache stayed
	// permanently empty for every recipe-flow service. The deployed setup
	// metadata writer is a no-op when the value already matches what's on
	// disk, so unconditional write doesn't cause spurious disk churn either.
	//
	// Side effect: container-side bootstrap `route="adopt"` (which has
	// no Gate A wire-up — Gate A is local-env-only) also lands its
	// canonical name here on the first deploy. That closes the
	// adopt-route discovery gap plan §"Gate A items" tracked under
	// "Container-side adoption: first agent interaction surfaces …".
	if err := recordResolvedDeploySetupMeta(stateDir, targetHostname, entry.Setup, entry.HasPorts()); err != nil {
		return setup, nil, fmt.Errorf("preflight record deployed setup metadata: %w", err)
	}
	checks = append(checks, workflow.StepCheck{
		Name: targetHostname + "_setup", Status: statusPass,
	})

	// Dev/prod env divergence check.
	checks = append(checks, checkDevProdEnvDivergence(doc)...)

	// deployFiles path validation is owned by ops.ValidateZeropsYml (invoked
	// at the push site in deploy_ssh.go / deploy_local.go) and enforces DM-2
	// with full DeployClass context. DM-4 (docs/spec-workflows.md §8) forbids
	// a duplicate check in this layer. The pre-flight's sole yaml concern
	// here is setup resolution + schema + env-ref validation.

	// Self-shadow check (KEY: ${KEY} → the interpolator resolves it to the
	// literal string "${KEY}", so the app connects to "${db_hostname}:5432" and
	// crashes). Local — no API needed. The recipe-generate path runs this
	// (workflow_checks_recipe.go), but the develop/classic deploy path did NOT,
	// so a self-shadowed run.envVariables shipped GREEN and the DB-backed
	// endpoint was broken yet reported working (Wave-2 finding: the
	// develop-active atom WARNS about this anti-pattern but no CHECK enforced it
	// on the dominant deploy path — tell-without-check). Single owner:
	// checkEnvSelfShadow, so tell == check.
	if len(entry.Run.EnvVariables) > 0 {
		checks = append(checks, checkEnvSelfShadow(ctx, targetHostname, entry))
	}

	// Validate env var references.
	if len(entry.Run.EnvVariables) > 0 && client != nil {
		checks = append(checks, preflightEnvRefs(ctx, client, projectID, targetHostname, entry)...)
	}

	allPassed := checksAllPassed(checks)
	summary := "pre-flight checks passed"
	if !allPassed {
		summary = "pre-flight checks failed — fix issues before deploying"
	}
	return resolvedSetup, &workflow.StepCheckResult{
		Passed: allPassed, Checks: checks, Summary: summary,
	}, nil
}

// resolveSetupEntry finds the zerops.yaml setup entry using priority:
// explicit setup param → role-based name → hostname fallback.
func resolveSetupEntry(doc *ops.ZeropsYmlDoc, setup string, role topology.Mode, hostname string) *ops.ZeropsYmlEntry {
	if setup != "" {
		return doc.FindEntry(setup)
	}
	// Role-based: "dev" or "stage" → try as setup name.
	if entry := doc.FindEntry(string(role)); entry != nil {
		return entry
	}
	// Stage and simple roles map to "prod" setup.
	if role == topology.DeployRoleStage || role == topology.DeployRoleSimple {
		if entry := doc.FindEntry(workflow.RecipeSetupProd); entry != nil {
			return entry
		}
	}
	// Fallback: hostname as setup name (legacy).
	return doc.FindEntry(hostname)
}

// preflightEnvRefs validates env var references against live API data for a single target.
func preflightEnvRefs(ctx context.Context, client platform.Client, projectID, hostname string, entry *ops.ZeropsYmlEntry) []workflow.StepCheck {
	services, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		return []workflow.StepCheck{{
			Name: hostname + "_env_refs", Status: statusFail,
			Detail: fmt.Sprintf("failed to list services for env var validation: %v", err),
		}}
	}

	// Project envs are identical for every service — fetch once. A FAILED read
	// is NOT "no project vars": collapsing it to empty would false-fail valid
	// inherited refs (e.g. ${api_SHARED_KEY}) during a transient blip (E2).
	// Surface it as a non-blocking WARN and short-circuit — never a typo-FAIL.
	projVars, projErr := inventory.FetchProjectEnvs(ctx, client, projectID)
	if projErr != nil {
		return []workflow.StepCheck{{
			Name: hostname + "_env_refs", Status: statusPass,
			Detail: "project env layer unavailable — cross-refs unverified (transient); retry after `zcli vpn up`",
		}}
	}
	projectLayer := ops.ProjectEnvLayer{Vars: projVars, State: ops.LayerState{Availability: ops.LayerPresent}}

	// Known-var universe per sibling = slim service env ∪ yaml-baked
	// run.envVariables (app-version userDataList) ∪ project env. The slim
	// /env alone misses yaml-baked vars, which is what made valid refs to a
	// sibling's run.envVariables var false-fail. Spec §6.
	liveHostnames := make([]string, 0, len(services))
	discoveredEnvVars := make(map[string][]string)
	neverDeployed := make(map[string]bool)
	unconfirmable := make(map[string]bool)
	for _, svc := range services {
		liveHostnames = append(liveHostnames, svc.Name)
		eff, effErr := ops.EffectiveServiceEnv(ctx, client, svc, projectLayer)
		if effErr != nil {
			// Precondition failure (e.g. nil client) — can't confirm this
			// sibling; route to WARN, never a hard typo-FAIL.
			unconfirmable[svc.Name] = true
			continue
		}
		// A transient fetch failure on a layer is Unavailable, NOT empty (F3):
		// mark the sibling unconfirmable BEFORE the never-deployed set so a blip
		// routes to WARN instead of nil-knownVars → false typo-FAIL.
		if !eff.ReadComplete() {
			unconfirmable[svc.Name] = true
		}
		discoveredEnvVars[svc.Name] = eff.Keys()
		if ops.IsRuntimeNeverDeployed(svc) {
			neverDeployed[svc.Name] = true
		}
	}

	// E3: overlay the candidate target's OWN run.envVariables keys. The version
	// being deployed isn't on the platform yet, so a self-introduced var (and a
	// self-ref to it, e.g. BAR: ${app_FOO}) must validate against the LOCAL yaml,
	// not the prior app-version. The target's authoritative source for THIS
	// deploy is its local entry; siblings still validate against platform state.
	// run.envVariables is the canonical location (CLAUDE.md).
	for k := range entry.Run.EnvVariables {
		discoveredEnvVars[hostname] = append(discoveredEnvVars[hostname], k)
	}

	envErrs := ops.ValidateEnvReferences(entry.Run.EnvVariables, discoveredEnvVars, liveHostnames)

	// Partition by sibling lifecycle/availability. A never-deployed runtime's
	// yaml-baked vars aren't on the platform yet, and an Unavailable sibling's
	// layers couldn't be read — neither is a confirmed typo → WARN, don't block.
	// A miss on a fully-read live/managed sibling IS a real typo → FAIL.
	var failDetails, warnDetails []string
	for _, e := range envErrs {
		switch {
		case neverDeployed[e.Host]:
			warnDetails = append(warnDetails, fmt.Sprintf("%s → %q not yet deployed; its run.envVariables can't be confirmed (verify the ref is intentional)", e.Reference, e.Host))
		case unconfirmable[e.Host]:
			warnDetails = append(warnDetails, fmt.Sprintf("%s → %q env layer unavailable (transient); ref unverified — retry after `zcli vpn up`", e.Reference, e.Host))
		default:
			failDetails = append(failDetails, fmt.Sprintf("%s: %s", e.Reference, e.Reason))
		}
	}
	if len(failDetails) > 0 {
		return []workflow.StepCheck{{
			Name: hostname + "_env_refs", Status: statusFail,
			Detail: strings.Join(failDetails, "; "),
		}}
	}
	if len(warnDetails) > 0 {
		return []workflow.StepCheck{{
			Name: hostname + "_env_refs", Status: statusPass,
			Detail: strings.Join(warnDetails, "; "),
		}}
	}
	return []workflow.StepCheck{{
		Name: hostname + "_env_refs", Status: statusPass,
	}}
}
