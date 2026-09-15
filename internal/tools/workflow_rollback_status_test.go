// Tests for: tools/workflow_rollback_status.go — the platform-read wiring
// behind the envelope's per-service rollback block (docs/spec-workflows.md
// §8 R2 generalised / §12.6 GF-8). workflow.ApplyRollbackInfo itself is
// unit-tested in the workflow package; this covers only attachRollbackInfo's
// gating and the hostname -> serviceID resolution.
package tools

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestAttachRollbackInfo_NilClient_NoOp(t *testing.T) {
	t.Parallel()
	services := []workflow.ServiceSnapshot{{Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic}}
	attachRollbackInfo(context.Background(), services, nil, "proj-1")
	if services[0].Rollback != nil {
		t.Errorf("Rollback = %+v, want nil without a platform client", services[0].Rollback)
	}
}

func TestAttachRollbackInfo_RuntimeService_GetsActiveAndBackup(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage"}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 2},
			{ID: "av-old", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 1},
		})
	services := []workflow.ServiceSnapshot{{Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic}}

	attachRollbackInfo(context.Background(), services, mock, "proj-1")

	if services[0].Rollback == nil {
		t.Fatal("Rollback is nil, want it populated")
	}
	if services[0].Rollback.Active != "av-new" {
		t.Errorf("Active = %q, want av-new", services[0].Rollback.Active)
	}
	backup := services[0].Rollback.Backup
	if len(backup) != 1 {
		t.Fatalf("Backup = %v, want [av-old]", backup)
	}
	if backup[0] != "av-old" {
		t.Errorf("Backup[0] = %q, want av-old", backup[0])
	}
}

func TestAttachRollbackInfo_ManagedService_StaysNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "db"}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 1},
		})
	services := []workflow.ServiceSnapshot{{Hostname: "db", RuntimeClass: topology.RuntimeManaged}}

	attachRollbackInfo(context.Background(), services, mock, "proj-1")

	if services[0].Rollback != nil {
		t.Errorf("Rollback = %+v, want nil for a managed service", services[0].Rollback)
	}
}

func TestAttachRollbackInfo_UnknownHostname_StaysNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "otherhost"}})
	services := []workflow.ServiceSnapshot{{Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic}}

	attachRollbackInfo(context.Background(), services, mock, "proj-1")

	if services[0].Rollback != nil {
		t.Errorf("Rollback = %+v, want nil when no live service matches the snapshot hostname", services[0].Rollback)
	}
}

func TestAttachRollbackInfo_NeverDeployed_StaysNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage"}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{})
	services := []workflow.ServiceSnapshot{{Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic}}

	attachRollbackInfo(context.Background(), services, mock, "proj-1")

	if services[0].Rollback != nil {
		t.Errorf("Rollback = %+v, want nil for a service with no app-version history", services[0].Rollback)
	}
}
