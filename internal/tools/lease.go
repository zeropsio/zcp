package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	leaseActionAcquire = "acquire"
	leaseActionStatus  = "status"
	leaseActionRelease = "release"
	leaseActionRollout = "rollout"
	leaseActionSync    = "sync"
	leaseActionPark    = "park"
	leaseActionUnpark  = "unpark"
)

// LeaseInput is the input type for zerops_lease.
type LeaseInput struct {
	Action string `json:"action"`
	Pool   string `json:"pool,omitempty"`
	// Pools is a group acquire: one lease per pool on the branch (DP-17).
	Pools  []string `json:"pools,omitempty"`
	Branch string   `json:"branch,omitempty"`
	// From is the branch an acquire cuts a branch new to origin from (DP-6).
	From  string    `json:"from,omitempty"`
	Repo  string    `json:"repo,omitempty"`
	Lease string    `json:"lease,omitempty"`
	Force *FlexBool `json:"force,omitempty"`
}

// leaseInputSchema is explicit because force is a FlexBool (an agent that
// stringifies booleans must not be bounced by the schema).
func leaseInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"action": {Type: "string", Description: "acquire, status, release, rollout, sync, park or unpark."},
		"pool":   {Type: "string", Description: "Pool service hostname. acquire (or pools), rollout: required. status: optional (default every pool)."},
		"pools":  {Type: "array", Items: &jsonschema.Schema{Type: "string"}, Description: "acquire: one lease per pool on the branch, all or none."},
		"branch": {Type: "string", Description: "acquire: branch to check out (default main); a new one is cut from main. release: every lease of it, all or none."},
		"from":   {Type: "string", Description: "acquire: cut a new branch from this one instead."},
		"repo":   {Type: "string", Description: "acquire: https clone URL; the first one on a pool becomes its default. release: the branch's, if several."},
		"lease":  {Type: "string", Description: "release, sync, park, unpark: the lease alias <pool>-<number> from acquire or status (not a secret)."},
		"force":  flexBoolSchema("release: discard unpushed commits and uncommitted files (default false: refused while any exist)."),
	}, "action")
}

// leaseDeps wires the lease operations: with a mounter (container mode) a
// lease gets its ssh alias and mount (ops.NewLeaseHost); without one the host
// side does nothing. The HQ a repository is recognised on is the container's
// (registerLease sets it from the runtime). Each pool keeps
// ops.LeaseWarmTarget warm containers (DP-27).
func leaseDeps(client platform.Client, projectID, stateDir string, swarm ops.SwarmFactory, mounter ops.Mounter, ssh ops.SSHDeployer) ops.LeaseDeps {
	deps := ops.LeaseDeps{Client: client, ProjectID: projectID, StateDir: stateDir, Swarm: swarm, SSH: ssh,
		Warm: &ops.LeaseWarm{Target: ops.LeaseWarmTarget}}
	if mounter != nil {
		deps.Host = ops.NewLeaseHost(mounter)
	}
	return deps
}

// pairKeptPoolCredential answers, for a pool, the record of the HQ credential
// it holds that a pair keyed by the pool keeps — the pool service the pair
// records and its credential fingerprint — once the repository pass took the
// acquire's per-pool record over (takeOverPoolCredential, DP-16); false when
// no pair keyed by the pool keeps one.
func pairKeptPoolCredential(stateDir string) func(pool string) (string, string, bool) {
	return func(pool string) (string, string, bool) {
		m, err := workflow.ReadServiceMeta(stateDir, pool)
		if err != nil || !m.DevIsPool() || m.DevPool.ServiceID == "" || m.HQ == nil || m.HQ.CredentialFingerprint == "" {
			return "", "", false
		}
		return m.DevPool.ServiceID, m.HQ.CredentialFingerprint, true
	}
}

// leaseToolResult is what zerops_lease returns. It never carries the lease
// string itself.
type leaseToolResult struct {
	Action string          `json:"action"`
	Leases []ops.LeaseView `json:"leases"`
	// Rewired are the other leases of the branch an acquire or a release
	// handed the recomputed sibling overlay (DP-26).
	Rewired []ops.LeaseRewire `json:"rewired,omitempty"`
	// UnreadPools are the pools a status of every pool could not read, each
	// with why; their leases are listed from zcp's records, unverified.
	UnreadPools []ops.LeasePoolUnread `json:"unreadPools,omitempty"`
	// Warm is each pool's warm containers on a status (DP-27).
	Warm []ops.LeasePoolWarm `json:"warm,omitempty"`
	// PoolNotices are what a status says of a pool as a whole: one zcp
	// leases from whose maxContainers runs past its preview ports (PS-17).
	PoolNotices []ops.LeasePoolNotice   `json:"poolNotices,omitempty"`
	NextActions string                  `json:"nextActions,omitempty"`
	Envelope    *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// leaseRolloutToolResult is what zerops_lease action=rollout returns.
type leaseRolloutToolResult struct {
	Action   string                  `json:"action"`
	Rollout  ops.LeaseRolloutView    `json:"rollout"`
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// RegisterLease registers the zerops_lease tool: acquire, status and release
// of a container of a Swarm pool, park and unpark of a lease (DP-28), and
// rollout of the pool, through the pool's orchestrator, and sync of a lease's
// branch to the branch's other leases (DP-24). swarm
// builds the orchestrator client (tests inject a fake or a stub transport).
// mounter (container mode; nil elsewhere) gives a lease its ssh alias and
// mount; ssh runs git inside the leased container (nil: no checkout, no
// release guard and no sync). Every successful result carries the lifecycle envelope,
// computed after the operation so it lists the new lease set (spec-mate §1.3);
// rt is the runtime that envelope is computed for.
func RegisterLease(srv *mcp.Server, client platform.Client, projectID, stateDir string, swarm ops.SwarmFactory, mounter ops.Mounter, ssh ops.SSHDeployer, rt runtime.Info, opts ...LeaseOption) {
	setup := leaseSetup{deps: leaseDeps(client, projectID, stateDir, swarm, mounter, ssh)}
	for _, opt := range opts {
		opt(&setup)
	}
	registerLease(srv, setup.deps, rt, setup.hq)
}

// leaseSetup is what LeaseOptions configure: the lease operations' deps, and
// the HTTP client a status reads this Mate's HQ with (nil: never).
type leaseSetup struct {
	deps ops.LeaseDeps
	hq   ops.HTTPDoer
}

// LeaseOption configures RegisterLease.
type LeaseOption func(*leaseSetup)

// WithLeaseBackground runs the warm policy's background work — the creates
// and clones a call starts and returns without waiting for (DP-27, DP-32) —
// through spawn, which must run it on a goroutine of its own, so its owner
// can wait for it (server.Server.WaitBackground).
func WithLeaseBackground(spawn func(func())) LeaseOption {
	return func(s *leaseSetup) {
		if s.deps.Warm != nil {
			s.deps.Warm.Go = spawn
		}
	}
}

// WithLeaseHQ lets a status read the Mate's own state in HQ through
// httpClient, once, for each lease group's change (leaseGroupChanges): inside
// a Mate only. Without it a status names them from the ledger, as last read.
func WithLeaseHQ(httpClient ops.HTTPDoer) LeaseOption {
	return func(s *leaseSetup) { s.hq = httpClient }
}

// registerLease is RegisterLease over deps leaseDeps wired, and hqHTTP the
// client a status reads this Mate's HQ with (nil: never). The warm policy's
// creates and clones run in the background and outlive the call that started
// them (DP-27, DP-32); tests set deps.Warm.Go to wait for them.
func registerLease(srv *mcp.Server, deps ops.LeaseDeps, rt runtime.Info, hqHTTP ops.HTTPDoer) {
	deps = leaseDepsIn(deps, rt)
	client, projectID, stateDir, swarm := deps.Client, deps.ProjectID, deps.StateDir, deps.Swarm
	// An acquire that starts a lease's dev servers runs the public-access
	// step zerops_dev_server's start runs, on the lease's pool (§8 O3).
	deps.PublicAccess = leasePublicAccess(client, projectID, stateDir)
	// envelope is the result's lifecycle envelope; read are the views this
	// call read checkouts for, whose activity and unsaved work ride leases[]
	// (the envelope's own lease reader never reads a checkout).
	envelope := func(ctx context.Context, read ...ops.LeaseView) *workflow.StateEnvelope {
		extra := []workflow.EnvelopeOption{workflow.WithLeasesAlwaysRead()}
		if swarm != nil {
			extra = append(extra, workflow.WithLeaseReader(leaseReaderWith(client, projectID, stateDir, swarm, deps.Host, leaseStatusTimeout)))
		}
		if reads := leaseReads(read); len(reads) > 0 {
			extra = append(extra, workflow.WithLeaseActivity(reads))
		}
		return freshEnvelope(ctx, stateDir, client, projectID, rt, extra...)
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_lease",
		Description: "Lease a Swarm pool (swarm@1) container per branch. acquire checks the branch out into /var/www of a clean container <pool>-<number>; " +
			"status lists leases; release resets or removes it, refusing on unpushed or uncommitted work unless force=true; " +
			"sync pushes a lease's commits to the branch's other leases; park/unpark stops/starts it, keeping its files; rollout replaces free outdated containers. In-project only.",
		InputSchema: leaseInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Lease a pool container",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input LeaseInput) (*mcp.CallToolResult, any, error) {
		if stateDir == "" {
			return convertError(platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				"No state directory: leases are recorded under .zcp/state/leases", "Run zcp from the project's working directory")), nil, nil
		}
		if input.From != "" && input.Action != leaseActionAcquire {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				"from is an acquire's: the branch a new branch is cut from", "Pass from only with action=acquire")), nil, nil
		}
		if input.Pools != nil && input.Action != leaseActionAcquire {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				"pools is for acquire only", "Address one lease by its alias as lease, or one pool as pool")), nil, nil
		}
		switch input.Action {
		case leaseActionAcquire:
			pools := input.Pools
			switch {
			case input.Pool != "" && pools != nil:
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"acquire takes pool or pools, not both", "Pass one pool as pool, or every pool of the branch as pools")), nil, nil
			case input.Pool != "":
				pools = []string{input.Pool}
			case len(pools) == 0:
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"acquire needs pool", "Pass the pool service's hostname as pool, or several as pools")), nil, nil
			}
			change, err := ops.LeaseAcquireGroupChangeFrom(ctx, deps, pools, input.Repo, input.Branch, input.From)
			if err != nil {
				return convertError(withLeaseSSHStderr(err)), nil, nil
			}
			if rt.MateEnabled {
				// Which zcp session took or reused each lease: `zcp lease
				// release` refuses one another live session shares (DP-35).
				recordLeaseSessions(stateDir, change.Leases)
			}
			return jsonResult(leaseToolResult{
				Action: input.Action, Leases: change.Leases, Rewired: change.Rewired,
				NextActions: joinNext(leaseAcquireNext(change.Leases), leaseRewireNext(change.Rewired)), Envelope: envelope(ctx),
			}), nil, nil
		case leaseActionStatus:
			status, err := ops.LeaseStatus(ctx, deps, input.Pool)
			if err != nil {
				return convertError(err), nil, nil
			}
			views := status.Leases
			if views == nil {
				views = []ops.LeaseView{}
			}
			return jsonResult(leaseToolResult{
				Action: input.Action, Leases: views, UnreadPools: status.Unread, Warm: status.Warm, PoolNotices: ops.LeasePoolPreviewNotices(ctx, deps, input.Pool), NextActions: joinNext(leaseStatusNext(views), leaseGroupChanges(stateDir, hqAddress(rt), views, leaseHQState(ctx, hqHTTP, rt))), Envelope: envelope(ctx, views...),
			}), nil, nil
		case leaseActionRelease:
			// The change on each released branch, as lease status names it
			// (leaseGroupChanges): the Mate's state read once, inside a Mate.
			releasedChanges := func(released []ops.LeaseView) string {
				return leaseReleaseChanges(stateDir, hqAddress(rt), released, leaseHQState(ctx, hqHTTP, rt))
			}
			force := false
			if f := input.Force.Ptr(); f != nil {
				force = *f
			}
			switch {
			case input.Lease != "" && input.Branch != "":
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"release takes lease or branch, not both", "Pass one lease by its alias as lease, or every lease of a branch as branch")), nil, nil
			case input.Branch != "":
				change, err := releaseLeaseBranch(ctx, deps, input.Repo, input.Branch, force)
				if err != nil {
					return convertError(err), nil, nil
				}
				forgetLeaseSessions(stateDir, releasedAliases(change.Leases))
				return jsonResult(leaseToolResult{
					Action: input.Action, Leases: change.Leases, Rewired: change.Rewired,
					NextActions: joinNext(leaseReleaseBranchNext(change.Leases, input.Branch, input.Repo, force), leaseRewireNext(change.Rewired), releasedChanges(change.Leases)), Envelope: envelope(ctx),
				}), nil, nil
			case input.Lease == "":
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"release needs lease or branch", "Pass the lease alias (<pool>-<number>) as lease, or a branch as branch to release every lease of it; zerops_lease action=status lists them")), nil, nil
			}
			change, err := ops.LeaseReleaseChange(ctx, deps, input.Lease, force)
			if err != nil {
				return convertError(withLeaseSSHStderr(err)), nil, nil
			}
			forgetLeaseSessions(stateDir, releasedAliases(change.Leases))
			return jsonResult(leaseToolResult{
				Action: input.Action, Leases: change.Leases, Rewired: change.Rewired, NextActions: joinNext(leaseRewireNext(change.Rewired), releasedChanges(change.Leases)), Envelope: envelope(ctx),
			}), nil, nil
		case leaseActionSync:
			if refusal := leaseSyncInputRefusal(input); refusal != nil {
				return convertError(refusal), nil, nil
			}
			view, err := ops.LeaseSync(ctx, deps, input.Lease)
			if err != nil {
				return convertError(withLeaseSSHStderr(err)), nil, nil
			}
			return jsonResult(leaseSyncToolResult{Action: input.Action, Sync: view, NextActions: leaseSyncNext(view), Envelope: envelope(ctx)}), nil, nil
		case leaseActionPark, leaseActionUnpark:
			if refusal := leaseParkInputRefusal(input); refusal != nil {
				return convertError(refusal), nil, nil
			}
			step := ops.LeasePark
			if input.Action == leaseActionUnpark {
				step = ops.LeaseUnpark
			}
			view, err := step(ctx, deps, input.Lease)
			if err != nil {
				return convertError(withLeaseSSHStderr(err)), nil, nil
			}
			return jsonResult(leaseToolResult{
				Action: input.Action, Leases: []ops.LeaseView{view}, NextActions: leaseParkNext(input.Action, view), Envelope: envelope(ctx),
			}), nil, nil
		case leaseActionRollout:
			if input.Pool == "" {
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"rollout needs pool", "Pass the pool service's hostname as pool")), nil, nil
			}
			view, err := ops.LeaseRollout(ctx, deps, input.Pool)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(leaseRolloutToolResult{Action: input.Action, Rollout: view, Envelope: envelope(ctx)}), nil, nil
		default:
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Unknown action %q", input.Action), "action is acquire, status, release, rollout, sync, park or unpark")), nil, nil
		}
	})
}

// leasePublicAccess is an acquire's public-access step for a lease it took
// and started dev servers in: ensurePublicAccessAt on the lease's pool —
// eligibility, intent and the enable-once stamp as ever — with no readiness
// wait (a dev server may still be compiling; verify probes the preview). It
// answers whether the pool's subdomain serves now, and its warnings as a note.
func leasePublicAccess(client platform.Client, projectID, stateDir string) func(context.Context, string) (bool, string) {
	return func(ctx context.Context, alias string) (bool, string) {
		if client == nil {
			return false, ""
		}
		result := &ops.DeployResult{}
		ensurePublicAccessAt(ctx, client, nil, projectID, stateDir, alias, result, nil, func() string { return "" })
		var note string
		if len(result.Warnings) > 0 {
			note = "the pool's public access: " + strings.Join(result.Warnings, "; ")
		}
		return result.SubdomainAccessEnabled, note
	}
}

// leaseAcquireNext is what an acquire's result tells the agent to do next:
// release each lease when done, and that a reused one is the lease already
// held for the branch, not a second container.
func leaseAcquireNext(views []ops.LeaseView) string {
	aliases := make([]string, 0, len(views))
	var reused []string
	for _, v := range views {
		aliases = append(aliases, v.Alias)
		if v.Reused {
			reused = append(reused, v.Alias)
		}
	}
	var next string
	if len(views) == 1 {
		next = fmt.Sprintf("The container is yours until you release it: zerops_lease action=release lease=%s.", views[0].Alias)
	} else {
		next = fmt.Sprintf("The containers are yours until you release them, each by its alias: zerops_lease action=release lease=<alias> for %s.", strings.Join(aliases, ", "))
	}
	if len(reused) > 0 {
		next += fmt.Sprintf(" %s was already held for this branch (reused): the same container, not a second one.", strings.Join(reused, ", "))
	}
	return next + leaseDevServersNext(views) + leaseParkedNext(views) + " zerops_lease action=status shows every lease."
}

// leaseDevServersNext names, among the dev servers an acquire read on the
// leases it took (DP-30), the status call for each whose port did not answer
// yet when the acquire ended and the log of each that crashed; "" when every
// one runs or none was read.
func leaseDevServersNext(views []ops.LeaseView) string {
	var starting, crashed []string
	for _, v := range views {
		for _, s := range v.DevServers {
			switch devsup.Status(s.Status) {
			case devsup.StatusStarting:
				starting = append(starting, fmt.Sprintf("zerops_dev_server action=status hostname=%q name=%q", v.Alias, s.Name))
			case devsup.StatusCrashed:
				crashed = append(crashed, fmt.Sprintf("zerops_dev_server action=logs hostname=%q name=%q", v.Alias, s.Name))
			case devsup.StatusRunning, devsup.StatusStopped:
			}
		}
	}
	var next string
	if len(starting) > 0 {
		next += " Dev servers whose port did not answer yet when the acquire ended (still starting) — check each again: " + strings.Join(starting, "; ") + "."
	}
	if len(crashed) > 0 {
		next += " Dev servers that crashed — read why: " + strings.Join(crashed, "; ") + "."
	}
	return next
}

// leaseParkedNext names the parked leases among views and the call that
// starts one (DP-28), "" when none is parked.
func leaseParkedNext(views []ops.LeaseView) string {
	var parked []string
	for _, v := range views {
		if v.State == ops.LeaseStateParked {
			parked = append(parked, v.Alias)
		}
	}
	if len(parked) == 0 {
		return ""
	}
	return fmt.Sprintf(" Parked — its container stopped, its files kept: %s; %s starts one.", strings.Join(parked, ", "), ops.LeaseUnparkCall("<alias>"))
}

// leaseParkInputRefusal refuses a park or unpark that does not name exactly
// one lease by its alias, and nil otherwise.
func leaseParkInputRefusal(input LeaseInput) error {
	switch {
	case input.Lease == "":
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			input.Action+" needs lease", "Pass the lease alias (<pool>-<number>) as lease; zerops_lease action=status lists them")
	case input.Pool != "" || input.Branch != "" || input.Repo != "" || input.Force != nil:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			input.Action+" takes lease alone", "Pass only the lease alias as lease: one lease is parked or unparked at a time")
	}
	return nil
}

// leaseParkNext is what a park or an unpark tells the agent next (DP-28). One
// that found the lease no longer zcp's — the container gone, being deleted,
// free, or another holder's — says the lease ended outside zcp; its note says
// how, and what became of its record, ssh alias and mount (DP-2).
func leaseParkNext(action string, v ops.LeaseView) string {
	if v.State == ops.LeaseStateLost {
		return fmt.Sprintf("%s was not %sed: its lease had ended outside zcp — its note says how, and what zcp did with its record, ssh alias and mount. "+
			"zerops_lease action=acquire takes a container of your own.", v.Alias, action)
	}
	if action == leaseActionPark {
		return fmt.Sprintf("%s is parked: still yours, its files kept and nothing running. zerops_dev_server, zerops_deploy and zerops_verify refuse it until %s. "+
			"A release still frees it: it starts the container first to check for unsaved work.", v.Alias, ops.LeaseUnparkCall(v.Alias))
	}
	if v.State != ops.LeaseStateHeld {
		return fmt.Sprintf("%s runs again, but not everything came back (its note says what): %s again finishes it.", v.Alias, ops.LeaseUnparkCall(v.Alias))
	}
	return fmt.Sprintf("%s runs again with its files, mounted at /var/www/%s; its supervisor starts the declared dev servers — zerops_dev_server action=status hostname=%q shows them.", v.Alias, v.Alias, v.Alias)
}

// leaseStatusNext is what a status tells the agent of its idle leases (DP-25):
// which they are, that each one's idle says what frees it or what a release
// would lose, that nothing frees a lease by itself, and to ask the person
// before releasing one this conversation did not take — and of its parked
// ones, with the call that starts one (DP-28); "" when none is either.
func leaseStatusNext(views []ops.LeaseView) string {
	var idle []string
	for _, v := range views {
		if v.Idle != nil {
			idle = append(idle, v.Alias)
		}
	}
	parked := strings.TrimSpace(leaseParkedNext(views))
	silent := leaseNotAnsweringNext(views)
	if len(idle) == 0 {
		return joinNext(silent, parked)
	}
	return joinNext(silent, fmt.Sprintf("Idle — nothing has happened in it for a day or more: %s. Each one's idle.next names the release that frees it, or what a release would lose and how to keep it. "+
		"Nothing releases a lease by itself: ask the person before releasing one this conversation did not take.", strings.Join(idle, ", ")), parked)
}

// leaseNotAnsweringNext names the leases whose container did not answer the
// status read, each with why: a lease that stops answering is reported to the
// person, never waited on (DP-21); "" when every lease read answered.
func leaseNotAnsweringNext(views []ops.LeaseView) string {
	var silent []string
	for _, v := range views {
		if v.NotAnswering != "" {
			silent = append(silent, fmt.Sprintf("%s (%s)", v.Alias, v.NotAnswering))
		}
	}
	if len(silent) == 0 {
		return ""
	}
	return "Leases not answering: " + strings.Join(silent, "; ") + ". Nothing zcp or you run there works meanwhile: tell the person now rather than wait on it or retry in a loop. " +
		"A container at its process limit does not recover by itself while what fills it runs, and restarting its pool restarts every container of it, the other leases' dev servers too."
}

// leaseReleaseBranchNext is what a release of a branch tells the agent when
// a lease of it could not be released: which, and that the same call again
// releases what is left (a released lease has no record left); "" when every
// lease went.
func leaseReleaseBranchNext(views []ops.LeaseView, branch, repo string, force bool) string {
	var failed []string
	for _, v := range views {
		if v.Error != "" {
			failed = append(failed, v.Alias)
		}
	}
	if len(failed) == 0 {
		return ""
	}
	next := fmt.Sprintf("Not released, still held and recorded (each lease's error says why): %s.", strings.Join(failed, ", "))
	if len(failed) < len(views) {
		next += " Every other lease of the branch was released."
	}
	return next + " " + ops.LeaseReleaseBranchCall(branch, repo, force) + " again releases what is left of the branch."
}

// leaseRewireNext is what the rewire of a branch's other leases tells the
// agent: the note of each lease it could not hand the new overlay, or whose
// servers had not yet restarted under it — each naming the lease and the call
// that sends the overlay again; "" when every one took it.
func leaseRewireNext(rewired []ops.LeaseRewire) string {
	var notes []string
	for _, r := range rewired {
		if r.Note != "" {
			notes = append(notes, r.Note)
		}
	}
	if len(notes) == 0 {
		return ""
	}
	return "Sibling wiring of the branch's other leases needs a look (a release or acquire never waits on it): " + strings.Join(notes, "; ") + "."
}

// joinNext joins the non-empty next-action sentences.
func joinNext(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}

// leaseReads is what a call read of each lease's checkout, keyed by alias, for
// the envelope (workflow.WithLeaseActivity): a view that carries no activity
// and no unsaved work was not read and is left out.
func leaseReads(views []ops.LeaseView) map[string]workflow.LeaseRead {
	reads := map[string]workflow.LeaseRead{}
	for _, v := range views {
		if v.Activity != nil || v.Unsaved != nil {
			reads[v.Alias] = workflow.LeaseRead{Activity: v.Activity, Unsaved: v.Unsaved}
		}
	}
	return reads
}

// withLeaseSSHStderr puts git's stderr from an ssh failure into the error's
// message (withSSHStderr: SSHExecError.Error() drops it). Errors that carry no
// ssh failure pass through.
func withLeaseSSHStderr(err error) error {
	var pe *platform.PlatformError
	var sshErr *platform.SSHExecError
	if !errors.As(err, &pe) || !errors.As(err, &sshErr) {
		return err
	}
	out := *pe
	out.Message = withSSHStderr(pe.Message, sshErr)
	return &out
}

// leaseHQState is lease status's reader of the Mate's own state in HQ, for
// leaseGroupChanges: inside a Mate, through hqHTTP; nil — never read — with
// no client or outside a Mate.
func leaseHQState(ctx context.Context, hqHTTP ops.HTTPDoer, rt runtime.Info) func() *hq.MateState {
	if hqHTTP == nil || !rt.MateEnabled {
		return nil
	}
	return func() *hq.MateState { return hqStateNow(ctx, hqHTTP, rt) }
}
