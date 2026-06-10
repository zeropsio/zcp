package workflow

import (
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// WorkflowPort is the registry workflow name for the OSS port workflow — a
// peer to develop on the deploy-debug ops layer. The port flow takes foreign
// self-hosted software (Strapi, PostHog), gets it running on Zerops by
// iterating against live deploys + logs (Stage A), then captures the working
// deployment as a shape:software recipe (Stage B). Phase 0 delivers Stage A0
// (recon) only.
const WorkflowPort = "port"

// AcquisitionStrategy names how the port flow ACQUIRES the OSS artifact onto a
// Zerops runtime. The ladder is source-build → prebuilt-binary → crane
// image-lift; the single true bail is software that needs Kubernetes *runtime*
// orchestration primitives (sidecars-as-pods, init-container ordering, service
// mesh) inexpressible as prepareCommands/initCommands. Image-only is IN-band
// via crane-lift (a run.prepareCommands `crane export` extracting the image
// filesystem onto a stock base runtime) — NOT a bail. Spec: plan §12.1.
type AcquisitionStrategy string

const (
	// AcquireSourceBuild — a buildable source repo exists; build it on Zerops.
	AcquireSourceBuild AcquisitionStrategy = "source-build"
	// AcquirePrebuiltBinary — only a prebuilt binary URL exists; download + run.
	AcquirePrebuiltBinary AcquisitionStrategy = "prebuilt-binary"
	// AcquireCraneImageLift — only a container image exists; crane-export its
	// filesystem onto a stock base runtime. IN-band, never a bail.
	AcquireCraneImageLift AcquisitionStrategy = "crane-image-lift"
	// AcquireBail — needs Kubernetes runtime orchestration; the one true bail.
	AcquireBail AcquisitionStrategy = "bail"
)

// AcquisitionHint is the agent-supplied signal about what artifact form the
// OSS ships in. Recon maps the hint to an AcquisitionStrategy deterministically;
// the agent does the off-platform research, recon does the classification.
type AcquisitionHint string

const (
	// AcquireHintSourceRepo — a buildable source repository exists.
	AcquireHintSourceRepo AcquisitionHint = "source-repo"
	// AcquireHintBinaryURL — only a prebuilt-binary download URL exists.
	AcquireHintBinaryURL AcquisitionHint = "binary-url"
	// AcquireHintImageOnly — only a container image exists (crane-lift path).
	AcquireHintImageOnly AcquisitionHint = "image-only"
	// AcquireHintK8sRuntime — needs Kubernetes runtime orchestration (bail).
	AcquireHintK8sRuntime AcquisitionHint = "k8s-runtime"
)

// FeasibilityBand is recon's deploy-difficulty ESTIMATE — not the ceiling. The
// loop measures the true FitCeiling in later phases; the band only sets the
// iteration budget and frames expectations.
//
//	EASY   — source/crane + every dep managed, no self-run, single runtime.
//	MEDIUM — exactly one self-run dependency (no managed equivalent).
//	HARD   — multiple self-run deps OR many runtimes (cross-service complexity).
//	bail   — needs Kubernetes runtime orchestration (recon refuses, no deploy).
type FeasibilityBand string

const (
	BandEasy   FeasibilityBand = "easy"
	BandMedium FeasibilityBand = "medium"
	BandHard   FeasibilityBand = "hard"
	BandBail   FeasibilityBand = "bail"
)

// DepMapping classifies how a declared dependency maps onto Zerops.
type DepMapping string

const (
	// DepMappingManaged — the dependency has a Zerops managed-service
	// equivalent (postgresql, clickhouse, kafka, valkey, object-storage, …).
	DepMappingManaged DepMapping = "managed"
	// DepMappingSelfRun — no managed equivalent; the dependency must be
	// self-run as an extra runtime service. Raises the band.
	DepMappingSelfRun DepMapping = "self-run"
	// DepMappingConstraint — the declared dependency could not be mapped at
	// all (empty / unrecognizable token). Surfaced as a recon constraint.
	DepMappingConstraint DepMapping = "constraint"
)

// PortDependency is one declared dependency's recon classification.
type PortDependency struct {
	// Declared is the agent-supplied dependency token (e.g. "postgresql",
	// "clickhouse", "cockroachdb").
	Declared string `json:"declared"`
	// Mapping classifies the dependency onto the Zerops managed catalog.
	Mapping DepMapping `json:"mapping"`
	// ManagedType is the resolved managed service-type token when
	// Mapping==DepMappingManaged (e.g. "postgresql@16"); empty otherwise.
	ManagedType string `json:"managedType,omitempty"`
}

// PortTargetDescriptor is the AGENT-PROVIDED input to recon. The agent
// researches the OSS off-platform and fills this in; recon CLASSIFIES it. No
// network, no deploy — recon never researches the software itself.
type PortTargetDescriptor struct {
	// Name is the OSS target identity (e.g. "strapi", "posthog").
	Name string `json:"name"`
	// AcquisitionHint signals what artifact form the OSS ships in.
	AcquisitionHint AcquisitionHint `json:"acquisitionHint"`
	// Dependencies are the declared dependency tokens (database / cache /
	// search / messaging / storage the software needs).
	Dependencies []string `json:"dependencies,omitempty"`
	// Runtimes are the declared runtime service-type tokens the software's
	// own processes run on (e.g. "nodejs@22", "python@3.12").
	Runtimes []string `json:"runtimes,omitempty"`
}

// PortPlan is recon's deterministic classification of a PortTargetDescriptor:
// target identity, acquisition strategy, dependency→managed-service mapping,
// feasibility band, and the candidate service topology (runtimes + managed
// deps). It is an ESTIMATE the loop refines, not the fit ceiling.
type PortPlan struct {
	// Target is the OSS identity (descriptor.Name).
	Target string `json:"target"`
	// Acquisition is the resolved acquisition strategy.
	Acquisition AcquisitionStrategy `json:"acquisition"`
	// Band is the feasibility estimate.
	Band FeasibilityBand `json:"band"`
	// Runtimes is the candidate runtime topology (the descriptor's runtimes,
	// preserved verbatim — they come from the agent's research, not from a
	// catalog enum, so recon does not validate them as managed/runtime here).
	Runtimes []string `json:"runtimes,omitempty"`
	// Dependencies is the per-dependency managed-mapping classification.
	Dependencies []PortDependency `json:"dependencies,omitempty"`
	// Constraints names recon-surfaced blockers that are not a hard bail but
	// the agent must know about (unmappable deps, the bail reason).
	Constraints []string `json:"constraints,omitempty"`
}

// ReconClassify turns an agent-provided target descriptor into a PortPlan. Pure
// and deterministic — given the same descriptor + catalog it always produces
// the same plan. No deploy, no network. The schema catalog answers managed-type
// EXISTENCE (schemas.HasServiceType / ManagedBaseNames, equivalence-aware) and
// topology predicates answer managed CLASSIFICATION (the schema owns existence,
// topology owns classification — the single-source-of-truth split from CLAUDE.md).
//
// schemas may be nil (the embedded-seeded cache makes that rare); a nil catalog
// means no managed type exists, so every dependency falls to self-run and the
// band rises accordingly — a conservative, never-panicking default.
func ReconClassify(desc PortTargetDescriptor, schemas *schema.Schemas) PortPlan {
	plan := PortPlan{
		Target:      desc.Name,
		Acquisition: acquisitionFromHint(desc.AcquisitionHint),
		Runtimes:    append([]string(nil), desc.Runtimes...),
	}

	// K8s-runtime-orchestration is the one true bail: refuse before any deploy.
	if plan.Acquisition == AcquireBail {
		plan.Band = BandBail
		plan.Constraints = append(plan.Constraints,
			"needs Kubernetes runtime orchestration (sidecars-as-pods / init-container ordering / service mesh) inexpressible as prepareCommands/initCommands — recon bails before deploy")
		// Still classify the deps so the bail report is informative.
		plan.Dependencies = classifyDependencies(desc.Dependencies, schemas)
		return plan
	}

	plan.Dependencies = classifyDependencies(desc.Dependencies, schemas)

	var selfRun, constraints int
	for _, d := range plan.Dependencies {
		switch d.Mapping {
		case DepMappingManaged:
			// Managed dep — no band penalty.
		case DepMappingSelfRun:
			selfRun++
		case DepMappingConstraint:
			constraints++
			plan.Constraints = append(plan.Constraints,
				"dependency "+d.Declared+" could not be mapped to a managed type or a runnable runtime — agent must resolve before deploy")
		}
	}

	plan.Band = deriveBand(len(plan.Runtimes), selfRun)
	return plan
}

// acquisitionFromHint maps the agent hint to a strategy. Unknown / empty hint
// defaults to source-build (the most capable, lowest-risk path) — the loop will
// surface a build failure if no source actually exists, which is in-band.
func acquisitionFromHint(hint AcquisitionHint) AcquisitionStrategy {
	switch hint {
	case AcquireHintBinaryURL:
		return AcquirePrebuiltBinary
	case AcquireHintImageOnly:
		return AcquireCraneImageLift
	case AcquireHintK8sRuntime:
		return AcquireBail
	case AcquireHintSourceRepo:
		return AcquireSourceBuild
	default:
		return AcquireSourceBuild
	}
}

// classifyDependencies maps each declared dependency to the managed catalog. A
// dep with a managed equivalent → DepMappingManaged (carrying the resolved
// type); a recognizable token with no managed equivalent → DepMappingSelfRun
// (raises the band); an empty / unusable token → DepMappingConstraint.
func classifyDependencies(deps []string, schemas *schema.Schemas) []PortDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]PortDependency, 0, len(deps))
	for _, raw := range deps {
		out = append(out, classifyDependency(raw, schemas))
	}
	return out
}

func classifyDependency(declared string, schemas *schema.Schemas) PortDependency {
	dep := PortDependency{Declared: declared}
	if declared == "" {
		dep.Mapping = DepMappingConstraint
		return dep
	}
	if mt := managedTypeFor(declared, schemas); mt != "" {
		dep.Mapping = DepMappingManaged
		dep.ManagedType = mt
		return dep
	}
	// Recognizable token, no managed equivalent → the agent can self-run it as
	// an extra runtime service. (A genuinely unusable token would be empty;
	// any non-empty token is at least self-runnable in principle.)
	dep.Mapping = DepMappingSelfRun
	return dep
}

// managedTypeFor resolves a declared dependency token to a concrete managed
// service-type the catalog accepts, or "" when none exists. It first tries the
// token verbatim (a versioned token like "postgresql@16" the agent supplied),
// then matches the bare base name against the catalog's managed base names so a
// bare "postgresql" / "clickhouse" / "kafka" resolves to whatever version the
// live schema carries. Both paths require topology to CLASSIFY the result as
// managed — the schema owns existence, topology owns classification.
func managedTypeFor(declared string, schemas *schema.Schemas) string {
	if schemas == nil {
		return ""
	}
	// Verbatim versioned token (e.g. "postgresql@16", "object-storage").
	if schemas.HasServiceType(declared) && topology.IsManagedService(declared) {
		return declared
	}
	// Bare base name → first catalog service-type whose base matches.
	base := topology.CanonicalBaseName(declared)
	if base == "" {
		return ""
	}
	if !schemas.ManagedBaseNames()[base] {
		return ""
	}
	if schemas.ImportYml == nil {
		return ""
	}
	for _, t := range schemas.ImportYml.ServiceTypes {
		if !topology.IsManagedService(t) {
			continue
		}
		if topology.CanonicalBaseName(t) == base {
			return t
		}
	}
	return ""
}

// deriveBand rolls up the recon estimate from runtime count + self-run count.
//
//	0 self-run, ≤1 runtime  → EASY
//	1 self-run, ≤1 runtime  → MEDIUM
//	≥2 self-run OR >1 runtime → HARD
//
// Many runtimes signal cross-service init ordering + bespoke-knowledge depth
// (the PostHog-class HARD frontier); multiple self-run services compound that.
func deriveBand(runtimes, selfRun int) FeasibilityBand {
	if selfRun >= 2 || runtimes > 1 {
		return BandHard
	}
	if selfRun == 1 {
		return BandMedium
	}
	return BandEasy
}
