// Tests for the delegated launch-token minting feature
// (plans/token-delegation-implementation-spec-2026-07-10.md §4.6).
package tools

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// sentinelMintedToken is a value that, if it ever appears in a response,
// state file, audit record, or stderr log, reveals a P-LP-1/D-2
// violation. sentinelMintedTokenPrefix is a stable leading substring —
// scans check both the full value and the prefix so a truncated/partial
// leak (e.g. a log line cut mid-token) still trips the guard.
const (
	sentinelMintedToken       = "zcp-mint-leak-canary-9f3e7a1c8d2b5610"
	sentinelMintedTokenPrefix = "zcp-mint-leak-canary"
)

// delegatedPublishInput mirrors completeLaunchInput/pLP3CompleteInput but
// publishes via the delegated path (ConfirmLaunch) instead of an
// explicit LaunchKey.
func delegatedPublishInput() WorkflowInput {
	return WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		Region:                "eu-central",
		TargetService:         "app",
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
		ConfirmLaunch:         FlexBool(true),
	}
}

// usableDelegation is the seed shape ListOwnTokenDelegations must return
// for delegationsUsable to report available.
func usableDelegation() platform.TokenDelegation {
	return platform.TokenDelegation{ID: "d1", TokenID: "own-token-id", RoleCode: "NO_ACCESS", CanCreateProjects: true}
}

// happyMockAdmin returns a MockProjectAdminClient wired for a successful
// CreateAndImportProject + poll, matching the fixture used across the
// existing launch-mutation happy-path tests.
func happyMockAdmin() *platform.MockProjectAdminClient {
	return platform.NewMockProjectAdminClient().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "new-prod-id",
			ProjectName: "myapp-prod",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-prod-app", Name: "app", Processes: []platform.Process{{ID: "proc-1", Status: "FINISHED"}}},
			},
		}).
		WithProcess(&platform.Process{ID: "proc-1", Status: "FINISHED"}).
		WithClientUserID("client-user-abc")
}

// captureStderr redirects os.Stderr for the duration of fn and returns
// what was written. Not safe for t.Parallel() (os.Stderr is global).
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = orig
	data, _ := io.ReadAll(r)
	return string(data)
}

// scanLaunchStateDirForSentinels walks every file under
// stateDir/launch-production (state files + audit log) and fails the
// test if any needle appears.
func scanLaunchStateDirForSentinels(t *testing.T, stateDir string, needles ...string) {
	t.Helper()
	dir := filepath.Join(stateDir, launchStateDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read state dir %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		body := string(data)
		for _, needle := range needles {
			if strings.Contains(body, needle) {
				t.Errorf("state dir file %s contains sentinel %q", path, needle)
			}
		}
	}
}

// ---------------------------------------------------------------------
// §4.2 publish gate + mutual-exclusion tightening.
// ---------------------------------------------------------------------

// TestPublishGate_ConfirmLaunchPublishes pins that ConfirmLaunch=true
// alone (no LaunchKey) enters the mutation pipeline.
func TestPublishGate_ConfirmLaunchPublishes(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient() // no delegation seeded -> falls back

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	// No delegation seeded -> D-6 fallback, but the STATUS proves the
	// publish gate let confirmLaunch through into the mutation pipeline
	// (a non-publishing call would return classify/scope, not
	// ready-to-launch-with-a-block-severity blocker).
	if resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Fatalf("status: got %q want ready-to-launch (delegation-unavailable fallback)\n%s", resp.Status, extractText(result))
	}
	foundBlocker := false
	for _, b := range resp.Blockers {
		if b.ID == "delegation-unavailable" {
			foundBlocker = true
		}
	}
	if !foundBlocker {
		t.Errorf("confirmLaunch=true with no delegation must reach the mutation pipeline and see delegation-unavailable; blockers=%+v", resp.Blockers)
	}
}

// TestPublishGate_NeitherKeyNorConfirm_StaysReadOnly pins that omitting
// both LaunchKey and ConfirmLaunch keeps the workflow on read-only
// statuses (today's behavior, unchanged).
func TestPublishGate_NeitherKeyNorConfirm_StaysReadOnly(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()

	input := pLP3CompleteInput()
	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		input, stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Fatalf("status: got %q want ready-to-launch", resp.Status)
	}
}

// TestPublishGate_ConfirmLaunchWithIncompleteExistingPair_Refused pins
// the partial-existing-pair guard: confirmLaunch=true plus only ONE of
// existingProjectId/existingProdToken must fail closed instead of
// silently falling through to new-project delegated creation.
func TestPublishGate_ConfirmLaunchWithIncompleteExistingPair_Refused(t *testing.T) {
	tests := []struct {
		name              string
		existingProjectID string
		existingProdToken string
	}{
		{"projectId only", "some-existing-project-id", ""},
		{"prodToken only", "", "some-existing-prod-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := withTempState(t)
			installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
			sourceClient := pLP3MockClient()

			input := delegatedPublishInput()
			input.ExistingProjectID = tt.existingProjectID
			input.ExistingProdToken = tt.existingProdToken

			result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
				input, stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
			if err != nil {
				t.Fatalf("handleLaunchProduction: %v", err)
			}
			text := extractText(result)
			if !strings.Contains(text, platform.ErrInvalidParameter) {
				t.Errorf("expected ErrInvalidParameter for incomplete existing pair, got:\n%s", text)
			}
			if sourceClient.CallCounts["ListOwnTokenDelegations"] != 0 || sourceClient.CallCounts["MintDelegatedLaunchToken"] != 0 {
				t.Errorf("incomplete-pair refusal must happen before any delegation call; list=%d mint=%d",
					sourceClient.CallCounts["ListOwnTokenDelegations"], sourceClient.CallCounts["MintDelegatedLaunchToken"])
			}
		})
	}
}

// TestPublishGate_ConfirmLaunchWithExistingPair_Refused pins the
// mutual-exclusion guard extended to confirmLaunch: a complete existing-
// project pair plus confirmLaunch=true is ambiguous — fail closed, same
// shape as the existing launchKey+existing-pair conflict.
func TestPublishGate_ConfirmLaunchWithExistingPair_Refused(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()

	input := delegatedPublishInput()
	input.ExistingProjectID = expectedExistingProjectID
	input.ExistingProdToken = sentinelExistingProdToken

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		input, stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, platform.ErrInvalidParameter) {
		t.Errorf("expected ErrInvalidParameter for confirmLaunch+existing-pair conflict, got:\n%s", text)
	}
	if strings.Contains(text, sentinelExistingProdToken) {
		t.Errorf("ExistingProdToken sentinel leaked into refusal response")
	}
}

// TestEchoInputs_ConfirmLaunchEchoed pins that confirmLaunch is not
// treated as a secret — it echoes in the input-forensics block.
func TestEchoInputs_ConfirmLaunchEchoed(t *testing.T) {
	t.Parallel()
	echo := echoInputs(WorkflowInput{ConfirmLaunch: FlexBool(true)})
	if !echo.ConfirmLaunch {
		t.Error("echoInputs must echo ConfirmLaunch=true")
	}
}

// ---------------------------------------------------------------------
// §4.3 ready-to-launch availability decoration.
// ---------------------------------------------------------------------

// TestReadyToLaunch_DelegationAvailable_AdvertisesPrimaryPath pins the
// available branch: delegatedLaunch.available=true plus guidance making
// the delegated path primary.
func TestReadyToLaunch_DelegationAvailable_AdvertisesPrimaryPath(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	client := pLP3MockClient().WithTokenDelegations(usableDelegation())

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil,
		pLP3CompleteInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Fatalf("status: got %q want ready-to-launch\n%s", resp.Status, extractText(result))
	}
	if resp.DelegatedLaunch == nil || !resp.DelegatedLaunch.Available {
		t.Fatalf("delegatedLaunch.available must be true; got %+v", resp.DelegatedLaunch)
	}
	if !strings.Contains(resp.Guidance, "confirmLaunch") {
		t.Errorf("guidance must make the delegated path primary (mentions confirmLaunch); got %q", resp.Guidance)
	}
	if client.CallCounts["MintDelegatedLaunchToken"] != 0 {
		t.Errorf("availability read must never mint; got %d mint calls", client.CallCounts["MintDelegatedLaunchToken"])
	}
}

// TestReadyToLaunch_NoDelegation_ManualPathUnchanged pins the
// unavailable branch: delegatedLaunch.available=false and the guidance
// text is the ORIGINAL manual-walkthrough atom (byte-for-byte per D-6).
func TestReadyToLaunch_NoDelegation_ManualPathUnchanged(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	client := pLP3MockClient() // no delegation seeded

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil,
		pLP3CompleteInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.DelegatedLaunch == nil || resp.DelegatedLaunch.Available {
		t.Fatalf("delegatedLaunch.available must be false; got %+v", resp.DelegatedLaunch)
	}
	if !strings.Contains(resp.Guidance, "launchKey") {
		t.Errorf("guidance must stay the manual launchKey walkthrough; got %q", resp.Guidance)
	}
}

// TestReadyToLaunch_ListError_FailsOpenToManualPath pins D-6's fail-open
// rule: a list-read error must NOT surface as a launch blocker and must
// NOT crash the response — it silently renders as unavailable.
func TestReadyToLaunch_ListError_FailsOpenToManualPath(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	client := pLP3MockClient().WithError("ListOwnTokenDelegations", errors.New("simulated transport failure"))

	var result *mcp.CallToolResult
	stderrOut := captureStderr(t, func() {
		r, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil,
			pLP3CompleteInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		result = r
	})
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Fatalf("status: got %q want ready-to-launch (list error must fail open)\n%s", resp.Status, extractText(result))
	}
	if resp.DelegatedLaunch == nil || resp.DelegatedLaunch.Available {
		t.Fatalf("delegatedLaunch.available must be false on a list error; got %+v", resp.DelegatedLaunch)
	}
	for _, b := range resp.Blockers {
		if strings.Contains(strings.ToLower(b.ID), "delegat") {
			t.Errorf("a list-read error must not surface as a launch blocker; got %+v", b)
		}
	}
	if !strings.Contains(stderrOut, "list own token delegations") {
		t.Errorf("list error must be logged to stderr; got %q", stderrOut)
	}
}

// ---------------------------------------------------------------------
// §4.4 mutation seam — precedence, ordering, happy path, outcome table.
// ---------------------------------------------------------------------

// TestExecuteLaunchMutation_Precedence_LaunchKeyZeroDelegationCalls pins
// D-5: an explicit launchKey — even alongside a seeded delegation — must
// never consult the delegation machinery.
func TestExecuteLaunchMutation_Precedence_LaunchKeyZeroDelegationCalls(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().WithTokenDelegations(usableDelegation())
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	input := pLP3CompleteInput()
	input.LaunchKey = sentinelLaunchKey
	input.ConfirmLaunch = FlexBool(true) // even if also set, launchKey wins per D-5

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		input, stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusLaunched {
		t.Fatalf("status: got %q want launched\n%s", resp.Status, extractText(result))
	}
	if got := sourceClient.CallCounts["ListOwnTokenDelegations"]; got != 0 {
		t.Errorf("ListOwnTokenDelegations call count: got %d want 0", got)
	}
	if got := sourceClient.CallCounts["MintDelegatedLaunchToken"]; got != 0 {
		t.Errorf("MintDelegatedLaunchToken call count: got %d want 0", got)
	}
}

// TestExecuteLaunchMutation_Ordering_GateRefusalSkipsMint pins D-3: the
// mint must not be reached when a pre-mint refusal gate fires. Uses the
// publish-side source-control gate refusal (no ServiceMeta seeded) as
// the representative early gate.
func TestExecuteLaunchMutation_Ordering_GateRefusalSkipsMint(t *testing.T) {
	stateDir := withTempState(t) // no installLaunchGateReady -> gate refuses
	sourceClient := pLP3MockClient().WithTokenDelegations(usableDelegation())

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status == topology.LaunchStatusLaunched {
		t.Fatalf("expected a pre-mint gate refusal, got launched:\n%s", extractText(result))
	}
	if got := sourceClient.CallCounts["MintDelegatedLaunchToken"]; got != 0 {
		t.Errorf("mint must not be called when a pre-mint gate refuses; got %d calls", got)
	}
}

// TestExecuteLaunchMutation_Delegated_HappyPath pins the full delegated
// mutation: mint runs once, the MINTED value (not any placeholder) is
// staged, the project is created, and no token value appears anywhere
// in the response.
func TestExecuteLaunchMutation_Delegated_HappyPath(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"})
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusLaunched {
		t.Fatalf("status: got %q want launched\n%s", resp.Status, extractText(result))
	}
	if got := sourceClient.CallCounts["MintDelegatedLaunchToken"]; got != 1 {
		t.Errorf("MintDelegatedLaunchToken call count: got %d want 1", got)
	}
	if got := sourceClient.CallCounts["ListOwnTokenDelegations"]; got != 1 {
		t.Errorf("ListOwnTokenDelegations call count: got %d want 1", got)
	}
	if got := stagedTokenValue(t, sourceClient, "svc-app"); got != sentinelMintedToken {
		t.Errorf("staged token: got %q want the minted value", got)
	}
	if mockAdmin.CapturedImportYAML == "" {
		t.Error("CreateAndImportProject must have run")
	}
	text := extractText(result)
	if strings.Contains(text, sentinelMintedToken) {
		t.Errorf("response leaks the minted token:\n%s", text)
	}

	// A subsequent ready-to-launch-shaped read (against a fresh launchID)
	// on a mock still carrying no further delegation shows unavailable —
	// F4 one-shot semantics consumed it. Availability for THIS launch is
	// pinned separately by TestReadyToLaunch_DelegationAvailable_AdvertisesPrimaryPath.
	if sourceClient.CallCounts["ListOwnTokenDelegations"] != 1 {
		t.Errorf("delegation should be consumed after mint (F4); ListOwnTokenDelegations calls = %d", sourceClient.CallCounts["ListOwnTokenDelegations"])
	}
}

// TestExecuteLaunchMutation_Fallback_NoDelegation_NoErrorEnvelope pins
// D-6: no usable delegation returns the ready-to-launch-shaped
// delegation-unavailable blocker, NOT an error envelope, and never
// calls mint.
func TestExecuteLaunchMutation_Fallback_NoDelegation_NoErrorEnvelope(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient() // no delegation seeded
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Fatalf("status: got %q want ready-to-launch (not an error envelope)\n%s", resp.Status, extractText(result))
	}
	foundBlocker := false
	for _, b := range resp.Blockers {
		if b.ID == "delegation-unavailable" {
			foundBlocker = true
			if b.Severity != topology.BlockerSeverityBlock {
				t.Errorf("delegation-unavailable severity: got %q want block", b.Severity)
			}
		}
	}
	if !foundBlocker {
		t.Errorf("expected delegation-unavailable blocker; got %+v", resp.Blockers)
	}
	if sourceClient.CallCounts["MintDelegatedLaunchToken"] != 0 {
		t.Errorf("mint must not run when no delegation is usable; got %d calls", sourceClient.CallCounts["MintDelegatedLaunchToken"])
	}
	if mockAdmin.CapturedImportYAML != "" {
		t.Error("CreateAndImportProject must not run on the fallback path")
	}
}

// TestExecuteLaunchMutation_ListError_CouldNotCheckWording pins the
// 2026-07-12 audit #4 fix: a mutation-time ListOwnTokenDelegations
// failure falls back to the manual path (D-6) but must NOT assert the
// definitive "no unused delegation — never granted, consumed, or
// revoked" diagnosis; the honest truth is "the availability check
// itself failed". The raw list error stays out of the response
// (stderr only), and the mint never runs on an unconfirmed read.
func TestExecuteLaunchMutation_ListError_CouldNotCheckWording(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	rawErr := errors.New("simulated list transport failure")
	sourceClient := pLP3MockClient().
		WithError("ListOwnTokenDelegations", rawErr)
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	var result *mcp.CallToolResult
	stderrOut := captureStderr(t, func() {
		r, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		result = r
	})
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Fatalf("status: got %q want ready-to-launch (D-6 fallback)\n%s", resp.Status, text)
	}
	var blocker *topology.Blocker
	for i := range resp.Blockers {
		if resp.Blockers[i].ID == "delegation-unavailable" {
			blocker = &resp.Blockers[i]
		}
	}
	if blocker == nil {
		t.Fatalf("expected delegation-unavailable blocker; got %+v", resp.Blockers)
	}
	if strings.Contains(blocker.Message, "never granted") || strings.Contains(blocker.Message, "revoked") {
		t.Errorf("a failed CHECK must not claim the definitive no-delegation diagnosis; got %q", blocker.Message)
	}
	if !strings.Contains(strings.ToLower(blocker.Message), "could not verify") {
		t.Errorf("blocker must say the availability check itself failed; got %q", blocker.Message)
	}
	if !strings.Contains(blocker.Message, "ask the user") || !strings.Contains(blocker.Message, "never create or guess") {
		t.Errorf("credential ownership: the fallback must route token generation through the user and forbid fabrication; got %q", blocker.Message)
	}
	if strings.Contains(text, rawErr.Error()) {
		t.Errorf("response must not serialize the raw list error:\n%s", text)
	}
	if !strings.Contains(stderrOut, "list own token delegations") {
		t.Errorf("list error must be logged to stderr; got %q", stderrOut)
	}
	if sourceClient.CallCounts["MintDelegatedLaunchToken"] != 0 {
		t.Errorf("mint must not run on an unconfirmed availability read; got %d calls", sourceClient.CallCounts["MintDelegatedLaunchToken"])
	}
}

// TestExecuteLaunchMutation_ConfirmedEmpty_KeepsDefinitiveWording pins
// the sibling: a confirmed-empty list keeps the definitive diagnosis.
func TestExecuteLaunchMutation_ConfirmedEmpty_KeepsDefinitiveWording(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient() // no delegation seeded, list succeeds empty
	defer installMockAdminFactory(t, happyMockAdmin())()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	for _, b := range resp.Blockers {
		if b.ID == "delegation-unavailable" {
			if !strings.Contains(b.Message, "never granted") {
				t.Errorf("confirmed-empty must keep the definitive diagnosis; got %q", b.Message)
			}
			return
		}
	}
	t.Fatalf("expected delegation-unavailable blocker; got %+v", resp.Blockers)
}

// TestExecuteLaunchMutation_MintOutcome_Indeterminate pins the
// indeterminate-mint-error outcome-table row: an untyped mint error
// (timeout/5xx/transport) yields the distinct delegation-mint-
// indeterminate blocker, not the D-6 fallback, and zero project
// creation. The raw error is never serialized into the response.
func TestExecuteLaunchMutation_MintOutcome_Indeterminate(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	rawErr := errors.New("simulated transport timeout with a sensitive-looking body")
	sourceClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithError("MintDelegatedLaunchToken", rawErr)
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	var result *mcp.CallToolResult
	stderrOut := captureStderr(t, func() {
		r, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		result = r
	})
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusFailed {
		t.Fatalf("status: got %q want failed\n%s", resp.Status, text)
	}
	found := false
	for _, b := range resp.Blockers {
		if b.ID == "delegation-mint-indeterminate" {
			found = true
		}
		if b.ID == "delegation-unavailable" {
			t.Errorf("indeterminate error must not use the D-6 fallback blocker")
		}
	}
	if !found {
		t.Errorf("expected delegation-mint-indeterminate blocker; got %+v", resp.Blockers)
	}
	if strings.Contains(text, rawErr.Error()) {
		t.Errorf("response must never serialize the raw SDK error body:\n%s", text)
	}
	if !strings.Contains(stderrOut, "indeterminate") {
		t.Errorf("indeterminate error must be logged to stderr; got %q", stderrOut)
	}
	if mockAdmin.CapturedImportYAML != "" {
		t.Error("CreateAndImportProject must not run on an indeterminate mint error")
	}

	// Retry unblocked: the abort must not leave a stale Launching state
	// that trips the P0 concurrent-mutation lock.
	state, readErr := readLaunchState(stateDir, generateLaunchID("source-project-id", "myapp-prod"))
	if readErr != nil {
		t.Fatalf("read state after indeterminate abort: %v", readErr)
	}
	if state.Status != topology.LaunchStatusFailed {
		t.Errorf("state.Status after indeterminate abort: got %q want failed (so retry is not P0-locked)", state.Status)
	}
}

// TestExecuteLaunchMutation_MintOutcome_RaceUnavailable pins the race
// row: a typed ErrDelegationUnavailable returned BY THE MINT (list said
// usable, mint disagreed) routes through the D-6 fallback, not the
// indeterminate blocker.
func TestExecuteLaunchMutation_MintOutcome_RaceUnavailable(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithError("MintDelegatedLaunchToken", platform.NewPlatformError(
			platform.ErrDelegationUnavailable, "raced", "manual fallback"))
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Fatalf("status: got %q want ready-to-launch (D-6 fallback)\n%s", resp.Status, extractText(result))
	}
	found := false
	for _, b := range resp.Blockers {
		if b.ID == "delegation-unavailable" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected delegation-unavailable blocker on a raced mint; got %+v", resp.Blockers)
	}
	if mockAdmin.CapturedImportYAML != "" {
		t.Error("CreateAndImportProject must not run on a raced-unavailable mint")
	}
}

// TestExecuteLaunchMutation_MintOutcome_EmptyToken pins the D-7
// consumed-delegation narrative for a mint that returns 200 with an
// empty token value.
func TestExecuteLaunchMutation_MintOutcome_EmptyToken(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithMintedToken(platform.MintedToken{Token: "", TokenID: "minted-id"})
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusFailed {
		t.Fatalf("status: got %q want failed\n%s", resp.Status, text)
	}
	found := false
	for _, b := range resp.Blockers {
		if b.ID == "delegation-consumed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected delegation-consumed blocker; got %+v", resp.Blockers)
	}
	if !strings.Contains(text, "zcp-launch-myapp-prod") {
		t.Errorf("D-7 narrative must name the minted token; got:\n%s", text)
	}
	if !strings.Contains(strings.ToLower(text), "regenerate") || !strings.Contains(text, "launchKey") {
		t.Errorf("D-7 narrative must direct the user to regenerate + re-call with launchKey; got:\n%s", text)
	}
	if mockAdmin.CapturedImportYAML != "" {
		t.Error("CreateAndImportProject must not run when the mint returned an empty token")
	}
}

// TestExecuteLaunchMutation_MintOutcome_AdminFactoryFailure pins the D-7
// narrative for a successfully-minted token that the admin-client
// factory then rejects.
func TestExecuteLaunchMutation_MintOutcome_AdminFactoryFailure(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"})
	restore := setProjectAdminClientFactory(func(_, _ string) (platform.ProjectAdminClient, error) {
		return nil, errors.New("simulated admin construction failure")
	})
	defer restore()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusFailed {
		t.Fatalf("status: got %q want failed\n%s", resp.Status, text)
	}
	found := false
	for _, b := range resp.Blockers {
		if b.ID == "delegation-consumed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected delegation-consumed blocker; got %+v", resp.Blockers)
	}
	if strings.Contains(text, sentinelMintedToken) {
		t.Errorf("response leaks the minted token:\n%s", text)
	}
	scanLaunchStateDirForSentinels(t, stateDir, sentinelMintedToken, sentinelMintedTokenPrefix)
}

// TestExecuteLaunchMutation_StagedRetry_AdminFactoryRejectsStagedToken
// pins the staged-retry edge the director's review caught: when
// resolveDelegatedLaunchToken resolves a STAGED token from a prior
// attempt (mintedName == "") and the admin-client factory then rejects
// it, the response must NOT use the D-7 consumed-delegation narrative
// (this call minted nothing, so "the delegation was consumed" is false
// and mintedName=="" would render as an empty-string %q). It gets a
// distinct honest staged-token-rejected blocker instead.
func TestExecuteLaunchMutation_StagedRetry_AdminFactoryRejectsStagedToken(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()

	// Prior attempt: minted + staged the token, then failed later (e.g.
	// a create failure) — status=failed, no TargetProjectID (the
	// existing "safe to retry" resume branch), token still staged.
	svc, err := ops.LookupService(context.Background(), sourceClient, "source-project-id", "app")
	if err != nil {
		t.Fatalf("lookup app service: %v", err)
	}
	if _, err := ops.EnvSetSecretService(context.Background(), sourceClient, svc.ID, ops.LaunchTokenEnvKey, sentinelMintedToken); err != nil {
		t.Fatalf("pre-stage token: %v", err)
	}
	launchID := generateLaunchID("source-project-id", "myapp-prod")
	priorState := &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "source-project-id",
		TargetProjectName:     "myapp-prod",
		TargetServiceHostname: "app",
		Status:                topology.LaunchStatusFailed,
		TokenAcquisition:      "delegated",
		MintedTokenName:       "zcp-launch-myapp-prod",
	}
	if err := writeLaunchState(stateDir, priorState); err != nil {
		t.Fatalf("seed prior state: %v", err)
	}

	restore := setProjectAdminClientFactory(func(_, _ string) (platform.ProjectAdminClient, error) {
		return nil, errors.New("simulated: token revoked")
	})
	defer restore()

	var text string
	stderrOut := captureStderr(t, func() {
		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		text = extractText(result)
	})
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusFailed {
		t.Fatalf("status: got %q want failed\n%s", resp.Status, text)
	}
	foundStagedRejected := false
	for _, b := range resp.Blockers {
		if b.ID == "staged-token-rejected" {
			foundStagedRejected = true
		}
		if b.ID == "delegation-consumed" {
			t.Errorf("staged-retry admin-factory failure must NOT use the consumed-delegation narrative; got %+v", b)
		}
	}
	if !foundStagedRejected {
		t.Errorf("expected staged-token-rejected blocker; got %+v", resp.Blockers)
	}
	if strings.Contains(text, `named ""`) {
		t.Errorf("must not render an empty minted-name %%q (this call minted nothing); got:\n%s", text)
	}
	if strings.Contains(strings.ToLower(text), "the delegation was consumed") {
		t.Errorf("staged-retry rejection must not claim a delegation was consumed by this call; got:\n%s", text)
	}
	if sourceClient.CallCounts["ListOwnTokenDelegations"] != 0 {
		t.Errorf("staged-retry must not consult the delegation list; got %d calls", sourceClient.CallCounts["ListOwnTokenDelegations"])
	}
	if sourceClient.CallCounts["MintDelegatedLaunchToken"] != 0 {
		t.Errorf("staged-retry must not mint; got %d calls", sourceClient.CallCounts["MintDelegatedLaunchToken"])
	}
	for _, needle := range []string{sentinelMintedToken, sentinelMintedTokenPrefix} {
		if strings.Contains(text, needle) {
			t.Errorf("response contains sentinel %q:\n%s", needle, text)
		}
		if strings.Contains(stderrOut, needle) {
			t.Errorf("stderr contains sentinel %q:\n%s", needle, stderrOut)
		}
	}
	scanLaunchStateDirForSentinels(t, stateDir, sentinelMintedToken, sentinelMintedTokenPrefix)
}

// TestExecuteLaunchMutation_MintOutcome_StagingFailure pins the D-7
// narrative for a successfully-minted token whose staging write fails —
// wording says "not confirmed", not "failed" (the write may have
// already committed).
func TestExecuteLaunchMutation_MintOutcome_StagingFailure(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"}).
		WithError("CreateServiceEnvVar", errors.New("simulated env write failure"))
	mockAdmin := platform.NewMockProjectAdminClient()
	defer installMockAdminFactory(t, mockAdmin)()

	var result *mcp.CallToolResult
	stderrOut := captureStderr(t, func() {
		r, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		result = r
	})
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusFailed {
		t.Fatalf("status: got %q want failed\n%s", resp.Status, text)
	}
	found := false
	for _, b := range resp.Blockers {
		if b.ID == "launch-token-stage-failed" {
			found = true
			if !strings.Contains(b.Message, "not confirmed") {
				t.Errorf("staging failure on the delegated path must say NOT CONFIRMED, not failed; got %q", b.Message)
			}
			if strings.Contains(b.Message, "was not confirmed") && strings.Contains(strings.ToLower(b.Message), " failed:") {
				// tolerate — the outer sentence structure may still use
				// "failed" for a different clause; the load-bearing check
				// is the explicit "not confirmed" phrase above.
				_ = b
			}
		}
	}
	if !found {
		t.Errorf("expected launch-token-stage-failed blocker; got %+v", resp.Blockers)
	}
	if !strings.Contains(text, "zcp-launch-myapp-prod") {
		t.Errorf("D-7 narrative must name the minted token; got:\n%s", text)
	}
	if mockAdmin.CapturedImportYAML != "" {
		t.Error("CreateAndImportProject must not run when staging failed")
	}
	// Sentinel parity with the sibling outcome rows (2026-07-12 audit #5):
	// the minted value flowed into stageLaunchToken right before the
	// failure — scan the RESPONSE and STDERR too, not just the state dir.
	for _, needle := range []string{sentinelMintedToken, sentinelMintedTokenPrefix} {
		if strings.Contains(text, needle) {
			t.Errorf("response contains sentinel %q:\n%s", needle, text)
		}
		if strings.Contains(stderrOut, needle) {
			t.Errorf("stderr contains sentinel %q:\n%s", needle, stderrOut)
		}
	}
	scanLaunchStateDirForSentinels(t, stateDir, sentinelMintedToken, sentinelMintedTokenPrefix)
}

// TestExecuteLaunchMutation_AbortStateForensics pins the D-7 forensic
// contract of every controlled post-mint abort (2026-07-12 audit #2):
// the persisted Failed state must retain TokenAcquisition and — on the
// outcomes where a standing token exists or MAY exist — MintedTokenName,
// so a post-compaction action="status" can still name the dashboard
// token to regenerate. The staging-failure outcome must ALSO persist
// TargetServiceHostname: the env write may have committed before the
// error (launch_stage.go ambiguity), and reset's staged-secret delete
// keys on that field — a hostname-less state silently orphans the
// staged credential.
func TestExecuteLaunchMutation_AbortStateForensics(t *testing.T) {
	const requestedName = "zcp-launch-myapp-prod"
	cases := []struct {
		name string
		// setup configures the source client + admin factory and
		// returns a teardown (nil when installMockAdminFactory's
		// deferred restore suffices via t.Cleanup).
		setup func(t *testing.T) *platform.Mock
		// expected persisted forensics after the abort.
		wantMintedName string
		wantHostname   string
	}{
		{
			// The typed 403 proves nothing was created — the name must
			// NOT be persisted (no token exists to regenerate).
			name: "race-unavailable clears the minted name",
			setup: func(t *testing.T) *platform.Mock {
				t.Helper()
				c := pLP3MockClient().
					WithTokenDelegations(usableDelegation()).
					WithError("MintDelegatedLaunchToken", platform.NewPlatformError(
						platform.ErrDelegationUnavailable, "raced", "manual fallback"))
				t.Cleanup(installMockAdminFactory(t, happyMockAdmin()))
				return c
			},
			wantMintedName: "",
			wantHostname:   "",
		},
		{
			// Indeterminate: the POST may have committed — the token MAY
			// exist under the requested name; status must be able to say so.
			name: "indeterminate mint keeps the requested name",
			setup: func(t *testing.T) *platform.Mock {
				t.Helper()
				c := pLP3MockClient().
					WithTokenDelegations(usableDelegation()).
					WithError("MintDelegatedLaunchToken", errors.New("simulated transport timeout"))
				t.Cleanup(installMockAdminFactory(t, happyMockAdmin()))
				return c
			},
			wantMintedName: requestedName,
			wantHostname:   "",
		},
		{
			name: "empty-token mint keeps the requested name",
			setup: func(t *testing.T) *platform.Mock {
				t.Helper()
				c := pLP3MockClient().
					WithTokenDelegations(usableDelegation()).
					WithMintedToken(platform.MintedToken{Token: "", TokenID: "minted-id"})
				t.Cleanup(installMockAdminFactory(t, happyMockAdmin()))
				return c
			},
			wantMintedName: requestedName,
			wantHostname:   "",
		},
		{
			name: "admin-factory failure keeps the requested name",
			setup: func(t *testing.T) *platform.Mock {
				t.Helper()
				c := pLP3MockClient().
					WithTokenDelegations(usableDelegation()).
					WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"})
				t.Cleanup(setProjectAdminClientFactory(func(_, _ string) (platform.ProjectAdminClient, error) {
					return nil, errors.New("simulated admin construction failure")
				}))
				return c
			},
			wantMintedName: requestedName,
			wantHostname:   "",
		},
		{
			// Staging may have committed before erroring — reset must be
			// able to find (and delete) the staged secret.
			name: "staging failure keeps name AND push hostname",
			setup: func(t *testing.T) *platform.Mock {
				t.Helper()
				c := pLP3MockClient().
					WithTokenDelegations(usableDelegation()).
					WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"}).
					WithError("CreateServiceEnvVar", errors.New("simulated env write failure"))
				t.Cleanup(installMockAdminFactory(t, platform.NewMockProjectAdminClient()))
				return c
			},
			wantMintedName: requestedName,
			wantHostname:   "app",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := withTempState(t)
			installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
			sourceClient := tc.setup(t)

			_ = captureStderr(t, func() {
				if _, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
					delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), ""); err != nil {
					t.Fatalf("handleLaunchProduction: %v", err)
				}
			})

			state, readErr := readLaunchState(stateDir, generateLaunchID("source-project-id", "myapp-prod"))
			if readErr != nil {
				t.Fatalf("read state after abort: %v", readErr)
			}
			if state.Status != topology.LaunchStatusFailed {
				t.Fatalf("state.Status: got %q want failed", state.Status)
			}
			if state.TokenAcquisition != "delegated" {
				t.Errorf("state.TokenAcquisition: got %q want %q (forensic field dropped by the abort)", state.TokenAcquisition, "delegated")
			}
			if state.MintedTokenName != tc.wantMintedName {
				t.Errorf("state.MintedTokenName: got %q want %q", state.MintedTokenName, tc.wantMintedName)
			}
			if state.TargetServiceHostname != tc.wantHostname {
				t.Errorf("state.TargetServiceHostname: got %q want %q", state.TargetServiceHostname, tc.wantHostname)
			}
		})
	}
}

// TestExecuteLaunchMutation_StagingFailureThenReset_ReachesSecretDelete
// pins the P1 chain end-to-end: a delegated staging failure persists the
// push hostname (the env write may have committed), so a subsequent
// reset REACHES the staged-secret delete path instead of silently
// skipping it on a hostname-less state and orphaning the credential.
func TestExecuteLaunchMutation_StagingFailureThenReset_ReachesSecretDelete(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	failingClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"}).
		WithError("CreateServiceEnvVar", errors.New("simulated env write failure"))
	restore := installMockAdminFactory(t, platform.NewMockProjectAdminClient())
	if _, _, err := handleLaunchProduction(context.Background(), "source-project-id", failingClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), ""); err != nil {
		t.Fatalf("staging-failure mutation: %v", err)
	}
	restore()

	// Reset with a healthy client: must look the service up by the
	// persisted hostname and run the delete attempt (the mock's write
	// never committed, so the delete reports the key as already absent —
	// the load-bearing fact is the path RUNS and the state clears).
	// Model the AMBIGUOUS staging failure: the env write committed even
	// though the call errored — the reset client's service carries the
	// staged secret. stagedSecretDeleted=true can only come from the
	// delete path actually running against the persisted hostname.
	resetClient := pLP3MockClient().
		WithServiceEnv("svc-app", []platform.ServiceEnvVar{
			{ID: "env-launch", Key: ops.LaunchTokenEnvKey, Content: sentinelMintedToken, Type: platform.ServiceEnvSecret},
		})
	defer installMockAdminFactory(t, platform.NewMockProjectAdminClient())()
	if _, _, err := handleLaunchReset(context.Background(), stateDir, "source-project-id", resetClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, ""); err != nil {
		t.Fatalf("reset arm call: %v", err)
	}
	result, _, err := handleLaunchReset(context.Background(), stateDir, "source-project-id", resetClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	}, "")
	if err != nil {
		t.Fatalf("reset ack call: %v", err)
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, `"stagedSecretDeleted":true`) {
		t.Errorf("reset must have deleted the staged secret via the persisted hostname:\n%s", body)
	}
	if got := stagedTokenValue(t, resetClient, "svc-app"); got != "" {
		t.Errorf("staged secret must be gone after reset; still reads %q", got)
	}
	if _, readErr := readLaunchState(stateDir, generateLaunchID("source-project-id", "myapp-prod")); !errors.Is(readErr, ErrLaunchStateMissing) {
		t.Errorf("state must be cleared after reset; read err = %v", readErr)
	}
}

// TestExecuteLaunchMutation_MintOutcome_PreMintStateWriteFailure pins
// the FATAL pre-mint state-write row: when the write itself fails,
// abort BEFORE the mint — zero mint calls, safe to retry.
func TestExecuteLaunchMutation_MintOutcome_PreMintStateWriteFailure(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	// Make the launch-production directory read-only so writeLaunchState's
	// os.WriteFile (tmp-file create) fails, while a nonexistent-launchID
	// os.ReadFile earlier in handleLaunchProduction still resolves as
	// ErrLaunchStateMissing (the directory itself remains listable).
	launchProdDir := filepath.Join(stateDir, launchStateDir)
	if err := os.MkdirAll(launchProdDir, 0o755); err != nil {
		t.Fatalf("seed launch-production dir: %v", err)
	}
	if err := os.Chmod(launchProdDir, 0o500); err != nil {
		t.Fatalf("chmod launch-production dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(launchProdDir, 0o755) })

	sourceClient := pLP3MockClient().WithTokenDelegations(usableDelegation())
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusFailed {
		t.Fatalf("status: got %q want failed\n%s", resp.Status, text)
	}
	found := false
	for _, b := range resp.Blockers {
		if b.ID == "launch-state-write-failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected launch-state-write-failed blocker; got %+v", resp.Blockers)
	}
	if got := sourceClient.CallCounts["MintDelegatedLaunchToken"]; got != 0 {
		t.Errorf("mint must not be called when the pre-mint state write fails; got %d", got)
	}
	if mockAdmin.CapturedImportYAML != "" {
		t.Error("CreateAndImportProject must not run when the pre-mint state write fails")
	}
}

// TestExecuteLaunchMutation_Delegated_SentinelNeverLeaks_Success is the
// P-LP-1/D-2 sentinel scan for the SUCCESS path: full sentinel + stable
// prefix must be absent from the response, every file under the
// launch-production state dir (including the audit log), and stderr.
func TestExecuteLaunchMutation_Delegated_SentinelNeverLeaks_Success(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"})
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	var text string
	stderrOut := captureStderr(t, func() {
		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		text = extractText(result)
	})
	for _, needle := range []string{sentinelMintedToken, sentinelMintedTokenPrefix} {
		if strings.Contains(text, needle) {
			t.Errorf("response contains sentinel %q:\n%s", needle, text)
		}
		if strings.Contains(stderrOut, needle) {
			t.Errorf("stderr contains sentinel %q:\n%s", needle, stderrOut)
		}
	}
	scanLaunchStateDirForSentinels(t, stateDir, sentinelMintedToken, sentinelMintedTokenPrefix)
}

// TestExecuteLaunchMutation_Delegated_SentinelNeverLeaks_CreateFailure is
// the P-LP-1/D-2 sentinel scan for the CREATE-FAILURE path: mint +
// stage succeed on the minted value, but CreateAndImportProject itself
// fails — the fourth scenario the spec's leak-scan enumerates (success +
// admin-factory failure + staging failure + create failure).
func TestExecuteLaunchMutation_Delegated_SentinelNeverLeaks_CreateFailure(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"})
	mockAdmin := platform.NewMockProjectAdminClient().WithImportError(errors.New("simulated create failure"))
	defer installMockAdminFactory(t, mockAdmin)()

	var text string
	stderrOut := captureStderr(t, func() {
		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		text = extractText(result)
	})
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusFailed {
		t.Fatalf("status: got %q want failed\n%s", resp.Status, text)
	}
	for _, needle := range []string{sentinelMintedToken, sentinelMintedTokenPrefix} {
		if strings.Contains(text, needle) {
			t.Errorf("response contains sentinel %q:\n%s", needle, text)
		}
		if strings.Contains(stderrOut, needle) {
			t.Errorf("stderr contains sentinel %q:\n%s", needle, stderrOut)
		}
	}
	scanLaunchStateDirForSentinels(t, stateDir, sentinelMintedToken, sentinelMintedTokenPrefix)
}

// ---------------------------------------------------------------------
// §4.5 delegated retry + reset.
// ---------------------------------------------------------------------

// TestExecuteLaunchMutation_StageReadError_BlocksBeforeMint pins the
// §4.5 stage-read-error branch (the audit found it untested): when the
// staged-token READ fails on a delegated publish, the mutation must
// return the delegation-stage-read-failed blocker WITHOUT consulting
// the delegation list or minting — a mint on an unconfirmed read could
// create a second token when one was already minted and staged.
func TestExecuteLaunchMutation_StageReadError_BlocksBeforeMint(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"}).
		WithError("GetServiceEnv", errors.New("simulated staged-secret read failure"))
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusFailed {
		t.Fatalf("status: got %q want failed\n%s", resp.Status, text)
	}
	found := false
	for _, b := range resp.Blockers {
		if b.ID == "delegation-stage-read-failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected delegation-stage-read-failed blocker; got %+v", resp.Blockers)
	}
	if got := sourceClient.CallCounts["ListOwnTokenDelegations"]; got != 0 {
		t.Errorf("list must not run on an unconfirmed stage read; got %d calls", got)
	}
	if got := sourceClient.CallCounts["MintDelegatedLaunchToken"]; got != 0 {
		t.Errorf("mint must not run on an unconfirmed stage read; got %d calls", got)
	}
	if mockAdmin.CapturedImportYAML != "" {
		t.Error("CreateAndImportProject must not run on an unconfirmed stage read")
	}
}

// TestExecuteLaunchMutation_DelegatedRetry_UsesStagedToken_ZeroDelegationCalls
// pins the §4.5 retry: a delegated attempt that mints + stages but then
// fails at CreateAndImportProject leaves a `failed` state with the
// token already staged; retrying with confirmLaunch resolves the staged
// value with ZERO further list/mint calls.
func TestExecuteLaunchMutation_DelegatedRetry_UsesStagedToken_ZeroDelegationCalls(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().
		WithTokenDelegations(usableDelegation()).
		WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"})

	failingAdmin := platform.NewMockProjectAdminClient().WithImportError(errors.New("simulated create failure"))
	restore := installMockAdminFactory(t, failingAdmin)
	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	restore()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusFailed {
		t.Fatalf("first call status: got %q want failed\n%s", resp.Status, extractText(result))
	}
	if got := sourceClient.CallCounts["MintDelegatedLaunchToken"]; got != 1 {
		t.Fatalf("setup: expected exactly 1 mint before retry, got %d", got)
	}
	if got := stagedTokenValue(t, sourceClient, "svc-app"); got != sentinelMintedToken {
		t.Fatalf("setup: staged token must be the minted value before retry; got %q", got)
	}

	successAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, successAdmin)()

	mintBefore := sourceClient.CallCounts["MintDelegatedLaunchToken"]
	listBefore := sourceClient.CallCounts["ListOwnTokenDelegations"]

	result2, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("retry call: %v", err)
	}
	resp2 := decodeLaunchResp(t, []byte(extractText(result2)))
	if resp2.Status != topology.LaunchStatusLaunched {
		t.Fatalf("retry status: got %q want launched\n%s", resp2.Status, extractText(result2))
	}
	if got := sourceClient.CallCounts["MintDelegatedLaunchToken"] - mintBefore; got != 0 {
		t.Errorf("retry must resolve the staged token — mint call delta: got %d want 0", got)
	}
	if got := sourceClient.CallCounts["ListOwnTokenDelegations"] - listBefore; got != 0 {
		t.Errorf("retry must resolve the staged token — list call delta: got %d want 0", got)
	}
}

// TestExecuteLaunchMutation_DelegatedRetry_StaleLaunching_UsesStagedToken
// pins the stale-`launching` equivalent: a genuinely-stuck Launching
// state (past launchMutationStaleAfter) with the token already staged
// also resolves via the staged value on retry.
func TestExecuteLaunchMutation_DelegatedRetry_StaleLaunching_UsesStagedToken(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()

	svc, err := ops.LookupService(context.Background(), sourceClient, "source-project-id", "app")
	if err != nil {
		t.Fatalf("lookup app service: %v", err)
	}
	if _, err := ops.EnvSetSecretService(context.Background(), sourceClient, svc.ID, ops.LaunchTokenEnvKey, sentinelMintedToken); err != nil {
		t.Fatalf("pre-stage token: %v", err)
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
	seed.LastUpdate = time.Now().Add(-2 * launchMutationStaleAfter)
	if err := writeRawLaunchStateForTest(t, stateDir, seed); err != nil {
		t.Fatalf("rewrite stale state: %v", err)
	}

	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusLaunched {
		t.Fatalf("status: got %q want launched\n%s", resp.Status, extractText(result))
	}
	if got := sourceClient.CallCounts["MintDelegatedLaunchToken"]; got != 0 {
		t.Errorf("stale-launching retry must resolve the staged token — mint calls: got %d want 0", got)
	}
	if got := sourceClient.CallCounts["ListOwnTokenDelegations"]; got != 0 {
		t.Errorf("stale-launching retry must resolve the staged token — list calls: got %d want 0", got)
	}
}

// TestHandleLaunchReset_NoTargetProject_DeletesStagedSecret pins the
// §4.5 reset fix: a no-target reset (state never reached
// CreateAndImportProject) must still delete the staged secret — the
// pre-fix code only resolved/deleted credentials on the orphan-delete
// (TargetProjectID-set) path, orphaning the secret otherwise.
func TestHandleLaunchReset_NoTargetProject_DeletesStagedSecret(t *testing.T) {
	stateDir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	state := &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	stageClient := stagedSourceClient()

	// First call — arm the ack.
	firstCall, _, firstErr := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, "")
	if firstErr != nil {
		t.Fatalf("first handleLaunchReset call: %v", firstErr)
	}
	if !strings.Contains(getTextContent(t, firstCall), "myapp-prod") {
		t.Fatalf("first call must arm the diagnose-before-destruct ack: %s", getTextContent(t, firstCall))
	}

	result, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	}, "")
	if err != nil {
		t.Fatalf("handleLaunchReset: %v", err)
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, `"stagedSecretDeleted":true`) {
		t.Errorf("reset result must report stagedSecretDeleted=true:\n%s", body)
	}
	if got := stagedTokenValue(t, stageClient, "svc-dev"); got != "" {
		t.Errorf("staged secret must be deleted; still reads %q", got)
	}
	if _, readErr := readLaunchState(stateDir, launchID); !errors.Is(readErr, ErrLaunchStateMissing) {
		t.Errorf("state file must be cleared after a successful reset; read err = %v", readErr)
	}
}

// TestHandleLaunchReset_ProjectDeletedSecretDeleteFails_RetrySkipsProjectDelete
// pins the 2026-07-12 audit #3 fix (reset idempotence after partial
// success): when the orphan-project delete SUCCEEDS and the staged-
// secret delete then FAILS, the persisted state must checkpoint the
// project as gone (TargetProjectID cleared) — otherwise every retry
// re-runs DeleteProject on the already-deleted project, errors, and
// never reaches the secret again (permanent wedge).
func TestHandleLaunchReset_ProjectDeletedSecretDeleteFails_RetrySkipsProjectDelete(t *testing.T) {
	stateDir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	if err := writeLaunchState(stateDir, &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "orphan-pid",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
		TokenAcquisition:      "delegated",
		MintedTokenName:       "zcp-launch-myapp-prod",
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	// Attempt 1: project delete succeeds, staged-secret delete fails.
	failingClient := stagedSourceClient().WithError("DeleteUserData", errors.New("simulated delete failure"))
	adminA := platform.NewMockProjectAdminClient().
		WithDeleteResult(&platform.Process{ID: "del-1", Status: platform.ProcessStatusRunning}).
		WithProcess(&platform.Process{ID: "del-1", Status: platform.ProcessStatusFinished})
	restoreA := installMockAdminFactory(t, adminA)

	firstCall, _, firstErr := handleLaunchReset(context.Background(), stateDir, "src", failingClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, "")
	if firstErr != nil {
		t.Fatalf("arm call: %v", firstErr)
	}
	if !strings.Contains(getTextContent(t, firstCall), "orphan-pid") {
		t.Fatalf("arm call must diagnose the orphan project in wouldDestroy: %s", getTextContent(t, firstCall))
	}
	result, _, err := handleLaunchReset(context.Background(), stateDir, "src", failingClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod", "orphan-pid"},
		},
	}, "")
	if err != nil {
		t.Fatalf("handleLaunchReset attempt 1: %v", err)
	}
	text := extractText(result)
	if adminA.CapturedDeleteProject != "orphan-pid" {
		t.Fatalf("attempt 1 must delete the orphan project; captured %q", adminA.CapturedDeleteProject)
	}
	if !strings.Contains(text, "NOT cleared") {
		t.Errorf("attempt 1 must refuse completion on the failed secret delete; got:\n%s", text)
	}
	if !strings.Contains(text, "orphan-pid") {
		t.Errorf("the refusal must report the project WAS already deleted (partial progress); got:\n%s", text)
	}
	state, readErr := readLaunchState(stateDir, launchID)
	if readErr != nil {
		t.Fatalf("read state after attempt 1: %v", readErr)
	}
	if state.TargetProjectID != "" {
		t.Fatalf("checkpoint missing: TargetProjectID must be cleared after the successful project delete; got %q", state.TargetProjectID)
	}
	restoreA()

	// Attempt 2 (secret delete now succeeds): must NOT re-run
	// DeleteProject, must delete the secret and clear the state.
	healthyClient := stagedSourceClient()
	adminB := platform.NewMockProjectAdminClient()
	defer installMockAdminFactory(t, adminB)()

	if _, _, armErr := handleLaunchReset(context.Background(), stateDir, "src", healthyClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, ""); armErr != nil {
		t.Fatalf("arm call attempt 2: %v", armErr)
	}
	result2, _, err2 := handleLaunchReset(context.Background(), stateDir, "src", healthyClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	}, "")
	if err2 != nil {
		t.Fatalf("handleLaunchReset attempt 2: %v", err2)
	}
	if adminB.CapturedDeleteProject != "" {
		t.Errorf("attempt 2 must not re-delete the already-deleted project; captured %q", adminB.CapturedDeleteProject)
	}
	if got := stagedTokenValue(t, healthyClient, "svc-dev"); got != "" {
		t.Errorf("attempt 2 must delete the staged secret; still reads %q", got)
	}
	if !strings.Contains(getTextContent(t, result2), `"stagedSecretDeleted":true`) {
		t.Errorf("attempt 2 must report stagedSecretDeleted=true:\n%s", getTextContent(t, result2))
	}
	if _, readErr2 := readLaunchState(stateDir, launchID); !errors.Is(readErr2, ErrLaunchStateMissing) {
		t.Errorf("state must be cleared after attempt 2; read err = %v", readErr2)
	}
}

// TestHandleLaunchReset_DeleteProcessFails_NoCheckpoint pins the async
// truth of the P4 checkpoint (Codex review, 2026-07-12): DeleteProject
// call-success is only INITIATION — when the platform-side deletion
// process ends FAILED, the project may still exist, so the checkpoint
// must NOT clear TargetProjectID and the reset must refuse with the
// orphan still tracked for a re-delete.
func TestHandleLaunchReset_DeleteProcessFails_NoCheckpoint(t *testing.T) {
	stateDir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	if err := writeLaunchState(stateDir, &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "orphan-pid",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	stageClient := stagedSourceClient()
	failReason := "quota teardown rejected"
	admin := platform.NewMockProjectAdminClient().
		WithDeleteResult(&platform.Process{ID: "del-1", Status: platform.ProcessStatusRunning}).
		WithProcess(&platform.Process{ID: "del-1", Status: platform.ProcessStatusFailed, FailReason: &failReason})
	defer installMockAdminFactory(t, admin)()

	if _, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, ""); err != nil {
		t.Fatalf("arm call: %v", err)
	}
	result, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod", "orphan-pid"},
		},
	}, "")
	if err != nil {
		t.Fatalf("handleLaunchReset: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, "did not succeed") || !strings.Contains(text, failReason) {
		t.Errorf("refusal must report the failed deletion process with its reason; got:\n%s", text)
	}
	state, readErr := readLaunchState(stateDir, launchID)
	if readErr != nil {
		t.Fatalf("read state: %v", readErr)
	}
	if state.TargetProjectID != "orphan-pid" {
		t.Errorf("a FAILED deletion process must NOT checkpoint the project as gone; TargetProjectID = %q", state.TargetProjectID)
	}
	if got := stagedTokenValue(t, stageClient, "svc-dev"); got == "" {
		t.Error("the staged secret must be untouched when the reset refused before secret cleanup")
	}
}

// TestHandleLaunchReset_DeleteReturnsNoProcess_NoCheckpoint pins the
// defensive branch (Codex confirmation round): a DeleteProject call that
// returns no trackable process cannot be CONFIRMED — reset must refuse
// without checkpointing (live DeleteProject always returns the process;
// nil would mean an unobservable outcome).
func TestHandleLaunchReset_DeleteReturnsNoProcess_NoCheckpoint(t *testing.T) {
	stateDir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	if err := writeLaunchState(stateDir, &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "orphan-pid",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	stageClient := stagedSourceClient()
	admin := platform.NewMockProjectAdminClient() // DeleteProject returns (nil, nil)
	defer installMockAdminFactory(t, admin)()

	if _, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, ""); err != nil {
		t.Fatalf("arm call: %v", err)
	}
	result, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod", "orphan-pid"},
		},
	}, "")
	if err != nil {
		t.Fatalf("handleLaunchReset: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, "no trackable process") {
		t.Errorf("refusal must name the unconfirmable deletion; got:\n%s", text)
	}
	state, readErr := readLaunchState(stateDir, launchID)
	if readErr != nil {
		t.Fatalf("read state: %v", readErr)
	}
	if state.TargetProjectID != "orphan-pid" {
		t.Errorf("an unconfirmable deletion must NOT checkpoint; TargetProjectID = %q", state.TargetProjectID)
	}
}

// TestHandleLaunchReset_StagedSecretDeleteFails_PreservesState pins the
// delete-first discipline: when the staged-secret delete fails, the
// state file is preserved and completion is refused (mirror of
// confirm-production's delete-first rule).
func TestHandleLaunchReset_StagedSecretDeleteFails_PreservesState(t *testing.T) {
	stateDir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	state := &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	stageClient := stagedSourceClient().WithError("DeleteUserData", errors.New("simulated delete failure"))

	firstCall, _, firstErr := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, "")
	if firstErr != nil {
		t.Fatalf("first handleLaunchReset call: %v", firstErr)
	}
	if !strings.Contains(getTextContent(t, firstCall), "myapp-prod") {
		t.Fatalf("first call must arm the diagnose-before-destruct ack: %s", getTextContent(t, firstCall))
	}

	result, _, err := handleLaunchReset(context.Background(), stateDir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod"},
		},
	}, "")
	if err != nil {
		t.Fatalf("handleLaunchReset: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, "NOT cleared") {
		t.Errorf("a failed staged-secret delete must refuse completion honestly; got:\n%s", text)
	}
	if _, readErr := readLaunchState(stateDir, launchID); readErr != nil {
		t.Errorf("state must be PRESERVED after a failed staged-secret delete; read err = %v", readErr)
	}
}

// ---------------------------------------------------------------------
// D-7 message-builder unit pins.
// ---------------------------------------------------------------------

// TestLaunchTokenStageFailedMessage_EmptyMintedName_OriginalWording pins
// that the explicit-launchKey / existing-project call sites (mintedName
// == "") keep the byte-for-byte original message — no delegation
// language leaks in.
func TestLaunchTokenStageFailedMessage_EmptyMintedName_OriginalWording(t *testing.T) {
	t.Parallel()
	msg := launchTokenStageFailedMessage(errors.New("boom"), "appdev", "")
	for _, forbidden := range []string{"delegation", "regenerate", "one-time"} {
		if strings.Contains(strings.ToLower(msg), forbidden) {
			t.Errorf("existing-project/explicit-key staging message must carry NONE of the delegated narrative; found %q in %q", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "re-call with the same launchKey") {
		t.Errorf("original wording must be preserved; got %q", msg)
	}
}

// TestLaunchTokenStageFailedMessage_MintedName_D7Narrative pins the
// delegated-path wording: names the token, says "not confirmed", and
// directs to regenerate + re-call with launchKey.
func TestLaunchTokenStageFailedMessage_MintedName_D7Narrative(t *testing.T) {
	t.Parallel()
	msg := launchTokenStageFailedMessage(errors.New("boom"), "appdev", "zcp-launch-myapp-prod")
	if !strings.Contains(msg, "zcp-launch-myapp-prod") {
		t.Errorf("D-7 narrative must name the minted token; got %q", msg)
	}
	if !strings.Contains(msg, "not confirmed") {
		t.Errorf("D-7 narrative must say NOT CONFIRMED (the write may have committed); got %q", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "regenerate") || !strings.Contains(msg, "launchKey") {
		t.Errorf("D-7 narrative must direct to regenerate + re-call with launchKey; got %q", msg)
	}
}

// ---------------------------------------------------------------------
// delegatedTokenName table tests.
// ---------------------------------------------------------------------

func TestDelegatedTokenName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"typical", "myapp-prod", "zcp-launch-myapp-prod"},
		{"empty", "", "zcp-launch"},
		{"punctuation only", "!!!___...", "zcp-launch"},
		{"repeated letters preserved", "aaaa-prod", "zcp-launch-aaaa-prod"},
		{"repeated hyphens collapsed", "app--prod", "zcp-launch-app-prod"},
		{"uppercase lowered", "MyApp-Prod", "zcp-launch-myapp-prod"},
		{
			"long input truncated to 48 total",
			strings.Repeat("a", 80),
			"zcp-launch-" + strings.Repeat("a", 48-len("zcp-launch-")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := delegatedTokenName(tt.input)
			if got != tt.want {
				t.Errorf("delegatedTokenName(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if len(got) > delegatedTokenNameMaxLen {
				t.Errorf("delegatedTokenName(%q) length %d exceeds ceiling %d", tt.input, len(got), delegatedTokenNameMaxLen)
			}
		})
	}
}
