//go:build linux || darwin

package devsup

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// echoOver sends msg to 127.0.0.1:port and reads it back; ok is false when the
// port refuses or the answer differs.
func echoOver(port int, msg string) bool {
	d := net.Dialer{Timeout: time.Second}
	c, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write([]byte(msg)); err != nil {
		return false
	}
	got := make([]byte, len(msg))
	n, _ := c.Read(got)
	return bytes.Equal(got[:n], []byte(msg))
}

func (r *rig) echoer(name string, port int) Server {
	return Server{Name: name, Port: port, Command: helperCmd(r.t, "echo", "PORT="+strconv.Itoa(port))}
}

func TestSupervisor_PreviewForward_SlotPort(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.env = []string{"ZEROPS_StableNumber=3"}
	web, api := freePort(t), freePort(t)
	preview := freePort(t)
	// The marked server is the one forwarded when several have a port.
	r.writeDeclaration(`{"version":1,"previewPortBase":` + strconv.Itoa(preview-3) + `,"servers":[` +
		`{"name":"web","port":` + strconv.Itoa(web) + `,"command":` + strconv.Quote(r.echoer("web", web).Command) + `},` +
		`{"name":"api","port":` + strconv.Itoa(api) + `,"preview":true,"command":` + strconv.Quote(r.echoer("api", api).Command) + `}]}`)
	s := r.supervisor()
	s.Boot()

	eventually(t, "the preview port to carry api's traffic", func() bool { return echoOver(preview, "ping") })
	st := s.State()
	if got := mustServer(t, st, "api").PreviewPort; got != preview {
		t.Fatalf("api previewPort = %d, want %d", got, preview)
	}
	if got := mustServer(t, st, "web").PreviewPort; got != 0 {
		t.Fatalf("web previewPort = %d, want none", got)
	}

	// The forward stops with its server.
	if err := s.Stop("api"); err != nil {
		t.Fatal(err)
	}
	released(t, preview)
	released(t, api)

	// And comes back with it.
	if err := s.Start("api"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the preview port to carry api's traffic again", func() bool { return echoOver(preview, "again") })
}

func TestSupervisor_PreviewForward_EndsWithTheServer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		end  func(t *testing.T, s *Supervisor)
	}{
		{name: "the server crashes", end: func(t *testing.T, s *Supervisor) {
			t.Helper()
			time.Sleep(400 * time.Millisecond) // past the start window: no second start
			if err := syscall.Kill(mustServer(t, s.State(), "web").Pgid, syscall.SIGKILL); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "the supervisor shuts down", end: func(_ *testing.T, s *Supervisor) { s.Shutdown() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			r.env = []string{"ZEROPS_StableNumber=1"}
			r.window = 200 * time.Millisecond
			web := freePort(t)
			preview := freePort(t)
			r.writeDeclaration(`{"version":1,"previewPortBase":` + strconv.Itoa(preview-1) + `,"servers":[` +
				`{"name":"web","port":` + strconv.Itoa(web) + `,"command":` + strconv.Quote(r.echoer("web", web).Command) + `}]}`)
			s := r.supervisor()
			s.Boot()
			eventually(t, "the preview port to listen", func() bool { return echoOver(preview, "x") })

			tt.end(t, s)

			released(t, preview)
		})
	}
}

func TestSupervisor_PreviewForward_AbsentBase_NoForward(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  []string
		// base is the declaration's previewPortBase offset from the probed
		// port; 0 leaves the key out.
		withBase bool
	}{
		{name: "no previewPortBase", env: []string{"ZEROPS_StableNumber=3"}},
		{name: "a base but no slot in the environment", withBase: true},
		{name: "a base but a slot that is not a number", env: []string{"ZEROPS_StableNumber=x"}, withBase: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			r.env = tt.env
			web := freePort(t)
			probe := freePort(t)
			base := ""
			if tt.withBase {
				base = `"previewPortBase":` + strconv.Itoa(probe-3) + `,`
			}
			r.writeDeclaration(`{"version":1,` + base + `"servers":[` +
				`{"name":"web","port":` + strconv.Itoa(web) + `,"command":` + strconv.Quote(r.echoer("web", web).Command) + `}]}`)
			s := r.supervisor()
			s.Boot()
			eventually(t, "web to listen", func() bool { return listening(web) })
			// The forward, were there one, opens as web starts listening.
			time.Sleep(600 * time.Millisecond)

			if listening(probe) {
				t.Fatalf("something listens on %d with no preview forward configured", probe)
			}
			if got := mustServer(t, s.State(), "web").PreviewPort; got != 0 {
				t.Fatalf("previewPort = %d, want none", got)
			}
		})
	}
}

// echoTarget is a server on 127.0.0.1 that echoes every connection's bytes
// back, in this process: the target a forward under test dials.
func echoTarget(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", ln.Addr())
	}
	return addr.Port
}

// heldConn is a connection through port that has carried one round trip and
// stays open until the test closes it (or the test ends).
func heldConn(t *testing.T, port int) net.Conn {
	t.Helper()
	d := net.Dialer{Timeout: time.Second}
	c, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "ping" {
		t.Fatalf("round trip through :%d answered %q (%v), want the bytes back unchanged", port, got, err)
	}
	return c
}

// previewOpen is the open-connection count seen holds now.
func previewOpen(seen *previewSeen) int {
	_, open := seen.read()
	return open
}

// TestForward_RecordsLastConnectionAndOpenCount: the preview forward records
// when it last took a connection and counts the connections it holds open —
// the clients, never its own dial to the server — while their bytes pass
// unchanged. A closed connection lowers the count and keeps the time; a
// closed forward leaves the count at 0 and the time as it was, and the next
// forward of the same server counts on from there (docs/spec-mate.md §2.8a.2).
func TestForward_RecordsLastConnectionAndOpenCount(t *testing.T) {
	t.Parallel()
	target, listen := echoTarget(t), freePort(t)
	seen := &previewSeen{}
	f, err := openForward(listen, target, seen)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.close)
	if last, open := seen.read(); last != nil || open != 0 {
		t.Fatalf("before any connection: last %v, open %d; want none and 0", last, open)
	}

	before := time.Now()
	a := heldConn(t, listen)
	_ = heldConn(t, listen)
	after := time.Now()

	last, open := seen.read()
	if open != 2 {
		t.Errorf("open = %d with two clients connected, want 2 (the forward's own dials are not preview connections)", open)
	}
	if last == nil || last.Before(before.Truncate(time.Microsecond)) || last.After(after) {
		t.Fatalf("last connection = %v, want between %s and %s", last, before, after)
	}
	first := *last

	_ = a.Close()
	eventually(t, "the closed connection to leave the count", func() bool { return previewOpen(seen) == 1 })
	if last, _ := seen.read(); last == nil || !last.Equal(first) {
		t.Errorf("last connection = %v after a close, want %s: a close is not a connection", last, first)
	}

	f.close()
	if last, open := seen.read(); open != 0 || last == nil || !last.Equal(first) {
		t.Fatalf("after the forward closed: last %v, open %d; want %s and 0", last, open, first)
	}

	// The server's next forward (a restart opens a new one) records into the
	// same record. Another port: the closed one may linger in TIME_WAIT.
	next := freePort(t)
	f2, err := openForward(next, target, seen)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f2.close)
	_ = heldConn(t, next)
	if last, open := seen.read(); open != 1 || last == nil || last.Before(first) {
		t.Fatalf("on the next forward: last %v, open %d; want a time from %s on and 1", last, open, first)
	}
}

// TestSupervisor_PreviewActivity_KeptAcrossServerRestartsNeverInStateFile:
// the preview server's state reports when its forward last took a connection
// and how many it holds open; a server without a preview port reports
// neither. A restart of the server cuts its connections and opens a new
// forward, and the time stays. Both live in memory only: state.json never
// holds them.
func TestSupervisor_PreviewActivity_KeptAcrossServerRestartsNeverInStateFile(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.env = []string{"ZEROPS_StableNumber=1"}
	web, api, preview := freePort(t), freePort(t), freePort(t)
	r.writeDeclaration(`{"version":1,"previewPortBase":` + strconv.Itoa(preview-1) + `,"servers":[` +
		`{"name":"web","port":` + strconv.Itoa(web) + `,"preview":true,"command":` + strconv.Quote(r.echoer("web", web).Command) + `},` +
		`{"name":"api","port":` + strconv.Itoa(api) + `,"command":` + strconv.Quote(r.echoer("api", api).Command) + `}]}`)
	s := r.supervisor()
	s.Boot()
	forwarded := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		sv := s.servers["web"]
		return sv != nil && sv.fwd != nil
	}
	eventually(t, "web's preview forward to open", forwarded)

	st := s.State()
	if w := mustServer(t, st, "web"); w.PreviewOpenConns == nil || *w.PreviewOpenConns != 0 || w.PreviewLastConnAt != nil {
		t.Fatalf("web before any connection: open %v, last %v; want an observed 0 and no time", w.PreviewOpenConns, w.PreviewLastConnAt)
	}
	if a := mustServer(t, st, "api"); a.PreviewOpenConns != nil || a.PreviewLastConnAt != nil {
		t.Fatalf("api has no preview port, yet reports open %v, last %v", a.PreviewOpenConns, a.PreviewLastConnAt)
	}

	before := time.Now()
	held := heldConn(t, preview)
	w := mustServer(t, s.State(), "web")
	if w.PreviewOpenConns == nil || *w.PreviewOpenConns != 1 || w.PreviewLastConnAt == nil || w.PreviewLastConnAt.Before(before.Truncate(time.Microsecond)) {
		t.Fatalf("web with one connection: open %v, last %v; want 1 and a time from %s on", w.PreviewOpenConns, w.PreviewLastConnAt, before)
	}
	last := *w.PreviewLastConnAt

	s.persist()
	raw, err := os.ReadFile(statePath(r.state))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("previewOpenConns")) || bytes.Contains(raw, []byte("previewLastConnAt")) {
		t.Errorf("state.json holds the preview activity, which lives in memory only:\n%s", raw)
	}

	if err := s.Restart("web"); err != nil {
		t.Fatal(err)
	}
	_ = held.SetReadDeadline(time.Now().Add(5 * time.Second))
	if n, err := held.Read(make([]byte, 1)); err == nil {
		t.Fatalf("the held connection still reads (%d bytes) after its server restarted", n)
	}
	eventually(t, "web's new preview forward to open", forwarded)
	w = mustServer(t, s.State(), "web")
	if w.PreviewOpenConns == nil || *w.PreviewOpenConns != 0 || w.PreviewLastConnAt == nil || !w.PreviewLastConnAt.Equal(last) {
		t.Fatalf("web after its restart: open %v, last %v; want 0 and %s kept", w.PreviewOpenConns, w.PreviewLastConnAt, last)
	}
}
