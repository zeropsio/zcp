package server

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
)

// credentialHost counts the connection-info reads: each one is a fresh read of
// the service's credentials.
type credentialHost struct{ reads atomic.Int32 }

func (*credentialHost) Project(context.Context) (console.ProjectRef, error) {
	return console.ProjectRef{ID: "p1", Name: "proj"}, nil
}
func (*credentialHost) ManagedServices(context.Context) ([]console.ManagedServiceRef, error) {
	return []console.ManagedServiceRef{{ID: "s1", Hostname: "store", Type: "object-storage", Status: "ACTIVE"}}, nil
}
func (h *credentialHost) ConnectionInfo(context.Context, string) (console.ConnectionInfo, error) {
	h.reads.Add(1)
	return testConnectionInfo("object-storage"), nil
}

// staleConn is one built connection: its service refuses its credentials when
// refused is set.
type staleConn struct {
	fakeObject
	refused bool
	writes  *atomic.Int32
}

func (c *staleConn) List(ctx context.Context, p provider.Path, pg provider.Page) ([]provider.Node, string, error) {
	if c.refused {
		return nil, "", fmt.Errorf("object: access denied: %w: %w", provider.ErrAuth, provider.ErrUpstream)
	}
	return c.fakeObject.List(ctx, p, pg)
}

func (c *staleConn) WriteBlob(context.Context, provider.Path, []byte, string) error {
	c.writes.Add(1)
	return fmt.Errorf("object: access denied: %w: %w", provider.ErrAuth, provider.ErrUpstream)
}

// newStaleCredentialServer builds a console whose n-th built connection is
// refused when refusedBuilds[n] is set; builds past the list are accepted.
func newStaleCredentialServer(t *testing.T, refusedBuilds ...bool) (*testServer, *credentialHost, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	host := &credentialHost{}
	var builds, writes atomic.Int32
	factories := map[provider.Family]console.Factory{
		provider.FamilyObject: func(console.ConnectionInfo, *safety.Policy) (provider.Provider, error) {
			n := int(builds.Add(1)) - 1
			refused := n < len(refusedBuilds) && refusedBuilds[n]
			return &staleConn{fakeObject: fakeObject{blobs: map[string][]byte{}}, refused: refused, writes: &writes}, nil
		},
	}
	eng := console.NewEngine(host, writePolicy(true), factories)
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	srv := New(eng, "secret", fstest.MapFS{"index.html": {Data: []byte("ok")}})
	return &testServer{handler: srv.Handler()}, host, &builds, &writes
}

func TestServer_CredentialsRefused_ReadRebuildsOnceWriteNever(t *testing.T) {
	t.Parallel()

	// witness: internal/dataconsole/console/console.go | stale := e.built[hostname] == failed → stale := e.built[hostname] != failed
	t.Run("a read that fails authentication is answered by a connection built from fresh credentials", func(t *testing.T) {
		t.Parallel()
		ts, host, _, _ := newStaleCredentialServer(t, true)
		if r := do(t, ts, "GET", "/api/tree?service=store", "secret", "", false); r.code != http.StatusOK {
			t.Fatalf("tree = %d, want 200", r.code)
		}
		if got := host.reads.Load(); got != 2 {
			t.Fatalf("credentials read %d times, want 2 (the first build and the rebuild)", got)
		}
	})

	// witness: internal/dataconsole/console/server/server.go | return read(ctx, fresh) → return readOn(ctx, e, service, fresh, read)
	t.Run("a rebuilt connection that fails too answers the failure, with no third build", func(t *testing.T) {
		t.Parallel()
		ts, _, builds, _ := newStaleCredentialServer(t, true, true)
		if r := do(t, ts, "GET", "/api/tree?service=store", "secret", "", false); r.code != http.StatusBadGateway {
			t.Fatalf("tree = %d, want 502", r.code)
		}
		if got := builds.Load(); got != 2 {
			t.Fatalf("connection built %d times, want 2", got)
		}
	})

	// witness: internal/dataconsole/console/server/server.go | if err := wb.WriteBlob(ctx, body.Path, body.Data, ct); err != nil { → if _, err := readOn(ctx, s.engine, body.Path.Service, p, func(context.Context, provider.Provider) (struct{}, error) { return struct{}{}, wb.WriteBlob(ctx, body.Path, body.Data, ct) }); err != nil {
	t.Run("a write that fails authentication is sent once", func(t *testing.T) {
		t.Parallel()
		ts, _, _, writes := newStaleCredentialServer(t, true)
		body := `{"path":{"service":"store","segments":["w.txt"]},"data":"aGk="}`
		if r := do(t, ts, "PUT", "/api/blob", "secret", body, true); r.code != http.StatusBadGateway {
			t.Fatalf("write = %d, want 502", r.code)
		}
		if got := writes.Load(); got != 1 {
			t.Fatalf("write sent %d times, want 1", got)
		}
	})
}
