package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestEnrichWithMetaStatus_AdoptableRuntime_AppendsAdoptWarning pins
// the adoptable-runtime classification + adopt-route warning. Live
// runtime services with no IsComplete meta land as AdoptionAdoptable;
// warning prose names every hostname + the exact `route="adopt"`
// recovery call + the ADOPT_REQUIRED consequence framing so the agent
// reads this as precondition, not advisory.
func TestEnrichWithMetaStatus_AdoptableRuntime_AppendsAdoptWarning(t *testing.T) {
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

	enrichWithMetaStatus(result, stateDir, nil)

	// AdoptionState assignments.
	wantStates := map[string]ops.AdoptionState{
		"appdev":      ops.AdoptionAdoptable,
		"appstage":    ops.AdoptionAdoptable,
		"workerstage": ops.AdoptionAdoptable,
		"db":          ops.AdoptionManagedDep,
	}
	for _, s := range result.Services {
		if got, want := s.AdoptionState, wantStates[s.Hostname]; got != want {
			t.Errorf("AdoptionState[%s]: got %q, want %q", s.Hostname, got, want)
		}
	}

	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings: got %d entries, want 1 (adopt-only); full=%v",
			len(result.Warnings), result.Warnings)
	}
	w := result.Warnings[0]
	for _, host := range []string{"appdev", "appstage", "workerstage"} {
		if !strings.Contains(w, host) {
			t.Errorf("adopt warning missing hostname %q; got: %s", host, w)
		}
	}
	if strings.Contains(w, " db,") || strings.Contains(w, "db ") || strings.HasSuffix(w, " db.") {
		t.Errorf("adopt warning must not list managed dep `db`; got: %s", w)
	}
	for _, want := range []string{`action="start"`, `workflow="bootstrap"`, `route="adopt"`, "ADOPT_REQUIRED"} {
		if !strings.Contains(w, want) {
			t.Errorf("adopt warning missing snippet %q; got: %s", want, w)
		}
	}
	// B-fix + drift-guard: the warning's op CLASS must name EVERY adoption-gated
	// surface, not an under-inclusive enumerated subset. The regression that hid
	// the launch bounce was exactly this list narrowing (commit 1d36eb73 dropped
	// launch-production); pinning the full set fails any future narrowing.
	for _, gatedOp := range []string{"zerops_deploy", "develop", "build-integration", "launch-production"} {
		if !strings.Contains(w, gatedOp) {
			t.Errorf("adopt warning must name adoption-gated op %q (drift-guard); got: %s", gatedOp, w)
		}
	}
	// 3 same-stack runtimes is NOT the 2-runtime pairing collision — no plan steer.
	if strings.Contains(w, "submit an explicit `plan=[...]`") {
		t.Errorf("3-runtime adopt must not emit the 2-runtime pairing steer; got: %s", w)
	}
}

// TestEnrichWithMetaStatus_TwoSameStackAdoptable_SteersToPlan pins the D-fix:
// when exactly two adoptable runtimes share a deployment stack, the adopt
// warning pre-steers to the explicit plan=[...] shape the handler accepts —
// instead of the scope=[...] path the handler is guaranteed to reject (it
// refuses to guess a standard dev/stage pair vs two independent devs). The
// collision predicate derives from the SAME topology.CanonicalBareForm equality
// the adopt CHECK uses, so the TELL can't drift.
func TestEnrichWithMetaStatus_TwoSameStackAdoptable_SteersToPlan(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "appstage", Type: "nodejs@22", IsInfrastructure: false},
		},
	}
	enrichWithMetaStatus(result, stateDir, nil)
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings: got %d, want 1; full=%v", len(result.Warnings), result.Warnings)
	}
	w := result.Warnings[0]
	if !strings.Contains(w, "submit an explicit `plan=[...]`") {
		t.Errorf("same-stack pair must steer to explicit plan, not scope; got: %s", w)
	}
	for _, host := range []string{"appdev", "appstage"} {
		if !strings.Contains(w, host) {
			t.Errorf("pairing steer missing hostname %q; got: %s", host, w)
		}
	}
}

// TestEnrichWithMetaStatus_TwoDifferentStackAdoptable_NoPlanSteer pins that the
// collision is CanonicalBareForm-keyed, not count-keyed: two adoptable runtimes
// of DIFFERENT stacks are unambiguous, so the scope path stays (no plan steer).
func TestEnrichWithMetaStatus_TwoDifferentStackAdoptable_NoPlanSteer(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "api", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "web", Type: "php-nginx@8.4", IsInfrastructure: false},
		},
	}
	enrichWithMetaStatus(result, stateDir, nil)
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings: got %d, want 1; full=%v", len(result.Warnings), result.Warnings)
	}
	if strings.Contains(result.Warnings[0], "submit an explicit `plan=[...]`") {
		t.Errorf("different-stack pair must NOT steer to plan (unambiguous); got: %s", result.Warnings[0])
	}
}

// Pair-keyed adopted runtime: complete meta on devhost means both
// halves resolve as AdoptionAdopted, no warning.
func TestEnrichWithMetaStatus_FullyAdopted_AdoptedStateNoWarning(t *testing.T) {
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

	enrichWithMetaStatus(result, stateDir, nil)

	// Both pair halves → AdoptionAdopted (pair-keyed meta hit).
	for _, s := range result.Services {
		if s.IsInfrastructure {
			if s.AdoptionState != ops.AdoptionManagedDep {
				t.Errorf("AdoptionState[%s]: got %q, want managed-dep", s.Hostname, s.AdoptionState)
			}
			continue
		}
		if s.AdoptionState != ops.AdoptionAdopted {
			t.Errorf("AdoptionState[%s]: got %q, want adopted", s.Hostname, s.AdoptionState)
		}
	}
	for _, w := range result.Warnings {
		if strings.Contains(w, "adoptable") || strings.Contains(w, "resumable") {
			t.Errorf("no adoption-related warning expected on fully-adopted project; got: %s", w)
		}
	}
}

// Resumable runtime: incomplete meta + non-empty BootstrapSession
// surfaces as AdoptionResumable. Warning prose includes sessionId so
// agent can call `route="resume" sessionId="<...>"` without hitting
// the INVALID_PARAMETER detour (handler rejects route=resume without
// sessionId).
func TestEnrichWithMetaStatus_ResumableRuntime_NamesSessionIdInWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "sess-abc123",
		// BootstrappedAt empty → IsComplete()=false → resumable.
	}); err != nil {
		t.Fatal(err)
	}

	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", Type: "nodejs@22", IsInfrastructure: false},
		},
	}

	enrichWithMetaStatus(result, stateDir, nil)

	if got := result.Services[0].AdoptionState; got != ops.AdoptionResumable {
		t.Errorf("AdoptionState: got %q, want resumable", got)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings: got %d, want 1 (resume only); full=%v",
			len(result.Warnings), result.Warnings)
	}
	w := result.Warnings[0]
	for _, want := range []string{"appdev", `route="resume"`, `sessionId="sess-abc123"`, "INVALID_PARAMETER"} {
		if !strings.Contains(w, want) {
			t.Errorf("resume warning missing snippet %q; got: %s", want, w)
		}
	}
	// The executable recovery call MUST be route=resume (not route=adopt).
	// Prose may mention adopt as contrast ("adopt would reject these"),
	// which is OK — the user-actionable call is the resume snippet.
	if !strings.Contains(w, `Run `+"`"+`zerops_workflow action="start" workflow="bootstrap" route="resume"`) {
		t.Errorf("resume warning's executable call must be route=resume; got: %s", w)
	}
}

// Orphan meta: incomplete meta WITH empty BootstrapSession falls to
// AdoptionAdoptable (matches workflow.adoptableServices semantics —
// route.go:309 "Incomplete meta with BootstrapSession tag is
// resumable, not adoptable" — empty session → adopt).
func TestEnrichWithMetaStatus_OrphanIncompleteMetaEmptySession_Adoptable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "", // orphan
		// BootstrappedAt empty → not complete.
	}); err != nil {
		t.Fatal(err)
	}

	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", Type: "nodejs@22", IsInfrastructure: false},
		},
	}

	enrichWithMetaStatus(result, stateDir, nil)

	if got := result.Services[0].AdoptionState; got != ops.AdoptionAdoptable {
		t.Errorf("AdoptionState: got %q, want adoptable (orphan meta → adopt route)", got)
	}
}

// Mixed project: adopted + adoptable + resumable + managed-dep + zcp-self
// all in one response. Each lands in its bucket; each emits warning
// only when the bucket warrants one (adoptable + resumable have
// warnings, others don't).
func TestEnrichWithMetaStatus_MixedStates_BothWarningsFire(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	// appdev adopted (complete meta).
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrappedAt:   "2026-05-27",
		BootstrapSession: "sess-1",
	}); err != nil {
		t.Fatal(err)
	}
	// workerdev resumable (incomplete + session).
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "workerdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "sess-2",
	}); err != nil {
		t.Fatal(err)
	}

	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "appstage", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "workerdev", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "frontend", Type: "nodejs@22", IsInfrastructure: false}, // adoptable (no meta)
			{Hostname: "db", Type: "postgresql@16", IsInfrastructure: true},
			{Hostname: "zcp", Type: "zcp@1", IsInfrastructure: false},
		},
	}

	enrichWithMetaStatus(result, stateDir, nil)

	wantStates := map[string]ops.AdoptionState{
		"appdev":    ops.AdoptionAdopted,
		"appstage":  ops.AdoptionAdopted,
		"workerdev": ops.AdoptionResumable,
		"frontend":  ops.AdoptionAdoptable,
		"db":        ops.AdoptionManagedDep,
		"zcp":       ops.AdoptionZCPSelf,
	}
	for _, s := range result.Services {
		if got, want := s.AdoptionState, wantStates[s.Hostname]; got != want {
			t.Errorf("AdoptionState[%s]: got %q, want %q", s.Hostname, got, want)
		}
	}

	if len(result.Warnings) != 2 {
		t.Fatalf("Warnings: got %d, want 2 (adopt + resume); full=%v",
			len(result.Warnings), result.Warnings)
	}
	// Identify which is which.
	var adoptW, resumeW string
	for _, w := range result.Warnings {
		switch {
		case strings.Contains(w, "adoptable"):
			adoptW = w
		case strings.Contains(w, "resumable"):
			resumeW = w
		}
	}
	if adoptW == "" || resumeW == "" {
		t.Fatalf("expected both adoptable + resumable warnings; got: %v", result.Warnings)
	}
	if !strings.Contains(adoptW, "frontend") {
		t.Errorf("adopt warning should list frontend; got: %s", adoptW)
	}
	if strings.Contains(adoptW, "workerdev") {
		t.Errorf("adopt warning must NOT list workerdev (resumable); got: %s", adoptW)
	}
	if !strings.Contains(resumeW, "workerdev") {
		t.Errorf("resume warning should list workerdev; got: %s", resumeW)
	}
	if !strings.Contains(resumeW, `sessionId="sess-2"`) {
		t.Errorf("resume warning should name sessionId=sess-2; got: %s", resumeW)
	}
	if strings.Contains(resumeW, "frontend") {
		t.Errorf("resume warning must NOT list frontend (adoptable); got: %s", resumeW)
	}
}

// ZCP self-service (type=zcp@1) → AdoptionZCPSelf, never in warnings.
// Mirror of launch_source_context.go::isZCPSelfService gating.
func TestEnrichWithMetaStatus_ZCPSelf_StateExcludedFromWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", Type: "alpine/php-nginx@8.4", IsInfrastructure: false},
			{Hostname: "zcp", Type: "zcp@1", IsInfrastructure: false},
		},
	}

	enrichWithMetaStatus(result, stateDir, nil)

	for _, s := range result.Services {
		if s.Hostname == "zcp" && s.AdoptionState != ops.AdoptionZCPSelf {
			t.Errorf("zcp AdoptionState: got %q, want zcp-self", s.AdoptionState)
		}
	}
	for _, w := range result.Warnings {
		if strings.Contains(w, " zcp,") || strings.Contains(w, "zcp ") || strings.Contains(w, ",zcp") {
			t.Errorf("warning must not list zcp self-service marker; got: %s", w)
		}
	}
}
