// Package devservertest is a fake dev container for tests of zcp's dev-server
// path: it answers the SSH commands zcp sends to a dev service — the
// supervisor's `zcp-devsup ctl`, zcp's start of a lease's supervisor, the
// declaration and zerops.yaml reads and writes, the in-container health probe
// and a zcli push — the way a container running zcp-devsup does
// (docs/spec-mate.md §2.8a.2). Only tests import it.
package devservertest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
)

// Container is one dev container. Zero value: no zcp-devsup, no files.
type Container struct {
	mu sync.Mutex
	// Installed: the zcp-devsup binary is in the image. Running: its
	// supervisor answers the control socket (run.start is `zcp-devsup run`).
	Installed, Running bool
	// Unresponsive: the supervisor runs but does not answer its control
	// socket in time (a thrashing container); `ctl` exits 5.
	Unresponsive bool
	// FailStart: a supervisor zcp starts in the container (a lease's, which
	// run.start never starts again after a reset) does not come up; its own
	// output is StartOutput.
	FailStart   bool
	StartOutput string
	// SupervisorStarts counts the supervisors zcp started in the container.
	SupervisorStarts int
	// Prepared are the repository's prepare commands zcp ran, in order; with
	// PrepareFails each fails (exit 1) with PrepareOutput.
	Prepared      []string
	PrepareFails  bool
	PrepareOutput string
	// Silent: the container itself does not answer over SSH in time (out of
	// memory or CPU); every command ends at the ssh deadline.
	Silent bool
	// SupervisorVersion, Formats and Capabilities answer `ctl version`;
	// Capabilities nil is a supervisor older than any (its answer carries no
	// capabilities key, and its ctl fails on `env`).
	SupervisorVersion string
	Formats           []int
	Capabilities      []string
	// Env is the overlay `ctl env` last set. A change restarts every running
	// server under it, as the supervisor does in its own goroutine: the env
	// answer shows them as they were, the next answer restarted — or the one
	// after EnvRestartReads more answers.
	Env             map[string]string
	EnvRestartReads int
	// EnvRefusal, when set, is the error the supervisor refuses `ctl env`
	// with (a state directory it cannot write, say); the overlay stays.
	EnvRefusal  string
	envRestarts []string
	envReads    int
	// Declaration is .zcp/dev-servers.json; "" when absent.
	Declaration string
	// YAMLFile and YAML are the repo's zerops.yaml; "" when absent.
	YAMLFile, YAML string
	// Servers is the supervisor's state.
	Servers map[string]*devsup.ServerState
	// HTTP is the code a running server's port answers (200 when unset; 0
	// refuses the connection, so the supervisor reports the server starting);
	// Foreign is a listener outside the supervisor: it answers its port while
	// no running server holds it, and a server started on it finds the port
	// busy. Any other port refuses the connection.
	HTTP, Foreign map[int]int
	// Loopback names the ports a running server listens on only at
	// 127.0.0.1; every other running server's port listens on all addresses.
	Loopback map[int]bool
	// Crash names servers that end on their own right after they start;
	// RetryPending names those of them the supervisor starts once more.
	Crash, RetryPending map[string]bool
	// TaskLimit names those of them that crashed because the service's task
	// limit was reached, with the limit the supervisor reports.
	TaskLimit map[string]*devsup.TaskLimit
	// Settle names servers that answer starting for that many reads of the
	// state after they start, and only then as running or as Crash says.
	Settle   map[string]int
	settling map[string]int
	// StableNumber is the container's ZEROPS_StableNumber (a pool
	// container's slot; 0: none). With the declaration's previewPortBase the
	// preview server's state carries its preview port, base + StableNumber,
	// as the supervisor's forward does.
	StableNumber int
	// Logs is each server's log.
	Logs map[string][]string
	// Commands records every command, in order.
	Commands []string
	// Deploys counts zcli pushes (each one replaces the container).
	Deploys int
	// DirtyCrew names the crew copies (.crew/<name>) with uncommitted or
	// untracked changes.
	DirtyCrew []string
	// MemMaxB, MemCurrentB, MemInactiveB and MemActiveB (memory.stat's
	// inactive_file and active_file) answer the cgroup memory probe in bytes;
	// MemMaxB 0 means the cgroup files are unreadable.
	MemMaxB, MemCurrentB, MemInactiveB, MemActiveB int64
	pid                                            int
}

// SupervisedYAML is a zerops.yaml whose dev setup runs the supervisor.
const SupervisedYAML = `zerops:
  - setup: dev
    run:
      prepareCommands:
        - sudo sh -c 'u=https://github.com/zeropsio/zcp/releases/download/v9.300.0/zcp-devsup-linux-amd64; f=/usr/local/bin/zcp-devsup; if command -v curl >/dev/null; then curl -fsSL "$u" -o "$f"; else wget -qO "$f" "$u"; fi && chmod 0755 "$f"'
      start: zcp-devsup run
`

// NewSupervised is a container whose dev setup runs the supervisor, with the
// declaration decl (JSON; "" for none) whose servers all run.
func NewSupervised(decl string) *Container {
	c := &Container{Installed: true, Running: true, SupervisorVersion: "v9.300.0", Formats: []int{1}, Capabilities: slices.Clone(devsup.Capabilities),
		Declaration: decl, YAMLFile: "zerops.yaml", YAML: SupervisedYAML}
	c.boot()
	return c
}

// ExecSSH answers one command.
func (c *Container) ExecSSH(_ context.Context, hostname string, command string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Commands = append(c.Commands, command)
	if c.Silent {
		return nil, &platform.SSHExecError{Hostname: hostname, Err: fmt.Errorf("ssh stopped: %w (signal: killed)", context.DeadlineExceeded)}
	}
	switch {
	case strings.Contains(command, "@@zcp-devsup check"):
		return []byte(c.ensure()), nil
	case strings.Contains(command, "@@zcp-devsup declaration") && !strings.Contains(command, "@@zcp-devsup version"):
		if c.Declaration == "" {
			return []byte("@@zcp-devsup declaration 1\n"), nil
		}
		return []byte(c.Declaration + "\n@@zcp-devsup declaration 0\n"), nil
	case strings.Contains(command, "/bin/sh -c "):
		c.Prepared = append(c.Prepared, firstQuoted(command, "/bin/sh -c "))
		if c.PrepareFails {
			return []byte(c.PrepareOutput), &platform.SSHExecError{Hostname: hostname, Output: c.PrepareOutput, Err: errors.New("exit status 1")}
		}
		return []byte(c.PrepareOutput), nil
	case strings.Contains(command, "@@zcp-devsup version"):
		return []byte(c.read()), nil
	case strings.Contains(command, "zcp-devsup ctl logs"):
		return c.logs(command)
	case strings.Contains(command, "@@zcp-devsup ctl"):
		return []byte(c.ctl(command)), nil
	case strings.Contains(command, "dev-servers.json.tmp"):
		c.Declaration = strings.TrimSuffix(firstQuoted(command, "printf '%s\\n' "), "\n")
		return nil, nil
	case strings.Contains(command, "@@zcp-memory"):
		if c.MemMaxB == 0 {
			return nil, errors.New("exit status 1")
		}
		return fmt.Appendf(nil, "@@zcp-memory %d %d %d %d\n", c.MemMaxB, c.MemCurrentB, c.MemInactiveB, c.MemActiveB), nil
	case strings.Contains(command, "@@zcp-crew-dirty"):
		var out strings.Builder
		for _, name := range c.DirtyCrew {
			out.WriteString("@@zcp-crew-dirty " + name + "\n")
		}
		return []byte(out.String()), nil
	case strings.Contains(command, "@@zcp-file"):
		if c.YAML == "" {
			return []byte("@@zcp-file -\n"), nil
		}
		return []byte("@@zcp-file " + c.YAMLFile + "\n" + c.YAML), nil
	case strings.Contains(command, "echo @@yaml"):
		// The parts read (ops.ReadPartsSignal): the repo's zerops.yaml and
		// declaration; no workspace markers or packages.
		file := ""
		if c.YAML != "" {
			file = c.YAMLFile
		}
		return []byte("@@file\n" + file + "\n@@yaml\n" + c.YAML + "\n@@declaration\n" + c.Declaration + "\n@@markers\n@@packages\n"), nil
	case strings.Contains(command, ".zcp-tmp"):
		c.YAML = firstQuoted(command, "printf '%s' ")
		return nil, nil
	case strings.Contains(command, "for i in $(seq"):
		code := c.httpCode(command)
		if code >= 200 && code < 400 {
			return fmt.Appendf(nil, "OK %d 123", code), nil // 123: the probe's own in-container measure
		}
		return fmt.Appendf(nil, "FAIL %03d", code), errors.New("exit status 1")
	case strings.HasPrefix(command, "ss -ltn"):
		return []byte(c.listen()), nil
	case strings.Contains(command, "curl -s -o /dev/null"):
		return fmt.Appendf(nil, "%03d", c.httpCode(command)), nil
	case strings.Contains(command, "rev-parse --verify HEAD") && strings.Contains(command, "git status --porcelain"):
		return nil, errors.New("fatal: not a git repository")
	case strings.Contains(command, "zcli push"):
		c.Deploys++
		c.redeploy()
		return []byte("ok"), nil
	}
	return []byte("ok"), nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (c *Container) ExecSSHStdin(ctx context.Context, hostname, command string, _ []byte) ([]byte, error) {
	return c.ExecSSH(ctx, hostname, command)
}

// ExecSSHBackground answers like ExecSSH; zcp's dev-server path never uses it.
func (c *Container) ExecSSHBackground(ctx context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	return c.ExecSSH(ctx, hostname, command)
}

// Sent returns the recorded commands.
func (c *Container) Sent() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.Commands)
}

// Status is a server's status, "" when the supervisor does not hold it.
func (c *Container) Status(name string) devsup.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.Servers[name]; ok {
		return s.Status
	}
	return ""
}

// Overlay is a copy of the overlay `ctl env` last set.
func (c *Container) Overlay() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.Env)
}

// Restarts is how often the supervisor restarted name, and why it last did.
func (c *Container) Restarts(name string) (int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.Servers[name]; ok {
		return s.Restarts, s.LastRestartReason
	}
	return 0, ""
}

// Stop ends every process in the container, as a stop of it does: the
// supervisor and its servers end; its files stay.
func (c *Container) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Running, c.Servers = false, nil
}

// Redeploy replaces the container as a deploy does.
func (c *Container) Redeploy() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Deploys++
	c.redeploy()
}

// redeploy is a new container: $HOME is empty, the image installs zcp-devsup
// when the dev setup's prepareCommands download it, and run.start starts the
// supervisor (which starts every declared server) when it says so.
func (c *Container) redeploy() {
	c.Installed = strings.Contains(c.YAML, "/zcp-devsup-linux-amd64")
	c.Running = c.Installed && regexp.MustCompile(`(?m)^\s*start: zcp-devsup run(\s+--root\s+\S+)?\s*$`).MatchString(c.YAML)
	if c.Installed && c.SupervisorVersion == "" {
		c.SupervisorVersion, c.Formats, c.Capabilities = "v9.300.0", []int{1}, slices.Clone(devsup.Capabilities)
	}
	c.Servers, c.Env, c.envRestarts = nil, nil, nil
	if c.Running {
		c.boot()
	}
}

// ensure answers zcp's start of a lease's supervisor: nothing is started
// without the binary or while a supervisor holds the control socket, or
// answers — otherwise one starts and boots the declaration, unless FailStart.
// The overlay and a reload are calls of their own (ctl).
func (c *Container) ensure() string {
	switch {
	case !c.Installed:
		return "@@zcp-devsup check 127\n"
	case c.Unresponsive:
		return "@@zcp-devsup check 5\n"
	case c.Running:
		return "@@zcp-devsup check 0\n"
	}
	c.SupervisorStarts++
	if c.FailStart {
		return "@@zcp-devsup check 2\n@@zcp-devsup started 2\n" + c.StartOutput + "\n@@zcp-devsup log 0\n"
	}
	c.Running = true
	if c.SupervisorVersion == "" {
		c.SupervisorVersion, c.Formats, c.Capabilities = "v9.300.0", []int{1}, slices.Clone(devsup.Capabilities)
	}
	c.boot()
	return "@@zcp-devsup check 2\n@@zcp-devsup started 0\n"
}

// boot gives every declared server its first desired state (autostart),
// as a supervisor does on a fresh $HOME.
func (c *Container) boot() {
	c.Servers = map[string]*devsup.ServerState{}
	d, err := devsup.ParseDeclaration([]byte(c.Declaration))
	if c.Declaration == "" || err != nil {
		return
	}
	for _, s := range d.Servers {
		if s.FirstDesired() == devsup.DesiredStopped {
			c.Servers[s.Name] = &devsup.ServerState{Source: devsup.SourceDeclared, Desired: devsup.DesiredStopped, Status: devsup.StatusStopped,
				LogPath: "/home/zerops/.zcp/devsup/logs/" + s.Name + ".log"}
			continue
		}
		c.start(s.Name, false)
	}
}

func (c *Container) start(name string, restart bool) {
	if c.Servers == nil {
		c.Servers = map[string]*devsup.ServerState{}
	}
	st := c.Servers[name]
	if st == nil {
		st = &devsup.ServerState{Source: devsup.SourceDeclared, LogPath: "/home/zerops/.zcp/devsup/logs/" + name + ".log"}
		c.Servers[name] = st
	} else if restart || st.Status != devsup.StatusRunning {
		st.Restarts++
	}
	if st.Status == devsup.StatusRunning && !restart {
		return
	}
	st.Desired = devsup.DesiredRunning
	if n := c.Settle[name]; n > 0 {
		if c.settling == nil {
			c.settling = map[string]int{}
		}
		c.settling[name] = n
	}
	now := time.Date(2026, 10, 14, 21, 0, 0, 0, time.UTC)
	st.StartedAt = &now
	switch {
	case c.Foreign[c.declaredPort(name)] != 0:
		st.Status, st.Pid, st.Pgid = devsup.StatusCrashed, 0, 0
		st.Exit, st.RetryPending, st.TaskLimit = &devsup.Exit{Code: -1, Signal: devsup.ExitPortBusy, At: now}, false, nil
	case c.Crash[name]:
		st.Status, st.Pid, st.Pgid = devsup.StatusCrashed, 0, 0
		st.Exit, st.RetryPending, st.TaskLimit = &devsup.Exit{Code: 1, At: now}, c.RetryPending[name], c.TaskLimit[name]
	default:
		c.pid += 100
		st.Status, st.Pid, st.Pgid, st.Exit, st.RetryPending, st.TaskLimit = devsup.StatusRunning, 700+c.pid, 700+c.pid, nil, false, nil
	}
}

func (c *Container) state(only string) devsup.State {
	st := devsup.State{Format: 1, Supervisor: devsup.SupervisorInfo{Version: c.SupervisorVersion, Pid: 412}, Env: maps.Clone(c.Env), Servers: map[string]devsup.ServerState{}}
	preview, previewPort := "", 0
	if d, err := devsup.ParseDeclaration([]byte(c.Declaration)); c.Declaration != "" {
		st.Declaration.Version = d.Version
		if err != nil {
			st.Declaration.Error = err.Error()
		}
		if ps, ok := devsup.PreviewServer(d.Servers); ok && err == nil && d.PreviewPortBase > 0 && c.StableNumber > 0 {
			preview, previewPort = ps.Name, d.PreviewPortBase+c.StableNumber
		}
	}
	for n, s := range c.Servers {
		if only == "" || n == only {
			server := *s
			if code, ok := c.HTTP[c.declaredPort(n)]; ok && code == 0 && server.Status == devsup.StatusRunning && c.declaredPort(n) != 0 {
				server.Status = devsup.StatusStarting
			}
			if c.settling[n] > 0 {
				c.settling[n]--
				server.Status, server.Exit, server.RetryPending, server.TaskLimit = devsup.StatusStarting, nil, false, nil
			}
			if n == preview {
				server.PreviewPort = previewPort
			}
			st.Servers[n] = server
		}
	}
	return st
}

func (c *Container) read() string {
	var b strings.Builder
	if c.Installed {
		c.applyEnvRestarts()
		version := map[string]any{"version": c.SupervisorVersion, "formats": c.Formats}
		if c.Capabilities != nil {
			version["capabilities"] = c.Capabilities
		}
		fmt.Fprintf(&b, "%s\n@@zcp-devsup version 0\n", encode(version))
		switch {
		case c.Unresponsive:
			fmt.Fprintf(&b, "{\"code\":5,\"error\":\"zcp-devsup is running here but did not answer\"}\n@@zcp-devsup status 5\n")
		case c.Running:
			fmt.Fprintf(&b, "%s\n@@zcp-devsup status 0\n", encode(c.state("")))
		default:
			fmt.Fprintf(&b, "{\"code\":2,\"error\":\"no zcp-devsup is running here\"}\n@@zcp-devsup status 2\n")
		}
	} else {
		b.WriteString("@@zcp-devsup version 127\n")
	}
	fmt.Fprintf(&b, "%s@@zcp-devsup listen 0\n", c.listen())
	if c.Declaration != "" {
		fmt.Fprintf(&b, "%s\n@@zcp-devsup declaration 0\n", c.Declaration)
	} else {
		b.WriteString("@@zcp-devsup declaration 1\n")
	}
	return b.String()
}

// listen is `ss -ltn` of the running servers' ports.
func (c *Container) listen() string {
	var b strings.Builder
	b.WriteString("State  Recv-Q Send-Q Local Address:Port Peer Address:Port\n")
	for name, s := range c.Servers {
		port := c.declaredPort(name)
		switch {
		case port == 0 || s.Status != devsup.StatusRunning:
		case c.Loopback[port]:
			fmt.Fprintf(&b, "LISTEN 0      511        127.0.0.1:%d      0.0.0.0:*\n", port)
		default:
			fmt.Fprintf(&b, "LISTEN 0      4096               *:%d            *:*\n", port)
		}
	}
	return b.String()
}

func (c *Container) ctl(command string) string {
	if !c.Installed {
		return "@@zcp-devsup ctl 127\n"
	}
	if c.Unresponsive {
		return "{\"code\":5,\"error\":\"zcp-devsup is running here but did not answer\"}\n@@zcp-devsup ctl 5\n"
	}
	if !c.Running {
		return "{\"code\":2,\"error\":\"no zcp-devsup is running here\"}\n@@zcp-devsup ctl 2\n"
	}
	c.applyEnvRestarts()
	args := ctlArgs(command)
	verb, name := args[0], ""
	if len(args) > 1 {
		name = args[1]
	}
	fail := func(code int, msg string) string {
		return fmt.Sprintf("%s\n@@zcp-devsup ctl %d\n", encode(map[string]any{"code": code, "error": msg}), code)
	}
	declared := map[string]devsup.Server{}
	d, derr := devsup.ParseDeclaration([]byte(c.Declaration))
	if c.Declaration != "" && derr == nil {
		for _, s := range d.Servers {
			declared[s.Name] = s
		}
	}
	switch verb {
	case "status":
		if _, ok := c.Servers[name]; name != "" && !ok {
			return fail(3, "no server named "+name)
		}
	case "start", "restart":
		if _, ok := declared[name]; !ok {
			return fail(3, "no server named "+name)
		}
		c.start(name, verb == "restart")
	case "stop":
		st, ok := c.Servers[name]
		if !ok {
			return fail(3, "no server named "+name)
		}
		st.Desired, st.Status, st.Pid, st.Pgid = devsup.DesiredStopped, devsup.StatusStopped, 0, 0
	case "reload":
		// A missing file is an empty declaration, as devsup.LoadDeclaration reads it.
		if c.Declaration != "" && derr != nil {
			return fail(4, derr.Error())
		}
		for n := range maps.Clone(c.Servers) {
			if _, ok := declared[n]; !ok {
				delete(c.Servers, n)
			}
		}
		for n := range declared {
			if _, ok := c.Servers[n]; !ok {
				c.start(n, false)
			}
		}
	case "env":
		if !slices.Contains(c.Capabilities, devsup.CapabilityEnv) {
			return fail(1, "unknown verb env")
		}
		if c.EnvRefusal != "" {
			return fail(1, c.EnvRefusal)
		}
		env, err := envArgs(args[1:])
		if err != nil {
			return fail(1, err.Error())
		}
		c.setEnv(env)
	default:
		return fail(1, "unknown verb "+verb)
	}
	only := ""
	if verb == "status" {
		only = name
	}
	return encode(c.state(only)) + "\n@@zcp-devsup ctl 0\n"
}

// envArgs reads `env`'s flags: the --set pairs, or --clear alone.
func envArgs(args []string) (map[string]string, error) {
	env, cleared := map[string]string{}, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--clear":
			cleared = true
		case "--set":
			if i+1 >= len(args) {
				return nil, errors.New("--set needs a value")
			}
			i++
			k, v, ok := strings.Cut(args[i], "=")
			if !ok {
				return nil, fmt.Errorf("--set %q is not NAME=VALUE", args[i])
			}
			env[k] = v
		default:
			return nil, fmt.Errorf("env: unexpected %q", args[i])
		}
	}
	if !cleared && len(env) == 0 {
		return nil, errors.New("env needs --set NAME=VALUE or --clear")
	}
	return env, nil
}

// setEnv replaces the overlay; a change restarts every running server, after
// this answer (applyEnvRestarts).
func (c *Container) setEnv(env map[string]string) {
	if maps.Equal(c.Env, env) {
		return
	}
	c.Env = env
	c.envRestarts, c.envReads = nil, c.EnvRestartReads
	for _, n := range slices.Sorted(maps.Keys(c.Servers)) {
		if st := c.Servers[n]; st.Status == devsup.StatusRunning && st.Desired == devsup.DesiredRunning {
			c.envRestarts = append(c.envRestarts, n)
		}
	}
}

// applyEnvRestarts restarts the servers an overlay change left to restart,
// once EnvRestartReads answers have passed since it.
func (c *Container) applyEnvRestarts() {
	if len(c.envRestarts) == 0 {
		return
	}
	if c.envReads > 0 {
		c.envReads--
		return
	}
	for _, n := range c.envRestarts {
		if st := c.Servers[n]; st != nil && st.Status == devsup.StatusRunning {
			c.pid += 100
			st.Restarts++
			st.Pid, st.Pgid, st.LastRestartReason = 700+c.pid, 700+c.pid, "env changed"
		}
	}
	c.envRestarts = nil
}

func (c *Container) logs(command string) ([]byte, error) {
	args := ctlArgs(command)
	name := ""
	if len(args) > 1 {
		name = args[1]
	}
	lines, ok := c.Logs[name]
	if !ok {
		return []byte(`{"code":3,"error":"no log"}`), errors.New("exit status 3")
	}
	return []byte(encode(map[string]any{"name": name, "lines": lines})), nil
}

var portRe = regexp.MustCompile(`http://localhost:(\d+)`)

func (c *Container) httpCode(command string) int {
	m := portRe.FindStringSubmatch(command)
	if m == nil {
		return 0
	}
	port, _ := strconv.Atoi(m[1])
	for name, st := range c.Servers {
		if st.Status == devsup.StatusRunning && port != 0 && c.declaredPort(name) == port {
			if code, ok := c.HTTP[port]; ok {
				return code
			}
			return 200
		}
	}
	return c.Foreign[port]
}

// declaredPort is name's port in the declaration, 0 when none.
func (c *Container) declaredPort(name string) int {
	d, err := devsup.ParseDeclaration([]byte(c.Declaration))
	if c.Declaration == "" || err != nil {
		return 0
	}
	for _, s := range d.Servers {
		if s.Name == name {
			return s.Port
		}
	}
	return 0
}

// encode is v as JSON, or a ctl failure naming why it is not.
func encode(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"code":1,"error":%q}`, err.Error())
	}
	return string(b)
}

// ctlArgs reads the single-quoted arguments after "zcp-devsup ctl ".
func ctlArgs(command string) []string {
	_, rest, _ := strings.Cut(command, "zcp-devsup ctl ")
	rest, _, _ = strings.Cut(rest, " --")
	out := make([]string, 0, 2)
	for f := range strings.FieldsSeq(rest) {
		out = append(out, strings.Trim(f, "'"))
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// firstQuoted unquotes the POSIX single-quoted word right after prefix.
func firstQuoted(command, prefix string) string {
	_, rest, ok := strings.Cut(command, prefix)
	if !ok || !strings.HasPrefix(rest, "'") {
		return ""
	}
	var b strings.Builder
	for i := 1; i < len(rest); i++ {
		if rest[i] != '\'' {
			b.WriteByte(rest[i])
			continue
		}
		if strings.HasPrefix(rest[i:], `'\''`) {
			b.WriteByte('\'')
			i += 3
			continue
		}
		break
	}
	return b.String()
}
