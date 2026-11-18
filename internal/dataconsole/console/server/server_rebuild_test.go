package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
)

// credentialHost answers the service's credentials. With rotated set, every
// read returns credentials different from the previous read's: the service's
// password changed since the connection was built.
type credentialHost struct {
	rotated bool
	reads   atomic.Int32
}

func (*credentialHost) Project(context.Context) (console.ProjectRef, error) {
	return console.ProjectRef{ID: "p1", Name: "proj"}, nil
}
func (*credentialHost) ManagedServices(context.Context) ([]console.ManagedServiceRef, error) {
	return []console.ManagedServiceRef{{ID: "s1", Hostname: "store", Type: "object-storage", Status: "ACTIVE"}}, nil
}
func (h *credentialHost) ConnectionInfo(context.Context, string) (console.ConnectionInfo, error) {
	n := h.reads.Add(1)
	ci := testConnectionInfo("object-storage")
	if h.rotated {
		d, _ := ci.Descriptor.(provider.ObjectConn)
		d.SecretKey = "secret-key-" + strconv.Itoa(int(n))
		ci.Descriptor = d
	}
	return ci, nil
}

// staleConn is one built connection: its service refuses its credentials when
// refused is set.
type staleConn struct {
	fakeObject
	refused bool
	pools   *pools
}

func (c *staleConn) List(ctx context.Context, p provider.Path, pg provider.Page) ([]provider.Node, string, error) {
	if c.refused {
		return nil, "", fmt.Errorf("object: access denied: %w: %w", provider.ErrAuth, provider.ErrUpstream)
	}
	return c.fakeObject.List(ctx, p, pg)
}

func (c *staleConn) WriteBlob(context.Context, provider.Path, []byte, string) error {
	c.pools.writes.Add(1)
	return fmt.Errorf("object: access denied: %w: %w", provider.ErrAuth, provider.ErrUpstream)
}

func (c *staleConn) Close() error {
	c.pools.closed.Add(1)
	return nil
}

// pools counts the connections the console opened and closed, and the writes
// it sent.
type pools struct {
	opened, closed, writes atomic.Int32
	// meet, when set, holds every build after the first until two builds
	// are in the factory together: two requests rebuild at the same moment.
	meet *sync.WaitGroup
}

// newStaleCredentialServer builds a console whose n-th built connection is
// refused when refusedBuilds[n] is set; builds past the list are accepted.
func newStaleCredentialServer(t *testing.T, host *credentialHost, meet bool, refusedBuilds ...bool) (*testServer, *pools) {
	t.Helper()
	counts := &pools{}
	if meet {
		counts.meet = &sync.WaitGroup{}
		counts.meet.Add(2)
	}
	factories := map[provider.Family]console.Factory{
		provider.FamilyObject: func(console.ConnectionInfo, *safety.Policy) (provider.Provider, error) {
			n := int(counts.opened.Add(1)) - 1
			if n > 0 && counts.meet != nil {
				counts.meet.Done()
				counts.meet.Wait()
			}
			refused := n < len(refusedBuilds) && refusedBuilds[n]
			return &staleConn{fakeObject: fakeObject{blobs: map[string][]byte{}}, refused: refused, pools: counts}, nil
		},
	}
	eng := console.NewEngine(host, writePolicy(true), factories)
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	srv := New(eng, "secret", fstest.MapFS{"index.html": {Data: []byte("ok")}})
	return &testServer{handler: srv.Handler()}, counts
}

func TestServer_CredentialsRefused_ReadRebuildsOnceWriteNever(t *testing.T) {
	t.Parallel()

	// witness: internal/dataconsole/console/console.go | stale := e.built[hostname].p == failed → stale := e.built[hostname].p != failed
	t.Run("a read that fails authentication is answered by a connection built from fresh credentials", func(t *testing.T) {
		t.Parallel()
		host := &credentialHost{rotated: true}
		ts, _ := newStaleCredentialServer(t, host, false, true)
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
		ts, counts := newStaleCredentialServer(t, &credentialHost{rotated: true}, false, true, true)
		if r := do(t, ts, "GET", "/api/tree?service=store", "secret", "", false); r.code != http.StatusBadGateway {
			t.Fatalf("tree = %d, want 502", r.code)
		}
		if got := counts.opened.Load(); got != 2 {
			t.Fatalf("connection built %d times, want 2", got)
		}
	})

	// witness: internal/dataconsole/console/console.go | if cached && ci.Descriptor == cur.from { → if cached && ci.Descriptor != cur.from {
	t.Run("a read that fails authentication on unchanged credentials is answered at once and keeps its connection", func(t *testing.T) {
		t.Parallel()
		ts, counts := newStaleCredentialServer(t, &credentialHost{}, false, true)
		if r := do(t, ts, "GET", "/api/tree?service=store", "secret", "", false); r.code != http.StatusBadGateway {
			t.Fatalf("tree = %d, want 502", r.code)
		}
		if opened, closed := counts.opened.Load(), counts.closed.Load(); opened != 1 || closed != 0 {
			t.Fatalf("connections opened %d, closed %d; want 1 opened, 0 closed", opened, closed)
		}
	})

	// witness: internal/dataconsole/console/console.go | if cached, ok := e.built[hostname]; ok { → if cached, ok := e.built[hostname]; ok && cached.p == nil {
	t.Run("two reads that rebuild at the same moment leave the service one open connection", func(t *testing.T) {
		t.Parallel()
		ts, counts := newStaleCredentialServer(t, &credentialHost{rotated: true}, true, true)
		// The first connection, built by a read that does not fail.
		if r := do(t, ts, "GET", `/api/stat?service=store&segs=%5B%22a%22%5D`, "secret", "", false); r.code != http.StatusOK {
			t.Fatalf("stat = %d, want 200", r.code)
		}
		codes := make([]int, 2)
		var reads sync.WaitGroup
		for i := range codes {
			reads.Go(func() {
				codes[i] = do(t, ts, "GET", "/api/tree?service=store", "secret", "", false).code
			})
		}
		reads.Wait()
		if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
			t.Fatalf("trees = %v, want both 200", codes)
		}
		if open := counts.opened.Load() - counts.closed.Load(); open != 1 {
			t.Fatalf("connections opened %d, closed %d: %d open, want 1", counts.opened.Load(), counts.closed.Load(), open)
		}
	})

	// witness: internal/dataconsole/console/server/server.go | if err := wb.WriteBlob(ctx, body.Path, body.Data, ct); err != nil { → if _, err := readOn(ctx, s.engine, body.Path.Service, p, func(context.Context, provider.Provider) (struct{}, error) { return struct{}{}, wb.WriteBlob(ctx, body.Path, body.Data, ct) }); err != nil {
	t.Run("a write that fails authentication is sent once", func(t *testing.T) {
		t.Parallel()
		ts, counts := newStaleCredentialServer(t, &credentialHost{rotated: true}, false, true)
		body := `{"path":{"service":"store","segments":["w.txt"]},"data":"aGk="}`
		if r := do(t, ts, "PUT", "/api/blob", "secret", body, true); r.code != http.StatusBadGateway {
			t.Fatalf("write = %d, want 502", r.code)
		}
		if got := counts.writes.Load(); got != 1 {
			t.Fatalf("write sent %d times, want 1", got)
		}
	})
}
