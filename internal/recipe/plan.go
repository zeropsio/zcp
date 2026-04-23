package recipe

// Plan is the authoritative state of a recipe run. Phases mutate specific
// fields; handlers gate mutation through workflow transitions (see
// workflow.go). Plan is framework-agnostic — every framework-specific
// fact is carried in Codebases / Services / EnvComments, which the agent
// populates during research and scaffold phases.
type Plan struct {
	Slug      string // e.g. "nestjs-showcase"
	Framework string // e.g. "nestjs" — filled by agent during research
	Tier      string // "hello-world" | "minimal" | "showcase"
	Research  ResearchResult
	Codebases []Codebase
	Services  []Service
	// EnvComments holds per-tier prose authored by the writer. Key = "0".."5".
	EnvComments map[string]EnvComments
	// ProjectEnvVars holds per-tier project-level env vars (values are opaque
	// strings including ${...} interpolation markers). Key = "0".."5".
	ProjectEnvVars map[string]map[string]string
}

// ResearchResult is the output of the research phase. All fields are
// framework-agnostic; strings are filled by the agent from framework
// knowledge + Zerops contracts.
type ResearchResult struct {
	// CodebaseShape is one of "1", "2", "3" — number of scaffold dispatches.
	CodebaseShape  string
	NeedsAppSecret bool
	// AppSecretKey is the env-var name for a shared cross-container secret
	// (framework-chosen — e.g. APP_KEY / APP_SECRET / SECRET_KEY_BASE).
	AppSecretKey string
	Description  string
}

// Codebase is one deployable codebase within a recipe. Hostname is the
// Zerops service hostname; BaseRuntime is the Zerops runtime identifier
// (e.g. "nodejs@22"). Role determines platform obligations via roles.go.
type Codebase struct {
	Hostname           string
	Role               Role
	BaseRuntime        string
	IsWorker           bool
	SharesCodebaseWith string // hostname of the host codebase if shared-worker
}

// Service is a managed or utility service in the recipe (database, cache,
// broker, object storage, search, mail, ...). Kind classifies the service
// for the yaml emitter's branches.
type Service struct {
	Kind        ServiceKind
	Hostname    string // "db" | "cache" | "broker" | "storage" | "search"
	Type        string // Zerops type string, e.g. "postgresql@18"
	Priority    int    // 0 means default; emitter picks based on kind
	ExtraFields map[string]string
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
	Project string
	Service map[string]string
}

// ParentRecipe is the structural data the chain resolver loads from a
// parent recipe's published tree. Showcase inherits from minimal; minimal
// has no parent. See plan §7.
type ParentRecipe struct {
	Slug       string
	Tier       string                    // "minimal" | "hello-world"
	Codebases  map[string]ParentCodebase // key = hostname
	EnvImports map[string]string         // key = "0".."5", value = import.yaml content
	Facts      []FactRecord              // optional
	SourceRoot string                    // mount path where the parent's codebases live
}

// ParentCodebase is the per-codebase slice of a parent recipe.
type ParentCodebase struct {
	README     string
	ZeropsYAML string
	SourceRoot string // absolute path to the codebase root on disk
}
