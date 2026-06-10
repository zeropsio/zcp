package workflow

import "github.com/zeropsio/zcp/internal/topology"

// PersistenceSurface names a durable surface the persistence sentinel can probe.
// Container FS is DELIBERATELY excluded — it is ephemeral by design, so a port
// must assert persistence on a managed DB / object-storage / shared-storage
// surface, never the container filesystem (C5 grades ephemeral-only survival as 1).
type PersistenceSurface string

const (
	// SurfaceManagedDB — a managed database (postgresql, clickhouse, …): write a
	// sentinel row, redeploy, re-read it.
	SurfaceManagedDB PersistenceSurface = "managed-db"
	// SurfaceObjectStorage — object-storage: put a sentinel object, redeploy, get it.
	SurfaceObjectStorage PersistenceSurface = "object-storage"
	// SurfaceSharedStorage — shared-storage volume: write a sentinel file, redeploy, read it.
	SurfaceSharedStorage PersistenceSurface = "shared-storage"
)

// PersistenceProbe is one sentinel the harden step plans to run against a durable
// dependency. The PLAN is pure (derived from the recon dep mapping); the agent
// executes it (write → redeploy → re-read) and reports the result back.
type PersistenceProbe struct {
	// Dependency is the managed dependency hostname/type the probe targets.
	Dependency string `json:"dependency"`
	// Surface is the durable surface class to assert on.
	Surface PersistenceSurface `json:"surface"`
	// Guidance is the agent-facing write→redeploy→re-read instruction.
	Guidance string `json:"guidance"`
}

// HardenPlan is the pure planning output of the harden step: which sentinels to
// run, the readiness/health injection guidance, and the HA scale-probe plan. It
// is derived from the recon PortPlan (the managed dep mapping). No ops calls.
type HardenPlan struct {
	// PersistenceProbes is the per-durable-dependency sentinel plan (C5 input).
	PersistenceProbes []PersistenceProbe `json:"persistenceProbes"`
	// ReadinessGuidance instructs the agent to inject readinessCheck/healthCheck.
	ReadinessGuidance string `json:"readinessGuidance"`
	// HAScaleProbe is the HA probe plan (C6 input).
	HAScaleProbe HAScaleProbe `json:"haScaleProbe"`
	// Notes carries any plan-level caveats (e.g. "no durable dep — C5 cannot reach 2").
	Notes []string `json:"notes,omitempty"`
}

// HAScaleProbe is the HA probe plan: scale the app runtime to ≥2 containers and
// flip every mode-bearing managed dep to HA mode, then verify. Keeps throughput
// (TargetAppContainers) distinct from the HA-replication assertion (HACandidates).
type HAScaleProbe struct {
	// TargetAppContainers is the app-runtime container count to scale to (≥2) for
	// the throughput axis of the probe.
	TargetAppContainers int `json:"targetAppContainers"`
	// HACandidates names the managed deps that SUPPORT a mode and so can be flipped
	// to HA (object-storage is excluded — always internally replicated, no mode).
	HACandidates []string `json:"haCandidates,omitempty"`
	// Guidance is the agent-facing scale + verify instruction.
	Guidance string `json:"guidance"`
}

// PlanHarden derives the pure harden plan from the recon PortPlan. It maps each
// managed dependency to the durable surface its sentinel should assert on
// (managed-db / object-storage / shared-storage — never container FS) and lists
// the mode-bearing deps as HA candidates. Pure + deterministic.
func PlanHarden(plan PortPlan) HardenPlan {
	hp := HardenPlan{
		ReadinessGuidance: "Inject a readinessCheck (and a healthCheck where the app exposes one) into run.* of the glue zerops.yaml so the platform gates traffic on the app being ready — this stabilizes C2 boots-stable and is a prerequisite for a meaningful HA probe.",
	}

	var haCandidates []string
	for _, dep := range plan.Dependencies {
		if dep.Mapping != DepMappingManaged || dep.ManagedType == "" {
			continue
		}
		surface, ok := durableSurfaceFor(dep.ManagedType)
		if !ok {
			continue
		}
		hp.PersistenceProbes = append(hp.PersistenceProbes, PersistenceProbe{
			Dependency: dep.ManagedType,
			Surface:    surface,
			Guidance:   persistenceProbeGuidance(dep.ManagedType, surface),
		})
		// Mode-bearing managed deps are HA candidates (object-storage excluded —
		// it has no mode, it is always internally replicated).
		if topology.ServiceSupportsMode(dep.ManagedType) {
			haCandidates = append(haCandidates, dep.ManagedType)
		}
	}

	if len(hp.PersistenceProbes) == 0 {
		hp.Notes = append(hp.Notes, "no durable managed/object/shared-storage dependency in the topology — C5 persists-across-redeploy cannot reach grade 2 (the container FS is ephemeral by design); only assert persistence claims you can prove on a durable surface")
	}

	hp.HAScaleProbe = HAScaleProbe{
		TargetAppContainers: 2,
		HACandidates:        haCandidates,
		Guidance:            haScaleProbeGuidance(haCandidates),
	}
	return hp
}

// durableSurfaceFor maps a managed service-type to its durable persistence
// surface. Object-storage → object-storage; shared-storage → shared-storage;
// every other managed type (databases, caches, search, messaging) → managed-db.
// Returns ok=false for a non-managed type (should not happen for a managed dep,
// but keeps the function total).
func durableSurfaceFor(managedType string) (PersistenceSurface, bool) {
	switch {
	case topology.IsObjectStorageType(managedType):
		return SurfaceObjectStorage, true
	case topology.IsSharedStorageType(managedType):
		return SurfaceSharedStorage, true
	case topology.IsManagedService(managedType):
		return SurfaceManagedDB, true
	default:
		return "", false
	}
}

func persistenceProbeGuidance(dep string, surface PersistenceSurface) string {
	//exhaustive:ignore — managed-db is the default arm (databases/caches/search/messaging all share the row-sentinel guidance).
	switch surface {
	case SurfaceObjectStorage:
		return "C5 sentinel on " + dep + ": PUT a sentinel object via the app's storage path, redeploy the app, then GET it back. Persistence is proven only if the object survives the redeploy."
	case SurfaceSharedStorage:
		return "C5 sentinel on " + dep + ": write a sentinel file onto the shared-storage mount, redeploy the app, then read it back. The container FS is ephemeral — the claim only holds if the file lives on the shared-storage volume."
	default:
		return "C5 sentinel on " + dep + ": write a sentinel row through the app's data path, redeploy the app, then re-read it. Persistence is proven only if the row survives the redeploy (the managed DB outlives the container)."
	}
}

func haScaleProbeGuidance(candidates []string) string {
	base := "C6 HA probe: scale the app runtime to ≥2 containers (minContainers≥2) for the THROUGHPUT axis. "
	if len(candidates) == 0 {
		return base + "No mode-bearing managed dependency to flip to HA — C6 can reach grade 1 (throughput) but NOT grade 2 (HA replication), which requires managed deps in HA mode. Horizontal scaling for throughput is distinct from HA replication."
	}
	return base + "Then flip the mode-bearing managed deps to HA mode and re-verify. HA REPLICATION (grade 2) requires BOTH ≥2 app containers AND the managed deps in HA mode AND a passing HA verify — distinct from throughput-only scaling (grade 1)."
}

// HardenResults carries the agent-reported (mocked in tests) outcomes of running
// the harden plan, fed into GradeHarden to produce the C5 + C6 grades. The pure
// grader does NOT run anything — it grades the reported results.
type HardenResults struct {
	// SentinelSurvivedRedeploy is true when at least one persistence sentinel
	// survived a redeploy.
	SentinelSurvivedRedeploy bool `json:"sentinelSurvivedRedeploy"`
	// SentinelOnDurableSurface is true when the surviving sentinel was on a
	// durable managed/object/shared-storage surface (not the ephemeral container FS).
	SentinelOnDurableSurface bool `json:"sentinelOnDurableSurface"`
	// AppContainers is the app-runtime container count the HA probe reached.
	AppContainers int `json:"appContainers"`
	// ManagedDepsHA is true when every mode-bearing managed dep reached HA mode.
	ManagedDepsHA bool `json:"managedDepsHa"`
	// HAVerifyPassed is true when the post-scale HA verify passed.
	HAVerifyPassed bool `json:"haVerifyPassed"`
}

// GradeHarden grades the harden results into the C5 + C6 rubric grades. Pure:
// takes the injected (mocked) results, returns the two grades. No ops calls.
func GradeHarden(res HardenResults) (c5, c6 PortGrade) {
	c5 = C5Persists(res.SentinelSurvivedRedeploy, res.SentinelOnDurableSurface)
	c6 = C6HACapable(res.AppContainers, res.ManagedDepsHA, res.HAVerifyPassed)
	return c5, c6
}
