package tools

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A wired pair pushes to HQ with a copy of this Mate's credential:
// git-push-setup writes it onto the push source as the GIT_TOKEN service
// secret, which the dev container's credential helper reads. The credential
// itself is kept in the enrollment on this container (hq.EnrollmentPath), and
// a re-enrollment replaces it — after a redeploy, or once HQ no longer knows
// it — while HQ revokes the one it replaced. Nothing else moves the copy, so
// after that every push and fetch the dev container made would be refused.
//
// hqEnsurePushCredential keeps the copy at the current credential, and it
// runs where the copy is used: before a delivery, before a git-push to HQ,
// and before a pass finishes a delivery owed. The same step re-asserts the
// repository's persisted credential helper, so a pair wired before the Mate's
// own shell could authenticate heals on its next delivery.

// hqEnsurePushCredential brings a wired pair's push credential to this Mate's
// current credential and proves a fresh session authenticates with it before
// anything pushes. A copy that already is the current credential costs two
// reads. A pair an earlier refusal marked (GitPushBroken) is checked again
// and healed the moment its credential works. A credential that still does
// not work marks the pair and is returned as an error, never silently: the
// caller does not push.
//
// A pair that is not wired, or a remote of the user's own, is left alone. A
// push source whose variables cannot be read is left alone too, while the
// pair is not marked: the push then proves the credential itself, and a
// refusal marks it (hqMarkPushRefused).
func hqEnsurePushCredential(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	projectID, stateDir string,
	hqc hq.Client,
	meta *workflow.ServiceMeta,
) error {
	if client == nil || sshDeployer == nil || !hqPairWired(meta) || !ops.IsHQRemote(meta.RemoteURL, hqc.Address()) {
		return nil
	}
	_, _ = sshDeployer.ExecSSH(ctx, meta.Hostname, ops.BuildGitCredentialHelperAssertCommand(hqPairWorkingDir, meta.RemoteURL, hqc.Address()))
	marked := meta.GitPushState == topology.GitPushBroken
	serviceID, held, known := heldPushCredential(ctx, client, projectID, meta.Hostname)
	current := known && subtle.ConstantTimeCompare([]byte(held), []byte(hqc.Credential())) == 1
	if !marked && (current || !known) {
		return nil
	}

	var proveErr error
	if known && !current {
		if _, err := ops.EnvSetService(ctx, client, serviceID, ops.GitTokenEnvKey, hqc.Credential(), true); err != nil {
			hqMarkPushRefused(stateDir, meta)
			return fmt.Errorf("writing this Mate's current HQ credential onto %s failed (%w)", meta.Hostname, err)
		}
		// A fresh session sees the new secret within seconds (zembed), so the
		// probe waits across that window the way git-push-setup does.
		proveErr = gitPushSessionAuthVerify(ctx, sshDeployer, meta.Hostname, meta.RemoteURL, hqc.Address())
	} else {
		// Nothing was written, so there is no window to wait out: one fresh
		// session answers whether the credential works now.
		_, proveErr = sshDeployer.ExecSSH(ctx, meta.Hostname, ops.BuildGitSessionAuthProbeCommand(meta.RemoteURL, hqc.Address()))
	}
	if proveErr != nil {
		hqMarkPushRefused(stateDir, meta)
		return errors.New(withSSHStderr(fmt.Sprintf("HQ refuses this Mate's current credential for %s", meta.Hostname), proveErr))
	}
	if marked {
		hqMarkPushWorking(stateDir, meta)
	}
	return nil
}

// heldPushCredential is the copy of the credential the push source holds,
// with the service it is on. known is false when the platform could not be
// read; a push source with no copy at all is known, and holds "".
func heldPushCredential(ctx context.Context, client platform.Client, projectID, hostname string) (serviceID, held string, known bool) {
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

// hqMarkPushRefused marks a pair whose push credential HQ refused, in memory
// and on disk — the same mark a plain git-push leaves
// (degradeGitPushStateToBroken), so every surface that reads the pair's state
// sees it.
func hqMarkPushRefused(stateDir string, meta *workflow.ServiceMeta) {
	degradeGitPushStateToBroken(stateDir, meta.Hostname)
	if meta.GitPushState == topology.GitPushConfigured {
		meta.GitPushState = topology.GitPushBroken
	}
}

// hqMarkPushWorking clears the mark once a fresh session authenticated with
// the pair's credential again. Best-effort on disk: a pass that could not
// write it probes once more next time.
func hqMarkPushWorking(stateDir string, meta *workflow.ServiceMeta) {
	meta.GitPushState = topology.GitPushConfigured
	_ = workflow.UpdateServiceMeta(stateDir, meta.Hostname, func(m *workflow.ServiceMeta) error {
		if m.GitPushState != topology.GitPushBroken {
			return workflow.ErrSkipWrite
		}
		m.GitPushState = topology.GitPushConfigured
		return nil
	})
}

// hqPushCredentialPreflight runs the credential step for a git-push of a
// wired pair to this Mate's HQ, and answers the refusal when the credential
// does not work — nil when there is nothing to check or it works.
func hqPushCredentialPreflight(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	projectID, stateDir string,
	input DeploySSHInput,
) *platform.PlatformError {
	meta, _ := workflow.FindServiceMeta(stateDir, input.TargetService)
	if !hqPairWired(meta) {
		return nil
	}
	hqc, enrolled := openHQ(httpClient)
	if !enrolled || !ops.IsHQRemote(resolveEffectiveRemote(stateDir, input.TargetService, input.RemoteURL), hqc.Address()) {
		return nil
	}
	if err := hqEnsurePushCredential(ctx, client, sshDeployer, projectID, stateDir, hqc, meta); err != nil {
		return platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("git-push from %s did not run: %v. %s is marked as refused.", input.TargetService, err, meta.Hostname),
			"The next push checks the credential against this Mate's current HQ credential again. If HQ keeps refusing it, tell the person: this Mate's credential is HQ's to issue, and no token is to be asked for or made up.",
		)
	}
	return nil
}
