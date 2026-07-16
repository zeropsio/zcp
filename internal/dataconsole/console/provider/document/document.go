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

// Provider browses + edits one document-engine service as a blob tree.
type Provider struct {
	eng  engine
	caps provider.Capabilities
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
		eng: eng,
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
	return provider.Node{
		Name: id, Kind: provider.KindBlob, Path: path,
		Meta: map[string]any{"size": int64(len(body)), "contentType": "application/json"},
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
	meta := provider.BlobMeta{ContentType: "application/json", Size: int64(len(out))}
	if int64(len(out)) > p.caps.MaxInlineBytes {
		meta.Truncated = true
		out = out[:p.caps.MaxInlineBytes]
	}
	return out, meta, nil
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
