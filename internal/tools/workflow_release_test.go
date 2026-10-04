package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// stubPushProof overrides the launch push-proof reader (the P-LP-11 read
// the release act reuses) for the duration of one test.
func stubPushProof(t *testing.T, proof LaunchPushProofResult) {
	t.Helper()
	prev := launchPushProofReader
	launchPushProofReader = func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, _ string, _ string, _ string) (LaunchPushProofResult, error) {
		return proof, nil
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
	stubPushProof(t, LaunchPushProofResult{LocalHead: "abc123def456", RemoteHead: "abc123def456"})

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

// TestHandleRelease_LegacyMetaWithoutTrackedRef_ComparesHEAD pins the
// GF-7 regression fix (docs/spec-workflows.md §12.6) on the release
// path's reuse of the P-LP-11 freshness read. seedReleaseMeta writes no
// TrackedRef — every ServiceMeta written before GF-7 — and
// trackedRefOrDefault falls back to the literal "main" for all of
// them, the same false-block risk the launch gate's P3 check carries.
// The release freshness read must fall back to launchPushProofRef's
// "HEAD" (the pre-GF-7 remote-default compare) instead.
func TestHandleRelease_LegacyMetaWithoutTrackedRef_ComparesHEAD(t *testing.T) {
	// non-parallel: stubs the package-level push-proof reader.
	stateDir := t.TempDir()
	seedReleaseMeta(t, stateDir, nil)

	var gotTrackedRef string
	prev := launchPushProofReader
	launchPushProofReader = func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, _ string, _ string, trackedRef string) (LaunchPushProofResult, error) {
		gotTrackedRef = trackedRef
		return LaunchPushProofResult{LocalHead: "abc123def456", RemoteHead: "abc123def456"}, nil
	}
	t.Cleanup(func() { launchPushProofReader = prev })

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "ls-remote --tags") {
				return []byte(""), nil
			}
			return []byte("ok"), nil
		},
	}

	result, _, _ := handleRelease(context.Background(), ssh,
		WorkflowInput{Service: "weather"}, stateDir, runtime.Info{InContainer: true})
	if result.IsError {
		t.Fatalf("expected release-prompt, got error: %s", extractText(result))
	}
	if gotTrackedRef != "HEAD" {
		t.Errorf("trackedRef passed to push-proof reader for a legacy meta (no TrackedRef): got %q want %q", gotTrackedRef, "HEAD")
	}
}

// TestHandleRelease_RefusesUnpushedState pins the freshness gate: dirty
// tree or HEAD-not-on-remote refuse the release — a tag must name
// exactly the pushed state the production pipeline builds.
func TestHandleRelease_RefusesUnpushedState(t *testing.T) {
	t.Run("dirty tree", func(t *testing.T) {
		stateDir := t.TempDir()
		seedReleaseMeta(t, stateDir, nil)
		stubPushProof(t, LaunchPushProofResult{DirtyTree: true, LocalHead: "a", RemoteHead: "a"})
		result, _, _ := handleRelease(context.Background(), &containerSSHStub{},
			WorkflowInput{Service: "weather"}, stateDir, runtime.Info{InContainer: true})
		if !result.IsError || !strings.Contains(extractText(result), "uncommitted") {
			t.Errorf("dirty tree must refuse the release; got: %s", extractText(result))
		}
	})
	t.Run("head not pushed", func(t *testing.T) {
		stateDir := t.TempDir()
		seedReleaseMeta(t, stateDir, nil)
		stubPushProof(t, LaunchPushProofResult{LocalHead: "aaa", RemoteHead: "bbb"})
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
	stubPushProof(t, LaunchPushProofResult{LocalHead: "abc123def456", RemoteHead: "abc123def456"})

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

// Non-parallel: isolates enrollment with HOME and stubs the push-proof reader.
func TestHandleRelease_HQHandsOffBeforeLegacyPreflight(t *testing.T) {
	tests := []struct {
		name         string
		enrolled     bool
		pairRecorded bool
		input        WorkflowInput
	}{
		{name: "enrolled without service", enrolled: true},
		{name: "enrolled without bootstrap", enrolled: true, input: WorkflowInput{Service: "missing"}},
		{name: "enrolled prompt", enrolled: true, input: WorkflowInput{Service: "weather"}},
		{name: "enrolled version confirmed", enrolled: true, input: WorkflowInput{Service: "weather", ReleaseVersion: "v1.0.1"}},
		{name: "enrolled legacy version invalid", enrolled: true, input: WorkflowInput{Service: "weather", ReleaseVersion: "invalid"}},
		{name: "recorded HQ pair without enrollment", pairRecorded: true, input: WorkflowInput{Service: "weather", ReleaseVersion: "v1.0.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			if tt.enrolled {
				if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: "https://hq.example", ProjectID: "p1", Credential: "test-credential"}); err != nil {
					t.Fatal(err)
				}
			}
			stateDir := t.TempDir()
			seedReleaseMeta(t, stateDir, nil)
			if tt.pairRecorded {
				if err := workflow.UpsertServiceMeta(stateDir, "weather", func(meta *workflow.ServiceMeta, _ bool) error {
					meta.HQ = &workflow.HQRepoRef{AppID: "app1", Repo: "weather"}
					meta.GitPushState = topology.GitPushUnconfigured
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			proofReads := 0
			prev := launchPushProofReader
			launchPushProofReader = func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, _ string, _ string, _ string) (LaunchPushProofResult, error) {
				proofReads++
				return LaunchPushProofResult{LocalHead: "abc123def456", RemoteHead: "abc123def456"}, nil
			}
			t.Cleanup(func() { launchPushProofReader = prev })
			ssh := &containerSSHStub{}
			result, structured, err := handleRelease(context.Background(), ssh, tt.input, stateDir, runtime.Info{InContainer: true})
			if err != nil || structured != nil || result == nil || result.IsError {
				t.Fatalf("expected a person handoff, got result=%+v structured=%+v err=%v", result, structured, err)
			}
			var body map[string]string
			if err := json.Unmarshal([]byte(extractText(result)), &body); err != nil {
				t.Fatal(err)
			}
			if body["status"] != "release-person-required" {
				t.Errorf("status = %q, want release-person-required", body["status"])
			}
			for _, want := range []string{"person", "Mate", "projects page", "Review release", "Release"} {
				if !strings.Contains(body["nextStep"], want) {
					t.Errorf("nextStep missing %q: %s", want, body["nextStep"])
				}
			}
			if proofReads != 0 || len(ssh.commands) != 0 {
				t.Errorf("HQ handoff ran legacy preflight: proofReads=%d SSH=%v", proofReads, ssh.commands)
			}
		})
	}
}

// Non-parallel: isolates the enrollment path via HOME.
func TestHandleRelease_HQExplainsProductionWithoutLegacyPipelineAdvice(t *testing.T) {
	for _, launches := range []struct {
		name string
		refs []workflow.ProdLaunchRef
	}{
		{name: "no legacy production launch"},
		{name: "stale legacy production launch", refs: []workflow.ProdLaunchRef{{ProdProjectID: "old-prod", ProdHostname: "weather"}}},
	} {
		t.Run(launches.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: "https://hq.example", ProjectID: "p1", Credential: "test-credential"}); err != nil {
				t.Fatal(err)
			}
			stateDir := t.TempDir()
			seedReleaseMeta(t, stateDir, launches.refs)
			result, _, err := handleRelease(context.Background(), nil, WorkflowInput{Service: "weather"}, stateDir, runtime.Info{InContainer: true})
			if err != nil || result == nil || result.IsError {
				t.Fatalf("expected person handoff, got result=%+v err=%v", result, err)
			}
			var body map[string]string
			text := extractText(result)
			if err := json.Unmarshal([]byte(text), &body); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"HQ Core", "approved releases", "has not checked"} {
				if !strings.Contains(body["pipeline"], want) {
					t.Errorf("pipeline missing %q: %s", want, body["pipeline"])
				}
			}
			for _, want := range []string{"Add production", "Merge", "Review release", "Release"} {
				if !strings.Contains(body["nextStep"], want) {
					t.Errorf("nextStep missing %q: %s", want, body["nextStep"])
				}
			}
			for _, unwanted := range []string{"No production launch is recorded", "launch-production", "CI", "Actions", "releaseVersion", "re-call", "git tag"} {
				if strings.Contains(text, unwanted) {
					t.Errorf("HQ handoff gives legacy recovery advice %q: %s", unwanted, text)
				}
			}
		})
	}
}
