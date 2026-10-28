package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// mountProbe stays indexed until Wait has reaped the process. A FUSE syscall
// in uninterruptible sleep can survive Kill: an expired observer must not
// start another probe beside it. Read and write observations share this slot.
type mountProbe struct {
	done     chan struct{}
	writable bool
	cmd      *exec.Cmd
	canceled bool
	err      error
}

// observePath bounds the observer, not the filesystem syscall. No goroutine
// calls mount-side Go I/O; only the tracked child can become stuck in FUSE.
func (m *SystemMounter) observePath(parent context.Context, path string, writable bool) error {
	ctx, cancel := context.WithTimeout(parent, mountCheckTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("mount probe: %w", err)
	}

	// The shared process outlives an individual observer; explicit Kill and
	// Wait below own cancellation/reaping, including uninterruptible I/O.
	p := m.pathProbe(context.WithoutCancel(ctx), path, writable)

	select {
	case <-ctx.Done():
		m.probeMu.Lock()
		p.canceled = true
		if p.cmd != nil {
			_ = p.cmd.Process.Kill()
		}
		m.probeMu.Unlock()
		return fmt.Errorf("mount probe observation: %w", ctx.Err())
	case <-p.done:
		m.probeMu.Lock()
		if m.probes[path] == p {
			delete(m.probes, path)
		}
		m.probeMu.Unlock()
		if p.err != nil {
			return fmt.Errorf("mount probe: %w", p.err)
		}
		if p.writable != writable {
			return fmt.Errorf("mount probe mode changed; observe again")
		}
		return nil
	}
}

// pathProbe starts or joins one process lifecycle for this path.
func (m *SystemMounter) pathProbe(ctx context.Context, path string, writable bool) *mountProbe {
	m.probeMu.Lock()
	if m.probes == nil {
		m.probes = make(map[string]*mountProbe)
	}
	p := m.probes[path]
	if p == nil {
		p = &mountProbe{done: make(chan struct{}), writable: writable}
		m.probes[path] = p
		go m.runProbe(ctx, path, p)
	}
	m.probeMu.Unlock()

	return p
}

func (m *SystemMounter) runProbe(ctx context.Context, path string, p *mountProbe) {
	defer close(p.done)
	if !p.writable {
		p.err = m.probeExec(ctx, p, "stat", "-L", "--", path)
		return
	}
	// Noclobber creates an exclusive destination before exec replaces the shell
	// with cp in the same tracked process. cp reports open,
	// write and close failures; rm's completion proves the probe was removed.
	probePath := filepath.Join(path, ".mount_probe_"+rand.Text())
	if err := m.probeExec(ctx, p, "sh", "-c", `set -C; : > "$1" || exit; exec cp -- /dev/null "$1"`, "mount-probe", probePath); err != nil {
		p.err = fmt.Errorf("create %s: %w", probePath, err)
		return
	}
	if err := m.probeExec(ctx, p, "rm", "--", probePath); err != nil {
		p.err = fmt.Errorf("remove %s: %w", probePath, err)
	}
}

func (m *SystemMounter) probeExec(ctx context.Context, p *mountProbe, name string, args ...string) error {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	if m.probeCommand != nil {
		cmd = m.probeCommand(name, args...)
	}
	// Fixed diagnostic language lets CheckMount recognize disconnected endpoints
	// while all other command/read errors remain unknown.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Stdout, cmd.Stderr = &out, &out
	m.probeMu.Lock()
	if p.canceled {
		m.probeMu.Unlock()
		return fmt.Errorf("mount probe canceled")
	}
	if err := cmd.Start(); err != nil {
		m.probeMu.Unlock()
		return fmt.Errorf("start %s: %w", name, err)
	}
	p.cmd = cmd
	m.probeMu.Unlock()
	err := cmd.Wait()
	m.probeMu.Lock()
	p.cmd = nil
	m.probeMu.Unlock()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, out.String())
	}
	return nil
}
