// Package server is the Data Console HTTP transport: a same-origin server that
// serves the embedded SPA and a JSON API over one origin (no CORS). It binds
// localhost only; the per-process bearer is the security boundary (any web page
// can reach 127.0.0.1, so every /api/* call is bearer-gated). All write guards
// route through safety.Policy server-side — the UI is never a boundary.
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
		{pattern: "/api/query", methods: []string{http.MethodPost}, action: provider.ActionQuerySQL, handler: s.handleQuery},
		{pattern: "/api/cell", methods: []string{http.MethodPost, http.MethodPut}, mutating: true, action: provider.ActionEditCell, handler: s.handleCell},
		{pattern: "/api/row", methods: []string{http.MethodPost}, mutating: true, action: provider.ActionInsertRow, handler: s.handleRow},
		{pattern: "/api/row", methods: []string{http.MethodDelete}, mutating: true, action: provider.ActionDeleteRow, handler: s.handleRow},
		{pattern: "/api/entry", methods: []string{http.MethodPut, http.MethodDelete}, mutating: true, action: provider.ActionEditKVEntry, handler: s.handleEntry},
		{pattern: "/api/upload", methods: []string{http.MethodPost}, mutating: true, action: provider.ActionUploadObject, handler: s.handleUpload},
		{pattern: "/api/rename", methods: []string{http.MethodPost}, mutating: true, action: provider.ActionRenameObject, handler: s.handleRename},
		{pattern: "/api/ttl", methods: []string{http.MethodPut}, mutating: true, action: provider.ActionSetTTL, handler: s.handleTTL},
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

// auth enforces the bearer on /api/* (constant-time compare).
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
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
		w.Header().Set("X-DataConsole-Truncated", boolStr(meta.Truncated))
		_, _ = w.Write(data)
	case http.MethodPut:
		// Write: the route middleware already authorized the mutation; the path
		// comes from the body (service is inside path, not the query).
		var body struct {
			Path provider.Path `json:"path"`
			Data []byte        `json:"data"`
		}
		if !decode(w, r, &body) {
			return
		}
		p, _, err := s.engine.ProviderFor(r.Context(), body.Path.Service)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		wb, ok := p.(interface {
			WriteBlob(context.Context, provider.Path, []byte) error
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("blob write: %w", provider.ErrUnsupported))
			return
		}
		if err := wb.WriteBlob(r.Context(), body.Path, body.Data); err != nil {
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

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Service string        `json:"service"`
		Stmt    string        `json:"stmt"`
		Page    provider.Page `json:"page"`
	}
	if !decode(w, r, &body) {
		return
	}
	p, _, err := s.engine.ProviderFor(r.Context(), body.Service)
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
	tp, err := q.Query(r.Context(), body.Stmt, body.Page)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, tp)
}

func (s *Server) handleCell(w http.ResponseWriter, r *http.Request) {
	var edit provider.CellEdit
	if !decode(w, r, &edit) {
		return
	}
	p, _, err := s.engine.ProviderFor(r.Context(), edit.Path.Service)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ec, ok := p.(interface {
		EditCell(context.Context, provider.CellEdit) (provider.Applied, error)
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("cell: %w", provider.ErrUnsupported))
		return
	}
	applied, err := ec.EditCell(r.Context(), edit)
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
		p, _, err := s.engine.ProviderFor(r.Context(), body.Path.Service)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		ins, ok := p.(interface {
			InsertRow(context.Context, provider.Path, map[string]any) (provider.Applied, error)
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("row insert: %w", provider.ErrUnsupported))
			return
		}
		applied, err := ins.InsertRow(r.Context(), body.Path, body.Row)
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
		p, _, err := s.engine.ProviderFor(r.Context(), body.Path.Service)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		del, ok := p.(interface {
			DeleteRow(context.Context, provider.Path, map[string]any) (provider.Applied, error)
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("row delete: %w", provider.ErrUnsupported))
			return
		}
		applied, err := del.DeleteRow(r.Context(), body.Path, body.Key)
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
		p, _, err := s.engine.ProviderFor(r.Context(), e.Path.Service)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		se, ok := p.(interface {
			SetEntry(context.Context, provider.KVEntryEdit) (provider.Applied, error)
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("entry set: %w", provider.ErrUnsupported))
			return
		}
		applied, err := se.SetEntry(r.Context(), e)
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
		p, _, err := s.engine.ProviderFor(r.Context(), body.Path.Service)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		de, ok := p.(interface {
			DeleteEntry(context.Context, provider.Path, string) (provider.Applied, error)
		})
		if !ok {
			writeErr(w, r, fmt.Errorf("entry delete: %w", provider.ErrUnsupported))
			return
		}
		applied, err := de.DeleteEntry(r.Context(), body.Path, body.Field)
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
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, r, fmt.Errorf("upload file: %w", provider.ErrInvalid))
		return
	}
	defer func() { _ = file.Close() }()
	path := provider.Path{Service: r.FormValue("service")}
	if segs := r.FormValue("segs"); segs != "" {
		_ = json.Unmarshal([]byte(segs), &path.Segments)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxWriteBody))
	if err != nil {
		writeErr(w, r, fmt.Errorf("upload read: %w", provider.ErrInvalid))
		return
	}
	p, _, err := s.engine.ProviderFor(r.Context(), path.Service)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	wb, ok := p.(interface {
		WriteBlob(context.Context, provider.Path, []byte) error
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("upload: %w", provider.ErrUnsupported))
		return
	}
	if err := wb.WriteBlob(r.Context(), path, data); err != nil {
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
	p, _, err := s.engine.ProviderFor(r.Context(), body.From.Service)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rn, ok := p.(interface {
		Rename(context.Context, provider.Path, provider.Path) error
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("rename: %w", provider.ErrUnsupported))
		return
	}
	if err := rn.Rename(r.Context(), body.From, body.To); err != nil {
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
	p, _, err := s.engine.ProviderFor(r.Context(), body.Path.Service)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	st, ok := p.(interface {
		SetTTL(context.Context, provider.Path, *int64) error
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("ttl: %w", provider.ErrUnsupported))
		return
	}
	if err := st.SetTTL(r.Context(), body.Path, body.TTLSeconds); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path provider.Path `json:"path"`
	}
	if !decode(w, r, &body) {
		return
	}
	p, _, err := s.engine.ProviderFor(r.Context(), body.Path.Service)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	del, ok := p.(interface {
		Delete(context.Context, provider.Path) error
	})
	if !ok {
		writeErr(w, r, fmt.Errorf("delete: %w", provider.ErrUnsupported))
		return
	}
	if err := del.Delete(r.Context(), body.Path); err != nil {
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
	return pg
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxWriteBody))
	if err := dec.Decode(v); err != nil {
		writeErr(w, r, fmt.Errorf("body: %w", provider.ErrInvalid))
		return false
	}
	return true
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
	if meta.requestID == "" {
		meta.requestID = newRequestID()
	}
	if w.Header().Get("X-Request-Id") == "" {
		w.Header().Set("X-Request-Id", meta.requestID)
	}
	status := provider.HTTPStatus(err)
	code := provider.ErrorCode(err)
	logErr(r, meta, status, code, err)

	b, mErr := json.Marshal(errorEnvelope{
		Code:      code,
		Status:    status,
		Message:   publicErrorMessage(err),
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

func publicErrorMessage(err error) string {
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

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
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
