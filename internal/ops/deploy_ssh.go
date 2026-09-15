package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

const defaultWorkingDir = "/var/www"

// DefaultWorkingDir is the container-side deploy working directory used
// when the caller omits workingDir. Shared with the tools layer.
const DefaultWorkingDir = defaultWorkingDir

// GitIdentity holds user name and email for git commits.
type GitIdentity struct {
	Name  string
	Email string
}

// DeployGitIdentity is the fallback identity ZCP fills into a container's
// /var/www/.git/config when no identity is configured yet (set-if-absent —
// an already-present value, including one the user set, is never
// overwritten). It also identifies the ZCP-internal HEAD-guarantee marker
// commit ("zcp init"), always applied per-invocation via `git -c`, never
// persisted as this constant's own commit — repo-local identity is
// user-owned.
var DeployGitIdentity = GitIdentity{Name: "Zerops Agent", Email: "agent@zerops.io"}

// deploySourceGitLocks serializes git mutation inside each source container's
// shared /var/www tree. Distinct source containers keep running in parallel.
var deploySourceGitLocks keyedMutex

// shellQuote wraps a string in POSIX single quotes, escaping embedded single quotes.
// This prevents shell injection from untrusted input (e.g., user names, emails).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// ShellQuote is the exported form of shellQuote for callers outside this
// package (e.g. tools building SSH commands). Single owner — one quoting
// implementation across the codebase.
func ShellQuote(s string) string { return shellQuote(s) }

type keyedMutex struct {
	locks sync.Map // string -> *sync.Mutex
}

func (m *keyedMutex) lock(ctx context.Context, key string) (func(), error) {
	lockValue, _ := m.locks.LoadOrStore(key, &sync.Mutex{})
	mu, _ := lockValue.(*sync.Mutex)
	acquired := make(chan struct{})
	go func() {
		mu.Lock()
		close(acquired)
	}()
	select {
	case <-acquired:
		return mu.Unlock, nil
	case <-ctx.Done():
		go func() {
			<-acquired
			mu.Unlock()
		}()
		return nil, ctx.Err()
	}
}

// DeploySSH deploys code to a Zerops service via SSH.
//
// Auto-inference:
//   - targetService is required
//   - sourceService == "" → auto-inferred as targetService (self-deploy)
//   - includeGit is derived from the source/target pair: true for self-deploy
//     (the service pushes its own code, .git must stay), false for cross-deploy
//     (dev→stage would otherwise carry the dev container's .git across).
//
// sha, when non-empty, switches the push to a deploy-from-commit: sha is
// resolved and its tree extracted into a fresh temp dir inside the SOURCE
// container (outside workingDir), then pushed from there with
// --version-name <sha>. docs/spec-workflows.md §4.9.
func DeploySSH(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	authInfo auth.Info,
	sourceService string,
	targetService string,
	setup string,
	workingDir string,
	sha string,
) (*DeployResult, error) {
	if sshDeployer == nil {
		return nil, platform.NewPlatformError(
			platform.ErrNotImplemented,
			"SSH deployer not configured",
			"SSH deploy requires a running Zerops container with SSH access",
		)
	}
	if targetService == "" {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"targetService is required",
			"Provide targetService for deploy. Omit sourceService for self-deploy (auto-inferred).",
		)
	}
	if sourceService == "" {
		sourceService = targetService // auto-infer self-deploy
	}
	includeGit := SelfBuildTarget(sourceService, targetService)

	return deploySSH(ctx, client, projectID, sshDeployer, authInfo,
		sourceService, targetService, setup, workingDir, includeGit, sha)
}

func deploySSH(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	authInfo auth.Info,
	sourceService string,
	targetService string,
	setup string,
	workingDir string,
	includeGit bool,
	sha string,
) (*DeployResult, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	source, err := FindService(services, sourceService)
	if err != nil {
		return nil, err
	}

	target, err := FindService(services, targetService)
	if err != nil {
		return nil, err
	}

	// No non-running refusal here (same as DeployLocal): a corrective redeploy
	// of a failed target is non-destructive and gating it deadlocked recovery.
	// See plans/deploy-gate-category-error-2026-06-04.md.

	if workingDir == "" {
		workingDir = defaultWorkingDir
	}
	// Reject mount-style paths — workingDir runs INSIDE the container where
	// /var/www/{hostname} doesn't exist. The correct container path is /var/www.
	if workingDir != defaultWorkingDir && strings.HasPrefix(workingDir, defaultWorkingDir+"/") {
		mountHost := strings.TrimPrefix(workingDir, defaultWorkingDir+"/")
		if i := strings.IndexByte(mountHost, '/'); i >= 0 {
			mountHost = mountHost[:i]
		}
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("workingDir %q looks like a local SSHFS mount path, not a container path. Inside the container, code lives at /var/www", workingDir),
			fmt.Sprintf("Omit workingDir and pass sourceService=%q — the mount /var/www/%s is the source code location, ZCP deploys from it automatically. (Or pass workingDir=\"/var/www\" if you really need to set it.)", mountHost, mountHost),
		)
	}

	// Pre-deploy validation: read zerops.yaml from SSHFS mount (local filesystem).
	// Mount path: /var/www/{sourceService}/ maps to remote /var/www/
	setupName := setup
	if setupName == "" {
		setupName = targetService
	}
	var warnings []string
	mountPath := filepath.Join("/var/www", sourceService)
	serviceType := target.ServiceStackTypeInfo.ServiceStackTypeVersionName
	class := ClassifyDeploy(sourceService, targetService)

	// Self-deploy guard on the sha path (docs/spec-workflows.md §4.9): a
	// self-deploy from a commit would extract sha's tree and push
	// --no-git, shipping no .git and losing the container's repository
	// that the normal path keeps via -g (GLC-2). This must fire
	// BEFORE any SSH round trip — not even the resolve call runs.
	if sha != "" && class == DeployClassSelf {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("self-deploy from a commit sha is not supported for %q: extracting the commit's tree and pushing --no-git would ship no .git, losing the container's own repository", targetService),
			"Deploy the working tree instead (omit sha), or cross-deploy this commit to another service (sourceService != targetService).",
		)
	}

	// Every git round trip against the source's shared /var/www tree —
	// resolve/validate/record reads as well as the final push — is
	// serialized per source so a concurrent batch deploy sharing a
	// source cannot interleave git operations against the same repo
	// (deploySourceGitLocks; TestDeployBatchSSH_SerializesPerSource).
	unlockGit, lockErr := deploySourceGitLocks.lock(ctx, source.Name)
	if lockErr != nil {
		return nil, lockErr
	}
	defer unlockGit()

	// Deploy-from-commit (docs/spec-workflows.md §4.9): sha is resolved and
	// materialized INSIDE the source container, reached over SSH — never
	// the SSHFS mount (docs/spec-mate.md §"git"), so this works whether or
	// not the mount is present. Every step is a discrete SSH round trip;
	// unlike buildSSHCommand's single combined command, correctness here
	// matters more than round-trip count for a first slice.
	//
	// Validation reads whichever tree will actually be pushed: the sha
	// branch validates the COMMIT's zerops.yaml (`git show
	// <sha>:zerops.yaml`) — the SSHFS mount may be missing entirely or
	// stale relative to an older/newer commit than what's on disk right
	// now. The no-sha branch keeps validating the mount, unchanged.
	resolvedSHA := ""
	dirty := false
	var cleanupTemp func()
	gitRunner := git.SSHRunner{Executor: sshDeployer, Hostname: source.Name}
	if sha != "" {
		var newWorkingDir string
		var prepErr error
		resolvedSHA, warnings, newWorkingDir, cleanupTemp, prepErr = deployFromCommitPrep(
			ctx, client, target, gitRunner, source.Name, workingDir, setupName, serviceType, class, sha)
		if prepErr != nil {
			return nil, prepErr
		}
		workingDir = newWorkingDir
	} else {
		if _, statErr := os.Stat(mountPath); statErr == nil {
			var vErr error
			warnings, vErr = ValidateZeropsYml(mountPath, setupName, serviceType, class)
			// DM-2 violation is a hard error — deploy aborts, warnings (if
			// any) travel with the error for visibility but the caller
			// won't issue a push.
			if vErr != nil {
				return nil, vErr
			}
			// Pre-deploy API validation: Zerops checks the full
			// zerops.yaml (field/syntax/version) server-side before we
			// waste a build cycle on a YAML the platform will reject.
			// Any failure — validation, transport, auth — aborts deploy.
			if err := RunPreDeployValidation(ctx, client, target, setupName, mountPath); err != nil {
				return nil, err
			}
		}
		// Record every zcp deploy, not only the sha path (docs/spec-
		// workflows.md §4.9): if the SOURCE has a git repo with a
		// reachable HEAD, record what actually shipped — read-only,
		// before the push, never altering the push command itself. No
		// repo / no HEAD ⇒ no source revision, no warning, no behaviour change.
		if headSHA, isDirty, hasRepo, _ := git.HeadStatus(ctx, gitRunner, workingDir); hasRepo {
			resolvedSHA = headSHA
			dirty = isDirty
		}
	}
	if cleanupTemp != nil {
		defer cleanupTemp()
	}

	var cmd string
	if sha != "" {
		cmd = buildSSHCommandSHA(authInfo, target.ID, workingDir, setup, resolvedSHA)
	} else {
		cmd = buildSSHCommand(authInfo, target.ID, workingDir, setup, includeGit, topology.RuntimeClassFor(serviceType))
	}

	output, err := sshDeployer.ExecSSH(ctx, source.Name, cmd)
	if err != nil {
		if isSSHBuildTriggered(string(output)) {
			// SSH connection dropped after successful zcli push (common exit 255).
			// Build was submitted — let pollDeployBuild take over.
			return &DeployResult{
				Status:            "BUILD_TRIGGERED",
				Mode:              "ssh",
				SourceService:     sourceService,
				TargetService:     targetService,
				TargetServiceID:   target.ID,
				TargetServiceType: target.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				Message:           fmt.Sprintf("Build triggered from %s to %s (SSH session closed after push)", sourceService, targetService),
				MonitorHint:       "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
				Warnings:          warnings,
				SHA:               resolvedSHA,
				Dirty:             dirty,
			}, nil
		}
		return nil, classifySSHError(err, sourceService, targetService)
	}

	return &DeployResult{
		Status:            "BUILD_TRIGGERED",
		Mode:              "ssh",
		SourceService:     sourceService,
		TargetService:     targetService,
		TargetServiceID:   target.ID,
		TargetServiceType: target.ServiceStackTypeInfo.ServiceStackTypeVersionName,
		Message:           fmt.Sprintf("Build triggered from %s to %s via SSH", sourceService, targetService),
		MonitorHint:       "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
		Warnings:          warnings,
		SHA:               resolvedSHA,
		Dirty:             dirty,
	}, nil
}

// deployFromCommitPrep resolves sha, validates the COMMIT's zerops.yaml (never the SSHFS mount, which
// may be missing or stale relative to an arbitrary sha), and extracts the
// commit's tree into a fresh temp dir. Returns the tmpDir as the
// workingDir the caller should push from, plus a cleanup func the caller
// must defer on success. docs/spec-workflows.md §4.9.
func deployFromCommitPrep(
	ctx context.Context,
	client platform.Client,
	target *platform.ServiceStack,
	gitRunner git.SSHRunner,
	sourceName, workingDir, setupName, serviceType string,
	class DeployClass,
	sha string,
) (resolvedSHA string, warnings []string, newWorkingDir string, cleanupTemp func(), err error) {
	resolved, resolveErr := git.ResolveSHA(ctx, gitRunner, workingDir, sha)
	if resolveErr != nil {
		return "", nil, "", nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("sha %q did not resolve to a commit in %s on %s: %v", sha, workingDir, sourceName, resolveErr),
			`Pass a commit sha reachable via "git rev-parse" in the source container's working dir.`,
		)
	}
	resolvedSHA = resolved

	// Same file-name fallback as ParseZeropsYml/DeployLocal: a pre-rename
	// repo carries zerops.yml, and a working-tree deploy accepts it.
	content, showErr := git.ReadFileAtCommit(ctx, gitRunner, workingDir, resolvedSHA, "zerops.yaml")
	if showErr != nil {
		content, showErr = git.ReadFileAtCommit(ctx, gitRunner, workingDir, resolvedSHA, "zerops.yml")
	}
	if showErr != nil {
		return "", nil, "", nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("commit %s has no zerops.yaml (or zerops.yml): %v", resolvedSHA[:min(10, len(resolvedSHA))], showErr),
			"Add zerops.yaml to the commit being deployed, or deploy a different sha.",
		)
	}
	var vErr error
	warnings, vErr = ValidateZeropsYmlContent([]byte(content), setupName, serviceType, class)
	// DM-2 violation is a hard error — deploy aborts, warnings (if any)
	// travel with the error for visibility but the caller won't issue a
	// push.
	if vErr != nil {
		return "", warnings, "", nil, vErr
	}
	// Pre-deploy API validation: Zerops checks the full zerops.yaml
	// (field/syntax/version) server-side before we waste a build cycle on
	// a YAML the platform will reject. Any failure — validation,
	// transport, auth — aborts deploy.
	if err := ValidatePreDeployContent(ctx, client, target, setupName, content); err != nil {
		return "", warnings, "", nil, err
	}

	tmpDir, mkErr := git.MkTempDir(ctx, gitRunner)
	if mkErr != nil {
		return "", warnings, "", nil, fmt.Errorf("create archive tmp dir on %s: %w", sourceName, mkErr)
	}
	cleanup := func() {
		cctx, cancel := cleanupTempCtx(ctx)
		defer cancel()
		_ = git.RemoveTemp(cctx, gitRunner, tmpDir)
	}
	if extractErr := git.ExtractCommitToTemp(ctx, gitRunner, workingDir, resolvedSHA, tmpDir); extractErr != nil {
		cleanup()
		return "", warnings, "", nil, fmt.Errorf("extract commit %s on %s: %w", resolvedSHA, sourceName, extractErr)
	}
	return resolvedSHA, warnings, tmpDir, cleanup, nil
}

func buildSSHCommand(authInfo auth.Info, targetServiceID, workingDir, setup string, includeGit bool, class topology.RuntimeClass) string {
	parts := make([]string, 0, 2)

	// Login to zcli on the remote host.
	loginCmd := fmt.Sprintf("zcli login -- %s", shellQuote(authInfo.Token))
	parts = append(parts, loginCmd)

	// .git lifecycle (GLC-2 / GLC-3). Direct deploy is an ARTIFACT
	// operation — it must reach a state where zcli's `--workspace-state
	// all` archiver can snapshot the (possibly dirty) working tree, without
	// ZCP ever minting a user-visible commit. That archiver's real
	// prerequisites: a repo, a reachable HEAD, and a present identity (any).
	// GitEnsureRepoHeadCommand is the single owner of all three:
	//
	//   1. Bootstrap-mounted service: InitServiceGit already ran post-mount
	//      — .git/, identity, and HEAD all present. Every guard here no-ops.
	//   2. Cold path (migration / `sudo rm -rf /var/www/.git`): no .git/.
	//      The init guard creates it; identity + HEAD are filled fresh.
	//   3. Service provisioned via buildFromGit (or any upstream clone):
	//      .git/ exists but carries the cloning user's identity (or none).
	//      The init guard no-ops; identity is set-if-absent (B13's actual
	//      requirement was "identity exists", not "identity is ZCP's") and
	//      HEAD is filled if the clone happened to be unborn.
	//
	// No `git add` / `git commit` here — zcli ships the tree via an
	// ephemeral stash-archive (no ref moves, no history written); ZCP never
	// touches the user's repo history on a direct deploy.
	gitEnsure := GitEnsureRepoHeadCommand(workingDir, class)

	// Push. setup is an agent-supplied tool input (and recipe-session
	// deploys reach here with meta=nil, bypassing the tools-layer setup
	// resolution), so it's shell-quoted — a setup name with
	// whitespace/metacharacters would otherwise splice the compound command.
	pushArgs := fmt.Sprintf("zcli push --service-id %s", targetServiceID)
	if setup != "" {
		pushArgs += " --setup " + shellQuote(setup)
	}
	if includeGit {
		pushArgs += " -g"
	}

	pushCmd := fmt.Sprintf("%s && %s", gitEnsure, pushArgs)
	parts = append(parts, pushCmd)

	return strings.Join(parts, " && ")
}

// buildSSHCommandSHA builds the push command for a resolved deploy-from-
// commit: extractedDir already holds sha's tree (see ExtractCommitToTemp)
// with no .git of its own, so — unlike buildSSHCommand — there is no
// GitEnsureRepoHeadCommand step and no -g flag; --no-git is unconditional
// and --version-name records the sha the platform can't otherwise see
// (docs/spec-workflows.md §4.9, P1).
func buildSSHCommandSHA(authInfo auth.Info, targetServiceID, extractedDir, setup, sha string) string {
	loginCmd := fmt.Sprintf("zcli login -- %s", shellQuote(authInfo.Token))
	pushArgs := fmt.Sprintf("zcli push --service-id %s --no-git --version-name %s", targetServiceID, shellQuote(sha))
	if setup != "" {
		pushArgs += " --setup " + shellQuote(setup)
	}
	pushCmd := fmt.Sprintf("cd %s && %s", shellQuote(extractedDir), pushArgs)
	return loginCmd + " && " + pushCmd
}
