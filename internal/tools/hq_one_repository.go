package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate's monorepo is one repository in HQ (docs/spec-mate.md §10.10, "One
// repository per monorepo"). Every pair a pass wires asks HQ for a
// repository named after its dev hostname (hqRepoNameOf), so a dev service
// per part — each holding a checkout of the same repository — would split
// one repository into as many as there are dev services, and HQ cannot
// remove one. The pass therefore compares a pair's checkout with the
// checkouts of the pairs already wired before it asks HQ for a new one.

// planStagesFallback is what a plan refusal names in place of the plan made
// from it when that plan cannot be marshalled.
const planStagesFallback = "runtime.stages listing {hostname, setupName} for every part"

// hqRootsReadTimeout bounds each root-commit read the one-repository check
// makes over SSH: a dev service that does not answer costs a pass no more
// than this, and is no evidence.
const hqRootsReadTimeout = 20 * time.Second

// readPairRoots reads the root commits carrying code of host's checkout at
// workingDir (ops.ReadRepositoryRoots), bounded by hqRootsReadTimeout; nil
// when it cannot be read.
func readPairRoots(ctx context.Context, sshDeployer ops.SSHDeployer, host, workingDir string) []string {
	readCtx, cancel := context.WithTimeout(ctx, hqRootsReadTimeout)
	defer cancel()
	roots, err := ops.ReadRepositoryRoots(readCtx, sshDeployer, host, workingDir)
	if err != nil {
		return nil
	}
	return roots
}

// hqOneRepositoryRefusal is why a Mate's pass gives the pair m no repository
// of its own in HQ: m is not wired yet, so wiring it would ask HQ for a new
// repository, and its dev checkout holds the repository a pair already wired
// checks out — the two share a root commit carrying code. Such a pair stays
// unwired, and the line names both dev services, the wired pair's
// repository and the way out: one dev service with a stage per part, then
// pool-migrate. "" outside a Mate, for a pair already wired (it is wired
// again to the repository its record names), when no wired pair's checkout
// shares a root with m's, and when m's checkout cannot be read or has no
// root carrying code — no evidence, so the pair is wired as before. A wired
// pair whose checkout cannot be read is skipped, and a pair keyed by its dev
// pool is never compared: a pool holds no checkout of its own.
func hqOneRepositoryRefusal(ctx context.Context, sshDeployer ops.SSHDeployer, rt runtime.Info, stateDir string, m *workflow.ServiceMeta) string {
	if !rt.MateEnabled || m == nil || hqPairWired(m) {
		return ""
	}
	// Read now, not with the pass's list: a pair this pass wired is wired.
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return ""
	}
	var wired []*workflow.ServiceMeta
	for _, other := range metas {
		if other.Hostname != m.Hostname && hqPairWired(other) && !other.DevIsPool() {
			wired = append(wired, other)
		}
	}
	if len(wired) == 0 {
		return ""
	}
	sort.Slice(wired, func(i, j int) bool { return wired[i].Hostname < wired[j].Hostname })
	roots := readPairRoots(ctx, sshDeployer, m.Hostname, hqPairWorkingDir(m))
	if len(roots) == 0 {
		return ""
	}
	for _, other := range wired {
		if ops.RepositoryRootsShared(roots, readPairRoots(ctx, sshDeployer, other.Hostname, hqPairWorkingDir(other))) {
			return oneRepositoryLine(m, other)
		}
	}
	return ""
}

// oneRepositoryLine is the pass's line for the pair m whose checkout holds
// the repository the pair kept is wired to.
func oneRepositoryLine(m, kept *workflow.ServiceMeta) string {
	takes := fmt.Sprintf("%s's part becomes a stage of %s's pair", m.Hostname, kept.Hostname)
	switch stages := m.StageHostnames(); len(stages) {
	case 0:
	case 1:
		takes = fmt.Sprintf("%s's pair takes %s's stage %s", kept.Hostname, m.Hostname, quotedList(stages))
	default:
		takes = fmt.Sprintf("%s's pair takes %s's stages %s", kept.Hostname, m.Hostname, quotedList(stages))
	}
	return fmt.Sprintf("gets no repository in HQ: its checkout holds the same repository as %s's, which is wired to %q, and a monorepo is one repository, never one per dev service. "+
		"Keep one dev service with a stage per part — %s, %s's work goes onto a branch of %q, and %s then serves no part — "+
		"then zerops_workflow workflow=\"pool-migrate\" service=%q gives each part a dev pool of its own (zerops_knowledge uri=\"zerops://playbooks/monorepo\")",
		kept.Hostname, kept.HQ.Repo, takes, m.Hostname, kept.HQ.Repo, m.Hostname, kept.Hostname)
}

// oneRepositoryPlanError refuses a Mate's bootstrap plan pairing two existing
// dev services whose checkouts are one repository — they share a root commit
// carrying code (ops.ReadRepositoryRoots): every pair a Mate plans gets a
// repository of its own in HQ, so the plan would split one repository. The
// refusal carries the plan made from it: the first of those dev services, in
// plan order, keeps every stage of the pairs holding its repository, the
// others leave the plan, and pool-migrate then gives each part a dev pool of
// its own. Only existing standard pairs are read — a dev service the plan
// creates holds no checkout yet — and only when the plan has two of them; a
// checkout that cannot be read, or has no root carrying code, is no
// evidence. nil outside a container or a Mate.
func oneRepositoryPlanError(ctx context.Context, plan []workflow.BootstrapTarget, ssh ops.SSHDeployer, rt runtime.Info) *platform.PlatformError {
	if !rt.InContainer || !rt.MateEnabled || ssh == nil {
		return nil
	}
	var existing []int
	for i, target := range plan {
		if target.Runtime.IsExisting && target.Runtime.EffectiveMode() == topology.PlanModeStandard {
			existing = append(existing, i)
		}
	}
	if len(existing) < 2 {
		return nil
	}
	roots := make(map[int][]string, len(existing))
	for _, i := range existing {
		roots[i] = readPairRoots(ctx, ssh, plan[i].Runtime.DevHostname, topology.CodeMountRoot)
	}
	fixed := make([]workflow.BootstrapTarget, 0, len(plan))
	merged := map[int]bool{}
	var reasons, kept, dropped []string
	for i, target := range plan {
		if merged[i] {
			continue
		}
		group := []int{i}
		if len(roots[i]) > 0 {
			for _, j := range existing {
				if j > i && !merged[j] && ops.RepositoryRootsShared(roots[i], roots[j]) {
					group = append(group, j)
					merged[j] = true
				}
			}
		}
		if len(group) == 1 {
			fixed = append(fixed, target)
			continue
		}
		var devs []string
		var stages []topology.StageRef
		for _, j := range group {
			devs = append(devs, plan[j].Runtime.DevHostname)
			for _, s := range plan[j].Runtime.StageRefs() {
				if !slices.ContainsFunc(stages, func(have topology.StageRef) bool { return have.Hostname == s.Hostname }) {
					stages = append(stages, s)
				}
			}
		}
		reasons = append(reasons, fmt.Sprintf("targets %s hold one repository — their checkouts share a first commit — and a monorepo is one repository: in a Mate every pair gets a repository of its own in HQ, so they are one pair", quotedList(devs)))
		kept = append(kept, devs[0])
		dropped = append(dropped, devs[1:]...)
		r := target.Runtime
		r.ExplicitStage, r.StageSetupName, r.Stages = "", "", stages
		target.Runtime = r
		fixed = append(fixed, target)
	}
	if len(reasons) == 0 {
		return nil
	}
	planText := planStagesFallback
	if planJSON, err := json.Marshal(fixed); err == nil {
		planText = "plan=" + string(planJSON)
	}
	migrate := make([]string, 0, len(kept))
	for _, dev := range kept {
		migrate = append(migrate, fmt.Sprintf("zerops_workflow workflow=\"pool-migrate\" service=%q", dev))
	}
	pe := platform.NewPlatformError(platform.ErrInvalidParameter,
		"Plan validation failed: "+strings.Join(reasons, "; "),
		fmt.Sprintf("Re-submit the plan with one pair per repository: %s with isExisting and every part's stage in \"stages\", %s left out of it. Once bootstrap closes, %s gives each part a dev pool of its own (zerops_knowledge uri=\"zerops://playbooks/monorepo\"). %s",
			quotedList(kept), quotedList(dropped), strings.Join(migrate, ", then "), planText))
	pe.Subcode = platform.SubcodeWorkerPlanShape
	return pe
}

// monorepoPlanError refuses a Mate's classic plan of several dev/stage pairs
// (docs/spec-mate.md §10.10, "One repository per monorepo"): a classic
// bootstrap wires its pairs at its close, while their checkouts are still
// fresh from `zcp init`, so no root commit tells the parts of one repository
// apart (oneRepositoryPlanError reads existing checkouts only) — and every
// pair a Mate wires asks HQ for a repository of its own, which would split one
// repository into one per part for good. The refusal carries the plan made
// from it: the first pair's dev service holding every pair's stage, each with
// its setup (the plan's own, else the part's name, the stage hostname without
// its `stage` suffix), every dependency once, and pool-migrate after bootstrap
// closes, giving each part a dev pool of its own. separate — the plan's
// separateRepositories — says the pairs are separate repositories, and takes
// the plan as it is. nil outside a Mate, with separate, and for a plan of one
// pair however many stages it has.
func monorepoPlanError(plan []workflow.BootstrapTarget, mate, separate bool) *platform.PlatformError {
	if !mate || separate {
		return nil
	}
	var pairs []int
	for i, target := range plan {
		if target.Runtime.EffectiveMode() == topology.PlanModeStandard {
			pairs = append(pairs, i)
		}
	}
	if len(pairs) < 2 {
		return nil
	}
	first := plan[pairs[0]]
	r := first.Runtime
	var devs, stages []string
	var refs []topology.StageRef
	var deps []workflow.Dependency
	for _, i := range pairs {
		target := plan[i]
		devs = append(devs, target.Runtime.DevHostname)
		for _, s := range target.Runtime.StageRefs() {
			if s.SetupName == "" {
				s.SetupName = partOfStage(s.Hostname)
			}
			refs = append(refs, s)
			stages = append(stages, s.Hostname)
		}
		for _, d := range target.Dependencies {
			if !slices.ContainsFunc(deps, func(have workflow.Dependency) bool { return have.Hostname == d.Hostname }) {
				deps = append(deps, d)
			}
		}
	}
	// A dependency another pair created is created by the one pair now.
	for i := range deps {
		if strings.EqualFold(deps[i].Resolution, workflow.ResolutionShared) {
			deps[i].Resolution = workflow.ResolutionCreate
		}
	}
	r.ExplicitStage, r.StageSetupName, r.Stages = "", "", refs
	fixed := []workflow.BootstrapTarget{{Runtime: r, Dependencies: deps}}
	for i, target := range plan {
		if !slices.Contains(pairs, i) {
			fixed = append(fixed, target)
		}
	}
	planText := planStagesFallback
	if planJSON, err := json.Marshal(fixed); err == nil {
		planText = "plan=" + string(planJSON)
	}
	pe := platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("Plan validation failed: targets %s are each a dev/stage pair, and in a Mate every pair gets a repository of its own in HQ — a monorepo is one repository, never one per part, and HQ cannot merge or remove one once made", quotedList(devs)),
		fmt.Sprintf("Plan the repository as one pair: %s with a stage per part in \"stages\" (%s), every dependency once; once bootstrap closes, zerops_workflow workflow=\"pool-migrate\" service=%q gives each part a dev pool of its own (zerops_knowledge uri=\"zerops://playbooks/monorepo\"). If these runtimes are separate repositories, not parts of one, re-submit the same plan with separateRepositories=true. %s",
			quotedList([]string{r.DevHostname}), strings.Join(stages, ", "), r.DevHostname, planText))
	pe.Subcode = platform.SubcodeWorkerPlanShape
	return pe
}

// partOfStage is the part a stage builds, by its hostname: the hostname
// without its `stage` suffix (webstage → web), as pool-migrate names the
// part's pool (docs/spec-workflows.md §13 PM-12); the hostname itself when it
// has none.
func partOfStage(stage string) string {
	if part, ok := strings.CutSuffix(stage, "stage"); ok && part != "" {
		return part
	}
	return stage
}
