package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// goneProcess is the PID of a process that has ended.
func goneProcess(t *testing.T) *launchOwner {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run a process to end: %v", err)
	}
	return &launchOwner{PID: cmd.Process.Pid, Start: "gone"}
}

// keptOpen is one admin mock behind every client the factory builds: each
// call's Close leaves it open for the next, as a fresh client would be.
type keptOpen struct {
	*platform.MockProjectAdminClient
}

func (keptOpen) Close() {}

// TestLaunchResume_LaunchingWithoutProject_ReadsItsHandle: a `launching`
// state with no production project recorded is held while the call that
// wrote it runs — in another zcp process while that process lives, in this
// one while its mutation holds the launch — however long ago it was written.
// Once that call is gone, the next one reads its handle before mutating: the
// production project by its name, with the staged launch token. A project of
// that name is recorded as the launch's orphan (reset cleans it up), never
// created a second time; none, and the launch runs again.
func TestLaunchResume_LaunchingWithoutProject_ReadsItsHandle(t *testing.T) {
	self := &launchOwner{PID: os.Getpid(), Start: workflow.CurrentProcessStartTime()}
	tests := []struct {
		name     string
		owner    func(t *testing.T) *launchOwner
		held     bool
		staged   bool
		projects []platform.Project
		// wantText is in the response; wantTarget the target project the
		// state records after it; wantStatus the state's status.
		wantText   string
		wantTarget string
		wantStatus topology.LaunchProductionStatus
		wantImport bool
	}{
		{"another live zcp process runs it", func(*testing.T) *launchOwner { return &launchOwner{PID: os.Getppid()} },
			false, true, nil, "in progress", "", topology.LaunchStatusLaunching, false},
		{"this process runs it", func(*testing.T) *launchOwner { return self },
			true, true, nil, "in progress", "", topology.LaunchStatusLaunching, false},
		{"this process, its call ended, nothing created", func(*testing.T) *launchOwner { return self },
			false, true, nil, "", "new-prod-id", topology.LaunchStatusLaunched, true},
		{"its process gone, nothing created", goneProcess,
			false, true, nil, "", "new-prod-id", topology.LaunchStatusLaunched, true},
		{"its process gone, the project created", goneProcess,
			false, true, []platform.Project{{ID: "orphan-id", Name: "myapp-prod"}, {ID: "other-id", Name: "other"}},
			"orphan-id", "orphan-id", topology.LaunchStatusFailed, false},
		{"its process gone, two projects of the name", goneProcess,
			false, true, []platform.Project{{ID: "first-id", Name: "myapp-prod"}, {ID: "second-id", Name: "myapp-prod"}},
			"second-id", "", topology.LaunchStatusLaunching, false},
		{"no process named, nothing staged", func(*testing.T) *launchOwner { return nil },
			false, false, nil, "delegation-unavailable", "", topology.LaunchStatusLaunching, false},
	}
	// The pre-mint state names no stage service: a call that ended with it on disk ended
	// before the full state write, so before the create — it launches again, with the token
	// a prior attempt staged, minting nothing.
	t.Run("its process gone before the full state write", func(t *testing.T) {
		stateDir := withTempState(t)
		installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
		sourceClient := pLP3MockClient()
		svc, err := ops.LookupService(context.Background(), sourceClient, "source-project-id", "app")
		if err != nil {
			t.Fatalf("lookup app service: %v", err)
		}
		if _, err := ops.EnvSetService(context.Background(), sourceClient, svc.ID, ops.LaunchTokenEnvKey, sentinelMintedToken, true); err != nil {
			t.Fatalf("pre-stage token: %v", err)
		}
		launchID := generateLaunchID("source-project-id", "myapp-prod")
		if err := writeLaunchState(stateDir, &launchState{
			LaunchID:          launchID,
			SourceProjectID:   "source-project-id",
			TargetProjectName: "myapp-prod",
			Status:            topology.LaunchStatusLaunching,
			TokenAcquisition:  "delegated",
			MintedTokenName:   "zcp-launch-myapp-prod",
			Owner:             goneProcess(t),
		}); err != nil {
			t.Fatalf("seed state: %v", err)
		}
		mockAdmin := happyMockAdmin().WithProjects([]platform.Project{{ID: "unrelated-id", Name: "other"}})
		defer setProjectAdminClientFactory(func(string, string) (platform.ProjectAdminClient, error) {
			return keptOpen{mockAdmin}, nil
		})()

		if _, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
			delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), ""); err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		state, err := readLaunchState(stateDir, launchID)
		if err != nil {
			t.Fatalf("read state: %v", err)
		}
		if state.Status != topology.LaunchStatusLaunched || state.TargetProjectID != "new-prod-id" {
			t.Errorf("state = %s / %q, want launched / new-prod-id", state.Status, state.TargetProjectID)
		}
		if got := sourceClient.CallCounts["MintDelegatedLaunchToken"]; got != 0 {
			t.Errorf("minted %d launch tokens, want none", got)
		}
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := withTempState(t)
			installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
			sourceClient := pLP3MockClient()
			if tt.staged {
				svc, err := ops.LookupService(context.Background(), sourceClient, "source-project-id", "app")
				if err != nil {
					t.Fatalf("lookup app service: %v", err)
				}
				if _, err := ops.EnvSetService(context.Background(), sourceClient, svc.ID, ops.LaunchTokenEnvKey, sentinelMintedToken, true); err != nil {
					t.Fatalf("pre-stage token: %v", err)
				}
			}
			launchID := generateLaunchID("source-project-id", "myapp-prod")
			seed := &launchState{
				LaunchID:              launchID,
				SourceProjectID:       "source-project-id",
				TargetProjectName:     "myapp-prod",
				TargetServiceHostname: "app",
				Status:                topology.LaunchStatusLaunching,
				TokenAcquisition:      "delegated",
				MintedTokenName:       "zcp-launch-myapp-prod",
				Owner:                 tt.owner(t),
			}
			if err := writeLaunchState(stateDir, seed); err != nil {
				t.Fatalf("seed state: %v", err)
			}
			// Written a day ago: the age of a held launch frees nothing.
			seed.LastUpdate = time.Now().Add(-24 * time.Hour)
			if err := writeRawLaunchStateForTest(t, stateDir, seed); err != nil {
				t.Fatalf("seed state: %v", err)
			}
			if tt.held {
				launchesInFlight.Store(launchID, struct{}{})
				defer launchesInFlight.Delete(launchID)
			}
			mockAdmin := happyMockAdmin().WithProjects(tt.projects)
			defer setProjectAdminClientFactory(func(string, string) (platform.ProjectAdminClient, error) {
				return keptOpen{mockAdmin}, nil
			})()

			result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
				delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
			if err != nil {
				t.Fatalf("handleLaunchProduction: %v", err)
			}
			text := extractText(result)
			if tt.wantText != "" && !strings.Contains(text, tt.wantText) {
				t.Errorf("response lacks %q:\n%s", tt.wantText, text)
			}
			if got := mockAdmin.CapturedImportYAML != ""; got != tt.wantImport {
				t.Errorf("created the production project: %v, want %v", got, tt.wantImport)
			}
			state, err := readLaunchState(stateDir, launchID)
			if err != nil {
				t.Fatalf("read state: %v", err)
			}
			if state.TargetProjectID != tt.wantTarget || state.Status != tt.wantStatus {
				t.Errorf("state = %s / %q, want %s / %q", state.Status, state.TargetProjectID, tt.wantStatus, tt.wantTarget)
			}
			if got := sourceClient.CallCounts["MintDelegatedLaunchToken"]; got != 0 {
				t.Errorf("minted %d launch tokens, want none", got)
			}
		})
	}
}

// TestExecuteLaunchMutation_RecordsTheProjectBeforeAnythingElse: the
// production project's id is in the state file as soon as Zerops returns it,
// before the next call to Zerops — a call that ends after the create leaves
// its handle behind.
func TestExecuteLaunchMutation_RecordsTheProjectBeforeAnythingElse(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()
	launchID := generateLaunchID("source-project-id", "myapp-prod")
	mockAdmin := happyMockAdmin()
	var recorded string
	mockAdmin.OnGrantSelfRole = func() {
		state, err := readLaunchState(stateDir, launchID)
		if err == nil {
			recorded = state.TargetProjectID
		}
	}
	defer installMockAdminFactory(t, mockAdmin)()
	input := delegatedPublishInput()
	input.ConfirmLaunch = false
	input.LaunchKey = sentinelMintedToken

	if _, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
		input, stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), ""); err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	if recorded != "new-prod-id" {
		t.Errorf("state's target project when the next call went out = %q, want new-prod-id", recorded)
	}
}
