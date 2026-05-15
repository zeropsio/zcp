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

// BundleInputs feeds composition for the export variants
// (VariantExportDev / VariantExportStage). Mirrors the live state
// upper-layer handlers probe via Discover + SSH + git remote reads.
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
	// ManagedServices lists managed deps the bundle must re-import.
	ManagedServices []ManagedServiceEntry
}

// LaunchBundleInputs feeds composition for the launch variants
// (VariantLaunchNew / VariantLaunchExisting). Superset of BundleInputs
// fields plus prod-specific knobs (HA opt-out, source-snapshot
// inputs, audit-log fields).
//
// Variant selects launch shape — zero (VariantExportDev) is invalid
// for this struct and is normalized to VariantLaunchNew at compose
// time. Set explicitly to VariantLaunchExisting for the existing-
// project mutation path which calls PostProjectServiceStackImport
// (rejects yaml carrying a project: block).
type LaunchBundleInputs struct {
	// SourceProjectID — recorded on the bundle for audit.
	SourceProjectID string
	// TargetProjectName — what the new prod project will be named.
	TargetProjectName string
	// TargetHostname — the runtime hostname in the launch yaml.
	TargetHostname string
	// ServiceType — runtime type tag (e.g. "nodejs@22").
	ServiceType string
	// SetupName — the zerops.yaml setup-block name the runtime
	// resolves at build. Default "prod".
	SetupName string
	// RepoURL — buildFromGit URL pointing at the source repo.
	RepoURL string
	// ZeropsYAMLBody — verbatim source zerops.yaml body. Composer
	// hashes it into SourceSnapshot.ZeropsYAMLSHA256.
	ZeropsYAMLBody string
	// GitCommitSHA — current HEAD of the source repo. Captured into
	// SourceSnapshot.GitCommitSHA.
	GitCommitSHA string
	// ProjectEnvs — source project-level env snapshot for classification.
	ProjectEnvs []ProjectEnvVar
	// ManagedServices — managed dep entries in source. Bundle
	// promotes each to HA unless its Hostname is in KeepNonHA.
	ManagedServices []ManagedServiceEntry
	// KeepNonHA — opt-out: managed service hostnames the user
	// explicitly wants to stay NON_HA in prod.
	KeepNonHA []string
	// MinContainers — runtime min count. Default
	// runtimeProductionMinContainers (2).
	MinContainers int
	// AdditionalTags — appended to canonical launch tags.
	AdditionalTags []string
	// Variant selects between launch-new (full project block — feeds
	// PostClientProjectImport) and launch-existing (services-only yaml
	// — feeds PostProjectServiceStackImport, which rejects project
	// blocks). Zero value (VariantExportDev) is invalid for launch
	// inputs; BuildLaunch normalizes to VariantLaunchNew.
	Variant Variant
}
