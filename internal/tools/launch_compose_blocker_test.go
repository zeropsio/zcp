// Tests for: launch-production — a bundle that does not compose blocks at
// ready-to-launch, before the person is asked for a launch key.
package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// mailpitReadingSource is the pLP3 source project with the Mate's mail
// catcher beside the app, the app's prod setup mailing through it — a
// service the production bundle does not carry (spec-workflows.md §10.3
// P-LP-19).
func mailpitReadingSource() (*platform.Mock, *stubSSHDeployer) {
	app := platform.ServiceStack{
		ID: "svc-app", Name: "app", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeID: "nodejs", ServiceStackTypeVersionName: "nodejs@22"},
	}
	mailpit := platform.ServiceStack{
		ID: "svc-mailpit", Name: "mailpit", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "alpine@3.21", ServiceStackTypeCategoryName: "USER"},
	}
	ssh := pLP3SSHFrozen()
	ssh.responses["/var/www/zerops.yaml"] = []byte("zerops:\n  - setup: prod\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n      start: node dist/server.js\n      envVariables:\n        MAIL_URL: smtp://mailpit:1025\n")
	return pLP3MockClient().WithServices([]platform.ServiceStack{app, mailpit}), ssh
}

// Asking a person to paste a production credential and then refusing the
// launch is the wrong order. A bundle that does not compose — here a
// launched runtime reading a service the bundle does not carry — blocks
// the ready-to-launch call itself: a block-severity blocker carrying the
// composer's own message (the runtime, the variable, the service), no
// key or delegation offered, and no ready-to-launch baseline written.
// Spec: spec-workflows.md §10.3 P-LP-19.
func TestReadyToLaunch_ComposeRefusal_BlocksBeforeTheKeyIsAsked(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	client, ssh := mailpitReadingSource()
	input := pLP3CompleteInput() // no launchKey, no confirmLaunch: the manual-key path's ask

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, nil,
		input, stateDir, pLP3ContainerRuntime(), ssh, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status == topology.LaunchStatusReadyToLaunch {
		t.Fatalf("status ready-to-launch asks for the key; want the compose refusal to block first:\n%s", text)
	}
	var blocker *topology.Blocker
	for i := range resp.Blockers {
		if resp.Blockers[i].ID == "bundle-compose-failed" {
			blocker = &resp.Blockers[i]
		}
	}
	if blocker == nil {
		t.Fatalf("no bundle-compose-failed blocker:\n%s", text)
	}
	if blocker.Severity != topology.BlockerSeverityBlock {
		t.Errorf("blocker severity = %q, want block", blocker.Severity)
	}
	for _, want := range []string{`runtime "app"`, "run.envVariables.MAIL_URL", `"mailpit"`} {
		if !strings.Contains(blocker.Message, want) {
			t.Errorf("blocker message does not name %s: %q", want, blocker.Message)
		}
	}
	if strings.Contains(text, "launchKey") || resp.DelegatedLaunch != nil {
		t.Errorf("a blocked launch must not ask for a key or offer a delegation:\n%s", text)
	}
	if got := client.CallCounts["ListOwnTokenDelegations"]; got != 0 {
		t.Errorf("ListOwnTokenDelegations called %d times; a blocked launch offers no token path", got)
	}
	if _, err := readLaunchState(stateDir, generateLaunchID("source-project-id", input.ProductionProjectName)); !errors.Is(err, ErrLaunchStateMissing) {
		t.Errorf("a blocked launch must write no ready-to-launch baseline; readLaunchState err = %v", err)
	}
}

// The delegated path refuses the same bundle before a token is minted: the
// one-time delegation is never spent on a launch that cannot compose
// (P-LP-15 D-3).
func TestExecuteLaunchMutation_Delegated_ComposeRefusal_SkipsMint(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	client, ssh := mailpitReadingSource()
	client.WithTokenDelegations(usableDelegation()).
		WithMintedToken(platform.MintedToken{Token: sentinelMintedToken, TokenID: "minted-id"})
	mockAdmin := happyMockAdmin()
	defer installMockAdminFactory(t, mockAdmin)()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, nil,
		delegatedPublishInput(), stateDir, pLP3ContainerRuntime(), ssh, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	if got := client.CallCounts["MintDelegatedLaunchToken"]; got != 0 {
		t.Errorf("mint ran %d times; a bundle that does not compose must refuse before it", got)
	}
	if mockAdmin.CapturedImportYAML != "" {
		t.Error("CreateAndImportProject ran for a bundle that does not compose")
	}
	for _, want := range []string{"bundle-compose-failed", "run.envVariables.MAIL_URL", `\"mailpit\"`} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal does not say %s:\n%s", want, text)
		}
	}
}
