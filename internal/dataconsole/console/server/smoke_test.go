package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
	"github.com/zeropsio/zcp/internal/dataconsole/console/webui"
)

const smokeToken = "smoke-token"

type smokeHost struct {
	connectionCalls int
}

func (h *smokeHost) Project(context.Context) (console.ProjectRef, error) {
	return console.ProjectRef{ID: "project-1", Name: "Smoke Project"}, nil
}

func (h *smokeHost) ManagedServices(context.Context) ([]console.ManagedServiceRef, error) {
	return []console.ManagedServiceRef{{
		ID:       "svc-1",
		Hostname: "db",
		Type:     "postgresql:single@18",
		Status:   "ACTIVE",
	}}, nil
}

func (h *smokeHost) ConnectionInfo(context.Context, string) (console.ConnectionInfo, error) {
	h.connectionCalls++
	return testConnectionInfo("postgresql:single@18"), nil
}

type smokeTabularProvider struct {
	readOnly       bool
	healthCalls    int
	readTableCalls int
	editCellCalls  int
	lastEdit       provider.CellEdit
}

func (p *smokeTabularProvider) Kind() string { return "smoke-tabular" }

func (p *smokeTabularProvider) Caps() provider.Capabilities {
	return provider.Capabilities{
		Family:         provider.FamilyTabular,
		Support:        provider.SupportFull,
		Query:          true,
		EditTabular:    !p.readOnly,
		MaxInlineBytes: 1 << 20,
		ReadOnly:       p.readOnly,
	}
}

func (p *smokeTabularProvider) Health(context.Context) error {
	p.healthCalls++
	return nil
}

func (p *smokeTabularProvider) Close() error { return nil }

func (p *smokeTabularProvider) ReadTable(context.Context, provider.Path, provider.Page) (provider.TablePage, error) {
	p.readTableCalls++
	return provider.TablePage{
		Columns: []provider.Column{
			{Name: "id", DataType: "integer", PK: true},
			{Name: "name", DataType: "text"},
		},
		Rows:       [][]any{{float64(1), "Ada"}},
		RowKeyCols: []string{"id"},
	}, nil
}

func (p *smokeTabularProvider) EditCell(_ context.Context, edit provider.CellEdit) (provider.Applied, error) {
	p.editCellCalls++
	p.lastEdit = edit
	if p.readOnly {
		return provider.Applied{}, provider.ErrReadOnly
	}
	return provider.Applied{Statement: "UPDATE smoke", Affected: 1}, nil
}

type smokeRig struct {
	handler    http.Handler
	host       *smokeHost
	provider   *smokeTabularProvider
	buildCalls int
}

func newSmokeRig(t *testing.T, allowWrites bool) *smokeRig {
	t.Helper()
	host := &smokeHost{}
	prov := &smokeTabularProvider{readOnly: !allowWrites}
	rig := &smokeRig{host: host, provider: prov}
	factories := map[provider.Family]console.Factory{
		provider.FamilyTabular: func(console.ConnectionInfo, safety.Policy) (provider.Provider, error) {
			rig.buildCalls++
			return prov, nil
		},
	}
	eng := console.NewEngine(host, safety.Policy{AllowWrites: allowWrites}, factories)
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	srv := NewWithDiagnostics(eng, smokeToken, webui.FS(), io.Discard)
	rig.handler = srv.Handler()
	return rig
}

// TestServerSmoke_StandaloneUnderProxyPrefix pins that the SPA serves correctly
// behind a path prefix — the standalone console opened in code-server via its
// /proxy/<port>/ forward. Assets + /api must resolve relative to the mount.
func TestServerSmoke_StandaloneUnderProxyPrefix(t *testing.T) {
	rig := newSmokeRig(t, false)
	const prefix = "/proxy/43117"
	ts := newSmokeHTTPServer(t, rig.handler, prefix)
	defer ts.Close()

	unauth := smokeDo(t, ts.Client(), http.MethodGet, ts.URL+"/api/services", "", nil, false)
	if unauth.status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /api/services = %d, want 401", unauth.status)
	}
	assertSecurityHeaders(t, unauth.header)

	services := smokeDo(t, ts.Client(), http.MethodGet, ts.URL+"/api/services", smokeToken, nil, false)
	if services.status != http.StatusOK {
		t.Fatalf("authenticated /api/services = %d, want 200: %s", services.status, services.body)
	}
	assertSecurityHeaders(t, services.header)
	servicePayload := decodeServices(t, services.body)
	if len(servicePayload.Services) != 1 {
		t.Fatalf("/api/services returned %d services, want 1", len(servicePayload.Services))
	}
	svc := servicePayload.Services[0]
	if svc.Hostname != "db" || svc.Family != provider.FamilyTabular {
		t.Fatalf("service = hostname %q family %q, want db/tabular", svc.Hostname, svc.Family)
	}
	if !actionEnabled(svc.Actions, provider.ActionReadTable) {
		t.Fatalf("readTable action is not enabled: %+v", svc.Actions)
	}
	if actionEnabled(svc.Actions, provider.ActionEditCell) || actionReason(svc.Actions, provider.ActionEditCell) == "" {
		t.Fatalf("read-only editCell action should be disabled with a reason: %+v", svc.Actions)
	}

	root := smokeDo(t, ts.Client(), http.MethodGet, ts.URL+"/", "", nil, false)
	if root.status != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", root.status)
	}
	if ct := root.header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET / Content-Type = %q, want text/html", ct)
	}
	assertSecurityHeaders(t, root.header)
	html := string(root.body)
	if !strings.Contains(html, "Data Console") {
		t.Fatalf("GET / did not serve the SPA index")
	}

	prefixedRoot := smokeDo(t, ts.Client(), http.MethodGet, ts.URL+prefix+"/", "", nil, false)
	if prefixedRoot.status != http.StatusOK {
		t.Fatalf("GET %s/ = %d, want 200", prefix, prefixedRoot.status)
	}
	refs := assetRefs(t, string(prefixedRoot.body))
	assertAssetSet(t, refs)
	for _, ref := range refs {
		if strings.HasPrefix(ref, "/") || strings.Contains(ref, "://") || strings.Contains(ref, "..") {
			t.Fatalf("SPA asset ref %q is not relative to the proxy mount", ref)
		}
		asset := smokeDo(t, ts.Client(), http.MethodGet, ts.URL+prefix+"/"+ref, "", nil, false)
		if asset.status != http.StatusOK {
			t.Fatalf("GET %s/%s = %d, want 200", prefix, ref, asset.status)
		}
		if want := expectedAssetContentType(ref); want != "" && !strings.Contains(asset.header.Get("Content-Type"), want) {
			t.Fatalf("%s Content-Type = %q, want %s", ref, asset.header.Get("Content-Type"), want)
		}
		if ref == "app.js" {
			assertRelativeFetchClient(t, string(asset.body))
		}
	}

	prefixedAPI := smokeDo(t, ts.Client(), http.MethodGet, ts.URL+prefix+"/api/services", smokeToken, nil, false)
	if prefixedAPI.status != http.StatusOK {
		t.Fatalf("GET %s/api/services = %d, want 200: %s", prefix, prefixedAPI.status, prefixedAPI.body)
	}

	editBody := []byte(`{"path":{"service":"db","segments":["public","users"]},"rowKey":{"id":1},"column":"name","newValue":"Grace","expectedOld":"Ada"}`)
	gated := smokeDo(t, ts.Client(), http.MethodPost, ts.URL+"/api/cell", smokeToken, bytes.NewReader(editBody), true)
	if gated.status != http.StatusForbidden {
		t.Fatalf("read-only /api/cell = %d, want 403", gated.status)
	}
	if rig.buildCalls != 0 || rig.host.connectionCalls != 0 || rig.provider.editCellCalls != 0 {
		t.Fatalf("read-only write reached provider path: builds=%d connections=%d edits=%d", rig.buildCalls, rig.host.connectionCalls, rig.provider.editCellCalls)
	}

	table := smokeDo(t, ts.Client(), http.MethodGet, ts.URL+`/api/table?service=db&segs=%5B%22public%22,%22users%22%5D`, smokeToken, nil, false)
	if table.status != http.StatusOK {
		t.Fatalf("GET /api/table = %d, want 200: %s", table.status, table.body)
	}
	if rig.provider.readTableCalls != 1 {
		t.Fatalf("ReadTable calls = %d, want 1", rig.provider.readTableCalls)
	}

	rwRig := newSmokeRig(t, true)
	rwTS := newSmokeHTTPServer(t, rwRig.handler, prefix)
	defer rwTS.Close()
	wrote := smokeDo(t, rwTS.Client(), http.MethodPost, rwTS.URL+"/api/cell", smokeToken, bytes.NewReader(editBody), true)
	if wrote.status != http.StatusOK {
		t.Fatalf("write-enabled /api/cell = %d, want 200: %s", wrote.status, wrote.body)
	}
	if rwRig.provider.editCellCalls != 1 || rwRig.provider.lastEdit.Column != "name" {
		t.Fatalf("write did not reach fake provider once: edits=%d last=%+v", rwRig.provider.editCellCalls, rwRig.provider.lastEdit)
	}
}

func TestServerSmoke_CSPFrameAncestorsNone(t *testing.T) {
	rig := newSmokeRig(t, false)
	ts := newSmokeHTTPServer(t, rig.handler, "")
	defer ts.Close()
	resp := smokeDo(t, ts.Client(), http.MethodGet, ts.URL+"/", "", nil, false)
	assertSecurityHeaders(t, resp.header)
	csp := resp.header.Get("Content-Security-Policy")
	// The console is NEVER iframed (embedded = webview content; standalone =
	// top-level tab), so nothing may frame this origin.
	if !strings.Contains(csp, "frame-ancestors 'none';") {
		t.Fatalf("CSP = %q, want frame-ancestors 'none'", csp)
	}
	if strings.Contains(csp, "vscode-webview") {
		t.Fatalf("CSP still references the removed embed-iframe policy: %q", csp)
	}
}

// TestNginxTemplate_NoDCProxyTunnel locks the live-proven unauthenticated bypass
// closed: the Data Console is embedded natively (a WebviewPanel, brokered through
// the extension host), never proxied. The old gate-exempt `location /dcproxy/`
// rewrote to code-server's generic /proxy/<port>/ and let an unauthenticated
// visitor reach ANY localhost port (incl. code-server --auth none). It must stay
// gone, and code-server (:8081) must be reachable ONLY from the cookie-gated root.
func TestNginxTemplate_NoDCProxyTunnel(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoInternal := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	templatePath := filepath.Join(repoInternal, "content", "templates", "nginx.conf.tmpl")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read nginx template: %v", err)
	}
	tmpl := string(data)
	for _, forbidden := range []string{"/dcproxy/", "rewrite ^/dcproxy"} {
		if strings.Contains(tmpl, forbidden) {
			t.Fatalf("nginx template re-introduced the console tunnel (%q) — the console is native now, never proxied", forbidden)
		}
	}
	// code-server's generic /proxy/<port>/ is reachable only behind the cookie
	// gate: exactly one proxy_pass to :8081, and it lives in the gated `location /`.
	if n := strings.Count(tmpl, "proxy_pass http://127.0.0.1:8081;"); n != 1 {
		t.Fatalf("code-server (:8081) must be proxied from exactly one gated location; found %d proxy_pass", n)
	}
}

type smokeResponse struct {
	status int
	header http.Header
	body   []byte
}

type smokeHTTPServer struct {
	URL    string
	client *http.Client
	close  func()
}

func (s *smokeHTTPServer) Client() *http.Client { return s.client }

func (s *smokeHTTPServer) Close() {
	if s.close != nil {
		s.close()
	}
}

func newSmokeHTTPServer(t *testing.T, handler http.Handler, prefix string) *smokeHTTPServer {
	t.Helper()
	mux := http.NewServeMux()
	if prefix != "" {
		mux.Handle(prefix+"/", http.StripPrefix(prefix, handler))
	}
	mux.Handle("/", handler)
	ts, err := tryNewHTTPTestServer(mux)
	if err == nil {
		return &smokeHTTPServer{URL: ts.URL, client: ts.Client(), close: ts.Close}
	}
	if !isListenPermissionError(err) {
		t.Fatalf("start httptest server: %v", err)
	}
	t.Logf("httptest.NewServer blocked by sandbox (%v); using in-process HTTP transport", err)
	return &smokeHTTPServer{
		URL:    "http://dataconsole.test",
		client: &http.Client{Transport: handlerTransport{handler: mux}},
		close:  func() {},
	}
}

func tryNewHTTPTestServer(handler http.Handler) (srv *httptest.Server, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return httptest.NewServer(handler), nil
}

func isListenPermissionError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "operation not permitted") || strings.Contains(msg, "permission denied")
}

type handlerTransport struct {
	handler http.Handler
}

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rr := httptest.NewRecorder()
	t.handler.ServeHTTP(rr, req)
	resp := rr.Result()
	resp.Request = req
	return resp, nil
}

func smokeDo(t *testing.T, client *http.Client, method, url, token string, body io.Reader, confirm bool) smokeResponse {
	t.Helper()
	hasBody := body != nil
	if body == nil {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if confirm {
		req.Header.Set("X-Confirm", "true")
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return smokeResponse{status: resp.StatusCode, header: resp.Header.Clone(), body: data}
}

type servicesSmokePayload struct {
	AllowWrites bool `json:"allowWrites"`
	Services    []struct {
		Hostname string            `json:"hostname"`
		Family   provider.Family   `json:"family"`
		Actions  []provider.Action `json:"actions"`
	} `json:"services"`
}

func decodeServices(t *testing.T, body []byte) servicesSmokePayload {
	t.Helper()
	var payload servicesSmokePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode /api/services: %v\n%s", err, body)
	}
	return payload
}

func assertSecurityHeaders(t *testing.T, h http.Header) {
	t.Helper()
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	csp := h.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("Content-Security-Policy missing frame-ancestors 'none': %q", csp)
	}
}

var assetRefPattern = regexp.MustCompile(`\b(?:src|href)="([^"]+\.(?:js|css))"`)

func assetRefs(t *testing.T, html string) []string {
	t.Helper()
	matches := assetRefPattern.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatal("SPA index has no script/link assets")
	}
	refs := make([]string, 0, len(matches))
	for _, m := range matches {
		refs = append(refs, m[1])
	}
	return refs
}

func assertAssetSet(t *testing.T, refs []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, ref := range refs {
		seen[ref] = true
	}
	for _, want := range []string{"app.js", "contract.js"} {
		if !seen[want] {
			t.Fatalf("SPA asset refs missing %s: %v", want, refs)
		}
	}
	hasModule := false
	for _, ref := range refs {
		if strings.HasPrefix(ref, "dc-") && strings.HasSuffix(ref, ".js") {
			hasModule = true
		}
	}
	if !hasModule {
		t.Fatalf("SPA asset refs missing dc-*.js module: %v", refs)
	}
}

func expectedAssetContentType(ref string) string {
	switch {
	case strings.HasSuffix(ref, ".js"):
		return "text/javascript"
	case strings.HasSuffix(ref, ".css"):
		return "text/css"
	default:
		return ""
	}
}

func assertRelativeFetchClient(t *testing.T, source string) {
	t.Helper()
	if !strings.Contains(source, `path.replace(/^\//, "")`) || !strings.Contains(source, "fetch(u,") {
		t.Fatalf("app.js no longer strips leading slashes before fetch")
	}
	if strings.Contains(source, `fetch("/`) || strings.Contains(source, `fetch('/`) {
		t.Fatalf("app.js contains an absolute fetch URL")
	}
}
