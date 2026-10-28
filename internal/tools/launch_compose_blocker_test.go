// Tests for: launch-production — what ready-to-launch says of a bundle the
// composer refuses, before the person is asked for a launch key.
package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// launchSourceReading is the pLP3 source project with one more service
// beside the app, the app's prod setup reading it through envVariables.
func launchSourceReading(extra platform.ServiceStack, envVariables string) (*platform.Mock, *stubSSHDeployer) {
	app := platform.ServiceStack{
		ID: "svc-app", Name: "app", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeID: "nodejs", ServiceStackTypeVersionName: "nodejs@22"},
	}
	ssh := pLP3SSHFrozen()
	ssh.responses["/var/www/zerops.yaml"] = []byte("zerops:\n  - setup: prod\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n      start: node dist/server.js\n      envVariables:\n" + envVariables)
	return pLP3MockClient().WithServices([]platform.ServiceStack{app, extra}), ssh
}

// mailpitReadingSource is the source whose app mails through the Mate's mail
// catcher — a service the production bundle does not carry (spec-workflows.md
// §10.3 P-LP-19).
func mailpitReadingSource() (*platform.Mock, *stubSSHDeployer) {
	return launchSourceReading(platform.ServiceStack{
		ID: "svc-mailpit", Name: "mailpit", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "alpine@3.21", ServiceStackTypeCategoryName: "USER"},
	}, "        MAIL_URL: smtp://mailpit:1025\n")
}

// boxReadingSource is the source whose app reads the project's Zerops Box,
// which the bundle carries.
func boxReadingSource() (*platform.Mock, *stubSSHDeployer) {
	return launchSourceReading(platform.ServiceStack{
		ID: "svc-box", Name: "box", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1", ServiceStackTypeCategoryName: "STANDARD"},
	}, "        BOX_API_URL: http://box:8080\n")
}

// Asking a person to paste a production credential and then refusing the
// launch is the wrong order. A bundle the composer refuses whatever project
// it lands in — here a carried Box into a new project outside prg1 — blocks
// the ready-to-launch call itself: a block-severity blocker carrying the
// composer's own message, no key or delegation offered, and no
// ready-to-launch baseline written. Spec: spec-workflows.md §10.3 P-LP-19.
func TestReadyToLaunch_ComposeRefusal_BlocksBeforeTheKeyIsAsked(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	client, ssh := boxReadingSource()
	input := pLP3CompleteInput() // no launchKey, no confirmLaunch: the manual-key path's ask
	input.Region = "us-east-1"

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
	for _, want := range []string{`Zerops Box "box"`, "prg1", `"us-east-1"`} {
		if !strings.Contains(blocker.Message, want) {
			t.Errorf("blocker message does not say %s: %q", want, blocker.Message)
		}
	}
	assertNoKeyAsked(t, client, text, resp)
	if _, err := readLaunchState(stateDir, generateLaunchID("source-project-id", input.ProductionProjectName)); !errors.Is(err, ErrLaunchStateMissing) {
		t.Errorf("a blocked launch must write no ready-to-launch baseline; readLaunchState err = %v", err)
	}
}

// Before a key is asked the production project is not chosen: an existing
// one comes only with existingProjectId + existingProdToken, whose publish
// counts the services the project already runs. So a launched runtime
// reading a service the bundle does not carry is no dead end at
// ready-to-launch: a new project is certain to be refused, so no key or
// delegation is offered for one, but an existing project that runs the
// service under that name is not — a warning names the runtime, the variable
// and the service production must run, the guidance points at the existing-
// project path, and its publish decides with the target known. Spec:
// spec-workflows.md §10.3 P-LP-19.
func TestReadyToLaunch_ClosureGap_WarnsAndLeavesTheExistingProjectPath(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	client, ssh := mailpitReadingSource()
	input := pLP3CompleteInput()

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, nil,
		input, stateDir, pLP3ContainerRuntime(), ssh, "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Errorf("status = %q, want ready-to-launch — an existing project may run the service:\n%s", resp.Status, text)
	}
	var warning *topology.Blocker
	for i := range resp.Blockers {
		if resp.Blockers[i].Severity == topology.BlockerSeverityBlock {
			t.Errorf("blocker %q blocks; a gap an existing project can close is a warning here:\n%s", resp.Blockers[i].ID, text)
		}
		if strings.Contains(resp.Blockers[i].Message, `"mailpit"`) {
			warning = &resp.Blockers[i]
		}
	}
	if warning == nil {
		t.Fatalf("no warning names the service:\n%s", text)
	}
	if warning.Severity != topology.BlockerSeverityWarn {
		t.Errorf("warning severity = %q, want warn", warning.Severity)
	}
	for _, want := range []string{`runtime "app"`, "run.envVariables.MAIL_URL", `production must run "mailpit" under that name`} {
		if !strings.Contains(warning.Message, want) {
			t.Errorf("warning does not say %s: %q", want, warning.Message)
		}
	}
	for _, want := range []string{"existingProjectId", "existingProdToken"} {
		if !strings.Contains(resp.Guidance, want) {
			t.Errorf("guidance does not point at the existing-project path (%s):\n%s", want, resp.Guidance)
		}
	}
	assertNoKeyAsked(t, client, text, resp)
}

// assertNoKeyAsked fails when a launch-production response asks the person
// for a new project's launch key or offers a delegated launch.
func assertNoKeyAsked(t *testing.T, client *platform.Mock, text string, resp launchProductionResponse) {
	t.Helper()
	if strings.Contains(text, "launchKey") || resp.DelegatedLaunch != nil {
		t.Errorf("the response asks for a new project's key or offers a delegation:\n%s", text)
	}
	if got := client.CallCounts["ListOwnTokenDelegations"]; got != 0 {
		t.Errorf("ListOwnTokenDelegations called %d times; no new-project token path may be offered", got)
	}
}

// The delegated path is a new-project launch, which the bundle's gap refuses
// for certain: it is refused before a token is minted, so the one-time
// delegation is never spent on it (P-LP-15 D-3).
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
