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
	attachRepoStatus(context.Background(), services, &stubSSH{output: []byte("sha1\n")}, runtime.Info{InContainer: false})
	if services[0].Repo != nil {
		t.Errorf("Repo = %+v, want nil in local mode (attachRepoStatus is container-only)", services[0].Repo)
	}
}

func TestAttachRepoStatus_NilSSH_NoOp(t *testing.T) {
	t.Parallel()
	services := []workflow.ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}}
	attachRepoStatus(context.Background(), services, nil, runtime.Info{InContainer: true})
	if services[0].Repo != nil {
		t.Errorf("Repo = %+v, want nil without an SSH deployer", services[0].Repo)
	}
}

func TestAttachRepoStatus_Container_DevService_GetsRepoBlock(t *testing.T) {
	t.Parallel()
	ssh := &stubSSH{output: []byte("sha-abc\n")}
	services := []workflow.ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}}
	attachRepoStatus(context.Background(), services, ssh, runtime.Info{InContainer: true})
	if services[0].Repo == nil || !services[0].Repo.Present {
		t.Errorf("Repo = %+v, want Present:true", services[0].Repo)
	}
}

func TestAttachRepoStatus_Container_ManagedService_StaysNil(t *testing.T) {
	t.Parallel()
	ssh := &stubSSH{output: []byte("sha-abc\n")}
	services := []workflow.ServiceSnapshot{{Hostname: "db", RuntimeClass: topology.RuntimeManaged}}
	attachRepoStatus(context.Background(), services, ssh, runtime.Info{InContainer: true})
	if services[0].Repo != nil {
		t.Errorf("Repo = %+v, want nil for a managed service (no SSH read attempted)", services[0].Repo)
	}
}

func TestAttachRepoStatus_Container_SSHFailure_ReportsNotPresentNoPanic(t *testing.T) {
	t.Parallel()
	ssh := &stubSSH{err: errWorkflowChecksRepo}
	services := []workflow.ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}}
	attachRepoStatus(context.Background(), services, ssh, runtime.Info{InContainer: true})
	if services[0].Repo == nil {
		t.Fatal("Repo is nil, want {Present:false} reported for a no-repo-yet service")
	}
	if services[0].Repo.Present {
		t.Error("Repo.Present = true, want false on an SSH failure (no repo yet)")
	}
}
