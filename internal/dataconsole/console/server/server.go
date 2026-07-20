// Package server is the Data Console HTTP transport: a same-origin server that
// serves the embedded SPA and a JSON API over one origin (no CORS). It binds
// localhost only; the per-process bearer authorizes every /api/* call (any web
// page can reach 127.0.0.1, so every request must present it). The bearer alone
// never authorizes a MUTATION: the per-request X-Write-Token is that boundary
// (safety.Policy, checked server-side in the route middleware) — a caller
// holding only the read bearer, such as the standalone SPA, can read but can
// never write. The UI is never a boundary.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const maxWriteBody = 64 << 20 // 64 MiB cap on a single write body

// Server wires the engine + the session bearer + the embedded SPA.
type Server struct {
	engine      *console.Engine
	token       string
	webui       fs.FS
	diagnostics io.Writer
	mux         *http.ServeMux
}

type route struct {
	pattern  string
	methods  []string
	mutating bool
	action   provider.ActionID
	handler  http.HandlerFunc
}

type requestContextKey struct{}

type requestContext struct {
	requestID   string
	service     string
	family      provider.Family
	action      provider.ActionID
	diagnostics io.Writer
}

// New builds the server. token is the per-process session bearer; webui is the
// embedded SPA filesystem (the dist/ subtree).
func New(engine *console.Engine, token string, webui fs.FS) *Server {
	return NewWithDiagnostics(engine, token, webui, os.Stderr)
}

// NewWithDiagnostics builds the server with an injectable raw-cause diagnostic
// sink. Production defaults to stderr through New; tests pass a buffer.
func NewWithDiagnostics(engine *console.Engine, token string, webui fs.FS, diagnostics io.Writer) *Server {
	if diagnostics == nil {
		diagnostics = os.Stderr
	}
	s := &Server{engine: engine, token: token, webui: webui, diagnostics: diagnostics, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the root handler (CSP + static + API).
func (s *Server) Handler() http.Handler { return s.security(s.mux) }

func (s *Server) routes() {
	s.registerAPIRoutes(s.apiRoutes())
	// Static SPA (not a secret — the data API behind it is what's gated).
	s.mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) apiRoutes() []route {
	return []route{
		{pattern: "/api/services", methods: []string{http.MethodGet}, handler: s.handleServices},
		{pattern: "/api/refresh", methods: []string{http.MethodPost}, handler: s.handleRefresh},
		{pattern: "/api/tree", methods: []string{http.MethodGet}, handler: s.handleTree},
		{pattern: "/api/stat", methods: []string{http.MethodGet}, handler: s.handleStat},
		{pattern: "/api/blob", methods: []string{http.MethodGet}, action: provider.ActionReadBlob, handler: s.handleBlob},
		{pattern: "/api/blob", methods: []string{http.MethodPut}, mutating: true, action: provider.ActionWriteBlob, handler: s.handleBlob},
		{pattern: "/api/blob", methods: []string{http.MethodDelete}, mutating: true, action: provider.ActionDeleteNode, handler: s.handleNode},
		{pattern: "/api/table", methods: []string{http.MethodGet}, action: provider.ActionReadTable, handler: s.handleTable},
		{pattern: "/api/table/count", methods: []string{http.MethodGet}, action: provider.ActionReadTable, handler: s.handleTableCount},
		{pattern: "/api/query", methods: []string{http.MethodPost}, action: provider.ActionQuerySQL, handler: s.handleQuery},
		{pattern: "/api/search", methods: []string{http.MethodGet}, action: provider.ActionSearchDocs, handler: s.handleSearch},
		{pattern: "/api/cell", methods: []string{http.MethodPost, http.MethodPut}, mutating: true, action: provider.ActionEditCell, handler: s.handleCell},
		{pattern: "/api/row", methods: []string{http.MethodPost}, mutating: true, action: provider.ActionInsertRow, handler: s.handleRow},
		{pattern: "/api/row", methods: []string{http.MethodDelete}, mutating: true, action: provider.ActionDeleteRow, handler: s.handleRow},
		{pattern: "/api/entry", methods: []string{http.MethodPut, http.MethodDelete}, mutating: true, action: provider.ActionEditKVEntry, handler: s.handleEntry},
		{pattern: "/api/upload", methods: []string{http.MethodPost}, mutating: true, action: provider.ActionUploadObject, handler: s.handleUpload},
		{pattern: "/api/rename", methods: []string{http.MethodPost}, mutating: true, action: provider.ActionRenameObject, handler: s.handleRename},
		{pattern: "/api/ttl", methods: []string{http.MethodPut}, mutating: true, action: provider.ActionSetTTL, handler: s.handleTTL},
		{pattern: "/api/kv/create", methods: []string{http.MethodPost}, mutating: true, action: provider.ActionCreateKey, handler: s.handleKVCreate},
		{pattern: "/api/document/create", methods: []string{http.MethodPost}, mutating: true, action: provider.ActionCreateDoc, handler: s.handleDocCreate},
		{pattern: "/api/node", methods: []string{http.MethodGet}, handler: s.handleStat},
		{pattern: "/api/node", methods: []string{http.MethodDelete}, mutating: true, action: provider.ActionDeleteNode, handler: s.handleNode},
	}
}

func (s *Server) registerAPIRoutes(routes []route) {
	byPattern := map[string][]route{}
	var patterns []string
	for _, rt := range routes {
		if _, ok := byPattern[rt.pattern]; !ok {
			patterns = append(patterns, rt.pattern)
		}
		byPattern[rt.pattern] = append(byPattern[rt.pattern], rt)
	}
	for _, pattern := range patterns {
		group := append([]route(nil), byPattern[pattern]...)
		s.mux.HandleFunc(pattern, s.auth(s.routeGroup(group)))
	}
}

func (s *Server) routeGroup(routes []route) http.HandlerFunc {
	allow := allowHeader(routes)
	return func(w http.ResponseWriter, r *http.Request) {
		rt, ok := routeForMethod(routes, r.Method)
		if !ok {
			w.Header().Set("Allow", allow)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r = s.withRouteContext(r, rt)
		if rt.mutating {
			// The per-request WRITE TOKEN is the boundary: only the trusted embed host
			// holds it, so a caller with just the read bearer (the standalone SPA) can
			// never mutate. The Origin check is defense-in-depth (CSRF), never the
			// boundary — it passes the no-Origin broker and same-host/loopback pages.
			if !s.originAllowed(r) {
				writeErr(w, r, fmt.Errorf("write origin: %w", provider.ErrReadOnly))
				return
			}
			if err := s.engine.Policy().AuthorizeWrite(r.Header.Get("X-Write-Token"), s.confirmed(r)); err != nil {
				writeErr(w, r, err)
				return
			}
		}
		rt.handler(w, r)
	}
}

func routeForMethod(routes []route, method string) (route, bool) {
	for _, rt := range routes {
		if slices.Contains(rt.methods, method) {
			return rt, true
		}
	}
	return route{}, false
}

func allowHeader(routes []route) string {
	seen := map[string]bool{}
	var methods []string
	for _, rt := range routes {
		for _, method := range rt.methods {
			if !seen[method] {
				seen[method] = true
				methods = append(methods, method)
			}
		}
	}
	return strings.Join(methods, ", ")
}

func (s *Server) withRouteContext(r *http.Request, rt route) *http.Request {
	meta := requestContextFrom(r.Context())
	meta.action = rt.action
	meta.service = r.URL.Query().Get("service")
	if meta.service != "" {
		meta.family = s.serviceFamily(meta.service)
	}
	return r.WithContext(context.WithValue(r.Context(), requestContextKey{}, meta))
}

func (s *Server) serviceFamily(hostname string) provider.Family {
	for _, svc := range s.engine.Services() {
		if svc.Hostname == hostname {
			return svc.Family
		}
	}
	return ""
}

// enrichRouteContext fills in the error-envelope service+family once a
// body-addressed handler has decoded its body and knows which service it
// targets. withRouteContext only ever reads the URL query (§apiRoutes), so
// every route that carries its Path inside the JSON body (blob write, query,
// cell, row, entry, upload, rename, ttl, node delete) left service/family
// empty on error (KV-AUD-04). This runs strictly on the handler's own time,
// after the write-token gate in routeGroup has already run for mutating
// routes — it only ever ADDS context to an error a handler is about to
// report, never moves authorization earlier or later, and it never
// overwrites a service withRouteContext already resolved from the query.
//
// It takes and returns a context.Context, not a *http.Request: every caller
// derives it from a single r.Context() call and threads the result straight
// into its downstream provider calls (and back into the request via
// r.WithContext for writeErr to pick up) — contextcheck requires the whole
// chain trace back to one r.Context() read per handler, which a
// request-in/request-out helper breaks.
func (s *Server) enrichRouteContext(ctx context.Context, service string) context.Context {
	if service == "" {
		return ctx
	}
	meta := requestContextFrom(ctx)
	if meta.service != "" {
		return ctx
	}
	meta.service = service
	meta.family = s.serviceFamily(service)
	return context.WithValue(ctx, requestContextKey{}, meta)
}

// security sets the embedding + sniffing headers on every response.
func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := requestContext{
			requestID:   newRequestID(),
			diagnostics: s.diagnostics,
		}
		w.Header().Set("X-Request-Id", meta.requestID)
		r = r.WithContext(context.WithValue(r.Context(), requestContextKey{}, meta))
		// frame-ancestors 'none': the console is NEVER iframed. Embedded, the SPA is
		// VS Code webview content (no HTTP framing); standalone, it is a top-level
		// tab. So nothing may frame this origin — clickjacking surface is zero.
		// img-src allows blob: so the SPA can preview an object's bytes via an
		// object-URL (images are inert — they can't execute, and SVG-in-<img> has
		// scripting disabled), without ever navigating the origin to the raw blob.
		w.Header().Set("Content-Security-Policy",
			"frame-ancestors 'none'; default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// auth enforces the bearer on /api/* (constant-time compare). A missing/wrong
// bearer gets the same JSON error envelope as every other failure path —
// previously a bare text/plain 401 (KV-AUD-08), forcing a client that parses
// errors uniformly to special-case this one status. auth runs before
// withRouteContext (it wraps routeGroup), so there is no sentinel error or
// resolved service/family to carry — writeEnvelope is the shared primitive
// that needs neither.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeEnvelope(w, r, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		next(w, r)
	}
}

func (s *Server) confirmed(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("X-Confirm"), "true")
}

// ---- handlers ----

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	proj, err := s.engine.Project(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"project": proj, "services": s.engine.Services(), "allowWrites": s.engine.Policy().ArmingPermitted()})
}

// originAllowed is a defense-in-depth CSRF guard for mutating routes; the bearer +
// write token are the real boundary (a cross-origin page can neither read the
// bearer nor set the custom Authorization/X-Write-Token headers without a CORS
// preflight this server never answers). On top of that: a browser Origin, if
// present, must be same-origin as the request Host (covers the reverse-proxy case,
// where Host is the proxy's) or a loopback origin (direct console access). A
// non-browser client — the embed broker — sends no Origin and is allowed; the
// bearer + write token still gate it.
func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Host == r.Host {
		return true
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.Refresh(r.Context()); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"services": s.engine.Services()})
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	lister, ok := p.(interface {
		List(context.Context, provider.Path, provider.Page) ([]provider.Node, string, error)
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("tree: %w", provider.ErrUnsupported))
		return
	}
	path := parsePath(r)
	nodes, next, err := lister.List(r.Context(), path, parsePage(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"nodes": nodes, "nextCursor": next})
}

func (s *Server) handleStat(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	st, ok := p.(interface {
		Stat(context.Context, provider.Path) (provider.Node, error)
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("stat: %w", provider.ErrUnsupported))
		return
	}
	node, err := st.Stat(r.Context(), parsePath(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, node)
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Read: the path is in the query (service + segs).
		p, ok := s.providerFor(w, r)
		if !ok {
			return
		}
		rb, ok := p.(interface {
			ReadBlob(context.Context, provider.Path) ([]byte, provider.BlobMeta, error)
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("blob: %w", provider.ErrUnsupported))
			return
		}
		data, meta, err := rb.ReadBlob(r.Context(), parsePath(r))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		// Hardening: attacker-controlled bytes must never render inline on the
		// console origin (stored-XSS → bearer theft). Force download semantics.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("X-DataConsole-ContentType", sanitizeHeader(meta.ContentType))
		w.Header().Set("X-DataConsole-Truncated", strconv.FormatBool(meta.Truncated))
		// Vector signals a vector-bearing payload (qdrant: the raw JSON embeds
		// a full embedding array) so the SPA can collapse it into a summary
		// instead of rendering a wall of floats inline (UI-AUD-03).
		w.Header().Set("X-DataConsole-Vector", strconv.FormatBool(meta.Vector))
		// StreamMetadata marks a stream family's summary read (kafka/nats): the
		// body is topic/stream METADATA, not message content, so the SPA labels it
		// a metadata card and never renders it as editable content (U-04).
		w.Header().Set("X-DataConsole-StreamMetadata", strconv.FormatBool(meta.StreamMetadata))
		// Size is the TRUE pre-truncation size (every provider computes it before
		// slicing the response body) — without it a truncated read has no way to
		// show "showing 16 MiB of N" (KV-AUD-05).
		w.Header().Set("X-DataConsole-Size", strconv.FormatInt(meta.Size, 10))
		if meta.TTLSeconds != nil {
			w.Header().Set("X-DataConsole-Ttl-Seconds", strconv.FormatInt(*meta.TTLSeconds, 10))
		}
		_, _ = w.Write(data)
	case http.MethodPut:
		// Write: the route middleware already authorized the mutation; the path
		// comes from the body (service is inside path, not the query).
		var body struct {
			Path        provider.Path `json:"path"`
			Data        []byte        `json:"data"`
			ContentType string        `json:"contentType,omitempty"`
		}
		if !decode(w, r, &body) {
			return
		}
		ctx := s.enrichRouteContext(r.Context(), body.Path.Service)
		r = r.WithContext(ctx)
		p, ok := s.providerForWrite(ctx, w, r, body.Path.Service)
		if !ok {
			return
		}
		wb, ok := p.(interface {
			WriteBlob(context.Context, provider.Path, []byte, string) error
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("blob write: %w", provider.ErrUnsupported))
			return
		}
		ct := resolveContentType(body.ContentType, body.Data)
		if err := wb.WriteBlob(ctx, body.Path, body.Data, ct); err != nil {
			writeErr(w, r, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTable(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	rt, ok := p.(interface {
		ReadTable(context.Context, provider.Path, provider.Page) (provider.TablePage, error)
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("table: %w", provider.ErrUnsupported))
		return
	}
	tp, err := rt.ReadTable(r.Context(), parsePath(r), parsePage(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, tp)
}

func (s *Server) handleTableCount(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	counter, ok := p.(provider.TableCounter)
	if !ok {
		writeErr(w, r, fmt.Errorf("table count: %w", provider.ErrUnsupported))
		return
	}
	count, err := counter.CountTable(r.Context(), parsePath(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]int64{"count": count})
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Service string        `json:"service"`
		Stmt    string        `json:"stmt"`
		Page    provider.Page `json:"page"`
	}
	if !decode(w, r, &body) {
		return
	}
	ctx := s.enrichRouteContext(r.Context(), body.Service)
	r = r.WithContext(ctx)
	p, _, err := s.engine.ProviderFor(ctx, body.Service)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	q, ok := p.(interface {
		Query(context.Context, string, provider.Page) (provider.TablePage, error)
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("query: %w", provider.ErrUnsupported))
		return
	}
	tp, err := q.Query(ctx, body.Stmt, body.Page)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, tp)
}

// handleSearch runs a bounded, read-only document-family text search
// (GET /api/search?service=&segs=[container]&q=&limit=). It is behind the read
// bearer (auth wraps every /api/*) and is non-mutating, so no write token is
// required; the provider bounds the query length + limit and returns matching
// document ids only — never engine highlight/snippet HTML — so no upstream
// markup can reach the SPA. A provider without a Search method (non-document,
// or qdrant) gets a clean 422 unsupported.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	sr, ok := p.(interface {
		Search(context.Context, provider.Path, string, provider.Page) ([]provider.Node, string, error)
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("search: %w", provider.ErrUnsupported))
		return
	}
	nodes, next, err := sr.Search(r.Context(), parsePath(r), r.URL.Query().Get("q"), parsePage(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"nodes": nodes, "nextCursor": next})
}

func (s *Server) handleCell(w http.ResponseWriter, r *http.Request) {
	var edit provider.CellEdit
	if !decode(w, r, &edit) {
		return
	}
	ctx := s.enrichRouteContext(r.Context(), edit.Path.Service)
	r = r.WithContext(ctx)
	p, ok := s.providerForWrite(ctx, w, r, edit.Path.Service)
	if !ok {
		return
	}
	ec, ok := p.(interface {
		EditCell(context.Context, provider.CellEdit) (provider.Applied, error)
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("cell: %w", provider.ErrUnsupported))
		return
	}
	applied, err := ec.EditCell(ctx, edit)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, applied)
}

// handleRow inserts (POST) or deletes (DELETE) one tabular row. Insert carries
// the row map; delete carries the PK key map.
func (s *Server) handleRow(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Path provider.Path  `json:"path"`
			Row  map[string]any `json:"row"`
		}
		if !decode(w, r, &body) {
			return
		}
		ctx := s.enrichRouteContext(r.Context(), body.Path.Service)
		r = r.WithContext(ctx)
		p, ok := s.providerForWrite(ctx, w, r, body.Path.Service)
		if !ok {
			return
		}
		ins, ok := p.(interface {
			InsertRow(context.Context, provider.Path, map[string]any) (provider.Applied, error)
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("row insert: %w", provider.ErrUnsupported))
			return
		}
		applied, err := ins.InsertRow(ctx, body.Path, body.Row)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		writeJSON(w, applied)
	case http.MethodDelete:
		var body struct {
			Path provider.Path  `json:"path"`
			Key  map[string]any `json:"key"`
		}
		if !decode(w, r, &body) {
			return
		}
		ctx := s.enrichRouteContext(r.Context(), body.Path.Service)
		r = r.WithContext(ctx)
		p, ok := s.providerForWrite(ctx, w, r, body.Path.Service)
		if !ok {
			return
		}
		del, ok := p.(interface {
			DeleteRow(context.Context, provider.Path, map[string]any) (provider.Applied, error)
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("row delete: %w", provider.ErrUnsupported))
			return
		}
		applied, err := del.DeleteRow(ctx, body.Path, body.Key)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		writeJSON(w, applied)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleEntry edits (PUT) or deletes (DELETE) one KV collection entry — a hash
// field, list element, set or zset member. The provider issues a single typed
// command (allowlist-safe); a string value uses /api/blob, not this path.
func (s *Server) handleEntry(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPut:
		var e provider.KVEntryEdit
		if !decode(w, r, &e) {
			return
		}
		ctx := s.enrichRouteContext(r.Context(), e.Path.Service)
		r = r.WithContext(ctx)
		p, ok := s.providerForWrite(ctx, w, r, e.Path.Service)
		if !ok {
			return
		}
		se, ok := p.(interface {
			SetEntry(context.Context, provider.KVEntryEdit) (provider.Applied, error)
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("entry set: %w", provider.ErrUnsupported))
			return
		}
		applied, err := se.SetEntry(ctx, e)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		writeJSON(w, applied)
	case http.MethodDelete:
		var body struct {
			Path  provider.Path `json:"path"`
			Field string        `json:"field"`
		}
		if !decode(w, r, &body) {
			return
		}
		ctx := s.enrichRouteContext(r.Context(), body.Path.Service)
		r = r.WithContext(ctx)
		p, ok := s.providerForWrite(ctx, w, r, body.Path.Service)
		if !ok {
			return
		}
		de, ok := p.(interface {
			DeleteEntry(context.Context, provider.Path, string) (provider.Applied, error)
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("entry delete: %w", provider.ErrUnsupported))
			return
		}
		applied, err := de.DeleteEntry(ctx, body.Path, body.Field)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		writeJSON(w, applied)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleUpload streams a multipart file to a new/replaced object (POST
// /api/upload) — the large-body path that avoids base64-in-JSON bloat. The path
// rides the form (service + segs); the bytes are capped at maxWriteBody.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWriteBody)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, r, bodyErr("upload file", err))
		return
	}
	defer func() { _ = file.Close() }()
	path := provider.Path{Service: r.FormValue("service")}
	if segs := r.FormValue("segs"); segs != "" {
		_ = json.Unmarshal([]byte(segs), &path.Segments)
	}
	ctx := s.enrichRouteContext(r.Context(), path.Service)
	r = r.WithContext(ctx)
	// Upload is object-storage-only semantics (multipart file -> S3 object).
	// document.Provider also satisfies the WriteBlob shape below (it maps
	// cleanly onto provider.ObjectProvider — document.go's package doc), so
	// without this check a document (or KV) service would silently accept an
	// upload too, even though familyMutatingActionIDs(FamilyDocument) never
	// advertises ActionUploadObject (resolution 3: "upload a file as a JSON
	// document" has no clear meaning for es/meili/typesense) — the advertised
	// action set and the server's actual enforcement must agree. An unknown
	// service (empty family) falls through to the ProviderFor lookup below,
	// which reports the more precise ErrNotFound rather than this mismatch.
	if fam := s.serviceFamily(path.Service); fam != "" && fam != provider.FamilyObject {
		writeErr(w, r, fmt.Errorf("upload: %w", provider.ErrUnsupported))
		return
	}
	// file's bytes are already bounded by the MaxBytesReader wrapping the
	// whole request body above — no separate LimitReader needed; an over-cap
	// part surfaces here as the same *http.MaxBytesError bodyErr classifies.
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, r, bodyErr("upload read", err))
		return
	}
	// providerForWrite is the uniform action-policy enforcement (absent →
	// ErrUnsupported, disabled → ErrReadOnly); the family pre-check above is
	// only the cheap refusal BEFORE the multipart body is read. ctx was
	// already enriched for this service at the top of the handler.
	p, ok := s.providerForWrite(ctx, w, r, path.Service)
	if !ok {
		return
	}
	wb, ok := p.(interface {
		WriteBlob(context.Context, provider.Path, []byte, string) error
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("upload: %w", provider.ErrUnsupported))
		return
	}
	// header carries the browser-declared MIME type on the multipart part
	// itself (OBJ-AUD-01) — falls back to sniffing only when the client sent
	// none at all.
	ct := resolveContentType(header.Header.Get("Content-Type"), data)
	if err := wb.WriteBlob(ctx, path, data, ct); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleRename renames an object (POST /api/rename, from→to) by copy+delete on
// the provider.
func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From provider.Path `json:"from"`
		To   provider.Path `json:"to"`
	}
	if !decode(w, r, &body) {
		return
	}
	ctx := s.enrichRouteContext(r.Context(), body.From.Service)
	r = r.WithContext(ctx)
	p, ok := s.providerForWrite(ctx, w, r, body.From.Service)
	if !ok {
		return
	}
	rn, ok := p.(interface {
		Rename(context.Context, provider.Path, provider.Path) error
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("rename: %w", provider.ErrUnsupported))
		return
	}
	if err := rn.Rename(ctx, body.From, body.To); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleTTL sets or clears a KV key's TTL (PUT /api/ttl). ttlSeconds null ⇒ persist.
func (s *Server) handleTTL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path       provider.Path `json:"path"`
		TTLSeconds *int64        `json:"ttlSeconds"`
	}
	if !decode(w, r, &body) {
		return
	}
	ctx := s.enrichRouteContext(r.Context(), body.Path.Service)
	r = r.WithContext(ctx)
	p, ok := s.providerForWrite(ctx, w, r, body.Path.Service)
	if !ok {
		return
	}
	st, ok := p.(interface {
		SetTTL(context.Context, provider.Path, *int64) error
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("ttl: %w", provider.ErrUnsupported))
		return
	}
	if err := st.SetTTL(ctx, body.Path, body.TTLSeconds); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleKVCreate creates a NEW KV key of a chosen type (POST /api/kv/create,
// KV-AUD-03). The route middleware already authorized the mutation; the path +
// type + first entry ride the body. A name collision is refused with 409
// conflict (KVCreate never overwrites — the KV-AUD-01 clobber guard).
func (s *Server) handleKVCreate(w http.ResponseWriter, r *http.Request) {
	var c provider.KVCreate
	if !decode(w, r, &c) {
		return
	}
	ctx := s.enrichRouteContext(r.Context(), c.Path.Service)
	r = r.WithContext(ctx)
	p, ok := s.providerForWrite(ctx, w, r, c.Path.Service)
	if !ok {
		return
	}
	cr, ok := p.(interface {
		CreateKey(context.Context, provider.KVCreate) (provider.Applied, error)
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("kv create: %w", provider.ErrUnsupported))
		return
	}
	applied, err := cr.CreateKey(ctx, c)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, applied)
}

// handleDocCreate creates a NEW document (POST /api/document/create, S17). The
// path is [container] (engine-assigned id) or [container, id] (explicit id,
// collision-refusing). It echoes the id the document was stored under (the
// engine-assigned id for typesense/es auto-id — audit D-05).
func (s *Server) handleDocCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path provider.Path `json:"path"`
		Data []byte        `json:"data"`
	}
	if !decode(w, r, &body) {
		return
	}
	ctx := s.enrichRouteContext(r.Context(), body.Path.Service)
	r = r.WithContext(ctx)
	p, ok := s.providerForWrite(ctx, w, r, body.Path.Service)
	if !ok {
		return
	}
	cr, ok := p.(interface {
		CreateDoc(context.Context, provider.Path, []byte) (string, error)
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("document create: %w", provider.ErrUnsupported))
		return
	}
	id, err := cr.CreateDoc(ctx, body.Path, body.Data)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path provider.Path `json:"path"`
	}
	if !decode(w, r, &body) {
		return
	}
	ctx := s.enrichRouteContext(r.Context(), body.Path.Service)
	r = r.WithContext(ctx)
	p, ok := s.providerForWrite(ctx, w, r, body.Path.Service)
	if !ok {
		return
	}
	del, ok := p.(interface {
		Delete(context.Context, provider.Path) error
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("delete: %w", provider.ErrUnsupported))
		return
	}
	if err := del.Delete(ctx, body.Path); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	upath := strings.TrimPrefix(r.URL.Path, "/")
	if upath == "" {
		upath = "index.html"
	}
	data, err := fs.ReadFile(s.webui, upath)
	if err != nil {
		// SPA fallback.
		data, err = fs.ReadFile(s.webui, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		upath = "index.html"
	}
	w.Header().Set("Content-Type", contentType(upath))
	_, _ = w.Write(data)
}

// providerFor resolves the provider named by the request's `service` param.
func (s *Server) providerFor(w http.ResponseWriter, r *http.Request) (provider.Provider, bool) {
	svc := r.URL.Query().Get("service")
	if svc == "" {
		writeErr(w, r, fmt.Errorf("missing service: %w", provider.ErrInvalid))
		return nil, false
	}
	p, _, err := s.engine.ProviderFor(r.Context(), svc)
	if err != nil {
		writeErr(w, r, err)
		return nil, false
	}
	return p, true
}

// providerForWrite is providerFor's mutating-route counterpart: the ONE choke
// point every mutating handler routes through once it has decoded its target
// hostname from the request body/form (every mutating route is body/form-
// addressed — none carries `service` in the query, see apiRoutes). Beyond
// resolving the provider, it enforces the route's declared action (stashed in
// the request context by withRouteContext) against the resolved service's OWN
// action policy (provider.ServiceActions, carried on ServiceView.Actions) —
// the server-side enforcement half of what the SPA's disabled affordance is
// only presentation for (docs/spec-dataconsole-testing.md §3). An action
// absent from the service's list, or present but Enabled==false (a view-only
// or not-yet support tier, independent of and in addition to the session
// write-token gate routeGroup already ran), is refused with the SAME
// provider.ErrReadOnly envelope every other write-capability failure uses —
// there is no oracle distinguishing "wrong family", "wrong support tier", and
// "session read-only" from a caller holding only a valid write token.
//
// Constructors remain the ultimate posture owners (e.g. tabular forces
// ClickHouse non-editable intrinsically) — this check is an independent,
// additional gate, never a replacement for that defense-in-depth.
func (s *Server) providerForWrite(ctx context.Context, w http.ResponseWriter, r *http.Request, service string) (provider.Provider, bool) {
	p, view, err := s.engine.ProviderFor(ctx, service)
	if err != nil {
		writeErr(w, r, err)
		return nil, false
	}
	action := requestContextFrom(ctx).action
	if err := actionPolicyErr(view.Actions, action); err != nil {
		writeErr(w, r, fmt.Errorf("action %s: %w", action, err))
		return nil, false
	}
	return p, true
}

// actionPolicyErr checks id against the service's own single-owner action
// list (provider.ServiceActions) — never a second, independently computed
// policy. Two distinct refusals (spec-dataconsole-testing.md §3):
//   - id ABSENT from the list: the operation does not exist for this
//     family at all (upload on a tabular service) → ErrUnsupported. The
//     action set is public per-service knowledge (GET /api/services), so
//     this leaks nothing — while a read_only answer would send an
//     authorized caller into a futile re-arm loop.
//   - id present but DISABLED (view-only tier, read-only posture) →
//     the uniform capability refusal ErrReadOnly (spec-dataconsole §5.1 —
//     no oracle on WHICH capability condition failed).
func actionPolicyErr(actions []provider.Action, id provider.ActionID) error {
	for _, a := range actions {
		if a.ID == id {
			if a.Enabled {
				return nil
			}
			return provider.ErrReadOnly
		}
	}
	return provider.ErrUnsupported
}

// ---- helpers ----

func parsePath(r *http.Request) provider.Path {
	p := provider.Path{Service: r.URL.Query().Get("service")}
	if segs := r.URL.Query().Get("segs"); segs != "" {
		_ = json.Unmarshal([]byte(segs), &p.Segments)
	}
	return p
}

func parsePage(r *http.Request) provider.Page {
	pg := provider.Page{Cursor: r.URL.Query().Get("cursor")}
	if l := r.URL.Query().Get("limit"); l != "" {
		_, _ = fmt.Sscanf(l, "%d", &pg.Limit)
	}
	column, direction := r.URL.Query().Get("sort"), provider.SortDirection(r.URL.Query().Get("direction"))
	if column != "" || direction != "" {
		pg.Sort = &provider.Sort{Column: column, Direction: direction}
	}
	return pg
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	// MaxBytesReader (not a bare io.LimitReader): a body over maxWriteBody must
	// surface as the typed 413 ErrTooLarge, not silently truncate mid-token
	// into what looks like ordinary malformed JSON — a legitimate large write
	// and a client sending garbage used to look identical (400 invalid,
	// DOC-AUD-02). MaxBytesReader is the one primitive that raises a
	// distinguishable error (*http.MaxBytesError) exactly at the cap instead
	// of just stopping the byte stream.
	r.Body = http.MaxBytesReader(w, r.Body, maxWriteBody)
	dec := json.NewDecoder(r.Body)
	// UseNumber: a field typed `any` (CellEdit.NewValue/ExpectedOld,
	// InsertRow/DeleteRow's row/key maps) would otherwise decode a bare JSON
	// integer into a float64 — for a bigint-range value that is IEEE-754
	// double rounding at JSON-decode time itself, before the provider ever
	// sees it (T-AUD-02, proven ±1 corruption above 2^53). json.Number
	// preserves the exact source decimal text; every concretely-typed field
	// (int, *int64, *float64, …) is unaffected — UseNumber only changes the
	// interface{} fallback.
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		writeErr(w, r, bodyErr("body", err))
		return false
	}
	return true
}

// bodyErr classifies a request-body read/decode failure: an over-cap body
// (http.MaxBytesReader tripped, surfaced as *http.MaxBytesError possibly
// wrapped by a decoder/multipart layer above it) is ErrTooLarge (413), never
// the misleading ErrInvalid (400) a silently truncated read used to produce
// (DOC-AUD-02) — every other read/parse failure stays ErrInvalid.
func bodyErr(op string, err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return fmt.Errorf("%s: %w", op, provider.ErrTooLarge)
	}
	return fmt.Errorf("%s: %w", op, provider.ErrInvalid)
}

// resolveContentType prefers the caller's explicit content-type; when absent,
// it sniffs one from the bytes. A write that carries no content-type at all
// is OBJ-AUD-01's root cause: minio-go/S3 default an empty type to
// "application/octet-stream", which degrades every later read — an uploaded
// image loses its preview, and a text file loses its own editability the
// next time the console opens it.
func resolveContentType(explicit string, data []byte) string {
	if explicit != "" {
		return explicit
	}
	return http.DetectContentType(data)
}

func writeJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"encode failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

type errorEnvelope struct {
	Code      string            `json:"code"`
	Status    int               `json:"status"`
	Message   string            `json:"message"`
	Service   string            `json:"service,omitempty"`
	Family    provider.Family   `json:"family,omitempty"`
	Action    provider.ActionID `json:"action,omitempty"`
	RequestID string            `json:"requestId"`
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	meta := requestContextFrom(r.Context())
	status := provider.HTTPStatus(err)
	code := provider.ErrorCode(err)
	logErr(r, meta, status, code, err)
	writeEnvelope(w, r, status, code, publicErrorMessage(err))
}

// writeEnvelope writes the shared JSON error envelope for a status/code/
// message triple that has no provider sentinel to drive it — today only the
// bearer-auth failure (401, KV-AUD-08): auth() runs before any provider or
// route context is reached, so there is no sentinel error to map through
// writeErr, but the client-facing shape must still be the one uniform
// envelope every other error path uses.
func writeEnvelope(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	meta := requestContextFrom(r.Context())
	if meta.requestID == "" {
		meta.requestID = newRequestID()
	}
	if w.Header().Get("X-Request-Id") == "" {
		w.Header().Set("X-Request-Id", meta.requestID)
	}
	b, mErr := json.Marshal(errorEnvelope{
		Code:      code,
		Status:    status,
		Message:   message,
		Service:   meta.service,
		Family:    meta.family,
		Action:    meta.action,
		RequestID: meta.requestID,
	})
	if mErr != nil {
		b = []byte(`{"code":"internal","status":500,"message":"internal error","requestId":"` + meta.requestID + `"}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func requestContextFrom(ctx context.Context) requestContext {
	meta, _ := ctx.Value(requestContextKey{}).(requestContext)
	if meta.diagnostics == nil {
		meta.diagnostics = os.Stderr
	}
	return meta
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b[:])
}

// publicErrorMessage builds the client-facing envelope message: the flat
// generic string for err's sentinel, plus — ONLY when err carries a
// provider.PublicDetailer (EXT-1's opt-in exception to "raw driver causes
// never cross the wire", I-2) — the already-sanitized detail appended after
// a ": " separator. An error that is not a PublicDetailer (the vast
// majority — every plain fmt.Errorf("...: %w", Err...) wrap across every
// family) gets EXACTLY the pre-EXT-1 flat generic string, unchanged
// (TestServer_ErrorEnvelope_SentinelErrors pins this).
func publicErrorMessage(err error) string {
	msg := sentinelMessage(err)
	var pd provider.PublicDetailer
	if errors.As(err, &pd) {
		if detail := pd.PublicDetail(); detail != "" {
			return msg + ": " + detail
		}
	}
	return msg
}

func sentinelMessage(err error) string {
	switch {
	case errors.Is(err, provider.ErrNotFound):
		return provider.ErrNotFound.Error()
	case errors.Is(err, provider.ErrReadOnly):
		return provider.ErrReadOnly.Error()
	case errors.Is(err, provider.ErrNeedsConfirm):
		return provider.ErrNeedsConfirm.Error()
	case errors.Is(err, provider.ErrConflict):
		return provider.ErrConflict.Error()
	case errors.Is(err, provider.ErrTooLarge):
		return provider.ErrTooLarge.Error()
	case errors.Is(err, provider.ErrUnsupported):
		return provider.ErrUnsupported.Error()
	case errors.Is(err, provider.ErrUnreachable):
		return provider.ErrUnreachable.Error()
	case errors.Is(err, provider.ErrUpstream):
		return provider.ErrUpstream.Error()
	case errors.Is(err, provider.ErrInvalid):
		return provider.ErrInvalid.Error()
	case errors.Is(err, provider.ErrWrongType):
		return provider.ErrWrongType.Error()
	case errors.Is(err, provider.ErrTimeout):
		return provider.ErrTimeout.Error()
	default:
		return "internal error"
	}
}

func logErr(r *http.Request, meta requestContext, status int, code string, err error) {
	_, _ = fmt.Fprintf(meta.diagnostics,
		"dataconsole error requestId=%s method=%s path=%s service=%s family=%s action=%s status=%d code=%s err=%q\n",
		meta.requestID,
		r.Method,
		r.URL.Path,
		meta.service,
		meta.family,
		meta.action,
		status,
		code,
		err.Error(),
	)
}

func sanitizeHeader(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
}

func contentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}
