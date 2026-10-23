package tools

import (
	"context"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// attachPartsSignal reads each dev half's repo (ops.ReadPartsSignal) for the
// multi-part signal the `parts:` atom axis sees, and for whether its
// zerops.yaml already declares bases — carried to each stage of the pair,
// which deploys setups of the same file — and records on engine the config
// file name each repo keeps, so guidance names the file the repo has
// (namingRepoConfigFile). Container only; a read that fails leaves the
// service single and undeclared, as an unread one is, and records nothing.
func attachPartsSignal(ctx context.Context, services []workflow.ServiceSnapshot, ssh ops.SSHDeployer, rt runtime.Info, engine *workflow.Engine) {
	if !rt.InContainer || ssh == nil {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, attachRepoStatusConcurrency)
	for i := range services {
		svc := services[i]
		if svc.Mode != topology.ModeDev && svc.Mode != topology.ModeStandard {
			continue
		}
		if holdsNoRepo(svc.RuntimeClass) {
			continue
		}
		wg.Add(1)
		go func(i int, svc workflow.ServiceSnapshot) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sig, err := ops.ReadPartsSignal(ctx, ssh, svc.Hostname, svc.SetupName)
			if err != nil {
				return
			}
			// Each goroutine writes only its own index.
			services[i].MultiPart = sig.MultiPart()
			services[i].DeclaresBase = sig.DeclaresBase
			engine.RecordRepoConfigFile(svc.Hostname, sig.ConfigFile)
		}(i, svc)
	}
	wg.Wait()
	declared := map[string]bool{}
	for _, svc := range services {
		if !svc.DeclaresBase {
			continue
		}
		for _, stage := range svc.PairStageHostnames() {
			declared[stage] = true
		}
	}
	for i := range services {
		if declared[services[i].Hostname] {
			services[i].DeclaresBase = true
		}
	}
}

// holdsNoRepo reports a class whose service never holds the app's repo to read:
// a managed service, an unknown type, a pool (a read by service hostname
// reaches a random container of it) or a Box (spec-workflows.md §8 PS-3).
func holdsNoRepo(class topology.RuntimeClass) bool {
	switch class {
	case topology.RuntimeManaged, topology.RuntimeUnknown, topology.RuntimePool, topology.RuntimeSandbox:
		return true
	case topology.RuntimeDynamic, topology.RuntimeStatic, topology.RuntimeImplicitWeb:
		return false
	}
	return false
}

// multiPartRouteNote is the route-menu line that points at the monorepo
// playbook when an existing runtime service's repo holds several deployable
// parts, so the plan is made with the playbook in hand rather than after the
// first bootstrap. "" when none is multi-part, or outside a container.
func multiPartRouteNote(ctx context.Context, existing []platform.ServiceStack, ssh ops.SSHDeployer, rt runtime.Info, engine *workflow.Engine) string {
	services := readExistingRepos(ctx, existing, ssh, rt, engine)
	var multi []string
	for _, s := range services {
		if s.MultiPart {
			multi = append(multi, "`"+s.Hostname+"`")
		}
	}
	if len(multi) == 0 {
		return ""
	}
	return "\n\nThe repo of " + strings.Join(multi, ", ") + " holds more than one deployable part. " +
		"Before you plan its services and stages, read `zerops_knowledge uri=\"zerops://playbooks/monorepo\"`."
}

// readExistingRepos reads the repo of every existing runtime service, before
// bootstrap has given any of them a mode, as attachPartsSignal reads a dev
// half's — so the route menu and the adopt route see the parts and name the
// config file each repo keeps. A service a pair already records as its stage
// is skipped: it holds what its setup deployed, never a repo.
func readExistingRepos(ctx context.Context, existing []platform.ServiceStack, ssh ops.SSHDeployer, rt runtime.Info, engine *workflow.Engine) []workflow.ServiceSnapshot {
	stages := map[string]bool{}
	if engine != nil {
		metas, _ := workflow.ListServiceMetas(engine.StateDir())
		for _, m := range metas {
			for _, stage := range m.StageHostnames() {
				stages[stage] = true
			}
		}
	}
	var services []workflow.ServiceSnapshot
	for _, s := range existing {
		class := topology.RuntimeClassFor(s.ServiceStackTypeInfo.ServiceStackTypeVersionName)
		if holdsNoRepo(class) || stages[s.Name] {
			continue
		}
		services = append(services, workflow.ServiceSnapshot{Hostname: s.Name, Mode: topology.ModeDev, RuntimeClass: class})
	}
	attachPartsSignal(ctx, services, ssh, rt, engine)
	return services
}
