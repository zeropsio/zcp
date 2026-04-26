package recipe

import "strings"

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
	case "postgresql", "valkey", "redis", "nats", "rabbitmq", "elasticsearch":
		return true
	}
	return false
}

// Plan.Tier canonical values. The research-phase atom + gateTierServiceSet
// pin these to a fixed set; brief composers + validators that branch on
// tier should reference these constants rather than literal strings.
const (
	TierHelloWorld = "hello-world"
	TierMinimal    = "minimal"
	TierShowcase   = "showcase"
)

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
