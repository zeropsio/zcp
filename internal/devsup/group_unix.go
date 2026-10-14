//go:build linux || darwin

package devsup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// pollInterval is how often a stop or a port wait looks again.
const pollInterval = 50 * time.Millisecond

// group is one running dev server: the leader started in its own process
// group, so a stop reaches every process the command forks.
type group struct {
	pgid        int
	leaderStart int64
	done        chan struct{} // closed once the leader is reaped
	state       *os.ProcessState
}

// startGroup starts command under /bin/sh -c in dir as the leader of a new
// process group, with stdout and stderr appended to logFile.
func startGroup(command, dir string, env []string, logFile *os.File) (*group, error) {
	// The group outlives no context: stop is stopGroup, never a cancel.
	c := exec.CommandContext(context.Background(), "/bin/sh", "-c", command)
	c.Dir = dir
	c.Env = env
	c.Stdout, c.Stderr = logFile, logFile
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	g := &group{pgid: c.Process.Pid, done: make(chan struct{})}
	// The leader is not reaped before Wait, so its start time is its own.
	g.leaderStart, _ = processStart(g.pgid)
	go func() {
		_ = c.Wait()
		g.state = c.ProcessState
		close(g.done)
	}()
	return g, nil
}

// groupAlive reports whether any process is still in the group.
func groupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ownsGroup reports whether pgid is still the group whose leader started at
// leaderStart: the leader lives, leads its group and has that start time. A
// reused pid has another start time and is left alone.
func ownsGroup(pgid int, leaderStart int64) bool {
	if pgid <= 1 || leaderStart == 0 {
		return false
	}
	if g, err := syscall.Getpgid(pgid); err != nil || g != pgid {
		return false
	}
	start, ok := processStart(pgid)
	return ok && start == leaderStart
}

// stopGroup ends every process in the group: SIGTERM, then SIGKILL once grace
// has passed. It returns once the group is gone, or killWait after SIGKILL.
// The caller has confirmed the group is its own (ownsGroup, or a leader it
// started and has just reaped, whose id cannot be reused while members live).
func stopGroup(pgid int, grace time.Duration) error {
	if pgid <= 1 {
		return fmt.Errorf("stop group %d: not a dev server group", pgid)
	}
	if !groupAlive(pgid) {
		return nil
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	if waitGone(pgid, grace) {
		return nil
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	if waitGone(pgid, killWait) {
		return nil
	}
	return fmt.Errorf("stop group %d: still alive after SIGKILL", pgid)
}

// killWait bounds the wait for a SIGKILLed group to disappear.
const killWait = 5 * time.Second

func waitGone(pgid int, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		if !groupAlive(pgid) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}

// portFree reports whether nothing listens on port: it binds on every address
// and nothing answers on loopback.
func portFree(port int) bool {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return !portAccepts(port)
}

// portAccepts reports whether something accepts TCP on the loopback port.
func portAccepts(port int) bool {
	d := net.Dialer{Timeout: 200 * time.Millisecond}
	c, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// waitPortFree waits up to limit for port to be free.
func waitPortFree(port int, limit time.Duration) bool {
	if port == 0 {
		return true
	}
	deadline := time.Now().Add(limit)
	for {
		if portFree(port) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}
