package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
)

// recProvider records the last mutating call so route tests can assert the
// handler reached the provider. It implements every optional method the handlers
// type-assert (tabular row, KV entry, object blob/rename), so one fake serves all
// the new S0 routes.
type recProvider struct {
	readOnly bool
	lastOp   string

	// lastInsertRow/lastCellEdit capture exactly what the server handed to
	// the provider — used to pin decode()'s UseNumber() precision fix
	// end-to-end (T-AUD-02): a bare JSON integer in the request body must
	// reach the provider as json.Number, never a rounded float64.
	lastInsertRow map[string]any
	lastCellEdit  provider.CellEdit
}

func (r *recProvider) Kind() string { return "rec" }
func (r *recProvider) Caps() provider.Capabilities {
	return provider.Capabilities{Support: provider.SupportFull, ReadOnly: r.readOnly, MaxInlineBytes: 1 << 20}
}
func (r *recProvider) Health(context.Context) error { return nil }
func (r *recProvider) Close() error                 { return nil }

func (r *recProvider) gate() error {
	if r.readOnly {
		return provider.ErrReadOnly
	}
	return nil
}

// tabular cell
func (r *recProvider) EditCell(_ context.Context, edit provider.CellEdit) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.lastOp = "editcell"
	r.lastCellEdit = edit
	return provider.Applied{Statement: "UPDATE", Affected: 1}, nil
}

// tabular row
func (r *recProvider) InsertRow(_ context.Context, _ provider.Path, row map[string]any) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.lastOp = "insert"
	r.lastInsertRow = row
	return provider.Applied{Statement: "INSERT", Affected: 1}, nil
}
func (r *recProvider) DeleteRow(_ context.Context, _ provider.Path, _ map[string]any) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.lastOp = "deleterow"
	return provider.Applied{Statement: "DELETE", Affected: 1}, nil
}

// KV entry
func (r *recProvider) SetEntry(_ context.Context, _ provider.KVEntryEdit) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.lastOp = "setentry"
	return provider.Applied{Statement: "HSET", Affected: 1}, nil
}
func (r *recProvider) DeleteEntry(_ context.Context, _ provider.Path, _ string) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.lastOp = "delentry"
	return provider.Applied{Statement: "HDEL", Affected: 1}, nil
}

// object blob + rename
func (r *recProvider) WriteBlob(_ context.Context, _ provider.Path, _ []byte, _ string) error {
	if err := r.gate(); err != nil {
		return err
	}
	r.lastOp = "write"
	return nil
}
func (r *recProvider) Rename(_ context.Context, _, _ provider.Path) error {
	if err := r.gate(); err != nil {
		return err
	}
	r.lastOp = "rename"
	return nil
}
func (r *recProvider) Delete(_ context.Context, _ provider.Path) error {
	if err := r.gate(); err != nil {
		return err
	}
	r.lastOp = "delete"
	return nil
}
func (r *recProvider) Stat(_ context.Context, p provider.Path) (provider.Node, error) {
	return provider.Node{Name: "node", Kind: provider.KindBlob, Path: p}, nil
}

// document search (a READ — never gated on the write posture).
func (r *recProvider) Search(_ context.Context, _ provider.Path, q string, _ provider.Page) ([]provider.Node, string, error) {
	r.lastOp = "search"
	return []provider.Node{{Name: "hit-" + q, Kind: provider.KindBlob}}, "", nil
}

// KV create
func (r *recProvider) CreateKey(_ context.Context, _ provider.KVCreate) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.lastOp = "createkey"
	return provider.Applied{Statement: "HSET", Affected: 1}, nil
}

// document create
func (r *recProvider) CreateDoc(_ context.Context, _ provider.Path, _ []byte) (string, error) {
	if err := r.gate(); err != nil {
		return "", err
	}
	r.lastOp = "createdoc"
	return "assigned-1", nil
}

type recHost struct {
	svcType         string
	connectionCalls int
}

func (*recHost) Project(context.Context) (console.ProjectRef, error) {
	return console.ProjectRef{ID: "p", Name: "proj"}, nil
}
func (h *recHost) ManagedServices(context.Context) ([]console.ManagedServiceRef, error) {
	return []console.ManagedServiceRef{{ID: "s1", Hostname: "svc", Type: h.svcType, Status: "ACTIVE"}}, nil
}
func (h *recHost) ConnectionInfo(context.Context, string) (console.ConnectionInfo, error) {
	h.connectionCalls++
	return testConnectionInfo(h.svcType), nil
}

func newRouteServer(t *testing.T, svcType string, fam provider.Family, allowWrites bool) (*testServer, string, *recProvider, *recHost) {
	t.Helper()
	rec := &recProvider{readOnly: !allowWrites}
	factories := map[provider.Family]console.Factory{
		fam: func(console.ConnectionInfo, *safety.Policy) (provider.Provider, error) { return rec, nil },
	}
	host := &recHost{svcType: svcType}
	eng := console.NewEngine(host, writePolicy(allowWrites), factories)
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	srv := New(eng, "secret", fstest.MapFS{"index.html": {Data: []byte("ok")}})
	return &testServer{handler: srv.Handler()}, "secret", rec, host
}

type routeMatrixCase struct {
	name     string
	method   string
	path     string
	body     string
	mutating bool
	confirm  bool
	headers  map[string]string
}

func routeMatrix() []routeMatrixCase {
	return []routeMatrixCase{
		{name: "services", method: http.MethodGet, path: "/api/services"},
		{name: "refresh", method: http.MethodPost, path: "/api/refresh"},
		{name: "tree", method: http.MethodGet, path: "/api/tree?service=svc"},
		{name: "stat", method: http.MethodGet, path: "/api/stat?service=svc"},
		{name: "blob read", method: http.MethodGet, path: `/api/blob?service=svc&segs=%5B%22a.txt%22%5D`},
		{name: "blob write", method: http.MethodPut, path: "/api/blob", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "blob delete", method: http.MethodDelete, path: "/api/blob", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "table", method: http.MethodGet, path: "/api/table?service=svc"},
		{name: "query", method: http.MethodPost, path: "/api/query", body: invalidJSONBody},
		{name: "search", method: http.MethodGet, path: "/api/search?service=svc&q=x"},
		{name: "cell post", method: http.MethodPost, path: "/api/cell", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "cell put", method: http.MethodPut, path: "/api/cell", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "row insert", method: http.MethodPost, path: "/api/row", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "row delete", method: http.MethodDelete, path: "/api/row", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "entry set", method: http.MethodPut, path: "/api/entry", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "entry delete", method: http.MethodDelete, path: "/api/entry", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "upload", method: http.MethodPost, path: "/api/upload", body: invalidMultipartBody, mutating: true, confirm: true, headers: map[string]string{"Content-Type": "multipart/form-data; boundary=x"}},
		{name: "rename", method: http.MethodPost, path: "/api/rename", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "ttl", method: http.MethodPut, path: "/api/ttl", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "kv create", method: http.MethodPost, path: "/api/kv/create", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "document create", method: http.MethodPost, path: "/api/document/create", body: invalidJSONBody, mutating: true, confirm: true},
		{name: "node read", method: http.MethodGet, path: "/api/node?service=svc"},
		{name: "node delete", method: http.MethodDelete, path: "/api/node", body: invalidJSONBody, mutating: true, confirm: true},
	}
}

const (
	invalidJSONBody      = `{"path":`
	invalidMultipartBody = "not a multipart body"
)

type trackingReader struct {
	body string
	read bool
}

func (r *trackingReader) Read(p []byte) (int, error) {
	r.read = true
	if r.body == "" {
		return 0, io.EOF
	}
	n := copy(p, r.body)
	r.body = r.body[n:]
	return n, nil
}

func TestServer_RouteMatrix_AuthAndMethods(t *testing.T) {
	t.Parallel()
	ts, tok, _, _ := newRouteServer(t, "postgresql:single@18", provider.FamilyTabular, true)
	t.Cleanup(ts.Close)

	for _, c := range routeMatrix() {
		t.Run(c.name+"/auth", func(t *testing.T) {
			t.Parallel()
			req := matrixRequest(t, c)
			resp := doReq(t, ts, req, "", c.confirm)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s without bearer = %d, want 401", c.name, resp.StatusCode)
			}
		})
		t.Run(c.name+"/allowed-method", func(t *testing.T) {
			t.Parallel()
			req := matrixRequest(t, c)
			resp := doReq(t, ts, req, tok, c.confirm)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == http.StatusMethodNotAllowed {
				t.Fatalf("%s %s returned 405, want route method allowed", c.name, c.method)
			}
		})
		t.Run(c.name+"/wrong-method", func(t *testing.T) {
			t.Parallel()
			req := matrixRequest(t, c)
			req.Method = wrongMethod(c.method)
			resp := doReq(t, ts, req, tok, c.confirm)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("%s wrong method %s = %d, want 405", c.name, req.Method, resp.StatusCode)
			}
			if allow := resp.Header.Get("Allow"); allow == "" || !strings.Contains(allow, c.method) {
				t.Fatalf("%s Allow = %q, want it to include %s", c.name, allow, c.method)
			}
		})
	}
}

func TestServer_RouteMatrix_WriteGateBeforeDecodeAndProvider(t *testing.T) {
	t.Parallel()

	for _, c := range routeMatrix() {
		if !c.mutating {
			continue
		}
		for _, confirm := range []bool{false, true} {
			t.Run(c.name+"/readonly-confirm-"+boolName(confirm), func(t *testing.T) {
				t.Parallel()
				ts, tok, rec, host := newRouteServer(t, "postgresql:single@18", provider.FamilyTabular, false)
				defer ts.Close()
				reader := &trackingReader{body: c.body}
				req := matrixRequestWithBody(t, c, reader)
				resp := doReq(t, ts, req, tok, confirm)
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("%s readonly confirm=%v = %d, want 403", c.name, confirm, resp.StatusCode)
				}
				if reader.read {
					t.Fatalf("%s read request body before rejecting readonly write", c.name)
				}
				if host.connectionCalls != 0 || rec.lastOp != "" {
					t.Fatalf("%s reached provider before write gate: connectionCalls=%d lastOp=%q", c.name, host.connectionCalls, rec.lastOp)
				}
			})
		}
	}
}

func TestServer_RouteMatrix_ConfirmIsIntentOnly(t *testing.T) {
	t.Parallel()

	for _, c := range routeMatrix() {
		if !c.mutating {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ts, tok, _, _ := newRouteServer(t, "postgresql:single@18", provider.FamilyTabular, true)
			defer ts.Close()
			req := matrixRequest(t, c)
			resp := doReq(t, ts, req, tok, false)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("%s without confirm = %d, want 409", c.name, resp.StatusCode)
			}
		})
	}
}

// TestServer_APIRoutes_ActionMutatingCoherence pins the S23 invariant: `action`
// and `mutating` are dual-owned per route entry (server.go:apiRoutes), so a hand
// edit could set one without the other. This walks the REAL route table and
// checks, per entry: any action carried is Go-owned (provider.AllActionIDs);
// mutating:true routes carry an action that IS in provider.MutatingActionIDs
// (the write side can never be actionless or carry a read action); mutating:false
// routes carry either no action or a read-only one (never a mutating action id).
func TestServer_APIRoutes_ActionMutatingCoherence(t *testing.T) {
	t.Parallel()
	known := map[provider.ActionID]bool{}
	for _, id := range provider.AllActionIDs() {
		known[id] = true
	}
	mutatingIDs := map[provider.ActionID]bool{}
	for _, id := range provider.MutatingActionIDs() {
		mutatingIDs[id] = true
	}

	routes := (&Server{}).apiRoutes()
	for _, rt := range routes {
		name := rt.pattern + " " + strings.Join(rt.methods, "|")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if rt.action != "" && !known[rt.action] {
				t.Fatalf("action %q is not in provider.AllActionIDs()", rt.action)
			}
			switch {
			case rt.mutating && rt.action == "":
				t.Fatal("mutating:true route carries no action")
			case rt.mutating && !mutatingIDs[rt.action]:
				t.Fatalf("mutating:true route action %q is not in provider.MutatingActionIDs()", rt.action)
			case !rt.mutating && mutatingIDs[rt.action]:
				t.Fatalf("mutating:false route carries mutating action %q", rt.action)
			}
		})
	}
}

func matrixRequest(t *testing.T, c routeMatrixCase) *http.Request {
	t.Helper()
	return matrixRequestWithBody(t, c, strings.NewReader(c.body))
}

func matrixRequestWithBody(t *testing.T, c routeMatrixCase, body io.Reader) *http.Request {
	t.Helper()
	req := newRequest(t, c.method, c.path, body)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	return req
}

func wrongMethod(allowed string) string {
	if allowed == http.MethodPatch {
		return http.MethodOptions
	}
	return http.MethodPatch
}

func boolName(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestServer_RowInsert(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "postgresql:single@18", provider.FamilyTabular, true)
	defer ts.Close()
	body := `{"path":{"service":"svc","segments":["public","t"]},"row":{"a":1}}`
	if r := do(t, ts, "POST", "/api/row", tok, body, true); r.code != http.StatusOK {
		t.Fatalf("insert = %d, want 200", r.code)
	}
	if rec.lastOp != "insert" {
		t.Fatalf("provider not reached: lastOp=%q", rec.lastOp)
	}
}

func TestServer_RowDelete_Tabular(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "postgresql:single@18", provider.FamilyTabular, true)
	defer ts.Close()
	body := `{"path":{"service":"svc","segments":["public","t"]},"key":{"id":1}}`
	if r := do(t, ts, "DELETE", "/api/row", tok, body, true); r.code != http.StatusOK {
		t.Fatalf("delete row = %d, want 200", r.code)
	}
	if rec.lastOp != "deleterow" {
		t.Fatalf("DeleteRow not reached: lastOp=%q", rec.lastOp)
	}
}

func TestServer_Row_WriteGate(t *testing.T) {
	t.Parallel()
	ts, tok, _, _ := newRouteServer(t, "postgresql:single@18", provider.FamilyTabular, false)
	defer ts.Close()
	body := `{"path":{"service":"svc","segments":["public","t"]},"row":{"a":1}}`
	if r := do(t, ts, "POST", "/api/row", tok, body, true); r.code != http.StatusForbidden {
		t.Fatalf("read-only insert = %d, want 403", r.code)
	}
}

func TestServer_Entry_SetAndDelete(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "valkey@7", provider.FamilyKV, true)
	defer ts.Close()
	set := `{"path":{"service":"svc","segments":["h"]},"field":"a","value":"Mg=="}`
	if r := do(t, ts, "PUT", "/api/entry", tok, set, true); r.code != http.StatusOK {
		t.Fatalf("set entry = %d, want 200", r.code)
	}
	if rec.lastOp != "setentry" {
		t.Fatalf("SetEntry not reached: lastOp=%q", rec.lastOp)
	}
	del := `{"path":{"service":"svc","segments":["h"]},"field":"a"}`
	if r := do(t, ts, "DELETE", "/api/entry", tok, del, true); r.code != http.StatusOK {
		t.Fatalf("delete entry = %d, want 200", r.code)
	}
	if rec.lastOp != "delentry" {
		t.Fatalf("DeleteEntry not reached: lastOp=%q", rec.lastOp)
	}
}

func TestServer_Rename(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "object-storage", provider.FamilyObject, true)
	defer ts.Close()
	body := `{"from":{"service":"svc","segments":["a.txt"]},"to":{"service":"svc","segments":["b.txt"]}}`
	if r := do(t, ts, "POST", "/api/rename", tok, body, true); r.code != http.StatusOK {
		t.Fatalf("rename = %d, want 200", r.code)
	}
	if rec.lastOp != "rename" {
		t.Fatalf("Rename not reached: lastOp=%q", rec.lastOp)
	}
}

func TestServer_Upload(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "object-storage", provider.FamilyObject, true)
	defer ts.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("service", "svc")
	_ = mw.WriteField("segs", `["new.txt"]`)
	fw, _ := mw.CreateFormFile("file", "new.txt")
	_, _ = fw.Write([]byte("hello"))
	_ = mw.Close()

	req := newRequest(t, http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp := doReq(t, ts, req, tok, true)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload = %d, want 200", resp.StatusCode)
	}
	if rec.lastOp != "write" {
		t.Fatalf("WriteBlob not reached: lastOp=%q", rec.lastOp)
	}
}

// TestServer_Upload_RefusesNonObjectFamily pins resolution 3 (S12): "upload a
// file as a JSON document" has no clear meaning for es/meili/typesense, so
// familyMutatingActionIDs(FamilyDocument) never advertises ActionUploadObject
// — but document.Provider also happens to satisfy the WriteBlob shape
// (document.go's package doc: "maps cleanly onto provider.ObjectProvider"),
// so without a server-side family check /api/upload would silently accept a
// document-family service anyway, even though the SPA never offers it. The
// advertised action set and the server's actual enforcement must agree.
func TestServer_Upload_RefusesNonObjectFamily(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "elasticsearch", provider.FamilyDocument, true)
	defer ts.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("service", "svc")
	_ = mw.WriteField("segs", `["idx","1"]`)
	fw, _ := mw.CreateFormFile("file", "doc.json")
	_, _ = fw.Write([]byte(`{}`))
	_ = mw.Close()

	req := newRequest(t, http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp := doReq(t, ts, req, tok, true)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("upload to document family = %d, want 422 (unsupported)", resp.StatusCode)
	}
	if rec.lastOp == "write" {
		t.Fatalf("WriteBlob reached for a non-object family — upload is object-only")
	}
}

// TestServer_Upload_UnknownService_StaysNotFound pins that the family gate
// above does not shadow the pre-existing ProviderFor 404 for a service that
// does not exist at all — that is a more precise, more honest error than
// "unsupported" for a caller who simply misspelled the hostname.
func TestServer_Upload_UnknownService_StaysNotFound(t *testing.T) {
	t.Parallel()
	ts, tok, _, _ := newRouteServer(t, "object-storage", provider.FamilyObject, true)
	defer ts.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("service", "does-not-exist")
	_ = mw.WriteField("segs", `["new.txt"]`)
	fw, _ := mw.CreateFormFile("file", "new.txt")
	_, _ = fw.Write([]byte("hello"))
	_ = mw.Close()

	req := newRequest(t, http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp := doReq(t, ts, req, tok, true)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("upload to unknown service = %d, want 404", resp.StatusCode)
	}
}

// TestServer_ActionsPayload_ReflectsPolicy reads the /api/services BODY and pins
// the single-owner contract: per-service actions + top-level allowWrites track
// the launch posture.
func TestServer_ActionsPayload_ReflectsPolicy(t *testing.T) {
	t.Parallel()
	type svc struct {
		Hostname string            `json:"hostname"`
		Actions  []provider.Action `json:"actions"`
	}
	type resp struct {
		AllowWrites bool  `json:"allowWrites"`
		Services    []svc `json:"services"`
	}
	read := func(t *testing.T, allowWrites bool) resp {
		t.Helper()
		ts, tok, _, _ := newRouteServer(t, "object-storage", provider.FamilyObject, allowWrites)
		defer ts.Close()
		req := newRequest(t, http.MethodGet, "/api/services", nil)
		r := doReq(t, ts, req, tok, false)
		defer func() { _ = r.Body.Close() }()
		var out resp
		if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}
	rw := read(t, true)
	if !rw.AllowWrites || len(rw.Services) != 1 ||
		!actionEnabled(rw.Services[0].Actions, provider.ActionWriteBlob) ||
		!actionEnabled(rw.Services[0].Actions, provider.ActionUploadObject) {
		t.Fatalf("writes-on actions payload wrong: %+v", rw)
	}
	ro := read(t, false)
	if ro.AllowWrites ||
		actionEnabled(ro.Services[0].Actions, provider.ActionWriteBlob) ||
		actionEnabled(ro.Services[0].Actions, provider.ActionUploadObject) ||
		actionReason(ro.Services[0].Actions, provider.ActionWriteBlob) == "" {
		t.Fatalf("writes-off actions payload wrong: %+v", ro)
	}
}

func actionEnabled(actions []provider.Action, id provider.ActionID) bool {
	for _, a := range actions {
		if a.ID == id {
			return a.Enabled
		}
	}
	return false
}

func actionReason(actions []provider.Action, id provider.ActionID) string {
	for _, a := range actions {
		if a.ID == id {
			return a.Reason
		}
	}
	return ""
}

func TestServer_CSP_FrameAncestorsNone(t *testing.T) {
	t.Parallel()
	rec := &recProvider{readOnly: true}
	factories := map[provider.Family]console.Factory{
		provider.FamilyObject: func(console.ConnectionInfo, *safety.Policy) (provider.Provider, error) { return rec, nil },
	}
	eng := console.NewEngine(&recHost{svcType: "object-storage"}, writePolicy(false), factories)
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := New(eng, "secret", fstest.MapFS{"index.html": {Data: []byte("ok")}})
	ts := &testServer{handler: srv.Handler()}
	defer ts.Close()
	r := do(t, ts, "GET", "/", "secret", "", false)
	csp := r.header.Get("Content-Security-Policy")
	// The console is never iframed (native webview content / standalone tab).
	if !strings.Contains(csp, "frame-ancestors 'none';") {
		t.Fatalf("CSP must forbid all framing: %q", csp)
	}
}
