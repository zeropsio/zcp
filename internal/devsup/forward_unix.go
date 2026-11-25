//go:build linux || darwin

package devsup

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// forwardDialTimeout bounds the dial from the preview port to the server: the
// server is on loopback, so a dial that has not finished has no listener.
const forwardDialTimeout = 2 * time.Second

// forwardAcceptWaitFirst and forwardAcceptWaitCap bound the wait after an
// Accept error before the forward takes the next connection.
const (
	forwardAcceptWaitFirst = 5 * time.Millisecond
	forwardAcceptWaitCap   = time.Second
)

// forwardHalfCloseWait bounds how long a connection one side has ended (a
// half-close, passed on to the other side) waits for the other side to end
// its direction too.
const forwardHalfCloseWait = 30 * time.Second

// forwardRequestWait bounds the wait for the request the preview port answers
// itself, and for writing its answer.
const forwardRequestWait = 10 * time.Second

// forward is the TCP forward from the preview server's preview port, on every
// interface, to the port the server listens on at 127.0.0.1. It lives with the
// supervisor, whatever the server's status: to a running server it is nothing
// but a byte copy, so any protocol the server speaks (HTTP, a dev server's
// websocket) passes; otherwise it answers the request itself with the holding
// page (previewAnswer). A stop of the server cuts its connections. It records
// the connections it takes in the server's previewSeen.
type forward struct {
	// name is the server it reaches; view is what to answer for it now and
	// the channel closed at its next change (Supervisor.previewView).
	name                   string
	view                   func() (previewView, <-chan struct{})
	listenPort, targetPort int
	ln                     net.Listener
	seen                   *previewSeen

	// cancel ends a dial to the server still under way (shut).
	cancel context.CancelFunc

	// conns holds every open connection, true for the two ends of a relay
	// to the server — what a stop of the server cuts.
	mu     sync.Mutex
	conns  map[net.Conn]bool
	closed bool
	wg     sync.WaitGroup
}

// previewSeen is what a server's preview forwards saw in this supervisor's
// life: when one last relayed a connection to the server and how many
// relayed connections they hold open — the clients, never a forward's own
// dial to the server, nor a holding page or its held request, which no
// person's work is behind (an abandoned tab would hold its lease). It outlives
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

// openForward listens on 0.0.0.0:listenPort for server name and answers each
// connection as view says — forwarded to 127.0.0.1:targetPort, or the holding
// page — recording each in seen.
func openForward(name string, listenPort, targetPort int, seen *previewSeen, view func() (previewView, <-chan struct{})) (*forward, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", ":"+strconv.Itoa(listenPort))
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller logs it with the ports
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &forward{name: name, view: view, listenPort: listenPort, targetPort: targetPort, ln: ln, seen: seen, cancel: cancel, conns: map[net.Conn]bool{}}
	f.wg.Go(func() { f.accept(ctx) })
	return f, nil
}

// accept serves each connection the preview port takes; ctx ends when the
// forward is shut, and only that ends it. Any other Accept error — every file
// descriptor in use, a connection aborted before it was taken — waits a
// moment, doubling up to forwardAcceptWaitCap, and takes the next: a
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
			backoff = min(max(2*backoff, forwardAcceptWaitFirst), forwardAcceptWaitCap)
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

// track records c — a client is a preview connection; any other is the
// forward's own dial to the server, an end of a relay — or reports false once
// the forward is closed.
func (f *forward) track(c net.Conn, client bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.conns[c] = !client
	}
	return !f.closed
}

func (f *forward) serve(ctx context.Context, client net.Conn) {
	defer func() {
		_ = client.Close()
		f.mu.Lock()
		delete(f.conns, client)
		f.mu.Unlock()
	}()
	v, _ := f.view()
	if v.kind == previewForward {
		d := net.Dialer{Timeout: forwardDialTimeout}
		server, err := d.DialContext(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(f.targetPort))
		if err == nil {
			f.relay(ctx, client, server)
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
	f.answer(ctx, client)
}

// relay copies the bytes between a client and the server until both ends
// are done; the client counts as seen (previewSeen) meanwhile.
func (f *forward) relay(ctx context.Context, client, server net.Conn) {
	if !f.track(server, false) {
		_ = server.Close()
		return
	}
	f.seen.opened(time.Now().UTC())
	defer f.seen.closed()
	f.mu.Lock()
	if _, open := f.conns[client]; open {
		f.conns[client] = true
	}
	f.mu.Unlock()
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

// forwardBodyDrain bounds the request body the preview port reads and drops
// before it answers a request itself: an answer written over a body left
// unread ends in a reset, which a balancer shows as its own error.
const forwardBodyDrain = 1 << 20

// answer reads the client's request and answers it while the server is not
// running: the held request on HoldPath, any other path the holding page with
// 503. What to say is read once the request came, so a connection that
// waited says what holds now; a server that runs by then gets the page that
// opens it at once.
func (f *forward) answer(ctx context.Context, client net.Conn) {
	_ = client.SetReadDeadline(time.Now().Add(forwardRequestWait))
	req, err := http.ReadRequest(bufio.NewReader(client))
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(req.Body, forwardBodyDrain))
	_ = client.SetReadDeadline(time.Time{})
	if req.URL.Path == HoldPath {
		f.hold(ctx, client, req.URL.Query().Get("page"))
		return
	}
	v, _ := f.view()
	if v.kind == previewForward {
		v.kind = previewHold
	}
	_ = client.SetWriteDeadline(time.Now().Add(forwardRequestWait))
	_ = writeAnswer(client, http.StatusServiceUnavailable, []string{"Content-Type: text/html; charset=utf-8"}, holdingPage(v))
}

// now is what the port answers for its server now: its view, except that a
// running server whose port refuses a dial gets the page that says it is
// starting, as answer gives it — the dial is the probe, so a page never
// reloads into a refusal.
func (f *forward) now(ctx context.Context) (previewView, <-chan struct{}) {
	v, changed := f.view()
	if v.kind != previewForward {
		return v, changed
	}
	d := net.Dialer{Timeout: 200 * time.Millisecond}
	c, err := d.DialContext(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(f.targetPort))
	if err != nil {
		v.kind = previewHold
		return v, changed
	}
	_ = c.Close()
	return v, changed
}

// hold answers the held request of the holding page served (its pageID)
// once the port would answer something else (holdDone), and after holdWait
// with holdAgain, on which the page asks again; a client that leaves ends it.
func (f *forward) hold(ctx context.Context, client net.Conn, served string) {
	gone := make(chan struct{})
	f.wg.Go(func() {
		_, _ = client.Read(make([]byte, 1))
		close(gone)
	})
	wait := time.NewTimer(holdWait)
	defer wait.Stop()
	header := []string{"Access-Control-Allow-Origin: *", "Access-Control-Expose-Headers: " + holdHeader}
	for {
		v, changed := f.now(ctx)
		if holdDone(served, v) {
			break
		}
		select {
		case <-changed:
			continue
		case <-wait.C:
			header = append(header, holdHeader+": "+holdAgain)
		case <-gone:
			return
		case <-ctx.Done():
			return
		}
		break
	}
	_ = client.SetWriteDeadline(time.Now().Add(forwardRequestWait))
	_ = writeAnswer(client, http.StatusNoContent, header, nil)
}

// cut ends every relay to the server, as a stop of the server ends them, and
// keeps listening: a held request goes on waiting for the server's next
// status.
func (f *forward) cut() {
	f.mu.Lock()
	conns := make([]net.Conn, 0, len(f.conns))
	for c, relayed := range f.conns {
		if relayed {
			conns = append(conns, c)
		}
	}
	f.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
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
