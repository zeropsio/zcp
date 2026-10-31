package web

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type closeReceiptConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (conn *closeReceiptConn) Close() error {
	err := conn.Conn.Close()
	conn.once.Do(func() { close(conn.closed) })
	return err
}

func TestServer_ClientCleanupClosesUnusedSpeculativeConnection(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	secondConnected := make(chan struct{})
	unusedClosed := make(chan struct{})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// This observer runs after newTestServer's cleanup and releases connections
	// even when the old fixture leaves its own speculative socket behind.
	t.Cleanup(func() {
		defer transport.CloseIdleConnections()
		select {
		case <-secondConnected:
			select {
			case <-unusedClosed:
			default:
				t.Error("test client left its unused connection open after server cleanup")
			}
		default:
		}
	})
	server, client, _ := newTestServer(t)
	client.Transport = transport
	var dials atomic.Int32
	releaseDial := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseDial) })
	defer release()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil || dials.Add(1) != 2 {
			return conn, err
		}
		tracked := &closeReceiptConn{Conn: conn, closed: unusedClosed}
		close(secondConnected)
		select {
		case <-releaseDial:
			return tracked, nil
		case <-ctx.Done():
			_ = tracked.Close()
			return nil, ctx.Err()
		}
	}
	first := testGET(t, client, server.URL()+"/")
	defer first.Body.Close()
	secondDone := make(chan error, 1)
	secondJoined := make(chan struct{})
	reused := make(chan bool, 1)
	go func() {
		defer close(secondJoined)
		request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) { reused <- info.Reused },
		}), http.MethodGet, server.URL()+"/", nil)
		if err == nil {
			var response *http.Response
			response, err = client.Do(request)
			if err == nil {
				_, err = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}
		}
		secondDone <- err
	}()
	t.Cleanup(func() {
		release()
		cancel()
		select {
		case <-secondJoined:
		case <-time.After(time.Second):
			t.Error("second HTTP request did not join after cancellation")
		}
	})
	select {
	case <-secondConnected:
	case <-ctx.Done():
		t.Fatal("second dial did not reach the connection barrier", ctx.Err())
	}
	if _, err := io.Copy(io.Discard, first.Body); err != nil {
		t.Fatal("release first connection", err)
	}
	_ = first.Body.Close()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal("second HTTP request", err)
		}
	case <-ctx.Done():
		t.Fatal("second request did not reuse the first connection", ctx.Err())
	}
	if !<-reused {
		t.Fatal("second request used the held dial instead of the idle connection")
	}
	release()
}
