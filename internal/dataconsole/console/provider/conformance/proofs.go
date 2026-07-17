package conformance

import "github.com/zeropsio/zcp/internal/dataconsole/console/provider"

// ProofID names one semantic, live-conformance-proven fact about a managed-
// service base type — the declared unit the coverage lint (coverage_test.go)
// checks against RequiredProofs (docs/spec-dataconsole-testing.md §4). Two
// clusters, per the spec:
//
//   - Action-cluster proofs: enabled ServiceActions map onto these at the
//     smallest independent backend failure mode. Several UI actions may share
//     one live proof (e.g. object's upload bar and PUT-blob route both bottom
//     out in the same WriteBlob call, so they share ProofWriteRoundtrip).
//   - Engine-contract proofs: facts an action alone cannot express — a task
//     queue's real completion, a vector payload's shape, a value crossing the
//     wire exactly, an error condition matching across sibling engines.
type ProofID string

const (
	// ProofBrowseTree proves List walks the family's tree (schemas/tables,
	// keyspace, prefixes, indices/collections, topics/streams) and returns
	// real nodes — every family, every support tier (even view-only browses).
	ProofBrowseTree ProofID = "browse_tree"
	// ProofReadValue proves a leaf reads back real content (ReadTable/
	// ReadBlob) — every family, every support tier.
	ProofReadValue ProofID = "read_value"
	// ProofWriteRoundtrip proves a write is APPLIED, not merely accepted: an
	// immediate authoritative read after the write returns it (spec-
	// dataconsole.md §7.1 I-1). Full-tier families only.
	ProofWriteRoundtrip ProofID = "write_roundtrip"
	// ProofInsertKeyEcho proves InsertRow's Applied.Key echoes the new row's
	// primary key (RETURNING / LastInsertId / caller-supplied verbatim) per
	// §7.3 point 5 — tabular full only (the only family with generated-PK
	// insert).
	ProofInsertKeyEcho ProofID = "insert_key_echo"
	// ProofDelete proves a delete both succeeds and is applied (a subsequent
	// read confirms absence) — full-tier families with a delete affordance.
	ProofDelete ProofID = "delete"
	// ProofRename proves object's copy-then-remove Rename: the old path is
	// gone, the new path holds the same bytes — object full only.
	ProofRename ProofID = "rename"
	// ProofUpload proves the multipart upload bar's success path. It shares
	// the provider-level WriteBlob call the PUT-blob route also uses (the
	// server decodes the multipart part, then calls the same WriteBlob), so
	// it is satisfied by the same case that proves ProofWriteRoundtrip for
	// object — object full only.
	ProofUpload ProofID = "upload"
	// ProofCreateCollision proves a collision-refusing create (I-4): retrying
	// an explicit-name/id create against an existing name/id is ErrConflict,
	// never a clobber. KV collection-create and document create both carry
	// this; object has no create-collection (ProofFolderRefusal instead).
	ProofCreateCollision ProofID = "create_collision"
	// ProofTTL proves SetTTL's set/clear round-trips through a subsequent
	// Stat/ReadBlob's ttlSeconds — kv full only.
	ProofTTL ProofID = "ttl"
	// ProofQueryReadOnly proves the engine — not a statement-text whitelist —
	// refuses a write-shaped statement issued through the arbitrary-SQL query
	// path, while a plain read still succeeds. Tabular only, EVERY tier
	// (Query is always available per DerivedCaps, full and view-only alike).
	ProofQueryReadOnly ProofID = "query_read_only"
	// ProofSearch proves the bounded, read-only, ids-only document search
	// (/api/search, DD-2) actually matches seeded content — document full
	// only (qdrant's vector/payload search is deferred, view-only).
	ProofSearch ProofID = "search"
	// ProofMutationRefusal proves the "one live writes-armed mutation-refusal
	// cell" support-tier shape (§4): a view-only engine's provider refuses a
	// mutation with ErrReadOnly even when the CALLER requests armed writes —
	// the provider's own constructed posture wins, never the caller's intent.
	// Every view-only base type.
	ProofMutationRefusal ProofID = "mutation_refusal"
	// ProofTaskConfirmedWrite proves meilisearch's async task queue is polled
	// to a terminal status before a write reports success (I-1; DOC-AUD-01) —
	// meilisearch only.
	ProofTaskConfirmedWrite ProofID = "task_confirmed_write"
	// ProofVectorMeta proves a qdrant point read carries BlobMeta.Vector=true
	// so the SPA collapses the raw embedding behind a summary (UI-AUD-03) —
	// qdrant only.
	ProofVectorMeta ProofID = "vector_meta"
	// ProofValueFidelity proves §7.3's per-family wire-fidelity facts: tabular
	// bigint/int64 binds via json.Number→string (bindArg) preserving exact
	// decimal text past 2^53, and (bonus, postgres) a timestamp preserves
	// sub-second precision (RFC3339Nano); object's write carries the
	// declared content-type through to a subsequent Stat; kv's Stat/ReadBlob
	// surface "no expiry" as a nil ttlSeconds, never the literal 0.
	ProofValueFidelity ProofID = "value_fidelity"
	// ProofErrorParity proves the same condition maps to the same sentinel
	// across sibling engines / across an engine's own resources — here, a
	// missing table (postgresql/mariadb) is ErrNotFound, not a flat upstream
	// failure (§7.1 I-2). Full tabular only.
	ProofErrorParity ProofID = "error_parity"
	// ProofPagination proves a cursor-paged read (ReadTable) actually walks
	// multiple pages without dropping or duplicating rows. Full tabular only.
	ProofPagination ProofID = "pagination"
	// ProofStreamMetadata proves a topic/stream ReadBlob carries
	// BlobMeta.StreamMetadata=true so the SPA renders a labelled metadata
	// card, never message content (U-04, DD-3) — every stream base type.
	ProofStreamMetadata ProofID = "stream_metadata"
	// ProofWrongTypeGuard proves a KV WriteBlob (string) refuses to clobber an
	// existing collection value with ErrWrongType (KV-AUD-01) — kv full only.
	ProofWrongTypeGuard ProofID = "wrong_type_guard"
	// ProofFolderRefusal proves an object Delete/Rename targeting a
	// folder-shaped prefix (children present, no literal object at the key)
	// refuses clearly (ErrUnsupported), never a false success or a bare
	// not-found (D-07) — object full only.
	ProofFolderRefusal ProofID = "folder_refusal"
)

// familySupport is the (Family, Support) pair every base-type profile
// resolves to — the key the base required-proof set is derived from.
type familySupport struct {
	family  provider.Family
	support provider.Support
}

// baseRequiredProofs is the §4 derivation, encoded as data: every base type
// sharing a (Family, Support) cell shares this proof set, before any
// per-base-type extra (extraRequiredProofsByBaseType) is added. A cell absent
// from this map (including every NotYet cell) requires nothing — an honest
// not-yet/unknown profile is proven offline only (zero-affordance rendering),
// never at this live tier.
var baseRequiredProofs = map[familySupport][]ProofID{
	// tabular full (postgresql, mariadb; mysql via ProvenBy) — spec-
	// dataconsole.md §7.5 tabular: browse, arbitrary read-only SQL, cell/row
	// edit+insert(echo)+delete, value fidelity, a missing table is ErrNotFound.
	{provider.FamilyTabular, provider.SupportFull}: {
		ProofBrowseTree, ProofReadValue, ProofWriteRoundtrip, ProofInsertKeyEcho,
		ProofDelete, ProofQueryReadOnly, ProofValueFidelity, ProofPagination, ProofErrorParity,
	},
	// tabular view-only (clickhouse) — browse/read/query stay live (Query is
	// unconditional per DerivedCaps); every mutation is refused.
	{provider.FamilyTabular, provider.SupportViewOnly}: {
		ProofBrowseTree, ProofReadValue, ProofMutationRefusal, ProofQueryReadOnly,
	},
	// kv full (valkey) — §7.5 kv: SCAN tree, typed-command entries,
	// collision-refusing collection-create, WriteBlob never clobbers a
	// collection, no-TTL is the nil sentinel.
	{provider.FamilyKV, provider.SupportFull}: {
		ProofBrowseTree, ProofReadValue, ProofWriteRoundtrip, ProofDelete,
		ProofTTL, ProofCreateCollision, ProofWrongTypeGuard, ProofValueFidelity,
	},
	// object full (object-storage) — §7.5 object: prefix tree, read/edit/
	// upload/rename/delete, content-type carried on write, a folder delete/
	// rename is refused (no create-collection exists for this family).
	{provider.FamilyObject, provider.SupportFull}: {
		ProofBrowseTree, ProofReadValue, ProofWriteRoundtrip, ProofDelete,
		ProofUpload, ProofRename, ProofValueFidelity, ProofFolderRefusal,
	},
	// document full (elasticsearch, meilisearch, typesense) — §7.5 document:
	// index/collection browse, bounded read-only search, create+edit with
	// collision-refusing create.
	{provider.FamilyDocument, provider.SupportFull}: {
		ProofBrowseTree, ProofReadValue, ProofWriteRoundtrip, ProofDelete,
		ProofSearch, ProofCreateCollision,
	},
	// document view-only (qdrant) — points render as a collapsed-vector
	// summary; every mutation refuses.
	{provider.FamilyDocument, provider.SupportViewOnly}: {
		ProofBrowseTree, ProofReadValue, ProofMutationRefusal, ProofVectorMeta,
	},
	// stream view-only (kafka, nats) — a labelled metadata card, never
	// message content; every mutation refuses with the one ErrReadOnly signal.
	{provider.FamilyStream, provider.SupportViewOnly}: {
		ProofBrowseTree, ProofReadValue, ProofMutationRefusal, ProofStreamMetadata,
	},
}

// extraRequiredProofsByBaseType adds an engine-specific proof beyond the
// (Family, Support) base set, for the one base type in v1 whose engine
// carries a distinguishing contract no sibling in the same cell shares:
// meilisearch's async task queue (I-1). Keyed by BaseType, never by a second
// switch — a new distinguishing engine contract earns a new entry here, not
// a branch in RequiredProofs.
var extraRequiredProofsByBaseType = map[string][]ProofID{
	"meilisearch": {ProofTaskConfirmedWrite},
}

// RequiredProofs derives the proof set a registered profile must carry a
// live case for for, per docs/spec-dataconsole-testing.md §4: the base
// (Family, Support) set plus any base-type-specific extra. SupportNotYet
// (keydb, rabbitmq, shared-storage) requires nothing — proven offline only.
func RequiredProofs(p provider.ServiceProfile) []ProofID {
	if p.Support == provider.SupportNotYet {
		return nil
	}
	base := baseRequiredProofs[familySupport{p.Family, p.Support}]
	extra := extraRequiredProofsByBaseType[p.BaseType]
	out := make([]ProofID, 0, len(base)+len(extra))
	out = append(out, base...)
	out = append(out, extra...)
	return out
}

// CaseDecl declares that the live test func TestName proves Proof for every
// base type named in BaseTypes — one row per (test, proof); a test proving
// several distinct proofs (the common case: one comprehensive live case,
// several CaseDecl rows) repeats TestName across rows, never BaseTypes
// across proofs it doesn't actually assert.
type CaseDecl struct {
	TestName  string
	Proof     ProofID
	BaseTypes []string
}

// ConformanceCases is the package-level declared-proof registry: every live
// case in this package, mapped to the ProofID(s) and base types it proves.
// coverage_test.go's lint fails the build when a registered profile
// (provider.ServiceProfiles) has a RequiredProofs entry with no row here
// covering its base type (directly, or via ProvenBy — see coverage_test.go).
var ConformanceCases = []CaseDecl{
	// ---- tabular ----
	{TestName: "TestTabular_Smoke", Proof: ProofBrowseTree, BaseTypes: []string{"postgresql", "mariadb", "clickhouse"}},
	{TestName: "TestTabular_Smoke", Proof: ProofReadValue, BaseTypes: []string{"postgresql", "mariadb", "clickhouse"}},
	{TestName: "TestTabular_FullEngineWriteAndFidelity", Proof: ProofWriteRoundtrip, BaseTypes: []string{"postgresql", "mariadb"}},
	{TestName: "TestTabular_FullEngineWriteAndFidelity", Proof: ProofInsertKeyEcho, BaseTypes: []string{"postgresql", "mariadb"}},
	{TestName: "TestTabular_FullEngineWriteAndFidelity", Proof: ProofDelete, BaseTypes: []string{"postgresql", "mariadb"}},
	{TestName: "TestTabular_FullEngineWriteAndFidelity", Proof: ProofValueFidelity, BaseTypes: []string{"postgresql", "mariadb"}},
	{TestName: "TestTabular_FullEngineWriteAndFidelity", Proof: ProofPagination, BaseTypes: []string{"postgresql", "mariadb"}},
	{TestName: "TestTabular_FullEngineWriteAndFidelity", Proof: ProofErrorParity, BaseTypes: []string{"postgresql", "mariadb"}},
	{TestName: "TestTabular_QueryReadOnly", Proof: ProofQueryReadOnly, BaseTypes: []string{"postgresql", "mariadb", "clickhouse"}},
	{TestName: "TestTabular_MutationRefusal_ClickHouse", Proof: ProofMutationRefusal, BaseTypes: []string{"clickhouse"}},

	// ---- kv ----
	{TestName: "TestKV_Smoke", Proof: ProofBrowseTree, BaseTypes: []string{"valkey"}},
	{TestName: "TestKV_Smoke", Proof: ProofReadValue, BaseTypes: []string{"valkey"}},
	{TestName: "TestKV_WriteRoundtrip", Proof: ProofWriteRoundtrip, BaseTypes: []string{"valkey"}},
	{TestName: "TestKV_WriteRoundtrip", Proof: ProofDelete, BaseTypes: []string{"valkey"}},
	{TestName: "TestKV_WriteRoundtrip", Proof: ProofTTL, BaseTypes: []string{"valkey"}},
	{TestName: "TestKV_WriteRoundtrip", Proof: ProofCreateCollision, BaseTypes: []string{"valkey"}},
	{TestName: "TestKV_WriteRoundtrip", Proof: ProofWrongTypeGuard, BaseTypes: []string{"valkey"}},
	{TestName: "TestKV_WriteRoundtrip", Proof: ProofValueFidelity, BaseTypes: []string{"valkey"}},

	// ---- object ----
	{TestName: "TestObject_Conversions", Proof: ProofBrowseTree, BaseTypes: []string{"object-storage"}},
	{TestName: "TestObject_Conversions", Proof: ProofReadValue, BaseTypes: []string{"object-storage"}},
	{TestName: "TestObject_Conversions", Proof: ProofWriteRoundtrip, BaseTypes: []string{"object-storage"}},
	{TestName: "TestObject_Conversions", Proof: ProofDelete, BaseTypes: []string{"object-storage"}},
	{TestName: "TestObject_Conversions", Proof: ProofRename, BaseTypes: []string{"object-storage"}},
	{TestName: "TestObject_Conversions", Proof: ProofUpload, BaseTypes: []string{"object-storage"}},
	{TestName: "TestObject_Conversions", Proof: ProofValueFidelity, BaseTypes: []string{"object-storage"}},
	{TestName: "TestObject_FolderRefusal", Proof: ProofFolderRefusal, BaseTypes: []string{"object-storage"}},

	// ---- document ----
	{TestName: "TestDocument_Smoke", Proof: ProofBrowseTree, BaseTypes: []string{"elasticsearch", "meilisearch", "typesense", "qdrant"}},
	{TestName: "TestDocument_Smoke", Proof: ProofReadValue, BaseTypes: []string{"elasticsearch", "meilisearch", "typesense", "qdrant"}},
	{TestName: "TestDocument_WriteRoundtrip", Proof: ProofWriteRoundtrip, BaseTypes: []string{"elasticsearch", "meilisearch", "typesense"}},
	{TestName: "TestDocument_WriteRoundtrip", Proof: ProofDelete, BaseTypes: []string{"elasticsearch", "meilisearch", "typesense"}},
	{TestName: "TestDocument_WriteRoundtrip", Proof: ProofSearch, BaseTypes: []string{"elasticsearch", "meilisearch", "typesense"}},
	{TestName: "TestDocument_WriteRoundtrip", Proof: ProofCreateCollision, BaseTypes: []string{"elasticsearch", "meilisearch", "typesense"}},
	{TestName: "TestDocument_WriteRoundtrip", Proof: ProofTaskConfirmedWrite, BaseTypes: []string{"meilisearch"}},
	{TestName: "TestDocument_ViewOnlyQdrant", Proof: ProofMutationRefusal, BaseTypes: []string{"qdrant"}},
	{TestName: "TestDocument_ViewOnlyQdrant", Proof: ProofVectorMeta, BaseTypes: []string{"qdrant"}},

	// ---- stream ----
	{TestName: "TestStream_Conversions", Proof: ProofBrowseTree, BaseTypes: []string{"kafka", "nats"}},
	{TestName: "TestStream_Conversions", Proof: ProofReadValue, BaseTypes: []string{"kafka", "nats"}},
	{TestName: "TestStream_Conversions", Proof: ProofStreamMetadata, BaseTypes: []string{"kafka", "nats"}},
	{TestName: "TestStream_MutationRefusal", Proof: ProofMutationRefusal, BaseTypes: []string{"kafka", "nats"}},
}
