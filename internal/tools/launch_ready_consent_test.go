package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// F6 — informed consent + wait-for-user credential discipline.

// TestLaunchReadyToLaunchResponse_CarriesRubricAndPreview pins LP-4: the
// ready-to-launch response (the moment BEFORE the irreversible launchKey
// ask) carries the readiness rubric + the compact bundle preview — no
// more empty consent.
func TestLaunchReadyToLaunchResponse_CarriesRubricAndPreview(t *testing.T) {
	t.Parallel()
	bundle := &ops.LaunchBundle{
		ImportYAML:     "project:\n  corePackage: SERIOUS\n  location: eu-central\n",
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "abc"},
		Warnings:       []string{"prod policy: app minContainers 1→2 (HA floor)"},
	}
	inputs := ops.LaunchBundleInputs{
		TargetProjectName: "myapp-prod",
		Location:          "us-west-1",
		Runtimes: []ops.LaunchRuntimeInput{{
			ProdHostname: "app", ServiceType: "nodejs@22", SetupName: "prod",
			RepoURL: "https://github.com/me/app", ZeropsYAMLBody: "zerops:\n  - setup: prod\n",
		}},
		ManagedServices: []ops.ManagedServiceEntry{{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"}},
	}

	checks := runReadinessRubric(bundle, inputs)
	preview := launchBundlePreviewFrom(bundle, inputs)

	result := launchReadyToLaunchResponse(nil, WorkflowInput{}, nil, nil, checks, preview)
	body := getTextContent(t, result)

	for _, want := range []string{
		`"readinessChecks"`,
		`"prod-core-package"`,
		`"bundlePreview"`,
		`"targetProjectName":"myapp-prod"`,
		`"corePackage":"SERIOUS"`,
		`"location":"us-west-1"`,
		`"hostname":"db"`,
		`"mode":"HA"`,
		`minContainers 1→2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("ready-to-launch consent missing %q:\n%s", want, body)
		}
	}
	// Size discipline: the preview must NOT inline the full import yaml.
	if strings.Contains(body, "corePackage: SERIOUS\\n") {
		t.Error("ready-to-launch must not inline the raw import yaml body")
	}
}

// TestLaunchSourceControlRequired_CredentialsAskBlock pins LP-2: when a
// blocker chains into git-push-setup, the response carries the typed
// credentialsRequired block with the wait-for-user contract — the
// proactive sibling of the error-side credential contract.
func TestLaunchSourceControlRequired_CredentialsAskBlock(t *testing.T) {
	t.Parallel()
	blockers := []topology.Blocker{{
		ID:       "git-push-unconfigured-appdev",
		Severity: topology.BlockerSeverityBlock,
		Category: topology.BlockerCategorySourceControl,
		Message:  "no user-owned git remote is wired",
		Recovery: &topology.Recovery{
			Tool:   "zerops_workflow",
			Action: "git-push-setup",
			Args:   map[string]string{"service": "appdev"},
		},
	}}
	result := launchSourceControlRequiredResponse(nil, WorkflowInput{}, nil, blockers)
	body := getTextContent(t, result)

	for _, want := range []string{
		`"credentialsRequired"`,
		`"name":"gitToken"`,
		`"secret":true`,
		`"fromUser":true`,
		"WAIT for their answer",
		"NEVER invent",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("source-control-required missing credential-ask %q:\n%s", want, body)
		}
	}
}

// TestLaunchSourceControlRequired_NoCredentialBlockWithoutGitPushChain
// pins that the ask block appears ONLY when a git-push-setup chain is
// present (a build-integration-recommended warn alone must not demand
// credentials).
func TestLaunchSourceControlRequired_NoCredentialBlockWithoutGitPushChain(t *testing.T) {
	t.Parallel()
	blockers := []topology.Blocker{{
		ID:       "build-integration-recommended-appdev",
		Severity: topology.BlockerSeverityWarn,
		Category: topology.BlockerCategorySourceControl,
		Message:  "declared but not verified",
		Recovery: &topology.Recovery{Tool: "zerops_workflow", Action: "build-integration"},
	}}
	result := launchSourceControlRequiredResponse(nil, WorkflowInput{}, nil, blockers)
	if strings.Contains(getTextContent(t, result), `"credentialsRequired"`) {
		t.Error("credential ask must not appear without a git-push-setup chain")
	}
}

// keep the workflow import used when goldens change shape later
var _ = workflow.PhaseLaunchProductionActive
