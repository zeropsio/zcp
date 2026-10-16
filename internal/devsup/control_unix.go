//go:build linux || darwin

package devsup

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Control verbs carried over the socket; `logs` and `version` are answered by
// `ctl` itself.
const (
	VerbStatus  = "status"
	VerbStart   = "start"
	VerbStop    = "stop"
	VerbRestart = "restart"
	VerbReload  = "reload"
	VerbRun     = "run"
	VerbRm      = "rm"
)

// `ctl` exit codes (docs/spec-mate.md §2.8a.2).
const (
	CodeOK                 = 0
	CodeNoSupervisor       = 2
	CodeUnknownName        = 3
	CodeInvalidDeclaration = 4
	// CodeUnresponsive: a supervisor holds the socket but did not answer in
	// time — never "no supervisor".
	CodeUnresponsive = 5
	// CodeFailed is any other failure (a bad request, a refused runtime entry).
	CodeFailed = 1
)

// Request is one control call: a JSON line on the supervisor's socket.
type Request struct {
	Verb string `json:"verb"`
	Name string `json:"name,omitempty"`
	// Server is the runtime entry of `run`.
	Server *Server `json:"server,omitempty"`
}

// Response answers a Request with the state after it was applied.
type Response struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
	State State  `json:"state"`
}

// ErrNoSupervisor means nothing answers on the state directory's socket.
var ErrNoSupervisor = errors.New("no zcp-devsup is running here (the dev setup's run.start must be `zcp-devsup run`)")

// ErrUnresponsive means a supervisor holds the state directory's socket but
// did not take or answer the call in time (a container out of memory or CPU).
var ErrUnresponsive = errors.New("zcp-devsup is running here but did not answer in time (the container may be out of memory or CPU)")

// SocketPath is the control socket in the state directory.
func SocketPath(dir string) string { return filepath.Join(dir, sockFile) }

// listenControl binds the control socket, replacing one a killed instance left
// behind; a socket that still answers belongs to a running supervisor.
func listenControl(dir string) (net.Listener, error) {
	p := SocketPath(dir)
	d := net.Dialer{Timeout: time.Second}
	if c, err := d.DialContext(context.Background(), "unix", p); err == nil {
		_ = c.Close()
		return nil, fmt.Errorf("another zcp-devsup is running on %s", p)
	}
	_ = os.Remove(p)
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", p)
	if err != nil {
		return nil, fmt.Errorf("control socket: %w", err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("control socket: %w", err)
	}
	return ln, nil
}

func (s *Supervisor) serveControl(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *Supervisor) handle(c net.Conn) {
	defer c.Close()
	var req Request
	resp := Response{Code: CodeFailed}
	if err := json.NewDecoder(bufio.NewReader(c)).Decode(&req); err != nil {
		resp.Error = "bad request: " + err.Error()
	} else {
		resp = s.apply(req)
	}
	if err := json.NewEncoder(c).Encode(resp); err != nil {
		s.logf("control answer: %v", err)
	}
}

// apply runs one verb and answers with the state after it.
func (s *Supervisor) apply(req Request) Response {
	var err error
	switch req.Verb {
	case VerbStatus:
	case VerbStart:
		err = s.Start(req.Name)
	case VerbStop:
		err = s.Stop(req.Name)
	case VerbRestart:
		err = s.Restart(req.Name)
	case VerbReload:
		s.Reload()
	case VerbRun:
		if req.Server == nil {
			err = errors.New("run needs a server")
		} else {
			err = s.RunEntry(*req.Server)
		}
	case VerbRm:
		err = s.Remove(req.Name)
	default:
		err = fmt.Errorf("unknown verb %q", req.Verb)
	}
	resp := Response{State: s.State()}
	if req.Verb == VerbStatus && req.Name != "" {
		one, ok := resp.State.Servers[req.Name]
		if !ok {
			err = fmt.Errorf("%w: no server named %q", ErrUnknownServer, req.Name)
		}
		resp.State.Servers = map[string]ServerState{}
		if ok {
			resp.State.Servers[req.Name] = one
		}
	}
	switch {
	case errors.Is(err, ErrUnknownServer):
		resp.Code, resp.Error = CodeUnknownName, err.Error()
	case err != nil:
		resp.Code, resp.Error = CodeFailed, err.Error()
	case req.Verb == VerbReload && resp.State.Declaration.Error != "":
		resp.Code, resp.Error = CodeInvalidDeclaration, resp.State.Declaration.Error
	}
	return resp
}

// Control sends one request to the supervisor whose state directory is dir.
// Every verb is idempotent: a caller that lost the answer asks status again.
func Control(dir string, req Request, timeout time.Duration) (Response, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	c, err := d.DialContext(context.Background(), "unix", SocketPath(dir))
	if err != nil {
		return dialFailure(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return exchangeFailure(err)
	}
	var resp Response
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		return exchangeFailure(err)
	}
	return resp, nil
}

// dialFailure is a dial that reached no supervisor: only an absent socket or a
// refused connection means none runs; one that timed out or found the backlog
// full reached a supervisor that does not keep up.
func dialFailure(err error) (Response, error) {
	if unanswered(err) {
		err = fmt.Errorf("%w: %w", ErrUnresponsive, err)
		return Response{Code: CodeUnresponsive, Error: err.Error()}, err
	}
	err = fmt.Errorf("%w: %w", ErrNoSupervisor, err)
	return Response{Code: CodeNoSupervisor, Error: err.Error()}, err
}

// exchangeFailure is a request or answer lost on an open connection.
func exchangeFailure(err error) (Response, error) {
	if unanswered(err) {
		err = fmt.Errorf("%w: %w", ErrUnresponsive, err)
		return Response{Code: CodeUnresponsive, Error: err.Error()}, err
	}
	return Response{Code: CodeFailed}, fmt.Errorf("devsup control: %w", err)
}

// unanswered says the supervisor behind the socket did not keep up: a
// deadline passed or its accept backlog is full.
func unanswered(err error) bool {
	var ne net.Error
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}
