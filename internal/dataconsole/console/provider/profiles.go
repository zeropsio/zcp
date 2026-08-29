package provider

// ServiceProfile is the canonical, enumerable record for one managed-service
// base type: its data-shape Family, its v1 support tier, and (when
// non-empty) an explicit ProvenBy equivalence. This is the §2 registry
// (docs/spec-dataconsole-testing.md §2) — the single owner of the service-
// type universe. Classify and SupportFor are pure derivations of it, never
// parallel switches.
type ServiceProfile struct {
	BaseType string
	Family   Family
	Support  Support
	// ProvenBy names another registered BaseType whose live-conformance proof
	// stands in for this one — an explicit, reviewed equivalence (owner
	// decision DD-B for mysql→mariadb), never an identity or version match.
	// Empty means this profile carries its own proof.
	ProvenBy string
}

// serviceProfiles is the single ordered enumeration of every managed service
// base type this package classifies (docs/spec-dataconsole.md §6 taxonomy
// table). A base type not listed here falls to FamilyUnknown/SupportNotYet
// (see Classify/SupportFor) — surfaced honestly, never mis-rendered.
var serviceProfiles = []ServiceProfile{
	{BaseType: "postgresql", Family: FamilyTabular, Support: SupportFull},
	{BaseType: "mariadb", Family: FamilyTabular, Support: SupportFull},
	{BaseType: "mysql", Family: FamilyTabular, Support: SupportFull, ProvenBy: "mariadb"},
	{BaseType: "clickhouse", Family: FamilyTabular, Support: SupportViewOnly},
	{BaseType: "valkey", Family: FamilyKV, Support: SupportFull},
	{BaseType: "keydb", Family: FamilyKV, Support: SupportNotYet}, // classified even though removed from the platform
	{BaseType: "object-storage", Family: FamilyObject, Support: SupportFull},
	{BaseType: "elasticsearch", Family: FamilyDocument, Support: SupportFull},
	{BaseType: "meilisearch", Family: FamilyDocument, Support: SupportFull},
	{BaseType: "typesense", Family: FamilyDocument, Support: SupportFull},
	{BaseType: "qdrant", Family: FamilyDocument, Support: SupportViewOnly}, // vectors not human-editable
	{BaseType: "kafka", Family: FamilyStream, Support: SupportViewOnly},
	{BaseType: "nats", Family: FamilyStream, Support: SupportViewOnly},
	{BaseType: "rabbitmq", Family: FamilyStream, Support: SupportNotYet},
	{BaseType: "shared-storage", Family: FamilyFile, Support: SupportNotYet}, // mount-only, no network endpoint
	{BaseType: "local-storage", Family: FamilyFile, Support: SupportNotYet},  // mount-only, no network endpoint
}

// serviceProfileIndex is the base-type → profile lookup, built once from
// serviceProfiles at package init and never mutated afterward.
var serviceProfileIndex = indexServiceProfiles(serviceProfiles)

func indexServiceProfiles(profiles []ServiceProfile) map[string]ServiceProfile {
	idx := make(map[string]ServiceProfile, len(profiles))
	for _, p := range profiles {
		idx[p.BaseType] = p
	}
	return idx
}

// ServiceProfiles returns a copy of the full ordered registry — the
// canonical enumerable set of managed service base types this package
// classifies (docs/spec-dataconsole-testing.md §2).
func ServiceProfiles() []ServiceProfile {
	out := make([]ServiceProfile, len(serviceProfiles))
	copy(out, serviceProfiles)
	return out
}
