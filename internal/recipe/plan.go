package recipe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Plan is the authoritative state of a recipe run. Phases mutate specific
// fields; handlers gate mutation through workflow transitions (see
// workflow.go). Plan is framework-agnostic — every framework-specific
// fact is carried in Codebases / Services / EnvComments, which the agent
// populates during research and scaffold phases.
type Plan struct {
	Slug           string                       `json:"slug"`
	Framework      string                       `json:"framework,omitempty"`
	Tier           string                       `json:"tier,omitempty"`
	Research       ResearchResult               `json:"research"`
	Codebases      []Codebase                   `json:"codebases,omitempty"`
	Services       []Service                    `json:"services,omitempty"`
	EnvComments    map[string]EnvComments       `json:"envComments,omitempty"`
	ProjectEnvVars map[string]map[string]string `json:"projectEnvVars,omitempty"`
	// Fragments carries in-phase-authored content keyed by fragment id
	// (for example "root/intro", "codebase/apidev/integration-guide").
	// Sub-agents record fragments via zerops_recipe action=record-fragment
	// at the moment they hold the densest context; the assembler reads
	// them out at finalize and splices them into the surface templates.
	// See docs/zcprecipator3/plans/run-8-readiness.md §2.A.4 for the id
	// taxonomy and append-vs-overwrite semantics.
	Fragments map[string]string `json:"fragments,omitempty"`
	// FeatureKinds records the showcase features the main agent plans
	// to implement at the feature phase (crud, cache-demo, queue-demo,
	// storage-upload, search-items, seed, scout-import). The feature
	// brief injects the execOnce key-shape concept atom when the list
	// includes any item that authors initCommands (seed, scout-import).
	FeatureKinds []string `json:"featureKinds,omitempty"`
}

// HasWorkerCodebase reports whether any codebase in the plan has
// IsWorker=true. Used by the feature-phase brief composer (run-22
// followup F-5) to gate the worker-shape teaching atom: only load
// `worker_subscription_shape.md` when the plan actually scaffolds a
// worker codebase. Predicate is a plain slice scan; cost is negligible
// vs. the readAtom that follows it on the gate's true branch.
func (p *Plan) HasWorkerCodebase() bool {
	if p == nil {
		return false
	}
	for _, cb := range p.Codebases {
		if cb.IsWorker {
			return true
		}
	}
	return false
}

// ResearchResult is the output of the research phase. All fields are
// framework-agnostic; strings are filled by the agent from framework
// knowledge + Zerops contracts.
type ResearchResult struct {
	CodebaseShape  string `json:"codebaseShape,omitempty"`
	NeedsAppSecret bool   `json:"needsAppSecret,omitempty"`
	AppSecretKey   string `json:"appSecretKey,omitempty"`
	Description    string `json:"description,omitempty"`
}

// Codebase is one deployable codebase within a recipe. Hostname is the
// Zerops service hostname; BaseRuntime is the Zerops runtime identifier
// (e.g. "nodejs@22"). Role determines platform obligations via roles.go.
//
// SourceRoot points at the scaffold-authored workspace directory for this
// codebase — the per-codebase zerops.yaml + README source live there. A2
// of run-8-readiness copies <SourceRoot>/zerops.yaml verbatim into the
// stitched apps-repo shape so inline comments the scaffold sub-agent
// authored survive byte-identical.
type Codebase struct {
	Hostname           string `json:"hostname"`
	Role               Role   `json:"role"`
	BaseRuntime        string `json:"baseRuntime,omitempty"`
	IsWorker           bool   `json:"isWorker,omitempty"`
	SharesCodebaseWith string `json:"sharesCodebaseWith,omitempty"`
	SourceRoot         string `json:"sourceRoot,omitempty"`
	// HasInitCommands records that this codebase's scaffold authors
	// `initCommands` in its zerops.yaml (migrations, seeds, search-index
	// bootstrap). Briefs use it to decide whether to inject the
	// execOnce key-shape concept atom — see briefs.go. Main agent sets
	// this at update-plan time before build-brief kind=scaffold.
	HasInitCommands bool `json:"hasInitCommands,omitempty"`
	// ConsumesServices lists the managed-service hostnames this
	// codebase references via `${<host>_*}` / `${<host>}` patterns in
	// the scaffold-authored zerops.yaml's run.envVariables. Engine-
	// populated by parseConsumedServicesFromYaml at scaffold completion;
	// codebase-content brief composer + recipe-context Services block
	// filter on this list so a frontend SPA doesn't see db/cache/broker
	// in its brief when it only consumes `${api_zeropsSubdomain}`.
	// Run-21 R2-3.
	ConsumesServices []string `json:"consumesServices,omitempty"`
}

// Service is a managed or utility service in the recipe (database, cache,
// broker, object storage, search, mail, ...). Kind classifies the service
// for the yaml emitter's branches.
type Service struct {
	Kind     ServiceKind `json:"kind"`
	Hostname string      `json:"hostname"`
	Type     string      `json:"type"`
	Priority int         `json:"priority,omitempty"`
	// SupportsHA reports whether the managed service family supports
	// HA mode on Zerops. Run-12 §Y3 — tier 5 emits HA uniformly across
	// managed services, but meilisearch (and a few others) are single-
	// node only; the emitter downgrades to NON_HA when SupportsHA=false.
	// Set during plan composition via managedServiceSupportsHA(svc.Type).
	SupportsHA  bool              `json:"supportsHa,omitempty"`
	ExtraFields map[string]string `json:"extraFields,omitempty"`
}

// managedServiceSupportsHA reports whether a managed service family
// supports HA mode on Zerops. Type strings include version (e.g.
// "postgresql@18"); only the family prefix matters. Conservative
// default for unknown families is false (NON_HA emit).
//
// TODO: extend the table when run-13+ recipes use new managed-service
// families.
func managedServiceSupportsHA(serviceType string) bool {
	family := serviceType
	if i := strings.IndexByte(serviceType, '@'); i > 0 {
		family = serviceType[:i]
	}
	switch family {
	case "postgresql", "valkey", "redis", serviceFamilyNATS, "rabbitmq", "elasticsearch":
		return true
	}
	return false
}

// serviceFamilyNATS is the canonical type-prefix for the NATS managed
// service family ("nats@<version>"). Centralised because several
// composers / validators short-circuit on it.
const serviceFamilyNATS = "nats"

// ServiceKind classifies a service for YAML emission branches. Runtime
// and utility services both have zeropsSetup + buildFromGit; managed
// services do not. Storage has size+policy fields.
type ServiceKind string

const (
	ServiceKindManaged ServiceKind = "managed" // db, cache, broker, search
	ServiceKindStorage ServiceKind = "storage" // object storage
	ServiceKindUtility ServiceKind = "utility" // mailpit etc.
)

// EnvComments holds writer-authored prose bound to one tier's
// import.yaml. Project is the project-level block comment; Service is a
// per-hostname map.
type EnvComments struct {
	Project string            `json:"project,omitempty"`
	Service map[string]string `json:"service,omitempty"`
}

// ParentRecipe is the structural data the chain resolver loads from a
// parent recipe's published tree. Showcase inherits from minimal; minimal
// has no parent. See plan §7.
type ParentRecipe struct {
	Slug       string                    `json:"slug"`
	Tier       string                    `json:"tier"`
	Codebases  map[string]ParentCodebase `json:"codebases,omitempty"`
	EnvImports map[string]string         `json:"envImports,omitempty"`
	Facts      []FactRecord              `json:"facts,omitempty"`
	SourceRoot string                    `json:"sourceRoot,omitempty"`
}

// ParentCodebase is the per-codebase slice of a parent recipe.
type ParentCodebase struct {
	README     string `json:"readme,omitempty"`
	ZeropsYAML string `json:"zeropsYaml,omitempty"`
	SourceRoot string `json:"sourceRoot,omitempty"`
}

// WritePlan persists the session's Plan to <outputRoot>/plan.json so the
// content-phase replay tooling can reconstruct the brief without walking
// session jsonl logs. Atomic via temp+rename to match facts.go's pattern.
// No-op when outputRoot is empty (in-memory tests construct sessions
// without an on-disk root).
func WritePlan(outputRoot string, plan *Plan) error {
	if outputRoot == "" || plan == nil {
		return nil
	}
	body, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	body = append(body, '\n')
	dst := filepath.Join(outputRoot, "plan.json")
	tmp, err := os.CreateTemp(outputRoot, "plan.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp plan: %w", err)
	}
	tmpPath := tmp.Name()
	if _, wErr := tmp.Write(body); wErr != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp plan: %w", wErr)
	}
	if cErr := tmp.Close(); cErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp plan: %w", cErr)
	}
	if rErr := os.Rename(tmpPath, dst); rErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp plan: %w", rErr)
	}
	return nil
}

// ReadPlan loads a Plan from <outputRoot>/plan.json. Used by replay
// tooling and tests that pin against on-disk plan artifacts.
func ReadPlan(outputRoot string) (*Plan, error) {
	body, err := os.ReadFile(filepath.Join(outputRoot, "plan.json"))
	if err != nil {
		return nil, fmt.Errorf("read plan: %w", err)
	}
	var p Plan
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}
	return &p, nil
}
