// Package document is the search/vector "document" family provider —
// Elasticsearch, Meilisearch, Typesense and Qdrant. A document is a JSON blob
// addressed by id, so the family maps cleanly onto provider.ObjectProvider: the
// index/collection is a container (folder), each document is a blob leaf whose
// Name is its id. The Provider is a thin shell over a per-engine `engine`
// adapter; the engine speaks each service's REST dialect over net/http while the
// shell owns the Path→address mapping, the read-only write gate and the
// MaxInlineBytes head-slice. Errors are mapped to sanitized provider sentinels —
// the endpoint, the api-key and the raw response body NEVER leak into them.
//
// Qdrant is read-only in v1: its documents carry vectors that are not
// human-editable, so EditBlob is false and putDoc/deleteDoc reject regardless of
// the configured ReadOnly flag.
package document

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const (
	reqTimeout       = 20 * time.Second
	defaultLimit     = 200
	maxLimit         = 1000
	maxInlineDefault = 16 << 20 // 16 MiB
	// maxSearchQueryLen bounds the free-text search query (S16, DD-2): a
	// bounded query keeps the upstream request cheap and predictable; a longer
	// one is a client bug or abuse and is refused (ErrInvalid), never forwarded.
	maxSearchQueryLen = 512
)

// Config is the resolved document-engine connection, populated from the core
// typed descriptor at the composition root.
type Config struct {
	Engine   string // "elasticsearch" | "meilisearch" | "typesense" | "qdrant"
	BaseURL  string // e.g. "http://es:9200" (NO trailing slash)
	APIKey   string
	User     string // elasticsearch basic-auth user (e.g. "elastic"); empty for others
	ReadOnly bool
}

// engine is the per-service REST adapter. Each method speaks one engine's
// dialect and returns sanitized provider sentinels on failure.
type engine interface {
	containers(ctx context.Context) ([]string, error)                                                     // index/collection names
	docs(ctx context.Context, container, cursor string, limit int) (ids []string, next string, err error) // paged doc ids
	getDoc(ctx context.Context, container, id string) ([]byte, error)                                     // raw document JSON
	putDoc(ctx context.Context, container, id string, body []byte) error                                  // upsert
	deleteDoc(ctx context.Context, container, id string) error
	name() string
}

// searcher is the OPTIONAL bounded-search capability (S16, DD-2). es/meili/
// typesense implement it; qdrant does NOT (its search is a vector/payload
// filter, deferred), so a qdrant Provider.Search returns ErrUnsupported because
// the type assertion below fails — no no-op stub to keep in sync. Results are
// matching document ids only (never engine highlight/snippet markup), so no
// upstream HTML can reach the SPA (XSS-safe by construction).
type searcher interface {
	search(ctx context.Context, container, q, cursor string, limit int) (ids []string, next string, err error)
}

// docCreator is the OPTIONAL explicit-create capability (S17). es/meili/
// typesense implement it; qdrant is read-only, so Provider.CreateDoc gates on
// the ReadOnly capability BEFORE dispatching (like WriteBlob) and never reaches
// an engine that lacks the method.
type docCreator interface {
	createDoc(ctx context.Context, container, id string, body []byte) (assignedID string, err error)
}

// Provider browses + edits one document-engine service as a blob tree.
type Provider struct {
	eng    engine
	caps   provider.Capabilities
	vector bool // true only for qdrant: every point read embeds a raw vector[] the SPA must collapse (UI-AUD-03)
}

// New builds the provider for the configured engine.
func New(cfg Config) (*Provider, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("doc: %w: missing base url", provider.ErrInvalid)
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	client := &http.Client{Timeout: reqTimeout}

	var eng engine
	readOnly := cfg.ReadOnly
	vector := false
	switch strings.ToLower(strings.TrimSpace(cfg.Engine)) {
	case "elasticsearch":
		eng = newESEngine(base, cfg.User, cfg.APIKey, client)
	case "meilisearch":
		eng = newMeiliEngine(base, cfg.APIKey, client)
	case "typesense":
		eng = newTypesenseEngine(base, cfg.APIKey, client)
	case "qdrant":
		eng = newQdrantEngine(base, cfg.APIKey, client)
		readOnly = true // vectors are view-only in v1; payload-only edit is out of scope
		vector = true   // every point embeds a raw vector[] — ReadBlob signals it (UI-AUD-03)
	default:
		return nil, fmt.Errorf("doc: %w: unknown engine %q", provider.ErrInvalid, cfg.Engine)
	}

	// Support must not contradict the read-only gate: a forced-read-only engine
	// (qdrant — vectors aren't human-editable) is view-only, never "supported".
	// This keeps the provider's Caps() internally consistent with the family.go
	// classification (SupportFor("qdrant") == view-only).
	support := provider.SupportFull
	if readOnly {
		support = provider.SupportViewOnly
	}
	return &Provider{
		eng:    eng,
		vector: vector,
		caps: provider.Capabilities{
			Family:         provider.FamilyDocument,
			Support:        support,
			EditBlob:       !readOnly,
			MaxInlineBytes: maxInlineDefault,
			ReadOnly:       readOnly,
		},
	}, nil
}

func (p *Provider) Kind() string                { return p.eng.name() }
func (p *Provider) Caps() provider.Capabilities { return p.caps }
func (p *Provider) Close() error                { return nil }

// Health forces an authenticated round-trip (list containers) so the preflight
// proves reachable + authorized, not merely a TCP open.
func (p *Provider) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()
	if _, err := p.eng.containers(ctx); err != nil {
		return fmt.Errorf("doc: health: %w", err)
	}
	return nil
}

// List maps the path arity to the tree level:
//   - []            → indices/collections as KindContainer nodes (HasChildren).
//   - [container]   → documents as KindBlob nodes (Name = the doc id), cursor-paged.
func (p *Provider) List(ctx context.Context, path provider.Path, page provider.Page) ([]provider.Node, string, error) {
	switch len(path.Segments) {
	case 0:
		names, err := p.eng.containers(ctx)
		if err != nil {
			return nil, "", err
		}
		nodes := make([]provider.Node, 0, len(names))
		for _, n := range names {
			nodes = append(nodes, provider.Node{
				Name: n, Kind: provider.KindContainer,
				Path: child(path, n), HasChildren: true,
			})
		}
		return nodes, "", nil
	case 1:
		limit := page.Limit
		if limit <= 0 || limit > maxLimit {
			limit = defaultLimit
		}
		ids, next, err := p.eng.docs(ctx, path.Segments[0], page.Cursor, limit)
		if err != nil {
			return nil, "", err
		}
		nodes := make([]provider.Node, 0, len(ids))
		for _, id := range ids {
			nodes = append(nodes, provider.Node{
				Name: id, Kind: provider.KindBlob, Path: child(path, id),
			})
		}
		return nodes, next, nil
	default:
		return nil, "", fmt.Errorf("doc: %w: list expects [] or [container]", provider.ErrInvalid)
	}
}

// Stat returns a KindBlob node for one document, confirming existence by
// fetching it (there is no universal cheap HEAD across these engines).
func (p *Provider) Stat(ctx context.Context, path provider.Path) (provider.Node, error) {
	container, id, err := blobAddr(path)
	if err != nil {
		return provider.Node{}, err
	}
	body, err := p.eng.getDoc(ctx, container, id)
	if err != nil {
		return provider.Node{}, err
	}
	size := int64(len(body))
	return provider.Node{
		Name: id, Kind: provider.KindBlob, Path: path,
		Meta: &provider.NodeMeta{Size: &size, ContentType: "application/json"},
	}, nil
}

// ReadBlob returns the document as pretty-printed JSON, head-sliced (Truncated)
// over MaxInlineBytes for a view-only preview of oversize blobs.
func (p *Provider) ReadBlob(ctx context.Context, path provider.Path) ([]byte, provider.BlobMeta, error) {
	container, id, err := blobAddr(path)
	if err != nil {
		return nil, provider.BlobMeta{}, err
	}
	raw, err := p.eng.getDoc(ctx, container, id)
	if err != nil {
		return nil, provider.BlobMeta{}, err
	}
	out := prettyJSON(raw)
	meta := provider.BlobMeta{ContentType: "application/json", Size: int64(len(out)), Vector: p.vector}
	if int64(len(out)) > p.caps.MaxInlineBytes {
		// D-06: a raw byte-slice of pretty JSON cuts mid-token, returning
		// invalid JSON at a 200 that any parser trusting the content-type fails
		// on. Return a VALID JSON marker instead (the true Size already rode the
		// meta, KV-AUD-05); the SPA disables inline edit on Truncated, so a
		// truncated body can never round-trip back as a save.
		meta.Truncated = true
		out = truncationNotice(meta.Size, p.caps.MaxInlineBytes)
	}
	return out, meta, nil
}

// Search runs a bounded, read-only text search over ONE container, returning
// matching document ids as KindBlob nodes — the same shape List emits, so the
// SPA renders results exactly like a browse listing. The provider returns ids
// only, never engine highlight/snippet HTML, so no upstream markup crosses the
// wire (XSS-safe). es/meili/typesense support it; qdrant's vector/payload search
// is deferred (DD-2) and returns ErrUnsupported.
func (p *Provider) Search(ctx context.Context, path provider.Path, q string, page provider.Page) ([]provider.Node, string, error) {
	sr, ok := p.eng.(searcher)
	if !ok {
		return nil, "", fmt.Errorf("doc: search: %w", provider.ErrUnsupported)
	}
	if len(path.Segments) != 1 {
		return nil, "", fmt.Errorf("doc: %w: search expects [container]", provider.ErrInvalid)
	}
	if len(q) > maxSearchQueryLen {
		return nil, "", fmt.Errorf("doc: %w: query exceeds %d bytes", provider.ErrInvalid, maxSearchQueryLen)
	}
	limit := page.Limit
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}
	ctx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()
	ids, next, err := sr.search(ctx, path.Segments[0], q, page.Cursor, limit)
	if err != nil {
		return nil, "", err
	}
	nodes := make([]provider.Node, 0, len(ids))
	for _, id := range ids {
		nodes = append(nodes, provider.Node{Name: id, Kind: provider.KindBlob, Path: child(path, id)})
	}
	return nodes, next, nil
}

// WriteBlob upserts a document (gated by the read-only capability).
// contentType is accepted only for parity with provider.ObjectProvider's
// WriteBlob — a document is always JSON, so it is ignored.
func (p *Provider) WriteBlob(ctx context.Context, path provider.Path, data []byte, _ string) error {
	if p.caps.ReadOnly {
		return provider.ErrReadOnly
	}
	container, id, err := blobAddr(path)
	if err != nil {
		return err
	}
	return p.eng.putDoc(ctx, container, id, data)
}

// CreateDoc creates a NEW document (S17). The path is either [container]
// (engine assigns the id, echoed back) or [container, id] (explicit id,
// collision-refusing — ErrConflict if it already exists). meili is async, so
// the write is task-confirmed (reusing putDoc's poll); typesense/es are
// synchronous. qdrant is read-only and is refused at the capability gate before
// any engine call. It returns the id the document was actually stored under.
func (p *Provider) CreateDoc(ctx context.Context, path provider.Path, data []byte) (string, error) {
	if p.caps.ReadOnly {
		return "", provider.ErrReadOnly
	}
	dc, ok := p.eng.(docCreator)
	if !ok {
		return "", fmt.Errorf("doc: create: %w", provider.ErrUnsupported)
	}
	var container, id string
	switch len(path.Segments) {
	case 1:
		container = path.Segments[0] // engine-assigned id
	case 2:
		container, id = path.Segments[0], path.Segments[1] // explicit id
	default:
		return "", fmt.Errorf("doc: %w: create expects [container] or [container, id]", provider.ErrInvalid)
	}
	return dc.createDoc(ctx, container, id, data)
}

// Delete removes a document (gated by the read-only capability).
func (p *Provider) Delete(ctx context.Context, path provider.Path) error {
	if p.caps.ReadOnly {
		return provider.ErrReadOnly
	}
	container, id, err := blobAddr(path)
	if err != nil {
		return err
	}
	return p.eng.deleteDoc(ctx, container, id)
}

// ---- helpers ----

// blobAddr unpacks a [container, id] document address.
func blobAddr(path provider.Path) (container, id string, err error) {
	if len(path.Segments) != 2 {
		return "", "", fmt.Errorf("doc: %w: expected [container, id]", provider.ErrInvalid)
	}
	return path.Segments[0], path.Segments[1], nil
}

// child appends name to path without aliasing the parent's slice backing array.
func child(parent provider.Path, name string) provider.Path {
	seg := make([]string, len(parent.Segments)+1)
	copy(seg, parent.Segments)
	seg[len(parent.Segments)] = name
	return provider.Path{Service: parent.Service, Segments: seg}
}

// prettyJSON indents raw JSON; non-JSON input is returned verbatim.
func prettyJSON(raw []byte) []byte {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return raw
	}
	return buf.Bytes()
}

// truncationNotice renders a small, VALID JSON marker returned in place of an
// over-cap document body (D-06). It never contains the mangled prefix a raw
// byte-slice would, so a client that trusts the JSON content-type always parses
// it; size/inlineCap tell the SPA the true size and the applied cap.
func truncationNotice(size, inlineCap int64) []byte {
	b, err := json.Marshal(map[string]any{
		"_dataconsole_truncated": true,
		"_size_bytes":            size,
		"_inline_cap_bytes":      inlineCap,
		"_note":                  "Document exceeds the inline view limit and was not loaded; download the full document to view or edit it.",
	})
	if err != nil {
		return []byte(`{"_dataconsole_truncated":true}`)
	}
	return b
}

// bodyField reads the value of field `key` from a JSON-object body, rendered as
// a plain string id (matching rawToID). present is false when the field is
// absent; a body that is not a JSON object is an ErrInvalid. It is the identity
// guard's reader (DD-6): a Save must honor the PATH id, so a body carrying a
// different id for its engine's key field is refused, never silently misrouted.
func bodyField(body []byte, key string) (val string, present bool, err error) {
	var m map[string]json.RawMessage
	if uerr := json.Unmarshal(body, &m); uerr != nil {
		return "", false, fmt.Errorf("doc: %w: body is not a JSON object", provider.ErrInvalid)
	}
	raw, ok := m[key]
	if !ok {
		return "", false, nil
	}
	return rawToID(raw), true, nil
}

// rawToID renders a JSON scalar id (string or number) as a plain string.
func rawToID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

// jsonBody marshals a request body, mapping the (practically unreachable) encode
// failure to a sanitized sentinel.
func jsonBody(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("doc: encode: %w", provider.ErrInvalid)
	}
	return b, nil
}

// invalidCursor is returned when an opaque cursor fails to parse — a client bug,
// since the provider mints every cursor it later receives.
func invalidCursor() error {
	return fmt.Errorf("doc: %w: malformed cursor", provider.ErrInvalid)
}
