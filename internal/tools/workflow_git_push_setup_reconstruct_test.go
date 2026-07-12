package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// writeConfiguredPairMeta seeds a configured standard pair for the
// reconstruction tests.
func writeConfiguredPairMeta(t *testing.T, stateDir string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/app.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// TestGitPushSetupContainer_ReconstructsMissingGit pins S6
// (spec-git-delivery-target §5): a token-less re-call on a configured
// pair whose /var/www/.git vanished (artifact deploy without -g,
// container replacement) RECONSTRUCTS the repo from the recorded remote
// instead of claiming "already-configured" over broken wiring. The
// reconstruction command is non-destructive (guarded init + fetch +
// mixed reset) and authenticates via the SESSION env helper — the
// service secret already exists for a configured pair.
func TestGitPushSetupContainer_ReconstructsMissingGit(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeConfiguredPairMeta(t, stateDir)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "test -d /var/www/.git") {
				return []byte("absent"), nil
			}
			if strings.Contains(cmd, "git status --porcelain") {
				return []byte(""), nil // clean tree after reconstruction
			}
			return []byte("ok"), nil
		},
	}

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("reconstruction path should succeed, got error: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "reconstructed") {
		t.Errorf("response should carry the reconstructed marker; got: %s", body)
	}
	if strings.Contains(body, "already-configured") {
		t.Errorf("missing .git must not short-circuit as already-configured; got: %s", body)
	}

	// The reconstruction command itself: guarded, identity, origin, helper,
	// authenticated fetch, mixed reset — and NO destructive verbs.
	var recon string
	for _, cmd := range ssh.commands {
		if strings.Contains(cmd, "git init -q -b main") {
			recon = cmd
			break
		}
	}
	if recon == "" {
		t.Fatalf("no reconstruction command issued; commands: %v", ssh.commands)
	}
	for _, want := range []string{
		"if test ! -d .git",
		// F1 site 3: identity is filled via the single-owner set-if-absent
		// ensure fragment (same shape every self-heal site uses), not a
		// bare unconditional write.
		`(test -n "$(git config user.email)" || git config user.email 'agent@zerops.io') && (test -n "$(git config user.name)" || git config user.name 'Zerops Agent')`,
		"git remote add origin 'https://github.com/example/app.git'",
		"credential.https://github.com.helper",
		"fetch -q origin HEAD",
		"git update-ref refs/heads/main FETCH_HEAD",
		"git reset -q FETCH_HEAD",
	} {
		if !strings.Contains(recon, want) {
			t.Errorf("reconstruction command missing %q:\n%s", want, recon)
		}
	}
	for _, forbidden := range []string{"reset --hard", "clean -", "checkout --", "GIT_TOKEN='"} {
		if strings.Contains(recon, forbidden) {
			t.Errorf("reconstruction must be non-destructive + session-authed (found %q):\n%s", forbidden, recon)
		}
	}
}

// TestGitPushSetupContainer_Reconstruct_ReportsDivergence pins the honest
// divergence report: when the post-reconstruction working tree differs
// from the remote HEAD, the response says so — never destroys, never
// hides.
func TestGitPushSetupContainer_Reconstruct_ReportsDivergence(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeConfiguredPairMeta(t, stateDir)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "test -d /var/www/.git") {
				return []byte("absent"), nil
			}
			if strings.Contains(cmd, "git status --porcelain") {
				return []byte(" M src/index.ts\n?? dist/bundle.js"), nil
			}
			return []byte("ok"), nil
		},
	}

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git"},
		stateDir, runtime.Info{InContainer: true},
	)
	body := extractText(result)
	if !strings.Contains(body, "divergence") || !strings.Contains(body, "src/index.ts") {
		t.Errorf("divergent tree must surface in the response; got: %s", body)
	}
}

// TestValidateLaunchSourceControl_GitStateMissing_DistinctFromMismatch
// pins the gate's check 3a: an absent /var/www/.git renders as the
// honest git-state-missing blocker (Recovery → git-push-setup, which
// reconstructs), NEVER as remote-mismatch with live="" — drift
// instructions for a missing repo were the prod.txt T2 spiral's first
// misdirection.
func TestValidateLaunchSourceControl_GitStateMissing_DistinctFromMismatch(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeConfiguredPairMeta(t, stateDir)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "test -d /var/www/.git") {
				return []byte("absent"), nil
			}
			return []byte(""), nil
		},
	}

	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, ssh, runtime.Info{InContainer: true},
		stateDir, "test-project", "appdev", nil,
	)
	if err != nil {
		t.Fatalf("validateLaunchSourceControl: %v", err)
	}
	ids := make([]string, 0, len(blockers))
	for _, b := range blockers {
		ids = append(ids, b.ID)
	}
	joined := strings.Join(ids, ",")
	if !strings.Contains(joined, "git-state-missing-appdev") {
		t.Errorf("expected git-state-missing blocker; got %v", ids)
	}
	if strings.Contains(joined, "remote-mismatch") {
		t.Errorf("absent .git must NOT render as remote-mismatch; got %v", ids)
	}
	for _, b := range blockers {
		if b.ID == "git-state-missing-appdev" {
			if b.Recovery == nil || b.Recovery.Action != "git-push-setup" {
				t.Errorf("git-state-missing Recovery must chain into git-push-setup; got %+v", b.Recovery)
			}
			if !strings.Contains(b.Message, "NOT remote drift") {
				t.Errorf("message must name the state honestly; got: %s", b.Message)
			}
		}
	}
}
