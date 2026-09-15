// Tests for: workflow/envelope.go's rollback block — the
// ServiceSnapshot.Rollback field and ApplyRollbackInfo gating (docs/spec-
// workflows.md §8 R2 generalised / §12.6 GF-8). ApplyRollbackInfo is the
// pure gating logic; the live platform read that feeds it
// (ops.AppVersionCandidates) is wired in at the tools layer, out of scope
// for this workflow-layer unit test.
package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestStatusEnvelope_RollbackIds pins the shape: a runtime service with a
// rollback info entry gets `rollback: {active, backup}` on its snapshot.
func TestStatusEnvelope_RollbackIds(t *testing.T) {
	services := []ServiceSnapshot{
		{Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic},
	}
	ApplyRollbackInfo(services, map[string]RollbackInfo{
		"appstage": {Active: "av-new", Backup: []string{"av-old", "av-older"}},
	})
	if services[0].Rollback == nil {
		t.Fatal("Rollback is nil, want it populated for a runtime service with candidates")
	}
	if services[0].Rollback.Active != "av-new" {
		t.Errorf("Active = %q, want av-new", services[0].Rollback.Active)
	}
	backup := services[0].Rollback.Backup
	if len(backup) != 2 {
		t.Fatalf("Backup = %v, want [av-old av-older]", backup)
	}
	if backup[0] != "av-old" || backup[1] != "av-older" {
		t.Errorf("Backup = %v, want [av-old av-older]", backup)
	}
}

// TestStatusEnvelope_RollbackIds_ManagedService_StaysNil pins that a
// managed service never gets a rollback block even if the caller
// mistakenly supplies one — rollback only applies to runtime services
// with app-version history.
func TestStatusEnvelope_RollbackIds_ManagedService_StaysNil(t *testing.T) {
	services := []ServiceSnapshot{
		{Hostname: "db", RuntimeClass: topology.RuntimeManaged},
	}
	ApplyRollbackInfo(services, map[string]RollbackInfo{
		"db": {Active: "should-never-apply"},
	})
	if services[0].Rollback != nil {
		t.Errorf("Rollback = %+v, want nil for a managed service", services[0].Rollback)
	}
}

// TestStatusEnvelope_RollbackIds_NoEntry_StaysNil pins that a runtime
// service with no entry in infos (e.g. the live read failed or the
// service has never deployed) stays nil rather than getting a
// zero-value block.
func TestStatusEnvelope_RollbackIds_NoEntry_StaysNil(t *testing.T) {
	services := []ServiceSnapshot{
		{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic},
	}
	ApplyRollbackInfo(services, map[string]RollbackInfo{})
	if services[0].Rollback != nil {
		t.Errorf("Rollback = %+v, want nil when no entry was supplied", services[0].Rollback)
	}
}
