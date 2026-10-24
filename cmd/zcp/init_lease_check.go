package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/zeropsio/zcp/internal/auth"
	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// initOptions are what `zcp init` and `zcp init sshfs` hand the init steps:
// the lease check the lease restore asks before it mounts a lease
// (docs/spec-workflows.md §8 DP-2).
func initOptions() []zcpinit.Option {
	return []zcpinit.Option{zcpinit.WithLeaseCheck(initLeaseCheck(resolveInitLeaseDeps))}
}

// initLeaseCheck is the lease check `zcp init` hands its lease restore:
// ops.LeaseTargetChecked — the one every tool that reaches a lease's container
// asks — over the API client, project and orchestrators resolve builds, with
// the records in the state dir the restore reads. They are built on the first
// check, so a container with no lease to mount never reaches the API, and
// once, however many leases ask at once. A client that cannot be built leaves
// every lease unverified, the refusal saying why; never lost.
func initLeaseCheck(resolve func(context.Context) (ops.LeaseDeps, error)) zcpinit.LeaseCheck {
	var (
		once    sync.Once
		deps    ops.LeaseDeps
		depsErr error
	)
	return func(ctx context.Context, stateDir, alias, what string) (ops.LeaseTargetInfo, bool, error) {
		once.Do(func() { deps, depsErr = resolve(ctx) })
		if depsErr != nil {
			refusal := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				fmt.Sprintf("Lease %s was not %s: zcp could not build the Zerops API client it asks the pool's orchestrator with whether its container is still this lease's (%v)", alias, what, depsErr),
				"")
			refusal.Cause = depsErr
			return ops.LeaseTargetInfo{}, true, refusal
		}
		d := deps
		d.StateDir = stateDir
		return ops.LeaseTargetChecked(ctx, d, alias, what)
	}
}

// resolveInitLeaseDeps builds what a lease check asks with the way serve mode
// builds its client (runServer): the credentials (ZCP_API_KEY, else zcli's
// login), the platform client, and the project auth resolves — validating the
// token and binding the client to the project's org — plus the pools'
// orchestrators.
func resolveInitLeaseDeps(ctx context.Context) (ops.LeaseDeps, error) {
	creds, err := auth.ResolveCredentials()
	if err != nil {
		return ops.LeaseDeps{}, fmt.Errorf("auth: %w", err)
	}
	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		return ops.LeaseDeps{}, fmt.Errorf("create platform client: %w", err)
	}
	info, err := auth.Resolve(ctx, client)
	if err != nil {
		return ops.LeaseDeps{}, fmt.Errorf("auth: %w", err)
	}
	return ops.LeaseDeps{
		Client:    client,
		ProjectID: info.ProjectID,
		Swarm:     ops.NewSwarmFactory(&http.Client{Timeout: ops.LeaseCallTimeout}),
	}, nil
}
