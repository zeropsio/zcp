// Package provider defines the data-shape model and the family-specific
// provider contracts the Data Console engine drives. It imports ZERO zcp core
// packages (pinned by depguard `dataconsole-core-isolated` + the boundary test)
// so the whole console/ subtree lifts to its own repo with a `git mv`.
//
// The "one uniform browser" thesis holds for two-and-a-half shapes and breaks
// for the rest, so there is one thin Provider shell + family-specific contracts
// gated by Capabilities — never one interface forced onto every service type.
package provider

import (
	"context"
	"time"
)

// Family is the data-shape family a managed service maps to. The classifier
// (Classify) is console-owned domain knowledge — it is the taxonomy itself and
// moves with the engine on extraction, never the zcp adapter.
type Family string

const (
	FamilyTabular  Family = "tabular"  // postgresql, mariadb, mysql, clickhouse(view-only)
	FamilyKV       Family = "kv"       // valkey (keydb removed from the platform)
	FamilyObject   Family = "object"   // object-storage (S3)
	FamilyDocument Family = "document" // elasticsearch, meilisearch, typesense, qdrant
	FamilyStream   Family = "stream"   // kafka, nats — no provider (dropped)
	FamilyFile     Family = "file"     // shared-storage — no network endpoint (deferred)
	FamilyUnknown  Family = "unknown"
)

// Support is how fully the console supports a discovered service, surfaced
// honestly in the UI so "browser for ALL managed services" never overclaims.
type Support string

const (
	SupportFull     Support = "supported" // a family provider fully drives it
	SupportViewOnly Support = "view-only" // browse/query but no edit (e.g. ClickHouse)
	SupportNotYet   Support = "not yet"   // discovered, no provider yet
)

// Path is a service-agnostic address; Segments are interpreted by the owning
// provider (object: [prefix.../object]; tabular: [schema, table]; kv: [key]).
type Path struct {
	Service  string   `json:"service"` // the service hostname
	Segments []string `json:"segments"`
}

// NodeKind routes a tree node to the right UI surface.
type NodeKind string

const (
	KindContainer NodeKind = "container" // schema, bucket, prefix, db-index
	KindTabular   NodeKind = "tabular"   // table/view, redis collection
	KindBlob      NodeKind = "blob"      // object, redis string value
)

// Node is one entry in the unified tree.
type Node struct {
	Name        string    `json:"name"`
	Kind        NodeKind  `json:"kind"`
	Path        Path      `json:"path"`
	HasChildren bool      `json:"hasChildren"`
	Meta        *NodeMeta `json:"meta,omitempty"`
}

// NodeMeta is server-declared, discriminated presentation metadata for one
// tree node (DD-4) — the one canon every family's List/Stat populates from,
// replacing the untyped map[string]any every provider used to invent its own
// keys into. A provider sets ONLY the fields it actually knows; every other
// field stays nil/empty so it drops off the wire (omitempty) and the SPA
// renders exactly what the server can vouch for, never a guess.
//
//   - Size: object/blob byte size (object List/Stat, document Stat).
//   - Modified: object last-modified (object List).
//   - ContentType: blob MIME type (object Stat, document Stat).
//   - ETag: object version/identity hint — the family's only stable one
//     (object Stat; object-stream.md §3, U-03).
//   - EntryType: the redis TYPE reply (string/hash/list/set/zset) so the tree
//     can render a per-type glyph instead of collapsing every collection to
//     one look (kv List/Stat; UI-AUD-04/U-03).
//   - Count: container child count, ONLY where a provider already knows it
//     for free (no family populates this yet — every current container
//     listing would need an extra, non-cheap round trip to count children).
//   - TTLSeconds: kv key TTL: nil means "no expiry", NEVER the literal 0
//     (kv Stat; the S28 sentinel, KV-AUD-02).
type NodeMeta struct {
	Size        *int64     `json:"size,omitempty"`
	Modified    *time.Time `json:"modified,omitempty"`
	ContentType string     `json:"contentType,omitempty"`
	ETag        string     `json:"etag,omitempty"`
	EntryType   string     `json:"entryType,omitempty"`
	Count       *int64     `json:"count,omitempty"`
	TTLSeconds  *int64     `json:"ttlSeconds,omitempty"`
}

// Capabilities is what a provider can safely do; advertised, never assumed.
type Capabilities struct {
	Family         Family  `json:"family"`
	Support        Support `json:"support"`
	Query          bool    `json:"query"`
	EditBlob       bool    `json:"editBlob"`
	EditTabular    bool    `json:"editTabular"`
	Upload         bool    `json:"upload"`
	TTL            bool    `json:"ttl"`
	MaxInlineBytes int64   `json:"maxInlineBytes"`
	ReadOnly       bool    `json:"readOnly"` // effective write gate (from --allow-writes)
}

// Action is one operation-level affordance in /api/services. ReadOnly describes
// whether the action is safe/read-only by nature; mutating actions have
// ReadOnly=false and become Enabled only when support is full and the launch
// posture allows writes. Disabled actions carry a UI-presentable reason.
type Action struct {
	ID       ActionID `json:"id"`
	Enabled  bool     `json:"enabled"`
	ReadOnly bool     `json:"readOnly"`
	Reason   string   `json:"reason"`
}

// Page is cursor-based pagination; the cursor is opaque + provider-defined.
type Page struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit"`
}

// Column is one tabular column with its key role + server-declared
// editability (DD-4): Editable/Reason ride alongside Name/DataType/PK so the
// SPA renders per-column edit affordances from server truth, never client
// guessing (a page-level "is this table editable" flag cannot express "the
// key column is locked but the value column isn't"). Reason carries the
// human-readable "why not" whenever Editable is false, and stays empty
// otherwise.
type Column struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	PK       bool   `json:"pk"`
	Editable bool   `json:"editable"`
	Reason   string `json:"reason,omitempty"`
}

// ColumnEditability derives the one shared per-column editability rule every
// family's Column-producing code applies (resolution 6): a primary-key / row-
// identity column is never editable in v1 — editing it is a delete+recreate
// relational operation, out of scope for inline cell edit — so pk always
// yields Editable=false with a stated reason. This is the baseline only: a
// caller composes a family-specific override on top (e.g. a view-only tier,
// or a read-only result set like SQL Query) where the whole column set is
// non-editable regardless of PK-ness.
func ColumnEditability(pk bool) (editable bool, reason string) {
	if pk {
		return false, "primary key"
	}
	return true, ""
}

// TablePage is a window of tabular rows. RowKeyCols empty ⇒ the table is
// view-only (no safe way to address a row).
type TablePage struct {
	Columns    []Column `json:"columns"`
	Rows       [][]any  `json:"rows"`
	NextCursor string   `json:"nextCursor,omitempty"`
	RowKeyCols []string `json:"rowKeyCols"`
}

// BlobMeta describes a blob/value read; Truncated ⇒ a guarded head-slice
// (view-only). Vector ⇒ the payload is a vector-bearing document (qdrant),
// whose raw JSON embeds a full embedding array — the minimal server-side
// signal the SPA (S15) keys on to collapse it into a summary instead of
// rendering a wall of floats inline (UI-AUD-03).
//
// StreamMetadata ⇒ the blob is a generated topic/stream metadata summary
// (kafka/nats), NOT stored message content — the minimal server-side signal
// the SPA (S15) keys on to render a labelled "metadata, not messages" card and
// never an editable content view (U-04, DD-3). It is the honest discriminator
// the stream family lacked: without it the JSON summary is byte-
// indistinguishable from a real document and renders through the same editable
// blob branch. Every stream summary read carries this flag; message peek stays
// out of scope (DD-3). Server-serialized to the wire beside Vector/Truncated
// (server.go's X-DataConsole-* header set).
type BlobMeta struct {
	ContentType    string `json:"contentType"`
	Size           int64  `json:"size"`
	TTLSeconds     *int64 `json:"ttlSeconds,omitempty"`
	Truncated      bool   `json:"truncated"`
	Vector         bool   `json:"vector,omitempty"`
	StreamMetadata bool   `json:"streamMetadata,omitempty"`
}

// CellEdit is a single optimistic-concurrency cell update.
type CellEdit struct {
	Path        Path           `json:"path"`
	RowKey      map[string]any `json:"rowKey"`
	Column      string         `json:"column"`
	NewValue    any            `json:"newValue"`
	ExpectedOld any            `json:"expectedOld"`
}

// Applied reports a mutation's executed statement + affected count. Key, when
// non-nil, is the affected row's primary key (column name -> value) — set by
// InsertRow so a caller can address the just-inserted row for a follow-up
// edit/delete without a separate lookup (T-AUD-03). Left nil when the key
// cannot be determined honestly (e.g. a multi-column MySQL/MariaDB PK with no
// RETURNING support and no caller-supplied value).
type Applied struct {
	Statement string         `json:"statement"`
	Affected  int64          `json:"affected"`
	Key       map[string]any `json:"key,omitempty"`
}

// Provider is the shell every provider implements.
type Provider interface {
	Kind() string
	Caps() Capabilities
	Health(ctx context.Context) error
	Close() error
}

// ObjectProvider — blob-tree family (object-storage; later shared-storage).
// WriteBlob's contentType is the MIME type to store as object metadata
// (OBJ-AUD-01: a write that never sets one degrades every later read to
// "application/octet-stream", breaking preview/edit on the console's own
// round-trip) — callers pass "" only when genuinely unknown, and the server
// sniffs a fallback rather than send an empty type.
type ObjectProvider interface {
	Provider
	List(ctx context.Context, p Path, page Page) (nodes []Node, next string, err error)
	Stat(ctx context.Context, p Path) (Node, error)
	ReadBlob(ctx context.Context, p Path) ([]byte, BlobMeta, error)
	WriteBlob(ctx context.Context, p Path, data []byte, contentType string) error
	Delete(ctx context.Context, p Path) error
}

// TabularProvider — relational/columnar SQL. Query MUST run inside an
// engine-enforced READ ONLY transaction (never a statement-text whitelist).
type TabularProvider interface {
	Provider
	List(ctx context.Context, p Path, page Page) (nodes []Node, next string, err error)
	Stat(ctx context.Context, p Path) (Node, error)
	ReadTable(ctx context.Context, p Path, page Page) (TablePage, error)
	Query(ctx context.Context, stmt string, page Page) (TablePage, error)
	EditCell(ctx context.Context, e CellEdit) (Applied, error)
	InsertRow(ctx context.Context, p Path, row map[string]any) (Applied, error)
	DeleteRow(ctx context.Context, p Path, key map[string]any) (Applied, error)
}

// KVEntryEdit is one collection-entry mutation (hash field / list element / set
// or zset member). It carries the collection key (Path) + the entry address
// (Field: hash field, list index, set/zset member) + the new Value, and Score
// for a zset member. It shares the provider's command-allowlist safety: the
// provider only ever issues the one typed command the entry's collection type
// maps to (HSET/LSET/SADD+SREM/ZADD), never an arbitrary command.
type KVEntryEdit struct {
	Path  Path     `json:"path"`
	Field string   `json:"field"`
	Value []byte   `json:"value"`
	Score *float64 `json:"score"`
}

// KVCreate creates a NEW key of an explicit redis type in a single op (S17,
// KV-AUD-03: there is no other collection-creation path — SetEntry dispatches on
// a key's CURRENT type, so a nonexistent key is ErrUnsupported and a destroyed
// or deleted collection can never be recreated in-console). A name COLLISION is
// refused with ErrConflict — create never overwrites, preserving the KV-AUD-01
// clobber guard — and no initial TTL is set (persistent by default; TTL is a
// separate SetTTL op). Redis cannot represent an empty collection, so a
// collection type MUST carry its first entry in the same op; a string may be
// created empty. Field carries the identity of a hash field or zset member;
// Value carries the payload of a string / hash field / list element / set
// member; Score carries a zset member's score:
//
//   - "string": Value is the value (empty allowed).
//   - "hash":   Field is the field name (required), Value its value.
//   - "list":   Value is the sole initial element.
//   - "set":    Value is the sole initial member.
//   - "zset":   Field is the member (required), Score its score (required).
type KVCreate struct {
	Path  Path     `json:"path"`
	Type  string   `json:"type"`
	Field string   `json:"field,omitempty"`
	Value []byte   `json:"value,omitempty"`
	Score *float64 `json:"score,omitempty"`
}

// KVProvider — redis-shape. Mutations are command-allowlisted, never a denylist.
// WriteBlob sets a string value (shares the server's blob-write path with object).
// Its contentType parameter exists only for interface parity with
// ObjectProvider — a redis string has no MIME concept, so KV ignores it.
// SetEntry/DeleteEntry edit one collection entry (hash/list/set/zset), so the KV
// grid is editable, not just the string-value blob path. CreateKey adds a NEW
// key of a chosen type (collision-refusing — KVCreate).
type KVProvider interface {
	Provider
	List(ctx context.Context, p Path, page Page) (nodes []Node, next string, err error)
	Stat(ctx context.Context, p Path) (Node, error)
	ReadBlob(ctx context.Context, p Path) ([]byte, BlobMeta, error)
	ReadTable(ctx context.Context, p Path, page Page) (TablePage, error)
	WriteBlob(ctx context.Context, p Path, data []byte, contentType string) error
	SetTTL(ctx context.Context, p Path, seconds *int64) error
	SetEntry(ctx context.Context, e KVEntryEdit) (Applied, error)
	DeleteEntry(ctx context.Context, p Path, field string) (Applied, error)
	CreateKey(ctx context.Context, c KVCreate) (Applied, error)
	Delete(ctx context.Context, p Path) error
}
