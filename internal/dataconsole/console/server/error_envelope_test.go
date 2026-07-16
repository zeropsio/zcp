package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
)

type serverErrorEnvelope struct {
	Code      string            `json:"code"`
	Status    int               `json:"status"`
	Message   string            `json:"message"`
	Service   string            `json:"service"`
	Family    provider.Family   `json:"family"`
	Action    provider.ActionID `json:"action"`
	RequestID string            `json:"requestId"`
}

func TestServer_ErrorEnvelope_SentinelErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		err     error
		code    string
		status  int
		message string
	}{
		{name: "not_found", err: provider.ErrNotFound, code: "not_found", status: http.StatusNotFound, message: "not found"},
		{name: "read_only", err: provider.ErrReadOnly, code: "read_only", status: http.StatusForbidden, message: "read-only"},
		{name: "needs_confirm", err: provider.ErrNeedsConfirm, code: "needs_confirm", status: http.StatusConflict, message: "confirmation required"},
		{name: "conflict", err: provider.ErrConflict, code: "conflict", status: http.StatusConflict, message: "conflict"},
		{name: "too_large", err: provider.ErrTooLarge, code: "too_large", status: http.StatusRequestEntityTooLarge, message: "too large"},
		{name: "unsupported", err: provider.ErrUnsupported, code: "unsupported", status: http.StatusUnprocessableEntity, message: "unsupported"},
		{name: "unreachable", err: provider.ErrUnreachable, code: "unreachable", status: http.StatusServiceUnavailable, message: "unreachable"},
		{name: "upstream", err: provider.ErrUpstream, code: "upstream", status: http.StatusBadGateway, message: "upstream error"},
		{name: "invalid", err: provider.ErrInvalid, code: "invalid", status: http.StatusBadRequest, message: "invalid request"},
		{name: "wrong_type", err: provider.ErrWrongType, code: "wrong_type", status: http.StatusConflict, message: "wrong type"},
		{name: "timeout", err: provider.ErrTimeout, code: "timeout", status: http.StatusGatewayTimeout, message: "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts, tok, logs := newEnvelopeServer(t, fmt.Errorf("provider read: %w", tc.err))
			defer ts.Close()

			resp, body := doEnvelopeReq(t, ts, tok)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, tc.status, body)
			}
			env := decodeEnvelope(t, body)
			assertEnvelopeContext(t, resp, env)
			if env.Code != tc.code || env.Status != tc.status || env.Message != tc.message {
				t.Fatalf("envelope = %+v, want code=%q status=%d message=%q", env, tc.code, tc.status, tc.message)
			}
			logText := logs.String()
			for _, want := range []string{env.RequestID, "GET", "/api/blob", "service=svc", "family=object", "action=readBlob", "provider read"} {
				if !strings.Contains(logText, want) {
					t.Fatalf("diagnostic log %q missing %q", logText, want)
				}
			}
		})
	}
}

func TestServer_ErrorEnvelope_InternalSanitizesAndLogsRawCause(t *testing.T) {
	t.Parallel()
	const rawCause = "driver leaked password=secret cause"
	ts, tok, logs := newEnvelopeServer(t, errors.New(rawCause))
	defer ts.Close()

	resp, body := doEnvelopeReq(t, ts, tok)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), rawCause) {
		t.Fatalf("body leaked raw internal cause: %s", body)
	}
	env := decodeEnvelope(t, body)
	assertEnvelopeContext(t, resp, env)
	if env.Code != "internal" || env.Status != http.StatusInternalServerError || env.Message != "internal error" {
		t.Fatalf("envelope = %+v, want internal/500/generic", env)
	}
	logText := logs.String()
	for _, want := range []string{env.RequestID, rawCause, "GET", "/api/blob", "service=svc", "family=object", "action=readBlob"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("diagnostic log %q missing %q", logText, want)
		}
	}
}

func assertEnvelopeContext(t *testing.T, resp *http.Response, env serverErrorEnvelope) {
	t.Helper()
	headerID := resp.Header.Get("X-Request-Id")
	if headerID == "" {
		t.Fatal("X-Request-Id header is empty")
	}
	if env.RequestID == "" || env.RequestID != headerID {
		t.Fatalf("requestId envelope/header mismatch: envelope=%q header=%q", env.RequestID, headerID)
	}
	if env.Service != "svc" || env.Family != provider.FamilyObject || env.Action != provider.ActionReadBlob {
		t.Fatalf("context envelope = service=%q family=%q action=%q, want svc/object/readBlob", env.Service, env.Family, env.Action)
	}
}

func decodeEnvelope(t *testing.T, body []byte) serverErrorEnvelope {
	t.Helper()
	var env serverErrorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v; body=%s", err, body)
	}
	return env
}

func doEnvelopeReq(t *testing.T, ts *testServer, token string) (*http.Response, []byte) {
	t.Helper()
	req := newRequest(t, http.MethodGet, `/api/blob?service=svc&segs=%5B%22x.txt%22%5D`, nil)
	resp := doReq(t, ts, req, token, false)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp, body
}

func newEnvelopeServer(t *testing.T, err error) (*testServer, string, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	factories := map[provider.Family]console.Factory{
		provider.FamilyObject: func(console.ConnectionInfo, *safety.Policy) (provider.Provider, error) {
			return errorProvider{err: err}, nil
		},
	}
	eng := console.NewEngine(errorHost{}, writePolicy(false), factories)
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	srv := NewWithDiagnostics(eng, "secret", fstest.MapFS{"index.html": {Data: []byte("ok")}}, logs)
	return &testServer{handler: srv.Handler()}, "secret", logs
}

type errorProvider struct {
	err error
}

func (e errorProvider) Kind() string { return "error-provider" }
func (e errorProvider) Caps() provider.Capabilities {
	return provider.Capabilities{Family: provider.FamilyObject, Support: provider.SupportFull}
}
func (e errorProvider) Health(context.Context) error { return nil }
func (e errorProvider) Close() error                 { return nil }
func (e errorProvider) ReadBlob(context.Context, provider.Path) ([]byte, provider.BlobMeta, error) {
	return nil, provider.BlobMeta{}, e.err
}

type errorHost struct{}

func (errorHost) Project(context.Context) (console.ProjectRef, error) {
	return console.ProjectRef{ID: "p", Name: "proj"}, nil
}
func (errorHost) ManagedServices(context.Context) ([]console.ManagedServiceRef, error) {
	return []console.ManagedServiceRef{{ID: "s", Hostname: "svc", Type: "object-storage", Status: "ACTIVE"}}, nil
}
func (errorHost) ConnectionInfo(context.Context, string) (console.ConnectionInfo, error) {
	return testConnectionInfo("object-storage"), nil
}

// ---- KV-AUD-04: service+family on body-addressed routes ----

// bodyRouteCase is one body-addressed route to probe for envelope enrichment.
// Every case's body carries a real "svc" service reference the way a real
// caller sends it (Path.Service / From.Service / a top-level Service field),
// so the only way the envelope's service/family can end up populated is the
// handler itself learning it from the decoded body — withRouteContext only
// ever reads the URL query, which none of these routes populate.
type bodyRouteCase struct {
	name   string
	method string
	path   string
	body   string
}

func bodyRouteCases() []bodyRouteCase {
	return []bodyRouteCase{
		{"blob write", http.MethodPut, "/api/blob", `{"path":{"service":"svc","segments":["x"]},"data":"aGk="}`},
		{"blob delete (node)", http.MethodDelete, "/api/blob", `{"path":{"service":"svc","segments":["x"]}}`},
		{"query", http.MethodPost, "/api/query", `{"service":"svc","stmt":"SELECT 1"}`},
		{"cell edit", http.MethodPost, "/api/cell", `{"path":{"service":"svc","segments":["public","t"]},"column":"a","rowKey":{"id":1}}`},
		{"row insert", http.MethodPost, "/api/row", `{"path":{"service":"svc","segments":["public","t"]},"row":{"a":1}}`},
		{"row delete", http.MethodDelete, "/api/row", `{"path":{"service":"svc","segments":["public","t"]},"key":{"id":1}}`},
		{"entry set", http.MethodPut, "/api/entry", `{"path":{"service":"svc","segments":["h"]},"field":"a","value":"Mg=="}`},
		{"entry delete", http.MethodDelete, "/api/entry", `{"path":{"service":"svc","segments":["h"]},"field":"a"}`},
		{"rename", http.MethodPost, "/api/rename", `{"from":{"service":"svc","segments":["a.txt"]},"to":{"service":"svc","segments":["b.txt"]}}`},
		{"ttl", http.MethodPut, "/api/ttl", `{"path":{"service":"svc","segments":["k"]},"ttlSeconds":60}`},
		{"node delete", http.MethodDelete, "/api/node", `{"path":{"service":"svc","segments":["x"]}}`},
	}
}

// newBodyAddressedServer builds a KV-family server whose write-token gate is
// armed (routeGroup's AuthorizeWrite check — running BEFORE any handler code —
// passes, exactly like a real embed host presenting a valid token) while the
// underlying provider unconditionally refuses every mutation it implements.
// This isolates the thing under test: an error envelope produced AFTER the
// write gate (from the handler/provider, never the gate itself) must carry
// service+family.
func newBodyAddressedServer(t *testing.T) (*testServer, string) {
	t.Helper()
	rec := &recProvider{readOnly: true}
	factories := map[provider.Family]console.Factory{
		provider.FamilyKV: func(console.ConnectionInfo, *safety.Policy) (provider.Provider, error) { return rec, nil },
	}
	host := &recHost{svcType: "valkey@7"}
	eng := console.NewEngine(host, writePolicy(true), factories) // armed: mints a real write token
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	srv := New(eng, "secret", fstest.MapFS{"index.html": {Data: []byte("ok")}})
	return &testServer{handler: srv.Handler()}, "secret"
}

func TestServer_ErrorEnvelope_BodyAddressedRoutesCarryServiceAndFamily(t *testing.T) {
	t.Parallel()
	ts, tok := newBodyAddressedServer(t)
	defer ts.Close()

	for _, c := range bodyRouteCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			req := newRequest(t, c.method, c.path, strings.NewReader(c.body))
			resp := doReq(t, ts, req, tok, true)
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if resp.StatusCode < 400 {
				t.Fatalf("%s: status = %d, want an error; body=%s", c.name, resp.StatusCode, body)
			}
			env := decodeEnvelope(t, body)
			if env.Service != "svc" {
				t.Errorf("%s: envelope service = %q, want %q (body=%s)", c.name, env.Service, "svc", body)
			}
			if env.Family != provider.FamilyKV {
				t.Errorf("%s: envelope family = %q, want %q (body=%s)", c.name, env.Family, provider.FamilyKV, body)
			}
		})
	}
}

// ---- DOC-AUD-02: over-cap body is 413, not a truncated 400 ----

// zeroReader emits exactly n zero bytes without allocating them all up
// front — keeps the over-cap-body test lightweight even though n is
// comparable to maxWriteBody (64 MiB).
type zeroReader struct{ n int64 }

func (z *zeroReader) Read(p []byte) (int, error) {
	if z.n <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > z.n {
		p = p[:z.n]
	}
	for i := range p {
		p[i] = '0'
	}
	z.n -= int64(len(p))
	return len(p), nil
}

// oversizedJSONBody is valid-JSON-shaped (a real InsertRow body whose "a"
// string value is stretched past maxWriteBody) so decode() genuinely trips
// the byte cap mid-token, the same shape a real oversized write would take —
// never a body that would fail for some OTHER reason (e.g. plain syntax
// garbage) before the cap is even reached.
func oversizedJSONBody() io.Reader {
	prefix := `{"path":{"service":"svc","segments":["public","t"]},"row":{"a":"`
	suffix := `"}}`
	filler := maxWriteBody + 1 - int64(len(prefix)) - int64(len(suffix))
	return io.MultiReader(
		strings.NewReader(prefix),
		&zeroReader{n: filler},
		strings.NewReader(suffix),
	)
}

func TestServer_ErrorEnvelope_OverCapBody_Is413NotTruncated400(t *testing.T) {
	t.Parallel()
	ts, tok, _, _ := newRouteServer(t, "postgresql:single@18", provider.FamilyTabular, true)
	defer ts.Close()

	req := newRequest(t, http.MethodPost, "/api/row", oversizedJSONBody())
	resp := doReq(t, ts, req, tok, true)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", resp.StatusCode, body)
	}
	env := decodeEnvelope(t, body)
	if env.Code != "too_large" {
		t.Fatalf("code = %q, want too_large (body=%s)", env.Code, body)
	}
}

// ---- KV-AUD-08: the bearer-auth 401 is the JSON envelope, not text/plain ----

func TestServer_Auth401_IsJSONEnvelope(t *testing.T) {
	t.Parallel()
	ts, _ := newTestServer(t, false)
	defer ts.Close()

	req := newRequest(t, http.MethodGet, "/api/services", nil)
	resp := doReq(t, ts, req, "wrong-bearer", false)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json; body=%s", ct, body)
	}
	env := decodeEnvelope(t, body)
	if env.Code != "unauthorized" || env.Status != http.StatusUnauthorized {
		t.Fatalf("envelope = %+v, want code=unauthorized status=401 (body=%s)", env, body)
	}
	if env.RequestID == "" {
		t.Fatal("requestId empty")
	}
}
