package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// bearerResolveWait bounds EnsureBearer's wait for a written reference to
// resolve in the live env: it resolved in about 15–20 s on a deployed pool
// (live, 2026-10-23 and 2026-11-16).
const bearerResolveWait = 60 * time.Second

// bearerResolvePoll is how often that wait reads the live env file (a local
// read; the platform sends no receipt of the file's rewrite).
const bearerResolvePoll = time.Second

// bearerWriteBudget bounds one EnsureBearer call as a whole: the wait for
// another writer's bearer lock, the service reads, the write's process and
// the resolve wait.
const bearerWriteBudget = 3 * time.Minute

// selfServiceIDKey is the container env key Zerops sets to the id of the
// service the container belongs to — here zcp's own service.
const selfServiceIDKey = "serviceId"

// serviceStatusCreating is a service the platform is still creating.
const serviceStatusCreating = "CREATING"

// poolUndeployed reports a pool status before any successful deploy: the
// reference written then resolves only during the first successful deploy
// (live, 2026-11-16), so nothing is waited for.
func poolUndeployed(status string) bool {
	switch status {
	case platform.ServiceStatusReadyToDeploy, platform.ServiceStatusNew, serviceStatusCreating:
		return true
	}
	return false
}

// EnsureBearer is the one writer of a pool's bearer reference on this zcp
// service, for an acquire (or the stand-up) that finds no usable bearer: it
// writes <pool>_LEASE_TOKEN=${<pool>_API_TOKEN} on zcp's own service under its
// own per-pool lock (never a lease key lock: an acquire calls it holding one,
// so the order is key lock, then bearer lock), and returns the bearer.
//
// A usable bearer in the live env is returned as it is, never rewritten; a
// reference already on the service is not written again, so two acquires
// write once. A pool that has had no successful deploy yet (NEW, CREATING,
// READY_TO_DEPLOY, read by direct GET) resolves the reference only during its
// first successful deploy: EnsureBearer then writes it and returns "" with no
// error — the reference is in place, not yet resolved, which the caller
// reports. On a deployed pool it waits at most bearerResolveWait (and never
// past bearerWriteBudget) for the reference to resolve and otherwise fails
// naming the variable, the time waited and the state it last saw (absent or
// the literal reference; never a value). The read sources (liveBearer, the
// pool's env) stay as they are.
func EnsureBearer(ctx context.Context, d LeaseDeps, pool string) (string, error) {
	if token, _, err := liveBearer(d, pool); err != nil || token != "" {
		return token, err
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, bearerWriteBudget)
	defer cancel()
	release, err := lockBearer(ctx, d.StateDir, pool)
	if err != nil {
		return "", budgetSpent(ctx, err, start, "waiting for the bearer lock of pool '"+pool+"'")
	}
	defer release()
	// Another writer may have finished while this one waited for the lock.
	if token, _, err := liveBearer(d, pool); err != nil || token != "" {
		return token, err
	}

	services, err := ListProjectServicesDirect(ctx, d.Client, d.ProjectID)
	if err != nil {
		return "", err
	}
	svc, err := FindService(services, pool)
	if err != nil {
		return "", err
	}
	if typ := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName; !topology.IsPoolType(typ) {
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Service '%s' is %s, not a Swarm pool", pool, typ),
			"A bearer reference is written only for a pool (swarm@1 or swarm-vm@1); pass the pool's hostname")
	}
	selfID, ok, err := d.env(selfServiceIDKey)
	if err != nil {
		return "", err
	}
	if !ok {
		pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Cannot write the bearer reference of pool '%s': this zcp's env has no %s, so it runs outside a Zerops service", pool, selfServiceIDKey),
			"Lease a pool from the zcp service of the pool's project")
		return "", &LeasePrerequisiteError{PlatformError: pe, cause: prerequisitePoolKey, pool: pool}
	}
	selfName := "this zcp service"
	for _, s := range services {
		if s.ID == selfID {
			selfName = s.Name
		}
	}

	aliasKey := pool + "_" + leaseTokenAliasKey
	apiKey := pool + "_" + leaseTokenKey
	reference := "${" + apiKey + "}"
	own, err := FetchServiceEnv(ctx, d.Client, selfID)
	if err != nil {
		return "", fmt.Errorf("read this zcp service's env: %w", err)
	}
	present, selfReference := false, false
	for _, e := range own {
		switch e.Key {
		case aliasKey:
			if !e.Masked && e.Content != reference {
				pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
					fmt.Sprintf("This zcp service's %s holds something other than %s, and this container's env has no usable bearer for pool '%s'", aliasKey, reference, pool),
					fmt.Sprintf("Delete %s on this zcp service; the next acquire writes it as %s", aliasKey, reference))
				return "", &LeasePrerequisiteError{PlatformError: pe, cause: prerequisitePoolKey, pool: pool}
			}
			present = true
		case apiKey:
			selfReference = !e.Masked && e.Content == reference
		}
	}
	if !present {
		if err := writeBearerReference(ctx, d.Client, selfID, selfName, aliasKey, reference); err != nil {
			return "", budgetSpent(ctx, err, start, "writing "+aliasKey+" on this zcp service")
		}
	}
	if selfReference {
		pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("This zcp service has a variable %s=%s: a variable named like the reference it holds stays the literal text, and while it exists %s stays literal too", apiKey, reference, aliasKey),
			fmt.Sprintf("Delete %s on this zcp service; %s=%s is in place and resolves once it is gone", apiKey, aliasKey, reference))
		return "", &LeasePrerequisiteError{PlatformError: pe, cause: prerequisitePoolKey, pool: pool}
	}
	if poolUndeployed(svc.Status) {
		return "", nil
	}
	return awaitBearer(ctx, d, pool, aliasKey, start)
}

// budgetSpent is err as the failure of what: an API_TIMEOUT naming what and
// the time spent when ctx's deadline ended it, err otherwise.
func budgetSpent(ctx context.Context, err error, start time.Time, what string) error {
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return err
	}
	return platform.NewPlatformError(platform.ErrAPITimeout,
		fmt.Sprintf("Ensuring a pool bearer ran out of time after %s while %s", time.Since(start).Round(time.Second), what),
		"Another acquire may hold the bearer lock or the platform is slow; acquire again")
}

// lockBearer takes pool's bearer lock: a file of its own beside the lease key
// locks, held across the read of zcp's service env, the write and the wait.
func lockBearer(ctx context.Context, stateDir, pool string) (func(), error) {
	if stateDir == "" {
		return nil, errors.New("lock the bearer of pool " + pool + ": no state directory")
	}
	dir := filepath.Join(stateDir, leaseLocksDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create lease lock directory: %w", err)
	}
	release, err := lockLeaseKey(ctx, filepath.Join(dir, "bearer-"+pool+".lock"))
	if err != nil {
		return nil, fmt.Errorf("lock the bearer of pool %s: %w", pool, err)
	}
	return release, nil
}

// writeBearerReference writes key=reference on zcp's service selfID (named
// selfName) and waits for the platform's process. The value is the reference
// text, not a secret. A duplicate key is one zcp's own zerops.yaml bakes in.
func writeBearerReference(ctx context.Context, client platform.Client, selfID, selfName, key, reference string) error {
	proc, err := client.CreateServiceEnvVar(ctx, selfID, key, reference, false)
	if err != nil {
		if hasAPICode(err, apiCodeUserDataDuplicateKey) {
			return yamlOwnedKeyError(key, selfName)
		}
		return fmt.Errorf("write %s on this zcp service: %w", key, err)
	}
	if proc == nil || proc.ID == "" {
		return nil
	}
	done, err := PollProcess(ctx, client, proc.ID, nil)
	if err != nil {
		return fmt.Errorf("write %s on this zcp service: %w", key, err)
	}
	if done.Status != platform.ProcessStatusFinished {
		why := done.Status
		if done.FailReason != nil && *done.FailReason != "" {
			why += ": " + *done.FailReason
		}
		return platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Writing %s on this zcp service ended %s", key, why),
			"The platform's process did not finish; the reference is not in place")
	}
	return nil
}

// awaitBearer waits for pool's bearer to appear in the live env — at most
// bearerResolveWait, never past ctx's deadline — and returns it; otherwise it
// fails naming aliasKey, the time waited and the state last seen.
func awaitBearer(ctx context.Context, d LeaseDeps, pool, aliasKey string, start time.Time) (string, error) {
	waitStart := time.Now()
	deadline := waitStart.Add(bearerResolveWait)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	for {
		// An env that cannot be read now (the platform rewriting it) is read
		// again until the deadline; the state last seen says if it stayed so.
		if token, _, err := liveBearer(d, pool); err == nil && token != "" {
			return token, nil
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", fmt.Errorf("wait for %s to resolve: %w", aliasKey, ctx.Err())
			}
		case <-time.After(min(bearerResolvePoll, time.Until(deadline))):
			continue
		}
		break
	}
	seen := "absent from this container's live env"
	if v, ok, err := d.env(aliasKey); err != nil {
		seen = "unread: " + err.Error()
	} else if ok && unresolvedReference.MatchString(v) {
		seen = "still the literal reference " + v
	}
	return "", platform.NewPlatformError(platform.ErrAPITimeout,
		fmt.Sprintf("%s on this zcp service did not resolve within %s of waiting (%s since the bearer was asked for): it is %s",
			aliasKey, time.Since(waitStart).Round(time.Second), time.Since(start).Round(time.Second), seen),
		fmt.Sprintf("The pool '%s' has been deployed, so the reference normally resolves within about 20 s; the next acquire reads it again", pool))
}
