// Tests for GF-7 (docs/spec-workflows.md §12.6): git-push-setup records
// the tracked ref ONCE, at the first confirm that configures a pair —
// detected from the push source's current branch, falling back to the
// remote's default branch, falling back to "main". An explicit trackedRef
// input always overrides and skips detection entirely.
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestGitPushSetupContainer_RecordsTrackedRef_FromCurrentBranch(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "symbolic-ref --short HEAD") {
				return []byte("release\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_ok"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("confirm failed: %s", extractText(result))
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta == nil || meta.TrackedRef != "release" {
		t.Errorf("meta.TrackedRef = %+v, want %q (detected current branch)", meta, "release")
	}
	if !strings.Contains(extractText(result), "release") {
		t.Errorf("response should surface the recorded trackedRef; got: %s", extractText(result))
	}
}

func TestGitPushSetupContainer_RecordsTrackedRef_DetachedFallsBackToRemoteDefault(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "symbolic-ref --short HEAD") {
				return []byte(""), nil // detached/unborn — no output
			}
			if strings.Contains(cmd, "ls-remote --symref") {
				return []byte("ref: refs/heads/develop\tHEAD\nabc123\tHEAD\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_ok"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("confirm failed: %s", extractText(result))
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta == nil || meta.TrackedRef != "develop" {
		t.Errorf("meta.TrackedRef = %+v, want %q (remote default branch)", meta, "develop")
	}
}

func TestGitPushSetupContainer_RecordsTrackedRef_ExplicitOverride(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	ssh := &containerSSHStub{}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, nil, ssh, "test-project",
		WorkflowInput{
			Service:    "appdev",
			RemoteURL:  "https://github.com/example/app.git",
			GitToken:   "ghp_ok",
			TrackedRef: "release",
		},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("confirm failed: %s", extractText(result))
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta == nil || meta.TrackedRef != "release" {
		t.Errorf("meta.TrackedRef = %+v, want %q (explicit override)", meta, "release")
	}
	for _, cmd := range ssh.commands {
		if strings.Contains(cmd, "symbolic-ref") {
			t.Errorf("explicit trackedRef override must skip detection entirely; got SSH call: %s", cmd)
		}
	}
}

func TestGitPushSetupLocal_RecordsTrackedRef_ExplicitOverride(t *testing.T) {
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	defer setLocalGitProbeReader(func(context.Context, string, string) error { return nil })()
	defer setLocalGitOriginSyncer(func(context.Context, string, string) error { return nil })()
	detectorCalled := false
	defer setLocalGitBranchDetector(func(context.Context, string, string) (string, error) {
		detectorCalled = true
		return "should-not-be-used", nil
	})()

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, nil, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", TrackedRef: "release"},
		stateDir, runtime.Info{InContainer: false},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	if detectorCalled {
		t.Error("explicit trackedRef override must skip detection")
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta == nil || meta.TrackedRef != "release" {
		t.Errorf("meta.TrackedRef = %+v, want %q", meta, "release")
	}
}

func TestGitPushSetupLocal_RecordsTrackedRef_FromDetector(t *testing.T) {
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	defer setLocalGitProbeReader(func(context.Context, string, string) error { return nil })()
	defer setLocalGitOriginSyncer(func(context.Context, string, string) error { return nil })()
	defer setLocalGitBranchDetector(func(context.Context, string, string) (string, error) {
		return "feature-x", nil
	})()

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, nil, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git"},
		stateDir, runtime.Info{InContainer: false},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta == nil || meta.TrackedRef != "feature-x" {
		t.Errorf("meta.TrackedRef = %+v, want %q", meta, "feature-x")
	}
}
