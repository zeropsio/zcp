// Tests for: tools/workflow_repo_status.go — the SSH-side wiring behind
// the envelope's per-service repo block (docs/spec-workflows.md §8 GLC-7,
// G1/G2). workflow.ApplyRepoStatus itself is unit-tested in the
// workflow package; this covers only attachRepoStatus's gating (which
// services get an SSH read at all) and the SSH-failure fallback.
package tools

import (
	"context"
	"testing"
	"time"

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

// stubSSHSlow sleeps for Delay on every ExecSSH call before returning
// Output — used to prove attachRepoStatus reads services concurrently
// rather than one SSH round trip at a time.
type stubSSHSlow struct {
	delay  time.Duration
	output []byte
}

func (s *stubSSHSlow) ExecSSH(ctx context.Context, _ string, _ string) ([]byte, error) {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.output, nil
}

func (s *stubSSHSlow) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return s.output, nil
}

// TestAttachRepoStatus_ManyServices_ReadsRunConcurrently proves the
// per-service SSH reads overlap instead of running one at a time: 5
// services each behind a ~150ms SSH read finish in well under 5x150ms if
// (and only if) they run concurrently. Concurrency is bounded at
// attachRepoStatusConcurrency (4), so 5 services still take two waves
// (~2x150ms wall-clock) rather than one — the threshold below sits
// comfortably above that 2-wave floor and well below the 5x150ms serial
// floor, so scheduler jitter around the 2-wave boundary can't flake it.
func TestAttachRepoStatus_ManyServices_ReadsRunConcurrently(t *testing.T) {
	t.Parallel()
	const delay = 150 * time.Millisecond
	const wantUnder = 4 * delay // serial would take 5*delay=750ms; 2 waves land near 2*delay=300ms
	ssh := &stubSSHSlow{delay: delay, output: []byte("5ba0abc\n")}
	services := []workflow.ServiceSnapshot{
		{Hostname: "svc1", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc2", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc3", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc4", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc5", RuntimeClass: topology.RuntimeDynamic},
	}

	start := time.Now()
	attachRepoStatus(context.Background(), services, ssh, runtime.Info{InContainer: true}, t.TempDir())
	elapsed := time.Since(start)

	if elapsed >= wantUnder {
		t.Errorf("elapsed = %v, want < %v (serial reads would take >= %v)", elapsed, wantUnder, 5*delay)
	}
	for _, svc := range services {
		if svc.Repo == nil || !svc.Repo.Present {
			t.Errorf("service %s: Repo = %+v, want Present:true", svc.Hostname, svc.Repo)
		}
	}
}
