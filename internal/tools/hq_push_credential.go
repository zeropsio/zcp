package tools

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

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
// runs where the copy is used: before a delivery and before a git-push to HQ.
// The same step re-asserts the repository's persisted credential helper, so a
// pair wired before the Mate's own shell could authenticate heals on its next
// delivery.

// hqCredentialRefusedError is HQ refusing the pair's push credential (401 or
// 403): the pair is marked refused.
type hqCredentialRefusedError struct{ message string }

func (e *hqCredentialRefusedError) Error() string { return e.message }

// hqNotAnsweringError is a delivery step HQ could not serve after its tries,
// line saying so (hqNotAnsweringLine).
type hqNotAnsweringError struct{ line string }

func (e *hqNotAnsweringError) Error() string { return e.line }

// credentialPropagation paces the wait for a credential zcp wrote onto a push
// source to reach its fresh sessions — the platform's env store reaches them
// within seconds (zembed) — which is asked of the container, never of HQ. A
// var so tests narrow it.
var credentialPropagation = struct{ every, within time.Duration }{time.Second, 15 * time.Second}

// hqEnsurePushCredential brings a wired pair's push credential to this Mate's
// current credential and proves a fresh session authenticates with it before
// anything pushes. A copy that already is the current credential costs two
// reads. A credential zcp rewrites is waited for until a fresh session holds
// it, then proved once. A pair an earlier refusal marked (GitPushBroken) is
// checked again and healed the moment its credential works. The proof is a
// delivery step (deliveryRetry): HQ not serving it is an
// hqNotAnsweringError and marks nothing; HQ refusing the credential marks
// the pair and is an hqCredentialRefusedError; anything else is said and
// marks nothing. The caller does not push on any error.
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

	if known && !current {
		if _, err := ops.EnvSetService(ctx, client, serviceID, ops.GitTokenEnvKey, hqc.Credential(), true); err != nil {
			return fmt.Errorf("writing this Mate's current HQ credential onto %s failed (%w)", meta.Hostname, err)
		}
		if err := awaitSessionCredential(ctx, sshDeployer, meta.Hostname, hqc.Credential()); err != nil {
			return err
		}
	}
	output, tries, err := gitAgainstHQ(ctx, sshDeployer, meta.Hostname, ops.BuildGitSessionAuthProbeCommand(meta.RemoteURL, hqc.Address()))
	switch {
	case hqGitAnswer(err, output) != hqAnswered:
		return &hqNotAnsweringError{line: hqNotAnsweringLine(hqc.Address(), fmt.Sprintf("proving %s's credential", meta.Hostname), tries, gitNotServingWords(err, output))}
	case err != nil && ops.GitCredentialRefused(string(output)):
		hqMarkPushRefused(stateDir, meta)
		return &hqCredentialRefusedError{message: withSSHStderr(fmt.Sprintf("HQ refuses this Mate's current credential for %s", meta.Hostname), err)}
	case err != nil:
		return errors.New(withSSHStderr(fmt.Sprintf("proving %s's credential with HQ failed", meta.Hostname), err))
	}
	if marked {
		hqMarkPushWorking(stateDir, meta)
	}
	return nil
}

// awaitSessionCredential waits, as credentialPropagation paces it, until a
// fresh session on hostname holds credential as its GIT_TOKEN — compared by
// digest, so the credential is on no command line.
func awaitSessionCredential(ctx context.Context, sshDeployer ops.SSHDeployer, hostname, credential string) error {
	want := ops.SecretDigest(credential)
	deadline := time.Now().Add(credentialPropagation.within)
	for {
		out, err := sshDeployer.ExecSSH(ctx, hostname, ops.BuildSessionGitTokenDigestCommand())
		if err == nil && strings.TrimSpace(string(out)) == want {
			return nil
		}
		if time.Now().Add(credentialPropagation.every).After(deadline) {
			return fmt.Errorf("the credential written onto %s has not reached its sessions within %s", hostname, credentialPropagation.within)
		}
		if err := waitFor(ctx, credentialPropagation.every); err != nil {
			return fmt.Errorf("waiting for the credential written onto %s to reach its sessions: %w", hostname, err)
		}
	}
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
	err := hqEnsurePushCredential(ctx, client, sshDeployer, projectID, stateDir, hqc, meta)
	if err == nil {
		return nil
	}
	var (
		notAnswering *hqNotAnsweringError
		refused      *hqCredentialRefusedError
		failure      *platform.PlatformError
	)
	switch {
	case errors.As(err, &notAnswering):
		failure = platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push from %s did not run: %s.", input.TargetService, notAnswering.line),
			hqNotAnswering(meta.Hostname, "pushing again"),
		)
	case errors.As(err, &refused):
		failure = platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("git-push from %s did not run: %v. %s is marked as refused.", input.TargetService, err, meta.Hostname),
			"The next push checks the credential against this Mate's current HQ credential again. If HQ keeps refusing it, tell the person: this Mate's credential is HQ's to issue, and no token is to be asked for or made up.",
		)
	default:
		failure = platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push from %s did not run: %v.", input.TargetService, err),
			"Fix the cause named above, then push again.",
		)
	}
	logDeliveryFailure(input.TargetService, failure.Message)
	return failure
}
