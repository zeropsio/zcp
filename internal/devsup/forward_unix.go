//go:build linux || darwin

package devsup

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// forwardDialTimeout bounds the dial from the preview port to the server: the
// server is on loopback, so a dial that has not finished has no listener.
const forwardDialTimeout = 2 * time.Second

// forward is the TCP forward from a server's preview port, on every
// interface, to the port the server listens on at 127.0.0.1: nothing but a
// byte copy, so any protocol the server speaks (HTTP, a dev server's
// websocket) passes. It lives and dies with one server's process group.
type forward struct {
	listenPort, targetPort int
	ln                     net.Listener

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// openForward listens on 0.0.0.0:listenPort and forwards each connection to
// 127.0.0.1:targetPort.
func openForward(listenPort, targetPort int) (*forward, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", ":"+strconv.Itoa(listenPort))
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller logs it with the ports
	}
	f := &forward{listenPort: listenPort, targetPort: targetPort, ln: ln, conns: map[net.Conn]struct{}{}}
	f.wg.Go(f.accept)
	return f, nil
}

func (f *forward) accept() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		if !f.track(c) {
			_ = c.Close()
			return
		}
		f.wg.Go(func() { f.serve(c) })
	}
}

// track records c, or reports false once the forward is closed.
func (f *forward) track(c net.Conn) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return false
	}
	f.conns[c] = struct{}{}
	return true
}

func (f *forward) serve(client net.Conn) {
	defer func() {
		_ = client.Close()
		f.mu.Lock()
		delete(f.conns, client)
		f.mu.Unlock()
	}()
	d := net.Dialer{Timeout: forwardDialTimeout}
	server, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(f.targetPort))
	if err != nil {
		return
	}
	if !f.track(server) {
		_ = server.Close()
		return
	}
	defer func() {
		_ = server.Close()
		f.mu.Lock()
		delete(f.conns, server)
		f.mu.Unlock()
	}()
	// One side ending ends the connection: both are closed on return, which
	// ends the other copy.
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	f.wg.Go(func() { pipe(server, client) })
	f.wg.Go(func() { pipe(client, server) })
	<-done
}

// close stops listening, ends every open connection and waits for the copies
// to finish: the preview port is free when it returns.
func (f *forward) close() {
	f.mu.Lock()
	f.closed = true
	conns := make([]net.Conn, 0, len(f.conns))
	for c := range f.conns {
		conns = append(conns, c)
	}
	f.mu.Unlock()
	_ = f.ln.Close()
	for _, c := range conns {
		_ = c.Close()
	}
	f.wg.Wait()
}
