package tools

import (
	"context"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// reconcileAdoptedGitPush reflects the LIVE git-push state of just-adopted
// runtimes into their ServiceMeta. Adoption stamps GitPushState="" by default
// (correct for a greenfield service that has no remote yet), but a service that
// was ALREADY git-push-configured OUTSIDE ZCP has a live origin + a GIT_TOKEN
// secret. Re-asserting "unconfigured" makes the launch source-control gate force
// a needless git-push-setup re-run on an already push-capable service — the only
// normal path into the credential-rotation branch that can DESTROY the working
// token (finding #2). This reconciles to the live truth instead: stamp
// GitPushState=configured + RemoteURL ONLY when BOTH a live origin AND a
// GIT_TOKEN secret are present. Reflect-and-report, never fabricate; the value of
// the GIT_TOKEN secret is never read (presence-only). Read failures are
// non-fatal — they degrade to today's unconfigured state, never block adoption.
//
// Container-only: the GIT_TOKEN service-secret signal is a container concept
// (local mode authenticates via the user's local credential helper, not a
// service secret). Returns the hostnames it reconciled, for the agent-facing
// report. The stamp routes through the single owner (workflow.UpdateServiceMeta),
// the same path git-push-setup writes, so the value the launch gate later reads
// derives from one owner.
func reconcileAdoptedGitPush(ctx context.Context, client platform.Client, sshDeployer ops.SSHDeployer, rt runtime.Info, stateDir string, existing []platform.ServiceStack) []string {
	if !rt.InContainer || sshDeployer == nil {
		return nil
	}
	idByHost := make(map[string]string, len(existing))
	for _, s := range existing {
		idByHost[s.Name] = s.ID
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return nil
	}
	var reconciled []string
	for _, m := range metas {
		// Reflect-and-report over every complete runtime meta not already marked
		// configured. Idempotent: an already-configured pair is skipped; a managed
		// dep or any non-runtime has no /var/www/.git origin so the live read
		// returns empty and it's skipped too. Scoped to the live project by the
		// idByHost lookup below.
		if m.GitPushState == topology.GitPushConfigured || !m.IsComplete() {
			continue
		}
		pushHost := m.Hostname
		serviceID := idByHost[pushHost]
		if serviceID == "" {
			continue
		}
		origin, rerr := readGitRemoteURL(ctx, sshDeployer, pushHost)
		if rerr != nil || origin == "" {
			continue // read failure / no remote → leave unconfigured (non-fatal)
		}
		hasToken, terr := ops.EnvHasServiceKey(ctx, client, serviceID, ops.GitTokenEnvKey)
		if terr != nil || !hasToken {
			continue // no GIT_TOKEN secret → not git-push-configured outside ZCP
		}
		// Both live signals present — reflect the configured state. RemoteURL is
		// stored verbatim (every identity compare/emit routes through
		// topology.CanonicalRepoURL elsewhere).
		if uerr := workflow.UpdateServiceMeta(stateDir, pushHost, func(sm *workflow.ServiceMeta) error {
			sm.GitPushState = topology.GitPushConfigured
			sm.RemoteURL = origin
			return nil
		}); uerr == nil {
			reconciled = append(reconciled, pushHost)
		}
	}
	return reconciled
}
