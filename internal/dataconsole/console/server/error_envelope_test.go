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
		provider.FamilyObject: func(console.ConnectionInfo, safety.Policy) (provider.Provider, error) {
			return errorProvider{err: err}, nil
		},
	}
	eng := console.NewEngine(errorHost{}, safety.Policy{}, factories)
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
