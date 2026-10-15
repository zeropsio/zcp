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

// attachPartsSignal reads each dev half's repo for the multi-part signal
// (ops.ReadPartsSignal) so the `parts:` atom axis sees it. Container only;
// a read that fails leaves the service single, as an unread one is.
func attachPartsSignal(ctx context.Context, services []workflow.ServiceSnapshot, ssh ops.SSHDeployer, rt runtime.Info) {
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
		if svc.RuntimeClass == topology.RuntimeManaged || svc.RuntimeClass == topology.RuntimeUnknown {
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
		}(i, svc)
	}
	wg.Wait()
}

// multiPartRouteNote is the route-menu line that points at the monorepo
// playbook when an existing runtime service's repo holds several deployable
// parts, so the plan is made with the playbook in hand rather than after the
// first bootstrap. "" when none is multi-part, or outside a container.
func multiPartRouteNote(ctx context.Context, existing []platform.ServiceStack, ssh ops.SSHDeployer, rt runtime.Info) string {
	var services []workflow.ServiceSnapshot
	for _, s := range existing {
		class := topology.RuntimeClassFor(s.ServiceStackTypeInfo.ServiceStackTypeVersionName)
		if class == topology.RuntimeManaged || class == topology.RuntimeUnknown {
			continue
		}
		services = append(services, workflow.ServiceSnapshot{Hostname: s.Name, Mode: topology.ModeDev, RuntimeClass: class})
	}
	attachPartsSignal(ctx, services, ssh, rt)
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
