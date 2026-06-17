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
type ServiceTypeRules struct {
	AcceptsMode               bool
	RequiresObjectStorageSize bool
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
		return ServiceTypeRules{AcceptsMode: false, RequiresObjectStorageSize: true}
	case topology.IsSharedStorageType(serviceType):
		return ServiceTypeRules{AcceptsMode: false, RequiresObjectStorageSize: false}
	default:
		return ServiceTypeRules{AcceptsMode: true, RequiresObjectStorageSize: false}
	}
}

// ResolvedManagedType is the SINGLE owner of the authoritative (type, mode)
// a managed-service entry composes to. Both the import-yaml composer
// (managedEntryWithRules) and the agent-facing launch consent preview read it,
// so the preview cannot drift from what the bundle actually emits — the
// `type:postgresql@16 + mode:HA` self-contradiction the preview used to
// hand-pair was a tell≠emit drift with no single source.
//
// launchPromote=true (launch only) HA-promotes via the type VARIANT
// (`postgresql:ha@16`) unless keepNonHA opts the dep out (`postgresql:single@16`)
// — the modern authoritative form. modeField is non-empty ONLY as the
// bare-legacy fallback: a type with no variant carrying a source `mode:`
// (export of a pre-variant source). A variant type self-describes, so it never
// gets a sibling mode. Types that don't accept a mode (object/shared storage)
// never get one either.
func ResolvedManagedType(serviceType, sourceMode string, launchPromote, keepNonHA bool) (typeStr, modeField string) {
	rules := RulesForType(serviceType)
	typeStr = serviceType
	if rules.AcceptsMode && launchPromote {
		typeStr = topology.WithDeploymentVariant(serviceType, topology.VariantForHA(!keepNonHA))
	}
	if rules.AcceptsMode && !topology.HasDeploymentVariant(typeStr) && sourceMode != "" {
		modeField = sourceMode
	}
	return typeStr, modeField
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
// `launchPromote` is launch-only: when true (and rules permit a variant),
// the entry HA-promotes unless `keepNonHA` opts out — this is the launch
// composer's per-managed-service HA upgrade behavior. The HA-ness is encoded
// in the type VARIANT (`postgresql:ha@18`), the modern authoritative form, NOT
// a sibling `mode:` field: discovery now returns a `:single` type that would
// silently win over a `mode: HA` and defeat the promotion. Export passes
// launchPromote=false and keeps the source type (already the live variant) +
// its live profile verbatim (R7 identity snapshot).
func managedEntryWithRules(m ManagedServiceEntry, launchPromote, keepNonHA bool) map[string]any {
	finalType, modeField := ResolvedManagedType(m.Type, m.Mode, launchPromote, keepNonHA)
	rules := RulesForType(m.Type)
	entry := map[string]any{
		"hostname": m.Hostname,
		"type":     finalType,
		"priority": 10,
	}
	if modeField != "" {
		entry["mode"] = modeField
	}
	if prof := managedProfile(finalType, m.Profile, launchPromote); prof != "" {
		entry["profile"] = prof
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

// managedProfile resolves the scaling-tier `profile` value for a managed
// entry. Non-profile-bearing types (everything but PostgreSQL/Valkey) get
// none. Launch applies the recommended production baseline (`staging` tier —
// PostgreSQL `oltp-staging`, Valkey `staging`; higher tiers are an operator
// escalation, never a silent default); the per-family name form is owned by
// topology.ScalingProfileName. Export carries the live source profile verbatim
// (identity snapshot); an empty source profile emits nothing and the platform
// applies its own default on re-import.
func managedProfile(serviceType, sourceProfile string, launchPromote bool) string {
	if launchPromote {
		return topology.ScalingProfileName(serviceType, "staging")
	}
	if !topology.IsProfileBearing(serviceType) {
		return ""
	}
	return sourceProfile
}
