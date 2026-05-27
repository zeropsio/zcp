package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestEnrichWithMetaStatus_UnmanagedRuntimes_AppendsDirectiveWarning pins
// the discover-side adopt hint shape after the v9.101.4 redesign:
// unadopted runtime services append a directive prose warning to
// result.Warnings rather than populating a separate structured
// AdoptRecovery field. Agents parse Warnings as a prominent system-
// message bucket; a `Recovery`-named struct field they skim past as
// passive fallback (verified via t3.txt agent introspection).
//
// The warning MUST:
//   - name every unadopted runtime hostname (so agent doesn't need to
//     cross-reference services[i].managedByZcp per service)
//   - reference the exact `zerops_workflow action="start"
//     workflow="bootstrap" route="adopt"` call so the agent can
//     copy-paste rather than reconstruct
//   - state the consequence (subsequent service-scoped tools will
//     reject with ADOPT_REQUIRED) so the agent sees this as a
//     precondition, not advisory.
func TestEnrichWithMetaStatus_UnmanagedRuntimes_AppendsDirectiveWarning(t *testing.T) {
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

	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings: got %d entries, want 1; full=%v", len(result.Warnings), result.Warnings)
	}
	w := result.Warnings[0]

	// Names every unadopted runtime.
	for _, host := range []string{"appdev", "appstage", "workerstage"} {
		if !strings.Contains(w, host) {
			t.Errorf("warning missing hostname %q; got: %s", host, w)
		}
	}
	// Managed dep (db) MUST NOT appear — adoption is runtime-only.
	if strings.Contains(w, "db") {
		t.Errorf("warning must not list managed dep `db`; got: %s", w)
	}
	// Names the exact recovery call so the agent can copy-paste.
	for _, want := range []string{`action="start"`, `workflow="bootstrap"`, `route="adopt"`} {
		if !strings.Contains(w, want) {
			t.Errorf("warning missing recovery snippet %q; got: %s", want, w)
		}
	}
	// References the consequence so the warning is read as
	// precondition, not advisory.
	if !strings.Contains(w, "ADOPT_REQUIRED") {
		t.Errorf("warning must mention ADOPT_REQUIRED consequence (so agent reads it as precondition, not optional); got: %s", w)
	}

	// ManagedByZCP must still report per-service (false in this case).
	for _, s := range result.Services {
		if s.ManagedByZCP {
			t.Errorf("ManagedByZCP must stay false when no meta exists; %s = true", s.Hostname)
		}
	}
}

// Fully adopted project: no warning appended, no attention noise.
func TestEnrichWithMetaStatus_FullyAdopted_NoWarning(t *testing.T) {
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

	for _, w := range result.Warnings {
		if strings.Contains(w, "not adopted") {
			t.Errorf("Warnings must not include adopt-required prose when fully adopted; got: %s", w)
		}
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

// ZCP self-service (type=zcp@1) must never appear in the warning even
// when it shows up with ManagedByZCP:false + IsInfrastructure:false.
// Mirrors launch_source_context.go::isZCPSelfService — the
// control-plane container is never an adopt candidate.
func TestEnrichWithMetaStatus_ZCPSelfService_ExcludedFromWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", Type: "alpine/php-nginx@8.4", IsInfrastructure: false},
			{Hostname: "zcp", Type: "zcp@1", IsInfrastructure: false},
			{Hostname: "db", Type: "postgresql:single@18", IsInfrastructure: true},
		},
	}

	enrichWithMetaStatus(result, stateDir)

	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings: got %d entries, want 1; full=%v", len(result.Warnings), result.Warnings)
	}
	w := result.Warnings[0]
	// zcp@1 self-service MUST NOT appear in the warning's hostname list.
	// Use space-boundary check so "zcp" inside "zcp_workflow" / "zerops"
	// snippets doesn't false-positive.
	for _, marker := range []string{" zcp,", " zcp.", "zcp ", "zcp,"} {
		if strings.Contains(w, marker) {
			t.Errorf("warning hostname list must NOT contain zcp self-service marker %q; got: %s", marker, w)
		}
	}
	if !strings.Contains(w, "appdev") {
		t.Errorf("warning must list appdev (only legitimate unadopted runtime); got: %s", w)
	}
}

// Mixed adoption: only the unadopted runtimes surface in the
// warning's hostname enumeration; already-adopted halves are skipped.
func TestEnrichWithMetaStatus_MixedAdoption_WarningListsOnlyUnmanaged(t *testing.T) {
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
			{Hostname: "workerstage", Type: "nodejs@22", IsInfrastructure: false},
		},
	}

	enrichWithMetaStatus(result, stateDir)

	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings: got %d entries, want 1 (workerstage is unmanaged); full=%v",
			len(result.Warnings), result.Warnings)
	}
	w := result.Warnings[0]
	if !strings.Contains(w, "workerstage") {
		t.Errorf("warning must list workerstage; got: %s", w)
	}
	// appdev / appstage already adopted — must NOT appear in the
	// hostname list. Use space-comma boundary to avoid false positives.
	if strings.Contains(w, "appdev,") || strings.Contains(w, "appdev ") || strings.HasSuffix(w, "appdev.") {
		t.Errorf("warning must NOT list already-adopted appdev; got: %s", w)
	}
}
