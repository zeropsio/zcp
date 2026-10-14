package tools

import (
	"context"
	"sync"

	"github.com/zeropsio/zcp/internal/ops"
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
