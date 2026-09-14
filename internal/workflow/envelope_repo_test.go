// Tests for: workflow/envelope.go's repo block — the ServiceSnapshot.Repo
// field and ApplyRepoStatus gating (docs/spec-workflows.md §8 GLC-7, G1/G2).
// ApplyRepoStatus is the pure gating logic; the live SSH read that feeds
// it (ops.ReadRepoStatus) is wired in at the tools layer, out of scope
// for this workflow-layer unit test.
package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestApplyRepoStatus_DevService_GetsRepoBlock(t *testing.T) {
	services := []ServiceSnapshot{
		{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic},
	}
	ApplyRepoStatus(services, map[string]RepoStatus{
		"appdev": {Present: true, Head: "abc123", Baseline: "av-1"},
	})
	if services[0].Repo == nil {
		t.Fatal("Repo is nil, want it populated for a dynamic (dev) service")
	}
	if !services[0].Repo.Present || services[0].Repo.Head != "abc123" || services[0].Repo.Baseline != "av-1" {
		t.Errorf("Repo = %+v, want {Present:true Head:abc123 Baseline:av-1}", services[0].Repo)
	}
}

func TestApplyRepoStatus_ManagedService_StaysNilEvenIfStatusSupplied(t *testing.T) {
	services := []ServiceSnapshot{
		{Hostname: "db", RuntimeClass: topology.RuntimeManaged},
	}
	ApplyRepoStatus(services, map[string]RepoStatus{
		"db": {Present: true, Head: "should-never-apply"},
	})
	if services[0].Repo != nil {
		t.Errorf("Repo = %+v, want nil for a managed service", services[0].Repo)
	}
}

func TestApplyRepoStatus_UnknownClass_StaysNil(t *testing.T) {
	services := []ServiceSnapshot{
		{Hostname: "mystery", RuntimeClass: topology.RuntimeUnknown},
	}
	ApplyRepoStatus(services, map[string]RepoStatus{
		"mystery": {Present: true},
	})
	if services[0].Repo != nil {
		t.Errorf("Repo = %+v, want nil for an unknown-class service", services[0].Repo)
	}
}

func TestApplyRepoStatus_NoStatusSupplied_StaysNil(t *testing.T) {
	services := []ServiceSnapshot{
		{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic},
	}
	ApplyRepoStatus(services, map[string]RepoStatus{})
	if services[0].Repo != nil {
		t.Errorf("Repo = %+v, want nil when no status was supplied for this hostname", services[0].Repo)
	}
}

func TestApplyRepoStatus_MixedServices_OnlyDevGetsRepo(t *testing.T) {
	services := []ServiceSnapshot{
		{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "db", RuntimeClass: topology.RuntimeManaged},
		{Hostname: "appstage", RuntimeClass: topology.RuntimeStatic},
	}
	statuses := map[string]RepoStatus{
		"appdev":   {Present: true, Head: "sha1"},
		"db":       {Present: true, Head: "irrelevant"},
		"appstage": {Present: false},
	}
	ApplyRepoStatus(services, statuses)
	if services[0].Repo == nil || services[0].Repo.Head != "sha1" {
		t.Errorf("appdev.Repo = %+v, want {Present:true Head:sha1}", services[0].Repo)
	}
	if services[1].Repo != nil {
		t.Errorf("db.Repo = %+v, want nil (managed)", services[1].Repo)
	}
	if services[2].Repo == nil || services[2].Repo.Present {
		t.Errorf("appstage.Repo = %+v, want {Present:false}", services[2].Repo)
	}
}
