package tools

import (
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestEnrichWithMetaStatus_UnmanagedRuntimes_PopulatesAdoptRecovery pins
// the discover-side adopt hint. Karel's transcript-2 friction: agent
// called workflow="develop" on a project where the runtime services
// existed live but had no ServiceMeta, and the only learning surface
// was the ADOPT_REQUIRED rejection from develop. With this enrich
// step, the FIRST discover call surfaces:
//
//   - unmanagedRuntimes: ["appdev", "appstage", "workerstage"]
//   - adoptRecovery: {tool: zerops_workflow, action: start, args: {workflow:bootstrap, route:adopt}}
//
// so the agent picks bootstrap+route=adopt without the wasted develop
// round-trip.
func TestEnrichWithMetaStatus_UnmanagedRuntimes_PopulatesAdoptRecovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "appstage", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "workerstage", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "db", Type: "postgresql@16", IsInfrastructure: true},
		},
	}

	enrichWithMetaStatus(result, stateDir)

	// Managed-service (db) excluded from unmanagedRuntimes — adoption
	// is runtime-only, managed deps live as API-authoritative
	// dependencies.
	wantUnmanaged := []string{"appdev", "appstage", "workerstage"}
	if len(result.UnmanagedRuntimes) != len(wantUnmanaged) {
		t.Fatalf("UnmanagedRuntimes: got %v, want %v",
			result.UnmanagedRuntimes, wantUnmanaged)
	}
	for i, want := range wantUnmanaged {
		if result.UnmanagedRuntimes[i] != want {
			t.Errorf("UnmanagedRuntimes[%d]: got %q, want %q",
				i, result.UnmanagedRuntimes[i], want)
		}
	}

	if result.AdoptRecovery == nil {
		t.Fatal("AdoptRecovery must be populated when unmanagedRuntimes is non-empty")
	}
	if result.AdoptRecovery.Tool != "zerops_workflow" {
		t.Errorf("AdoptRecovery.Tool: got %q, want zerops_workflow", result.AdoptRecovery.Tool)
	}
	if result.AdoptRecovery.Action != "start" {
		t.Errorf("AdoptRecovery.Action: got %q, want start", result.AdoptRecovery.Action)
	}
	if got := result.AdoptRecovery.Args["workflow"]; got != "bootstrap" {
		t.Errorf("AdoptRecovery.Args[workflow]: got %q, want bootstrap", got)
	}
	if got := result.AdoptRecovery.Args["route"]; got != "adopt" {
		t.Errorf("AdoptRecovery.Args[route]: got %q, want adopt", got)
	}

	// ManagedByZCP must still report per-service (false in this case).
	for _, s := range result.Services {
		if s.ManagedByZCP {
			t.Errorf("ManagedByZCP must stay false when no meta exists; %s = true", s.Hostname)
		}
	}
}

// When the project is fully adopted (every runtime has a complete meta),
// the hint stays absent — agents shouldn't pay attention noise for the
// happy path.
func TestEnrichWithMetaStatus_FullyAdopted_NoHint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrappedAt:   "2026-05-27",
		BootstrapSession: "sess-1",
	}); err != nil {
		t.Fatal(err)
	}

	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "appstage", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "db", Type: "postgresql@16", IsInfrastructure: true},
		},
	}

	enrichWithMetaStatus(result, stateDir)

	if len(result.UnmanagedRuntimes) > 0 {
		t.Errorf("UnmanagedRuntimes: got %v, want empty (every runtime adopted)",
			result.UnmanagedRuntimes)
	}
	if result.AdoptRecovery != nil {
		t.Errorf("AdoptRecovery must be nil when no unmanaged runtimes; got %+v",
			result.AdoptRecovery)
	}
	// Pair-keyed: both halves resolve to the shared meta → ManagedByZCP=true.
	for _, s := range result.Services {
		if s.IsInfrastructure {
			continue
		}
		if !s.ManagedByZCP {
			t.Errorf("ManagedByZCP must be true for %q (pair-keyed meta hit)", s.Hostname)
		}
	}
}

// Mixed state: some runtimes adopted, some not. The not-adopted ones
// surface in unmanagedRuntimes; the hint still fires (any unadopted
// runtime is a signal worth surfacing).
func TestEnrichWithMetaStatus_MixedAdoption_HintListsOnlyUnmanaged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrappedAt:   "2026-05-27",
		BootstrapSession: "sess-1",
	}); err != nil {
		t.Fatal(err)
	}

	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "appstage", Type: "nodejs@22", IsInfrastructure: false},
			// workerstage exists live but no meta — should land in unmanaged.
			{Hostname: "workerstage", Type: "nodejs@22", IsInfrastructure: false},
		},
	}

	enrichWithMetaStatus(result, stateDir)

	if len(result.UnmanagedRuntimes) != 1 || result.UnmanagedRuntimes[0] != "workerstage" {
		t.Errorf("UnmanagedRuntimes: got %v, want [workerstage]", result.UnmanagedRuntimes)
	}
	if result.AdoptRecovery == nil {
		t.Fatal("AdoptRecovery must fire when ANY runtime is unmanaged")
	}
}
