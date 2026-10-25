//go:build linux || darwin

package devsup

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// forwardDialTimeout bounds the dial from the preview port to the server: the
// server is on loopback, so a dial that has not finished has no listener.
const forwardDialTimeout = 2 * time.Second

// forwardAcceptBackoffMin and forwardAcceptBackoffMax bound the wait after
// an Accept error before the forward takes the next connection.
const (
	forwardAcceptBackoffMin = 5 * time.Millisecond
	forwardAcceptBackoffMax = time.Second
)

// forwardHalfCloseWait bounds how long a connection one side has ended (a
// half-close, passed on to the other side) waits for the other side to end
// its direction too.
const forwardHalfCloseWait = 30 * time.Second

// forward is the TCP forward from a server's preview port, on every
// interface, to the port the server listens on at 127.0.0.1: nothing but a
// byte copy, so any protocol the server speaks (HTTP, a dev server's
// websocket) passes. It lives and dies with one server's process group, and
// records the connections it takes in the server's previewSeen.
type forward struct {
	listenPort, targetPort int
	ln                     net.Listener
	seen                   *previewSeen

	// cancel ends a dial to the server still under way (shut).
	cancel context.CancelFunc

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// previewSeen is what a server's preview forwards saw in this supervisor's
// life: when one last took a connection and how many connections they hold
// open — the clients, never a forward's own dial to the server. It outlives
// each forward (a restart of the server opens a new one) and lives in memory
// only. Its lock guards these two fields and nothing else.
type previewSeen struct {
	mu   sync.Mutex
	last time.Time
	open int
}

func (p *previewSeen) opened(at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = at
	p.open++
}

func (p *previewSeen) closed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.open--
}

// read is the last connection's time (nil before the first) and the open
// count.
func (p *previewSeen) read() (*time.Time, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last.IsZero() {
		return nil, p.open
	}
	last := p.last
	return &last, p.open
}

// openForward listens on 0.0.0.0:listenPort and forwards each connection to
// 127.0.0.1:targetPort, recording each in seen.
func openForward(listenPort, targetPort int, seen *previewSeen) (*forward, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", ":"+strconv.Itoa(listenPort))
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller logs it with the ports
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &forward{listenPort: listenPort, targetPort: targetPort, ln: ln, seen: seen, cancel: cancel, conns: map[net.Conn]struct{}{}}
	f.wg.Go(func() { f.accept(ctx) })
	return f, nil
}

// accept serves each connection the preview port takes; ctx ends when the
// forward is shut, and only that ends it. Any other Accept error — every file
// descriptor in use, a connection aborted before it was taken — waits a
// moment, doubling up to forwardAcceptBackoffMax, and takes the next: a
// forward that ended there would hold its port bound, serving nothing, while
// the server kept it and status read its connections as observed.
func (f *forward) accept(ctx context.Context) {
	var backoff time.Duration
	for {
		c, err := f.ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			backoff = min(max(2*backoff, forwardAcceptBackoffMin), forwardAcceptBackoffMax)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}
		backoff = 0
		if !f.track(c, true) {
			_ = c.Close()
			return
		}
		f.wg.Go(func() { f.serve(ctx, c) })
	}
}

// track records c — a client is a preview connection, stamped with the time
// it was taken — or reports false once the forward is closed.
func (f *forward) track(c net.Conn, client bool) bool {
	f.mu.Lock()
	ok := !f.closed
	if ok {
		f.conns[c] = struct{}{}
	}
	f.mu.Unlock()
	if ok && client {
		f.seen.opened(time.Now().UTC())
	}
	return ok
}

func (f *forward) serve(ctx context.Context, client net.Conn) {
	defer func() {
		_ = client.Close()
		f.mu.Lock()
		delete(f.conns, client)
		f.mu.Unlock()
		f.seen.closed()
	}()
	d := net.Dialer{Timeout: forwardDialTimeout}
	server, err := d.DialContext(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(f.targetPort))
	if err != nil {
		return
	}
	if !f.track(server, false) {
		_ = server.Close()
		return
	}
	defer func() {
		_ = server.Close()
		f.mu.Lock()
		delete(f.conns, server)
		f.mu.Unlock()
	}()
	// A direction that ends is passed on as a half-close — the other side's
	// write side is shut — so a client that shut its write side after its
	// request still reads the whole reply. The connection ends once both
	// directions have, forwardHalfCloseWait after the first did, or when the
	// forward is shut: both are closed on return, which ends a copy still
	// running.
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if hc, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = hc.CloseWrite()
		}
		done <- struct{}{}
	}
	f.wg.Go(func() { pipe(server, client) })
	f.wg.Go(func() { pipe(client, server) })
	<-done
	wait := time.NewTimer(forwardHalfCloseWait)
	defer wait.Stop()
	select {
	case <-done:
	case <-wait.C:
	case <-ctx.Done():
	}
}

// close shuts the forward and waits for its copies to finish.
func (f *forward) close() {
	f.shut()
	f.wait()
}

// shut stops listening, ends every open connection and cancels a dial to the
// server still under way, without waiting for anything: the preview port is
// free when it returns.
func (f *forward) shut() {
	f.mu.Lock()
	f.closed = true
	conns := make([]net.Conn, 0, len(f.conns))
	for c := range f.conns {
		conns = append(conns, c)
	}
	f.mu.Unlock()
	f.cancel()
	_ = f.ln.Close()
	for _, c := range conns {
		_ = c.Close()
	}
}

// wait waits for a shut forward's copies and serves to finish.
func (f *forward) wait() {
	f.wg.Wait()
}
