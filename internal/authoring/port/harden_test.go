package port

import (
	"slices"
	"testing"
)

// managedDep builds a managed PortDependency with the given resolved type.
func managedDep(declared, managedType string) PortDependency {
	return PortDependency{Declared: declared, Mapping: DepMappingManaged, ManagedType: managedType}
}

// TestPlanHarden_PerSurfaceSentinels asserts the sentinel plan targets the RIGHT
// durable surface per dependency kind (managed-db / object-storage /
// shared-storage), never the ephemeral container FS.
func TestPlanHarden_PerSurfaceSentinels(t *testing.T) {
	t.Parallel()
	plan := PortPlan{
		Target: "strapi",
		Dependencies: []PortDependency{
			managedDep("postgresql", "postgresql@18"),
			managedDep("object-storage", "object-storage"),
			managedDep("shared-storage", "shared-storage"),
		},
	}
	hp := PlanHarden(plan)
	if len(hp.PersistenceProbes) != 3 {
		t.Fatalf("expected 3 sentinels, got %d (%+v)", len(hp.PersistenceProbes), hp.PersistenceProbes)
	}
	surfaceByDep := map[string]PersistenceSurface{}
	for _, p := range hp.PersistenceProbes {
		surfaceByDep[p.Dependency] = p.Surface
	}
	if surfaceByDep["postgresql@18"] != SurfaceManagedDB {
		t.Errorf("postgres surface = %q, want managed-db", surfaceByDep["postgresql@18"])
	}
	if surfaceByDep["object-storage"] != SurfaceObjectStorage {
		t.Errorf("object-storage surface = %q", surfaceByDep["object-storage"])
	}
	if surfaceByDep["shared-storage"] != SurfaceSharedStorage {
		t.Errorf("shared-storage surface = %q", surfaceByDep["shared-storage"])
	}
}

// TestPlanHarden_HACandidatesExcludeObjectStorage: object-storage has no mode, so
// it is NOT an HA candidate; postgres (mode-bearing) is.
func TestPlanHarden_HACandidatesExcludeObjectStorage(t *testing.T) {
	t.Parallel()
	plan := PortPlan{
		Dependencies: []PortDependency{
			managedDep("postgresql", "postgresql@18"),
			managedDep("object-storage", "object-storage"),
		},
	}
	hp := PlanHarden(plan)
	if hp.HAScaleProbe.TargetAppContainers != 2 {
		t.Errorf("HA probe target containers = %d, want 2", hp.HAScaleProbe.TargetAppContainers)
	}
	if !slices.Contains(hp.HAScaleProbe.HACandidates, "postgresql@18") {
		t.Errorf("postgres should be an HA candidate, got %v", hp.HAScaleProbe.HACandidates)
	}
	if slices.Contains(hp.HAScaleProbe.HACandidates, "object-storage") {
		t.Errorf("object-storage must NOT be an HA candidate (no mode), got %v", hp.HAScaleProbe.HACandidates)
	}
}

// TestPlanHarden_NoDurableDepNote: a port with no durable dependency notes that
// C5 cannot reach grade 2 (container FS is ephemeral).
func TestPlanHarden_NoDurableDepNote(t *testing.T) {
	t.Parallel()
	hp := PlanHarden(PortPlan{Target: "x"})
	if len(hp.PersistenceProbes) != 0 {
		t.Errorf("no managed dep → no sentinels, got %v", hp.PersistenceProbes)
	}
	if len(hp.Notes) == 0 {
		t.Errorf("expected a note that C5 cannot reach grade 2 without a durable dep")
	}
}

// TestGradeHarden_FromMockedResults grades C5/C6 from injected (mocked) harden
// results — proving the grader is pure with no live calls.
func TestGradeHarden_FromMockedResults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		res    HardenResults
		ha     AchievableHA // the already-DERIVED breakdown C6 grades off (filtered list)
		wantC5 int
		wantC6 int
	}{
		{
			name:   "durable persistence + full HA",
			res:    HardenResults{SentinelSurvivedRedeploy: true, SentinelOnDurableSurface: true, HAVerifyPassed: true},
			ha:     AchievableHA{AppContainers: 2, ManagedHADeps: []string{"clickhouse@25.3"}},
			wantC5: 2, wantC6: 2,
		},
		{
			name: "C6 grades off the FILTERED list: ≥2 containers + haVerify but NO mode-bearing dep measured HA → throughput-only (C6=1)",
			res:  HardenResults{SentinelSurvivedRedeploy: true, SentinelOnDurableSurface: true, HAVerifyPassed: true},
			// ManagedHADeps empty even though the agent may have sent raw haDeps —
			// a bogus/storage/unplanned entry never reaches this filtered list, so
			// the grade stays C6=1 in lockstep with the all-NON_HA emitted topology.
			ha:     AchievableHA{AppContainers: 2},
			wantC5: 2, wantC6: 1,
		},
		{
			name:   "ephemeral survival + throughput-only scaling",
			res:    HardenResults{SentinelSurvivedRedeploy: true, SentinelOnDurableSurface: false, HAVerifyPassed: false},
			ha:     AchievableHA{AppContainers: 2},
			wantC5: 1, wantC6: 1,
		},
		{
			name:   "no persistence + single container",
			res:    HardenResults{},
			ha:     AchievableHA{},
			wantC5: 0, wantC6: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c5, c6 := GradeHarden(tt.res, tt.ha)
			if c5.Grade != tt.wantC5 {
				t.Errorf("C5 grade = %d, want %d", c5.Grade, tt.wantC5)
			}
			if c6.Grade != tt.wantC6 {
				t.Errorf("C6 grade = %d, want %d", c6.Grade, tt.wantC6)
			}
			if c5.Check != PortCheckPersists || c6.Check != PortCheckHA {
				t.Errorf("grades carry the wrong check labels: %v %v", c5.Check, c6.Check)
			}
		})
	}
}

// TestDeriveAchievableHA_PerDepTopology pins the honest HA breakdown: a managed
// dep lands in ManagedHADeps iff its canonical base was MEASURED HA (haDeps);
// every other managed dep lands in NotHA. The PostHog shape — ONLY ClickHouse
// HA, Postgres/Kafka/Valkey NOT — is the canonical case. Self-run deps never
// appear. Matching is canonical-base (so bare "clickhouse" matches the resolved
// "clickhouse@25.3").
func TestDeriveAchievableHA_PerDepTopology(t *testing.T) {
	t.Parallel()
	plan := PortPlan{Dependencies: []PortDependency{
		managedDep("clickhouse", "clickhouse@25.3"),
		managedDep("postgresql", "postgresql@18"),
		managedDep("kafka", "kafka@3.9"),
		managedDep("valkey", "valkey@7.2"),
		managedDep("object-storage", "object-storage"),   // NOT mode-bearing → neither list
		{Declared: "exotic", Mapping: DepMappingSelfRun}, // never managed → never in either list
	}}

	// Even if the agent erroneously lists object-storage as HA, it must NOT appear
	// in either list (storage has no `mode:` field).
	ha := DeriveAchievableHA(plan, 2, []string{"clickhouse", "object-storage"})

	if ha.AppContainers != 2 {
		t.Errorf("AppContainers = %d, want 2", ha.AppContainers)
	}
	if !slices.Equal(ha.ManagedHADeps, []string{"clickhouse@25.3"}) {
		t.Errorf("ManagedHADeps = %v, want [clickhouse@25.3] (object-storage excluded even though listed in haDeps)", ha.ManagedHADeps)
	}
	if !slices.Equal(ha.NotHA, []string{"postgresql@18", "kafka@3.9", "valkey@7.2"}) {
		t.Errorf("NotHA = %v, want the 3 mode-bearing non-measured deps (storage + self-run excluded)", ha.NotHA)
	}
	if slices.Contains(ha.ManagedHADeps, "object-storage") || slices.Contains(ha.NotHA, "object-storage") {
		t.Errorf("object-storage must be in NEITHER HA list (not mode-bearing), got ManagedHADeps=%v NotHA=%v", ha.ManagedHADeps, ha.NotHA)
	}

	// No measured HA deps → every mode-bearing managed dep is NotHA, none claimed HA.
	none := DeriveAchievableHA(plan, 1, nil)
	if len(none.ManagedHADeps) != 0 {
		t.Errorf("empty haDeps must yield no ManagedHADeps, got %v", none.ManagedHADeps)
	}
	if len(none.NotHA) != 4 { // clickhouse + postgresql + kafka + valkey (storage + self-run excluded)
		t.Errorf("empty haDeps → all 4 mode-bearing managed deps in NotHA, got %v", none.NotHA)
	}
}
