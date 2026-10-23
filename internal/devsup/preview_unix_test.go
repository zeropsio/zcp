//go:build linux || darwin

package devsup

import (
	"bytes"
	"context"
	"net"
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
