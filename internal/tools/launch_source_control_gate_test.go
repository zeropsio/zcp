package tools

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestValidateLaunchSourceControl_NoMeta_ReturnsBootstrapBlocker pins
// the "service has no ServiceMeta" branch — gate cannot validate state
// that does not exist, so it surfaces a bootstrap blocker instead of a
// git-push blocker. Recovery hint points at the adopt route.
func TestValidateLaunchSourceControl_NoMeta_ReturnsBootstrapBlocker(t *testing.T) {
	stateDir := t.TempDir()

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "missing-host", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) != 1 {
		t.Fatalf("blockers: got %d want 1\n%+v", len(blockers), blockers)
	}
	if blockers[0].ID != "service-not-bootstrapped" {
		t.Errorf("blocker ID: got %q want service-not-bootstrapped", blockers[0].ID)
	}
	if blockers[0].Recovery == nil || blockers[0].Recovery.Action != "start" {
		t.Errorf("expected recovery hint pointing at bootstrap start, got %+v", blockers[0].Recovery)
	}
}

// TestValidateLaunchSourceControl_GitPushUnconfigured_BlocksBeforeClassify
// pins the canonical P1 failure mode: meta exists but GitPushState !=
// configured. Returns a block-severity blocker with Recovery pointing
// at git-push-setup for the canonical (dev-half) hostname.
func TestValidateLaunchSourceControl_GitPushUnconfigured_BlocksBeforeClassify(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", "",
		withMetaGitPushUnconfigured())

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) < 1 {
		t.Fatalf("expected at least 1 blocker for unconfigured git-push, got 0")
	}
	first := blockers[0]
	if !strings.HasPrefix(first.ID, "git-push-unconfigured-") {
		t.Errorf("first blocker ID: got %q want git-push-unconfigured-app", first.ID)
	}
	if first.Severity != topology.BlockerSeverityBlock {
		t.Errorf("severity: got %q want block", first.Severity)
	}
	if first.Recovery == nil || first.Recovery.Action != "git-push-setup" {
		t.Errorf("recovery: got %+v want git-push-setup chain", first.Recovery)
	}
	if first.Recovery.Args["service"] != "app" {
		t.Errorf("recovery service: got %q want app", first.Recovery.Args["service"])
	}
}

// TestValidateLaunchSourceControl_LiveRemoteMismatch_Blocks fires when
// meta records RemoteURL but the live origin on the push hostname
// reports a different URL. This catches the recipe-bootstrap loophole:
// meta says "we wired github.com/me/myapp" but live /var/www/.git/config
// still points at the recipe template.
func TestValidateLaunchSourceControl_LiveRemoteMismatch_Blocks(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", "https://github.com/me/myapp.git")
	// Live read returns a DIFFERENT URL (simulates drift / recipe leftover).
	installFakeLiveRemoteReader(t, map[string]string{"app": "https://github.com/zerops-recipe-apps/template.git"})

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) != 1 {
		t.Fatalf("blockers: got %d want 1\n%+v", len(blockers), blockers)
	}
	if !strings.HasPrefix(blockers[0].ID, "remote-mismatch-") {
		t.Errorf("blocker ID: got %q want remote-mismatch-app", blockers[0].ID)
	}
	if !strings.Contains(blockers[0].Message, "https://github.com/zerops-recipe-apps/template.git") {
		t.Errorf("expected message to surface the live URL for diagnosis, got %q", blockers[0].Message)
	}
}

// TestValidateLaunchSourceControl_CredentialInLiveRemote_Redacted pins B10a:
// when the live `git remote get-url origin` carries an embedded credential
// (user pasted https://user:PAT@github.com/...), the blocker message must mask
// it — a full PAT must never be reflected back into an agent-facing payload.
func TestValidateLaunchSourceControl_CredentialInLiveRemote_Redacted(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", "https://github.com/me/myapp.git")
	installFakeLiveRemoteReader(t, map[string]string{"app": "https://octocat:ghp_SECRET12345@github.com/me/other.git"})

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) != 1 || !strings.HasPrefix(blockers[0].ID, "remote-mismatch-") {
		t.Fatalf("expected one remote-mismatch blocker, got %+v", blockers)
	}
	if strings.Contains(blockers[0].Message, "ghp_SECRET12345") {
		t.Errorf("blocker message leaked the PAT: %q", blockers[0].Message)
	}
	if !strings.Contains(blockers[0].Message, "https://***@github.com/me/other.git") {
		t.Errorf("expected masked live URL in message, got %q", blockers[0].Message)
	}
}

// TestValidateLaunchSourceControl_RemoteDotGitDiffersOnly_NoBlock pins that
// a ".git"/slash-only difference between recorded meta and live origin is the
// SAME repo, not drift — the gate compares repo IDENTITY, not bytes. Without
// canonicalization this false-blocks a legitimate launch (meta stored the
// conventional clone URL with ".git", live origin was set without it).
func TestValidateLaunchSourceControl_RemoteDotGitDiffersOnly_NoBlock(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", "https://github.com/example/myapp.git")
	// Live origin is the SAME repo, just without the ".git" suffix.
	installFakeLiveRemoteReader(t, map[string]string{"app": "https://github.com/example/myapp"})

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	for _, b := range blockers {
		if strings.HasPrefix(b.ID, "remote-mismatch-") {
			t.Errorf("a .git-only difference must NOT fire remote-mismatch, got %+v", b)
		}
	}
}

// TestValidateLaunchSourceControl_BuildIntegrationRecommended_WarnOnly
// pins the warn-severity build-integration blocker: meta is otherwise
// gate-ready but BuildIntegration=none. Blocker fires but with
// Severity=warn so the gate does not block status advancement (caller
// uses gateHasBlockingFailure to distinguish).
func TestValidateLaunchSourceControl_BuildIntegrationRecommended_WarnOnly(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", "https://github.com/me/myapp.git",
		withMetaBuildIntegration(topology.BuildIntegrationNone))
	installFakeLiveRemoteReader(t, map[string]string{"app": "https://github.com/me/myapp.git"})

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) != 1 {
		t.Fatalf("blockers: got %d want 1\n%+v", len(blockers), blockers)
	}
	if !strings.HasPrefix(blockers[0].ID, "build-integration-recommended-") {
		t.Errorf("blocker ID: got %q want build-integration-recommended-app", blockers[0].ID)
	}
	if blockers[0].Severity != topology.BlockerSeverityWarn {
		t.Errorf("severity: got %q want warn", blockers[0].Severity)
	}
	if gateHasBlockingFailure(blockers) {
		t.Errorf("warn-only blocker should NOT report as blocking; gateHasBlockingFailure returned true")
	}
	// F10: the message must STATE it doesn't block (the agent's confusion was
	// "do I have to skip to advance?"). Pin only this load-bearing clause, not
	// the full advisory prose.
	if !strings.Contains(blockers[0].Message, "does NOT block") {
		t.Errorf("warn message must lead with the non-blocking fact: %s", blockers[0].Message)
	}
}

// TestValidateLaunchSourceControl_SkipBuildIntegration_AckSuppressesWarn
// pins the D1 acknowledgement: when the user opts out via
// SkipBuildIntegration=[hostname], the warn blocker is suppressed and
// the gate goes fully green.
func TestValidateLaunchSourceControl_SkipBuildIntegration_AckSuppressesWarn(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", "https://github.com/me/myapp.git",
		withMetaBuildIntegration(topology.BuildIntegrationNone))
	installFakeLiveRemoteReader(t, map[string]string{"app": "https://github.com/me/myapp.git"})

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app",
		[]string{"app"},
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) != 0 {
		t.Errorf("expected no blockers after ack, got %+v", blockers)
	}
}

// TestValidateLaunchSourceControl_DevTreeDirty_Blocks pins the P3
// dev-tree-dirty check: meta passes checks 1-3 but the push hostname
// has uncommitted changes (`git status --porcelain` non-empty).
// Production would build stale code; gate blocks with chain to
// zerops_deploy strategy=git-push.
func TestValidateLaunchSourceControl_DevTreeDirty_Blocks(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	installFakeLiveRemoteReader(t, map[string]string{"app": canonicalLaunchTestRemoteURL})
	// Override with dirty-tree push-proof.
	installFakePushProofReader(t, map[string]LaunchPushProofResult{
		"app": {DirtyTree: true, LocalHead: canonicalLaunchTestHeadSHA, RemoteHead: canonicalLaunchTestHeadSHA},
	})

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) != 1 {
		t.Fatalf("blockers: got %d want 1\n%+v", len(blockers), blockers)
	}
	if !strings.HasPrefix(blockers[0].ID, "dev-tree-dirty-") {
		t.Errorf("blocker ID: got %q want dev-tree-dirty-app", blockers[0].ID)
	}
	if blockers[0].Severity != topology.BlockerSeverityBlock {
		t.Errorf("severity: got %q want block", blockers[0].Severity)
	}
	if blockers[0].Recovery == nil || blockers[0].Recovery.Args["strategy"] != "git-push" {
		t.Errorf("recovery: got %+v want zerops_deploy strategy=git-push", blockers[0].Recovery)
	}
}

// TestValidateLaunchSourceControl_HeadNotPushed_Blocks pins the P3
// head-not-pushed check: meta passes checks 1-3 + dev tree is clean,
// but local HEAD does not match remote HEAD (commits not pushed).
// Gate blocks with chain to zerops_deploy strategy=git-push.
func TestValidateLaunchSourceControl_HeadNotPushed_Blocks(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	installFakeLiveRemoteReader(t, map[string]string{"app": canonicalLaunchTestRemoteURL})
	installFakePushProofReader(t, map[string]LaunchPushProofResult{
		"app": {DirtyTree: false, LocalHead: "local-sha-newer", RemoteHead: "remote-sha-older"},
	})

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) != 1 {
		t.Fatalf("blockers: got %d want 1\n%+v", len(blockers), blockers)
	}
	if !strings.HasPrefix(blockers[0].ID, "head-not-pushed-") {
		t.Errorf("blocker ID: got %q want head-not-pushed-app", blockers[0].ID)
	}
	if blockers[0].Severity != topology.BlockerSeverityBlock {
		t.Errorf("severity: got %q want block", blockers[0].Severity)
	}
}

// TestValidateLaunchSourceControl_EmptyLiveRemote_SourceReadFailed_NotMismatch
// pins B3: a live `git remote get-url origin` that returns empty WITH NO
// transport error (origin removed, broken perms, dubious-ownership, or a
// local CWD that is not a git repo — readLocalGitRemote returns ("", nil) by
// contract) must surface as source-read-failed (an unverified read), NEVER as
// remote-mismatch with live="" — that handed the agent "your remote differs,
// rewrite it" for a read/absent-origin problem.
func TestValidateLaunchSourceControl_EmptyLiveRemote_SourceReadFailed_NotMismatch(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	installFakeLiveRemoteReader(t, map[string]string{"app": ""}) // empty live origin, no error

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) != 1 {
		t.Fatalf("blockers: got %d want 1\n%+v", len(blockers), blockers)
	}
	if !strings.HasPrefix(blockers[0].ID, "source-read-failed-") {
		t.Errorf("blocker ID: got %q want source-read-failed-app (empty live read is unverified, not a confirmed mismatch)", blockers[0].ID)
	}
	for _, b := range blockers {
		if strings.HasPrefix(b.ID, "remote-mismatch-") {
			t.Errorf("empty live read must NOT fire remote-mismatch (live='' is not a confirmed different URL): %+v", b)
		}
	}
}

// TestReadLaunchPushProofLocal_LsRemoteError_ReturnsError pins B3/Codex#7:
// the local push-proof reader must RETURN a ls-remote error (so the gate
// renders source-read-failed) instead of swallowing it into an empty
// RemoteHead — which the gate read as head-not-pushed ("push your code") for
// what was actually an unreachable/unauthorized remote. Mirrors container
// parity (readLaunchPushProofContainer returns the ls-remote error).
func TestReadLaunchPushProofLocal_LsRemoteError_ReturnsError(t *testing.T) {
	if _, err := exec.CommandContext(context.Background(), "git", "rev-parse", "--is-inside-work-tree").Output(); err != nil {
		t.Skip("not inside a git work tree — skipping local push-proof reader test")
	}
	// A nonexistent local path is a guaranteed-offline ls-remote failure.
	_, err := readLaunchPushProofLocal(context.Background(), "/nonexistent/zcp-bogus-remote.git")
	if err == nil {
		t.Fatal("ls-remote against a nonexistent remote must return an error (gate → source-read-failed), not swallow it into an empty RemoteHead (head-not-pushed)")
	}
	if !strings.Contains(err.Error(), "ls-remote") {
		t.Errorf("error should name the ls-remote read failure; got %v", err)
	}
}

// TestValidateLaunchSourceControl_FullyGateReady_NoBlockers pins the
// happy path: meta has all four checks aligned (GitPushState=configured,
// RemoteURL non-empty, live origin matches, build-integration set) →
// zero blockers, MetaRemoteURL populated on the check for composer use.
func TestValidateLaunchSourceControl_FullyGateReady_NoBlockers(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	installFakeLiveRemoteReader(t, map[string]string{"app": canonicalLaunchTestRemoteURL})

	check, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "app", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) != 0 {
		t.Errorf("expected no blockers for fully gate-ready meta, got %+v", blockers)
	}
	if check.MetaRemoteURL != canonicalLaunchTestRemoteURL {
		t.Errorf("MetaRemoteURL: got %q want %q (composer must read meta, not SSH)",
			check.MetaRemoteURL, canonicalLaunchTestRemoteURL)
	}
	if check.PushHostname != "app" {
		t.Errorf("PushHostname: got %q want app", check.PushHostname)
	}
}

// TestValidateLaunchSourceControl_StageHalfNormalizesToDevHalf pins
// pair-key normalization: user passes the stage-half hostname,
// resolver returns the canonical dev-half (= meta primary key) as
// PushHostname. ChoiceHostname preserves the user's input for UX
// continuity.
func TestValidateLaunchSourceControl_StageHalfNormalizesToDevHalf(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "appdev", canonicalLaunchTestRemoteURL,
		withMetaMode(topology.ModeStandard),
		withMetaStageHostname("appstage"))
	installFakeLiveRemoteReader(t, map[string]string{"appdev": canonicalLaunchTestRemoteURL})

	check, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, nil, runtime.Info{}, stateDir, "", "appstage", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	if len(blockers) != 0 {
		t.Errorf("expected gate green for stage-half input pointing at pair-keyed meta, got %+v", blockers)
	}
	if check.ChoiceHostname != "appstage" {
		t.Errorf("ChoiceHostname: got %q want appstage (user choice preserved)", check.ChoiceHostname)
	}
	if check.PushHostname != "appdev" {
		t.Errorf("PushHostname: got %q want appdev (normalized to canonical dev-half)", check.PushHostname)
	}
}

// TestHandleLaunchProduction_GitPushUnconfigured_FiresSourceControlRequired
// pins the integration end-to-end: handler runs the gate between
// scope-prompt and classify-prompt and surfaces source-control-required
// status when meta is gate-failing.
func TestHandleLaunchProduction_GitPushUnconfigured_FiresSourceControlRequired(t *testing.T) {
	stateDir := t.TempDir()
	// Seed an incomplete meta: bootstrapped but GitPushState=unconfigured
	// (simulates the session log's recipe-bootstrap scenario).
	seedLaunchGateReadyMeta(t, stateDir, "app", "",
		withMetaGitPushUnconfigured())

	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "app",
	}
	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, input, stateDir, runtime.Info{}, nil, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusSourceControlRequired {
		t.Fatalf("status: got %q want source-control-required\nresponse: %s",
			resp.Status, extractText(result))
	}
	if len(resp.Blockers) == 0 {
		t.Fatal("expected at least one source-control blocker")
	}
	if resp.Blockers[0].Recovery == nil || resp.Blockers[0].Recovery.Action != "git-push-setup" {
		t.Errorf("expected git-push-setup recovery, got %+v", resp.Blockers[0].Recovery)
	}
}

// TestHandleLaunchProduction_MultiRuntime_ReadSideGate_FiresOnUnconfiguredB
// is the LAUNCH-3/LAUNCH-4 pin: a Promotables[] launch where runtime A is
// gate-ready but runtime B is git-push-unconfigured MUST surface
// source-control-required at the READ-SIDE — before the one-shot launchKey
// is minted. Pre-fix the read-side gate validated only input.TargetService,
// so B's failure stayed invisible until the publish-side gate, after the
// irreplaceable key had already been spent.
func TestHandleLaunchProduction_MultiRuntime_ReadSideGate_FiresOnUnconfiguredB(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL) // runtime A: gate-ready
	seedLaunchGateReadyMeta(t, stateDir, "worker", "",                       // runtime B: unconfigured
		withMetaGitPushUnconfigured())

	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})
	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		Promotables: []LaunchPromotableInput{
			{Hostname: "app"},
			{Hostname: "worker"},
		},
	}
	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, input, stateDir, runtime.Info{}, nil, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusSourceControlRequired {
		t.Fatalf("LAUNCH-3: 2-runtime launch with unconfigured runtime B must fire source-control-required at read-side; got %q\n%s",
			resp.Status, extractText(result))
	}
	foundWorker := false
	for _, b := range resp.Blockers {
		if strings.Contains(b.ID, "worker") || strings.Contains(b.Message, "worker") {
			foundWorker = true
		}
	}
	if !foundWorker {
		t.Errorf("expected a blocker referencing the unconfigured runtime 'worker'; got %+v", resp.Blockers)
	}
}

// TestHandleLaunchProduction_ReadSideGate_DoesNotAudit pins the
// audit-asymmetry split (Codex round-2 insight): the read-side gate
// (fires on every poll before publish credentials are supplied) must
// NEVER write to launch-audit-log.json. The publish-side gate
// (executeLaunchMutation / executeExistingProjectMutation) is the
// site that audits. Without this split, every poll against a gate-
// failing source would spam refusals the user never authored.
func TestHandleLaunchProduction_ReadSideGate_DoesNotAudit(t *testing.T) {
	stateDir := t.TempDir()
	seedLaunchGateReadyMeta(t, stateDir, "app", "",
		withMetaGitPushUnconfigured())

	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "app",
		// No LaunchKey / ExistingProdToken → read-side only.
	}
	_, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, input, stateDir, runtime.Info{}, nil, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}

	// Audit log MUST NOT exist after a read-side gate refusal.
	auditPath := stateDir + "/launch-production/launch-audit-log.json"
	if statResult, statErr := fileExists(auditPath); statErr != nil {
		t.Fatalf("stat audit: %v", statErr)
	} else if statResult {
		t.Errorf("read-side gate must NOT write audit entries; %s exists", auditPath)
	}
}

// fileExists is a tiny helper for the audit-asymmetry test.
func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}
