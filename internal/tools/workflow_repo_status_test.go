// Tests for: tools/workflow_repo_status.go — the SSH-side wiring behind
// the envelope's per-service repo block (docs/spec-workflows.md §8 GLC-7,
// G1/G2). workflow.ApplyRepoStatus itself is unit-tested in the
// workflow package; this covers only attachRepoStatus's gating (which
// services get an SSH read at all) and the SSH-failure fallback.
package tools

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestAttachRepoStatus_LocalMode_NoOp(t *testing.T) {
	t.Parallel()
	services := []workflow.ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}}
	attachRepoStatus(context.Background(), services, &stubSSH{output: []byte("5ba1\n")}, runtime.Info{InContainer: false}, t.TempDir())
	if services[0].Repo != nil {
		t.Errorf("Repo = %+v, want nil in local mode (attachRepoStatus is container-only)", services[0].Repo)
	}
}

func TestAttachRepoStatus_NilSSH_NoOp(t *testing.T) {
	t.Parallel()
	services := []workflow.ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}}
	attachRepoStatus(context.Background(), services, nil, runtime.Info{InContainer: true}, t.TempDir())
	if services[0].Repo != nil {
		t.Errorf("Repo = %+v, want nil without an SSH deployer", services[0].Repo)
	}
}

func TestAttachRepoStatus_Container_DevService_GetsRepoBlock(t *testing.T) {
	t.Parallel()
	ssh := &stubSSH{output: []byte("5ba0abc\n")}
	services := []workflow.ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}}
	attachRepoStatus(context.Background(), services, ssh, runtime.Info{InContainer: true}, t.TempDir())
	if services[0].Repo == nil || !services[0].Repo.Present {
		t.Errorf("Repo = %+v, want Present:true", services[0].Repo)
	}
}

func TestAttachRepoStatus_Container_ManagedService_StaysNil(t *testing.T) {
	t.Parallel()
	ssh := &stubSSH{output: []byte("5ba0abc\n")}
	services := []workflow.ServiceSnapshot{{Hostname: "db", RuntimeClass: topology.RuntimeManaged}}
	attachRepoStatus(context.Background(), services, ssh, runtime.Info{InContainer: true}, t.TempDir())
	if services[0].Repo != nil {
		t.Errorf("Repo = %+v, want nil for a managed service (no SSH read attempted)", services[0].Repo)
	}
}

func TestAttachRepoStatus_Container_SSHFailure_ReportsNotPresentNoPanic(t *testing.T) {
	t.Parallel()
	ssh := &stubSSH{err: errWorkflowChecksRepo}
	services := []workflow.ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}}
	attachRepoStatus(context.Background(), services, ssh, runtime.Info{InContainer: true}, t.TempDir())
	if services[0].Repo == nil {
		t.Fatal("Repo is nil, want {Present:false} reported for a no-repo-yet service")
	}
	if services[0].Repo.Present {
		t.Error("Repo.Present = true, want false on an SSH failure (no repo yet)")
	}
}

// TestAttachRepoStatus_MetaHasProvenance_AddsProvenanceToRepoBlock proves
// provenance is read from ServiceMeta (a recorded fact), not derived from
// the live SSH read (docs/spec-workflows.md §8 GLC-7): present/head
// stay live while baseline and provenance come from disk.
func TestAttachRepoStatus_MetaHasProvenance_AddsProvenanceToRepoBlock(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := workflow.NewServiceMeta("proj", topology.PlanModeLocalStage)
	meta.Hostname = "appdev"
	meta.SetRepoBaseline("av-1", topology.RepoProvenanceInitialized)
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	ssh := &stubSSH{output: []byte("5ba0abc\n")}
	services := []workflow.ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}}
	attachRepoStatus(context.Background(), services, ssh, runtime.Info{InContainer: true}, stateDir)

	if services[0].Repo == nil {
		t.Fatal("Repo is nil, want it populated")
	}
	if !services[0].Repo.Present {
		t.Error("Present = false, want true (still live)")
	}
	if services[0].Repo.Baseline != "av-1" {
		t.Errorf("Baseline = %q, want metadata av-1", services[0].Repo.Baseline)
	}
	if services[0].Repo.Provenance != topology.RepoProvenanceInitialized {
		t.Errorf("Provenance = %q, want %q", services[0].Repo.Provenance, topology.RepoProvenanceInitialized)
	}
}

// TestAttachRepoStatus_NoMeta_ProvenanceStaysEmpty proves a service with no
// recorded adopt baseline (bootstrapped fresh, never adopted) gets a repo
// block with no provenance rather than a fabricated one.
func TestAttachRepoStatus_NoMeta_ProvenanceStaysEmpty(t *testing.T) {
	t.Parallel()
	ssh := &stubSSH{output: []byte("5ba0abc\n")}
	services := []workflow.ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}}
	attachRepoStatus(context.Background(), services, ssh, runtime.Info{InContainer: true}, t.TempDir())

	if services[0].Repo == nil {
		t.Fatal("Repo is nil, want it populated")
	}
	if services[0].Repo.Provenance != "" {
		t.Errorf("Provenance = %q, want empty (no ServiceMeta.Repo recorded)", services[0].Repo.Provenance)
	}
}

func TestAttachRepoStatus_PairedStage_DoesNotInheritDevAdoption(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := workflow.NewServiceMeta("proj", topology.PlanModeStandard)
	meta.Hostname = "appdev"
	meta.StageHostname = "appstage"
	meta.SetRepoBaseline("av-dev", topology.RepoProvenanceExisting)
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}
	services := []workflow.ServiceSnapshot{{Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic}}
	attachRepoStatus(context.Background(), services, &stubSSH{output: []byte("5ba0abc\n")}, runtime.Info{InContainer: true}, stateDir)
	if services[0].Repo == nil {
		t.Fatal("missing live repo status")
	}
	if services[0].Repo.Baseline != "" || services[0].Repo.Provenance != "" {
		t.Fatalf("stage inherited dev adoption: %+v", services[0].Repo)
	}
}
