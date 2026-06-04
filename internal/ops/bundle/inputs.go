package bundle

import "github.com/zeropsio/zcp/internal/topology"

// ProjectEnvVar is the bundle's view of a project-level env entry
// awaiting classification. Distinct from platform.ProjectEnvVar
// (which carries the SDK shape) — this struct is the composer's input
// contract. The classification map keyed by Key drives composition.
//
// Phase 2 will migrate composer to consume platform.ProjectEnvVar
// directly once the envclass layer mediates.
type ProjectEnvVar struct {
	Key   string
	Value string
}

// ManagedServiceEntry describes a managed dep to re-import alongside
// the runtime so cross-service refs (${db_*}, ${redis_*}, ...) resolve
// in the destination project. Hostname + Type + Mode mirror Discover
// output; envs + envSecrets are intentionally absent — the platform
// regenerates managed credentials on import.
//
// QuotaGBytes carries the source object-storage quota (GB, 1-100
// range) for service types where ServiceTypeRules.
// RequiresObjectStorageSize=true. Zero defaults to 1 at compose time
// (platform import minimum); upstream caller may probe the source
// service's quotaGBytes env to plumb a higher value through. Closes
// F21 (object-storage entries missed the required objectStorageSize
// field, causing projectImportMissingParameter rejection).
type ManagedServiceEntry struct {
	Hostname    string
	Type        string
	Mode        string // "HA" / "NON_HA" / "" (object-storage and similar)
	QuotaGBytes int    // populated for object-storage; 0 → composer defaults to 1
}

// Scaling is the live platform-resolved autoscaling shape of a source runtime,
// projected verbatim into the export import.yaml so a re-import reproduces the
// deployed scaling instead of silently reverting to platform defaults (R7). All
// fields are zero-meaningful — a zero is omitted from the emitted YAML. Mapped
// from platform.CustomAutoscaling by the ops layer (bundle stays platform-free).
type Scaling struct {
	MinContainers int
	MaxContainers int
	MinCPU        int
	MaxCPU        int
	MinRAM        float64
	MaxRAM        float64
	MinDisk       float64
	MaxDisk       float64
	// CPUMode is the live SHARED/DEDICATED setting. Carried so export reproduces
	// it (a DEDICATED service must not silently revert to SHARED on re-import)
	// and launch can warn when its DEDICATED prod policy overrides a SHARED source.
	CPUMode string
}

// BundleInputs feeds export-bundle composition. Mirrors the live state
// upper-layer handlers probe via Discover + SSH + git remote reads. The
// chosen runtime hostname (TargetHostname) + its SourceMode determine the
// packaged half — there is no separate export variant value.
type BundleInputs struct {
	// ProjectName is the source project's name — copied verbatim into
	// `project.name` so re-imports describe their lineage.
	ProjectName string
	// TargetHostname is the chosen runtime hostname (dev or stage half).
	TargetHostname string
	// SourceMode is the topology.Mode of the chosen runtime hostname.
	// Drives the import.yaml `mode:` mapping per export §3.3 (β).
	SourceMode topology.Mode
	// ServiceType is the runtime's platform type tag, e.g. "nodejs@22".
	ServiceType string
	// SubdomainEnabled mirrors Discover's per-service subdomainEnabled.
	// When true, the import.yaml runtime entry carries
	// `enableSubdomainAccess: true` (re-asserted from Discover per the
	// legacy atom's contract).
	SubdomainEnabled bool
	// SetupName names the `setup:` block in the bundled zerops.yaml the
	// runtime should resolve at build time.
	SetupName string
	// ZeropsYAMLBody is the verbatim source zerops.yaml content; the
	// composer reads it to detect cross-service references the agent
	// would otherwise miss.
	ZeropsYAMLBody string
	// RepoURL is the buildFromGit target — live `git remote get-url
	// origin` resolved by the handler. Empty value is rejected.
	RepoURL string
	// ProjectEnvs is the project-level envVariables snapshot. Each
	// entry is bucketed via the classifications map at compose time.
	ProjectEnvs []ProjectEnvVar
	// ServiceEnvs is the runtime's per-service USER-set env layer
	// (Type=SECRET from the slim service /env — what `zerops_env set
	// serviceHostname=X` writes). The platform stores these as user data;
	// buildFromGit does NOT rebuild them (they are not in zerops.yaml), so
	// dropping them silently lost the key on re-import (GAP0-1). Emitted on
	// the runtime entry as schema-correct `envSecrets`, bucketed via the
	// same classifications map (secret-safe default — an unclassified
	// SECRET emits REPLACE_ME, never the verbatim value).
	ServiceEnvs []ProjectEnvVar
	// ManagedServices lists managed deps the bundle must re-import.
	ManagedServices []ManagedServiceEntry
	// Scaling is the live autoscaling shape of the target runtime, projected
	// verbatim into the import.yaml (R7). Nil → the composer emits a warning
	// that the re-import will use platform defaults (never a silent drop).
	Scaling *Scaling
}

// LaunchRuntimeInput is the per-runtime payload BuildLaunch consumes
// to emit one `services[]` entry in the production import.yaml. The
// composer loops over LaunchBundleInputs.Runtimes and emits one
// runtimeEntry per LaunchRuntimeInput.
//
// All RepoURL values MUST come from ServiceMeta.RemoteURL of a meta
// whose GitPushState=configured (P-LP-10 invariant — enforced at the
// handler-side gate in internal/tools/launch_source_control_gate.go).
// The composer is a downstream consumer that trusts the gate-validated
// value and never reads live SSH for the URL embedded in the bundle.
type LaunchRuntimeInput struct {
	// ProdHostname is the hostname the production runtime gets in
	// import.yaml. Typically the source dev-half stripped of its mode
	// suffix (`appdev` → `app`, `workerstage` → `worker`); the handler
	// owns derivation and override semantics.
	ProdHostname string
	// ServiceType is the runtime's platform type tag (e.g. "nodejs@22").
	ServiceType string
	// SetupName is the `setup:` block name the runtime resolves at
	// build. Per-runtime so monorepo + multi-runtime projects can
	// reference different setup blocks under the same zerops.yaml,
	// AND separate-repo deployments can each reference their own
	// zerops.yaml's setup. Default "prod" when empty (BuildLaunch
	// applies the default).
	SetupName string
	// RepoURL is the buildFromGit value. Gate-validated; composer
	// rejects when empty.
	RepoURL string
	// GitCommitSHA — source HEAD at compose time. Per-runtime so
	// multi-runtime projects sharing a repo carry the same SHA while
	// separate-repo projects can carry different SHAs.
	GitCommitSHA string
	// ZeropsYAMLBody — verbatim source zerops.yaml content. Used by
	// the composer to detect cross-service env refs that need
	// preprocessor preamble; per-runtime so separate-repo runtimes
	// each contribute their own yaml.
	ZeropsYAMLBody string
	// ServiceEnvs — this runtime's per-service USER-set env layer
	// (Type=SECRET slim service /env). Emitted as `envSecrets` on the
	// runtime entry, bucketed via the bundle classifications map
	// (secret-safe default). See BundleInputs.ServiceEnvs (GAP0-1).
	ServiceEnvs []ProjectEnvVar
	// MinContainers — runtime min count. Default
	// runtimeProductionMinContainers (2) when zero.
	MinContainers int
	// Scaling is the live source autoscaling shape (R7). Launch projects it
	// then applies named production transforms (minContainers HA floor, cpuMode
	// DEDICATED), each surfaced as a bundle warning rather than a silent
	// override. Nil → the production policy floor is used without reflection.
	Scaling *Scaling
}

// LaunchBundleInputs feeds composition for the launch variants
// (VariantLaunchNew / VariantLaunchExisting). Carries the per-project
// scope (TargetProjectName, SourceProjectID, ProjectEnvs, ManagedServices,
// KeepNonHA, AdditionalTags, Variant) plus the per-runtime payloads
// in Runtimes — one entry per promoted runtime. The composer loops
// over Runtimes to emit N runtime services in import.yaml; ManagedServices
// is deduplicated by hostname so multiple runtimes sharing infra get one
// entry each.
//
// Variant selects launch shape — the zero value is VariantLaunchNew
// (BuildLaunch treats an unset Variant as launch-new). Set explicitly to
// VariantLaunchExisting for the existing-project mutation path which calls
// PostProjectServiceStackImport (rejects yaml carrying a project: block).
type LaunchBundleInputs struct {
	// SourceProjectID — recorded on the bundle for audit.
	SourceProjectID string
	// TargetProjectName — what the new prod project will be named.
	TargetProjectName string
	// Runtimes — per-runtime payloads (one services[] entry each).
	// Composer requires at least one. Per-runtime checks (RepoURL,
	// ServiceType, ZeropsYAMLBody, setup-block presence) live on the
	// individual LaunchRuntimeInput; bundle-level field validation
	// rejects empty Runtimes.
	Runtimes []LaunchRuntimeInput
	// ProjectEnvs — source project-level env snapshot for classification.
	ProjectEnvs []ProjectEnvVar
	// ManagedServices — managed dep entries in source. Bundle
	// promotes each to HA unless its Hostname is in KeepNonHA.
	// Composer deduplicates by hostname so multiple runtimes sharing
	// infra get one entry each.
	ManagedServices []ManagedServiceEntry
	// KeepNonHA — opt-out: managed service hostnames the user
	// explicitly wants to stay NON_HA in prod.
	KeepNonHA []string
	// AdditionalTags — appended to canonical launch tags.
	AdditionalTags []string
	// Variant selects between launch-new (full project block — feeds
	// PostClientProjectImport) and launch-existing (services-only yaml
	// — feeds PostProjectServiceStackImport, which rejects project
	// blocks). Zero value (VariantExportDev) is invalid for launch
	// inputs; BuildLaunch normalizes to VariantLaunchNew.
	Variant Variant
}
