// Tests for: tools/workflow_rollback_status.go — the platform-read wiring
// behind the envelope's per-service rollback block (docs/spec-workflows.md
// §8 R2 generalised / §12.6 GF-8). workflow.ApplyRollbackInfo itself is
// unit-tested in the workflow package; this covers only attachRollbackInfo's
// gating and the hostname -> serviceID resolution.
package tools

import (
	"context"
	"testing"
	"time"

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

// slowAppVersionsClient wraps a platform.Client and sleeps for Delay on
// every ListServiceAppVersions call before delegating — used to prove
// attachRollbackInfo reads services concurrently rather than one
// AppVersionCandidates round trip at a time.
type slowAppVersionsClient struct {
	platform.Client
	delay time.Duration
}

func (c *slowAppVersionsClient) ListServiceAppVersions(ctx context.Context, serviceID string) ([]platform.AppVersionEvent, error) {
	select {
	case <-time.After(c.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return c.Client.ListServiceAppVersions(ctx, serviceID)
}

// TestAttachRollbackInfo_ManyServices_ReadsRunConcurrently proves the
// per-service AppVersionCandidates reads overlap instead of running one
// at a time: 5 services each behind a ~150ms platform read finish in
// well under 5x150ms if (and only if) they run concurrently. Concurrency
// is bounded at attachRollbackInfoConcurrency (4), so 5 services still
// take two waves (~2x150ms wall-clock) rather than one — the threshold
// below sits comfortably above that 2-wave floor and well below the
// 5x150ms serial floor, so scheduler jitter around the 2-wave boundary
// can't flake it.
func TestAttachRollbackInfo_ManyServices_ReadsRunConcurrently(t *testing.T) {
	t.Parallel()
	const delay = 150 * time.Millisecond
	const wantUnder = 4 * delay // serial would take 5*delay=750ms; 2 waves (bounded at attachRollbackInfoConcurrency=4) land near 2*delay=300ms
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "svc1"},
			{ID: "s2", Name: "svc2"},
			{ID: "s3", Name: "svc3"},
			{ID: "s4", Name: "svc4"},
			{ID: "s5", Name: "svc5"},
		})
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5"} {
		mock.WithServiceAppVersions(id, []platform.AppVersionEvent{
			{ID: "av-" + id, ServiceStackID: id, Status: platform.ServiceStatusActive, Sequence: 1},
		})
	}
	client := &slowAppVersionsClient{Client: mock, delay: delay}
	services := []workflow.ServiceSnapshot{
		{Hostname: "svc1", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc2", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc3", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc4", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc5", RuntimeClass: topology.RuntimeDynamic},
	}

	start := time.Now()
	attachRollbackInfo(context.Background(), services, client, "proj-1")
	elapsed := time.Since(start)

	if elapsed >= wantUnder {
		t.Errorf("elapsed = %v, want < %v (serial reads would take >= %v)", elapsed, wantUnder, 5*delay)
	}
	for _, svc := range services {
		if svc.Rollback == nil || svc.Rollback.Active == "" {
			t.Errorf("service %s: Rollback = %+v, want Active set", svc.Hostname, svc.Rollback)
		}
	}
}
