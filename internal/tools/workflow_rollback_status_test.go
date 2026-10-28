// Tests for: tools/workflow_rollback_status.go — the platform-read wiring
// behind the envelope's per-service rollback block (docs/spec-workflows.md
// §8 R2 generalised / §12.6 GF-8). workflow.ApplyRollbackInfo itself is
// unit-tested in the workflow package; this covers only attachRollbackInfo's
// gating and the hostname -> serviceID resolution.
package tools

import (
	"context"
	"fmt"
	"sync/atomic"
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

// barrierAppVersionsClient holds platform reads until their entry receipts
// prove the bounded first wave overlaps.
type barrierAppVersionsClient struct {
	platform.Client
	barrier *readBarrier
}

func (c *barrierAppVersionsClient) ListServiceAppVersions(ctx context.Context, serviceID string) ([]platform.AppVersionEvent, error) {
	if err := c.barrier.enter(ctx); err != nil {
		return nil, fmt.Errorf("app versions read barrier: %w", err)
	}
	return c.Client.ListServiceAppVersions(ctx, serviceID)
}

func TestAttachRollbackInfo_ManyServices_ReadsRunConcurrently(t *testing.T) {
	t.Parallel()
	barrier := &readBarrier{entered: make(chan struct{}, 5), release: make(chan struct{})}
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
	client := &barrierAppVersionsClient{Client: mock, barrier: barrier}
	services := []workflow.ServiceSnapshot{
		{Hostname: "svc1", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc2", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc3", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc4", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "svc5", RuntimeClass: topology.RuntimeDynamic},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		attachRollbackInfo(context.Background(), services, client, "proj-1")
	}()
	for range attachRollbackInfoConcurrency {
		<-barrier.entered
	}
	close(barrier.release)
	<-done
	if peak := barrier.peak.Load(); peak != attachRollbackInfoConcurrency {
		t.Errorf("peak concurrent reads = %d, want %d", peak, attachRollbackInfoConcurrency)
	}
	for _, svc := range services {
		if svc.Rollback == nil || svc.Rollback.Active == "" {
			t.Errorf("service %s: Rollback = %+v, want Active set", svc.Hostname, svc.Rollback)
		}
	}
}

// appVersionReadCounter counts the app-version history reads that reach the
// platform.
type appVersionReadCounter struct {
	platform.Client
	reads atomic.Int32
}

func (c *appVersionReadCounter) ListServiceAppVersions(ctx context.Context, serviceID string) ([]platform.AppVersionEvent, error) {
	c.reads.Add(1)
	return c.Client.ListServiceAppVersions(ctx, serviceID) //nolint:wrapcheck // test double
}

// TestAttachRollbackInfo_PoolAndSandbox_NeverRead pins that a pool or a Box
// gets no app-version history read: neither carries the app's versions
// (docs/spec-workflows.md §8 PS-3).
func TestAttachRollbackInfo_PoolAndSandbox_NeverRead(t *testing.T) {
	t.Parallel()
	for _, class := range []topology.RuntimeClass{topology.RuntimePool, topology.RuntimeSandbox} {
		t.Run(string(class), func(t *testing.T) {
			t.Parallel()
			counter := &appVersionReadCounter{Client: platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "s1", Name: "box"}}).
				WithServiceAppVersions("s1", []platform.AppVersionEvent{
					{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 1},
				})}
			services := []workflow.ServiceSnapshot{{Hostname: "box", RuntimeClass: class}}

			attachRollbackInfo(context.Background(), services, counter, "proj-1")

			if n := counter.reads.Load(); n != 0 {
				t.Errorf("app-version reads = %d, want 0 for class %q", n, class)
			}
			if services[0].Rollback != nil {
				t.Errorf("Rollback = %+v, want nil for class %q", services[0].Rollback, class)
			}
		})
	}
}
