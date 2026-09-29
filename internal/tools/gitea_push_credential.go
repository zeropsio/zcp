package tools

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A wired pair pushes to the group's Gitea with a copy of this Mate's bot
// token: git-push-setup writes it onto the push source as the GIT_TOKEN
// service secret, which the dev container's credential helper reads. The token
// itself arrives on this container as GITEA_TOKEN, and the broker rotates it —
// a new generation written there, older ones revoked once the held one is ten
// minutes old (gitea-mate's DefaultTokenGrace). Nothing moved the copy, so
// after a rotation every push and fetch the dev container made was refused.
//
// giteaEnsurePushCredential keeps the copy at the current token, and it runs
// where the copy is used: before a delivery and before a git-push to the
// group's Gitea. The Gitea reconcile cannot be the one to notice — it runs
// when bootstrap or adopt completes, never in the develop loop.

// giteaEnsurePushCredential brings a wired pair's push credential to this
// Mate's current Gitea token and proves a fresh session authenticates with it
// before anything pushes. A copy that already is the current token costs two
// reads. A pair an earlier refusal marked (GitPushBroken) is checked again and
// healed the moment its credential works. A credential that still does not
// work marks the pair and is returned as an error, never silently: the caller
// does not push.
//
// A container with no Gitea wiring, a pair that is not wired, or a remote of
// the user's own is left alone. A push source whose variables cannot be read
// is left alone too, while the pair is not marked: the push then proves the
// credential itself, and a refusal marks it (giteaMarkPushRefused).
func giteaEnsurePushCredential(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	projectID, stateDir string,
	wiring ops.GiteaWiring,
	meta *workflow.ServiceMeta,
) error {
	if client == nil || sshDeployer == nil || !wiring.Ready() || meta == nil || meta.Gitea == nil || meta.Gitea.FullName == "" {
		return nil
	}
	if topology.ClassifyGitHost(meta.RemoteURL, wiring.GiteaURL) != topology.GitHostGitea {
		return nil
	}
	marked := meta.GitPushState == topology.GitPushBroken
	serviceID, held, known := giteaHeldPushToken(ctx, client, projectID, meta.Hostname)
	current := known && subtle.ConstantTimeCompare([]byte(held), []byte(wiring.Token)) == 1
	if !marked && (current || !known) {
		return nil
	}

	var proveErr error
	if known && !current {
		if _, err := ops.EnvSetService(ctx, client, serviceID, ops.GitTokenEnvKey, wiring.Token, true); err != nil {
			giteaMarkPushRefused(stateDir, meta)
			return fmt.Errorf("writing this Mate's current Gitea token onto %s failed (%w)", meta.Hostname, err)
		}
		// A fresh session sees the new secret within seconds (zembed), so
		// the probe waits across that window the way git-push-setup does.
		proveErr = gitPushSessionAuthVerify(ctx, sshDeployer, meta.Hostname, meta.RemoteURL)
	} else {
		// Nothing was written, so there is no window to wait out: one
		// fresh session answers whether the credential works now.
		_, proveErr = sshDeployer.ExecSSH(ctx, meta.Hostname, ops.BuildGitSessionAuthProbeCommand(meta.RemoteURL))
	}
	if proveErr != nil {
		giteaMarkPushRefused(stateDir, meta)
		return errors.New(withSSHStderr(fmt.Sprintf("Gitea refuses this Mate's current token for %s", meta.Hostname), proveErr))
	}
	if marked {
		giteaMarkPushWorking(stateDir, meta)
	}
	return nil
}

// giteaHeldPushToken is the copy of the token the push source holds, with the
// service it is on. known is false when the platform could not be read; a
// push source with no copy at all is known, and holds "".
func giteaHeldPushToken(ctx context.Context, client platform.Client, projectID, hostname string) (serviceID, held string, known bool) {
	svc, err := ops.LookupService(ctx, client, projectID, hostname)
	if err != nil || svc == nil {
		return "", "", false
	}
	envs, err := ops.FetchServiceEnv(ctx, client, svc.ID)
	if err != nil {
		return svc.ID, "", false
	}
	for _, env := range envs {
		if env.Key == ops.GitTokenEnvKey {
			return svc.ID, env.Content, true
		}
	}
	return svc.ID, "", true
}

// giteaMarkPushRefused marks a pair whose push credential Gitea refused, in
// memory and on disk — the same mark a plain git-push leaves
// (degradeGitPushStateToBroken), so every surface that reads the pair's state
// sees it.
func giteaMarkPushRefused(stateDir string, meta *workflow.ServiceMeta) {
	degradeGitPushStateToBroken(stateDir, meta.Hostname)
	if meta.GitPushState == topology.GitPushConfigured {
		meta.GitPushState = topology.GitPushBroken
	}
}

// giteaMarkPushWorking clears the mark once a fresh session authenticated with
// the pair's credential again. Best-effort on disk: a pass that could not
// write it probes once more next time.
func giteaMarkPushWorking(stateDir string, meta *workflow.ServiceMeta) {
	meta.GitPushState = topology.GitPushConfigured
	_ = workflow.UpdateServiceMeta(stateDir, meta.Hostname, func(m *workflow.ServiceMeta) error {
		if m.GitPushState != topology.GitPushBroken {
			return workflow.ErrSkipWrite
		}
		m.GitPushState = topology.GitPushConfigured
		return nil
	})
}

// giteaPushCredentialPreflight runs the credential step for a git-push of a
// wired pair to this Mate's Gitea, and answers the refusal when the
// credential does not work — nil when there is nothing to check or it works.
func giteaPushCredentialPreflight(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	projectID, stateDir string,
	input DeploySSHInput,
) *platform.PlatformError {
	meta, _ := workflow.FindServiceMeta(stateDir, input.TargetService)
	if meta == nil || meta.Gitea == nil {
		return nil
	}
	if !giteaRemoteOfThisMate(resolveEffectiveRemote(stateDir, input.TargetService, input.RemoteURL)) {
		return nil
	}
	wiring := ops.ReadGiteaWiring(giteaEnvLookup(mate.LiveEnvStorePath))
	if err := giteaEnsurePushCredential(ctx, client, sshDeployer, projectID, stateDir, wiring, meta); err != nil {
		return platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("git-push from %s did not run: %v. %s is marked as refused.", input.TargetService, err, meta.Hostname),
			"The next push checks the credential against this Mate's current Gitea token again. If Gitea keeps refusing it, tell the person: this Mate's token is the broker's to deliver, and no token is to be asked for or made up.",
		)
	}
	return nil
}
