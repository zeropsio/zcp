package topology

import "strings"

// Canonical hyphenated storage-kind names (the form ZCP validates against).
const (
	kindObjectStorage = "object-storage"
	kindSharedStorage = "shared-storage"
)

// managedServicePrefixes is the source of managed (non-runtime, non-storage)
// service classification — databases/caches/search/messaging. Topology owns
// CLASSIFICATION (the schema owns type EXISTENCE). A managed type missing from
// this list is misclassified as runtime AND its mode-encoded variants
// (`:single`/`:ha`) are rejected by the catalog — the live all-types audit
// (schema.TestLiveAllTypesAudit) is what catches that (the embedded coverage
// pins can't, since a missing managed type simply never enters ManagedBaseNames).
// Storage (object-storage / shared-storage incl. no-hyphen + seaweedfs) is
// classified separately via canonicalStorageKind. mysql IS a real managed DB on
// the live platform (mysql:ha@5.7 / mysql:single@5.7); mongodb/redis are not
// currently exposed as Zerops service types.
var managedServicePrefixes = []string{
	"postgresql", "mariadb", "mysql", "valkey",
	"keydb", "elasticsearch", "meilisearch", "rabbitmq", "kafka",
	"nats", "clickhouse", "qdrant", "typesense",
	kindObjectStorage, kindSharedStorage,
}

// IsManagedService checks if a service type is a managed (non-runtime) service.
// Storage (object-storage / shared-storage, incl. the no-hyphen and seaweedfs
// spellings the platform accepts) is recognized via canonicalStorageKind so a
// single alias set serves every storage predicate; databases/caches/search/
// messaging match the static prefix list.
func IsManagedService(serviceType string) bool {
	if canonicalStorageKind(serviceType) != "" {
		return true
	}
	lower := strings.ToLower(serviceType)
	for _, prefix := range managedServicePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// ManagedServicePrefixes returns a copy of the static managed-service prefix
// list. Exposed for callers that need to iterate the canonical set
// (e.g. coverage tests that pin every prefix has a kind mapping).
func ManagedServicePrefixes() []string {
	out := make([]string, len(managedServicePrefixes))
	copy(out, managedServicePrefixes)
	return out
}

// ServiceSupportsMode returns true if the service type supports mode: HA/NON_HA.
// Managed services (databases, caches, search, shared-storage, messaging) do.
// Object-storage does NOT — it's always internally replicated.
func ServiceSupportsMode(serviceType string) bool {
	return IsManagedService(serviceType) && !IsObjectStorageType(serviceType)
}

// ServiceSupportsAutoscaling returns true if the type supports verticalAutoscaling.
// All services except object-storage and shared-storage.
func ServiceSupportsAutoscaling(serviceType string) bool {
	return !IsObjectStorageType(serviceType) && !IsSharedStorageType(serviceType)
}

// IsObjectStorageType returns true for object-storage services.
// Object storage has no mode, no verticalAutoscaling — needs objectStorageSize instead.
func IsObjectStorageType(serviceType string) bool {
	return canonicalStorageKind(serviceType) == kindObjectStorage
}

// IsSharedStorageType returns true for shared-storage services.
// Shared storage supports mode but NOT verticalAutoscaling.
func IsSharedStorageType(serviceType string) bool {
	return canonicalStorageKind(serviceType) == kindSharedStorage
}

// IsUtilityType returns true for utility services deployed from external repos.
// These are standalone apps (ubuntu-based) with their own buildFromGit URL and
// their own zerops.yaml in a zerops-recipe-apps repo. Currently: mailpit.
func IsUtilityType(serviceType string) bool {
	return strings.HasPrefix(strings.ToLower(serviceType), "mailpit")
}

// IsRuntimeType returns true for Zerops runtime types — the categories where
// user code executes (php-nginx, nodejs, go, bun, python, rust, nginx, static, ...).
// Defined by exclusion: not managed and not a utility. Runtime services need
// zeropsSetup + buildFromGit pointing at the recipe-app repo.
func IsRuntimeType(serviceType string) bool {
	return !IsManagedService(serviceType) && !IsUtilityType(serviceType)
}

// IsPushSource returns true when the Mode value names a service that can
// act as the source of a git-push or zcli-push operation. Used by handler
// validation (handleGitPush rejects targetService whose mode is not a push
// source) and by atom rendering (push-source-only atoms filter on this
// predicate via the `modes:` axis).
//
// True for: ModeStandard (dev half of standard pair), ModeSimple (single
// container service), ModeLocalStage (local CWD as source paired with
// Zerops stage), ModeLocalOnly (local CWD with no Zerops link).
//
// False for: ModeStage (build target, not source) and ModeDev (the legacy
// dev-only mode — invalid combo with push-git per the deploy-strategy
// decomposition).
//
// Spec: `plans/deploy-strategy-decomposition-2026-04-28.md` §3.2.
func IsPushSource(m Mode) bool {
	switch m {
	case ModeStandard, ModeSimple, ModeLocalStage, ModeLocalOnly:
		return true
	case ModeDev, ModeStage:
		return false
	}
	return false
}

// PushSourceResult discriminates the four reasons a hostname may or may not
// be a valid push-source within a ServiceMeta's scope. The plain boolean
// `IsPushSource` (and ServiceMeta.IsPushSourceFor before its replacement)
// collapsed these cases onto a single false, leading handlers to render
// generic "not a source-of-push (build target half of the pair)" messages
// that nonsensically pointed users back at themselves when the real reason
// was an unsupported mode (e.g. standalone ModeDev). Each result carries a
// distinct remediation path:
//
//   - PushSourceOK — proceed; the hostname can push.
//   - PushSourceIsStageHalf — push from the paired dev hostname instead.
//   - PushSourceModeUnsupported — the service's Mode (ModeDev standalone
//     or ModeStage standalone) does not support push-git; the user must
//     run mode-expansion before push-git becomes available.
//   - PushSourceUnknownHost — the hostname is not part of this meta's
//     pair scope at all; the meta lookup matched a different service.
type PushSourceResult int

const (
	PushSourceOK              PushSourceResult = iota // valid push source
	PushSourceIsStageHalf                             // hostname is the stage half — push from dev half
	PushSourceModeUnsupported                         // Mode rejects push-git (ModeDev / ModeStage standalone)
	PushSourceUnknownHost                             // hostname is unrelated to this meta's pair scope
)
