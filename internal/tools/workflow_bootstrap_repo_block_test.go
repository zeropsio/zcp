// Tests for: envelope_wire.go / workflow_bootstrap.go — the adopt route's
// provision-complete response must carry services[].repo in the SAME
// response that promises the agent to check it (docs/spec-workflows.md §8
// GLC-7). buildAdoptionTransitionMessage and the
// bootstrap-adopt-baseline-commit atom both tell the agent to read this
// block once complete step="provision" returns; before this slice that
// block only ever landed on action="status", never on the response making
// the promise.
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// bootstrapEnvelopeDoc unmarshals just the envelope out of a
// bootstrapResponse's JSON body — the same shape jsonResult(bootstrapResponse{...})
// produces (internal/tools/envelope_wire.go).
type bootstrapEnvelopeDoc struct {
	Envelope *workflow.StateEnvelope `json:"envelope"`
}

// repoBlockFor returns the Repo block for hostname in doc's envelope, or
// nil if the service isn't present in it.
func repoBlockFor(doc bootstrapEnvelopeDoc, hostname string) *workflow.RepoStatus {
	if doc.Envelope == nil {
		return nil
	}
	for _, svc := range doc.Envelope.Services {
		if svc.Hostname == hostname {
			return svc.Repo
		}
	}
	return nil
}

// TestHandleBootstrapComplete_AdoptProvisionComplete_CarriesRepoBlock proves
// the adopt route's provision-complete response embeds services[].repo
// (Present/Head live, Baseline/Provenance from the ServiceMeta
// adoptRepoBaseline just wrote) — the same block attachRepoStatus renders
// on action="status" (docs/spec-workflows.md §8 GLC-7). The SSH stub scripts
// AdoptBaseline's HEAD^{tree} probe to the empty-tree sha, so the adopted
// service takes the "initialized" case (same technique as
// TestAutoMountTargets_AdoptRoute_EmptyTreeHEAD_Initializes).
func TestHandleBootstrapComplete_AdoptProvisionComplete_CarriesRepoBlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	seedAdoptBootstrapPlan(t, eng, []workflow.BootstrapTarget{
		{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple", IsExisting: true}},
	})

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID:                   "svc-app",
			Name:                 "appdev",
			Status:               serviceStatusRunning,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-1"},
		},
	})
	mounter := &mountRecorder{}
	ssh := &sshRecorder{respond: func(cmd string) ([]byte, error) {
		if strings.Contains(cmd, "HEAD^{tree}") {
			return []byte("4b825dc642cb6eb9a060e54bf8d69288fbee4904\n"), nil
		}
		return nil, nil
	}}
	rt := runtime.Info{InContainer: true}
	input := WorkflowInput{Step: workflow.StepProvision, Attestation: "provisioned"}

	result, typed, err := handleBootstrapComplete(context.Background(), eng, mock, nil, nil, input, nil, "proj-1", dir, mounter, ssh, rt)
	if err != nil {
		t.Fatalf("handleBootstrapComplete: %v", err)
	}
	if typed != nil {
		t.Fatalf("typed output (2nd return) must stay nil — TestNoStructuredContentOnToolResults trap, got %#v", typed)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	var doc bootstrapEnvelopeDoc
	text := getTextContent(t, result)
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, text)
	}
	if doc.Envelope == nil {
		t.Fatal("response carries no envelope at all")
	}

	repo := repoBlockFor(doc, "appdev")
	if repo == nil {
		t.Fatalf("services[appdev].repo is nil, want the block buildAdoptionTransitionMessage and bootstrap-adopt-baseline-commit promise; services: %+v", doc.Envelope.Services)
	}
	if repo.Provenance != topology.RepoProvenanceInitialized {
		t.Errorf("repo.provenance = %q, want %q", repo.Provenance, topology.RepoProvenanceInitialized)
	}
}

// TestHandleBootstrapComplete_ClassicProvisionComplete_CarriesNoRepoBlock
// proves the repo block is adopt-route-specific: a classic-route
// provision-complete response pays no extra SSH round trips and carries no
// services[].repo at all (P4 — mutation responses stay terse outside the
// one route/step that makes an explicit promise about this payload).
func TestHandleBootstrapComplete_ClassicProvisionComplete_CarriesNoRepoBlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	seedBootstrapPlan(t, eng, []workflow.BootstrapTarget{
		{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"}},
	})

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "appdev", Status: serviceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})
	mounter := &mountRecorder{}
	ssh := &sshRecorder{}
	rt := runtime.Info{InContainer: true}
	input := WorkflowInput{Step: workflow.StepProvision, Attestation: "provisioned"}

	result, typed, err := handleBootstrapComplete(context.Background(), eng, mock, nil, nil, input, nil, "proj-1", dir, mounter, ssh, rt)
	if err != nil {
		t.Fatalf("handleBootstrapComplete: %v", err)
	}
	if typed != nil {
		t.Fatalf("typed output (2nd return) must stay nil, got %#v", typed)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	var doc bootstrapEnvelopeDoc
	text := getTextContent(t, result)
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, text)
	}
	if doc.Envelope == nil {
		t.Fatal("response carries no envelope at all")
	}

	if repo := repoBlockFor(doc, "appdev"); repo != nil {
		t.Errorf("classic-route provision-complete must carry no repo block, got %+v", repo)
	}
}
