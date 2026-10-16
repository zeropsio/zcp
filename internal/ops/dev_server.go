package ops

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
)

// DevServerResult is the standardized response from dev_server actions.
// All actions return the same shape so the MCP schema stays single.
type DevServerResult struct {
	Action   string `json:"action"`
	Hostname string `json:"hostname"`
	// Running is the supervisor's status of the reported server: exactly
	// status == "running" (docs/spec-mate.md §2.8a.3, "One source"), set
	// once from the call's last read (settle). The health probe never
	// decides it.
	Running bool `json:"running"`
	// Port is the health-check port the tool probed or would probe. Zero
	// when the action does not involve a port (stop, logs).
	Port int `json:"port,omitempty"`
	// HealthPath is the HTTP path that was probed (start, restart). Empty
	// when no probe was run.
	HealthPath string `json:"healthPath,omitempty"`
	// HealthStatus is the HTTP status code returned by the health probe,
	// or 0 when no probe was run or nothing answered HTTP.
	HealthStatus int `json:"healthStatus,omitempty"`
	// HealthOK is the health fact of this call's probe: the server answered
	// HTTP 2xx/3xx on healthPath. The envelope entry's verify carries it.
	HealthOK bool `json:"-"`
	// StartMillis is the time between asking the supervisor to start the
	// server and the health probe returning 2xx (start, restart). Zero on failure.
	StartMillis int64 `json:"startMillis,omitempty"`
	// LogTail is the last N lines of the server's own log, read through the
	// supervisor on a failed start, a crash and a log read.
	LogTail string `json:"logTail,omitempty"`
	// LogFile is the server's own log on the dev container
	// ($HOME/.zcp/devsup/logs/<name>.log), so the agent can tail it further.
	LogFile string `json:"logFile,omitempty"`
	// PID is the leader of the server's process group as the supervisor
	// reports it; zero when it does not run.
	PID int `json:"pid,omitempty"`
	// URL is the consumer-vantage address of the probed dev server —
	// http://<hostname>:<port><healthPath>, reachable from the agent's
	// container over the project-private network when the server binds
	// 0.0.0.0. Populated on a passing probe (start/restart/status). Empty
	// otherwise. The health probe itself runs localhost INSIDE the target
	// container, so the message states that as the checked fact and hands this
	// URL as the reachable pointer — it does NOT claim the hostname URL itself
	// responded (a loopback-only bind passes the probe but refuses hostname
	// traffic; B9).
	URL string `json:"url,omitempty"`
	// Message is a one-line human summary of what happened.
	Message string `json:"message"`
	// Reason classifies why the server does not run or why its health probe
	// failed — the agent can dispatch on it. Known values:
	//   crashed                   — the supervisor reports the server ended
	//                               on its own; it stays down until started
	//   port_in_use               — the supervisor could not start it: its
	//                               port stayed busy
	//   stopped                   — the server is stopped (status)
	//   no_supervisor             — the dev setup does not run zcp-devsup
	//   health_probe_timeout      — probe context deadline exceeded
	//   health_probe_slow         — the port accepts connections, no HTTP
	//                               answer within the wait
	//   health_probe_no_output    — probe returned nothing (malformed)
	//   health_probe_connection_refused  — curl got no response
	//   health_probe_http_<code>  — server returned non-ready status
	//   health_probe_unknown: <…> — unclassified; includes raw probe line
	//   connection_refused        — status-action: no HTTP response
	//   slow                      — status-action: accepts, no HTTP answer
	//                               within 2s
	//   http_<code>               — status-action: a code other than 2xx/3xx
	Reason string `json:"reason,omitempty"`
	// Name is the dev server's name in the declaration — set only when it
	// differs from Hostname (docs/spec-mate.md §2.8a.3): a single-part app's
	// one server is named after its dev service.
	Name string `json:"name,omitempty"`
	// Read is the call's read of the dev service's dev servers, its state the
	// supervisor's last answer in the call — what the envelope's devServers[]
	// projects (DevServerStates). Nil when no supervisor answered.
	Read *DevServers `json:"-"`
	// Restarts is how often the supervisor restarted the reported server
	// (status).
	Restarts int `json:"restarts,omitempty"`
	// LastRestartReason is the supervisor's word on why it last restarted
	// the reported server, or stopped restarting it on watched changes
	// (status).
	LastRestartReason string `json:"lastRestartReason,omitempty"`
	// Unreported: the call read several servers and reports none of them
	// alone (a status of a dev service with several), so it says nothing of
	// whether "the" server runs.
	Unreported bool `json:"-"`
}

// DevServerParams is the typed input of ExecuteDevServer. Fields not relevant
// to a given action are ignored (e.g. Command is unused by stop and logs).
type DevServerParams struct {
	Action   string
	Hostname string
	// Name is the dev server's name in the declaration. Empty: the server
	// named after the hostname, or the only one declared.
	Name        string
	Command     string
	Port        int
	HealthPath  string
	WaitSeconds int
	LogLines    int
	WorkDir     string
	// Root is the repository root the dev service's supervisor runs on, ""
	// for /var/www; a variant-B dev service's is its code volume's mount.
	Root string
	// NoHTTPProbe skips the post-start health probe. Set true for worker
	// services that have no HTTP port (NATS/Kafka consumers, disk-queue
	// runners, cron-style processes). With NoHTTPProbe=true, Port becomes
	// optional and the tool decides Running from the supervisor's status
	// after a short settle. Callers should follow up with zerops_logs to
	// confirm the worker is actually consuming.
	NoHTTPProbe bool
}

// DevServer action names — kept as constants so callers and tests
// reference the same identifiers.
const (
	devServerActionStart   = "start"
	devServerActionStop    = "stop"
	devServerActionStatus  = "status"
	devServerActionLogs    = "logs"
	devServerActionRestart = "restart"
)

// DevServerResult.Reason values.
const (
	reasonCrashed      = "crashed"
	reasonPortInUse    = "port_in_use"
	reasonStopped      = "stopped"
	reasonNoSupervisor = "no_supervisor"
)

// ReasonHealthProbeTimeout is the start/restart Reason when the health probe
// got no ready answer within the wait — the process may still be starting (a
// dev server compiling on its first request), unlike a refused connection.
const ReasonHealthProbeTimeout = "health_probe_timeout"

// reasonHealthProbeSlow (start/restart) and reasonSlow (status): the port
// accepts connections but no HTTP answer came within the wait — a dev server
// still compiling its first response, not a refused connection.
const (
	reasonHealthProbeSlow = "health_probe_slow"
	reasonSlow            = "slow"
)

const (
	// defaultDevServerWait is how long the start probe waits for a
	// successful health check before giving up. 15s is enough for a
	// typical Node/TS hot-reload boot on Zerops.
	defaultDevServerWait = 15
	// maxDevServerWait is an upper bound that keeps pathological boot
	// loops from eating the whole bash-timeout window.
	maxDevServerWait = 45
	// defaultLogTailLines is how many trailing log lines to include in
	// every result. Small enough to fit in an MCP response, large
	// enough to diagnose a failed startup.
	defaultLogTailLines = 40
	// startedLogTailLines is the tail of a server that started: enough to
	// show it came up, nothing to diagnose.
	startedLogTailLines = 8
	// pollIntervalMillis is the poll cadence of the start-probe loop.
	pollIntervalMillis = 500
	// probeTimeoutSlack is added on top of waitSeconds to bound the
	// foreground ssh call. The remote probe has its own `seq 1..N` loop
	// so slack only covers ssh dial + channel tear-down jitter.
	probeTimeoutSlack = 5 * time.Second
	// logTailTimeout bounds the log-tail ssh call on the failure path.
	logTailTimeout = 5 * time.Second
	// containerIdentityTimeout bounds a one-round-trip remote write or read.
	containerIdentityTimeout = 10 * time.Second
)

var (
	// devHostnameRe bounds the dev-server hostname to the same shape as
	// Zerops container hostnames. Prevents shell injection through
	// a crafted hostname that would end up inside `ssh HOST ...`.
	devHostnameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	// devServerNameRe is the declaration's name rule (docs/spec-mate.md §2.8a.1).
	devServerNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	// devWorkDirRe bounds workDir to an absolute POSIX path with no shell
	// metacharacters.
	devWorkDirRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,256}$`)
	// devHealthPathRe bounds the health-probe path to a URL path with no
	// shell metacharacters. healthPath is interpolated into the remote
	// curl probe (http://localhost:PORT<healthPath>); a single quote or `;`
	// would otherwise break or inject into that command (B3).
	devHealthPathRe = regexp.MustCompile(`^/[A-Za-z0-9._~/?=&%-]*$`)
)

// ExecuteDevServer is the single entry point for all dev_server actions. Every
// action reads the dev service's dev servers once and acts through its
// supervisor (docs/spec-mate.md §2.8a.2). A start or restart on a dev service
// whose dev setup does not run the supervisor returns *NoSupervisorError with
// nothing changed; the caller moves the dev setup onto it.
func ExecuteDevServer(
	ctx context.Context,
	ssh SSHDeployer,
	client platform.Client,
	projectID string,
	p DevServerParams,
) (*DevServerResult, error) {
	if err := validateDevServerParams(p); err != nil {
		return nil, err
	}
	if err := verifyDevServerTarget(ctx, client, projectID, p.Hostname); err != nil {
		return nil, err
	}
	action := strings.ToLower(p.Action)
	ds, err := ReadDevServers(ctx, ssh, p.Hostname, p.Root)
	var noSup *NoSupervisorError
	switch {
	case errors.As(err, &noSup) && (action == devServerActionStart || action == devServerActionRestart):
		return nil, err
	case errors.As(err, &noSup):
		return noSupervisorResult(action, p.Hostname), nil
	case err != nil:
		return nil, err
	}
	var result *DevServerResult
	switch action {
	case devServerActionStart, devServerActionRestart:
		result, err = startDevServer(ctx, ssh, client, projectID, p, ds)
	case devServerActionStop:
		result, err = stopDevServer(ctx, ssh, p, ds)
	case devServerActionStatus:
		result, err = statusDevServer(ctx, ssh, p, ds)
	default:
		result, err = logsDevServer(ctx, ssh, p, ds)
	}
	if err != nil {
		return nil, err
	}
	result.settle(ds)
	return result, nil
}

// settle ends a result on the call's read of the dev servers: running is the
// supervisor's last status of the reported server — the one derivation of
// running (docs/spec-mate.md §2.8a.3).
func (r *DevServerResult) settle(ds *DevServers) {
	r.Read = ds
	name := r.Name
	if name == "" {
		name = r.Hostname
	}
	r.Running = ds.State.Servers[name].Status == devsup.StatusRunning
}

// healthyHTTP is the health probe's pass: a 2xx or 3xx answer (the start
// probe's shell loop checks the same range).
func healthyHTTP(code int) bool {
	return code >= 200 && code < 400
}

func validateDevServerParams(p DevServerParams) error {
	switch strings.ToLower(p.Action) {
	case devServerActionStart, devServerActionStop, devServerActionStatus, devServerActionLogs, devServerActionRestart:
	default:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown dev_server action %q", p.Action),
			"Use one of: start, stop, status, logs, restart")
	}
	if !devHostnameRe.MatchString(p.Hostname) {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid hostname %q", p.Hostname),
			"Hostname must match Zerops container conventions: lowercase alphanumeric with dashes, starting with a letter, up to 63 chars.")
	}
	if p.Name != "" && !devServerNameRe.MatchString(p.Name) {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid dev server name %q", p.Name),
			"A dev server name is lowercase letters, digits and dashes, starting with a letter, up to 40 chars (e.g. web, api, worker).")
	}
	if p.WorkDir != "" {
		if _, err := devServerCwd(p.Root, p.WorkDir); err != nil {
			return err
		}
	}
	if p.HealthPath != "" && !devHealthPathRe.MatchString(p.HealthPath) {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid health path %q", p.HealthPath),
			"healthPath must be a URL path starting with / and containing only URL-safe characters (e.g. /health, /api/status).")
	}
	if p.Port < 0 || p.Port > 65535 {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid port %d", p.Port),
			"Pass the HTTP port the dev server listens on (e.g. 3000 for NestJS, 5173 for Vite). For services with no HTTP surface (NATS/queue workers, cron runners), set noHttpProbe=true and omit it.")
	}
	return nil
}

// devServerCwd turns an absolute workDir into the declaration's cwd, relative
// to the repo root.
func devServerCwd(root, workDir string) (string, error) {
	if workDir == "" {
		return ".", nil
	}
	repo := devRepoRoot(root)
	clean := path.Clean(workDir)
	rel, ok := strings.CutPrefix(clean, repo)
	if !devWorkDirRe.MatchString(workDir) || !ok || (rel != "" && !strings.HasPrefix(rel, "/")) {
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid working directory %q", workDir),
			"workDir must be an absolute path inside the repo at "+repo+" (e.g. "+repo+"/apps/web).")
	}
	if rel = strings.TrimPrefix(rel, "/"); rel == "" {
		return ".", nil
	}
	return rel, nil
}

// verifyDevServerTarget confirms the hostname resolves to an actual service
// in the current project AND that the service is in a state where a dev
// server can attach — SSH needs a live container, so a FAILED or
// failed-redeploy READY_TO_DEPLOY target gets a clear PRECONDITION error
// ("deploy it to RUNNING first") instead of an opaque SSH timeout.
//
// This is a precondition, NOT the deleted deploy refusal. Starting a dev
// server cannot itself fix a non-running container (unlike a corrective
// redeploy — which is why zerops_deploy no longer gates), so there is no
// deadlock: once the deploy lands the service is RUNNING and the dev server
// attaches.
func verifyDevServerTarget(ctx context.Context, client platform.Client, projectID, hostname string) error {
	if client == nil {
		// Unit tests can pass a nil client when only the SSH shape matters.
		return nil
	}
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return err
	}
	target, lookupErr := FindService(services, hostname)
	if lookupErr != nil {
		return platform.NewPlatformError(platform.ErrServiceNotFound,
			fmt.Sprintf("No service with hostname %q in this project", hostname),
			"Pass the hostname of a dev container that exists in the current project (e.g. apidev, appdev, workerdev).")
	}
	switch target.Status {
	case platform.ServiceStatusFailed:
		return devServerNotRunningErr(hostname, target.Status)
	case platform.ServiceStatusReadyToDeploy:
		// Same trigger the old gate used (failed history only) — a
		// never-deployed READY_TO_DEPLOY is left to the SSH layer, unchanged.
		failed, ctxErr := LatestFailedAppVersionContext(ctx, client, nil, projectID, hostname)
		if ctxErr != nil {
			return ctxErr
		}
		if failed != nil {
			return devServerNotRunningErr(hostname, target.Status)
		}
	}
	return nil
}

// devServerNotRunningErr is the precondition error when a dev server is
// asked to attach to a container that has no live process to SSH into.
func devServerNotRunningErr(hostname, status string) error {
	return platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("dev server target %q is in %s — a dev server needs a RUNNING container to attach to", hostname, status),
		"Deploy the service (zerops_deploy) to bring it RUNNING, then start the dev server. If a previous deploy failed, read zerops_events to see why before redeploying.",
	)
}

// noSupervisorResult answers stop, status and logs on a dev service whose dev
// setup does not run the supervisor: no dev server runs under zcp there.
func noSupervisorResult(action, hostname string) *DevServerResult {
	return &DevServerResult{
		Action:   action,
		Hostname: hostname,
		Reason:   reasonNoSupervisor,
		Message: fmt.Sprintf("%s has no dev-server supervisor yet, so no dev server runs under zcp there. zerops_dev_server action=start sets it up (one redeploy of %s; uncommitted work stays).",
			hostname, hostname),
	}
}

// resolveServerName picks the declared server a call names: the given name,
// else the one named after the hostname, else the only one declared. A start
// of a repo that declares several, none named after the hostname, defaults to
// the hostname too (a new single server).
func resolveServerName(p DevServerParams, ds *DevServers, starting bool) (string, error) {
	if p.Name != "" {
		return p.Name, nil
	}
	if _, ok := ds.Server(p.Hostname); ok {
		return p.Hostname, nil
	}
	servers := ds.Servers()
	if len(servers) == 1 {
		return servers[0].Name, nil
	}
	if starting || len(servers) == 0 {
		return p.Hostname, nil
	}
	return "", platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("%s declares several dev servers; name the one you mean", p.Hostname),
		"Pass name: one of "+declaredSummary(ds)+".")
}

// declaredSummary lists the declared servers with the supervisor's status.
func declaredSummary(ds *DevServers) string {
	servers := ds.Servers()
	parts := make([]string, 0, len(servers))
	for _, s := range servers {
		status := "not started"
		if st, ok := ds.State.Servers[s.Name]; ok {
			status = string(st.Status)
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", s.Name, status))
	}
	return strings.Join(parts, ", ")
}

// unknownServerErr answers a name the repo does not declare.
func unknownServerErr(ds *DevServers, name string) error {
	hint := "Start it with zerops_dev_server action=start name=" + name + " command=<the command that runs it>."
	if len(ds.Servers()) > 0 {
		hint = "Declared: " + declaredSummary(ds) + ". " + hint
	}
	return platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("%s declares no dev server named %q", ds.Hostname, name), hint)
}

// newResult starts a result for name on hostname.
func newResult(action, hostname, name string) *DevServerResult {
	r := &DevServerResult{Action: action, Hostname: hostname}
	if name != hostname {
		r.Name = name
	}
	return r
}

// serverLabel is how messages name a server: by its dev service when it is
// named after it, "<name> on <hostname>" otherwise.
func serverLabel(hostname, name string) string {
	if name == hostname || name == "" {
		return hostname
	}
	return name + " on " + hostname
}

// declarationProblem refuses to act on a declaration zcp cannot read or a
// supervisor that cannot run it.
func declarationProblem(ds *DevServers) error {
	// A declaration newer than the supervisor is unreadable to it (and to an
	// equally old zcp); name the old supervisor, not a broken file.
	if msg := ds.OlderThanDeclaration(); msg != "" {
		return platform.NewPlatformError(platform.ErrInvalidParameter, msg,
			"Redeploy the dev service (zerops_deploy) so its run.prepareCommands install the zcp-devsup this zcp writes, then start the server again.")
	}
	if ds.DeclarationErr != nil {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%s on %s is invalid: %v", devsup.DeclarationPath, ds.Hostname, ds.DeclarationErr),
			"Fix the file by hand (zcp never overwrites an invalid declaration); the supervisor keeps running what it runs meanwhile.")
	}
	return nil
}

// wantedServer is the declaration entry a start asks for: the declared one,
// with what the call passes replacing its command, port, health path and cwd.
// In variant B (a code volume's declaration, root set) it names the dev
// service whose supervisor runs it.
func wantedServer(p DevServerParams, name string, declared devsup.Server, isDeclared bool) (devsup.Server, error) {
	want := declared
	want.Name = name
	if p.Root != "" && want.Service == "" {
		want.Service = p.Hostname
	}
	if p.HealthPath != "" {
		want.HealthPath = p.HealthPath
	}
	if !isDeclared && strings.TrimSpace(p.Command) == "" {
		return devsup.Server{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			"Missing dev server command",
			"Pass the exact shell command that starts the dev server, e.g. 'npm run start:dev' or 'vite --host 0.0.0.0'. zcp declares it in "+devsup.DeclarationPath+" once; later starts need only the name.")
	}
	if strings.TrimSpace(p.Command) != "" {
		want.Command = strings.TrimSpace(p.Command)
	}
	if p.WorkDir != "" || !isDeclared {
		cwd, err := devServerCwd(p.Root, p.WorkDir)
		if err != nil {
			return devsup.Server{}, err
		}
		// The declaration is the repo's: another directory (a crew copy)
		// never rewrites a declared server's cwd (orchestrator decision D).
		if isDeclared && cwd != path.Clean(declared.Cwd) {
			return devsup.Server{}, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Dev server %s is declared in %s to run in %s, not %s", name, devsup.DeclarationPath, declared.Cwd, p.WorkDir),
				"To move it, edit its cwd in "+devsup.DeclarationPath+" and restart it. A copy of the repo runs as a supervisor runtime entry beside it: zcp-devsup ctl run --name <name> --cwd <dir> --port <port> -- <command> over SSH, and zcp-devsup ctl rm <name> ends it.")
		}
		want.Cwd = cwd
	}
	switch {
	case p.Port > 0:
		want.Port = p.Port
	case p.NoHTTPProbe && !isDeclared:
		want.Port = 0
	}
	if want.Port == 0 && !p.NoHTTPProbe && (!isDeclared || declared.Port != 0) {
		return devsup.Server{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid port %d", p.Port),
			"Pass the HTTP port the dev server listens on (e.g. 3000 for NestJS, 5173 for Vite). For services with no HTTP surface (NATS/queue workers, cron runners), set noHttpProbe=true to skip the health probe entirely.")
	}
	return want, nil
}

// startDevServer declares the server when the set (or its entry) changes,
// asks the supervisor to start it and waits for its health probe — or, for a
// server without HTTP, for the supervisor's status after a short settle.
func startDevServer(ctx context.Context, ssh SSHDeployer, client platform.Client, projectID string, p DevServerParams, ds *DevServers) (*DevServerResult, error) {
	action := strings.ToLower(p.Action)
	if err := declarationProblem(ds); err != nil {
		return nil, err
	}
	name, err := resolveServerName(p, ds, true)
	if err != nil {
		return nil, err
	}
	memoryNote, err := guardStartOnMemory(ctx, ssh, client, projectID, p, name, ds)
	if err != nil {
		return nil, err
	}
	declared, isDeclared := ds.Server(name)
	want, err := wantedServer(p, name, declared, isDeclared)
	if err != nil {
		return nil, err
	}
	decl, changed := DeclareDevServer(ds.Declared, want)
	if changed {
		if err := WriteDevDeclaration(ctx, ssh, p.Hostname, p.Root, decl); err != nil {
			return nil, err
		}
		if _, err := supervisorCtl(ctx, ssh, p.Hostname, "reload"); err != nil {
			return nil, err
		}
	}
	// A reload restarts a server whose run changed and starts a new one; a
	// restart of any other is the supervisor's own.
	verb := devServerActionStart
	if action == devServerActionRestart && isDeclared && declared.SameRun(want) {
		verb = devServerActionRestart
	}
	begun := time.Now()
	st, err := supervisorCtl(ctx, ssh, p.Hostname, verb, name)
	if err != nil {
		return nil, err
	}
	ds.observe(st)
	result := newResult(action, p.Hostname, name)
	if want.Port == 0 || p.NoHTTPProbe {
		result, err = settleNoProbe(ctx, ssh, ds, result, name)
	} else {
		result, err = probeStarted(ctx, ssh, ds, result, p, name, want.Port, begun)
	}
	if err == nil && memoryNote != "" {
		result.Message += "\n" + memoryNote
	}
	return result, err
}

// settleNoProbe decides a portless start from the supervisor's status after a
// short settle: a server that ended during init is reported crashed.
func settleNoProbe(ctx context.Context, ssh SSHDeployer, ds *DevServers, result *DevServerResult, name string) (*DevServerResult, error) {
	settle := currentNoProbeSettle()
	sleepCtx(ctx, settle)
	st, err := supervisorCtl(ctx, ssh, result.Hostname, devServerActionStatus, name)
	if err != nil {
		return nil, err
	}
	ds.observe(st)
	server := st.Servers[name]
	result.LogFile, result.PID = server.LogPath, server.Pid
	label := serverLabel(result.Hostname, name)
	if server.Status == devsup.StatusCrashed {
		applyCrash(ctx, ssh, result, name, server)
		return result, nil
	}
	result.LogTail = currentRun(supervisorLogTail(ctx, ssh, result.Hostname, name, startedLogTailLines))
	result.Message = fmt.Sprintf(
		"Dev server %s started under the supervisor in no-probe mode (no HTTP surface); after %s it is %s. "+
			"This tool CANNOT verify the worker is actually consuming messages / processing work — follow up with `zerops_logs serviceHostname=%q` to confirm the subscription / job loop is making progress.",
		label, settle, server.Status, result.Hostname)
	return result, nil
}

// probeStarted runs the in-container health probe for a started server and
// then confirms with the supervisor that its group is the one answering.
func probeStarted(ctx context.Context, ssh SSHDeployer, ds *DevServers, result *DevServerResult, p DevServerParams, name string, port int, begun time.Time) (*DevServerResult, error) {
	wait := p.WaitSeconds
	if wait <= 0 {
		wait = defaultDevServerWait
	} else if wait > maxDevServerWait {
		wait = maxDevServerWait
	}
	healthPath := probedPath(p, ds, name)
	result.Port, result.HealthPath = port, healthPath
	probeOut, _, probeDeadline, probeErr := runHealthProbe(ctx, ssh, p.Hostname, port, healthPath, wait)
	st, err := supervisorCtl(ctx, ssh, p.Hostname, devServerActionStatus, name)
	if err != nil {
		return nil, err
	}
	ds.observe(st)
	server := st.Servers[name]
	result.LogFile, result.PID = server.LogPath, server.Pid
	probeLine := strings.TrimSpace(string(probeOut))
	if server.Status == devsup.StatusCrashed {
		// A passing probe with the server's group gone is someone else's
		// listener answering its port (F3).
		applyCrash(ctx, ssh, result, name, server)
		return result, nil
	}
	if probeErr == nil && strings.HasPrefix(probeLine, "OK ") {
		result.LogTail = currentRun(supervisorLogTail(ctx, ssh, p.Hostname, name, startedLogTailLines))
		applyProbeSuccess(result, probeLine, time.Since(begun).Milliseconds(), name)
		return result, nil
	}
	result.LogTail = currentRun(supervisorLogTail(ctx, ssh, p.Hostname, name, defaultLogTailLines))
	applyProbeFailure(result, probeLine, probeDeadline, name, healthPath, wait, server.Status)
	return result, nil
}

// probedPath is the HTTP path a probe of name reads: the call's, else the
// one its declaration names, else /.
func probedPath(p DevServerParams, ds *DevServers, name string) string {
	if p.HealthPath != "" {
		return p.HealthPath
	}
	if declared, _ := ds.Server(name); declared.HealthPath != "" {
		return declared.HealthPath
	}
	return "/"
}

// applyCrash reports a server the supervisor holds as crashed, with its exit
// and log tail.
func applyCrash(ctx context.Context, ssh SSHDeployer, result *DevServerResult, name string, server devsup.ServerState) {
	result.LogTail = currentRun(supervisorLogTail(ctx, ssh, result.Hostname, name, defaultLogTailLines))
	label := serverLabel(result.Hostname, name)
	if server.Exit != nil && server.Exit.Signal == devsup.ExitPortBusy {
		result.Reason = reasonPortInUse
		result.Message = fmt.Sprintf("Dev server %s could not start: its port stayed busy — something outside the supervisor listens on it. Find and stop that process, then start the server again.", label)
		return
	}
	result.Reason = reasonCrashed
	result.Message = fmt.Sprintf("Dev server %s crashed (%s); the supervisor leaves it down. Read logTail, fix the cause, then start it again.", label, exitText(server.Exit))
}

// exitText is a server's last exit as messages say it.
func exitText(e *devsup.Exit) string {
	switch {
	case e == nil:
		return "no exit recorded"
	case e.Signal != "":
		return "signal " + e.Signal
	default:
		return "exit " + strconv.Itoa(e.Code)
	}
}

// stopDevServer asks the supervisor to stop the server's process group; it
// stays stopped until started (a redeploy starts every declared server).
func stopDevServer(ctx context.Context, ssh SSHDeployer, p DevServerParams, ds *DevServers) (*DevServerResult, error) {
	name, err := resolveServerName(p, ds, false)
	if err != nil {
		return nil, err
	}
	if _, ok := ds.State.Servers[name]; !ok {
		return nil, unknownServerErr(ds, name)
	}
	st, err := supervisorCtl(ctx, ssh, p.Hostname, devServerActionStop, name)
	if err != nil {
		return nil, err
	}
	ds.observe(st)
	result := newResult(devServerActionStop, p.Hostname, name)
	server := st.Servers[name]
	result.LogFile = server.LogPath
	result.Message = fmt.Sprintf("Dev server %s stopped — its whole process group ended and its port is free. It stays stopped until you start it; a redeploy of %s starts every declared server again.",
		serverLabel(p.Hostname, name), p.Hostname)
	if siblings := runningSiblings(st, name); len(siblings) > 0 {
		result.Message += " Still running: " + strings.Join(siblings, ", ") + "."
	}
	return result, nil
}

// runningSiblings names the other servers the supervisor runs.
func runningSiblings(st devsup.State, name string) []string {
	var out []string
	for n, s := range st.Servers {
		if n != name && (s.Status == devsup.StatusRunning || s.Status == devsup.StatusStarting) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// statusDevServer reports the supervisor's status of a server and, when it
// has a port, probes it once from inside the container. Without a name on a
// dev service with several servers, none named after it, it reports each.
func statusDevServer(ctx context.Context, ssh SSHDeployer, p DevServerParams, ds *DevServers) (*DevServerResult, error) {
	name, err := resolveServerName(p, ds, false)
	if err != nil && p.Name == "" && len(ds.Servers()) > 1 {
		return statusEveryServer(ctx, ssh, p, ds)
	}
	if err != nil {
		return nil, err
	}
	server, ok := ds.State.Servers[name]
	if !ok {
		return nil, unknownServerErr(ds, name)
	}
	declared, _ := ds.Server(name)
	result := newResult(devServerActionStatus, p.Hostname, name)
	result.LogFile, result.PID, result.Restarts = server.LogPath, server.Pid, server.Restarts
	result.LastRestartReason = server.LastRestartReason
	defer func() {
		if result.Restarts > 0 {
			result.Message += fmt.Sprintf(" The supervisor has restarted it %d times.", result.Restarts)
		}
		if result.LastRestartReason != "" {
			result.Message += fmt.Sprintf(" Last restart reason: %s.", result.LastRestartReason)
		}
	}()
	label := serverLabel(p.Hostname, name)
	switch server.Status {
	case devsup.StatusCrashed:
		applyCrash(ctx, ssh, result, name, server)
		return result, nil
	case devsup.StatusStopped:
		result.Reason = reasonStopped
		result.Message = fmt.Sprintf("Dev server %s is stopped.", label)
		return result, nil
	case devsup.StatusRunning, devsup.StatusStarting:
	}
	port := p.Port
	if port == 0 {
		port = declared.Port
	}
	if port == 0 {
		result.Message = fmt.Sprintf("Dev server %s is %s (no HTTP port).", label, server.Status)
		return result, nil
	}
	healthPath := probedPath(p, ds, name)
	result.Port, result.HealthPath = port, healthPath
	// curl's exit follows the code: 28 is a listener that accepted and did
	// not answer within --max-time.
	out, _ := ssh.ExecSSH(ctx, p.Hostname, fmt.Sprintf(
		"curl -s -o /dev/null -w '%%{http_code}' --max-time 2 http://localhost:%d%s; echo \" $?\"", port, healthPath))
	code, exit, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	switch {
	case (code == "" || code == "000") && exit == "28":
		result.Reason = reasonSlow
		result.Message = fmt.Sprintf("Dev server %s is %s and accepts connections on :%d, but sent no HTTP answer on %s within 2s — likely still compiling its first response; check again shortly.", label, server.Status, port, healthPath)
		return result, nil
	case code == "" || code == "000":
		result.Reason = "connection_refused"
		result.Message = fmt.Sprintf("Dev server %s is %s but nothing answers HTTP on :%d yet.", label, server.Status, port)
		return result, nil
	}
	var httpCode int
	_, _ = fmt.Sscanf(code, "%d", &httpCode)
	result.HealthStatus = httpCode
	result.HealthOK = healthyHTTP(httpCode)
	if !result.HealthOK {
		result.Reason = fmt.Sprintf("http_%d", httpCode)
		result.Message = fmt.Sprintf("Dev server %s is %s; its health probe got HTTP %d on :%d%s.", label, server.Status, httpCode, port, healthPath)
		return result, nil
	}
	result.URL = devServerURL(p.Hostname, port, healthPath)
	result.Message = fmt.Sprintf("Dev server %s is running on :%d (HTTP %d on %s).", label, port, httpCode, healthPath)
	return result, nil
}

// statusEveryServer is a status of each of the dev service's servers, one
// sentence each; it reports none of them alone.
func statusEveryServer(ctx context.Context, ssh SSHDeployer, p DevServerParams, ds *DevServers) (*DevServerResult, error) {
	servers := ds.Servers()
	lines := make([]string, 0, len(servers))
	for _, s := range servers {
		one := p
		one.Name, one.Port, one.HealthPath = s.Name, 0, ""
		if _, ok := ds.State.Servers[s.Name]; !ok {
			lines = append(lines, fmt.Sprintf("Dev server %s is declared but the supervisor does not run it.", serverLabel(p.Hostname, s.Name)))
			continue
		}
		r, err := statusDevServer(ctx, ssh, one, ds)
		if err != nil {
			return nil, err
		}
		lines = append(lines, r.Message)
	}
	result := newResult(devServerActionStatus, p.Hostname, p.Hostname)
	result.Unreported = true
	result.Message = fmt.Sprintf("%s declares %d dev servers:\n%s", p.Hostname, len(servers), strings.Join(lines, "\n"))
	return result, nil
}

// logsDevServer tails the server's own log through the supervisor.
func logsDevServer(ctx context.Context, ssh SSHDeployer, p DevServerParams, ds *DevServers) (*DevServerResult, error) {
	name, err := resolveServerName(p, ds, false)
	if err != nil {
		return nil, err
	}
	server, ok := ds.State.Servers[name]
	if !ok {
		return nil, unknownServerErr(ds, name)
	}
	lines := p.LogLines
	if lines <= 0 {
		lines = defaultLogTailLines
	}
	lines = min(lines, 500)
	result := newResult(devServerActionLogs, p.Hostname, name)
	result.LogFile = server.LogPath
	result.LogTail = supervisorLogTail(ctx, ssh, p.Hostname, name, lines)
	result.Message = fmt.Sprintf("Tailing last %d lines of %s on %s.", lines, server.LogPath, p.Hostname)
	return result, nil
}

// defaultNoProbeSettle bounds the pause between a start and the status read
// in no-probe mode: long enough for init-phase failures to end the process.
const defaultNoProbeSettle = 3 * time.Second

// noProbeSettleNanos holds the current settle interval in nanoseconds,
// atomic so test helpers can override it without racing parallel readers.
var noProbeSettleNanos atomic.Int64

func init() {
	noProbeSettleNanos.Store(int64(defaultNoProbeSettle))
}

func currentNoProbeSettle() time.Duration {
	return time.Duration(noProbeSettleNanos.Load())
}

func storeNoProbeSettle(d time.Duration) time.Duration {
	return time.Duration(noProbeSettleNanos.Swap(int64(d)))
}

// sleepCtx sleeps for d or until ctx is cancelled, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// runHealthProbe polls the health endpoint server-side in a single SSH
// call. Each attempt may take the rest of the wait, so a first response
// slower than one poll still passes; an attempt that runs out of it on an
// open connection (curl exit 28) answers SLOW. Returns probe output,
// locally-measured elapsed, a deadline-hit flag, and the ssh error
// (convention: error last).
func runHealthProbe(parent context.Context, ssh SSHDeployer, hostname string, port int, healthPath string, wait int) ([]byte, int64, bool, error) {
	maxAttempts := (wait*1000)/pollIntervalMillis + 1
	probe := fmt.Sprintf(
		`set +e; start_ns=$(date +%%s%%N); end=$(( $(date +%%s) + %d )); code=000; for i in $(seq 1 %d); do `+
			`left=$(( end - $(date +%%s) )); [ "$left" -gt 0 ] || break; `+
			`code=$(curl -s -o /dev/null -w '%%{http_code}' --max-time "$left" http://localhost:%d%s 2>/dev/null); rc=$?; `+
			`if [ -n "$code" ] && [ "$code" -ge 200 ] 2>/dev/null && [ "$code" -lt 400 ] 2>/dev/null; then `+
			`end_ns=$(date +%%s%%N); echo "OK $code $(( (end_ns - start_ns) / 1000000 ))"; exit 0; fi; `+
			`if [ "$rc" -eq 28 ]; then echo "SLOW $code"; exit 1; fi; `+
			`sleep %s; done; `+
			`echo "FAIL $code"; exit 1`,
		wait, maxAttempts, port, healthPath, pollIntervalAsShellDuration(),
	)

	probeCtx, cancel := context.WithTimeout(parent, time.Duration(wait)*time.Second+probeTimeoutSlack)
	defer cancel()
	start := time.Now()
	out, err := ssh.ExecSSH(probeCtx, hostname, probe)
	elapsed := time.Since(start).Milliseconds()
	return out, elapsed, probeCtx.Err() == context.DeadlineExceeded, err
}

// applyProbeSuccess fills in result fields for a successful probe.
func applyProbeSuccess(result *DevServerResult, probeLine string, elapsedMs int64, name string) {
	var httpCode int
	fields := strings.Fields(probeLine)
	if len(fields) >= 2 {
		// A malformed probe response is self-reported via the result shape.
		_, _ = fmt.Sscanf(fields[1], "%d", &httpCode)
	}
	result.HealthOK = true
	result.HealthStatus = httpCode
	// The probe measures its own wait inside the container; the caller's
	// clock (elapsedMs) also counts SSH round trips.
	result.StartMillis = elapsedMs
	if len(fields) >= 3 {
		if ms, err := strconv.ParseInt(fields[2], 10, 64); err == nil {
			result.StartMillis = ms
		}
	}
	result.URL = devServerURL(result.Hostname, result.Port, result.HealthPath)
	result.Message = fmt.Sprintf("Dev server %s started under the supervisor — health probe passed (HTTP %d in %dms, probed from inside the container). Reach it at %s (requires the app to bind 0.0.0.0, not loopback). It has its own log (logFile) and comes back by itself after a container restart or redeploy.",
		serverLabel(result.Hostname, name), httpCode, result.StartMillis, result.URL)
}

// applyProbeFailure classifies probe failure into a Reason code.
func applyProbeFailure(result *DevServerResult, probeLine string, deadlineHit bool, name, healthPath string, wait int, status devsup.Status) {
	switch {
	case deadlineHit:
		result.Reason = ReasonHealthProbeTimeout
	case probeLine == "":
		result.Reason = "health_probe_no_output"
	case strings.HasPrefix(probeLine, "SLOW"):
		result.Reason = reasonHealthProbeSlow
		result.Message = fmt.Sprintf(
			"Dev server %s accepts connections on :%d but sent no HTTP answer on %s within %ds (probed from inside the container); the supervisor reports it %s. It is likely still compiling its first response — check again with action=status, or start it with a longer waitSeconds.",
			serverLabel(result.Hostname, name), result.Port, healthPath, wait, status)
		return
	case strings.HasPrefix(probeLine, "FAIL "):
		fields := strings.Fields(probeLine)
		if len(fields) >= 2 && fields[1] != "" && fields[1] != "000" {
			result.Reason = "health_probe_http_" + fields[1]
			_, _ = fmt.Sscanf(fields[1], "%d", &result.HealthStatus)
		} else {
			result.Reason = "health_probe_connection_refused"
		}
	default:
		result.Reason = "health_probe_unknown: " + probeLine
	}
	result.Message = fmt.Sprintf(
		"Dev server %s did not pass the health probe (localhost:%d%s, probed from inside the container) within %ds (%s); the supervisor reports it %s. See logTail for the failing startup — if it references a missing dependency, install it over SSH first; if the server is still compiling, check again with action=status.",
		serverLabel(result.Hostname, name), result.Port, healthPath, wait, result.Reason, status,
	)
}

// devServerURL composes the consumer-vantage address of a dev server — the
// form reachable from the agent's container over the project-private network
// (internal DNS), NOT the localhost the in-container probe curls. Single owner
// so start, restart, and status messages compose it identically (B9).
func devServerURL(hostname string, port int, healthPath string) string {
	return "http://" + net.JoinHostPort(hostname, strconv.Itoa(port)) + healthPath
}

// DevServerSubdomainURL is the subdomain address of the port a dev server
// result probed — Zerops gives each HTTP port its own — or "" when that port
// serves no HTTP or its address is not known. Ask it only while the service's
// subdomain is on.
func DevServerSubdomainURL(ctx context.Context, client platform.Client, projectID string, result *DevServerResult) string {
	if result.Port == 0 {
		return ""
	}
	svc, err := LookupService(ctx, client, projectID, result.Hostname)
	if err != nil || !slices.ContainsFunc(svc.Ports, func(p platform.Port) bool { return p.Port == result.Port && (p.HTTPSupport || isHTTPScheme(p)) }) {
		return ""
	}
	return SubdomainURLForPort(ctx, client, projectID, svc, result.Port)
}

// pollIntervalAsShellDuration returns the pollIntervalMillis as a shell
// sleep argument ("0.500" for 500ms).
func pollIntervalAsShellDuration() string {
	return fmt.Sprintf("%d.%03d", pollIntervalMillis/1000, pollIntervalMillis%1000)
}
