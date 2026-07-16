package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
)

// fakeObject is an in-memory ObjectProvider for the handler tests.
type fakeObject struct {
	blobs    map[string][]byte
	readOnly bool
}

func (f *fakeObject) Kind() string { return "object-storage" }
func (f *fakeObject) Caps() provider.Capabilities {
	return provider.Capabilities{Family: provider.FamilyObject, Support: provider.SupportFull, MaxInlineBytes: 1 << 20, ReadOnly: f.readOnly}
}
func (f *fakeObject) Health(context.Context) error { return nil }
func (f *fakeObject) Close() error                 { return nil }
func (f *fakeObject) List(_ context.Context, _ provider.Path, _ provider.Page) ([]provider.Node, string, error) {
	return nil, "", nil
}
func (f *fakeObject) Stat(_ context.Context, p provider.Path) (provider.Node, error) {
	return provider.Node{Name: "x", Kind: provider.KindBlob, Path: p}, nil
}
func (f *fakeObject) ReadBlob(_ context.Context, p provider.Path) ([]byte, provider.BlobMeta, error) {
	key := strings.Join(p.Segments, "/")
	b, ok := f.blobs[key]
	if !ok {
		return nil, provider.BlobMeta{}, provider.ErrNotFound
	}
	return b, provider.BlobMeta{ContentType: "text/plain", Size: int64(len(b))}, nil
}
func (f *fakeObject) WriteBlob(_ context.Context, p provider.Path, data []byte) error {
	if f.readOnly {
		return provider.ErrReadOnly
	}
	f.blobs[strings.Join(p.Segments, "/")] = data
	return nil
}
func (f *fakeObject) Delete(_ context.Context, p provider.Path) error {
	delete(f.blobs, strings.Join(p.Segments, "/"))
	return nil
}

type fakeHost struct{}

func (fakeHost) Project(context.Context) (console.ProjectRef, error) {
	return console.ProjectRef{ID: "p1", Name: "proj"}, nil
}
func (fakeHost) ManagedServices(context.Context) ([]console.ManagedServiceRef, error) {
	return []console.ManagedServiceRef{{ID: "s1", Hostname: "store", Type: "object-storage", Status: "ACTIVE"}}, nil
}
func (fakeHost) ConnectionInfo(context.Context, string) (console.ConnectionInfo, error) {
	return testConnectionInfo("object-storage"), nil
}

func testConnectionInfo(typ string) console.ConnectionInfo {
	family := provider.Classify(typ)
	var desc provider.ConnectionDescriptor
	switch family {
	case provider.FamilyObject:
		desc = provider.ObjectConn{}
	case provider.FamilyTabular:
		desc = provider.SQLConn{Driver: "pgx", Dialect: provider.BaseType(typ)}
	case provider.FamilyKV:
		desc = provider.KVConn{}
	case provider.FamilyDocument:
		desc = provider.DocumentConn{Engine: provider.BaseType(typ)}
	case provider.FamilyStream:
		desc = provider.StreamConn{Engine: provider.BaseType(typ)}
	case provider.FamilyFile, provider.FamilyUnknown:
		desc = nil
	default:
		desc = nil
	}
	return console.ConnectionInfo{Type: typ, Family: family, Descriptor: desc}
}

// writeSecret is the write token a write-capable test server mints. The request
// helpers present it on every call (harmless on reads / read-only servers) so a
// write-capable server's mutating routes are reachable — mirroring the embed host,
// which holds the token and attaches it per mutating request.
const writeSecret = "write-token-secret"

// writePolicy builds the process policy the way cmd does — arming-permitted ==
// allowWrites — minting the write token (writeSecret) ONLY for a write-capable
// server (a read-only process mints none). The caller-bound boundary itself
// (bearer-only refused, wrong/absent token refused, launch-ceiling, dual-client) is
// pinned in server_writetoken_test.go, so presenting the token here hides nothing.
func writePolicy(allowWrites bool) *safety.Policy {
	writeToken := ""
	if allowWrites {
		writeToken = writeSecret
	}
	return safety.NewPolicy(allowWrites, writeToken, "")
}

func newTestServer(t *testing.T, allowWrites bool) (*testServer, string) {
	t.Helper()
	fake := &fakeObject{blobs: map[string][]byte{}, readOnly: !allowWrites}
	factories := map[provider.Family]console.Factory{
		provider.FamilyObject: func(console.ConnectionInfo, *safety.Policy) (provider.Provider, error) { return fake, nil },
	}
	eng := console.NewEngine(fakeHost{}, writePolicy(allowWrites), factories)
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	srv := New(eng, "secret", fstest.MapFS{"index.html": {Data: []byte("ok")}})
	return &testServer{handler: srv.Handler()}, "secret"
}

// result is the closed-body view of a response (status + headers), so no call
// site leaves a body open (satisfies bodyclose).
type result struct {
	code   int
	header http.Header
}

type testServer struct {
	handler http.Handler
}

func (s *testServer) Close() {}

func do(t *testing.T, ts *testServer, method, path, token, body string, confirm bool) result {
	t.Helper()
	req := httptest.NewRequest(method, "http://dataconsole.test"+path, strings.NewReader(body))
	resp := doReq(t, ts, req, token, confirm)
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return result{code: resp.StatusCode, header: resp.Header.Clone()}
}

func doReq(t *testing.T, ts *testServer, req *http.Request, token string, confirm bool) *http.Response {
	t.Helper()
	if req.Body == nil {
		req.Body = http.NoBody
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if confirm {
		req.Header.Set("X-Confirm", "true")
	}
	// Present the write capability the embed host holds. The server checks it only on
	// mutating routes (and only when arming is permitted), so it is inert on reads and
	// on read-only servers; the caller-bound refusals are in server_writetoken_test.go.
	req.Header.Set("X-Write-Token", writeSecret)
	rr := httptest.NewRecorder()
	ts.handler.ServeHTTP(rr, req)
	return rr.Result()
}

func newRequest(t *testing.T, method, path string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, "http://dataconsole.test"+path, body)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestServer_AuthGate(t *testing.T) {
	t.Parallel()
	ts, tok := newTestServer(t, false)
	defer ts.Close()
	if r := do(t, ts, "GET", "/api/services", "", "", false); r.code != http.StatusUnauthorized {
		t.Fatalf("no-bearer = %d, want 401", r.code)
	}
	if r := do(t, ts, "GET", "/api/services", "wrong", "", false); r.code != http.StatusUnauthorized {
		t.Fatalf("bad-bearer = %d, want 401", r.code)
	}
	if r := do(t, ts, "GET", "/api/services", tok, "", false); r.code != http.StatusOK {
		t.Fatalf("good-bearer = %d, want 200", r.code)
	}
}

func TestServer_BlobHardeningHeaders(t *testing.T) {
	t.Parallel()
	ts, tok := newTestServer(t, true)
	defer ts.Close()
	// seed one blob
	do(t, ts, "PUT", "/api/blob", tok, `{"path":{"service":"store","segments":["a.txt"]},"data":"aGk="}`, true)
	r := do(t, ts, "GET", `/api/blob?service=store&segs=%5B%22a.txt%22%5D`, tok, "", false)
	if r.code != http.StatusOK {
		t.Fatalf("read = %d", r.code)
	}
	if got := r.header.Get("Content-Disposition"); got != "attachment" {
		t.Errorf("Content-Disposition = %q, want attachment", got)
	}
	if got := r.header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff missing, got %q", got)
	}
	if got := r.header.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want octet-stream", got)
	}
}

func TestServer_WriteGate(t *testing.T) {
	t.Parallel()
	body := `{"path":{"service":"store","segments":["w.txt"]},"data":"aGk="}`

	roTS, roTok := newTestServer(t, false)
	defer roTS.Close()
	if r := do(t, roTS, "PUT", "/api/blob", roTok, body, true); r.code != http.StatusForbidden {
		t.Fatalf("writes-off PUT = %d, want 403", r.code)
	}

	rwTS, rwTok := newTestServer(t, true)
	defer rwTS.Close()
	if r := do(t, rwTS, "PUT", "/api/blob", rwTok, body, false); r.code != http.StatusConflict {
		t.Fatalf("no-confirm PUT = %d, want 409", r.code)
	}
	if r := do(t, rwTS, "PUT", "/api/blob", rwTok, body, true); r.code != http.StatusOK {
		t.Fatalf("confirmed PUT = %d, want 200", r.code)
	}
}

func TestServer_UnsupportedFamily(t *testing.T) {
	t.Parallel()
	// A service whose family has no factory → ProviderFor returns ErrUnsupported.
	eng := console.NewEngine(unsupportedHost{}, writePolicy(false), map[provider.Family]console.Factory{})
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := New(eng, "s", fstest.MapFS{})
	ts := &testServer{handler: srv.Handler()}
	defer ts.Close()
	r := do(t, ts, "GET", "/api/tree?service=q", "s", "", false)
	if r.code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported tree = %d, want 422", r.code)
	}
}

type unsupportedHost struct{}

func (unsupportedHost) Project(context.Context) (console.ProjectRef, error) {
	return console.ProjectRef{}, nil
}
func (unsupportedHost) ManagedServices(context.Context) ([]console.ManagedServiceRef, error) {
	return []console.ManagedServiceRef{{ID: "q1", Hostname: "q", Type: "qdrant", Status: "ACTIVE"}}, nil
}
func (unsupportedHost) ConnectionInfo(context.Context, string) (console.ConnectionInfo, error) {
	return testConnectionInfo("qdrant"), nil
}
