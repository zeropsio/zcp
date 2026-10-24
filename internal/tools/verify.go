package tools

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// VerifyInput is the input type for zerops_verify.
type VerifyInput struct {
	ServiceHostname string `json:"serviceHostname,omitempty" jsonschema:"Hostname of the service to verify. Omit to verify all services."`
}

// RegisterVerify registers the zerops_verify tool. ssh is used ONLY to
// freshen the single-hostname path's DeferredStart classification against
// a live dev-server liveness read (ops.DevServerRunning, §8 O3 PA-4) — nil
// is accepted (local-only mode has no SSH deployer) and degrades to the
// static (mode, class) classification.
// VerifyOption configures RegisterVerify.
type VerifyOption func(*verifyConfig)

type verifyConfig struct {
	swarm ops.SwarmFactory
	// vouch replaces the orchestrator's answer for a lease (tests).
	vouch leaseVouch
}

// WithPoolOrchestrators lets verify of a pool, in a container, also ask the
// pool's orchestrator (ops.PoolOrchestratorCheck) through swarm. Without it
// a pool is verified by service_running alone.
func WithPoolOrchestrators(swarm ops.SwarmFactory) VerifyOption {
	return func(c *verifyConfig) { c.swarm = swarm }
}

func RegisterVerify(srv *mcp.Server, client platform.Client, fetcher platform.LogFetcher, projectID, stateDir string, rt runtime.Info, ssh ops.SSHDeployer, opts ...VerifyOption) {
	var cfg verifyConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	vouch := cfg.vouch
	if vouch == nil {
		vouch = orchestratorVouch(client, projectID, stateDir, cfg.swarm)
	}
	// checkPool adds the orchestrator check to a pool's result; the
	// orchestrator answers only inside the project.
	checkPool := func(ctx context.Context, r *ops.VerifyResult) {
		if cfg.swarm == nil || !rt.InContainer || r == nil || r.Type != string(topology.RuntimePool) {
			return
		}
		r.AddCheck(ops.PoolOrchestratorCheck(ctx, ops.LeaseDeps{Client: client, ProjectID: projectID, StateDir: stateDir, Swarm: cfg.swarm}, r.Hostname))
	}
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_verify",
		Description: "Run health checks on a service. Returns structured results: service status, error logs, startup detection, HTTP connectivity. Check statuses: pass, fail, skip, info (advisory, not failure). Omit serviceHostname to verify all services.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Verify service health",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input VerifyInput) (*mcp.CallToolResult, any, error) {
		if input.ServiceHostname == "" {
			result, err := ops.VerifyAllWithMeta(ctx, client, fetcher, httpClient, projectID, runtimeMetaResolver(stateDir), publicAccessResolver(stateDir))
			if err != nil {
				return convertError(err, WithRecoveryStatus()), nil, nil
			}
			for i := range result.Services {
				checkPool(ctx, &result.Services[i])
			}
			recordVerifyAllToWorkSession(stateDir, result)
			var notes []string
			for i := range result.Services {
				if note := partsWithoutDevServerNote(ctx, ssh, rt, stateDir, result.Services[i].Hostname, &result.Services[i]); note != "" {
					notes = append(notes, note)
				}
			}
			return jsonResult(verifyAllResponse{
				VerifyAllResult:  result,
				Note:             strings.Join(notes, " "),
				WorkSessionState: sessionAnnotations(ctx, httpClient, stateDir),
				Envelope:         freshEnvelope(ctx, stateDir, client, projectID, rt),
			}), nil, nil
		}
		// A lease alias names a pool container, not a service: it is read
		// over SSH and never resolved as a service or a meta (DP-1, PS-3).
		// It is asked of its orchestrator first (DP-2): one that is no longer
		// the lease's — maybe another holder's — or that the orchestrator does
		// not answer about is never probed, and never called healthy.
		if lease, ok, err := checkedLeaseOf(ctx, stateDir, vouch, input.ServiceHostname, "verified"); ok {
			if err != nil {
				return convertError(err, WithRecoveryStatus()), nil, nil
			}
			return verifyLease(ctx, client, httpClient, ssh, projectID, stateDir, rt, lease), nil, nil
		}
		// Phase 4: if the input hostname is a push source whose builds land
		// on a different runtime (dev half of a standard pair under
		// git-push), verify against the BUILD TARGET instead. Self/cross
		// (auto close-mode) modes resolve to self → no redirect.
		// Recorded under the build-target hostname so the work session's
		// auto-close gate (which evaluates resolved targets after Phase 2)
		// sees the green tick on the right key.
		// A build target that never ran a deploy has nothing to check yet
		// (a new Mate's stage waits READY_TO_DEPLOY until its first build):
		// the service asked for is verified instead.
		host := input.ServiceHostname
		var redirectedFrom string
		if buildHost, _ := resolveBuildTargetForHost(stateDir, host); buildHost != "" && buildHost != host && runsDeploy(ctx, client, projectID, buildHost) {
			redirectedFrom = host
			host = buildHost
		}
		result, err := ops.VerifyWithMeta(ctx, client, fetcher, httpClient, projectID, host, runtimeMetaForHost(stateDir, host), publicAccessInputForHost(ctx, client, ssh, projectID, stateDir, host))
		if err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}
		checkPool(ctx, result)
		recordVerifyToWorkSession(stateDir, result)
		resp := verifyResponse{
			VerifyResult:     result,
			WorkSessionState: sessionAnnotations(ctx, httpClient, stateDir),
			Envelope:         freshEnvelope(ctx, stateDir, client, projectID, rt),
		}
		if redirectedFrom != "" {
			resp.Note = fmt.Sprintf("verify: checked %q, not %q: %q is a push source and %q is its build target (git-push standard pair builds on stage). Pass the build-target hostname directly next time.", host, redirectedFrom, redirectedFrom, host)
		}
		// RC-A′: a passing verify on a deferred-start (dev-mode dynamic)
		// service reflects a LIVE dev-server process, not the app's own
		// start — kept across restarts and redeploys, down after a crash.
		// Annotate so the agent does not report it as an always-on service
		// (the e2 failure).
		for _, note := range []string{deferredStartDurabilityNote(stateDir, host, result), partsWithoutDevServerNote(ctx, ssh, rt, stateDir, host, result)} {
			if note == "" {
				continue
			}
			if resp.Note != "" {
				resp.Note += " "
			}
			resp.Note += note
		}
		return jsonResult(resp), nil, nil
	})
}

// leaseProbeTimeout bounds `ssh <alias> true`: a container that does not
// answer is unreachable, not a verify that hangs.
const leaseProbeTimeout = 20 * time.Second

// verifyLease answers zerops_verify for a lease alias: whether the container
// answers over SSH, and the dev servers its supervisor reports. Nothing is
// recorded to the work session — a lease is no service of the part.
func verifyLease(ctx context.Context, client platform.Client, httpClient ops.HTTPDoer, ssh ops.SSHDeployer, projectID, stateDir string, rt runtime.Info, lease ops.LeaseTargetInfo) *mcp.CallToolResult {
	if ssh == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("%s is a lease: it is verified over SSH, and this zcp has no SSH access to it", lease.Alias),
			"Run zerops_verify from the zcp container of the project that holds the lease.",
		), WithRecoveryStatus())
	}
	unreachable := ""
	if lease.Parked {
		// Nothing answers on a parked lease (DP-28): no probe to wait out.
		unreachable = fmt.Sprintf("lease %s is parked: its container is stopped; %s starts it", lease.Alias, ops.LeaseUnparkCall(lease.Alias))
	} else {
		probeCtx, cancel := context.WithTimeout(ctx, leaseProbeTimeout)
		if _, err := ssh.ExecSSH(probeCtx, lease.Alias, "true"); err != nil {
			unreachable = withSSHStderr("ssh "+lease.Alias, err)
		}
		cancel()
	}
	result := ops.VerifyLease(ctx, ssh, lease, unreachable)
	return jsonResult(verifyResponse{
		VerifyResult:     result,
		WorkSessionState: sessionAnnotations(ctx, httpClient, stateDir),
		Envelope:         freshEnvelope(ctx, stateDir, client, projectID, rt),
	})
}

// runsDeploy reports a service that runs a deploy, read lag-free; false when
// the read fails, so a verify never moves to a target it cannot see.
func runsDeploy(ctx context.Context, client platform.Client, projectID, hostname string) bool {
	services, err := client.ListServicesDirect(ctx, projectID)
	if err != nil {
		return false
	}
	for i := range services {
		if services[i].Name == hostname {
			return services[i].HasDeployedCode()
		}
	}
	return false
}

// verifyResponse wraps ops.VerifyResult with the structured
// WorkSessionState lifecycle signal (F5 closure). Surfacing the
// session state turns verify from a pure HTTP probe into an observable
// lifecycle event — the agent sees whether this call landed inside an
// open session, on an auto-closed one, or with no session tracking.
//
// Note carries any user-facing hint about the verify call's routing —
// Phase 4 uses it to surface that the input hostname was redirected to
// the build target (e.g. dev → stage under git-push) so the agent learns
// the right hostname for next time.
type verifyResponse struct {
	*ops.VerifyResult
	Note             string            `json:"note,omitempty"`
	WorkSessionState *WorkSessionState `json:"workSessionState,omitempty"`
	// Envelope is the post-mutation lifecycle state (docs/spec-mate.md §1.3).
	// Absent when its computation failed — the rest of the response is
	// unaffected.
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// verifyAllResponse mirrors verifyResponse for the multi-service VerifyAll
// path. Each service's attempt is already recorded; one session-state
// snapshot at the response root reflects the whole scope.
type verifyAllResponse struct {
	*ops.VerifyAllResult
	Note             string            `json:"note,omitempty"`
	WorkSessionState *WorkSessionState `json:"workSessionState,omitempty"`
	// Envelope is the post-mutation lifecycle state (docs/spec-mate.md §1.3).
	// Absent when its computation failed — the rest of the response is
	// unaffected.
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

func runtimeMetaResolver(stateDir string) ops.RuntimeMetaResolver {
	if stateDir == "" {
		return nil
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return nil
	}
	idx := workflow.ManagedRuntimeIndex(metas)
	return func(hostname string) ops.RuntimeMeta {
		return runtimeMetaFromServiceMeta(idx[hostname], hostname)
	}
}

func runtimeMetaForHost(stateDir, host string) ops.RuntimeMeta {
	if stateDir == "" || host == "" {
		return ops.RuntimeMeta{}
	}
	meta, err := workflow.FindServiceMeta(stateDir, host)
	if err != nil {
		return ops.RuntimeMeta{}
	}
	return runtimeMetaFromServiceMeta(meta, host)
}

func runtimeMetaFromServiceMeta(meta *workflow.ServiceMeta, host string) ops.RuntimeMeta {
	serves := meta.ServesHTTPFor(host)
	if serves == nil {
		return ops.RuntimeMeta{}
	}
	return ops.RuntimeMeta{
		ServesHTTP: *serves,
		Recorded:   true,
		Setup:      meta.SetupNameFor(host),
	}
}

// defaultPublicAccessInput is what a meta-less caller passes: auto intent,
// not deferred-start — mirrors ops.defaultPublicAccessInput (unexported
// there; tools has no access to it and constructs the equivalent directly).
func defaultPublicAccessInput() ops.PublicAccessInput {
	return ops.PublicAccessInput{Record: topology.PublicAccessRecord{Intent: topology.PublicAccessAuto}}
}

// publicAccessInputForHost builds the ops.PublicAccessInput for the
// single-hostname verify path (§8 O3 PA-4) from host's persisted ServiceMeta
// record plus a fresh (mode, runtime-class) DeferredStart classification —
// mirroring tools/subdomain.go's skipDeferredStartProbe pattern (one extra
// ops.LookupService call to get the live TypeVersion needed for
// topology.RuntimeClassFor; ops itself has no Mode of its own to compute
// this internally). A lookup failure degrades to DeferredStart=false (probe
// runs) rather than blocking verify on an unrelated read.
//
// A statically deferred-start classification (dev-mode dynamic runtime) is
// then freshened against the dev-server supervisor (§8 O4): a declared server
// with a port it runs means the runtime DOES have a listener, so DeferredStart
// flips to false and http_internal probes
// for real instead of skipping with the "start it with zerops_dev_server"
// message. A dev service that declares several servers with a port has each
// checked (ops.PublicAccessInput.DevPorts, TEST-PLAN E1 #11). An SSH error
// (no deployer, unreachable container) falls back to
// the static classification — verify must never fail because the listener
// probe itself failed.
func publicAccessInputForHost(ctx context.Context, client platform.Client, ssh ops.SSHDeployer, projectID, stateDir, host string) ops.PublicAccessInput {
	if stateDir == "" || host == "" {
		return defaultPublicAccessInput()
	}
	meta, err := workflow.FindServiceMeta(stateDir, host)
	if err != nil || meta == nil {
		return defaultPublicAccessInput()
	}
	devServer := servedByDevServer(meta, host)
	deferredStart := false
	var devPorts []ops.DevPort
	if svc, lookupErr := ops.LookupService(ctx, client, projectID, host); lookupErr == nil && svc != nil {
		class := topology.RuntimeClassFor(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName)
		deferredStart = topology.IsDeferredStart(meta.ModeFor(host), class)
		if deferredStart && ssh != nil {
			if ds, readErr := ops.ReadDevServers(ctx, ssh, host, meta.WorkingDirFor(host)); readErr == nil {
				deferredStart = !ds.Listening()
				devPorts = ds.DevPorts()
			}
		}
	}
	return ops.PublicAccessInput{Record: meta.PublicAccessFor(host), DeferredStart: deferredStart, DevServer: devServer, DevPorts: devPorts}
}

// servedByDevServer reports a host whose mode leaves a dynamic runtime to a
// dev server (topology.IsDeferredStart's mode half) — whatever that server's
// live state; ops gives its HTTP probes a page's first compile when the
// runtime is dynamic.
func servedByDevServer(meta *workflow.ServiceMeta, host string) bool {
	return topology.IsDeferredStart(meta.ModeFor(host), topology.RuntimeDynamic)
}

// publicAccessResolver builds a per-hostname ops.PublicAccessResolver for
// the all-services verify path from ServiceMeta's persisted intent only —
// DeferredStart is left at its default (false) here rather than paying one
// ops.LookupService per service (VerifyAll already reads every service once
// internally; duplicating that N times in the tool layer would defeat the
// "single ListServices call" property VerifyAll is pinned on). A deferred-
// start dev runtime scanned via the all-services path therefore reads
// http_internal as a real probe (not skipped) until it's verified by
// hostname directly, where the DeferredStart classification is exact.
func publicAccessResolver(stateDir string) ops.PublicAccessResolver {
	if stateDir == "" {
		return nil
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return nil
	}
	idx := workflow.ManagedRuntimeIndex(metas)
	return func(hostname string) ops.PublicAccessInput {
		meta := idx[hostname]
		if meta == nil {
			return defaultPublicAccessInput()
		}
		return ops.PublicAccessInput{Record: meta.PublicAccessFor(hostname), DevServer: servedByDevServer(meta, hostname)}
	}
}

// deferredStartDurabilityNote returns a durability caveat when a passing
// verify reflects a deferred-start runtime — a dev-mode dynamic service whose
// app runs ONLY via the zerops_dev_server process (RC-A′). For those, an HTTP
// 200 is a live dev process, not the app's own start: the dev service's
// supervisor starts a declared server again after a restart or redeploy
// (spec-workflows §8 O4), but a crash leaves the URL down. Returns "" for non-deferred or
// failing verifies (the latter already carry their own failure detail).
// Pure over stable (mode, class) — no liveness read.
func deferredStartDurabilityNote(stateDir, host string, result *ops.VerifyResult) string {
	if result == nil || result.Status != ops.StatusHealthy {
		return ""
	}
	meta, err := workflow.FindServiceMeta(stateDir, host)
	if err != nil || meta == nil {
		return ""
	}
	class := topology.RuntimeClassFor(result.TypeVersion)
	if !topology.IsDeferredStart(meta.ModeFor(host), class) {
		return ""
	}
	return fmt.Sprintf("Durability: %q is dev-mode — this 200 is served by the zerops_dev_server process, not the app's own start. Its supervisor starts the dev server again after a restart or redeploy, but a crash leaves the URL down until you start it again. For an always-on service switch to simple mode; this is not a production deployment.", host)
}

// partsWithoutDevServerNote states, on a verify of a container dev service
// whose pair's stages build several parts, each part that runs a process but
// has no dev server declared on it (ops.PartsSignal.PartsWithoutDevServer),
// with the call that starts it. Develop can close with a part's stage deployed
// and verified while the dev service never runs that part; every auto-close
// passes a verify of the dev service, so the fact lands where the agent checks
// that dev works. A pair with one stage, local mode or a failed read: "" and
// no read.
func partsWithoutDevServerNote(ctx context.Context, ssh ops.SSHDeployer, rt runtime.Info, stateDir, host string, result *ops.VerifyResult) string {
	if !rt.InContainer || ssh == nil || result == nil {
		return ""
	}
	meta, err := workflow.FindServiceMeta(stateDir, host)
	if err != nil || meta == nil || meta.Hostname != host || len(meta.Stages) < 2 ||
		!topology.IsDeferredStart(meta.ModeFor(host), topology.RuntimeClassFor(result.TypeVersion)) {
		return ""
	}
	sig, err := ops.ReadPartsSignal(ctx, ssh, host, meta.PrimarySetupName)
	if err != nil {
		return ""
	}
	missing := sig.PartsWithoutDevServer(meta.Stages, host)
	if len(missing) == 0 {
		return ""
	}
	calls := make([]string, 0, len(missing))
	for _, part := range missing {
		calls = append(calls, fmt.Sprintf(`zerops_dev_server action=start hostname=%q name=%q command="<its dev command>"`, host, part))
	}
	subject := fmt.Sprintf("part %q runs", missing[0])
	if len(missing) > 1 {
		subject = "parts " + quoteAll(missing) + " run"
	}
	return fmt.Sprintf("Dev servers: %s's %s no dev server on %q — none is declared under its name. Start it (port=<its port> when it listens, else noHttpProbe=true): %s.",
		sig.ConfigFile, subject, host, strings.Join(calls, "; "))
}

// recordVerifyToWorkSession records one service verify result as a WorkSession attempt.
// Pass = summary "healthy" status; fail = first failing check detail.
//
// On failure the FailureClass is populated from the failing check's name —
// `service_running` → FailureClassStart (container not up); HTTP-shape
// checks → FailureClassVerify; everything else → FailureClassVerify as
// the catch-all for verify-time failures.
func recordVerifyToWorkSession(stateDir string, r *ops.VerifyResult) {
	if r == nil {
		return
	}
	attempt := workflow.VerifyAttempt{
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
	}
	// Auto-close counts a verify as passed when the service is operationally
	// up — healthy, OR degraded only by a cosmetic http_root 4xx (REST API
	// with no `/` route). The verify RESPONSE still reports the degraded
	// status; this only governs the lifecycle gate so a 404-at-/ no longer
	// forces a throwaway root route to auto-close. See PassedForLifecycle.
	passed := r.PassedForLifecycle()
	if passed {
		attempt.Passed = true
		attempt.PassedAt = attempt.AttemptedAt
		if r.Status == statusHealthy {
			attempt.Summary = statusHealthy
		} else {
			attempt.Summary = "reachable (root path not 2xx — accepted as cosmetic)"
		}
	} else {
		attempt.Summary = verifyFailureSummary(r)
		attempt.FailureClass = classifyVerifyFailure(r)
	}
	_ = workflow.RecordVerifyAttempt(stateDir, r.Hostname, attempt)
	if attempt.Passed {
		// Durable verified-setup evidence (F4): the session's Verifies map
		// dies with the session; this sidecar survives so the dev→prod
		// transition can ask "was this setup ever green-verified, when".
		if meta, _ := workflow.FindServiceMeta(stateDir, r.Hostname); meta != nil {
			if setup := meta.SetupNameFor(r.Hostname); setup != "" {
				_ = workflow.RecordVerifiedSetup(stateDir, r.Hostname, workflow.VerifiedSetupEvidence{
					SetupName:      setup,
					TargetHostname: r.Hostname,
					VerifiedAt:     attempt.PassedAt,
					Summary:        attempt.Summary,
				})
			}
		}
	}
}

// classifyVerifyFailure maps the first failing check name to the
// FailureClass that best describes the recovery path. service_running =
// container start issue (deploy may need redo). HTTP-shape checks fall
// under FailureClassVerify (route / app / config). Anything else is
// FailureClassVerify as a coarse catch-all — the Reason text still
// carries the specific check name + detail.
func classifyVerifyFailure(r *ops.VerifyResult) topology.FailureClass {
	for _, c := range r.Checks {
		if c.Status != statusFail {
			continue
		}
		switch c.Name {
		case "service_running":
			return topology.FailureClassStart
		default:
			return topology.FailureClassVerify
		}
	}
	return topology.FailureClassVerify
}

// recordVerifyAllToWorkSession records results for every service verified
// by VerifyAll. Each service appends one attempt.
func recordVerifyAllToWorkSession(stateDir string, r *ops.VerifyAllResult) {
	if r == nil {
		return
	}
	for i := range r.Services {
		recordVerifyToWorkSession(stateDir, &r.Services[i])
	}
}

func verifyFailureSummary(r *ops.VerifyResult) string {
	for _, c := range r.Checks {
		if c.Status == statusFail {
			if c.Detail != "" {
				return fmt.Sprintf("%s: %s", c.Name, c.Detail)
			}
			return c.Name
		}
	}
	return r.Status
}
