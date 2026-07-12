package tools

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// sourceControlGateCheck names one failure mode the launch source-control
// gate can surface. Each value maps to a distinct blocker ID + Recovery
// hint + per-mode user-facing message in the chain-out response. Pinned
// by TestSourceControlGate_* table coverage.
type sourceControlGateCheck string

const (
	// gateCheckGitPushUnconfigured fires when the ServiceMeta for the
	// promoted runtime lacks GitPushState=configured. Recovery →
	// zerops_workflow action=actionGitPushSetup service=<pushHostname>.
	gateCheckGitPushUnconfigured sourceControlGateCheck = "git-push-unconfigured"
	// gateCheckGitStateMissing fires when the push hostname's /var/www
	// carries NO .git at all (container mode). An artifact deploy without
	// -g (historic CI builds, non-ZCP CI, container replacement on
	// scale/failure) produces exactly this state; it is NOT remote drift
	// and the old rendering (remote-mismatch with live="") handed the
	// agent drift instructions for a missing repo (prod.txt T2).
	// Recovery → git-push-setup, which reconstructs from the recorded
	// remote (non-destructive: init + fetch + mixed reset).
	gateCheckGitStateMissing sourceControlGateCheck = "git-state-missing"
	// gateCheckRemoteMismatch fires when the live origin URL on the
	// push hostname's /var/www differs from ServiceMeta.RemoteURL.
	// Recovery → same git-push-setup action (idempotent reconfirm
	// rewrites the meta to match live, OR the user rewrites the live
	// remote to match meta).
	gateCheckRemoteMismatch sourceControlGateCheck = "remote-mismatch"
	// gateCheckDevTreeDirty (P3 check) fires when `git status --porcelain`
	// on the push hostname's /var/www reports any non-empty output —
	// the dev container has uncommitted / staged / untracked changes
	// that will NOT make it to production (git push only pushes
	// commits). Recovery → `zerops_deploy strategy="git-push"` (which
	// commits + pushes the live working tree) OR the user runs git
	// add/commit/push manually. Hard-block.
	gateCheckDevTreeDirty sourceControlGateCheck = "dev-tree-dirty"
	// gateCheckHeadNotPushed (P3 check) fires when `git ls-remote
	// <RemoteURL> HEAD` returns a SHA that does NOT match the local
	// HEAD on the push hostname's /var/www — the local commit is
	// ahead of the configured remote. Production would clone the
	// remote's HEAD and build stale code. Recovery → `zerops_deploy
	// strategy="git-push"` to push the missing commits. Hard-block.
	gateCheckHeadNotPushed sourceControlGateCheck = "head-not-pushed"
	// gateCheckBuildIntegrationRecommended fires (severity=warn) when
	// the source pair has meta.BuildIntegration=none. Recovery →
	// zerops_workflow action="build-integration" service=<pushHostname>
	// integration="actions|webhook". Warn-only — does not block the
	// status transition once acknowledged via WorkflowInput.SkipBuildIntegration.
	gateCheckBuildIntegrationRecommended sourceControlGateCheck = "build-integration-recommended"
	// gateCheckSourceReadFailed fires when the gate could not READ the
	// live source state (SSH/exec failure on the origin read or the
	// push-proof) — a transport problem, NOT a state problem. Without
	// this split an SSH outage rendered as remote-mismatch/head-not-
	// pushed and handed the agent "fix your remote/push" instructions
	// for a network failure. Hard-block (unverifiable ≠ verified), but
	// the recovery is retry/diagnose-the-read, never git surgery.
	gateCheckSourceReadFailed sourceControlGateCheck = "source-read-failed"
)

// LaunchSourceControlCheck carries the gate's read of one promotable's
// source-side state. Fields are populated by validateLaunchSourceControl
// and consumed by the chain-out response builder. The carrier is also
// returned to the caller for downstream use (composer threads RepoURL
// from MetaRemoteURL — D1 check 4: composer never reads live SSH for
// the RepoURL embed in the bundle).
type LaunchSourceControlCheck struct {
	// ChoiceHostname is the user-supplied hostname (may be the stage
	// half of a standard pair).
	ChoiceHostname string
	// PushHostname is the canonical dev-half (= ServiceMeta primary key)
	// — the runtime whose /var/www holds the working tree.
	PushHostname string
	// Meta is the pair-keyed ServiceMeta for the promoted runtime.
	Meta *workflow.ServiceMeta
	// MetaRemoteURL is meta.RemoteURL captured at gate time so the
	// composer threads the same value gate validated (no late-read
	// race).
	MetaRemoteURL string
	// LiveRemoteURL is the result of `git remote get-url origin` on the
	// push hostname's /var/www at gate time. Empty when no remote is
	// configured. Container mode SSH-reads; local mode exec-reads.
	LiveRemoteURL string
	// FailedChecks lists every check that did not pass, in stable order
	// (gate emission iteration order — caller renders into the chain
	// stack response). Empty = gate green.
	FailedChecks []sourceControlGateCheck
	// BuildIntegrationVerified is the in-memory result of the check-6
	// earn: true when meta.BuildIntegrationVerifiedAt was already
	// stamped OR the live earn probe verified the declared integration
	// this call. The READ-side gate only renders from it; the
	// PUBLISH-side caller stamps meta.BuildIntegrationVerifiedAt from it
	// (the read-side poll path never mutates meta).
	BuildIntegrationVerified bool
	// BuildIntegrationEvidence is the human-readable earn evidence (or
	// the reason verification is absent) for the declared integration.
	BuildIntegrationEvidence string
	// ReadFailure carries the live-read error text when
	// gateCheckSourceReadFailed fired — the blocker message embeds it so
	// the agent sees the transport cause instead of a phantom state
	// diagnosis.
	ReadFailure string
}

// validateLaunchSourceControl runs the launch source-control gate
// against the supplied promoted runtime. The helper is called at TWO
// sites with different audit semantics (Codex round-2 split):
//
//  1. Read-side, between scope-prompt and classify-prompt — writeAudit=false.
//     Failure returns a `source-control-required` chain-out response without
//     touching the audit log (gate fires on every poll; auditing would spam).
//  2. Publish-side, inside executeLaunchMutation / executeExistingProjectMutation —
//     writeAudit=true. Drift between read-side OK and publish-side fail is a
//     real publish refusal operators want logged.
//
// Returns the LaunchSourceControlCheck carrier on success; nil + a fully
// formed MCP response on failure (chain-out blocker stack). The carrier's
// MetaRemoteURL is the repo identity the launch pipeline wiring uses
// (the ZEROPS_TOKEN_PROD secret command's -R owner/repo, the webhook
// integration's repositoryFullName) — never use a live SSH read for it.
//
// The build-integration check is warn-only and suppressed when the
// promoted runtime's hostname appears in skipBuildIntegration; the gate
// still emits the warn blocker so the agent sees the suppression took
// effect (severity=warn, no chain advancement).
//
// hostname is the user-supplied targetService (dev OR stage half
// accepted); the helper normalizes to the canonical push hostname via
// workflow.FindServiceMeta.
//
// The platform.Client + projectID parameters feed the check-6 webhook
// earn probe (GetServiceStackIntegrationStatus on the build target);
// checks 1-5 remain meta + SSH only.
func validateLaunchSourceControl(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	projectID string,
	hostname string,
	skipBuildIntegration []string,
) (*LaunchSourceControlCheck, []topology.Blocker, error) {
	if hostname == "" {
		return nil, nil, fmt.Errorf("validateLaunchSourceControl: hostname required")
	}
	meta, err := workflow.FindServiceMeta(stateDir, hostname)
	if err != nil {
		return nil, nil, fmt.Errorf("read service meta %q: %w", hostname, err)
	}
	if meta == nil || !meta.IsComplete() {
		// No meta = bootstrap never ran for this service. Direct the
		// agent at bootstrap rather than the source-control chain —
		// the gate cannot validate state that does not exist.
		return nil, []topology.Blocker{{
			ID:       "service-not-bootstrapped",
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategorySourceControl,
			Message:  fmt.Sprintf("service %q is not bootstrapped — run zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\" before launching to production", hostname),
			Recovery: &topology.Recovery{
				Tool:   "zerops_workflow",
				Action: "start",
				Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
			},
		}}, nil
	}
	// LP-5: a mode that can NEVER act as a push source fails with the
	// honest mode-unsupported blocker (the old git-push-unconfigured
	// chain bounced into a handler that refuses those modes — the
	// prod.txt deadlock class).
	if blockers := modeUnsupportedBlocker(meta); blockers != nil {
		return nil, blockers, nil
	}

	pushHost := meta.Hostname // canonical dev-half = ServiceMeta primary key
	check := &LaunchSourceControlCheck{
		ChoiceHostname: hostname,
		PushHostname:   pushHost,
		Meta:           meta,
		MetaRemoteURL:  strings.TrimSpace(meta.RemoteURL),
	}

	// Check 1 — meta.GitPushState == configured.
	if meta.GitPushState != topology.GitPushConfigured {
		check.FailedChecks = append(check.FailedChecks, gateCheckGitPushUnconfigured)
	}

	// Check 2 — meta.RemoteURL != "". Combined with check 1, but
	// emitted as the same gateCheckGitPushUnconfigured failure since
	// the recovery is identical (run git-push-setup). Separate check
	// only useful when GitPushState=configured but RemoteURL empty
	// (corrupt meta) — also recovers via git-push-setup.
	if check.MetaRemoteURL == "" && !hasCheck(check.FailedChecks, gateCheckGitPushUnconfigured) {
		check.FailedChecks = append(check.FailedChecks, gateCheckGitPushUnconfigured)
	}

	// Check 3a — /var/www/.git exists at all (container mode only).
	runGitPresenceCheck(ctx, sshDeployer, rt, pushHost, check)

	// Check 3 — live `git remote get-url origin` on push hostname's
	// /var/www equals meta.RemoteURL. Only fires when checks 1-2 are
	// green; the chain disciplines top-down (resolve git-push-setup
	// first, then verify alignment).
	if len(check.FailedChecks) == 0 {
		live, liveErr := launchLiveRemoteReader(ctx, sshDeployer, rt, pushHost)
		if liveErr != nil {
			// Unable to READ the live remote (SSH failure, unreachable
			// local repo). This is a read problem, not a state problem —
			// surfacing it as remote-mismatch handed the agent "fix your
			// remote" instructions for a network outage (F2 split).
			check.LiveRemoteURL = ""
			check.ReadFailure = liveErr.Error()
			check.FailedChecks = append(check.FailedChecks, gateCheckSourceReadFailed)
		} else {
			check.LiveRemoteURL = strings.TrimSpace(live)
			switch {
			case topology.CanonicalRepoURL(check.LiveRemoteURL) == "":
				// Empty live origin with NO transport error (B3): the read
				// succeeded but yielded no origin URL — origin removed, broken
				// perms / dubious-ownership, or a local CWD that is not a git
				// repo (readLocalGitRemote returns ("", nil) by contract). This
				// is an UNVERIFIED read, not a confirmed mismatch: surfacing
				// remote-mismatch with live="" handed the agent "your remote
				// differs, rewrite it" for a read / absent-origin problem.
				check.ReadFailure = "live `git remote get-url origin` returned empty — no `origin` remote is wired on the push source, or it is not a git repository"
				check.FailedChecks = append(check.FailedChecks, gateCheckSourceReadFailed)
			case topology.CanonicalRepoURL(check.LiveRemoteURL) != topology.CanonicalRepoURL(check.MetaRemoteURL):
				// Compare repo IDENTITY, not byte-equality: a trailing ".git"
				// or slash difference between the live origin and the recorded
				// meta is the SAME repo. Raw values are preserved on the struct
				// for the diagnostic message.
				check.FailedChecks = append(check.FailedChecks, gateCheckRemoteMismatch)
			}
		}
	}

	// Checks 4 + 5 (P3 push proof) — fire only when checks 1-3 are
	// green so the chain stack remains "fix git remote first, then
	// push the code." Both run via the same launchPushProofReader hook
	// so tests can stub the SSH/local exec without a real container.
	if len(check.FailedChecks) == 0 {
		proof, proofErr := launchPushProofReader(ctx, sshDeployer, rt, pushHost, check.MetaRemoteURL)
		switch {
		case proofErr != nil:
			// Unable to READ the push proof — transport, not state.
			// (Previously rendered head-not-pushed, sending the agent to
			// push code over a broken read.)
			check.ReadFailure = proofErr.Error()
			check.FailedChecks = append(check.FailedChecks, gateCheckSourceReadFailed)
		case proof.DirtyTree:
			check.FailedChecks = append(check.FailedChecks, gateCheckDevTreeDirty)
		case proof.LocalHead == "" || proof.RemoteHead == "" || proof.LocalHead != proof.RemoteHead:
			check.FailedChecks = append(check.FailedChecks, gateCheckHeadNotPushed)
		}
	}

	// Check 6 — build-integration EARNED-or-recommended (warn-only;
	// emission gated on SkipBuildIntegration ack). The declared enum
	// (none/webhook/actions) records the user's CHOICE; the warn clears
	// only on an EARNED signal: a recorded VerifiedAt stamp, or a live
	// earn probe passing this call (actions: workflow file present —
	// trustworthy only AFTER push-proof checks 4-5 proved clean tree +
	// pushed HEAD, hence the FailedChecks==0 guard; webhook: platform
	// integration-status read). A bare declaration no longer suppresses
	// the warn (the unearned-state bug class).
	skipAcked := skipBuildIntegrationListed(skipBuildIntegration, pushHost) ||
		skipBuildIntegrationListed(skipBuildIntegration, hostname)
	switch {
	case meta.BuildIntegration == "" || meta.BuildIntegration == topology.BuildIntegrationNone:
		check.BuildIntegrationEvidence = "no ZCP-managed integration declared"
		if !skipAcked {
			check.FailedChecks = append(check.FailedChecks, gateCheckBuildIntegrationRecommended)
		}
	case meta.BuildIntegrationVerifiedAt != "":
		check.BuildIntegrationVerified = true
		check.BuildIntegrationEvidence = "verified at " + meta.BuildIntegrationVerifiedAt
	case len(check.FailedChecks) == 0:
		earned, evidence := buildIntegrationEarnProbe(ctx, earnProbeDeps{
			client:      client,
			sshDeployer: sshDeployer,
			rt:          rt,
			projectID:   projectID,
		}, pushHost, meta)
		check.BuildIntegrationVerified = earned
		check.BuildIntegrationEvidence = evidence
		if !earned && !skipAcked {
			check.FailedChecks = append(check.FailedChecks, gateCheckBuildIntegrationRecommended)
		}
	default:
		// Declared but unverifiable: earlier checks failed, so the
		// working-tree earn signal cannot be trusted (nothing proves the
		// file is on the pushed HEAD). Stay declared-unverified.
		check.BuildIntegrationEvidence = "declared, not verifiable until the source-control checks above pass"
		if !skipAcked {
			check.FailedChecks = append(check.FailedChecks, gateCheckBuildIntegrationRecommended)
		}
	}

	blockers := buildSourceControlBlockers(check)
	return check, blockers, nil
}

// readLaunchLiveRemote env-aware reads `git remote get-url origin` on
// the push hostname. Container mode SSH's; local mode exec's against
// the current working directory. Empty stdout = no origin wired
// (returned as empty string, not error); the gate's check 3 treats an
// empty live origin as source-read-failed (an unverified read), NOT a
// remote-mismatch — live="" is not a confirmed different URL (B3).
func readLaunchLiveRemote(ctx context.Context, sshDeployer ops.SSHDeployer, rt runtime.Info, pushHostname string) (string, error) {
	if rt.InContainer {
		if sshDeployer == nil {
			return "", fmt.Errorf("source-control gate: SSH deployer unavailable in container mode")
		}
		return readGitRemoteURL(ctx, sshDeployer, pushHostname)
	}
	return readLocalGitRemote(ctx, "")
}

// launchLiveRemoteReader is the function the gate calls to read the
// live origin URL. Default: readLaunchLiveRemote (real SSH / local
// exec). Tests override via setLaunchLiveRemoteReader so they can run
// without a real repo or container — the stub returns whatever the
// fixture seeded into ServiceMeta.RemoteURL, making check 3 pass.
//
// Initialized at package load (allowed by TestNoCrossCallHandlerState
// — initialized vars are fine, only zero-value vars are forbidden).
//
//nolint:gochecknoglobals // test-injection point for the live-remote read; documented above
var launchLiveRemoteReader = readLaunchLiveRemote

// setLaunchLiveRemoteReader swaps the live-remote reader for tests.
// Returns a cleanup func to restore the previous value via defer.
func setLaunchLiveRemoteReader(f func(ctx context.Context, ssh ops.SSHDeployer, rt runtime.Info, hostname string) (string, error)) func() {
	prev := launchLiveRemoteReader
	launchLiveRemoteReader = f
	return func() { launchLiveRemoteReader = prev }
}

// readLaunchGitPresence SSH-checks whether the push hostname's /var/www
// is a git repo at all. Container-only (the gate skips it in local mode).
func readLaunchGitPresence(ctx context.Context, sshDeployer ops.SSHDeployer, pushHostname string) (bool, error) {
	if sshDeployer == nil {
		return false, fmt.Errorf("source-control gate: SSH deployer unavailable in container mode")
	}
	out, err := sshDeployer.ExecSSH(ctx, pushHostname, "test -d /var/www/.git && echo present || echo absent")
	if err != nil {
		return false, fmt.Errorf("git presence check on %s: %w", pushHostname, err)
	}
	// Only the explicit "absent" marker means missing — any other output
	// flows on to check 3 (live remote read), which is the authoritative
	// wiring check. This keeps the presence probe a pure disambiguator:
	// it can ADD the honest missing-repo state, never veto a healthy one.
	return !strings.Contains(string(out), "absent"), nil
}

// launchGitPresenceReader is the test-injection point for the check-3a
// presence read, matching the launchLiveRemoteReader pattern.
//
//nolint:gochecknoglobals // test-injection point; initialized var
var launchGitPresenceReader = readLaunchGitPresence

// LaunchPushProofResult bundles the per-runtime push-proof signals
// the gate's P3 checks consume:
//
//	DirtyTree   — `git status --porcelain` on push hostname returned
//	              non-empty (uncommitted/staged/untracked changes).
//	LocalHead   — `git rev-parse HEAD` on push hostname.
//	RemoteHead  — `git ls-remote <remoteURL> HEAD` (or default branch).
//
// Empty LocalHead OR empty RemoteHead OR mismatched LocalHead vs
// RemoteHead all fail the gate (head-not-pushed). The reader returns
// an error only on tool failure (SSH/exec broken); "no remote" / "no
// commits" / "dirty tree" surface via the fields.
type LaunchPushProofResult struct {
	DirtyTree  bool
	LocalHead  string
	RemoteHead string
}

// readLaunchPushProof is the default env-aware push-proof reader.
// Container mode SSH-execs `git status` + `git rev-parse HEAD` + `git
// ls-remote` against the push hostname's /var/www; local mode exec's
// the same commands against the current working directory.
func readLaunchPushProof(ctx context.Context, sshDeployer ops.SSHDeployer, rt runtime.Info, pushHostname string, remoteURL string) (LaunchPushProofResult, error) {
	if rt.InContainer {
		if sshDeployer == nil {
			return LaunchPushProofResult{}, fmt.Errorf("launch push-proof: SSH deployer unavailable in container mode")
		}
		return readLaunchPushProofContainer(ctx, sshDeployer, pushHostname, remoteURL)
	}
	return readLaunchPushProofLocal(ctx, remoteURL)
}

// readLaunchPushProofContainer runs the three push-proof commands
// over SSH in /var/www on the push hostname. The `ls-remote` step
// uses ops.BuildGitAuthedLsRemoteCommand — the SAME session-env
// credential helper the probe and the real push use — so private
// repos do not false-fail as `head-not-pushed`, and tools/ carries
// no inline auth duplicate (the 2026-05-28 audit consolidation).
// The session env is live per fresh SSH session, no restart coupling.
func readLaunchPushProofContainer(ctx context.Context, ssh ops.SSHDeployer, pushHostname string, remoteURL string) (LaunchPushProofResult, error) {
	statusCmd := fmt.Sprintf(`cd %s 2>/dev/null && git status --porcelain 2>/dev/null || true`, exportRepoRoot)
	statusOut, err := ssh.ExecSSH(ctx, pushHostname, statusCmd)
	if err != nil {
		return LaunchPushProofResult{}, fmt.Errorf("git status on %s: %w", pushHostname, err)
	}
	dirty := strings.TrimSpace(string(statusOut)) != ""

	headCmd := fmt.Sprintf(`cd %s 2>/dev/null && git rev-parse HEAD 2>/dev/null || true`, exportRepoRoot)
	headOut, err := ssh.ExecSSH(ctx, pushHostname, headCmd)
	if err != nil {
		return LaunchPushProofResult{}, fmt.Errorf("git rev-parse on %s: %w", pushHostname, err)
	}
	local := strings.TrimSpace(string(headOut))

	remote := ""
	if remoteURL != "" {
		lsOut, lsErr := ssh.ExecSSH(ctx, pushHostname, ops.BuildGitAuthedLsRemoteCommand(remoteURL))
		if lsErr != nil {
			return LaunchPushProofResult{}, fmt.Errorf("git ls-remote on %s: %w", pushHostname, lsErr)
		}
		remote = strings.TrimSpace(string(lsOut))
	}
	return LaunchPushProofResult{DirtyTree: dirty, LocalHead: local, RemoteHead: remote}, nil
}

// readLaunchPushProofLocal runs the three push-proof commands against
// the current working directory in local mode. The `ls-remote` step
// runs with GIT_TERMINAL_PROMPT=0 + GIT_SSH_COMMAND='ssh -o BatchMode=yes'
// so a missing credential helper fails fast instead of hanging the MCP
// session on a credential prompt.
func readLaunchPushProofLocal(ctx context.Context, remoteURL string) (LaunchPushProofResult, error) {
	// git status --porcelain
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusOut, err := statusCmd.Output()
	if err != nil {
		return LaunchPushProofResult{}, fmt.Errorf("local git status: %w", err)
	}
	dirty := strings.TrimSpace(string(statusOut)) != ""

	headCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	headOut, err := headCmd.Output()
	local := ""
	if err == nil {
		local = strings.TrimSpace(string(headOut))
	}

	remote := ""
	if remoteURL != "" {
		lsCmd := exec.CommandContext(ctx, "git", "ls-remote", remoteURL, "HEAD")
		lsCmd.Env = append(lsCmd.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
		)
		lsOut, lsErr := lsCmd.Output()
		if lsErr != nil {
			// B3 / Codex #7: a ls-remote ERROR (network, auth, bad URL) is a
			// READ failure — return it so the gate classifies source-read-failed,
			// matching container-mode parity. Swallowing it left RemoteHead empty,
			// which the gate read as head-not-pushed ("push your code") for what
			// was actually an unreachable remote. A remote that EXISTS but has no
			// matching ref returns success + empty output → RemoteHead stays ""
			// → head-not-pushed, which is correct (never pushed).
			return LaunchPushProofResult{}, fmt.Errorf("local git ls-remote %s: %w", remoteURL, lsErr)
		}
		// Output format: "<SHA>\tHEAD"
		fields := strings.Fields(string(lsOut))
		if len(fields) > 0 {
			remote = strings.TrimSpace(fields[0])
		}
	}
	return LaunchPushProofResult{DirtyTree: dirty, LocalHead: local, RemoteHead: remote}, nil
}

// launchPushProofReader is the function the gate calls for P3 checks
// (DirtyTree + RemoteHead == LocalHead). Tests override via
// setLaunchPushProofReader.
//
//nolint:gochecknoglobals // test-injection point; matches launchLiveRemoteReader pattern
var launchPushProofReader = readLaunchPushProof

// setLaunchPushProofReader swaps the push-proof reader for tests.
// Returns a cleanup func to restore the previous value via defer.
func setLaunchPushProofReader(f func(ctx context.Context, ssh ops.SSHDeployer, rt runtime.Info, hostname string, remoteURL string) (LaunchPushProofResult, error)) func() {
	prev := launchPushProofReader
	launchPushProofReader = f
	return func() { launchPushProofReader = prev }
}

// hasCheck reports whether the supplied gate check is already in the
// failure list. Stable-order de-dup helper.
func hasCheck(list []sourceControlGateCheck, target sourceControlGateCheck) bool {
	return slices.Contains(list, target)
}

// skipBuildIntegrationListed reports whether the hostname is in the
// agent-supplied SkipBuildIntegration ack list. Accepts either the
// canonical push hostname or the user-supplied choice hostname (both
// halves of a pair) so the agent can ack with either name.
func skipBuildIntegrationListed(ack []string, hostname string) bool {
	return slices.Contains(ack, hostname)
}

// buildSourceControlBlockers converts a populated LaunchSourceControlCheck
// into the structured Blocker list embedded on the source-control-required
// response. Blockers are emitted in stable order (FailedChecks iteration);
// severity per check (warn for build-integration-recommended, block
// otherwise). Each blocker carries a Recovery hint targeting the existing
// workflow action that resolves it.
func buildSourceControlBlockers(check *LaunchSourceControlCheck) []topology.Blocker {
	if check == nil || len(check.FailedChecks) == 0 {
		return nil
	}
	out := make([]topology.Blocker, 0, len(check.FailedChecks))
	// Stable order so the agent always sees the same chain top-down.
	failed := append([]sourceControlGateCheck(nil), check.FailedChecks...)
	sort.SliceStable(failed, func(i, j int) bool {
		return gateCheckOrder(failed[i]) < gateCheckOrder(failed[j])
	})
	for _, ck := range failed {
		out = append(out, sourceControlBlockerFor(check, ck))
	}
	return out
}

// gateCheckOrder defines the canonical chain order. Top-down: git
// remote first (without a configured push the rest cannot be verified),
// then remote alignment, then push proof (commit-clean + HEAD pushed),
// then build-integration. Agent resolves in this order.
func gateCheckOrder(ck sourceControlGateCheck) int {
	switch ck {
	case gateCheckSourceReadFailed:
		return 0
	case gateCheckGitPushUnconfigured:
		return 1
	case gateCheckGitStateMissing:
		return 2
	case gateCheckRemoteMismatch:
		return 3
	case gateCheckDevTreeDirty:
		return 4
	case gateCheckHeadNotPushed:
		return 5
	case gateCheckBuildIntegrationRecommended:
		return 9
	default:
		return 6
	}
}

// sourceControlBlockerFor renders the per-check blocker payload. Each
// blocker carries (a) a launch-context user-facing message — the
// chained git-push-setup walkthrough is develop-styled, this layer's
// wording frames it as a prod-promotion prerequisite, and (b) a
// Recovery hint pointing at the exact next call.
func sourceControlBlockerFor(check *LaunchSourceControlCheck, ck sourceControlGateCheck) topology.Blocker {
	switch ck {
	case gateCheckGitPushUnconfigured:
		return topology.Blocker{
			ID:       fmt.Sprintf("git-push-unconfigured-%s", check.PushHostname),
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategorySourceControl,
			Message: fmt.Sprintf(
				"Cannot promote %q to production — no user-owned git remote is wired. Production builds from your repo via the production pipeline; ZCP must point that at a repo you control, not the recipe template the source service was bootstrapped from. Run git-push-setup for service=%q with your repo URL + token.",
				check.ChoiceHostname, check.PushHostname,
			),
			Recovery: &topology.Recovery{
				Tool:   "zerops_workflow",
				Action: actionGitPushSetup,
				Args:   map[string]string{"service": check.PushHostname},
			},
		}
	case gateCheckGitStateMissing:
		return topology.Blocker{
			ID:       fmt.Sprintf("git-state-missing-%s", check.PushHostname),
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategorySourceControl,
			Message: fmt.Sprintf(
				"/var/www on %s is not a git repository — a deploy replaced the container with an artifact that carried no .git (CI build without -g, container replacement). This is an EXPECTED platform state, NOT remote drift: the recorded remote %q is intact and the code is safe on it. Re-run git-push-setup for service=%q — it reconstructs the repo from the recorded remote (init + fetch + non-destructive mixed reset; nothing on the container is overwritten).",
				check.PushHostname,
				topology.RedactRepoURLCredentials(check.MetaRemoteURL),
				check.PushHostname,
			),
			Recovery: &topology.Recovery{
				Tool:   "zerops_workflow",
				Action: actionGitPushSetup,
				Args:   map[string]string{"service": check.PushHostname, "remoteUrl": check.MetaRemoteURL},
			},
		}
	case gateCheckRemoteMismatch:
		return topology.Blocker{
			ID:       fmt.Sprintf("remote-mismatch-%s", check.PushHostname),
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategorySourceControl,
			Message: fmt.Sprintf(
				"Live `git remote get-url origin` on %s does not match the recorded RemoteURL (live=%q, recorded=%q). The recorded URL is the repo the production pipeline will build from — the two must agree. Re-run git-push-setup for service=%q to realign, or rewrite the live remote to match.",
				check.PushHostname,
				topology.RedactRepoURLCredentials(check.LiveRemoteURL),
				topology.RedactRepoURLCredentials(check.MetaRemoteURL),
				check.PushHostname,
			),
			Recovery: &topology.Recovery{
				Tool:   "zerops_workflow",
				Action: actionGitPushSetup,
				Args:   map[string]string{"service": check.PushHostname, "remoteUrl": check.MetaRemoteURL},
			},
		}
	case gateCheckBuildIntegrationRecommended:
		// Two distinct warn variants: nothing declared vs declared but
		// not EARNED. The second names the declared-vs-verified
		// distinction so the agent finishes the GitHub/dashboard side
		// instead of treating the recorded choice as a working pipeline.
		msg := fmt.Sprintf(
			"This does NOT block launch — recommended, not required. Stage CI/CD for %q is not configured (meta.BuildIntegration=none); set it up so every push auto-builds the source pair. Ask the user: configure now, or skip? On skip, re-call launch with skipBuildIntegration=[%q].",
			check.PushHostname, check.PushHostname,
		)
		if bi := check.Meta.BuildIntegration; bi != "" && bi != topology.BuildIntegrationNone {
			msg = fmt.Sprintf(
				"This does NOT block launch — recommended, not required. Build integration %q for %q is declared but not verified (%s): the choice was recorded but ZCP could not confirm the integration actually exists. Finish the setup steps from `zerops_workflow action=\"build-integration\" service=%q integration=%q`, or skip with skipBuildIntegration=[%q].",
				bi, check.PushHostname, check.BuildIntegrationEvidence,
				check.PushHostname, bi, check.PushHostname,
			)
		}
		return topology.Blocker{
			ID:       fmt.Sprintf("build-integration-recommended-%s", check.PushHostname),
			Severity: topology.BlockerSeverityWarn,
			Category: topology.BlockerCategorySourceControl,
			Message:  msg,
			Recovery: &topology.Recovery{
				Tool:   "zerops_workflow",
				Action: "build-integration",
				Args:   map[string]string{"service": check.PushHostname},
			},
		}
	case gateCheckDevTreeDirty:
		return topology.Blocker{
			ID:       fmt.Sprintf("dev-tree-dirty-%s", check.PushHostname),
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategorySourceControl,
			Message: fmt.Sprintf(
				"Dev container %q has uncommitted changes — `git status --porcelain` is non-empty. Those changes will NOT make it to production (the platform clones the remote's HEAD; git push only pushes commits), and git-push will NOT stage or commit them for you (it warns, then pushes the committed HEAD only) — the commit step is yours; this launch gate blocks until the tree is clean. Commit first (`ssh %s \"cd /var/www && git add -A && git commit -m '<msg>'\"` in container mode, or plain git add/commit locally), then `zerops_deploy targetService=%q strategy=\"git-push\"` pushes the commits, then re-call launch.",
				check.PushHostname, check.PushHostname, check.PushHostname,
			),
			Recovery: &topology.Recovery{
				Tool:   "zerops_deploy",
				Action: "",
				Args:   map[string]string{"targetService": check.PushHostname, "strategy": "git-push"},
			},
		}
	case gateCheckSourceReadFailed:
		return topology.Blocker{
			ID:       fmt.Sprintf("source-read-failed-%s", check.PushHostname),
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategorySourceControl,
			Message: fmt.Sprintf(
				"Could not VERIFY source control on %q — the live read did not yield a usable result (%s). This is an UNVERIFIED read, NOT a confirmed remote mismatch: do not assume the recorded remote %q drifted. If the source is unreachable, fix that (SSH/VPN reachability in container mode, repository accessibility in local mode); if no `origin` remote is wired on the source, re-run git-push-setup for service=%q. Then re-call launch — the gate re-reads on every call.",
				check.PushHostname, check.ReadFailure, topology.RedactRepoURLCredentials(check.MetaRemoteURL), check.PushHostname,
			),
		}
	case gateCheckHeadNotPushed:
		return topology.Blocker{
			ID:       fmt.Sprintf("head-not-pushed-%s", check.PushHostname),
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategorySourceControl,
			Message: fmt.Sprintf(
				"Local HEAD on dev container %q is ahead of the configured remote (or remote HEAD unreachable). Production would build stale code. Run `zerops_deploy targetService=%q strategy=\"git-push\"` to push the missing commits, then re-call launch.",
				check.PushHostname, check.PushHostname,
			),
			Recovery: &topology.Recovery{
				Tool:   "zerops_deploy",
				Action: "",
				Args:   map[string]string{"targetService": check.PushHostname, "strategy": "git-push"},
			},
		}
	}
	// Defensive: unknown check ⇒ generic block (forces tests to catch a
	// new check variant added without an emission branch).
	return topology.Blocker{
		ID:       fmt.Sprintf("source-control-unknown-%s", check.PushHostname),
		Severity: topology.BlockerSeverityBlock,
		Category: topology.BlockerCategorySourceControl,
		Message:  fmt.Sprintf("unknown source-control gate failure for %q — please file a bug", check.PushHostname),
	}
}

// gateHasBlockingFailure reports whether the gate's blockers list
// includes at least one Severity=block entry. Warn-only blockers (e.g.
// build-integration-recommended) do not gate status advancement.
func gateHasBlockingFailure(blockers []topology.Blocker) bool {
	for _, b := range blockers {
		if b.Severity == topology.BlockerSeverityBlock {
			return true
		}
	}
	return false
}

// publishSidePublishGateResult bundles the publish-side gate's outputs
// for the mutation callers: the per-runtime gate-validated check carrier
// (composer reads MetaRemoteURL from these) plus an optional MCP
// response when the gate refuses. Both executeLaunchMutation and
// executeExistingProjectMutation consume this shape.
type publishSidePublishGateResult struct {
	Checks   []*LaunchSourceControlCheck
	Response *mcp.CallToolResult
}

// runReadSideSourceControlGate runs the source-control gate (P-LP-10/11)
// over EVERY promoted runtime at the read-side transition (scope →
// classify). It does NOT audit-log (the publish-side re-runs with audit).
// Aggregates per-runtime blockers so a multi-runtime launch surfaces
// source-control-required BEFORE the launch token is acquired —
// LAUNCH-3: the read-side previously validated only input.TargetService,
// so a 2-runtime launch where runtime B was unconfigured passed read-side,
// advanced to ready-to-launch, acquired the token (burning the one-time
// delegation on the delegated path), then failed at the publish-side gate.
func runReadSideSourceControlGate(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	projectID string,
	runtimes []resolvedLaunchRuntime,
	skipBuildIntegration []string,
) (checks []*LaunchSourceControlCheck, blockers []topology.Blocker, err error) {
	for _, r := range runtimes {
		check, b, gateErr := validateLaunchSourceControl(
			ctx, client, sshDeployer, rt, stateDir, projectID,
			r.ChoiceHostname, skipBuildIntegration,
		)
		if gateErr != nil {
			return nil, nil, gateErr
		}
		blockers = append(blockers, b...)
		if check != nil {
			checks = append(checks, check)
		}
	}
	return checks, blockers, nil
}

// runPublishSideSourceControlGate runs the source-control gate at
// publish time (writeAudit=true semantics) for every promoted runtime
// and returns the per-runtime checks + an optional refusal response.
// Callers test `result.Response != nil` and return early; otherwise
// they consume `result.Checks[i]` as the gate-validated RepoURL source
// for the matching runtime in the launch bundle.
//
// The helper centralizes the publish-side audit-logging discipline so
// the new-project and existing-project mutation paths share one source
// of "what to log when the gate fails", avoiding drift.
func runPublishSideSourceControlGate(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	input WorkflowInput,
	sourceProjectID string,
	stateDir string,
	launchID string,
	runtimes []resolvedLaunchRuntime,
) publishSidePublishGateResult {
	if len(runtimes) == 0 {
		return publishSidePublishGateResult{
			Response: convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"publish-side source-control gate: no runtimes resolved (Promotables empty + TargetService missing)",
				"Pass targetService=<hostname> or promotables=[{hostname:<host>}, ...].",
			), WithRecoveryStatus()),
		}
	}
	checks := make([]*LaunchSourceControlCheck, 0, len(runtimes))
	for _, r := range runtimes {
		check, blockers, gateErr := validateLaunchSourceControl(
			ctx, client, sshDeployer, rt, stateDir, sourceProjectID,
			r.ChoiceHostname, input.SkipBuildIntegration,
		)
		if gateErr != nil {
			_ = appendAuditLog(stateDir, launchAuditEntry{
				LaunchID:          launchID,
				Action:            "publish-rejected",
				SourceProjectID:   sourceProjectID,
				TargetProjectName: input.ProductionProjectName,
				Result:            "failure",
				ErrorMessage:      "source-control gate (" + r.PushHostname + "): " + gateErr.Error(),
			})
			return publishSidePublishGateResult{
				Response: convertError(platform.NewPlatformError(
					platform.ErrAPIError,
					fmt.Sprintf("publish-side source-control gate (%s): %v", r.PushHostname, gateErr),
					"",
				), WithRecoveryStatus()),
			}
		}
		if gateHasBlockingFailure(blockers) {
			_ = appendAuditLog(stateDir, launchAuditEntry{
				LaunchID:          launchID,
				Action:            "publish-rejected",
				SourceProjectID:   sourceProjectID,
				TargetProjectName: input.ProductionProjectName,
				Result:            "failure",
				ErrorMessage:      "publish-side source-control gate failed for " + r.PushHostname + " (drift from read-side)",
			})
			return publishSidePublishGateResult{
				Response: launchSourceControlRequiredResponse(input, nil, blockers),
			}
		}
		// Stamp the EARNED build-integration verification — publish-side
		// ONLY (the read-side gate runs on every poll and must never
		// mutate meta). Idempotent re-stamp refreshes the evidence time;
		// a write failure is non-fatal (the in-memory check already
		// carries the verified flag for this mutation).
		if check != nil && check.BuildIntegrationVerified &&
			check.Meta != nil && check.Meta.BuildIntegrationVerifiedAt == "" {
			_ = workflow.UpdateServiceMeta(stateDir, check.PushHostname, func(m *workflow.ServiceMeta) error {
				m.BuildIntegrationVerifiedAt = time.Now().UTC().Format(time.RFC3339)
				return nil
			})
		}
		checks = append(checks, check)
	}
	return publishSidePublishGateResult{Checks: checks}
}

// modeUnsupportedBlocker is the LP-5 gate head: non-push-source modes
// (legacy ModeDev, standalone ModeStage) return the honest expansion
// blocker instead of an unsatisfiable git-push-setup chain.
func modeUnsupportedBlocker(meta *workflow.ServiceMeta) []topology.Blocker {
	if meta.PushSourceCheckFor(meta.Hostname) != topology.PushSourceModeUnsupported {
		return nil
	}
	return []topology.Blocker{{
		ID:       "mode-unsupported-" + meta.Hostname,
		Severity: topology.BlockerSeverityBlock,
		Category: topology.BlockerCategorySourceControl,
		Message: fmt.Sprintf(
			"Service %q is in mode %q, which cannot act as a push source — production promotion needs a repo-backed pair. Expand to a standard pair first (the stage half becomes the verified promotion basis): re-run bootstrap with route=adopt and a plan target carrying isExisting=true + bootstrapMode=\"standard\" + an explicit stageHostname. See the develop-mode-expansion atom for the plan shape.",
			meta.Hostname, meta.Mode,
		),
		Recovery: &topology.Recovery{
			Tool:   "zerops_workflow",
			Action: "start",
			Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
		},
	}}
}

// runGitPresenceCheck is gate check 3a: distinguishes "repo gone"
// (artifact deploy without -g; container replacement) from genuine
// remote drift — the two states need OPPOSITE recoveries (reconstruct
// vs re-point). Local mode skips: the working dir's git is user-owned
// (GLC-6) and covered by the local pre-flights.
func runGitPresenceCheck(ctx context.Context, sshDeployer ops.SSHDeployer, rt runtime.Info, pushHost string, check *LaunchSourceControlCheck) {
	if len(check.FailedChecks) != 0 || !rt.InContainer {
		return
	}
	present, presErr := launchGitPresenceReader(ctx, sshDeployer, pushHost)
	switch {
	case presErr != nil:
		check.ReadFailure = presErr.Error()
		check.FailedChecks = append(check.FailedChecks, gateCheckSourceReadFailed)
	case !present:
		check.FailedChecks = append(check.FailedChecks, gateCheckGitStateMissing)
	}
}
