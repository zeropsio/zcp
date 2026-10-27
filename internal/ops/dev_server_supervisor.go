package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
)

// The dev servers of a dev service run under zcp-devsup, the supervisor its dev
// setup's run.start runs (docs/spec-mate.md §2.8a.2). zcp declares them in the
// repo's .zcp/dev-servers.json and drives the supervisor with `zcp-devsup ctl`
// over SSH; it never starts, finds or kills a dev-server process itself.

// DevRepoRoot is a dev container's repo root (variant A): zerops.yaml and the
// dev-server declaration live here.
const DevRepoRoot = "/var/www"

// devRepoRoot is the repository root a dev service's supervisor runs on:
// root, or DevRepoRoot when root is empty (variant A). A variant-B dev
// service's root is its code volume's mount.
func devRepoRoot(root string) string {
	if root == "" {
		return DevRepoRoot
	}
	return strings.TrimSuffix(root, "/")
}

// devDeclarationFile is the declaration's absolute path under root.
func devDeclarationFile(root string) string {
	return devRepoRoot(root) + "/" + devsup.DeclarationPath
}

// `zcp-devsup ctl` exit codes (docs/spec-mate.md §2.8a.2); devsupCodeAbsent is
// what a container without the binary answers.
const (
	devsupCodeOK                 = 0
	devsupCodeNoSupervisor       = 2
	devsupCodeUnknownName        = 3
	devsupCodeInvalidDeclaration = 4
	devsupCodeUnresponsive       = 5
	devsupCodeAbsent             = 127
)

// devsupCtlTimeout bounds one ctl round trip; ctl's own bound is 60 s (a
// restart may wait for a stop's grace and a free port).
const devsupCtlTimeout = 75 * time.Second

// devsupSection closes one section of a remote read: "@@zcp-devsup <name> <exit>".
const devsupSection = "@@zcp-devsup "

// NoSupervisorError says hostname's dev setup does not run zcp-devsup:
// Installed is false when the binary is absent, true when it is installed but
// nothing answers its control socket (run.start is something else).
type NoSupervisorError struct {
	Hostname  string
	Installed bool
}

func (e *NoSupervisorError) Error() string {
	if e.Installed {
		return fmt.Sprintf("no zcp-devsup runs on %s (its dev setup's run.start is not `zcp-devsup run`)", e.Hostname)
	}
	return fmt.Sprintf("zcp-devsup is not installed on %s", e.Hostname)
}

// SupervisorUnresponsiveError says hostname's supervisor runs but did not
// answer its control socket in time, or (SSH) the container itself did not
// answer over SSH in time — a container out of memory or CPU, never a dev
// setup to move onto the supervisor.
type SupervisorUnresponsiveError struct {
	Hostname string
	SSH      bool
}

func (e *SupervisorUnresponsiveError) Error() string {
	if e.SSH {
		return fmt.Sprintf("%s did not answer over SSH in time — the container may be out of memory or CPU. Check its load and memory (zerops_logs, zerops_scale) or restart it (zerops_manage action=restart), then retry.", e.Hostname)
	}
	return fmt.Sprintf("The dev-server supervisor on %s is running but did not answer in time — the container may be out of memory or CPU. Check its load and memory (zerops_logs, zerops_scale), then retry in a moment.", e.Hostname)
}

// SupervisorCtlError is a ctl answer other than done or "no supervisor".
type SupervisorCtlError struct {
	Code    int
	Message string
}

func (e *SupervisorCtlError) Error() string {
	return fmt.Sprintf("zcp-devsup ctl exited %d: %s", e.Code, e.Message)
}

// DevServers is one read of a dev service's dev servers: the declaration in its
// repo and the supervisor's state.
type DevServers struct {
	Hostname string
	// Service is the hostname the supervisor runs the declared entries of —
	// those naming it and those naming none (devsup.Declaration.ForService):
	// the pool for a lease, whose container's hostname env is the pool's;
	// empty means Hostname.
	Service string
	// Declared is the parsed declaration; empty when the file is absent.
	Declared devsup.Declaration
	// DeclarationExists is false when the repo has no declaration file.
	DeclarationExists bool
	// DeclarationErr is set when the file exists but cannot be parsed or
	// breaks a rule; zcp never overwrites such a file.
	DeclarationErr error
	// DeclaredVersion is the file's format version as written, even when zcp
	// cannot parse the rest.
	DeclaredVersion int
	// Supervisor is the version and formats `ctl version` reported; empty when
	// the binary is absent.
	Supervisor SupervisorVersion
	// State is the supervisor's answer to `ctl status`.
	State devsup.State
	// ReadAt is when zcp read it.
	ReadAt time.Time
	// LoopbackOnly holds the TCP ports the container listens on only at a
	// loopback address (`ss -ltn`); a port absent here is public or unread.
	LoopbackOnly map[int]bool
}

// SupervisorVersion is `zcp-devsup ctl version`.
type SupervisorVersion struct {
	Version string `json:"version"`
	Formats []int  `json:"formats"`
	// Capabilities names the ctl verbs and flags this supervisor has that an
	// older one lacks (devsup.Capabilities); absent from an older answer.
	Capabilities []string `json:"capabilities,omitempty"`
}

// Supports reports whether the supervisor lists capability: an older ctl
// fails on a verb or flag it does not know, so zcp sends one only then.
func (v SupervisorVersion) Supports(capability string) bool {
	return slices.Contains(v.Capabilities, capability)
}

// readDevServersCmd reads, in one round trip, the supervisor's version and
// state and the repo's declaration, each section closed by its exit code.
func readDevServersCmd(root string) string {
	file := devDeclarationFile(root)
	return `if command -v zcp-devsup >/dev/null 2>&1; then ` +
		`zcp-devsup ctl version --json 2>/dev/null; c=$?; echo; echo "@@zcp-devsup version $c"; ` +
		`zcp-devsup ctl status --json 2>/dev/null; c=$?; echo; echo "@@zcp-devsup status $c"; ` +
		`else echo "@@zcp-devsup version 127"; fi; ` +
		`ss -ltn 2>/dev/null; echo "@@zcp-devsup listen 0"; ` +
		`if [ -f '` + file + `' ]; then cat '` + file + `'; echo; echo "@@zcp-devsup declaration 0"; ` +
		`else echo "@@zcp-devsup declaration 1"; fi`
}

// ReadDevServers reads hostname's dev servers. A dev service whose dev setup
// does not run the supervisor answers *NoSupervisorError, one whose
// supervisor does not answer in time *SupervisorUnresponsiveError; its
// declaration is read all the same.
func ReadDevServers(ctx context.Context, ssh SSHDeployer, hostname, root string) (*DevServers, error) {
	readCtx, cancel := context.WithTimeout(ctx, devsupCtlTimeout)
	defer cancel()
	out, err := ssh.ExecSSH(readCtx, hostname, readDevServersCmd(root))
	if sshTimedOut(ctx, err) {
		return nil, &SupervisorUnresponsiveError{Hostname: hostname, SSH: true}
	}
	if err != nil {
		return nil, fmt.Errorf("read dev servers on %s: %w", hostname, err)
	}
	sections, err := splitDevsupSections(string(out))
	if err == nil {
		if _, ok := sections["declaration"]; !ok {
			err = fmt.Errorf("unreadable answer %q", strings.TrimSpace(string(out)))
		}
	}
	if err != nil {
		return nil, fmt.Errorf("read dev servers on %s: %w", hostname, err)
	}
	ds := &DevServers{Hostname: hostname, ReadAt: time.Now().UTC()}
	if decl, ok := sections["declaration"]; ok && decl.code == 0 {
		ds.DeclarationExists = true
		ds.DeclaredVersion = declarationVersion(decl.body)
		ds.Declared, ds.DeclarationErr = devsup.ParseDeclaration([]byte(decl.body))
	}
	if !ds.DeclarationExists {
		ds.Declared = devsup.Declaration{Version: devsupFormat, Servers: []devsup.Server{}}
	}
	ds.LoopbackOnly = loopbackOnlyPorts(sections["listen"].body)
	version := sections["version"]
	if version.code == devsupCodeAbsent {
		return ds, &NoSupervisorError{Hostname: hostname}
	}
	if version.code == devsupCodeOK {
		_ = json.Unmarshal([]byte(version.body), &ds.Supervisor)
	}
	status := sections["status"]
	switch status.code {
	case devsupCodeOK:
		if err := json.Unmarshal([]byte(status.body), &ds.State); err != nil {
			return nil, fmt.Errorf("read dev servers on %s: supervisor state: %w", hostname, err)
		}
		return ds, nil
	case devsupCodeNoSupervisor:
		return ds, &NoSupervisorError{Hostname: hostname, Installed: true}
	case devsupCodeUnresponsive:
		return ds, &SupervisorUnresponsiveError{Hostname: hostname}
	default:
		return nil, ctlFailure(status)
	}
}

// devServersSettlePoll is how often ReadDevServersSettled reads again.
const devServersSettlePoll = time.Second

// ReadDevServersSettled is ReadDevServers once each of the host's declared
// servers that was starting has answered running or crashed, or the start
// budget (maxDevServerWait) has passed: a server that crashes a minute into
// its start is named crashed, not starting. A failed read ends the wait.
func ReadDevServersSettled(ctx context.Context, ssh SSHDeployer, hostname, root string) (*DevServers, error) {
	return readDevServersSettled(ctx, ssh, hostname, root, time.Duration(maxDevServerWait)*time.Second, devServersSettlePoll)
}

func readDevServersSettled(ctx context.Context, ssh SSHDeployer, hostname, root string, budget, every time.Duration) (*DevServers, error) {
	deadline := time.Now().Add(budget)
	for {
		ds, err := ReadDevServers(ctx, ssh, hostname, root)
		if err != nil || !ds.starting() || time.Now().Add(every).After(deadline) {
			return ds, err
		}
		select {
		case <-ctx.Done():
			return ds, nil
		case <-time.After(every):
		}
	}
}

// starting reports whether one of the host's declared servers is starting.
func (ds *DevServers) starting() bool {
	for _, s := range ds.Servers() {
		if ds.State.Servers[s.Name].Status == devsup.StatusStarting {
			return true
		}
	}
	return false
}

// RetryPendingNote is how a reply says a server crashed while starting and
// the supervisor's one more start is still to come (ServerState.RetryPending).
const RetryPendingNote = "the supervisor starts it once more after its siblings settle"

// OutOfMemoryHint follows the restart reason of a server systemd restarted,
// with every other one, because the service ran out of memory
// (ServerState.OutOfMemory).
const OutOfMemoryHint = "If it runs out of memory again, raise the service's max RAM with zerops_scale or stop a dev server you are not working on."

// TaskLimitNote is how a reply says a server crashed, or its start was
// refused, because the service's task limit was reached
// (ServerState.TaskLimit): the limit, the tasks running now, and whether the
// supervisor starts it again (pending) or has stopped trying.
func TaskLimitNote(t devsup.TaskLimit, pending bool) string {
	note := fmt.Sprintf("the service's task limit (%d) was reached, %d tasks run now", t.Max, t.Current)
	if pending {
		return note + "; the supervisor starts it again after a backoff, one server at a time"
	}
	return note + "; the supervisor stopped starting it. Free tasks — stop a server you do not need, or cap the threads its tools start (RAYON_NUM_THREADS, UV_CONCURRENT_BUILDS, UV_CONCURRENT_INSTALLS) — then start it again"
}

// RecheckSentence tells the agent when to read names on hostname again: they
// are still starting or wait for their one more start. name, when set, is
// the one server the status call reads.
func RecheckSentence(hostname, name string, names []string) string {
	call := fmt.Sprintf("zerops_dev_server action=status hostname=%q", hostname)
	if name != "" {
		call += fmt.Sprintf(" name=%q", name)
	}
	return fmt.Sprintf("Check %s again in about 30 s with %s.", strings.Join(names, ", "), call)
}

// observeListeners re-reads which ports listen on a loopback address only: a
// server this call started listens only now. An unreadable answer keeps the
// last read.
func (ds *DevServers) observeListeners(ctx context.Context, ssh SSHDeployer) {
	if out, err := ssh.ExecSSH(ctx, ds.Hostname, "ss -ltn 2>/dev/null"); err == nil {
		ds.LoopbackOnly = loopbackOnlyPorts(string(out))
	}
}

// loopbackOnlyPorts reads `ss -ltn` output: the ports whose every listening
// socket is bound to a loopback address.
func loopbackOnlyPorts(out string) map[int]bool {
	loopback := map[int]bool{}
	public := map[int]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != "LISTEN" {
			continue
		}
		host, portText, ok := cutLastColon(f[3])
		port, err := strconv.Atoi(portText)
		if !ok || err != nil {
			continue
		}
		host = strings.Trim(host, "[]")
		if strings.HasPrefix(host, "127.") || host == "::1" {
			loopback[port] = true
		} else {
			public[port] = true
		}
	}
	for port := range public {
		delete(loopback, port)
	}
	return loopback
}

func cutLastColon(addr string) (host, port string, ok bool) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return "", "", false
	}
	return addr[:i], addr[i+1:], true
}

// devsupFormat is the declaration format zcp writes.
var devsupFormat = devsup.Formats[len(devsup.Formats)-1]

// declarationVersion reads a declaration's format version without the rest; 0
// when unreadable.
func declarationVersion(body string) int {
	var v struct {
		Version int `json:"version"`
	}
	if json.Unmarshal([]byte(body), &v) != nil {
		return 0
	}
	return v.Version
}

// OlderThanDeclaration reports a supervisor that cannot run the declaration's
// format, naming both — "" when it can (or either is unknown).
func (ds *DevServers) OlderThanDeclaration() string {
	if ds.DeclaredVersion == 0 || len(ds.Supervisor.Formats) == 0 || slices.Contains(ds.Supervisor.Formats, ds.DeclaredVersion) {
		return ""
	}
	return fmt.Sprintf("the supervisor on %s (zcp-devsup %s, declaration formats %v) is older than %s (format %d) — redeploy %s so its prepareCommands install a newer zcp-devsup",
		ds.Hostname, ds.Supervisor.Version, ds.Supervisor.Formats, devsup.DeclarationPath, ds.DeclaredVersion, ds.Hostname)
}

// Servers are the declared entries this dev service's supervisor runs: in
// variant B the code volume's declaration also holds the other dev services',
// and a lease's repository every part's.
func (ds *DevServers) Servers() []devsup.Server {
	return ds.Declared.ForService(cmp.Or(ds.Service, ds.Hostname))
}

// Server returns the declared entry named name.
func (ds *DevServers) Server(name string) (devsup.Server, bool) {
	for _, s := range ds.Declared.Servers {
		if s.Name == name {
			return s, true
		}
	}
	return devsup.Server{}, false
}

// DevPorts are this dev service's declared servers with a port, their status
// and health path, when more than one is declared, or one listens only on
// loopback — verify then checks each (PublicAccessInput.DevPorts).
func (ds *DevServers) DevPorts() []DevPort {
	var out []DevPort
	for _, s := range ds.Servers() {
		if s.Port != 0 {
			out = append(out, DevPort{Name: s.Name, Port: s.Port, Status: ds.State.Servers[s.Name].Status, HealthPath: s.HealthPath, LoopbackOnly: ds.LoopbackOnly[s.Port]})
		}
	}
	if len(out) < 2 && !slices.ContainsFunc(out, func(d DevPort) bool { return d.LoopbackOnly }) {
		return nil
	}
	return out
}

// Listening reports whether the supervisor runs a declared server with a port
// — "is something listening" for public access (docs/spec-mate.md §2.8a.2:
// status is derived once, by the supervisor).
func (ds *DevServers) Listening() bool {
	for _, s := range ds.Servers() {
		if s.Port == 0 {
			continue
		}
		if st, ok := ds.State.Servers[s.Name]; ok && (st.Status == devsup.StatusRunning || st.Status == devsup.StatusStarting) {
			return true
		}
	}
	return false
}

type devsupAnswer struct {
	code int
	body string
}

// splitDevsupSections cuts a remote read into its sections.
func splitDevsupSections(out string) (map[string]devsupAnswer, error) {
	sections := map[string]devsupAnswer{}
	var body strings.Builder
	for line := range strings.SplitSeq(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), devsupSection)
		if !ok {
			body.WriteString(line)
			body.WriteByte('\n')
			continue
		}
		name, codeText, _ := strings.Cut(rest, " ")
		code, err := strconv.Atoi(strings.TrimSpace(codeText))
		if err != nil {
			return nil, fmt.Errorf("unreadable answer %q", line)
		}
		sections[name] = devsupAnswer{code: code, body: strings.TrimSpace(body.String())}
		body.Reset()
	}
	if len(sections) == 0 {
		return nil, fmt.Errorf("unreadable answer %q", strings.TrimSpace(out))
	}
	return sections, nil
}

// ctlFailure turns a failed ctl answer ({"code","error"} with --json) into an error.
func ctlFailure(a devsupAnswer) error {
	var failure struct {
		Error string `json:"error"`
	}
	msg := strings.TrimSpace(a.body)
	if json.Unmarshal([]byte(a.body), &failure) == nil && failure.Error != "" {
		msg = failure.Error
	}
	return &SupervisorCtlError{Code: a.code, Message: msg}
}

// sshTimedOut says an SSH call failed at its own deadline while the caller's
// ctx still runs: the container did not answer in time.
func sshTimedOut(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil
}

// supervisorCtl runs one `zcp-devsup ctl <args> --json` on hostname and returns
// the state it answers with.
func supervisorCtl(ctx context.Context, ssh SSHDeployer, hostname string, args ...string) (devsup.State, error) {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	cmd := fmt.Sprintf(`if command -v zcp-devsup >/dev/null 2>&1; then zcp-devsup ctl %s --json 2>/dev/null; c=$?; echo; echo "@@zcp-devsup ctl $c"; else echo "@@zcp-devsup ctl 127"; fi`,
		strings.Join(quoted, " "))
	ctlCtx, cancel := context.WithTimeout(ctx, devsupCtlTimeout)
	defer cancel()
	out, err := ssh.ExecSSH(ctlCtx, hostname, cmd)
	if sshTimedOut(ctx, err) {
		return devsup.State{}, &SupervisorUnresponsiveError{Hostname: hostname, SSH: true}
	}
	if err != nil {
		return devsup.State{}, fmt.Errorf("zcp-devsup ctl %s on %s: %w", strings.Join(args, " "), hostname, err)
	}
	sections, err := splitDevsupSections(string(out))
	if err != nil {
		return devsup.State{}, fmt.Errorf("zcp-devsup ctl on %s: %w", hostname, err)
	}
	answer := sections["ctl"]
	switch answer.code {
	case devsupCodeOK:
	case devsupCodeAbsent:
		return devsup.State{}, &NoSupervisorError{Hostname: hostname}
	case devsupCodeNoSupervisor:
		return devsup.State{}, &NoSupervisorError{Hostname: hostname, Installed: true}
	case devsupCodeUnresponsive:
		return devsup.State{}, &SupervisorUnresponsiveError{Hostname: hostname}
	default:
		return devsup.State{}, ctlFailure(answer)
	}
	var st devsup.State
	if err := json.Unmarshal([]byte(answer.body), &st); err != nil {
		return devsup.State{}, fmt.Errorf("zcp-devsup ctl on %s: %w", hostname, err)
	}
	return st, nil
}

// supervisorLogTail is `ctl logs <name> --lines N`; "" when the server has no log yet.
// A start reads it through currentRun.
func supervisorLogTail(ctx context.Context, ssh SSHDeployer, hostname, name string, lines int) string {
	cmd := fmt.Sprintf(`zcp-devsup ctl logs %s --lines %d --json 2>/dev/null`, shellQuote(name), lines)
	tailCtx, cancel := context.WithTimeout(ctx, logTailTimeout)
	defer cancel()
	out, err := ssh.ExecSSH(tailCtx, hostname, cmd)
	if err != nil {
		return ""
	}
	var tail struct {
		Lines []string `json:"lines"`
	}
	if json.Unmarshal(out, &tail) != nil || len(tail.Lines) == 0 {
		return ""
	}
	return strings.Join(tail.Lines, "\n") + "\n"
}

// startMarker opens each run of a server in its log ("--- zcp-devsup: start
// #N at …" or "… start #N failed: …").
const startMarker = "--- zcp-devsup: start #"

// currentRun cuts a log tail to the server's current run: from the last start
// marker on, so an earlier run's failure never reads as this one's.
func currentRun(tail string) string {
	if i := strings.LastIndex(tail, startMarker); i == 0 || i > 0 && tail[i-1] == '\n' {
		return tail[i:]
	}
	return tail
}

// WriteDevDeclaration replaces hostname's declaration with d (atomic rename).
// Only a change of the set of servers writes it. A d the supervisor would
// refuse is refused here, coded, before anything is written.
func WriteDevDeclaration(ctx context.Context, ssh SSHDeployer, hostname, root string, d devsup.Declaration) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", devsup.DeclarationPath, err)
	}
	file := devDeclarationFile(root)
	if _, err := devsup.ParseDeclaration(b); err != nil {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("cannot declare the dev server on %s: %v", hostname, err),
			"A declared server keeps its name and port even while stopped. Start this one under another name or port, or remove the server that holds it from "+file+" over SSH and run `zcp-devsup ctl reload` there.")
	}
	tmp := file + ".tmp"
	cmd := fmt.Sprintf(`mkdir -p %s && printf '%%s\n' %s > %s && mv -f %s %s`,
		shellQuote(devRepoRoot(root)+"/.zcp"), shellQuote(string(b)), shellQuote(tmp), shellQuote(tmp), shellQuote(file))
	writeCtx, cancel := context.WithTimeout(ctx, containerIdentityTimeout)
	defer cancel()
	if _, err := ssh.ExecSSH(writeCtx, hostname, cmd); err != nil {
		return fmt.Errorf("write %s on %s: %w", devsup.DeclarationPath, hostname, err)
	}
	return nil
}

// DeclareDevServer returns d with s declared: unchanged when an entry of the
// same name runs the same thing for the same service and health path, that
// entry replaced otherwise, s appended when none has its name. changed says
// whether the file needs writing. The repository's own top-level keys —
// previewPortBase, siblingEnv, prepare — are kept as they are.
func DeclareDevServer(d devsup.Declaration, s devsup.Server) (devsup.Declaration, bool) {
	out := devsup.Declaration{Version: d.Version, PreviewPortBase: d.PreviewPortBase, SiblingEnv: maps.Clone(d.SiblingEnv),
		SiblingPreviewEnv: maps.Clone(d.SiblingPreviewEnv), Prepare: d.Prepare, Servers: slices.Clone(d.Servers)}
	if out.Version == 0 {
		out.Version = devsupFormat
	}
	for i, have := range out.Servers {
		if have.Name != s.Name {
			continue
		}
		if have.SameRun(s) && have.Service == s.Service && have.HealthPath == s.HealthPath {
			return d, false
		}
		out.Servers[i] = s
		return out, true
	}
	out.Servers = append(out.Servers, s)
	return out, true
}
