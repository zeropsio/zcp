package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// stubPushProof overrides the launch push-proof reader (the P-LP-11 read
// the release act reuses) for the duration of one test.
func stubPushProof(t *testing.T, proof LaunchPushProofResult, err error) {
	t.Helper()
	prev := launchPushProofReader
	launchPushProofReader = func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, _ string, _ string) (LaunchPushProofResult, error) {
		return proof, err
	}
	t.Cleanup(func() { launchPushProofReader = prev })
}

func seedReleaseMeta(t *testing.T, stateDir string, prodLaunches []workflow.ProdLaunchRef) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "weather",
		Mode:             topology.PlanModeSimple,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/weather.git",
		FirstDeployedAt:  "2026-06-10T09:00:00Z",
		ProdLaunches:     prodLaunches,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// TestHandleRelease_PromptSuggestsNextVersion pins the §7 two-call
// narrowing: a fresh (clean + pushed) source returns release-prompt with
// the next patch bump derived from the remote's existing v* tags.
func TestHandleRelease_PromptSuggestsNextVersion(t *testing.T) {
	// non-parallel: stubs the package-level push-proof reader.
	stateDir := t.TempDir()
	seedReleaseMeta(t, stateDir, []workflow.ProdLaunchRef{{ProdProjectID: "p1", ProdHostname: "weather"}})
	stubPushProof(t, LaunchPushProofResult{LocalHead: "abc123def456", RemoteHead: "abc123def456"}, nil)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "ls-remote --tags") {
				return []byte("aaa\trefs/tags/v1.0.0\nbbb\trefs/tags/v1.2.0\nccc\trefs/tags/v1.2.0^{}\n"), nil
			}
			return []byte("ok"), nil
		},
	}

	result, _, _ := handleRelease(context.Background(), ssh,
		WorkflowInput{Service: "weather"}, stateDir, runtime.Info{InContainer: true})
	if result.IsError {
		t.Fatalf("expected release-prompt, got error: %s", extractText(result))
	}
	body := extractText(result)
	for _, want := range []string{"release-prompt", `"suggestedVersion":"v1.2.1"`, "v1.0.0", "v1.2.0"} {
		if !strings.Contains(body, want) {
			t.Errorf("release-prompt missing %q; got: %s", want, body)
		}
	}
}

// TestHandleRelease_RefusesUnpushedState pins the freshness gate: dirty
// tree or HEAD-not-on-remote refuse the release — a tag must name
// exactly the pushed state the production pipeline builds.
func TestHandleRelease_RefusesUnpushedState(t *testing.T) {
	t.Run("dirty tree", func(t *testing.T) {
		stateDir := t.TempDir()
		seedReleaseMeta(t, stateDir, nil)
		stubPushProof(t, LaunchPushProofResult{DirtyTree: true, LocalHead: "a", RemoteHead: "a"}, nil)
		result, _, _ := handleRelease(context.Background(), &containerSSHStub{},
			WorkflowInput{Service: "weather"}, stateDir, runtime.Info{InContainer: true})
		if !result.IsError || !strings.Contains(extractText(result), "uncommitted") {
			t.Errorf("dirty tree must refuse the release; got: %s", extractText(result))
		}
	})
	t.Run("head not pushed", func(t *testing.T) {
		stateDir := t.TempDir()
		seedReleaseMeta(t, stateDir, nil)
		stubPushProof(t, LaunchPushProofResult{LocalHead: "aaa", RemoteHead: "bbb"}, nil)
		result, _, _ := handleRelease(context.Background(), &containerSSHStub{},
			WorkflowInput{Service: "weather"}, stateDir, runtime.Info{InContainer: true})
		if !result.IsError || !strings.Contains(extractText(result), "not the remote HEAD") {
			t.Errorf("unpushed HEAD must refuse the release; got: %s", extractText(result))
		}
	})
}

// TestHandleRelease_TagsAndPushes pins the executed act: annotated tag at
// HEAD pushed via the session-env credential helper, duplicate tags
// refused, and the response naming what fires.
func TestHandleRelease_TagsAndPushes(t *testing.T) {
	stateDir := t.TempDir()
	seedReleaseMeta(t, stateDir, []workflow.ProdLaunchRef{{ProdProjectID: "p1", ProdHostname: "weather"}})
	stubPushProof(t, LaunchPushProofResult{LocalHead: "abc123def456", RemoteHead: "abc123def456"}, nil)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "ls-remote --tags") {
				return []byte("aaa\trefs/tags/v1.0.0\n"), nil
			}
			return []byte("ok"), nil
		},
	}

	// Duplicate refused.
	dup, _, _ := handleRelease(context.Background(), ssh,
		WorkflowInput{Service: "weather", ReleaseVersion: "v1.0.0"}, stateDir, runtime.Info{InContainer: true})
	if !dup.IsError || !strings.Contains(extractText(dup), "already exists") {
		t.Errorf("duplicate tag must refuse; got: %s", extractText(dup))
	}

	result, _, _ := handleRelease(context.Background(), ssh,
		WorkflowInput{Service: "weather", ReleaseVersion: "v1.0.1"}, stateDir, runtime.Info{InContainer: true})
	if result.IsError {
		t.Fatalf("release should succeed, got: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, `"status":"released"`) || !strings.Contains(body, "v1.0.1") {
		t.Errorf("released response malformed: %s", body)
	}
	var tagCmd string
	for _, cmd := range ssh.commands {
		if strings.Contains(cmd, "git tag -a") {
			tagCmd = cmd
		}
	}
	if tagCmd == "" {
		t.Fatalf("no tag command issued; commands: %v", ssh.commands)
	}
	for _, want := range []string{"git tag -a 'v1.0.1'", "push origin 'v1.0.1'", "-c credential.helper="} {
		if !strings.Contains(tagCmd, want) {
			t.Errorf("tag command missing %q:\n%s", want, tagCmd)
		}
	}
}
