// Package devservertest is a fake dev container for tests of zcp's dev-server
// path: it answers the SSH commands zcp sends to a dev service — the
// supervisor's `zcp-devsup ctl`, the declaration and zerops.yaml reads and
// writes, the in-container health probe and a zcli push — the way a container
// running zcp-devsup does (docs/spec-mate.md §2.8a.2). Only tests import it.
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
)

// Container is one dev container. Zero value: no zcp-devsup, no files.
type Container struct {
	mu sync.Mutex
	// Installed: the zcp-devsup binary is in the image. Running: its
	// supervisor answers the control socket (run.start is `zcp-devsup run`).
	Installed, Running bool
	// SupervisorVersion and Formats answer `ctl version`.
	SupervisorVersion string
	Formats           []int
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
	// Crash names servers that end on their own right after they start.
	Crash map[string]bool
	// Settle names servers that answer starting for that many reads of the
	// state after they start, and only then as running or as Crash says.
	Settle   map[string]int
	settling map[string]int
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
	c := &Container{Installed: true, Running: true, SupervisorVersion: "v9.300.0", Formats: []int{1}, Declaration: decl, YAMLFile: "zerops.yaml", YAML: SupervisedYAML}
	c.boot()
	return c
}

// ExecSSH answers one command.
func (c *Container) ExecSSH(_ context.Context, _ string, command string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Commands = append(c.Commands, command)
	switch {
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
		c.SupervisorVersion, c.Formats = "v9.300.0", []int{1}
	}
	c.Servers = nil
	if c.Running {
		c.boot()
	}
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
		st.Exit = &devsup.Exit{Code: -1, Signal: devsup.ExitPortBusy, At: now}
	case c.Crash[name]:
		st.Status, st.Pid, st.Pgid = devsup.StatusCrashed, 0, 0
		st.Exit = &devsup.Exit{Code: 1, At: now}
	default:
		c.pid += 100
		st.Status, st.Pid, st.Pgid, st.Exit = devsup.StatusRunning, 700+c.pid, 700+c.pid, nil
	}
}

func (c *Container) state(only string) devsup.State {
	st := devsup.State{Format: 1, Supervisor: devsup.SupervisorInfo{Version: c.SupervisorVersion, Pid: 412}, Servers: map[string]devsup.ServerState{}}
	if d, err := devsup.ParseDeclaration([]byte(c.Declaration)); c.Declaration != "" {
		st.Declaration.Version = d.Version
		if err != nil {
			st.Declaration.Error = err.Error()
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
				server.Status, server.Exit = devsup.StatusStarting, nil
			}
			st.Servers[n] = server
		}
	}
	return st
}

func (c *Container) read() string {
	var b strings.Builder
	if c.Installed {
		fmt.Fprintf(&b, "%s\n@@zcp-devsup version 0\n", encode(map[string]any{"version": c.SupervisorVersion, "formats": c.Formats}))
		if c.Running {
			fmt.Fprintf(&b, "%s\n@@zcp-devsup status 0\n", encode(c.state("")))
		} else {
			fmt.Fprintf(&b, "{\"code\":2,\"error\":\"no zcp-devsup is running here\"}\n@@zcp-devsup status 2\n")
		}
	} else {
		b.WriteString("@@zcp-devsup version 127\n")
	}
	if c.Declaration != "" {
		fmt.Fprintf(&b, "%s\n@@zcp-devsup declaration 0\n", c.Declaration)
	} else {
		b.WriteString("@@zcp-devsup declaration 1\n")
	}
	return b.String()
}

func (c *Container) ctl(command string) string {
	if !c.Installed {
		return "@@zcp-devsup ctl 127\n"
	}
	if !c.Running {
		return "{\"code\":2,\"error\":\"no zcp-devsup is running here\"}\n@@zcp-devsup ctl 2\n"
	}
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
		if derr != nil {
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
	default:
		return fail(1, "unknown verb "+verb)
	}
	only := ""
	if verb == "status" {
		only = name
	}
	return encode(c.state(only)) + "\n@@zcp-devsup ctl 0\n"
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
