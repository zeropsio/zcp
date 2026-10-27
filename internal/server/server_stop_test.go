// Tests for: server package — what New starts off the startup path is the
// server's to stop and wait for.
package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// heldHQ is an HQ whose answer to the Mate's state waits for release, deaf
// to the caller giving up: a start pass held mid-way, as on a slow runner.
// Every other request finds no HQ.
type heldHQ struct {
	asked   func()
	release <-chan struct{}
}

func (h heldHQ) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/api/mate/self" {
		h.asked()
		<-h.release
	}
	return nil, errors.New("no HQ here")
}

// TestServerStop_StartPassRunning_WaitsForIt: the recipe pass New starts in a
// Mate is the server's work: once the server is stopped — Stop, or its
// context ended and its background waited for — the pass has ended, so a
// caller that removes the state directory next races nothing writing into it
// (an integration run's TempDir cleanup found `.zcp/state` not empty,
// 2026-10-27).
func TestServerStop_StartPassRunning_WaitsForIt(t *testing.T) {
	tests := []struct {
		name string
		stop func(cancel context.CancelFunc, srv *Server)
	}{
		{name: "Stop", stop: func(_ context.CancelFunc, srv *Server) { srv.Stop() }},
		{name: "its context ended, then WaitBackground", stop: func(cancel context.CancelFunc, srv *Server) {
			cancel()
			srv.WaitBackground()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Non-parallel: t.Chdir (the state dir) and a swap of
			// http.DefaultTransport (the HQ the start pass asks).
			t.Chdir(t.TempDir())
			asked, release := make(chan struct{}), make(chan struct{})
			var askOnce, releaseOnce sync.Once
			letGo := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(letGo)
			orig := http.DefaultTransport
			http.DefaultTransport = heldHQ{asked: func() { askOnce.Do(func() { close(asked) }) }, release: release}
			t.Cleanup(func() { http.DefaultTransport = orig })
			store, err := knowledge.GetEmbeddedStore()
			if err != nil {
				t.Fatalf("knowledge store: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			srv := New(ctx, platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "test"}).WithServices(nil),
				&auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}, store, platform.NewMockLogFetcher(), nil, nil,
				runtime.Info{InContainer: true, ServiceID: "s1", ProjectID: "p1", MateEnabled: true,
					HQ: &runtime.HQLink{Origin: "https://hq.invalid", ProjectID: "p1", Key: "the-project-key"}}, nil)
			select {
			case <-asked:
			case <-time.After(10 * time.Second):
				t.Fatal("the start pass never asked HQ")
			}

			stopped := make(chan struct{})
			go func() {
				tt.stop(cancel, srv)
				close(stopped)
			}()

			select {
			case <-stopped:
				t.Fatal("the server stopped while its start pass was still running")
			case <-time.After(200 * time.Millisecond):
			}
			letGo()
			select {
			case <-stopped:
			case <-time.After(10 * time.Second):
				t.Fatal("the server did not stop once its start pass could end")
			}
		})
	}
}
