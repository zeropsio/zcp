// Tests for the JSON-document envelope carrier (docs/spec-mate.md §1.3).
//
// Most mutating tools answer with one JSON document rather than prose, so
// they cannot carry a trailing markdown fence — it would stop the document
// parsing as JSON. Those results carry the envelope as a top-level
// `envelope` key beside the tool's own fields instead.
package tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestJSONCarriers_WireContract pins both halves of the contract for every
// JSON-carrying response type:
//
//   - with an envelope, the document exposes it at the top level and
//     workflow.ExtractEnvelope reads it back;
//   - without one (computation failed), the `envelope` key is absent
//     entirely — the document stays byte-identical to what the tool
//     produced before it carried an envelope at all.
func TestJSONCarriers_WireContract(t *testing.T) {
	t.Parallel()

	env := workflow.StateEnvelope{
		Phase:   workflow.PhaseDevelopActive,
		Project: workflow.ProjectSummary{ID: "proj-1", Name: "z3-eval"},
	}

	carriers := []struct {
		name    string
		with    any
		without any
	}{
		{
			"deployLocalResponse",
			deployLocalResponse{DeployResult: &ops.DeployResult{Status: "ACTIVE"}, Envelope: &env},
			deployLocalResponse{DeployResult: &ops.DeployResult{Status: "ACTIVE"}},
		},
		{
			"deploySSHResponse",
			deploySSHResponse{DeployResult: &ops.DeployResult{Status: "ACTIVE"}, Envelope: &env},
			deploySSHResponse{DeployResult: &ops.DeployResult{Status: "ACTIVE"}},
		},
		{
			"deployGitPushResponse",
			deployGitPushResponse{GitPushResult: &ops.GitPushResult{Status: "PUSHED"}, Envelope: &env},
			deployGitPushResponse{GitPushResult: &ops.GitPushResult{Status: "PUSHED"}},
		},
		{
			"localGitPushResponse",
			localGitPushResponse{GitPushResult: &ops.GitPushResult{Status: "PUSHED"}, Envelope: &env},
			localGitPushResponse{GitPushResult: &ops.GitPushResult{Status: "PUSHED"}},
		},
		{
			"verifyResponse",
			verifyResponse{VerifyResult: &ops.VerifyResult{}, Envelope: &env},
			verifyResponse{VerifyResult: &ops.VerifyResult{}},
		},
		{
			"verifyAllResponse",
			verifyAllResponse{VerifyAllResult: &ops.VerifyAllResult{}, Envelope: &env},
			verifyAllResponse{VerifyAllResult: &ops.VerifyAllResult{}},
		},
		{
			"mountResponse",
			mountResponse{MountResult: &ops.MountResult{}, Envelope: &env},
			mountResponse{MountResult: &ops.MountResult{}},
		},
		{
			"mountStatusResponse",
			mountStatusResponse{MountStatusResult: &ops.MountStatusResult{}, Envelope: &env},
			mountStatusResponse{MountStatusResult: &ops.MountStatusResult{}},
		},
		{
			"importResponse",
			importResponse{ImportResult: &ops.ImportResult{}, Envelope: &env},
			importResponse{ImportResult: &ops.ImportResult{}},
		},
		{
			"bootstrapResponse",
			bootstrapResponse{BootstrapResponse: &workflow.BootstrapResponse{}, Envelope: &env},
			bootstrapResponse{BootstrapResponse: &workflow.BootstrapResponse{}},
		},
		{
			"leaseToolResult",
			leaseToolResult{Action: "status", Leases: []ops.LeaseView{}, Envelope: &env},
			leaseToolResult{Action: "status", Leases: []ops.LeaseView{}},
		},
		{
			"bootstrapDiscoveryResponse",
			bootstrapDiscoveryResponse{BootstrapDiscoveryResponse: &workflow.BootstrapDiscoveryResponse{}, Envelope: &env},
			bootstrapDiscoveryResponse{BootstrapDiscoveryResponse: &workflow.BootstrapDiscoveryResponse{}},
		},
	}

	for _, c := range carriers {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			payload, err := json.Marshal(c.with)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got, ok := workflow.ExtractEnvelope(string(payload))
			if !ok {
				t.Fatalf("ExtractEnvelope found nothing in\n%s", payload)
			}
			if got.Project.ID != "proj-1" || got.Phase != workflow.PhaseDevelopActive {
				t.Errorf("envelope round trip: got %+v", got)
			}

			bare, err := json.Marshal(c.without)
			if err != nil {
				t.Fatalf("marshal bare: %v", err)
			}
			if strings.Contains(string(bare), "envelope") {
				t.Errorf("a nil envelope must be omitted entirely, got\n%s", bare)
			}
			if _, ok := workflow.ExtractEnvelope(string(bare)); ok {
				t.Errorf("bare document must yield no envelope")
			}
		})
	}
}

// TestJSONCarriers_LeaseActivity_ReadBackVerbatim pins the read-back half of
// leases[].activity/unsaved (docs/spec-mate.md §2.8a.3): workflow.ExtractEnvelope
// returns exactly what the zerops_lease result carried — each fact as sent, a
// fact not sent still absent — and derives nothing for a lease that carried
// neither.
func TestJSONCarriers_LeaseActivity_ReadBackVerbatim(t *testing.T) {
	t.Parallel()

	open := 2
	sent := []workflow.LeaseSnapshot{
		{
			Alias: "devpool-3", Pool: "devpool", Branch: "feat-x", ContainerHostname: "node-id-3.runtime.devpool.zerops", State: "held",
			AcquiredAt: "2026-10-23T12:00:00Z",
			Activity: &topology.LeaseActivity{
				LastChangeAt: "2026-10-23T13:00:00Z", PreviewOpenConns: &open, AcquiredAt: "2026-10-23T12:00:00Z", ReadAt: "2026-10-23T14:00:00Z",
			},
			Unsaved: &topology.LeaseUnsaved{Files: []string{" M src/app.ts", "?? notes.txt"}, FileCount: 2},
		},
		{Alias: "devpool-4", Pool: "devpool", ContainerHostname: "node-id-4.runtime.devpool.zerops", State: "unknown-holder"},
	}
	env := workflow.StateEnvelope{Phase: workflow.PhaseIdle, Project: workflow.ProjectSummary{ID: "proj-1"}, Leases: &sent}
	payload, err := json.Marshal(leaseToolResult{Action: "status", Leases: []ops.LeaseView{}, Envelope: &env})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, ok := workflow.ExtractEnvelope(string(payload))
	if !ok {
		t.Fatalf("ExtractEnvelope found nothing in\n%s", payload)
	}
	if !reflect.DeepEqual(got.LeaseList(), sent) {
		t.Errorf("leases read back\n %+v\nwant\n %+v", got.LeaseList(), sent)
	}
	if a := got.LeaseList()[0].Activity; a.PreviewLastConnAt != "" || a.Note != "" {
		t.Errorf("a fact not sent was read back: %+v", a)
	}
}
