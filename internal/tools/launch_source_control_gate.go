package tools

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"slices"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// urlParse is a package-local indirection over net/url.Parse so the
// launchPushProofHost helper doesn't shadow a stdlib import name in
// inline tests. Behavior identical to url.Parse.
var urlParse = url.Parse

// sourceControlGateCheck names one failure mode the launch source-control
// gate can surface. Each value maps to a distinct blocker ID + Recovery
// hint + per-mode user-facing message in the chain-out response. Pinned
// by TestSourceControlGate_* table coverage.
type sourceControlGateCheck string

const (
	// gateCheckGitPushUnconfigured fires when the ServiceMeta for the
	// promoted runtime lacks GitPushState=configured. Recovery →
	// zerops_workflow action="git-push-setup" service=<pushHostname>.
	gateCheckGitPushUnconfigured sourceControlGateCheck = "git-push-unconfigured"
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
// MetaRemoteURL is the value the composer must embed as buildFromGit on
// the prod runtime entry — never use a live SSH read for that field.
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
// The platform.Client parameter is reserved for P3 (push-proof
// checks 4-5) where we re-discover the source service to capture
// the `LastDeployedCommitSHA` recorded at deploy time; today the
// gate is meta + SSH only.
func validateLaunchSourceControl(
	ctx context.Context,
	_ platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
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

	// Check 3 — live `git remote get-url origin` on push hostname's
	// /var/www equals meta.RemoteURL. Only fires when checks 1-2 are
	// green; the chain disciplines top-down (resolve git-push-setup
	// first, then verify alignment).
	if len(check.FailedChecks) == 0 {
		live, liveErr := launchLiveRemoteReader(ctx, sshDeployer, rt, pushHost)
		if liveErr != nil {
			// Unable to read the live remote (SSH failure, unreachable
			// local repo). Surface as remote-mismatch with the read
			// error in the message — recovery is still "fix the
			// remote", same channel.
			check.LiveRemoteURL = ""
			check.FailedChecks = append(check.FailedChecks, gateCheckRemoteMismatch)
		} else {
			check.LiveRemoteURL = strings.TrimSpace(live)
			// Compare repo IDENTITY, not byte-equality: a trailing ".git"
			// or slash difference between the live origin and the recorded
			// meta is the SAME repo (and is stripped before buildFromGit is
			// emitted anyway). Raw values are preserved on the struct for
			// the diagnostic message.
			if topology.CanonicalRepoURL(check.LiveRemoteURL) != topology.CanonicalRepoURL(check.MetaRemoteURL) {
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
			// Unable to read push proof — surface as head-not-pushed
			// with the read error embedded; recovery is still "push
			// the code via zerops_deploy strategy=git-push".
			check.FailedChecks = append(check.FailedChecks, gateCheckHeadNotPushed)
		case proof.DirtyTree:
			check.FailedChecks = append(check.FailedChecks, gateCheckDevTreeDirty)
		case proof.LocalHead == "" || proof.RemoteHead == "" || proof.LocalHead != proof.RemoteHead:
			check.FailedChecks = append(check.FailedChecks, gateCheckHeadNotPushed)
		}
	}

	// Check 6 — build-integration recommended (warn-only). Always
	// evaluated; emission gated on SkipBuildIntegration ack.
	if meta.BuildIntegration == "" || meta.BuildIntegration == topology.BuildIntegrationNone {
		if !skipBuildIntegrationListed(skipBuildIntegration, pushHost) &&
			!skipBuildIntegrationListed(skipBuildIntegration, hostname) {
			check.FailedChecks = append(check.FailedChecks, gateCheckBuildIntegrationRecommended)
		}
	}

	blockers := buildSourceControlBlockers(check)
	return check, blockers, nil
}

// readLaunchLiveRemote env-aware reads `git remote get-url origin` on
// the push hostname. Container mode SSH's; local mode exec's against
// the current working directory. Empty stdout = no remote (returned
// as empty string, not error — caller treats as mismatch with the
// non-empty meta.RemoteURL).
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
// uses the same authenticated pattern as Phase 1 git-push-setup
// probe (ephemeral .netrc from $GIT_TOKEN) so private repos do not
// false-fail as `head-not-pushed`.
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
		// Authenticated ls-remote: ephemeral .netrc from $GIT_TOKEN
		// (assumed live in container shell — git-push-setup Phase 1
		// guarantees it by restarting the runtime post-write). Same
		// trap-cleanup + umask pattern as Phase 1 probe so a missing
		// token surfaces as auth failure here, not as a falsely-missing
		// remote HEAD. POSIX single-quote escape inlined to avoid a
		// tools→ops dependency for one shell command — RemoteURL
		// passed validateRemoteURL already.
		quotedURL := "'" + strings.ReplaceAll(remoteURL, "'", `'\''`) + "'"
		host := launchPushProofHost(remoteURL)
		lsCmd := strings.Join([]string{
			"trap 'rm -f ~/.netrc' EXIT",
			fmt.Sprintf(`umask 077 && echo "machine %s login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc`, host),
			fmt.Sprintf(`GIT_TERMINAL_PROMPT=0 git ls-remote %s HEAD 2>/dev/null | head -1 | cut -f1 || true`, quotedURL),
		}, " && ")
		lsOut, lsErr := ssh.ExecSSH(ctx, pushHostname, lsCmd)
		if lsErr != nil {
			return LaunchPushProofResult{}, fmt.Errorf("git ls-remote on %s: %w", pushHostname, lsErr)
		}
		remote = strings.TrimSpace(string(lsOut))
	}
	return LaunchPushProofResult{DirtyTree: dirty, LocalHead: local, RemoteHead: remote}, nil
}

// launchPushProofHost extracts the host from a remote URL for the
// ephemeral .netrc machine line. Mirrors ops.parseGitHost — duplicated
// here to keep this layer-4 file from importing the unexported helper.
// Falls back to "github.com" for URLs that don't parse cleanly.
func launchPushProofHost(remoteURL string) string {
	if remoteURL == "" {
		return "github.com"
	}
	if strings.Contains(remoteURL, "://") {
		if u, err := urlParse(remoteURL); err == nil && u.Host != "" {
			return u.Host
		}
	}
	if idx := strings.Index(remoteURL, "/"); idx > 0 {
		host := remoteURL[:idx]
		if colon := strings.LastIndex(host, ":"); colon > 0 {
			host = host[:colon]
		}
		if host != "" {
			return host
		}
	}
	return "github.com"
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
		if lsErr == nil {
			// Output format: "<SHA>\tHEAD"
			fields := strings.Fields(string(lsOut))
			if len(fields) > 0 {
				remote = strings.TrimSpace(fields[0])
			}
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
	case gateCheckGitPushUnconfigured:
		return 1
	case gateCheckRemoteMismatch:
		return 2
	case gateCheckDevTreeDirty:
		return 3
	case gateCheckHeadNotPushed:
		return 4
	case gateCheckBuildIntegrationRecommended:
		return 9
	default:
		return 5
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
				"Cannot promote %q to production — no user-owned git remote is wired. The production project will clone from buildFromGit; ZCP must point that at a repo you control, not the recipe template the source service was bootstrapped from. Run git-push-setup for service=%q with your repo URL + token.",
				check.ChoiceHostname, check.PushHostname,
			),
			Recovery: &topology.Recovery{
				Tool:   "zerops_workflow",
				Action: "git-push-setup",
				Args:   map[string]string{"service": check.PushHostname},
			},
		}
	case gateCheckRemoteMismatch:
		return topology.Blocker{
			ID:       fmt.Sprintf("remote-mismatch-%s", check.PushHostname),
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategorySourceControl,
			Message: fmt.Sprintf(
				"Live `git remote get-url origin` on %s does not match the recorded RemoteURL (live=%q, recorded=%q). The recorded URL is the buildFromGit value the production project will use — the two must agree. Re-run git-push-setup for service=%q to realign, or rewrite the live remote to match.",
				check.PushHostname, check.LiveRemoteURL, check.MetaRemoteURL, check.PushHostname,
			),
			Recovery: &topology.Recovery{
				Tool:   "zerops_workflow",
				Action: "git-push-setup",
				Args:   map[string]string{"service": check.PushHostname, "remoteUrl": check.MetaRemoteURL},
			},
		}
	case gateCheckBuildIntegrationRecommended:
		return topology.Blocker{
			ID:       fmt.Sprintf("build-integration-recommended-%s", check.PushHostname),
			Severity: topology.BlockerSeverityWarn,
			Category: topology.BlockerCategorySourceControl,
			Message: fmt.Sprintf(
				"Stage CI/CD for %q is not configured (meta.BuildIntegration=none). Recommended to set up before promoting — every push to your remote will then auto-build the source pair, matching the production CI/CD model you will configure post-launch. Ask the user: configure now, or skip? On skip, re-call launch with skipBuildIntegration=[%q].",
				check.PushHostname, check.PushHostname,
			),
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
				"Dev container %q has uncommitted changes — `git status --porcelain` is non-empty. Those changes will NOT make it to production (the platform clones the remote's HEAD; git push only pushes commits). Run `zerops_deploy targetService=%q strategy=\"git-push\"` to commit + push the live working tree, then re-call launch.",
				check.PushHostname, check.PushHostname,
			),
			Recovery: &topology.Recovery{
				Tool:   "zerops_deploy",
				Action: "",
				Args:   map[string]string{"targetService": check.PushHostname, "strategy": "git-push"},
			},
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
// source-control-required BEFORE the one-shot launchKey is minted —
// LAUNCH-3: the read-side previously validated only input.TargetService,
// so a 2-runtime launch where runtime B was unconfigured passed read-side,
// advanced to ready-to-launch, minted the irreplaceable key, then failed
// at the publish-side gate.
func runReadSideSourceControlGate(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	runtimes []resolvedLaunchRuntime,
	skipBuildIntegration []string,
) (checks []*LaunchSourceControlCheck, blockers []topology.Blocker, err error) {
	for _, r := range runtimes {
		check, b, gateErr := validateLaunchSourceControl(
			ctx, client, sshDeployer, rt, stateDir,
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
	corpus []workflow.KnowledgeAtom,
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
			ctx, client, sshDeployer, rt, stateDir,
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
				Response: launchSourceControlRequiredResponse(corpus, input, nil, blockers),
			}
		}
		checks = append(checks, check)
	}
	return publishSidePublishGateResult{Checks: checks}
}
