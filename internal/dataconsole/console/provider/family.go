package provider

import "strings"

// Classify maps a Zerops service type string (e.g. "postgresql:single@18",
// "object-storage", "valkey@7") to its data-shape Family. This is the §2
// taxonomy. The zcp adapter reuses this owner to choose the typed connection
// descriptor; it does not carry a second family/base-type parser. A type the
// platform adds that we have not classified falls to FamilyUnknown and is
// surfaced as SupportNotYet, never mis-rendered.
func Classify(serviceType string) Family {
	base := BaseType(serviceType)
	switch base {
	case "postgresql", "mariadb", "mysql", "clickhouse":
		return FamilyTabular
	case "valkey", "keydb":
		return FamilyKV
	case "object-storage":
		return FamilyObject
	case "elasticsearch", "meilisearch", "typesense", "qdrant":
		return FamilyDocument
	case "kafka", "nats", "rabbitmq":
		return FamilyStream
	case "shared-storage":
		return FamilyFile
	default:
		return FamilyUnknown
	}
}

// SupportFor returns how fully v1 supports a family: object/kv/most-tabular are
// full; clickhouse is view-only (async mutations); document/stream/file are not
// yet. The v1 service scope (PRD §5) is encoded here so the UI labels honestly.
func SupportFor(serviceType string) Support {
	switch BaseType(serviceType) {
	case "object-storage", "postgresql", "mariadb", "mysql", "valkey",
		"elasticsearch", "meilisearch", "typesense":
		return SupportFull
	case "clickhouse", "qdrant", "kafka", "nats":
		// clickhouse: async mutations; qdrant: vectors not human-editable;
		// kafka/nats: a message log is not an editable dataset — all view-only.
		return SupportViewOnly
	default:
		// shared-storage (mount-only, no network endpoint) + anything new.
		return SupportNotYet
	}
}

// DerivedCaps is the coarse runtime capability profile, derived from the
// classification (family + support) + the session write posture — WITHOUT a live
// provider connection. The operation-level UI contract is ServiceActions; this
// shape remains for provider Caps() compatibility and runtime refinements such
// as MaxInlineBytes and per-provider read-only enforcement.
//
// The invariant: ReadOnly ⟺ no edit/upload flag set. Writes require the
// --allow-writes posture AND a fully-supported family; a view-only/not-yet
// family is read-only regardless of the launch flag.
func DerivedCaps(fam Family, sup Support, allowWrites bool) Capabilities {
	rw := allowWrites && sup == SupportFull
	c := Capabilities{Family: fam, Support: sup, ReadOnly: !rw}
	switch fam {
	case FamilyObject:
		c.EditBlob = rw
		c.Upload = rw
	case FamilyTabular:
		c.Query = true // read-only query is always available (engine-enforced READ ONLY tx)
		c.EditTabular = rw
	case FamilyKV:
		c.EditBlob = rw    // string values
		c.EditTabular = rw // collection entries (hash/list/set/zset)
		c.TTL = true       // TTL is shown read-only; set/clear requires rw (handler-gated)
	case FamilyDocument:
		c.EditBlob = rw // documents are JSON blobs by id
	case FamilyStream:
		// messaging is view-only by nature — never writable.
		c.ReadOnly = true
	case FamilyFile, FamilyUnknown:
		// no network endpoint / unclassified — read-only, no affordances (the
		// zero value already; listed for exhaustiveness).
		c.ReadOnly = true
	}
	return c
}

// BaseType strips the mode/version decoration: "postgresql:single@18" →
// "postgresql", "valkey@7" → "valkey", "object-storage" → "object-storage".
func BaseType(serviceType string) string {
	s := strings.ToLower(strings.TrimSpace(serviceType))
	if i := strings.IndexAny(s, ":@"); i >= 0 {
		s = s[:i]
	}
	return s
}
