package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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
	// mu guards the recording fields: route-matrix tests share ONE
	// recProvider across parallel subtests, so handler goroutines record
	// concurrently.
	mu       sync.Mutex
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

// record stores the last mutating call under the lock.
func (r *recProvider) record(op string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastOp = op
}

func (r *recProvider) op() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastOp
}

func (r *recProvider) insertRow() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastInsertRow
}

func (r *recProvider) cellEdit() provider.CellEdit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastCellEdit
}
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
	r.mu.Lock()
	r.lastOp = "editcell"
	r.lastCellEdit = edit
	r.mu.Unlock()
	return provider.Applied{Statement: "UPDATE", Affected: 1}, nil
}

// tabular row
func (r *recProvider) InsertRow(_ context.Context, _ provider.Path, row map[string]any) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.mu.Lock()
	r.lastOp = "insert"
	r.lastInsertRow = row
	r.mu.Unlock()
	return provider.Applied{Statement: "INSERT", Affected: 1}, nil
}
func (r *recProvider) DeleteRow(_ context.Context, _ provider.Path, _ map[string]any) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.record("deleterow")
	return provider.Applied{Statement: "DELETE", Affected: 1}, nil
}

// KV entry
func (r *recProvider) SetEntry(_ context.Context, _ provider.KVEntryEdit) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.record("setentry")
	return provider.Applied{Statement: "HSET", Affected: 1}, nil
}
func (r *recProvider) DeleteEntry(_ context.Context, _ provider.Path, _ string) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.record("delentry")
	return provider.Applied{Statement: "HDEL", Affected: 1}, nil
}

// object blob + rename
func (r *recProvider) WriteBlob(_ context.Context, _ provider.Path, _ []byte, _ string) error {
	if err := r.gate(); err != nil {
		return err
	}
	r.record("write")
	return nil
}
func (r *recProvider) Rename(_ context.Context, _, _ provider.Path) error {
	if err := r.gate(); err != nil {
		return err
	}
	r.record("rename")
	return nil
}
func (r *recProvider) Delete(_ context.Context, _ provider.Path) error {
	if err := r.gate(); err != nil {
		return err
	}
	r.record("delete")
	return nil
}
func (r *recProvider) Stat(_ context.Context, p provider.Path) (provider.Node, error) {
	return provider.Node{Name: "node", Kind: provider.KindBlob, Path: p}, nil
}

// document search (a READ — never gated on the write posture).
func (r *recProvider) Search(_ context.Context, _ provider.Path, q string, _ provider.Page) ([]provider.Node, string, error) {
	r.record("search")
	return []provider.Node{{Name: "hit-" + q, Kind: provider.KindBlob}}, "", nil
}

// KV create
func (r *recProvider) CreateKey(_ context.Context, _ provider.KVCreate) (provider.Applied, error) {
	if err := r.gate(); err != nil {
		return provider.Applied{}, err
	}
	r.record("createkey")
	return provider.Applied{Statement: "HSET", Affected: 1}, nil
}

// document create
func (r *recProvider) CreateDoc(_ context.Context, _ provider.Path, _ []byte) (string, error) {
	if err := r.gate(); err != nil {
		return "", err
	}
	r.record("createdoc")
	return "assigned-1", nil
}

type recHost struct {
	svcType string
	// connectionCalls is atomic: parallel subtests share one server (one
	// recHost) and concurrent handler goroutines reach ConnectionInfo.
	connectionCalls atomic.Int64
}

func (*recHost) Project(context.Context) (console.ProjectRef, error) {
	return console.ProjectRef{ID: "p", Name: "proj"}, nil
}
func (h *recHost) ManagedServices(context.Context) ([]console.ManagedServiceRef, error) {
	return []console.ManagedServiceRef{{ID: "s1", Hostname: "svc", Type: h.svcType, Status: "ACTIVE"}}, nil
}
func (h *recHost) ConnectionInfo(context.Context, string) (console.ConnectionInfo, error) {
	h.connectionCalls.Add(1)
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
		{name: "download", method: http.MethodGet, path: `/api/download?service=svc&segs=%5B%22a.txt%22%5D`},
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
				if host.connectionCalls.Load() != 0 || rec.op() != "" {
					t.Fatalf("%s reached provider before write gate: connectionCalls=%d lastOp=%q", c.name, host.connectionCalls.Load(), rec.op())
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
	if rec.op() != "insert" {
		t.Fatalf("provider not reached: lastOp=%q", rec.op())
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
	if rec.op() != "deleterow" {
		t.Fatalf("DeleteRow not reached: lastOp=%q", rec.op())
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
	if rec.op() != "setentry" {
		t.Fatalf("SetEntry not reached: lastOp=%q", rec.op())
	}
	del := `{"path":{"service":"svc","segments":["h"]},"field":"a"}`
	if r := do(t, ts, "DELETE", "/api/entry", tok, del, true); r.code != http.StatusOK {
		t.Fatalf("delete entry = %d, want 200", r.code)
	}
	if rec.op() != "delentry" {
		t.Fatalf("DeleteEntry not reached: lastOp=%q", rec.op())
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
	if rec.op() != "rename" {
		t.Fatalf("Rename not reached: lastOp=%q", rec.op())
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
	if rec.op() != "write" {
		t.Fatalf("WriteBlob not reached: lastOp=%q", rec.op())
	}
}

// TestServer_Upload_RefusesNonObjectFamily pins resolution 3 (S12): "upload a
// file as a JSON document" has no clear meaning for es/meili/typesense, so
// familyMutatingActionIDs(FamilyDocument) never advertises ActionUploadObject
// — but document.Provider also happens to satisfy the WriteBlob shape
// (document.go's package doc: "maps cleanly onto provider.ObjectProvider"),
// so without providerForWrite's action-policy guard /api/upload would
// silently accept a document-family service anyway, even though the SPA
// never offers it. The advertised action set and the server's actual
// enforcement must agree. An action ABSENT from the family's list is an
// operation that does not exist here → ErrUnsupported (422) — the action
// set is public per-service knowledge (GET /api/services), so this leaks
// nothing, while a read_only answer would send an authorized armed caller
// into a futile re-arm loop (code-review finding 10; spec-dataconsole-
// testing.md §3). Present-but-DISABLED keeps the uniform ErrReadOnly.
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
		t.Fatalf("upload to document family = %d, want 422 (unsupported — action absent from the family's list)", resp.StatusCode)
	}
	if rec.op() == "write" {
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

// actionPolicyBody is a minimally-valid (decodes cleanly, targets hostname
// "svc") request body for one mutating route/method — deliberately separate
// from routeMatrix()'s invalidJSONBody fixtures, which exist specifically to
// prove decode() is never reached (a different test, WriteGateBeforeDecode).
// These bodies exist so a request can run PAST decode() into the
// action-policy guard (or the handler) under test.
func actionPolicyBody(pattern, method string) (string, bool) {
	bodies := map[string]string{
		"/api/blob PUT":             `{"path":{"service":"svc","segments":["a.txt"]},"data":"aGk="}`,
		"/api/blob DELETE":          `{"path":{"service":"svc","segments":["a.txt"]}}`,
		"/api/cell POST":            `{"path":{"service":"svc","segments":["public","t"]},"rowKey":{"id":1},"column":"a","newValue":1,"expectedOld":0}`,
		"/api/cell PUT":             `{"path":{"service":"svc","segments":["public","t"]},"rowKey":{"id":1},"column":"a","newValue":1,"expectedOld":0}`,
		"/api/row POST":             `{"path":{"service":"svc","segments":["public","t"]},"row":{"a":1}}`,
		"/api/row DELETE":           `{"path":{"service":"svc","segments":["public","t"]},"key":{"id":1}}`,
		"/api/entry PUT":            `{"path":{"service":"svc","segments":["h"]},"field":"a","value":"Mg=="}`,
		"/api/entry DELETE":         `{"path":{"service":"svc","segments":["h"]},"field":"a"}`,
		"/api/rename POST":          `{"from":{"service":"svc","segments":["a.txt"]},"to":{"service":"svc","segments":["b.txt"]}}`,
		"/api/ttl PUT":              `{"path":{"service":"svc","segments":["k"]},"ttlSeconds":60}`,
		"/api/kv/create POST":       `{"path":{"service":"svc","segments":["newkey"]},"type":"string","value":"aGk="}`,
		"/api/document/create POST": `{"path":{"service":"svc","segments":["idx"]},"data":"e30="}`,
		"/api/node DELETE":          `{"path":{"service":"svc","segments":["a.txt"]}}`,
	}
	body, ok := bodies[pattern+" "+method]
	return body, ok
}

// actionPolicyRequest builds a minimally-valid request for one mutating
// route/method (Origin set to a loopback address — the guard must still
// refuse with a legitimate embed-host-shaped Origin present, not just the
// no-Origin broker path). Upload is special-cased (multipart, not JSON).
func actionPolicyRequest(t *testing.T, method, pattern string) *http.Request {
	t.Helper()
	var req *http.Request
	if pattern == "/api/upload" {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("service", "svc")
		_ = mw.WriteField("segs", `["new.txt"]`)
		fw, _ := mw.CreateFormFile("file", "new.txt")
		_, _ = fw.Write([]byte("hello"))
		_ = mw.Close()
		req = newRequest(t, method, pattern, &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
	} else {
		body, ok := actionPolicyBody(pattern, method)
		if !ok {
			t.Fatalf("no actionPolicyBody fixture for %s %s — add one so this route stays covered", method, pattern)
		}
		req = newRequest(t, method, pattern, strings.NewReader(body))
	}
	req.Header.Set("Origin", "http://127.0.0.1")
	return req
}

// actionPolicyEnabledFamily names, for every mutating action, a FULL-support
// family/base-type combination that genuinely enables it (independent oracle:
// hand-copied from familyMutatingActionIDs, provider/actions.go) — the
// positive-control fixture proving the guard does not block a legitimately
// enabled action.
func actionPolicyEnabledFamily(action provider.ActionID) (svcType string, fam provider.Family, ok bool) {
	switch action {
	case provider.ActionWriteBlob, provider.ActionDeleteNode, provider.ActionRenameObject, provider.ActionUploadObject:
		return "object-storage", provider.FamilyObject, true
	case provider.ActionEditCell, provider.ActionInsertRow, provider.ActionDeleteRow:
		return "postgresql:single@18", provider.FamilyTabular, true
	case provider.ActionEditKVEntry, provider.ActionSetTTL, provider.ActionCreateKey:
		return "valkey@7", provider.FamilyKV, true
	case provider.ActionCreateDoc:
		return "elasticsearch", provider.FamilyDocument, true
	case provider.ActionReadBlob, provider.ActionQuerySQL, provider.ActionReadTable,
		provider.ActionSearchDocs, provider.ActionShowVPNGate:
		// Read actions never back a mutating route — no positive fixture exists.
		return "", "", false
	default:
		return "", "", false
	}
}

// TestMutatingRoutes_ActionPolicyEnforced_RefusesDisabledAction pins the S2
// server-side action-policy guard (docs/spec-dataconsole-testing.md §3): every
// mutating route must enforce its declared action against the TARGET
// SERVICE's OWN action policy, after resolution, before any provider
// dispatch — a disabled SPA affordance is presentation, this is enforcement.
// The route set is derived from apiRoutes() itself (never a hand-copied
// list), so a new mutating route cannot dodge this check unnoticed: a route
// with no actionPolicyBody fixture fails loudly via actionPolicyRequest,
// rather than being silently skipped.
//
// "shared-storage" (FamilyFile) is the fixture: per provider/actions.go,
// familyReadActionIDs/familyMutatingActionIDs both return nil for
// FamilyFile, and it is not a vpnGateFamily either, so
// ServiceActions(FamilyFile, ...) is the EMPTY list for every possible action
// — regardless of session write posture. That makes it a universal "every
// action ABSENT" double usable for every family's mutating routes alike
// (object-storage has no reduced-support tier, so no per-family view-only
// double generalizes to object routes). Absent ⇒ ErrUnsupported (422),
// per spec-dataconsole-testing.md §3; the present-but-DISABLED ⇒ 403
// read_only branch is pinned separately by the read-only-posture tests
// above. recProvider is wired permissive (readOnly:false via allowWrites:
// true) specifically so the refusal here can only come from the NEW guard,
// never from recProvider's own gate() or the session write-token gate.
func TestMutatingRoutes_ActionPolicyEnforced_RefusesDisabledAction(t *testing.T) {
	t.Parallel()
	for _, rt := range (&Server{}).apiRoutes() {
		if !rt.mutating {
			continue
		}
		for _, method := range rt.methods {
			rt, method := rt, method
			t.Run(rt.pattern+" "+method, func(t *testing.T) {
				t.Parallel()
				ts, tok, rec, _ := newRouteServer(t, "shared-storage", provider.FamilyFile, true)
				defer ts.Close()

				req := actionPolicyRequest(t, method, rt.pattern)
				resp := doReq(t, ts, req, tok, true)
				defer func() { _ = resp.Body.Close() }()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if resp.StatusCode != http.StatusUnprocessableEntity {
					t.Fatalf("%s %s against an action-absent service = %d, want 422; body=%s", method, rt.pattern, resp.StatusCode, body)
				}
				env := decodeEnvelope(t, body)
				if env.Code != "unsupported" {
					t.Fatalf("%s %s envelope code = %q, want unsupported (body=%s)", method, rt.pattern, env.Code, body)
				}
				if env.Service == "" || env.Family == "" {
					t.Fatalf("%s %s envelope missing service/family context: %+v", method, rt.pattern, env)
				}
				if rec.op() != "" {
					t.Fatalf("%s %s reached the provider (lastOp=%q) despite a disabled action", method, rt.pattern, rec.op())
				}
			})
		}
	}
}

// TestMutatingRoutes_ActionPolicyEnforced_AllowsEnabledAction is the positive
// control: the SAME route/method set, targeting a service whose family
// genuinely enables the route's declared action, must reach the handler — a
// 403 here would mean the guard is over-refusing legitimate writes. Per the
// brief, "any non-403 status proves dispatch" (a specific fake provider
// method the handler type-asserts for, e.g. SetTTL, need not exist on
// recProvider — that would 422, not 403, and still proves the guard let the
// request through).
func TestMutatingRoutes_ActionPolicyEnforced_AllowsEnabledAction(t *testing.T) {
	t.Parallel()
	for _, rt := range (&Server{}).apiRoutes() {
		if !rt.mutating {
			continue
		}
		for _, method := range rt.methods {
			rt, method := rt, method
			t.Run(rt.pattern+" "+method, func(t *testing.T) {
				t.Parallel()
				svcType, fam, ok := actionPolicyEnabledFamily(rt.action)
				if !ok {
					t.Fatalf("no actionPolicyEnabledFamily fixture for action %q (route %s %s) — add one so this route stays covered", rt.action, method, rt.pattern)
				}
				ts, tok, _, _ := newRouteServer(t, svcType, fam, true)
				defer ts.Close()

				req := actionPolicyRequest(t, method, rt.pattern)
				resp := doReq(t, ts, req, tok, true)
				defer func() { _ = resp.Body.Close() }()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if resp.StatusCode == http.StatusForbidden {
					t.Fatalf("%s %s against an enabled-action service = 403 (body=%s), want dispatch to reach the handler", method, rt.pattern, body)
				}
			})
		}
	}
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
