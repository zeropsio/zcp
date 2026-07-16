// Package provider defines the data-shape model and the family-specific
// provider contracts the Data Console engine drives. It imports ZERO zcp core
// packages (pinned by depguard `dataconsole-core-isolated` + the boundary test)
// so the whole console/ subtree lifts to its own repo with a `git mv`.
//
// The "one uniform browser" thesis holds for two-and-a-half shapes and breaks
// for the rest, so there is one thin Provider shell + family-specific contracts
// gated by Capabilities — never one interface forced onto every service type.
package provider

import "context"

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
	Name        string         `json:"name"`
	Kind        NodeKind       `json:"kind"`
	Path        Path           `json:"path"`
	HasChildren bool           `json:"hasChildren"`
	Meta        map[string]any `json:"meta,omitempty"`
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

// Column is one tabular column with its key role.
type Column struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	PK       bool   `json:"pk"`
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
// (view-only).
type BlobMeta struct {
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	TTLSeconds  *int64 `json:"ttlSeconds,omitempty"`
	Truncated   bool   `json:"truncated"`
}

// CellEdit is a single optimistic-concurrency cell update.
type CellEdit struct {
	Path        Path           `json:"path"`
	RowKey      map[string]any `json:"rowKey"`
	Column      string         `json:"column"`
	NewValue    any            `json:"newValue"`
	ExpectedOld any            `json:"expectedOld"`
}

// Applied reports a mutation's executed statement + affected count.
type Applied struct {
	Statement string `json:"statement"`
	Affected  int64  `json:"affected"`
}

// Provider is the shell every provider implements.
type Provider interface {
	Kind() string
	Caps() Capabilities
	Health(ctx context.Context) error
	Close() error
}

// ObjectProvider — blob-tree family (object-storage; later shared-storage).
type ObjectProvider interface {
	Provider
	List(ctx context.Context, p Path, page Page) (nodes []Node, next string, err error)
	Stat(ctx context.Context, p Path) (Node, error)
	ReadBlob(ctx context.Context, p Path) ([]byte, BlobMeta, error)
	WriteBlob(ctx context.Context, p Path, data []byte) error
	Delete(ctx context.Context, p Path) error
}

// TabularProvider — relational/columnar SQL. Query MUST run inside an
// engine-enforced READ ONLY transaction (never a statement-text whitelist).
type TabularProvider interface {
	Provider
	List(ctx context.Context, p Path, page Page) (nodes []Node, next string, err error)
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

// KVProvider — redis-shape. Mutations are command-allowlisted, never a denylist.
// WriteBlob sets a string value (shares the server's blob-write path with object).
// SetEntry/DeleteEntry edit one collection entry (hash/list/set/zset), so the KV
// grid is editable, not just the string-value blob path.
type KVProvider interface {
	Provider
	List(ctx context.Context, p Path, page Page) (nodes []Node, next string, err error)
	Stat(ctx context.Context, p Path) (Node, error)
	ReadBlob(ctx context.Context, p Path) ([]byte, BlobMeta, error)
	ReadTable(ctx context.Context, p Path, page Page) (TablePage, error)
	WriteBlob(ctx context.Context, p Path, data []byte) error
	SetTTL(ctx context.Context, p Path, seconds *int64) error
	SetEntry(ctx context.Context, e KVEntryEdit) (Applied, error)
	DeleteEntry(ctx context.Context, p Path, field string) (Applied, error)
	Delete(ctx context.Context, p Path) error
}
