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

const (
	wtBearer = "read-bearer-secret"
	wtSecret = "write-token-secret-independent"
	// wtBlobWrite is a valid PUT /api/blob body targeting the fakeHost "store" service.
	wtBlobWrite = `{"path":{"service":"store","segments":["w.txt"]},"data":"aGk="}`
)

// newWriteTokenServer builds a server whose write posture mirrors production: the
// provider is engine-level read-only exactly when arming is NOT permitted, and the
// policy mints the given write token (empty for a read-only process). The read
// bearer is wtBearer.
func newWriteTokenServer(t *testing.T, armingPermitted bool, writeToken string) *testServer {
	t.Helper()
	fake := &fakeObject{blobs: map[string][]byte{}, readOnly: !armingPermitted}
	factories := map[provider.Family]console.Factory{
		provider.FamilyObject: func(console.ConnectionInfo, *safety.Policy) (provider.Provider, error) { return fake, nil },
	}
	eng := console.NewEngine(fakeHost{}, safety.NewPolicy(armingPermitted, writeToken, ""), factories)
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	srv := New(eng, wtBearer, fstest.MapFS{"index.html": {Data: []byte("ok")}})
	return &testServer{handler: srv.Handler()}
}

// wtRequest is a mutating-call builder with explicit control over the bearer, write
// token, confirm and Origin headers — so each caller-bound refusal can be pinned in
// isolation (unlike the shared do/doReq helpers, which always present the token).
type wtRequest struct {
	method     string
	path       string
	body       string
	bearer     string
	writeToken string // sets X-Write-Token when non-empty
	confirm    bool   // sets X-Confirm: true
	origin     string // sets Origin when non-empty
	noBearer   bool   // omit Authorization entirely (default sends wtBearer)
}

func sendWT(t *testing.T, ts *testServer, wr wtRequest) int {
	t.Helper()
	req := httptest.NewRequest(wr.method, "http://dataconsole.test"+wr.path, strings.NewReader(wr.body))
	bearer := wr.bearer
	if bearer == "" && !wr.noBearer {
		bearer = wtBearer
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if wr.writeToken != "" {
		req.Header.Set("X-Write-Token", wr.writeToken)
	}
	if wr.confirm {
		req.Header.Set("X-Confirm", "true")
	}
	if wr.origin != "" {
		req.Header.Set("Origin", wr.origin)
	}
	rr := httptest.NewRecorder()
	ts.handler.ServeHTTP(rr, req)
	resp := rr.Result()
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestWriteToken_StandaloneGap_ConfirmWithoutTokenIsRefused is the exact blocker
// this closes: a caller holding the read bearer and setting X-Confirm:true —
// everything the standalone SPA can do — still cannot write, because it holds no
// write token. This now holds UNCONDITIONALLY: there is no process arm to flip.
func TestWriteToken_StandaloneGap_ConfirmWithoutTokenIsRefused(t *testing.T) {
	t.Parallel()
	ts := newWriteTokenServer(t, true, wtSecret)
	if code := sendWT(t, ts, wtRequest{method: "PUT", path: "/api/blob", body: wtBlobWrite, confirm: true}); code != http.StatusForbidden {
		t.Fatalf("bearer+confirm without write token = %d, want 403 (writes are caller-bound)", code)
	}
}

// TestWriteToken_CorrectTokenWrites pins the happy path and the confirm intent: the
// correct write token + confirm writes (200); the correct token WITHOUT confirm is
// 409; a WRONG token is a uniform 403 read-only (no oracle).
func TestWriteToken_CorrectTokenWrites(t *testing.T) {
	t.Parallel()
	ts := newWriteTokenServer(t, true, wtSecret)

	if code := sendWT(t, ts, wtRequest{method: "PUT", path: "/api/blob", body: wtBlobWrite, writeToken: wtSecret, confirm: true}); code != http.StatusOK {
		t.Fatalf("correct token + confirm = %d, want 200", code)
	}
	if code := sendWT(t, ts, wtRequest{method: "PUT", path: "/api/blob", body: wtBlobWrite, writeToken: wtSecret, confirm: false}); code != http.StatusConflict {
		t.Fatalf("correct token, no confirm = %d, want 409", code)
	}
	if code := sendWT(t, ts, wtRequest{method: "PUT", path: "/api/blob", body: wtBlobWrite, writeToken: "wrong-token", confirm: true}); code != http.StatusForbidden {
		t.Fatalf("wrong token = %d, want 403", code)
	}
}

// TestWriteToken_ArmingNotPermitted pins the launch ceiling: a process started
// WITHOUT --allow-writes mints no write token and refuses every mutation — even one
// presenting a (guessed) token.
func TestWriteToken_ArmingNotPermitted(t *testing.T) {
	t.Parallel()
	ts := newWriteTokenServer(t, false, "")
	if code := sendWT(t, ts, wtRequest{method: "PUT", path: "/api/blob", body: wtBlobWrite, writeToken: wtSecret, confirm: true}); code != http.StatusForbidden {
		t.Fatalf("write on read-only process = %d, want 403", code)
	}
}

// TestWriteToken_DualClient_CallerBound is the blocker's regression test: client A
// (the embed host) performs a successful mutation with the correct write token; then
// client B, presenting ONLY the read bearer + X-Confirm (no write token) — a
// standalone tab, or an attacker who sniffed the fragment bearer — STILL gets 403.
// This proves writes are caller-bound, not process-global: A's write latched nothing.
func TestWriteToken_DualClient_CallerBound(t *testing.T) {
	t.Parallel()
	ts := newWriteTokenServer(t, true, wtSecret)

	// Client A (host): correct write token → 200.
	if code := sendWT(t, ts, wtRequest{method: "PUT", path: "/api/blob", body: wtBlobWrite, writeToken: wtSecret, confirm: true}); code != http.StatusOK {
		t.Fatalf("client A (host) write = %d, want 200", code)
	}
	// Client B (bearer only, no write token): STILL refused after A's success.
	if code := sendWT(t, ts, wtRequest{method: "PUT", path: "/api/blob", body: wtBlobWrite, confirm: true}); code != http.StatusForbidden {
		t.Fatalf("client B (bearer only) after A's write = %d, want 403 (writes are caller-bound, not process-global)", code)
	}
}

// TestWriteToken_RequiresBearer pins that the mutating route stays behind the bearer
// gate: no bearer → 401, before any write-token check.
func TestWriteToken_RequiresBearer(t *testing.T) {
	t.Parallel()
	ts := newWriteTokenServer(t, true, wtSecret)
	if code := sendWT(t, ts, wtRequest{method: "PUT", path: "/api/blob", body: wtBlobWrite, writeToken: wtSecret, confirm: true, noBearer: true}); code != http.StatusUnauthorized {
		t.Fatalf("write without bearer = %d, want 401", code)
	}
}

// TestWriteToken_OriginGuard pins the defense-in-depth Origin check on mutating
// routes: a browser cross-origin write is refused even WITH the correct token, while
// same-origin, loopback and no-Origin (the non-browser broker) callers are accepted.
// The Origin check is NOT the boundary — the write token is — but it stays as CSRF
// defense-in-depth without false-blocking the legit broker/loopback/proxy path.
func TestWriteToken_OriginGuard(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		origin string
		want   int
	}{
		{"no origin (broker)", "", http.StatusOK},
		{"same origin as host", "http://dataconsole.test", http.StatusOK},
		{"loopback origin", "http://127.0.0.1:8080", http.StatusOK},
		{"cross origin refused", "https://evil.example", http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ts := newWriteTokenServer(t, true, wtSecret)
			code := sendWT(t, ts, wtRequest{method: "PUT", path: "/api/blob", body: wtBlobWrite, writeToken: wtSecret, confirm: true, origin: c.origin})
			if code != c.want {
				t.Fatalf("write with origin %q = %d, want %d", c.origin, code, c.want)
			}
		})
	}
}
