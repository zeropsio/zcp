package bundle

import "github.com/zeropsio/zcp/internal/topology"

// ServiceTypeRules is the closed table describing per-service-type
// composition constraints. Variant logic consults this instead of
// branching on type strings, so a new managed-service type is added
// by registering a row here rather than threading conditionals through
// the composer.
//
// AcceptsMode reports whether the platform import endpoint accepts a
// `mode:` field on a service entry of this type. object-storage +
// shared-storage do NOT — emitting `mode:` triggers
// projectImportInvalidParameter (F20). Phase 2 will activate this
// signal; Phase 1b registers the table without consulting it yet.
//
// RequiresObjectStorageSize reports whether the entry must carry an
// `objectStorageSize` field (GB, 1-100 range). Currently true only
// for object-storage. F21 fix will start emitting the field.
//
// AllowsBuildFromGit reports whether a `buildFromGit` URL is allowed
// on the entry. Only runtime services accept it; managed deps do
// not. Phase 6b will additionally distinguish launch-prod (no
// buildFromGit, startWithoutCode) from launch-stage / export
// variants.
type ServiceTypeRules struct {
	AcceptsMode               bool
	RequiresObjectStorageSize bool
	AllowsBuildFromGit        bool
}

// RulesForType returns the ServiceTypeRules entry for a platform
// service-type tag (e.g. "object-storage", "seaweedfs@3",
// "postgresql@16", "nodejs@22"). Storage detection routes through the
// topology predicates so every accepted storage spelling (hyphenated,
// no-hyphen, seaweedfs, mode/version-suffixed) is classified identically
// — the live Discover that feeds export/launch can report any of them.
func RulesForType(serviceType string) ServiceTypeRules {
	switch {
	case topology.IsObjectStorageType(serviceType):
		return ServiceTypeRules{AcceptsMode: false, RequiresObjectStorageSize: true, AllowsBuildFromGit: false}
	case topology.IsSharedStorageType(serviceType):
		return ServiceTypeRules{AcceptsMode: false, RequiresObjectStorageSize: false, AllowsBuildFromGit: false}
	default:
		return ServiceTypeRules{AcceptsMode: true, RequiresObjectStorageSize: false, AllowsBuildFromGit: true}
	}
}

// managedEntryWithRules composes the per-managed-service yaml entry,
// consulting ServiceTypeRules for type-specific shape requirements:
//
//   - !AcceptsMode → mode field omitted regardless of caller intent
//     (F20 fix: platform import rejects ANY mode value on
//     object-storage / shared-storage with
//     projectImportInvalidParameter).
//   - RequiresObjectStorageSize → emit objectStorageSize, defaulting
//     to 1 (platform minimum) when m.QuotaGBytes is unset
//     (F21 fix: platform import rejects without the field with
//     projectImportMissingParameter).
//
// `launchPromote` is launch-only: when true (and rules permit mode),
// the entry HA-promotes unless `keepNonHA` opts out — this is the
// launch composer's per-managed-service HA upgrade behavior. Export
// passes launchPromote=false and propagates the source Mode verbatim
// when present.
func managedEntryWithRules(m ManagedServiceEntry, launchPromote, keepNonHA bool) map[string]any {
	rules := RulesForType(m.Type)
	entry := map[string]any{
		"hostname": m.Hostname,
		"type":     m.Type,
		"priority": 10,
	}
	if rules.AcceptsMode {
		switch {
		case launchPromote && keepNonHA:
			if m.Mode != "" {
				entry["mode"] = m.Mode
			} else {
				entry["mode"] = importModeNonHA
			}
		case launchPromote:
			entry["mode"] = importModeHA
		case m.Mode != "":
			entry["mode"] = m.Mode
		}
	}
	if rules.RequiresObjectStorageSize {
		size := m.QuotaGBytes
		if size <= 0 {
			size = 1
		}
		entry["objectStorageSize"] = size
	}
	return entry
}
