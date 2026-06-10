// P-LP-3 active compare gate pins — assert the source-snapshot
// invariant from plans/workflow-family-architecture-2026-05-14.md §8.
// The handler persists a baseline at classify-prompt → ready-to-launch
// and compares it against current source at the launching transition;
// drift refuses publish with a structured source-drift blocker.
//
//   1. TestPersistsSnapshotAtReadyToLaunch — baseline written at
//      ready-to-launch.
//   2. TestRefusesOnSourceDriftBetweenReadyAndPublish — real source
//      mutation between transitions is refused.
//   3. TestRefusesOnTamperedStateFile — out-of-band state mutation
//      (file tampered) is also refused.
//
// These three tests were RED before the Phase 0.5 fix landed and
// became permanent pins on the same commit that wired the gate
// (workflow_launch_production.go handler refactor).

package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// errSimulatedImport is the canned error returned by the admin mock's
// CreateAndImportProject in these tests. If Phase 0 code reaches the
// import step (i.e. fails to gate on drift earlier), the response
// surfaces this error — visible test signal that drift compare was
// not yet wired.
var errSimulatedImport = errors.New("simulated CreateAndImportProject failure for drift-gate test")

// stubSSHDeployer satisfies ops.SSHDeployer for tests. Each canned
// command-substring response wins on first match; unmatched commands
// return empty bytes + nil error (preserves the empty-string semantics
// that real readers handle).
type stubSSHDeployer struct {
	responses map[string][]byte
}

func (s *stubSSHDeployer) ExecSSH(_ context.Context, _ string, command string) ([]byte, error) {
	for key, resp := range s.responses {
		if strings.Contains(command, key) {
			return resp, nil
		}
	}
	return nil, nil
}

func (s *stubSSHDeployer) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// pLP3ContainerRuntime is the runtime info for SSH-mode test paths.
// Container mode → readSourceState SSH's into source service.
func pLP3ContainerRuntime() runtime.Info {
	return runtime.Info{
		InContainer: true,
	}
}

// pLP3MockClient assembles a mock client that supports Discover +
// project-env reads for a single runtime fixture.
func pLP3MockClient() *platform.Mock {
	return platform.NewMock().
		WithProject(&platform.Project{
			ID:     "source-project-id",
			Name:   "source-project",
			Status: "ACTIVE",
		}).
		WithServices([]platform.ServiceStack{
			{
				ID:     "svc-app",
				Name:   "app",
				Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeID:          "nodejs",
					ServiceStackTypeVersionName: "nodejs@22",
				},
			},
		}).
		WithService(&platform.ServiceStack{
			ID:     "svc-app",
			Name:   "app",
			Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeID:          "nodejs",
				ServiceStackTypeVersionName: "nodejs@22",
			},
		}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{Key: "LOG_LEVEL", Content: "info"},
		})
}

// pLP3SSHFrozen returns SSH responses matching a "frozen" baseline:
// commit "frozen-baseline-sha", canonical zerops.yaml, frozen remote.
func pLP3SSHFrozen() *stubSSHDeployer {
	return &stubSSHDeployer{
		responses: map[string][]byte{
			"git rev-parse HEAD":   []byte("frozen-baseline-sha\n"),
			"git remote get-url":   []byte("https://github.com/example/myapp\n"),
			"/var/www/zerops.yaml": []byte("zerops:\n  - setup: prod\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n      start: node dist/server.js\n"),
		},
	}
}

// pLP3SSHDrifted is identical to pLP3SSHFrozen EXCEPT the commit SHA
// — simulates a real source-state mutation between ready-to-launch
// and launching transitions.
func pLP3SSHDrifted() *stubSSHDeployer {
	return &stubSSHDeployer{
		responses: map[string][]byte{
			"git rev-parse HEAD":   []byte("drifted-after-ready-sha\n"),
			"git remote get-url":   []byte("https://github.com/example/myapp\n"),
			"/var/www/zerops.yaml": []byte("zerops:\n  - setup: prod\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n      start: node dist/server.js\n"),
		},
	}
}

// pLP3CompleteInput returns a WorkflowInput with everything ready
// EXCEPT LaunchKey. Triggers ready-to-launch branch.
func pLP3CompleteInput() WorkflowInput {
	return WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		Region:                "eu-central",
		TargetService:         "app",
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
	}
}

// TestPersistsSnapshotAtReadyToLaunch — Phase 0.5 RED — asserts that
// the handler writes a launchState file containing a populated
// SourceSnapshot at the classify-prompt → ready-to-launch transition.
//
// Phase 0 baseline: handler returns the ready-to-launch preview but
// does NOT write any state file. readLaunchState returns
// ErrLaunchStateMissing. Test fails RED.
//
// Phase 0.5 expectation: handler computes SourceSnapshot from the
// current source state (git SHA + yaml hash + envs + service list)
// and persists it in launchState at the transition. Subsequent
// mutation calls compare against this baseline.
func TestPersistsSnapshotAtReadyToLaunch(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	client := pLP3MockClient()
	ssh := pLP3SSHFrozen()
	rt := pLP3ContainerRuntime()
	input := pLP3CompleteInput() // no LaunchKey → ready-to-launch path

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, input, stateDir, rt, ssh, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != "ready-to-launch" {
		t.Fatalf("status: got %q want ready-to-launch (response body: %s)", resp.Status, extractText(result))
	}

	launchID := generateLaunchID("source-project-id", input.ProductionProjectName)
	state, err := readLaunchState(stateDir, launchID)
	if err != nil {
		t.Fatalf("readLaunchState after ready-to-launch transition: %v\n"+
			"Phase 0.5 fix must persist SourceSnapshot baseline at this transition.\n"+
			"Without persistence, executeLaunchMutation has no baseline to compare against.",
			err)
	}
	if state == nil {
		t.Fatal("nil state after ready-to-launch — expected populated launchState")
	}
	if state.SourceSnapshot.GitCommitSHA == "" {
		t.Errorf("SourceSnapshot.GitCommitSHA empty in persisted state — Phase 0.5 fix must populate it from source SSH read.\nstate=%+v", *state)
	}
	if state.SourceSnapshot.ZeropsYAMLSHA256 == "" {
		t.Errorf("SourceSnapshot.ZeropsYAMLSHA256 empty in persisted state — Phase 0.5 fix must populate it.\nstate=%+v", *state)
	}
}

// TestRefusesOnSourceDriftBetweenReadyAndPublish — Phase 0.5 RED —
// asserts that when the persisted SourceSnapshot baseline differs
// from the current source state at mutation time, the handler
// refuses to publish with a source-drift blocker AND preserves the
// existing state file unchanged.
//
// Phase 0 baseline: executeLaunchMutation ignores existing.SourceSnapshot,
// recomputes from current source, writes over the previous value. No
// real drift detection exists. Test fails RED.
//
// Phase 0.5 expectation: executeLaunchMutation reads existing.SourceSnapshot,
// recomputes current, compares; on mismatch returns failed status
// with source-drift blocker; state file's SourceSnapshot is NOT
// overwritten (the baseline is preserved for retry-with-acknowledge
// flow OR cleanup).
func TestRefusesOnSourceDriftBetweenReadyAndPublish(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	client := pLP3MockClient()
	input := pLP3CompleteInput()
	input.LaunchKey = sentinelLaunchKey
	launchID := generateLaunchID("source-project-id", input.ProductionProjectName)

	// Pre-seed launchState as if a prior ready-to-launch transition
	// captured a baseline snapshot.
	baseline := ops.SourceSnapshot{
		GitCommitSHA:      "frozen-baseline-sha",
		ZeropsYAMLSHA256:  "frozen-yaml-hash-aaa",
		ProjectEnvsDigest: "frozen-envs-digest-bbb",
		ServiceListDigest: "frozen-services-digest-ccc",
	}
	if err := writeLaunchState(stateDir, &launchState{
		LaunchID:          launchID,
		SourceProjectID:   "source-project-id",
		TargetProjectName: input.ProductionProjectName,
		Status:            topology.LaunchStatusReadyToLaunch,
		SourceSnapshot:    baseline,
	}); err != nil {
		t.Fatalf("seed pre-launch state: %v", err)
	}

	// SSH returns the DRIFTED current state (different git SHA).
	rt := pLP3ContainerRuntime()
	ssh := pLP3SSHDrifted()

	// Use a working admin mock so the handler proceeds PAST auth into
	// source-state read + (post-fix) the drift compare gate. We never
	// want CreateAndImportProject to actually run — set an import error
	// so if Phase 0 code reaches that step the test surfaces evidence.
	adminMock := platform.NewMockProjectAdminClient().WithImportError(errSimulatedImport)
	defer setProjectAdminClientFactory(func(launchKey, apiHost string) (platform.ProjectAdminClient, error) {
		return adminMock, nil
	})()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, input, stateDir, rt, ssh, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))

	// Primary assertion: response must indicate failed with a
	// source-drift signal.
	if resp.Status != "failed" {
		t.Errorf("response status: got %q want \"failed\" — drift detection must refuse before mutation.\nresponse: %s",
			resp.Status, extractText(result))
	}
	driftSignalFound := false
	for _, b := range resp.Blockers {
		if strings.Contains(strings.ToLower(b.ID+" "+b.Message), "drift") ||
			strings.Contains(strings.ToLower(b.ID+" "+b.Message), "source-state") ||
			strings.Contains(strings.ToLower(b.ID+" "+b.Message), "snapshot") {
			driftSignalFound = true
			break
		}
	}
	if !driftSignalFound {
		t.Errorf("no drift/snapshot/source-state blocker in response — expected structured signal for drift refusal.\nblockers: %+v\nresponse: %s",
			resp.Blockers, extractText(result))
	}

	// Secondary assertion: pre-existing state's SourceSnapshot must
	// be preserved. The mutation must not overwrite the baseline as
	// a side effect of the refusal path.
	after, readErr := readLaunchState(stateDir, launchID)
	if readErr != nil {
		t.Fatalf("read state after refused mutation: %v", readErr)
	}
	if after.SourceSnapshot != baseline {
		t.Errorf("SourceSnapshot was overwritten during refused mutation (expected preservation).\n"+
			"before: %+v\nafter:  %+v",
			baseline, after.SourceSnapshot)
	}
}

// TestRefusesOnTamperedStateFile — Phase 0.5 RED — asserts that
// when the persisted state file's SourceSnapshot doesn't match the
// current source AND the user supplies LaunchKey, the handler
// refuses (whether the mismatch came from real drift or external
// tampering). Symmetric to TestRefusesOnSourceDriftBetweenReadyAndPublish
// but the framing is "state was modified out-of-band".
//
// Phase 0 baseline: no integrity check; whatever's in the state
// file is treated as authoritative; mutation proceeds. Test fails RED.
//
// Phase 0.5 expectation: same compare gate as the drift test
// rejects unmatched baselines. The state file integrity is implicit
// — if its content differs from current source, the mutation refuses
// regardless of whether the source or the file diverged.
func TestRefusesOnTamperedStateFile(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	client := pLP3MockClient()
	input := pLP3CompleteInput()
	input.LaunchKey = sentinelLaunchKey
	launchID := generateLaunchID("source-project-id", input.ProductionProjectName)

	// Tampered baseline — values that no legitimate compute could
	// have produced from the frozen source state. Mutation must
	// refuse.
	tampered := ops.SourceSnapshot{
		GitCommitSHA:      "tampered-sha-0000000000000000",
		ZeropsYAMLSHA256:  "tampered-yaml-hash-zzz",
		ProjectEnvsDigest: "tampered-envs-digest-yyy",
		ServiceListDigest: "tampered-services-digest-xxx",
	}
	if err := writeLaunchState(stateDir, &launchState{
		LaunchID:          launchID,
		SourceProjectID:   "source-project-id",
		TargetProjectName: input.ProductionProjectName,
		Status:            topology.LaunchStatusReadyToLaunch,
		SourceSnapshot:    tampered,
	}); err != nil {
		t.Fatalf("seed tampered state: %v", err)
	}

	rt := pLP3ContainerRuntime()
	ssh := pLP3SSHFrozen() // source is unchanged — only state file was tampered

	adminMock := platform.NewMockProjectAdminClient().WithImportError(errSimulatedImport)
	defer setProjectAdminClientFactory(func(launchKey, apiHost string) (platform.ProjectAdminClient, error) {
		return adminMock, nil
	})()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, input, stateDir, rt, ssh, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	t.Logf("response: status=%q blockers=%+v", resp.Status, resp.Blockers)

	if resp.Status != "failed" {
		t.Errorf("response status: got %q want \"failed\" — tampered state must refuse before mutation.\nresponse: %s",
			resp.Status, extractText(result))
	}

	driftSignalFound := false
	for _, b := range resp.Blockers {
		lower := strings.ToLower(b.ID + " " + b.Message)
		if strings.Contains(lower, "drift") ||
			strings.Contains(lower, "source-state") ||
			strings.Contains(lower, "snapshot") ||
			strings.Contains(lower, "tampered") {
			driftSignalFound = true
			break
		}
	}
	if !driftSignalFound {
		t.Errorf("no drift/snapshot/source-state/tampered blocker in response — expected structured signal for tampered-state refusal.\nblockers: %+v\nresponse: %s",
			resp.Blockers, extractText(result))
	}

	// State file must not be silently rewritten over the tampered
	// baseline — refusal preserves evidence of the tamper.
	after, readErr := readLaunchState(stateDir, launchID)
	if readErr != nil {
		t.Fatalf("read state after refused mutation: %v", readErr)
	}
	t.Logf("after.SourceSnapshot=%+v", after.SourceSnapshot)
	if after.SourceSnapshot != tampered {
		t.Errorf("tampered SourceSnapshot was overwritten — refusal path should preserve baseline for operator forensics.\n"+
			"before (tampered): %+v\nafter:             %+v",
			tampered, after.SourceSnapshot)
	}
}
