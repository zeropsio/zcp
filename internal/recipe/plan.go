package recipe

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
type Codebase struct {
	Hostname           string `json:"hostname"`
	Role               Role   `json:"role"`
	BaseRuntime        string `json:"baseRuntime,omitempty"`
	IsWorker           bool   `json:"isWorker,omitempty"`
	SharesCodebaseWith string `json:"sharesCodebaseWith,omitempty"`
}

// Service is a managed or utility service in the recipe (database, cache,
// broker, object storage, search, mail, ...). Kind classifies the service
// for the yaml emitter's branches.
type Service struct {
	Kind        ServiceKind       `json:"kind"`
	Hostname    string            `json:"hostname"`
	Type        string            `json:"type"`
	Priority    int               `json:"priority,omitempty"`
	ExtraFields map[string]string `json:"extraFields,omitempty"`
}

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
