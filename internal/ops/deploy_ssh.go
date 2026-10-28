package ops

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

const defaultWorkingDir = "/var/www"

// DefaultWorkingDir is the container-side deploy working directory used
// when the caller omits workingDir. Shared with the tools layer.
const DefaultWorkingDir = defaultWorkingDir

// SourceMountDir is where zcp's mounts under root show sourceService's
// container directory workingDir: `<root>/<source>` for /var/www (or no
// workingDir), the same folder below it for a workingDir under /var/www — a
// variant-B code volume (docs/spec-mate.md §2.8a).
func SourceMountDir(root, sourceService, workingDir string) string {
	rel, under := strings.CutPrefix(filepath.Clean(workingDir), defaultWorkingDir+"/")
	if workingDir == "" || !under {
		return filepath.Join(root, sourceService)
	}
	return filepath.Join(root, sourceService, rel)
}

// mountStyleWorkingDir reports whether workingDir under /var/www names zcp's
// local mount of a service rather than a folder in the container, with the
// hostname it names. A folder under /var/www is a container path only in a
// project holding a local-storage volume, and only when the folder is no
// service's hostname — a variant-B code volume mounted there.
func mountStyleWorkingDir(services []platform.ServiceStack, workingDir string) (string, bool) {
	rel, under := strings.CutPrefix(workingDir, defaultWorkingDir+"/")
	if !under {
		return "", false
	}
	mountHost, _, _ := strings.Cut(rel, "/")
	hasVolume := false
	for _, svc := range services {
		if svc.Name == mountHost {
			return mountHost, true
		}
		if topology.IsLocalStorageType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			hasVolume = true
		}
	}
	return mountHost, !hasVolume
}

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
	return DeploySSHWithLeases(ctx, client, projectID, sshDeployer, authInfo, "",
		sourceService, targetService, setup, workingDir, sha)
}

// DeploySSHWithLeases is DeploySSH for a project whose state directory may
// hold leases (docs/spec-workflows.md §8 DP): a sourceService that is the
// alias of a lease zcp holds a record for (LeaseTarget) is a pool container,
// not a service — it is never looked up in the project, the push runs inside
// that container over its alias, and the git lock and the zerops.yaml
// validation (read through its mount /var/www/<alias>) are keyed on the
// alias. A stateDir of "" resolves no lease, which is DeploySSH.
func DeploySSHWithLeases(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	authInfo auth.Info,
	stateDir string,
	sourceService string,
	targetService string,
	setup string,
	workingDir string,
	sha string,
) (*DeployResult, error) {
	return DeploySSHFromListing(ctx, client, projectID, sshDeployer, authInfo, stateDir, nil,
		sourceService, targetService, setup, workingDir, sha)
}

// DeploySSHFromListing is DeploySSHWithLeases resolving its source and target
// from services — the project's services as one listing the caller already
// read for its own checks — rather than a listing of its own (each is an
// ES-backed search, docs/spec-workflows.md §3.5). nil services: it lists them.
func DeploySSHFromListing(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	authInfo auth.Info,
	stateDir string,
	services []platform.ServiceStack,
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
	lease, isLease, err := LeaseTarget(stateDir, sourceService)
	if err != nil {
		return nil, fmt.Errorf("resolve lease %s: %w", sourceService, err)
	}
	var leaseSource *LeaseTargetInfo
	if isLease {
		if lease.Parked {
			// Nothing answers on a parked lease (DP-28).
			return nil, LeaseParkedError(lease, "a deploy from it")
		}
		leaseSource = &lease
	}

	return deploySSH(ctx, client, projectID, sshDeployer, authInfo, services,
		sourceService, targetService, setup, workingDir, includeGit, sha, leaseSource)
}

// RefusePoolSource refuses a deploy whose source service is a Swarm pool: a
// pool's own hostname reaches a random one of its containers (PS-3), so a
// push from it would write git state into, and build from, whichever
// container answered — possibly someone's lease. A lease alias is the source
// instead; it is never a service, so it never reaches here.
func RefusePoolSource(source *platform.ServiceStack, targetService string) error {
	if source == nil || !topology.IsPoolType(source.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return nil
	}
	return platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("'%s' is a Swarm pool: its hostname reaches a random container, so a deploy never runs from it", source.Name),
		fmt.Sprintf("Deploy from a lease of the pool: zerops_deploy sourceService=\"<lease alias>\" targetService=%q (zerops_lease action=status lists your leases; zerops_lease action=acquire pool=%q takes one)", targetService, source.Name))
}

func deploySSH(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	authInfo auth.Info,
	services []platform.ServiceStack,
	sourceService string,
	targetService string,
	setup string,
	workingDir string,
	includeGit bool,
	sha string,
	lease *LeaseTargetInfo,
) (*DeployResult, error) {
	if services == nil {
		listed, err := client.ListServices(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("list services: %w", err)
		}
		services = listed
	}

	sourceName, target, err := resolveDeployHosts(services, sourceService, targetService, lease)
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
	if mountHost, mountStyle := mountStyleWorkingDir(services, workingDir); mountStyle {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("workingDir %q looks like a local SSHFS mount path, not a container path. Inside the container, code lives at /var/www", workingDir),
			fmt.Sprintf("Omit workingDir and pass sourceService=%q — the mount /var/www/%s is the source code location, ZCP deploys from it automatically. (Or pass workingDir=\"/var/www\" if you really need to set it.)", mountHost, mountHost),
		)
	}

	// Pre-deploy validation: read zerops.yaml from SSHFS mount (local filesystem).
	// Mount path: /var/www/{sourceService}/ maps to remote /var/www/, so a
	// workingDir on a code volume under it reads from the same folder below.
	setupName := setup
	if setupName == "" {
		setupName = targetService
	}
	var warnings []string
	mountPath := SourceMountDir(defaultWorkingDir, sourceService, workingDir)
	serviceType := target.ServiceStackTypeInfo.ServiceStackTypeVersionName
	class := ClassifyDeploy(sourceService, targetService)
	toPool := topology.IsPoolType(serviceType)
	var poolLease string
	if lease != nil && lease.Pool == target.Name {
		poolLease = sourceService
	}
	// A stage tracks main (DP-8): a deploy from a lease to the part's stage
	// fetches origin/main before it resolves what ships, and says when that
	// is not on it. Every other deploy fetches nothing and says nothing more.
	stageMain := newLeaseStageMain(lease, target.Name, serviceType)

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
	// resolve/validate/record reads, the repo/HEAD ensure, the crew save and
	// zcli's archive and upload — is serialized per source so a concurrent
	// batch deploy sharing a source cannot interleave git operations against
	// the same repo: zcli's archiver writes a stash commit and then runs
	// `git prune`, which would drop another archiver's unreferenced commit
	// mid-archive. The wait on the build holds no git state and runs
	// outside the lock (deploySourceGitLocks;
	// TestDeployBatchSSH_SameSourceBuildsOverlap_GitSectionExclusive).
	unlockGit, lockErr := deploySourceGitLocks.lock(ctx, sourceName)
	if lockErr != nil {
		return nil, lockErr
	}
	unlockGit = sync.OnceFunc(unlockGit)
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
	var resolvedSHA, branch string
	var dirty bool
	var notCarried *git.NotCarried
	var envFiles []string
	var repoState string
	var cleanupTemp func()
	gitRunner := git.SSHRunner{Executor: sshDeployer, Hostname: sourceName}
	sourceWorkingDir := workingDir
	stageMain.fetch(ctx, gitRunner, sourceWorkingDir)
	if sha != "" {
		var newWorkingDir string
		var prepErr error
		resolvedSHA, warnings, newWorkingDir, cleanupTemp, prepErr = deployFromCommitPrep(
			ctx, client, target, gitRunner, sourceName, workingDir, setupName, serviceType, class, sha)
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
		head, headErr := readWorkingTreeHead(ctx, gitRunner, workingDir, class, sourceName, target.Name)
		if headErr != nil {
			return nil, headErr
		}
		resolvedSHA, branch, dirty = head.sha, head.branch, head.dirty
		repoState, notCarried, envFiles = head.repoState, head.notCarried, head.envFiles
		warnings = append(warnings, head.warnings...)
	}
	if cleanupTemp != nil {
		defer cleanupTemp()
	}
	stageMain.check(ctx, gitRunner, sourceWorkingDir, resolvedSHA)

	var versionName string
	// uploaded is the push's output up to its upload, when the build is
	// awaited in a call of its own; output is the call that ends with the build.
	var uploaded, output []byte
	if sha != "" {
		headSHA, headBranch, _, _, _ := git.HeadStatus(ctx, gitRunner, sourceWorkingDir)
		versionName = commitVersionName(resolvedSHA, headSHA, headBranch)
		// The commit's tree is already extracted and the push reads no git.
		unlockGit()
		output, err = execPush(ctx, sshDeployer, sourceName, buildSSHCommandSHA(target.ID, workingDir, setup, versionName), pushKeyInput(authInfo))
	} else {
		// GF-10: a plain working-tree deploy also passes --version-name
		// when the source has a reachable HEAD — its branch and short
		// sha, with a "-dirty" suffix on top of an uncommitted working
		// tree (versionNameFor).
		versionName = versionNameFor(branch, resolvedSHA, dirty)
		pushDir := "/tmp/zcp-push-" + rand.Text()
		uploaded, output, err = pushWorkingTree(ctx, sshDeployer, sourceName, pushDir,
			buildSSHCommand(target.ID, workingDir, setup, includeGit, versionName, pushDir), pushKeyInput(authInfo), unlockGit)
	}
	// What the upload carried, as the push measured it (DM-9); a session
	// dropped after the build started leaves it in the push's own output.
	upload, _ := splitUploadReport(string(uploaded) + string(output))
	outcome := sshDeployOutcome{
		sourceService: sourceService, targetService: targetService, target: target,
		resolvedSHA: resolvedSHA, versionName: versionName, dirty: dirty,
		warnings:   withUploadWarning(stageMain.withNote(warnings, versionName, dirty), upload.size),
		notCarried: notCarried, envFiles: envFiles, repoState: repoState, toPool: toPool, poolLease: poolLease,
		upload: upload.size,
	}
	if err != nil {
		// A self-deploy's build replaces the source, dropping the wait's
		// session: the upload's output still says the build started.
		if isSSHBuildTriggered(string(uploaded) + string(output)) {
			// SSH connection dropped after successful zcli push (common exit 255).
			// Build was submitted — let pollDeployBuild take over.
			return outcome.result(fmt.Sprintf("Build triggered from %s to %s (SSH session closed after push)", sourceService, targetService)), nil
		}
		return nil, classifySSHError(err, sourceService, targetService)
	}

	return outcome.result(fmt.Sprintf("Build triggered from %s to %s via SSH", sourceService, targetService)), nil
}

// pushWorkingTree runs a working-tree push in pushDir (buildSSHCommand) and,
// once zcli said it is deploying, awaits its build in a call of its own:
// uploaded is the push's output up to its upload, output the call that ended
// last. unlockGit runs once zcli's git work is done or stopped.
func pushWorkingTree(ctx context.Context, sshDeployer SSHDeployer, sourceName, pushDir, command string, key []byte, unlockGit func()) (uploaded, output []byte, err error) {
	output, err = execPush(ctx, sshDeployer, sourceName, command, key)
	if err != nil && !isSSHBuildTriggered(string(output)) {
		// The detached zcli outlives the failed call and would still ask for
		// the build; the git lock is held until it is stopped.
		output = append(output, stopPush(ctx, sshDeployer, sourceName, pushDir)...)
	}
	unlockGit()
	if err != nil {
		return nil, output, err
	}
	uploaded = output
	output, err = sshDeployer.ExecSSH(ctx, sourceName, buildWaitCommand(pushDir))
	return uploaded, output, err
}

// resolveDeployHosts is the host a deploy's push runs on and its target
// service. The host is the source service's hostname, or a lease's alias — a
// pool container is no service of the project, so the alias is never looked
// up (DP-1). A pool named as the source is refused (PS-3).
func resolveDeployHosts(services []platform.ServiceStack, sourceService, targetService string, lease *LeaseTargetInfo) (string, *platform.ServiceStack, error) {
	sourceName := sourceService
	if lease == nil {
		source, err := FindService(services, sourceService)
		if err != nil {
			return "", nil, err
		}
		if err := RefusePoolSource(source, targetService); err != nil {
			return "", nil, err
		}
		sourceName = source.Name
	}
	target, err := FindService(services, targetService)
	if err != nil {
		return "", nil, err
	}
	return sourceName, target, nil
}

// sshDeployOutcome is what a triggered SSH deploy reports of itself: the
// result is the same whether the push call returned or its session dropped
// after the build was submitted, only its message differs.
type sshDeployOutcome struct {
	sourceService, targetService string
	target                       *platform.ServiceStack
	resolvedSHA, versionName     string
	dirty                        bool
	warnings                     []string
	notCarried                   *git.NotCarried
	envFiles                     []string
	repoState                    string
	// toPool is a deploy whose target is a pool: the result says the build
	// reaches only containers the pool creates after it.
	toPool bool
	// poolLease is the source's alias when it is a lease of that pool.
	poolLease string
	// upload is what the push uploaded, nil when it measured nothing.
	upload *UploadSize
}

// result is the BUILD_TRIGGERED DeployResult carrying message.
func (o sshDeployOutcome) result(message string) *DeployResult {
	if o.toPool {
		message += PoolDeployNote(o.target.Name, o.poolLease)
	}
	return &DeployResult{
		Status:            "BUILD_TRIGGERED",
		Mode:              "ssh",
		SourceService:     o.sourceService,
		TargetService:     o.targetService,
		TargetServiceID:   o.target.ID,
		TargetServiceType: o.target.ServiceStackTypeInfo.ServiceStackTypeVersionName,
		Message:           appendPreflightNote(message, o.notCarried, o.envFiles),
		MonitorHint:       "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
		Warnings:          o.warnings,
		SHA:               o.resolvedSHA,
		Dirty:             o.dirty,
		VersionName:       o.versionName,
		NotCarried:        o.notCarried,
		EnvFiles:          o.envFiles,
		RepoState:         o.repoState,
		Upload:            o.upload,

		ReachesNewContainersOnly: o.toPool,
	}
}

// workingTreeHead is what a working-tree deploy records of its source's
// repository: the HEAD it shipped, the branch it is on, whether uncommitted
// changes rode on top, and the self-deploy preflight's facts.
type workingTreeHead struct {
	sha, branch string
	dirty       bool
	repoState   string
	notCarried  *git.NotCarried
	envFiles    []string
	warnings    []string
}

// readWorkingTreeHead records every zcp deploy, not only the sha path (docs/
// spec-workflows.md §4.9): if the SOURCE has a git repo with a reachable
// HEAD, what actually shipped — read-only, before the push, never altering
// the push command itself. No repo / no HEAD ⇒ no source revision, no
// warning, no behaviour change.
//
// Self-deploy additionally runs the combined GF-12 preflight (docs/
// spec-workflows.md §8 DM, §12.6): notCarried/envFiles/repoState facts,
// plus the two hard refusals — never a gate on the cross-deploy path, whose
// target's own repo shape a distinct source doesn't inherit.
func readWorkingTreeHead(ctx context.Context, gitRunner git.SSHRunner, workingDir string, class DeployClass, sourceName, targetName string) (workingTreeHead, error) {
	var head workingTreeHead
	if class == DeployClassSelf {
		var err error
		head.sha, head.branch, head.dirty, head.repoState, head.notCarried, head.envFiles, err = selfDeployPreflightFacts(ctx, gitRunner, workingDir, sourceName)
		return head, err
	}
	sha, branch, dirty, hasRepo, _ := git.HeadStatus(ctx, gitRunner, workingDir)
	if !hasRepo {
		return head, nil
	}
	head.sha, head.branch, head.dirty = sha, branch, dirty
	// GF-5: a dirty cross-deploy shipped code that isn't reproducible from
	// git alone — say so where the agent will see it. Self-deploy is
	// excluded by construction; the explicit-sha path never reaches here.
	if dirty {
		head.warnings = append(head.warnings,
			dirtyCrossDeployWarning(targetName, sourceName, sha)+" "+uncommittedIn(ctx, gitRunner, workingDir, sourceName))
	}
	return head, nil
}

// uncommittedShown is how many uncommitted paths a dirty warning names.
const uncommittedShown = 10

// uncommittedIn names what made a cross-deploy dirty and the directory it was
// read in: the agent's own `git status` may have run in another tree — the
// deploy copy beside a code volume — and found it clean.
func uncommittedIn(ctx context.Context, gitRunner git.Runner, workingDir, sourceName string) string {
	where := fmt.Sprintf("Uncommitted in %s on %s", workingDir, sourceName)
	out, _, err := gitRunner.Run(ctx, workingDir, fmt.Sprintf("GIT_OPTIONAL_LOCKS=0 git status --porcelain | head -n %d", uncommittedShown+1))
	if err != nil {
		return where + "."
	}
	var paths []string
	for line := range strings.Lines(out) {
		if line = strings.TrimRight(line, "\r\n"); len(line) > 3 {
			paths = append(paths, line[3:])
		}
	}
	switch {
	case len(paths) == 0:
		return where + "."
	case len(paths) > uncommittedShown:
		paths = append(paths[:uncommittedShown], "and more")
	}
	return fmt.Sprintf("%s: %s.", where, strings.Join(paths, ", "))
}

// selfDeployPreflightFacts runs the combined GF-12 self-deploy preflight
// (docs/spec-workflows.md §8 DM-7/DM-8, §12.6) and turns it into the
// values deploySSH's no-sha branch needs: the recorded HEAD sha/dirty
// flag (same facts HeadStatus would report), repoState, notCarried, and
// envFiles. Returns a *platform.PlatformError (as a plain error, so the
// caller can return it directly) for the two hard refusals — GitIsFile /
// HasSubmodules — and nil otherwise; the read itself never fails a plain
// self-deploy (facts, never a gate).
func selfDeployPreflightFacts(ctx context.Context, gitRunner git.SSHRunner, workingDir, sourceName string) (
	resolvedSHA, branch string, dirty bool, repoState string, notCarried *git.NotCarried, envFiles []string, err error,
) {
	preflight, _ := git.ReadSelfDeployPreflight(ctx, gitRunner, workingDir)
	if preflight.GitIsFile {
		return "", "", false, "", nil, nil, platform.NewPlatformError(
			platform.ErrGitWorktreeUnsupported,
			fmt.Sprintf("%s's .git is a regular file (a linked-worktree or submodule gitdir pointer), not a directory — a self-deploy archive carries only that pointer file, producing a broken repository in the replacement container", sourceName),
			"This container can't self-deploy from a linked worktree or submodule checkout. Replace .git with a real repository (a fresh `git clone`, or copy the real gitdir contents in place of the pointer), or cross-deploy a resolved commit to a different target instead.",
		)
	}
	if preflight.HasSubmodules {
		return "", "", false, "", nil, nil, platform.NewPlatformError(
			platform.ErrGitSubmodulesUnsupported,
			fmt.Sprintf("%s has a .gitmodules file — a self-deploy archive (git archive, no --recurse-submodules) ships submodule directories EMPTY, so the replacement container would start without that code", sourceName),
			"Vendor the submodule's content directly into the parent repository (remove .gitmodules, commit the files), or deploy a resolved commit sha to a different (cross) target instead.",
		)
	}
	if preflight.HasRepo {
		resolvedSHA = preflight.SHA
		branch = preflight.Branch
		dirty = preflight.Dirty
	}
	repoState = preflight.RepoState
	envFiles = preflight.EnvFiles
	if preflight.NotCarried.Count > 0 {
		nc := preflight.NotCarried
		notCarried = &nc
	}
	return resolvedSHA, branch, dirty, repoState, notCarried, envFiles, nil
}

// appendPreflightNote appends the GF-12 not-carried/env-files sentence to
// message when the self-deploy preflight (docs/spec-workflows.md §8 DM,
// §12.6) found anything to flag. Returns message unchanged when notCarried
// is nil (or empty) and envFiles is empty — including every cross-deploy,
// which never runs the preflight at all.
func appendPreflightNote(message string, notCarried *git.NotCarried, envFiles []string) string {
	var parts []string
	if notCarried != nil && notCarried.Count > 0 {
		parts = append(parts, fmt.Sprintf("%d ignored path(s) will not survive this deploy", notCarried.Count))
	}
	if len(envFiles) > 0 {
		parts = append(parts, fmt.Sprintf(".env file(s) present (%s)", strings.Join(envFiles, ", ")))
	}
	if len(parts) == 0 {
		return message
	}
	return message + " — " + strings.Join(parts, "; ") + "; configuration belongs in service env vars"
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
	source := zeropsYmlPrimary
	content, showErr := git.ReadFileAtCommit(ctx, gitRunner, workingDir, resolvedSHA, source)
	if showErr != nil {
		source = zeropsYmlFallback
		content, showErr = git.ReadFileAtCommit(ctx, gitRunner, workingDir, resolvedSHA, source)
	}
	if showErr != nil {
		return "", nil, "", nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("commit %s has no zerops.yaml (or zerops.yml): %v", resolvedSHA[:min(10, len(resolvedSHA))], showErr),
			"Add zerops.yaml to the commit being deployed, or deploy a different sha.",
		)
	}
	var vErr error
	warnings, vErr = ValidateZeropsYmlContent([]byte(content), source, setupName, serviceType, class)
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

// versionNameFor is the --version-name a zcp-driven working-tree build
// passes (GF-10, docs/spec-workflows.md §12.6), the name the platform keeps
// on the app version and the Mate app and HQ read back: a label and
// the commit's short sha, exactly two tokens — "main 7e2d4c1". The label is
// the branch HEAD is on, or "HEAD" when it is on none (detached; a branch
// name with whitespace, which git refuses anyway, counts as none) — git
// refuses a branch named HEAD, so it never reads as one. A "-dirty" suffix on
// the sha marks uncommitted changes, which every reader takes for no commit.
// "" when sha is empty (no reachable HEAD), so the caller omits the flag.
// Never itself a claim about what was deployed (GF-5).
func versionNameFor(branch, sha string, dirty bool) string {
	if sha == "" {
		return ""
	}
	if branch == "" || strings.ContainsAny(branch, " \t\n") {
		branch = "HEAD"
	}
	name := branch + " " + shortSHA(sha)
	if dirty {
		name += "-dirty"
	}
	return name
}

// commitVersionName is a deploy-from-commit's --version-name: the checked-out
// branch when sha is its tip, else "commit" — a label for people; every
// reader takes the short sha for the commit whatever it says. Never dirty:
// it ships exactly sha's tree. "" when sha is empty.
func commitVersionName(sha, headSHA, headBranch string) string {
	if sha == "" {
		return ""
	}
	label := "commit"
	if headBranch != "" && !strings.ContainsAny(headBranch, " \t\n") && strings.EqualFold(headSHA, sha) {
		label = headBranch
	}
	return label + " " + shortSHA(sha)
}

// versionNameShort is how much of the sha a version name carries: what git
// and the Mate app show.
const versionNameShort = 7

// shortSHA is sha as a version name spells it: lower case, its first
// versionNameShort characters.
func shortSHA(sha string) string {
	sha = strings.ToLower(sha)
	return sha[:min(versionNameShort, len(sha))]
}

func buildSSHCommand(targetServiceID, workingDir, setup string, includeGit bool, versionName, pushDir string) string {
	return buildSSHCommandWithin(targetServiceID, workingDir, setup, includeGit, versionName, pushDir, defaultUploadBudget())
}

// buildSSHCommandWithin is buildSSHCommand waiting for the upload by budget.
func buildSSHCommandWithin(targetServiceID, workingDir, setup string, includeGit bool, versionName, pushDir string, budget uploadBudget) string {
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
	gitEnsure := GitEnsureRepoHeadCommand(workingDir)

	// Push. The key reaches zcli through its environment alone (pushKeyRead)
	// — never `zcli login`, which writes it to ~/.config/zcli and leaves the
	// Mate's Zerops key sitting in the app's own container, where the app's
	// dependencies run (guide 0.9). The shell reads it into pushKeyName,
	// which it does not export; zcli's wrapper gets it for its own process,
	// drops it from its environment and hands it to zcli alone as
	// ZEROPS_TOKEN. setup is an agent-supplied tool input (and
	// recipe-session deploys reach here with meta=nil, bypassing the
	// tools-layer setup resolution), so it's shell-quoted — a setup name with
	// whitespace/metacharacters would otherwise splice the compound command.
	pushArgs := "zcli push --service-id " + targetServiceID
	if versionName != "" {
		pushArgs += " --version-name " + shellQuote(versionName)
	}
	if setup != "" {
		pushArgs += " --setup " + shellQuote(setup)
	}
	if includeGit {
		pushArgs += " -g"
	}

	// zcli push returns only when the build ends, but its git work — the
	// archive, then the archiver's cleanup — is done once it says it is
	// deploying. It runs detached into pushDir, and this command returns at
	// that line (or zcli's exit) with the output so far; buildWaitCommand
	// awaits the rest outside the source's git lock. With its output and
	// input redirected the background job outlives this session (a
	// non-pty session signals nothing on close). The push rides as the
	// wrapper's arguments, so its words keep the quoting above.
	// zcli's PID lands in pushDir for pushStopCommand. Run directories a
	// wait never removed — it lost its session or hit its ceiling — go once
	// their push has been finished for an hour, when no wait reads them.
	q := shellQuote(pushDir)
	sweep := fmt.Sprintf(`for f in %s/zcp-push-*/rc; do [ -z "$(find "$f" -mmin +60 2>/dev/null)" ] || rm -rf "${f%%/rc}"; done`,
		shellQuote(path.Dir(pushDir)))
	spawn := fmt.Sprintf(`{ %[1]s; mkdir %[2]s; } && { %[3]s="$%[3]s" sh -c 'k=$%[3]s; unset %[3]s; d=$1; shift; ZEROPS_TOKEN="$k" "$@" & echo $! > "$d/pid"; wait $!; echo $? > "$d/rc.tmp" && mv "$d/rc.tmp" "$d/rc"' zcp-push %[2]s %[4]s > %[2]s/log 2>&1 < /dev/null & }`,
		sweep, q, pushKeyName, pushArgs)
	// The wait on the upload lasts zcp_budget seconds, set by the
	// measurement before zcli starts (DM-9): an upload still running then
	// ends the call with uploadUnfinishedExit and its mark, for the caller to
	// stop zcli, so no slow upload is taken for a broken connection.
	waitUpload := fmt.Sprintf(`{ zcp_end=$(( $(date +%%s) + zcp_budget )); until [ -e %[1]s/rc ] || grep -qF %[2]s %[1]s/log 2>/dev/null; do `+
		`[ "$(date +%%s)" -lt "$zcp_end" ] || { cat %[1]s/log; echo "%[3]s budget=$zcp_budget"; exit %[4]d; }; sleep 1; done; cat %[1]s/log; }`,
		q, shellQuote(zcliDeployingLine), uploadUnfinishedMark, uploadUnfinishedExit)
	// The key is read after zcp's own git setup, none of which reads stdin,
	// and before the crew save, which then finds stdin at its end as before.
	// The measurement reads no stdin either.
	return fmt.Sprintf("%s && %s && %s && %s && %s && %s", gitEnsure, pushKeyRead, crewSaveCommand,
		uploadMeasureCommand(includeGit, budget), spawn, waitUpload)
}

// pushKeyRead reads the Zerops key, the first line of the push call's stdin
// (pushKeyInput), into pushKeyName: no command line, local ssh's or the
// remote shell's, ever carries the key, which any process in either container
// could read for the whole push.
const pushKeyRead = "IFS= read -r " + pushKeyName

// pushKeyName is the remote shell's own, unexported name for the key. zcli
// alone gets it, as ZEROPS_TOKEN scoped to its process: a ZEROPS_TOKEN the
// container already exports stays as it is for every other process, and no
// child of the shell — the crew save, a git hook — sees the key.
const pushKeyName = "zcp_key"

// pushKeyInput is the stdin a push call writes for pushKeyRead.
func pushKeyInput(authInfo auth.Info) []byte {
	return []byte(authInfo.Token + "\n")
}

// buildWaitCommand awaits the push buildSSHCommand started in pushDir: it
// prints zcli's whole output and exits with zcli's status, as the push
// itself would have, and removes pushDir. Without pushDir — the call reached
// another container of the source, or a replaced one — it exits 254 at once.
func buildWaitCommand(pushDir string) string {
	q := shellQuote(pushDir)
	return fmt.Sprintf("until [ -e %s/rc ]; do [ -d %s ] || exit 254; sleep 1; done; cat %s/log; rc=$(cat %s/rc); rm -rf %s; exit \"$rc\"", q, q, q, q, q)
}

// pushStopTimeout bounds stopPush's call: zcli ends on SIGTERM, and
// pushStopCommand sends SIGKILL after ten seconds.
const pushStopTimeout = 30 * time.Second

// stopPush stops the push buildSSHCommand started in pushDir after its call
// failed before zcli said it was deploying, so zcli never asks for the
// build. The call outlives ctx — a cancelled deploy is the case it serves —
// and returns pushStopCommand's output; a failed stop is not reported, as
// the deploy already failed.
func stopPush(ctx context.Context, sshDeployer SSHDeployer, hostname, pushDir string) []byte {
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pushStopTimeout)
	defer cancel()
	out, _ := sshDeployer.ExecSSH(stopCtx, hostname, pushStopCommand(pushDir))
	return out
}

// pushStopCommand ends the push in pushDir unless zcli already said it is
// deploying: it kills zcli, waits for its exit, prints its output and
// removes pushDir. A push past that line keeps running to its build, and
// the printed output says the build started.
func pushStopCommand(pushDir string) string {
	q := shellQuote(pushDir)
	deploying := fmt.Sprintf("grep -qF %s %s/log", shellQuote(zcliDeployingLine), q)
	return fmt.Sprintf(`[ -d %[1]s ] || exit 0; until [ -e %[1]s/pid ]; do sleep 1; done; `+
		`%[2]s || { kill "$(cat %[1]s/pid)" 2>/dev/null; i=0; until [ -e %[1]s/rc ]; do i=$((i+1)); [ $i -ne 10 ] || kill -9 "$(cat %[1]s/pid)" 2>/dev/null; sleep 1; done; }; `+
		`cat %[1]s/log; %[2]s || rm -rf %[1]s`, q, deploying)
}

// crewSaveCommand runs the Mate crew's save before the push, when the Mate
// wrote one (spec-mate §2.8a.2): a deploy of the service replaces its
// container, which keeps .git but not the crew's copies under .crew/, so the
// save commits each copy's uncommitted and untracked work onto a ref in .git
// first. A failing save stops the push — the work would be lost. Without a
// crew there is no save and the push runs as before.
const crewSaveCommand = `{ save="$(git rev-parse --git-common-dir)/t3-crew-save"; [ ! -x "$save" ] || "$save" || { echo "` + crewSaveFailedMarker + `" >&2; exit 1; }; }`

// crewSaveFailedMarker precedes the failing save's stderr in the SSH output,
// so classifySSHError tells a refused save from a failed push.
const crewSaveFailedMarker = "ZCP_CREW_SAVE_FAILED"

// buildSSHCommandSHA builds the push command for a resolved deploy-from-
// commit: extractedDir already holds sha's tree (see ExtractCommitToTemp)
// with no .git of its own, so — unlike buildSSHCommand — there is no
// GitEnsureRepoHeadCommand step and no -g flag; --no-git is unconditional
// and --version-name (commitVersionName) records the commit the platform
// can't otherwise see (docs/spec-workflows.md §4.9, P1).
func buildSSHCommandSHA(targetServiceID, extractedDir, setup, versionName string) string {
	// Same key discipline as buildSSHCommand: the key arrives on stdin
	// (pushKeyRead) and reaches only zcli, which replaces the shell, through
	// its environment — never `zcli login` (guide 0.9).
	pushArgs := fmt.Sprintf(
		"zcli push --service-id %s --no-git --version-name %s",
		targetServiceID, shellQuote(versionName),
	)
	if setup != "" {
		pushArgs += " --setup " + shellQuote(setup)
	}
	return fmt.Sprintf(`cd %s && %s && ZEROPS_TOKEN="$%s" exec %s`, shellQuote(extractedDir), pushKeyRead, pushKeyName, pushArgs)
}
