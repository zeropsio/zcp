package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// holdLaunch holds the launch's lock the way another call mutating it does,
// until the test ends.
func holdLaunch(t *testing.T, stateDir, launchID string) {
	t.Helper()
	held, err := tryLaunchLock(stateDir, launchID)
	if err != nil {
		t.Fatalf("hold the launch: %v", err)
	}
	t.Cleanup(held.release)
}

// keptOpen is one admin mock behind every client the factory builds: each
// call's Close leaves it open for the next, as a fresh client would be.
type keptOpen struct {
	*platform.MockProjectAdminClient
}

func (keptOpen) Close() {}

// TestLaunchResume_LaunchingWithoutProject_ReadsItsHandle: a launch is held
// by its lock while a call mutates it — in this zcp process or another, the
// lock going with the call or the process that held it — however long ago
// its state was written; a second call is refused. Once no call holds it, a
// `launching` state with no production project recorded is one whose call
// ended before recording what Zerops did: the next call reads its handle
// before mutating — the production project by its name, with the staged
// launch token. A project of that name is recorded as the launch's orphan
// (reset cleans it up), never created a second time; none, and the launch
// runs again.
func TestLaunchResume_LaunchingWithoutProject_ReadsItsHandle(t *testing.T) {
	tests := []struct {
		name     string
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
		{"another call holds it", true, true, nil, "in progress", "", topology.LaunchStatusLaunching, false},
		{"its call ended, nothing created", false, true, nil, "", "new-prod-id", topology.LaunchStatusLaunched, true},
		{"its call ended, the project created", false, true,
			[]platform.Project{{ID: "orphan-id", Name: "myapp-prod"}, {ID: "other-id", Name: "other"}},
			"orphan-id", "orphan-id", topology.LaunchStatusFailed, false},
		{"its call ended, two projects of the name", false, true,
			[]platform.Project{{ID: "first-id", Name: "myapp-prod"}, {ID: "second-id", Name: "myapp-prod"}},
			"second-id", "", topology.LaunchStatusLaunching, false},
		{"its call ended, nothing staged", false, false, nil, "delegation-unavailable", "", topology.LaunchStatusLaunching, false},
	}
	// The pre-mint state names no stage service: a call that ended with it on disk ended
	// before the full state write, so before the create — it launches again, with the token
	// a prior attempt staged, minting nothing.
	t.Run("its call ended before the full state write", func(t *testing.T) {
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
				holdLaunch(t, stateDir, launchID)
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
	heldMidway := false
	mockAdmin.OnGrantSelfRole = func() {
		state, err := readLaunchState(stateDir, launchID)
		if err == nil {
			recorded = state.TargetProjectID
		}
		other, err := tryLaunchLock(stateDir, launchID)
		heldMidway = errors.Is(err, errLaunchHeld)
		if err == nil {
			other.release()
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
	if !heldMidway {
		t.Error("a second call could take the launch while the mutation ran")
	}
	if after, err := tryLaunchLock(stateDir, launchID); err != nil {
		t.Errorf("the launch is still held after its call returned: %v", err)
	} else {
		after.release()
	}
}
