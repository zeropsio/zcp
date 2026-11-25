package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
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
	"github.com/zeropsio/zcp/internal/topology"
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
	//   heap_limit                — crashed, and its log ends with Node's
	//                               heap-limit error: it ran out of its own
	//                               heap, not of the container's RAM
	//   port_in_use               — the supervisor could not start it: its
	//                               port stayed busy
	//   stopped                   — the server is stopped (status)
	//   no_supervisor             — the dev setup does not run zcp-devsup
	//   supervisor_unresponsive   — zcp-devsup, or the container over SSH,
	//                               did not answer in time (container out
	//                               of memory or CPU)
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
	//   loopback_only             — it listens on a loopback address only:
	//                               nothing outside the container reaches it
	//   starting                  — the supervisor reports its start under
	//                               way or its port awaited: not failed, read
	//                               it again
	//   start_stalled             — the supervisor's listen wait ran out with
	//                               its port never accepting: a failed start
	//   port_elsewhere            — its port is still awaited while its
	//                               process listens on other ports only
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
	// Pool is the dev pool whose container the lease alias in Hostname
	// reaches (docs/spec-workflows.md "Dev pools (DP)"); set only for a lease.
	// Public access and max RAM are the pool's, never the alias's.
	Pool string `json:"pool,omitempty"`
	// PreviewPort is the port the supervisor forwards to the reported server
	// on every interface (docs/spec-mate.md §2.8a.1 previewPortBase), 0 when
	// it forwards none.
	PreviewPort int `json:"previewPort,omitempty"`
	// PreviewURL is the pool's subdomain address of PreviewPort — a lease's
	// public address; set only for a lease, and only while the pool's
	// subdomain is on.
	PreviewURL string `json:"previewUrl,omitempty"`
	// SiblingEnv is, for a start or restart on a lease whose declaration
	// names siblingEnv, the overlay zcp set on its supervisor before it: each
	// variable set to the container hostname of the branch's lease on the
	// pool it names, and each left to the container's own env
	// (docs/spec-workflows.md §8 DP-22).
	SiblingEnv *LeaseSiblingEnv `json:"siblingEnv,omitempty"`
	// Wiring is LeaseWiringStale on a status of a lease whose servers run with
	// an overlay naming a container no lease of its group holds by zcp's
	// records; the message says which and the call that fixes it (DP-26).
	Wiring string `json:"wiring,omitempty"`
	// Unreported: the call read several servers and reports none of them
	// alone (a status of a dev service with several), so it says nothing of
	// whether "the" server runs.
	Unreported bool `json:"-"`
	// NewLog is, on a status that reports one server whose supervisor lists
	// devsup.CapabilityLogsSince, what its log gained since this zcp's
	// previous status of it (the first status: its current run); absent when
	// it was not read.
	NewLog *DevServerNewLog `json:"newLog,omitempty"`
	// Unsaved is, on a status of a lease, what releasing the lease would lose,
	// from the release guard's own reads (docs/spec-workflows.md §8 DP-4);
	// absent when either read did not answer.
	Unsaved *topology.LeaseUnsaved `json:"unsaved,omitempty"`
	// LeaseActivity is that read's activity of the lease's checkout; the
	// envelope's leases[] carries it beside Unsaved.
	LeaseActivity *topology.LeaseActivity `json:"-"`
	// subdomainOn: the caller has just found or switched the lease's pool's
	// subdomain on, so a preview URL is built without waiting for the pool's
	// own flag to show it (leasePreviewURL's requireOn).
	subdomainOn bool
}

// DevServerRead is one read-only supervisor or lease checkout result.
// A nil Servers slice means the supervisor was not read; an empty slice means
// it answered with no servers. The two reads can fail independently.
type DevServerRead struct {
	Servers       []topology.DevServerState
	ServersUnread DevServerReadFailure
	Unsaved       *topology.LeaseUnsaved
	WorkUnread    string
}

// DevServerReadFailure is a safe reason for an unread supervisor: raw SSH
// and supervisor answers may contain environment values.
type DevServerReadFailure string

// ReadDevServerStatus begins one read of the supervisor and, for a lease, one
// of its checkout, without recovering or reloading the supervisor. Each read
// has its own receipt so a slow one cannot hide the other. The caller must
// already have confirmed a lease's ownership.
func ReadDevServerStatus(ctx context.Context, ssh SSHDeployer, client platform.Client, projectID, stateDir, hostname, root string, lease bool) (<-chan DevServerRead, <-chan DevServerRead) {
	servers := make(chan DevServerRead, 1)
	var checkout chan DevServerRead
	var rec leaseRecord
	if lease {
		checkout = make(chan DevServerRead, 1)
		var ok bool
		var err error
		rec, ok, err = leaseTargetRecord(stateDir, hostname)
		if err != nil || !ok {
			servers <- DevServerRead{ServersUnread: "lease record not read"}
			checkout <- DevServerRead{WorkUnread: "lease record not read"}
			return servers, checkout
		}
		go func() {
			facts := readLeaseFactsCommand(ctx, LeaseDeps{SSH: ssh}, rec, leaseCheckoutFactsCommand())
			got := DevServerRead{Unsaved: facts.unsaved}
			if facts.unsaved == nil {
				got.WorkUnread = cmp.Or(facts.notAnswering, "checkout did not answer")
				if ctx.Err() != nil {
					got.WorkUnread = "no answer in 3 s"
				}
			}
			checkout <- got
		}()
	}
	go func() {
		ds, err := ReadDevServers(ctx, ssh, hostname, root)
		if err != nil {
			servers <- DevServerRead{ServersUnread: devServerReadReason(err)}
			return
		}
		result := &DevServerResult{Hostname: hostname, Read: ds}
		if lease {
			ds.Service, result.Pool = rec.PoolHostname, rec.PoolHostname
		}
		servers <- DevServerRead{Servers: DevServerStates(ctx, client, projectID, result)}
	}()
	return servers, checkout
}

// devServerReadReason names the failed read without exposing an untrusted
// supervisor answer, which can contain its environment overlay.
func devServerReadReason(err error) DevServerReadFailure {
	var missing *NoSupervisorError
	var unresponsive *SupervisorUnresponsiveError
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "no answer in 3 s"
	case errors.As(err, &missing):
		return "supervisor not running"
	case errors.As(err, &unresponsive):
		return "supervisor did not answer"
	default:
		return "supervisor read failed"
	}
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
	// StateDir is zcp's state directory: with it, a Hostname that is the
	// alias of a lease zcp holds (LeaseTarget) is driven as that lease — the
	// supervisor over SSH to the alias, every platform fact from its pool.
	StateDir string
	// LogCursors is where this zcp's previous status of each server read its
	// log to; nil reads every status as the first.
	LogCursors *DevServerLogCursors
	// lease is the lease Hostname names, resolved by ExecuteDevServer.
	lease *LeaseTargetInfo
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
	reasonHeapLimit    = "heap_limit"
	reasonPortInUse    = "port_in_use"
	reasonStopped      = "stopped"
	reasonNoSupervisor = "no_supervisor"
	// reasonSupervisorUnresponsive never offers the move onto the
	// supervisor: one runs there.
	reasonSupervisorUnresponsive = "supervisor_unresponsive"
	// reasonStarting: the supervisor reports the server's start under way or
	// its port awaited (startingWait). No failed start: it is read again later.
	reasonStarting = "starting"
	// reasonStartStalled: the supervisor's listen wait ran out with the
	// server's port never accepting (startingStalled): a failed start.
	reasonStartStalled = "start_stalled"
	// reasonPortElsewhere: its port is awaited while its process group listens
	// on other ports only.
	reasonPortElsewhere = "port_elsewhere"
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
	// ReasonLoopbackOnly: the server listens on a loopback address only.
	ReasonLoopbackOnly = "loopback_only"
	reasonSlow         = "slow"
)

const (
	// defaultDevServerWait is how long the start probe waits for a
	// successful health check before giving up. 15s is enough for a
	// typical Node/TS hot-reload boot on Zerops.
	defaultDevServerWait = 15
	// maxDevServerWait is an upper bound that keeps pathological boot
	// loops from eating the whole bash-timeout window.
	maxDevServerWait = 45
	// maxSupervisorWait bounds a wait owned by a capable supervisor.
	maxSupervisorWait = 300
	// defaultLogTailLines is how many trailing log lines a result of a
	// failed start or status carries: enough to diagnose a failed startup.
	defaultLogTailLines = 40
	// logsActionTailLines is how many the logs action reads when it is not
	// asked for a number: a dev server's recent output, not its history.
	logsActionTailLines = 20
	// startedLogTailLines is the tail of a server that started: enough to
	// show it came up, nothing to diagnose.
	startedLogTailLines = 8
	// pollIntervalMillis is the poll cadence of the start-probe loop.
	pollIntervalMillis = 500
	// probeTimeoutSlack is added on top of waitSeconds to bound the
	// foreground ssh call. The remote probe ends by itself (its `seq 1..N`
	// loop and curl's --max-time), so this bound only ends a wedged call: it
	// covers ssh dial, tear-down and the probe's process starts on a
	// container busy compiling — a probe killed by it loses its verdict.
	probeTimeoutSlack = 30 * time.Second
	// logTailTimeout bounds the log-tail ssh call on the failure path.
	logTailTimeout = 5 * time.Second
	// containerIdentityTimeout bounds a one-round-trip remote write or read.
	containerIdentityTimeout = 10 * time.Second
)

// devServerWait decides the bound and the owner of a call's wait once.
func devServerWait(action string, asked int, v SupervisorVersion) (seconds int, bySupervisor bool) {
	bySupervisor = v.Supports(devsup.CapabilityWait)
	switch action {
	case devServerActionStart, devServerActionRestart:
		if asked <= 0 {
			asked = defaultDevServerWait
		}
		if bySupervisor {
			return min(asked, maxSupervisorWait), true
		}
		return min(asked, maxDevServerWait), false
	case devServerActionStatus:
		if bySupervisor && asked > 0 {
			return min(asked, maxSupervisorWait), true
		}
	}
	return 0, false
}

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
// nothing changed; the caller moves the dev setup onto it. A supervisor that
// does not answer in time is a result, never that error.
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
	rec, isLease, err := leaseTargetRecord(p.StateDir, p.Hostname)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", p.Hostname, err)
	}
	// A status of a lease also says what releasing it would lose: beside the
	// supervisor's read, the one read of its checkout zerops_lease
	// action=status makes (docs/spec-workflows.md §8 DP-21, DP-4).
	var checkout chan leaseFacts
	if isLease && strings.ToLower(p.Action) == devServerActionStatus {
		checkout = make(chan leaseFacts, 1)
		go func() { checkout <- readLeaseFacts(ctx, LeaseDeps{SSH: ssh}, rec) }()
	}
	result, err := executeDevServer(ctx, ssh, client, projectID, p, rec, isLease)
	if checkout != nil {
		facts := <-checkout
		if result != nil {
			result.Unsaved, result.LeaseActivity = facts.unsaved, facts.activity
		}
	}
	return result, err
}

// executeDevServer is ExecuteDevServer once the target is resolved: rec is
// the lease Hostname names when isLease.
func executeDevServer(ctx context.Context, ssh SSHDeployer, client platform.Client, projectID string, p DevServerParams, rec leaseRecord, isLease bool) (*DevServerResult, error) {
	lease := rec.targetInfo()
	if isLease {
		// A lease is a pool container, not a service: nothing to look up, and
		// the container is reached by its alias.
		p.lease = &lease
	} else if err := verifyDevServerTarget(ctx, client, projectID, p.Hostname); err != nil {
		return nil, err
	}
	action := strings.ToLower(p.Action)
	ds, err := ReadDevServers(ctx, ssh, p.Hostname, p.Root)
	why := ""
	if p.lease != nil {
		// A lease's supervisor is zcp's to start: its container's run.start
		// never runs again after the reset of an earlier release (DP-13).
		ds, why, err = recoverLeaseSupervisor(ctx, ssh, p.Hostname, p.Root, ds, err)
		if ds != nil {
			// It runs the entries naming its pool: the container's hostname
			// env is the pool's.
			ds.Service = p.lease.Pool
		}
	}
	var noSup *NoSupervisorError
	var unresponsive *SupervisorUnresponsiveError
	switch {
	case errors.As(err, &unresponsive):
		return unresponsiveResult(action, p.Hostname, unresponsive), nil
	case errors.As(err, &noSup) && p.lease != nil && (action == devServerActionStart || action == devServerActionRestart):
		return nil, leaseNoSupervisorErr(*p.lease, noSup.Installed, why)
	case errors.As(err, &noSup) && (action == devServerActionStart || action == devServerActionRestart):
		return nil, err
	case errors.As(err, &noSup) && p.lease != nil:
		return leaseNoSupervisorResult(action, *p.lease, noSup.Installed, why), nil
	case errors.As(err, &noSup):
		return noSupervisorResult(action, p.Hostname), nil
	case err != nil && p.lease != nil:
		if why, ok := leaseNotAnswering(err); ok {
			return nil, leaseNotAnsweringError(p.Hostname, why, "no dev server of it was read or changed", err)
		}
		return nil, err
	case err != nil:
		return nil, err
	}
	dropped, err := dropUndeclared(ctx, ssh, ds)
	if errors.As(err, &unresponsive) {
		return unresponsiveResult(action, p.Hostname, unresponsive), nil
	}
	if err != nil {
		return nil, err
	}
	var result *DevServerResult
	switch {
	case slices.Contains(dropped, p.Name) && action != devServerActionStart && action != devServerActionRestart:
		result = newResult(action, p.Hostname, p.Name)
	case action == devServerActionStart, action == devServerActionRestart:
		result, err = startDevServer(ctx, ssh, client, projectID, p, ds)
	case action == devServerActionStop:
		result, err = stopDevServer(ctx, ssh, p, ds)
	case action == devServerActionStatus:
		result, err = statusDevServer(ctx, ssh, p, ds)
	default:
		result, err = logsDevServer(ctx, ssh, p, ds)
	}
	if errors.As(err, &unresponsive) {
		return unresponsiveResult(action, p.Hostname, unresponsive), nil
	}
	if err != nil {
		return devServerActionError(action, p.Hostname, p.lease, why, err)
	}
	for _, name := range dropped {
		if result.Message != "" {
			result.Message += "\n"
		}
		result.Message += fmt.Sprintf("Dev server %s is no longer declared in %s, so the supervisor stopped it.", serverLabel(p.Hostname, name), devsup.DeclarationPath)
	}
	result.settle(ds)
	if action == devServerActionStatus && !result.Unreported && ds.Supervisor.Supports(devsup.CapabilityLogsSince) {
		name := cmp.Or(result.Name, result.Hostname)
		if _, ok := ds.State.Servers[name]; ok {
			result.NewLog = readNewLog(ctx, ssh, p.Hostname, name, p.LogCursors)
		}
		if result.NewLog != nil && len(result.NewLog.Errors) > 0 {
			result.Message += fmt.Sprintf(" Its current run logged %d error line(s) since this zcp's previous status of it (newLog.errors).", len(result.NewLog.Errors))
		}
	}
	if p.lease != nil {
		if action == devServerActionStatus {
			result.applyStaleWiring(p.StateDir, *p.lease, ds.State)
		}
		result.applyLease(*p.lease)
	}
	return result, nil
}

// devServerActionError reports a missing supervisor on status and leaves other action errors to their callers.
func devServerActionError(operation, hostname string, lease *LeaseTargetInfo, why string, err error) (*DevServerResult, error) {
	var noSup *NoSupervisorError
	if operation == devServerActionStatus && errors.As(err, &noSup) {
		if lease != nil {
			return leaseNoSupervisorResult(operation, *lease, noSup.Installed, why), nil
		}
		return noSupervisorResult(operation, hostname), nil
	}
	return nil, err
}

// dropUndeclared has the supervisor re-read the declaration when it still runs
// a declared server the file no longer names (an entry removed by hand, which
// the supervisor sees only on a reload, start or restart): the reload stops
// and drops it. It answers the servers it stopped.
func dropUndeclared(ctx context.Context, ssh SSHDeployer, ds *DevServers) ([]string, error) {
	var gone []string
	// A file the supervisor cannot run changes nothing on a reload; a missing
	// file (not committed, stashed, another commit) removed nothing by hand,
	// though the supervisor would read it as empty and stop every server.
	runnable := ds.DeclarationExists && ds.DeclarationErr == nil && ds.OlderThanDeclaration() == ""
	for name, st := range ds.State.Servers {
		live := st.Status == devsup.StatusRunning || st.Status == devsup.StatusStarting
		if runnable && st.Source == devsup.SourceDeclared && live && !slices.ContainsFunc(ds.Servers(), func(s devsup.Server) bool { return s.Name == name }) {
			gone = append(gone, name)
		}
	}
	if len(gone) == 0 {
		return nil, nil
	}
	st, err := supervisorCtl(ctx, ssh, ds.Hostname, "reload")
	if err != nil {
		return nil, err
	}
	ds.observe(st)
	slices.Sort(gone)
	return gone, nil
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
	r.PreviewPort = ds.State.Servers[name].PreviewPort
}

// applyLease names the pool a lease result is about, and gives the consumer
// vantage address the container's own hostname: the alias is an SSH host
// name, not one the project network resolves.
func (r *DevServerResult) applyLease(l LeaseTargetInfo) {
	r.Pool = l.Pool
	from, to := "http://"+l.Alias+":", "http://"+l.ContainerHostname+":"
	r.URL = strings.ReplaceAll(r.URL, from, to)
	r.Message = strings.ReplaceAll(r.Message, from, to)
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
	// A pool's or Box's own hostname reaches an arbitrary container over SSH
	// (PS-3): a dev server in a pool runs in a lease, addressed by its alias.
	switch typeVersion := target.ServiceStackTypeInfo.ServiceStackTypeVersionName; {
	case topology.IsPoolType(typeVersion):
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%q is a Swarm pool, not a dev container", hostname),
			fmt.Sprintf("Lease one of its containers with zerops_lease action=acquire pool=%q branch=<your branch> and pass the lease alias as hostname.", hostname))
	case topology.IsSandboxType(typeVersion):
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%q is a Zerops Box, not a dev container", hostname),
			"A Box hosts sandboxes; run dev servers in an app's dev container or a dev-pool lease.")
	}
	switch target.Status {
	case platform.ServiceStatusStopped:
		// Nothing answers SSH on a stopped service: say so before an SSH
		// call waits out its bound on it.
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("dev server target %q is STOPPED — no container runs to attach a dev server to", hostname),
			fmt.Sprintf("Start it (zerops_manage action=start serviceHostname=%q), then call zerops_dev_server again.", hostname),
		)
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
		Message: fmt.Sprintf("%s has no dev-server supervisor yet, so no dev server runs under zcp there. zerops_dev_server hostname=%q action=start sets it up (one redeploy of %s; uncommitted work stays).",
			hostname, hostname, hostname),
	}
}

// leaseNoSupervisorErr refuses a start on a lease whose container runs no
// supervisor. The move onto the supervisor redeploys a dev service; a lease is
// a pool container, and what a pool's image carries is its own zerops.yaml's.
// installed: zcp-devsup is in the image and zcp's start of it (why) did not
// bring it up.
func leaseNoSupervisorErr(l LeaseTargetInfo, installed bool, why string) error {
	if installed {
		return platform.NewPlatformError(platform.ErrPreflightFailed,
			fmt.Sprintf("Lease %s (a container of pool %s) runs no dev-server supervisor: %s", l.Alias, l.Pool, why),
			fmt.Sprintf("Read the supervisor's own output over SSH (ssh %s %s), then retry; or release the lease and acquire another.", l.Alias, shellQuote("tail -n 50 "+leaseSupervisorLog)))
	}
	return platform.NewPlatformError(platform.ErrPreflightFailed,
		fmt.Sprintf("Lease %s (a container of pool %s): %s", l.Alias, l.Pool, leaseNoDevsupSentence),
		fmt.Sprintf("A dev pool's image carries the supervisor: install zcp-devsup in run.prepareCommands and set run.start to `zcp-devsup run` in the zerops.yaml of pool %s (docs: zerops_knowledge), deploy the pool, release this lease, roll the pool out, then acquire again.", l.Pool))
}

// leaseNoSupervisorResult answers stop, status and logs on such a lease.
func leaseNoSupervisorResult(action string, l LeaseTargetInfo, installed bool, why string) *DevServerResult {
	msg := fmt.Sprintf("Lease %s (a container of pool %s): %s. Install zcp-devsup in run.prepareCommands and set run.start to `zcp-devsup run` in the pool's zerops.yaml; deploy the pool, release this lease, roll the pool out, then acquire again.",
		l.Alias, l.Pool, leaseNoDevsupSentence)
	if installed {
		msg = fmt.Sprintf("Lease %s (a container of pool %s) runs no dev-server supervisor, so no dev server runs under zcp there: %s. Its own output is in %s on the lease.",
			l.Alias, l.Pool, why, leaseSupervisorLog)
	}
	return &DevServerResult{
		Action:   action,
		Hostname: l.Alias,
		Pool:     l.Pool,
		Reason:   reasonNoSupervisor,
		Message:  msg,
	}
}

// unresponsiveResult is any action on a dev service whose supervisor did not
// answer in time.
func unresponsiveResult(action, hostname string, err *SupervisorUnresponsiveError) *DevServerResult {
	return &DevServerResult{
		Action:   action,
		Hostname: hostname,
		Reason:   reasonSupervisorUnresponsive,
		Message:  err.Error(),
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

// supervisedSummary lists the servers the supervisor runs, with its status.
func supervisedSummary(ds *DevServers) string {
	names := slices.Sorted(maps.Keys(ds.State.Servers))
	for i, n := range names {
		names[i] = fmt.Sprintf("%s (%s)", n, ds.State.Servers[n].Status)
	}
	return strings.Join(names, ", ")
}

// declareHint is how a dev service without a declaration file gets one back
// without a start: a start declares only the server it names, and the reload
// it sends stops every other one.
func declareHint(hostname string) string {
	return fmt.Sprintf("To have zcp manage them again, restore %s (from git when it was committed) or write it with each server's name, command, port and cwd, then run zcp-devsup ctl reload on %s over SSH. Do not start one of them with zerops_dev_server before that: the start declares only that server and stops the others.",
		devsup.DeclarationPath, hostname)
}

// unknownServerErr answers a name the repo does not declare. Without a
// declaration file it names what the supervisor runs, and suggests a start
// only when it runs nothing.
func unknownServerErr(ds *DevServers, name string) error {
	if !ds.DeclarationExists && len(ds.State.Servers) > 0 {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%s has no %s and its supervisor runs no dev server named %q", ds.Hostname, devsup.DeclarationPath, name),
			fmt.Sprintf("Its supervisor runs %s: pass name=<one of them>. %s", supervisedSummary(ds), declareHint(ds.Hostname)))
	}
	hint := fmt.Sprintf("Start it with zerops_dev_server action=start hostname=%q name=%q command=<the command that runs it>.", ds.Hostname, name)
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
// service whose supervisor runs it. An entry a lease adds names the lease's
// pool, so in a repository of several parts only that part's leases run it;
// a declared entry keeps its service, or its lack of one.
func wantedServer(p DevServerParams, name string, declared devsup.Server, isDeclared bool) (devsup.Server, error) {
	want := declared
	want.Name = name
	switch {
	case p.lease != nil && !isDeclared:
		want.Service = p.lease.Pool
	case p.Root != "" && want.Service == "":
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
	// A lease's servers get the branch's sibling hostnames through the
	// supervisor's overlay before anything starts or reloads them (DP-22).
	var wiring *leaseWiring
	if p.lease != nil {
		w, err := wireLeaseServers(ctx, ssh, leasePreviewURLs(ctx, client, projectID), p, ds)
		if err != nil {
			return nil, err
		}
		wiring = &w
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
	// A reload restarts a server whose run changed and starts a new one, and
	// a changed overlay one that ran under the old; a restart of any other is
	// the supervisor's own.
	verb := devServerActionStart
	if action == devServerActionRestart && isDeclared && declared.SameRun(want) && (wiring == nil || !wiring.restarted[name]) {
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
		if err == nil && ds.State.Servers[name].Status == devsup.StatusCrashed {
			appendCrashWait(result, ds.Supervisor, ds.State.Servers[name])
		}
	} else {
		result, err = probeStarted(ctx, ssh, ds, result, p, name, want.Port, begun)
	}
	if err == nil && memoryNote != "" {
		result.Message += "\n" + memoryNote
	}
	if err == nil && wiring != nil {
		wiring.apply(result)
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
	wait, bySupervisor := devServerWait(result.Action, p.WaitSeconds, ds.Supervisor)
	if bySupervisor {
		return probeSupervisorStarted(ctx, ssh, ds, result, p, name, port, begun, wait)
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
		appendCrashWait(result, ds.Supervisor, server)
		return result, nil
	}
	switch {
	case probeErr == nil && strings.HasPrefix(probeLine, "OK "):
		result.LogTail = currentRun(supervisorLogTail(ctx, ssh, p.Hostname, name, startedLogTailLines))
		applyProbeSuccess(result, probeLine, time.Since(begun).Milliseconds(), name)
	case server.Status == devsup.StatusStarting && answerStarting(ds.Supervisor, server) != startingProbe: // the wait ended first
		result.LogTail = currentRun(supervisorLogTail(ctx, ssh, p.Hostname, name, defaultLogTailLines))
		applyStarting(ctx, ssh, result, name, server, answerStarting(ds.Supervisor, server), ds.Supervisor)
	default:
		result.LogTail = currentRun(supervisorLogTail(ctx, ssh, p.Hostname, name, defaultLogTailLines))
		applyProbeFailure(result, probeLine, probeDeadline, name, healthPath, wait, server.Status)
	}
	// The probe reaches a loopback listener from inside the container; the
	// server listens only now, so the call's first read of them is stale.
	if ds.observeListeners(ctx, ssh); ds.LoopbackOnly[port] {
		applyLoopbackOnly(result, name, port)
	}
	return result, nil
}

// waitDevServer reads the supervisor's answer once, falling back to status
// only if the wait's answer was lost.
func waitDevServer(ctx context.Context, ssh SSHDeployer, ds *DevServers, name string, seconds int) error {
	st, err := supervisorWait(ctx, ssh, ds.Hostname, name, seconds)
	if err != nil {
		st, err = supervisorCtl(ctx, ssh, ds.Hostname, devServerActionStatus, name)
		if err != nil {
			return fmt.Errorf("read dev server after wait: %w", err)
		}
	}
	if _, ok := st.Servers[name]; ok {
		servers := maps.Clone(ds.State.Servers)
		servers[name] = st.Servers[name] // the wait's answer
		st.Servers = servers
	}
	ds.observe(st)
	return nil
}

// probeSupervisorStarted judges the state returned by the wait and makes one
// health request only when that state calls for a probe.
func probeSupervisorStarted(ctx context.Context, ssh SSHDeployer, ds *DevServers, result *DevServerResult, p DevServerParams, name string, port int, begun time.Time, wait int) (*DevServerResult, error) {
	if err := waitDevServer(ctx, ssh, ds, name, wait); err != nil {
		return nil, err
	}
	server := ds.State.Servers[name]
	result.Port, result.LogFile, result.PID = port, server.LogPath, server.Pid
	switch server.Status {
	case devsup.StatusCrashed:
		if server.RetryPending {
			applyStarting(ctx, ssh, result, name, server, startingWait, ds.Supervisor)
		} else {
			applyCrash(ctx, ssh, result, name, server)
			appendCrashWait(result, ds.Supervisor, server)
		}
		return result, nil
	case devsup.StatusStopped:
		result.Reason = reasonStopped
		result.Message = fmt.Sprintf("Dev server %s is stopped.", serverLabel(p.Hostname, name))
		return result, nil
	case devsup.StatusStarting:
		if answer := answerStarting(ds.Supervisor, server); answer != startingProbe {
			result.LogTail = currentRun(supervisorLogTail(ctx, ssh, p.Hostname, name, defaultLogTailLines))
			applyStarting(ctx, ssh, result, name, server, answer, ds.Supervisor)
			return result, nil
		}
	case devsup.StatusRunning:
	}
	healthPath := probedPath(p, ds, name)
	result.HealthPath = healthPath
	remaining := max(2, wait-int(time.Since(begun).Seconds()))
	probeLine, deadline, probeErr := runHealthProbeOnce(ctx, ssh, p.Hostname, port, healthPath, remaining)
	if probeErr == nil && strings.HasPrefix(probeLine, "OK ") {
		result.LogTail = currentRun(supervisorLogTail(ctx, ssh, p.Hostname, name, startedLogTailLines))
		applyProbeSuccess(result, probeLine, time.Since(begun).Milliseconds(), name)
	} else {
		result.LogTail = currentRun(supervisorLogTail(ctx, ssh, p.Hostname, name, defaultLogTailLines))
		applyProbeFailure(result, probeLine, deadline, name, healthPath, remaining, server.Status)
	}
	if ds.observeListeners(ctx, ssh); ds.LoopbackOnly[port] {
		applyLoopbackOnly(result, name, port)
	}
	return result, nil
}

// applyLoopbackOnly reports a server that listens on a loopback address only
// (as verify's DevPort.LoopbackOnly): a probe inside the container reaches
// it, nothing outside does, so the reply gives no address.
func applyLoopbackOnly(result *DevServerResult, name string, port int) {
	result.Reason, result.URL = ReasonLoopbackOnly, ""
	result.Message = fmt.Sprintf("Dev server %s listens on loopback only (127.0.0.1:%d): neither %s nor its subdomain reaches it. Make it bind 0.0.0.0 (for example --host 0.0.0.0) and restart it, unless a web server in front of it proxies to it.",
		serverLabel(result.Hostname, name), port, devServerURL(result.Hostname, port, ""))
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

// ServerVerdict is a declared server's state as every reply judges it: the
// phrase that follows its name, and what it asks of the agent — Down: read its
// logs, fix it and start it; Unsettled: read it again later, as it is still
// starting or the supervisor starts it once more.
type ServerVerdict struct {
	Phrase          string
	Down, Unsettled bool
}

// JudgeServer is the verdict on a declared server whose state the supervisor
// reports as server; ok false is one the supervisor does not run.
func JudgeServer(server devsup.ServerState, ok bool) ServerVerdict {
	switch {
	case !ok:
		return ServerVerdict{Phrase: "not started", Down: true}
	case server.Status == devsup.StatusRunning:
		return ServerVerdict{Phrase: "running"}
	case server.Status == devsup.StatusStopped:
		return ServerVerdict{Phrase: "stopped"}
	case server.Status == devsup.StatusStarting:
		return ServerVerdict{Phrase: "still starting", Unsettled: true}
	case server.Status != devsup.StatusCrashed:
		return ServerVerdict{Phrase: string(server.Status), Down: true}
	}
	verdict := ServerVerdict{Down: !server.RetryPending, Unsettled: server.RetryPending}
	switch {
	case server.TaskLimit != nil:
		verdict.Phrase = fmt.Sprintf("crashed (%s): %s", exitText(server.Exit), TaskLimitNote(*server.TaskLimit, server.RetryPending))
	case server.RetryPending:
		verdict.Phrase = fmt.Sprintf("crashed (%s); %s", exitText(server.Exit), RetryPendingNote)
	case server.Exit != nil && server.Exit.Signal == devsup.ExitPortBusy:
		verdict.Phrase = "could not start: its port stayed busy"
	default:
		verdict.Phrase = fmt.Sprintf("crashed (%s)", exitText(server.Exit))
	}
	return verdict
}

// applyCrash reports a server the supervisor holds as crashed, with its exit
// and log tail: down until started, or coming back when the supervisor's one
// more start is pending. A crash whose log holds Node's heap-limit error, with
// no task limit or busy port reported, is reason heap_limit with its fix.
func applyCrash(ctx context.Context, ssh SSHDeployer, result *DevServerResult, name string, server devsup.ServerState) {
	tail := currentRun(supervisorLogTail(ctx, ssh, result.Hostname, name, crashMatchLogLines))
	result.LogTail = lastLogLines(tail, defaultLogTailLines)
	label := serverLabel(result.Hostname, name)
	verdict := JudgeServer(server, true)
	portBusy := server.Exit != nil && server.Exit.Signal == devsup.ExitPortBusy
	heapLimit := server.TaskLimit == nil && !portBusy && strings.Contains(tail, nodeHeapLimitError)
	result.Reason = reasonCrashed
	if heapLimit {
		result.Reason = reasonHeapLimit
	}
	switch {
	case heapLimit && verdict.Unsettled:
		result.Message = fmt.Sprintf("Dev server %s crashed (%s); %s. %s %s",
			label, exitText(server.Exit), RetryPendingNote, heapLimitFix, RecheckSentence(result.Hostname, name, []string{name}))
	case heapLimit:
		result.Message = fmt.Sprintf("Dev server %s crashed (%s); the supervisor leaves it down. %s",
			label, exitText(server.Exit), heapLimitFix)
	case verdict.Unsettled:
		result.Message = fmt.Sprintf("Dev server %s %s. %s", label, verdict.Phrase, RecheckSentence(result.Hostname, name, []string{name}))
	case server.TaskLimit != nil:
		result.Message = fmt.Sprintf("Dev server %s %s.", label, verdict.Phrase)
	case portBusy:
		result.Reason = reasonPortInUse
		result.Message = fmt.Sprintf("Dev server %s %s — something outside the supervisor listens on it. Find and stop that process, then start the server again.", label, verdict.Phrase)
	default:
		result.Message = fmt.Sprintf("Dev server %s %s; the supervisor leaves it down. Read logTail, fix the cause, then start it again.", label, verdict.Phrase)
	}
}

// appendCrashWait extends only this tool's retry-pending replies.
func appendCrashWait(result *DevServerResult, v SupervisorVersion, server devsup.ServerState) {
	if server.RetryPending && v.Supports(devsup.CapabilityWait) {
		result.Message += " " + waitSentence()
	}
}

// nodeHeapLimitError is the line Node prints when a process reaches its own
// V8 heap limit. A wrapper (Nx, npm) exits 1 after it, so only the log names it.
const nodeHeapLimitError = "JavaScript heap out of memory"

// heapLimitFix completes a heap-limit crash's message: the fact read, why a
// plain restart repeats it, and the fix.
const heapLimitFix = "Its log ends with Node's heap-limit error: the process ran out of its own heap, not of the container's RAM. " +
	"Started unchanged, it hits the same limit. Raise the heap in the server's declared command (NODE_OPTIONS=--max-old-space-size=N, N in MB below the service's RAM) or make it load less; " +
	"when N would not fit, raise the service's RAM with zerops_scale. logTail shows whether the error is the server's or a child's."

// crashMatchLogLines is how many log lines a crash reply reads to recognise
// its cause: a FATAL line followed by a GC dump, a native stack and a
// wrapper's output sits beyond the 40 lines the reply returns.
const crashMatchLogLines = 80

// lastLogLines is the last n lines of a log tail, tail itself when it holds
// no more.
func lastLogLines(tail string, n int) string {
	body := strings.TrimSuffix(tail, "\n")
	cut := len(body)
	for range n {
		i := strings.LastIndexByte(body[:cut], '\n')
		if i < 0 {
			return tail
		}
		cut = i
	}
	return tail[cut+1:]
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
	if _, named := ds.State.Servers[p.Hostname]; p.Name == "" && !ds.DeclarationExists && len(ds.State.Servers) > 0 && !named {
		return statusSupervisedServers(ctx, ssh, p, ds)
	}
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
	probeWait := 2
	wait, bySupervisor := devServerWait(devServerActionStatus, p.WaitSeconds, ds.Supervisor)
	if bySupervisor {
		begun := time.Now()
		if err := waitDevServer(ctx, ssh, ds, name, wait); err != nil {
			return nil, err
		}
		server = ds.State.Servers[name]
		probeWait = max(2, wait-int(time.Since(begun).Seconds()))
		if server.Status == devsup.StatusRunning || (server.Status == devsup.StatusStarting && answerStarting(ds.Supervisor, server) == startingProbe) {
			ds.observeListeners(ctx, ssh)
		}
	}
	declared, isDeclared := ds.Server(name)
	result := newResult(devServerActionStatus, p.Hostname, name)
	result.LogFile, result.PID, result.Restarts = server.LogPath, server.Pid, server.Restarts
	result.LastRestartReason = server.LastRestartReason
	defer func() {
		if result.Restarts > 0 {
			result.Message += fmt.Sprintf(" The supervisor has restarted it %d times.", result.Restarts)
		}
		if result.LastRestartReason != "" {
			result.Message += fmt.Sprintf(" Last restart reason: %s.", result.LastRestartReason)
			if server.OutOfMemory {
				result.Message += " " + OutOfMemoryHint
			}
		}
	}()
	label := serverLabel(p.Hostname, name)
	switch server.Status {
	case devsup.StatusCrashed:
		if bySupervisor && server.RetryPending {
			applyStarting(ctx, ssh, result, name, server, startingWait, ds.Supervisor)
		} else {
			applyCrash(ctx, ssh, result, name, server)
			appendCrashWait(result, ds.Supervisor, server)
		}
		return result, nil
	case devsup.StatusStopped:
		result.Reason = reasonStopped
		result.Message = fmt.Sprintf("Dev server %s is stopped.", label)
		return result, nil
	case devsup.StatusStarting:
		// A server whose healthPath has not answered since its start is not
		// probed. One that answered since, or of a supervisor that cannot
		// say, is probed as a running one.
		if answer := answerStarting(ds.Supervisor, server); answer != startingProbe {
			result.Port = cmp.Or(p.Port, declared.Port, server.Port)
			if answer == startingStalled {
				result.LogTail = currentRun(supervisorLogTail(ctx, ssh, p.Hostname, name, defaultLogTailLines))
			}
			applyStarting(ctx, ssh, result, name, server, answer, ds.Supervisor)
			return result, nil
		}
	case devsup.StatusRunning:
	}
	port := cmp.Or(p.Port, declared.Port, server.Port)
	if port == 0 {
		result.Message = fmt.Sprintf("Dev server %s is %s%s", label, server.Status, unknownPortClause(ds, name, isDeclared, server))
		return result, nil
	}
	healthPath := probedPath(p, ds, name)
	result.Port, result.HealthPath = port, healthPath
	if ds.LoopbackOnly[port] {
		applyLoopbackOnly(result, name, port)
		return result, nil
	}
	// curl's exit follows the code: 28 is a listener that accepted and did
	// not answer within --max-time.
	out, err := ssh.ExecSSH(ctx, p.Hostname, fmt.Sprintf(
		"curl -s -o /dev/null -w '%%{http_code}' --max-time %d http://localhost:%d%s; echo \" $?\"", probeWait, port, healthPath))
	code, exit, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	switch {
	case err != nil:
		// ssh itself failed: the probe never ran, so nothing is known of
		// the port — never read as a server not listening.
		result.Message = fmt.Sprintf("Dev server %s is %s, but zcp could not run its HTTP probe on :%d (%s, class %s): this status says nothing of whether it answers.", label, server.Status, port, sshFailureText(err), cmp.Or(string(platform.ClassOf(err)), "unclassified"))
		return result, nil
	case (code == "" || code == "000") && exit == "28":
		result.Reason = reasonSlow
		result.Message = fmt.Sprintf("Dev server %s is %s and accepts connections on :%d, but sent no HTTP answer on %s within %ds — likely still compiling its first response; check again shortly.", label, server.Status, port, healthPath, probeWait)
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

// unknownPortClause ends the status of a running server no call, declaration
// or runtime entry gives a port: one declared or run without a port has none,
// any other has a port zcp does not know.
func unknownPortClause(ds *DevServers, name string, isDeclared bool, server devsup.ServerState) string {
	switch {
	case isDeclared:
		return " (declared without an HTTP port)."
	case server.Source == devsup.SourceRuntime:
		return " (run without an HTTP port)."
	case !ds.DeclarationExists:
		return fmt.Sprintf("; zcp does not know its port, since %s is missing.", devsup.DeclarationPath)
	default:
		return fmt.Sprintf("; zcp does not know its port, since %s does not declare %s.", devsup.DeclarationPath, name)
	}
}

// statusEveryServer is a status of each of the dev service's servers, one
// sentence each; it reports none of them alone.
func statusEveryServer(ctx context.Context, ssh SSHDeployer, p DevServerParams, ds *DevServers) (*DevServerResult, error) {
	servers := ds.Servers()
	lines := make([]string, 0, len(servers))
	for _, s := range servers {
		one := p
		one.Name, one.Port, one.HealthPath = s.Name, 0, ""
		one.WaitSeconds = 0
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

// statusSupervisedServers is a status of each server the supervisor runs on a
// dev service without a declaration file, and how to declare them again.
func statusSupervisedServers(ctx context.Context, ssh SSHDeployer, p DevServerParams, ds *DevServers) (*DevServerResult, error) {
	names := slices.Sorted(maps.Keys(ds.State.Servers))
	lines := make([]string, 0, len(names))
	for _, name := range names {
		one := p
		one.Name, one.Port, one.HealthPath = name, 0, ""
		one.WaitSeconds = 0
		r, err := statusDevServer(ctx, ssh, one, ds)
		if err != nil {
			return nil, err
		}
		lines = append(lines, r.Message)
	}
	count := "1 dev server"
	if len(names) > 1 {
		count = fmt.Sprintf("%d dev servers", len(names))
	}
	result := newResult(devServerActionStatus, p.Hostname, p.Hostname)
	result.Unreported = true
	result.Message = fmt.Sprintf("%s has no %s; its supervisor runs %s:\n%s\n%s",
		p.Hostname, devsup.DeclarationPath, count, strings.Join(lines, "\n"), declareHint(p.Hostname))
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
		lines = logsActionTailLines
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
// open connection (curl exit 28) answers SLOW. The first attempt runs even
// when a stalled container spent the wait before it, so the verdict always
// comes from the listener, never from the clock alone. Returns probe output,
// locally-measured elapsed, a deadline-hit flag, and the ssh error
// (convention: error last).
func runHealthProbe(parent context.Context, ssh SSHDeployer, hostname string, port int, healthPath string, wait int) ([]byte, int64, bool, error) {
	maxAttempts := (wait*1000)/pollIntervalMillis + 1
	probe := fmt.Sprintf(
		`set +e; start_ns=$(date +%%s%%N); end=$(( $(date +%%s) + %d )); code=000; for i in $(seq 1 %d); do `+
			`left=$(( end - $(date +%%s) )); if [ "$left" -le 0 ]; then [ "$i" -eq 1 ] || break; left=1; fi; `+
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

// runHealthProbeOnce makes the single health request after a supervisor wait.
func runHealthProbeOnce(parent context.Context, ssh SSHDeployer, hostname string, port int, healthPath string, wait int) (string, bool, error) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(wait)*time.Second+probeTimeoutSlack)
	defer cancel()
	cmd := fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' --max-time %d http://localhost:%d%s; echo \" $?\"", wait, port, healthPath)
	out, err := ssh.ExecSSH(ctx, hostname, cmd)
	code, exit, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if code == "" {
		return "", ctx.Err() == context.DeadlineExceeded, err
	}
	var httpCode int
	_, _ = fmt.Sscanf(code, "%d", &httpCode)
	switch {
	case healthyHTTP(httpCode):
		return "OK " + code, false, err
	case exit == "28":
		return "SLOW " + code, false, err
	default:
		return "FAIL " + code, false, err
	}
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
	if status == devsup.StatusStarting {
		result.Message += " " + RecheckSentence(result.Hostname, name, []string{name})
	}
}

// startingAnswer is how a reply reads a server the supervisor reports
// starting (answerStarting).
type startingAnswer int

const (
	// startingProbe: probe it as a running server — its healthPath answered
	// since this start and its port refuses now, or its supervisor reports no
	// startPhase.
	startingProbe startingAnswer = iota
	// startingWait: its start is under way or its healthPath is still awaited.
	startingWait
	// startingStalled: its listen wait ran out with its healthPath never
	// answering.
	startingStalled
)

// answerStarting decides how a reply reads a server the supervisor reports
// starting, from the supervisor's own startPhase alone: the supervisor owns
// the start, the port wait and its clock (devsup.CapabilityStartPhase).
func answerStarting(v SupervisorVersion, server devsup.ServerState) startingAnswer {
	if !v.Supports(devsup.CapabilityStartPhase) {
		return startingProbe
	}
	switch server.StartPhase {
	case devsup.StartLaunching, devsup.StartAwaitingPort:
		return startingWait
	case devsup.StartStalled:
		return startingStalled
	case devsup.StartListened:
		return startingProbe
	default:
		return startingProbe
	}
}

// applyStarting reports a server answerStarting reads as answer: still
// starting, with when to read it again, or a stalled start to fix and start
// again. A server whose port is awaited while its process group listens on
// other ports only is named so (port_elsewhere).
func applyStarting(ctx context.Context, ssh SSHDeployer, result *DevServerResult, name string, server devsup.ServerState, answer startingAnswer, v SupervisorVersion) {
	if server.RetryPending {
		// A timed-out wait can find a crash with one more start still to
		// come. Keep its exit and log while reporting the unsettled start.
		applyCrash(ctx, ssh, result, name, server)
		result.Reason = reasonStarting // the retry is still awaited
		appendCrashWait(result, v, server)
		return
	}
	label := serverLabel(result.Hostname, name)
	recheck := RecheckSentence(result.Hostname, name, []string{name})
	if answer != startingStalled && v.Supports(devsup.CapabilityWait) {
		recheck += " " + waitSentence()
	}
	if result.Port > 0 && (server.StartPhase == devsup.StartAwaitingPort || answer == startingStalled) {
		if others := groupListenPorts(ctx, ssh, result.Hostname, server.Pgid); len(others) > 0 && !slices.Contains(others, result.Port) {
			result.Reason = reasonPortElsewhere
			result.Message = fmt.Sprintf("Dev server %s has not answered on its port :%d, while its process listens on %s. If that is the server, make its declared port and its command's port the same, then restart it; if it is a side port (an inspector, a separate HMR socket) and the server still compiles, it is still starting. %s",
				label, result.Port, portList(others), recheck)
			if answer == startingStalled {
				result.Message = fmt.Sprintf("Dev server %s never answered on its port :%d within the supervisor's listen wait (%d minutes), while its process listens on %s: make its declared port and its command's port the same, then restart it.",
					label, result.Port, int(devsup.ListenWait.Minutes()), portList(others))
			}
			return
		}
	}
	if answer == startingStalled {
		result.Reason = reasonStartStalled
		result.Message = fmt.Sprintf("Dev server %s never answered on its port within the supervisor's listen wait (%d minutes): its process lives and its port does not accept. Read logTail, fix it, then restart it.",
			label, int(devsup.ListenWait.Minutes()))
		return
	}
	result.Reason = reasonStarting
	result.Message = fmt.Sprintf("Dev server %s is still starting: its port has not answered since its start, which is no failed start; the supervisor reports it starting until it does, for at most %d minutes. %s",
		label, int(devsup.ListenWait.Minutes()), recheck)
}

func waitSentence() string {
	return "Add waitSeconds (up to 300) to that status call to wait there; it answers as soon as the supervisor reports the server running, crashed, stopped or stalled."
}

// portList names ports as ":5174, :9229".
func portList(ports []int) string {
	named := make([]string, len(ports))
	for i, p := range ports {
		named[i] = ":" + strconv.Itoa(p)
	}
	return strings.Join(named, ", ")
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
// serves no HTTP or its address is not known; a lookup of the dev service that
// failed is its error. Ask it only while the service's subdomain is on.
func DevServerSubdomainURL(ctx context.Context, client platform.Client, projectID string, result *DevServerResult) (string, error) {
	if result.Pool != "" {
		// A lease is reached on its preview port, at the pool's subdomain.
		return leasePreviewURL(ctx, client, projectID, result.Pool, result.PreviewPort, false), nil
	}
	if result.Port == 0 {
		return "", nil
	}
	svc, err := LookupService(ctx, client, projectID, result.Hostname)
	if err != nil {
		return "", fmt.Errorf("look up %s for its subdomain address: %w", result.Hostname, err)
	}
	if !slices.ContainsFunc(svc.Ports, func(p platform.Port) bool { return p.Port == result.Port && (p.HTTPSupport || isHTTPScheme(p)) }) {
		return "", nil
	}
	return SubdomainURLForPort(ctx, client, projectID, svc, result.Port), nil
}

// pollIntervalAsShellDuration returns the pollIntervalMillis as a shell
// sleep argument ("0.500" for 500ms).
func pollIntervalAsShellDuration() string {
	return fmt.Sprintf("%d.%03d", pollIntervalMillis/1000, pollIntervalMillis%1000)
}
