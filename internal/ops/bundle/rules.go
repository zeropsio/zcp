package bundle

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

// serviceTypeRulesByPrefix is a closed look-up table. Match strategy:
// the LONGEST prefix of the service type string. New types land here.
//
// Empty key "" is the default — applies when no prefix matches.
//
//nolint:gochecknoglobals // closed look-up table; not state.
var serviceTypeRulesByPrefix = map[string]ServiceTypeRules{
	"object-storage": {AcceptsMode: false, RequiresObjectStorageSize: true, AllowsBuildFromGit: false},
	"shared-storage": {AcceptsMode: false, RequiresObjectStorageSize: false, AllowsBuildFromGit: false},
	"":               {AcceptsMode: true, RequiresObjectStorageSize: false, AllowsBuildFromGit: true},
}

// RulesForType returns the ServiceTypeRules entry for a platform
// service-type tag (e.g. "object-storage", "postgresql@16",
// "nodejs@22"). Match is longest-prefix; falls back to the default
// row when no prefix matches.
func RulesForType(serviceType string) ServiceTypeRules {
	best := serviceTypeRulesByPrefix[""]
	bestLen := 0
	for prefix, rules := range serviceTypeRulesByPrefix {
		if prefix == "" {
			continue
		}
		if len(prefix) > bestLen && hasPrefix(serviceType, prefix) {
			best = rules
			bestLen = len(prefix)
		}
	}
	return best
}

// hasPrefix is a stdlib-free prefix check used by RulesForType. Tiny
// + inline so the package keeps zero non-stdlib deps beyond yaml +
// platform types it brings in elsewhere.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
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
