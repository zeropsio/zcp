package ops

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/topology"
)

// DeployResult contains the outcome of a deploy operation (shared by SSH and local modes).
type DeployResult struct {
	Status            string   `json:"status"`
	Mode              string   `json:"mode"` // "ssh" or "local"
	SourceService     string   `json:"sourceService,omitempty"`
	TargetService     string   `json:"targetService"`
	TargetServiceID   string   `json:"targetServiceId"`
	TargetServiceType string   `json:"targetServiceType,omitempty"`
	Message           string   `json:"message"`
	MonitorHint       string   `json:"monitorHint,omitempty"`
	BuildStatus       string   `json:"buildStatus,omitempty"`
	BuildDuration     string   `json:"buildDuration,omitempty"`
	Suggestion        string   `json:"suggestion,omitempty"`
	SSHReady          bool     `json:"sshReady,omitempty"`
	TimedOut          bool     `json:"timedOut,omitempty"`
	NextActions       string   `json:"nextActions,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
	BuildLogs         []string `json:"buildLogs,omitempty"`       // last N lines of build container output (BUILD_FAILED, PREPARING_RUNTIME_FAILED)
	BuildLogsSource   string   `json:"buildLogsSource,omitempty"` // "build_container" or empty
	RuntimeLogs       []string `json:"runtimeLogs,omitempty"`     // last N lines of runtime container output (DEPLOY_FAILED — initCommand stderr)
	RuntimeLogsSource string   `json:"runtimeLogsSource,omitempty"`
	FailedPhase       string   `json:"failedPhase,omitempty"` // "build" | "prepare" | "init" — which lifecycle phase failed

	// SubdomainAccessEnabled is true when the L7 subdomain route is active
	// for the target service at deploy-response time. Set by the deploy
	// handler's post-success auto-enable step (plans/archive/subdomain-auto-enable.md).
	// May be true even without the auto-enable if the platform already had
	// the route live.
	SubdomainAccessEnabled bool `json:"subdomainAccessEnabled,omitempty"`
	// SubdomainURL is the first computed subdomain URL when
	// SubdomainAccessEnabled is true. Convenience for the agent; full list
	// and details available via zerops_discover.
	SubdomainURL string `json:"subdomainUrl,omitempty"`

	// PublicAccess is the PA-5 structured summary (docs/spec-workflows.md
	// §8 O3): {intent, subdomain, url, domains[]}, filled by the same
	// public-access hook that sets SubdomainAccessEnabled/SubdomainURL
	// above. Nil when the hook never ran (e.g. the target isn't eligible
	// for subdomain auto-enable at all — non-HTTP stack, system service).
	PublicAccess *PublicAccessSummary `json:"publicAccess,omitempty"`

	// FailureClassification is populated by the deploy handler on any
	// non-success outcome (build/prepare/init failures) so the agent has
	// a structured next-step instead of having to parse buildLogs/
	// runtimeLogs/failedPhase prose. Nil on success or when no
	// classification was attempted (e.g. early MCP/transport errors that
	// route through convertError instead of producing a DeployResult).
	// Pattern library + classifier in deploy_failure.go +
	// deploy_failure_signals.go (ticket E2).
	FailureClassification *topology.DeployFailureClassification `json:"failureClassification,omitempty"`

	// SHA is the commit hash this deploy shipped — set when zerops_deploy
	// resolved an explicit sha (deploy-from-commit), AND when a
	// working-tree deploy's SOURCE has a git repo with a reachable HEAD
	// (docs/spec-workflows.md §4.9: ops/git.HeadStatus records it even
	// without an explicit sha). Empty only when the source has no git
	// repo at all (or no HEAD yet) — its source revision is unknown.
	SHA string `json:"sha,omitempty"`
	// Dirty is true when this deploy shipped uncommitted changes on top
	// of SHA — only possible on the working-tree path (no explicit sha);
	// the deploy-from-commit path always ships exactly SHA's tree, so it
	// is always Dirty=false there. docs/spec-workflows.md §4.9.
	Dirty bool `json:"dirty,omitempty"`
	// AppVersionID is the platform appVersion id this build produced,
	// filled by pollDeployBuild once the build event resolves. Empty
	// until then, or on a failed/timed-out build. The tools layer threads
	// SHA + AppVersionID into the recorded deploy attempt.
	AppVersionID string `json:"appVersionId,omitempty"`
	// VersionName is the --version-name value this deploy actually passed
	// to zcli push, when it passed one (GF-10, docs/spec-workflows.md
	// §12.6): SHA for a deploy-from-commit, or SHA with a "-dirty" suffix
	// for a working-tree deploy whose source has uncommitted changes on
	// top of HEAD. Empty when no flag was passed (no reachable HEAD). This
	// is platform-side evidence only — NEVER proof of what was deployed
	// (GF-5): correlate it with the resulting appVersion before treating
	// the mapping as source-of-truth.
	VersionName string `json:"versionName,omitempty"`

	// NotCarried lists the self-deploy source's git-ignored paths that
	// zcli's archiver never ships — present only on a self-deploy
	// (DeployClassSelf) preflight read, and only when non-empty (docs/
	// spec-workflows.md §8 DM, §12.6 GF-12). Cross-deploy never sets this
	// (the target's own repo shape isn't what a cross-deploy ships).
	NotCarried *git.NotCarried `json:"notCarried,omitempty"`
	// EnvFiles lists .env / .env.* paths found in a self-deploy source's
	// working tree regardless of git-ignore state (a tracked .env is
	// still a config-in-a-file mistake) — GF-12, empty when none found.
	EnvFiles []string `json:"envFiles,omitempty"`
	// RepoState classifies a self-deploy source's working tree at
	// preflight time — "clean" | "dirty" | "merging" | "rebasing" |
	// "detached" (GF-12). Empty when the source has no repo yet, or on a
	// cross-deploy (preflight is self-deploy only).
	RepoState string `json:"repoState,omitempty"`
}

// GitPushResult contains the outcome of a git-push deploy operation.
//
// NextActions is the post-push guidance the agent must follow before any
// subsequent zerops_verify / close cadence: git-push transmitted bytes
// but the build is async, so the deploy hasn't actually landed yet.
// Agent must watch the async build via zerops_events and explicitly
// call zerops_workflow action="record-deploy" once Status=ACTIVE so
// FirstDeployedAt stamps. Without that bridge, ServiceSnapshot.Deployed
// stays false and verify/close gates won't progress. C2 audit closure
// (audit-prerelease-internal-testing-2026-04-29) — pre-fix the handler
// auto-stamped on push, racing the async build.
type GitPushResult struct {
	Status      string   `json:"status"`                // "DELIVERED" (build watched to ACTIVE) | "PUSHED" | "NOTHING_TO_PUSH"
	RemoteURL   string   `json:"remoteUrl,omitempty"`   // The remote URL used
	Branch      string   `json:"branch"`                // Branch pushed to
	Message     string   `json:"message"`               // Human-readable summary
	Warnings    []string `json:"warnings,omitempty"`    // Non-fatal warnings
	NextActions string   `json:"nextActions,omitempty"` // Post-push agent guidance

	// Build-watch fields (spec-git-delivery-target §6.1): in L1 the push
	// IS the deploy, so the handler follows the integration-triggered
	// build to terminal instead of returning at the push receipt.
	BuildTarget   string `json:"buildTarget,omitempty"`   // hostname the integration rebuilds
	BuildStatus   string `json:"buildStatus,omitempty"`   // last observed appVersion status
	BuildObserved bool   `json:"buildObserved,omitempty"` // an integration build appeared
	AutoRecorded  bool   `json:"autoRecorded,omitempty"`  // ACTIVE build auto-recorded (record-deploy bridge not needed)
	VerifyTarget  string `json:"verifyTarget,omitempty"`  // hostname to verify after a landed build

	// Failure depth on a FAILED integration build — same agents-read-first
	// contract as DeployResult.
	FailureClassification *topology.DeployFailureClassification `json:"failureClassification,omitempty"`
	BuildLogs             []string                              `json:"buildLogs,omitempty"`
}

// DeployClass classifies a deploy invocation as self-deploy (source container
// pushes to itself) or cross-deploy (source ≠ target, including local → remote
// and dev → stage). The class is determined at tool entry from source/target
// identity and propagates through the deploy pipeline.
//
// DM-1 / DM-2 invariants (docs/spec-workflows.md §8 Deploy Modes): self-deploy
// MUST use deployFiles: [.] — narrower patterns destroy the target's working
// tree on artifact extraction (same container is source AND target of the
// overwrite). Cross-deploy has no such constraint because source is distinct
// from target.
type DeployClass string

const (
	// DeployClassSelf — source and target are the same Zerops service.
	// SSH self-deploy from a service to itself (agent on ZCP invokes
	// zerops_deploy targetService=X with source omitted or source==target).
	DeployClassSelf DeployClass = "self"
	// DeployClassCross — source and target are distinct. Covers SSH cross-
	// deploy (dev → stage), local deploy (user machine → remote service),
	// and strategy=git-push (external git remote → Zerops).
	DeployClassCross DeployClass = "cross"
)

// ClassifyDeploy returns the deploy class for a source/target pair. An empty
// sourceService is treated as self-deploy (caller's auto-infer convention).
// See DM-1 in docs/spec-workflows.md §8.
func ClassifyDeploy(sourceService, targetService string) DeployClass {
	if sourceService == "" || sourceService == targetService {
		return DeployClassSelf
	}
	return DeployClassCross
}

// dirtyCrossDeployWarning builds the DeployResult.Warnings sentence for a
// working-tree (no explicit sha) deploy that shipped source's HEAD plus
// uncommitted changes to a CROSS target (docs/spec-workflows.md §4.9,
// §12.6 GF-5): the target now runs code that isn't reproducible from git
// alone. Single owner for both transports — callers append its result
// only when dirty is true and sha was empty; never on a self-deploy
// (DM-7's repoState already reports that) and never on the explicit-sha
// path (always clean, GF-3). An empty source selects the LOCAL wording
// (ops.DeployLocal's source is the caller's own machine, not a named
// Zerops service).
func dirtyCrossDeployWarning(target, source, sha string) string {
	short := sha
	if len(short) > 7 {
		short = short[:7]
	}
	if source == "" {
		return fmt.Sprintf(
			"%s received HEAD %s plus uncommitted changes — not reproducible from git. Commit and redeploy, or pass sha=%q to ship an exact commit.",
			target, short, sha,
		)
	}
	return fmt.Sprintf(
		"%s received HEAD %s plus uncommitted changes from %s — not reproducible from git. Commit on %s and redeploy, or pass sha=%q to ship an exact commit.",
		target, short, source, source, sha,
	)
}

// SSHDeployer executes commands on remote Zerops services.
//
// Two exec shapes live on this interface intentionally:
//
//   - ExecSSH is the foreground pattern: synchronous, bounded by a
//     deployer-wide ceiling (5 min), used for deploys / probes / tails
//     where the caller cares about exit status and full output.
//   - ExecSSHBackground is a fire-and-forget spawn: tighter per-call
//     timeout, extra ssh flags (-T -n BatchMode) to keep the channel
//     from lingering after the remote shell exits. Required for
//     dev-server style "spawn and detach" operations.
//
// Every implementation MUST implement both — test doubles in sibling
// packages embed noopExecSSHBackground to get a compliant stub for free.
type SSHDeployer interface {
	ExecSSH(ctx context.Context, hostname string, command string) ([]byte, error)
	ExecSSHBackground(ctx context.Context, hostname string, command string, timeout time.Duration) ([]byte, error)
}

// deployShaTempCleanupTimeout bounds cleanupTempCtx's detached context — a
// best-effort tmp-dir removal must not hang the caller indefinitely if the
// runner (local shell or SSH) never returns.
const deployShaTempCleanupTimeout = 30 * time.Second

// cleanupTempCtx derives a context for a deferred tmp-dir removal
// (git.RemoveTemp) that survives the parent ctx being canceled or timing
// out — a sha deploy's cleanup runs in a `defer` after the push already
// succeeded, and a caller-side cancellation (client disconnect, deploy
// timeout) must not leave the extracted tree orphaned on the local disk or
// inside the source container. Callers must invoke the returned cancel.
func cleanupTempCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), deployShaTempCleanupTimeout)
}
