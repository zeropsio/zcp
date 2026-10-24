package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// SwarmAPI is a pool orchestrator.
type SwarmAPI interface {
	Pool(ctx context.Context) (platform.SwarmPool, error)
	Containers(ctx context.Context) ([]platform.SwarmContainer, error)
	Acquire(ctx context.Context, req platform.SwarmAcquireRequest) (platform.SwarmAcquired, error)
	Release(ctx context.Context, id, lease string, reset bool) (platform.SwarmContainer, error)
	Renew(ctx context.Context, id, lease string) (platform.SwarmContainer, error)
	Delete(ctx context.Context, id, lease string) error
	Rollout(ctx context.Context) (platform.SwarmRollout, error)
}

// SwarmFactory builds an orchestrator client.
type SwarmFactory func(baseURL, token string) SwarmAPI

// NewSwarmFactory builds orchestrator clients over hc.
func NewSwarmFactory(hc *http.Client) SwarmFactory {
	return func(baseURL, token string) SwarmAPI { return platform.NewSwarmClient(baseURL, token, hc) }
}

var _ SwarmAPI = (*platform.SwarmClient)(nil)

// LeaseDeps are what the lease operations need.
type LeaseDeps struct {
	Client    platform.Client
	ProjectID string
	StateDir  string
	Swarm     SwarmFactory
	// Host is the ssh alias and mount side of a lease; the zero value does neither.
	Host LeaseHost
	// SSH runs git inside a leased container by its alias; nil means no
	// checkout and no release guard (zcp outside a container).
	SSH SSHDeployer
	// Env reads one key of the container's current env; nil means the live
	// env (liveContainerEnv).
	Env func(key string) (string, bool)
	// HQAddress is the address of the HQ this Mate enrolled with, read at
	// each use ("" — or nil — for none): a repository on it is checked out as
	// HQ's git user (DP-12).
	HQAddress func() string
	// PoolCredential puts the Mate credential on the pool's GIT_TOKEN before a
	// lease of a repository on HQ takes a container (DP-16); the zero value —
	// outside a Mate — never writes.
	PoolCredential PoolHQCredential
	// ReadTimeout bounds one orchestrator read (a container listing); zero
	// means leaseReadTimeout. The HTTP client allows minutes, for a start or
	// a reset; a read that hangs must not hold a status call that long.
	ReadTimeout time.Duration
	// ActivityTimeout bounds the one read of a held lease's checkout a status
	// call makes (readLeaseActivity); zero means leaseActivityReadTimeout.
	ActivityTimeout time.Duration
	// Now is zcp's clock (nil: time.Now): when a lease's checkout was read,
	// and how long ago its last activity was (DP-25).
	Now func() time.Time
}

// now reads zcp's clock.
func (d LeaseDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// leaseReadTimeout is the default ReadTimeout.
const leaseReadTimeout = 15 * time.Second

// containers lists the pool's containers within the read budget.
func (d LeaseDeps) containers(ctx context.Context, rp *resolvedPool) ([]platform.SwarmContainer, error) {
	budget := d.ReadTimeout
	if budget == 0 {
		budget = leaseReadTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	return rp.api.Containers(ctx)
}

// env reads key from the container's current env.
func (d LeaseDeps) env(key string) (string, bool) {
	if d.Env != nil {
		return d.Env(key)
	}
	return liveContainerEnv(key)
}

// liveEnvFile is where Zerops keeps a container's current env, rewritten as
// the project's services change; a process's own environment is a snapshot
// from its start, so a pool created after zcp started is only seen here.
const liveEnvFile = "/etc/zerops-zembed/env.json"

// liveContainerEnv reads key from the container's current env.
func liveContainerEnv(key string) (string, bool) {
	return liveContainerEnvFrom(liveEnvFile, key)
}

// liveContainerEnvFrom reads key from the live env file at path, read fresh on
// every call. Where the file exists it is the answer — a key it lacks is
// absent, never taken from the process env, which is a snapshot from the
// process's start (a re-created pool's old token would win). Only where there
// is no file (outside a Zerops container) does the process env answer.
func liveContainerEnvFrom(path, key string) (string, bool) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		v := os.Getenv(key)
		return v, v != ""
	}
	if err != nil {
		return "", false
	}
	var env map[string]any
	if json.Unmarshal(raw, &env) != nil {
		return "", false
	}
	v, ok := env[key].(string)
	return v, ok && v != ""
}

// Lease states.
const (
	LeaseStateHeld          = "held"
	LeaseStateLost          = "lost"
	LeaseStateUnknownHolder = "unknown-holder"
	LeaseStateReleased      = "released"
	// LeaseStateUnmounted is a held lease whose ssh alias or mount is missing
	// on this host.
	LeaseStateUnmounted = "unmounted"
)

// LeaseView is a lease as the agent sees it.
type LeaseView struct {
	Alias             string `json:"alias"`
	Pool              string `json:"pool"`
	ContainerHostname string `json:"containerHostname"`
	Branch            string `json:"branch,omitempty"`
	State             string `json:"state"`
	// Unverified marks a state read from zcp's own record because the pool's
	// orchestrator could not be asked (LeaseStatus of every pool).
	Unverified bool `json:"unverified,omitempty"`
	// Reused marks an acquire that returned the lease zcp already held for
	// the repository, branch and pool instead of taking another container
	// (DP-17): a client binds nothing to it.
	Reused     bool   `json:"reused,omitempty"`
	AcquiredAt string `json:"acquiredAt,omitempty"`
	Note       string `json:"note,omitempty"`
	// Error is why a release of the lease's branch could not release this
	// lease, which stays held and recorded (DP-23); "" on every other view.
	Error string `json:"error,omitempty"`
	// Prepare is what the repository's prepare command did after an
	// acquire's checkout (DP-14); nil when none ran.
	Prepare *LeasePrepare `json:"prepare,omitempty"`
	// Activity is what a status call read of the lease's checkout, or why it
	// could not (DP-21); nil when it was not read (another action, or no ssh).
	Activity *topology.LeaseActivity `json:"activity,omitempty"`
	// Unsaved is what releasing the lease would lose, read with the release
	// guard's own reads (DP-4, DP-21); nil when it was not read.
	Unsaved *topology.LeaseUnsaved `json:"unsaved,omitempty"`
	// Idle marks a lease nothing has happened in for leaseIdleAfter, by what
	// this status call read, with what frees it (DP-25); nil otherwise. A
	// label for the result only: nothing acts on it, and the envelope never
	// carries it.
	Idle *LeaseIdle `json:"idle,omitempty"`
	// noPreview: the read found no preview to observe — a supervisor that
	// lists activity runs no dev server on a preview port.
	noPreview bool
	// Wiring is LeaseWiringStale on a status view of a lease whose dev servers
	// run with an overlay naming a container no lease of its group holds, the
	// note saying which and the call that fixes it (DP-26); "" otherwise.
	Wiring string `json:"wiring,omitempty"`
	// overlay is what status's read found of the lease's supervisor overlay;
	// nil when it was not read or the supervisor predates it.
	overlay *leaseOverlay
	// containerDeleted: a release deleted the container because it could not
	// be reset (DP-15).
	containerDeleted bool
}

// leaseTokenKey is the pool service's own env var that its orchestrator takes
// as the bearer token.
const leaseTokenKey = "API_TOKEN"

// leaseTokenAliasKey names the variable a zcp service carries a pool's bearer
// under when nothing hands it <pool>_API_TOKEN: <pool>_LEASE_TOKEN, set to the
// reference ${<pool>_API_TOKEN}. The name must differ from the reference it
// holds: a variable <pool>_API_TOKEN=${<pool>_API_TOKEN} stays the literal
// text under every envIsolation, and while it exists every other reference to
// that name on the service stays literal too; a different name resolves into
// the live env within about 15 s (live, 2026-10-23).
const leaseTokenAliasKey = "LEASE_TOKEN"

// unresolvedReference is a value that is still the text of an env reference
// (`${name}`): a reference the platform did not resolve. It is never a token.
var unresolvedReference = regexp.MustCompile(`^\$\{[A-Za-z0-9_]+\}$`)

// leaseAlias is the alias a lease is addressed by: <pool>-<container number>.
func leaseAlias(pool string, number int64) string {
	return pool + "-" + strconv.FormatInt(number, 10)
}

// orchestratorURL is where a pool's orchestrator answers (in-project network
// only).
func orchestratorURL(pool string) string {
	return "http://orch." + pool + ".zerops"
}

// resolvedPool is a pool service with a client for its orchestrator.
type resolvedPool struct {
	svc *platform.ServiceStack
	api SwarmAPI
}

// resolvePool finds the pool service by hostname, refuses any other type by
// name, and finds its API_TOKEN: in the container's live env as
// <pool>_API_TOKEN, then as <pool>_LEASE_TOKEN (a Mate's own API token sees
// the pool's secrets masked; the live env carries the token where the pool's
// env reaches this container or this service references it under a different
// name), then through the ops env read (a token with full access).
func resolvePool(ctx context.Context, d LeaseDeps, hostname string) (*resolvedPool, error) {
	svc, err := LookupService(ctx, d.Client, d.ProjectID, hostname)
	if err != nil {
		return nil, err
	}
	return orchestratorFor(ctx, d, svc)
}

// orchestratorFor checks svc is a pool and builds the client of its orchestrator
// (resolvePool's work once the service is in hand).
func orchestratorFor(ctx context.Context, d LeaseDeps, svc *platform.ServiceStack) (*resolvedPool, error) {
	hostname := svc.Name
	typ := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	if !topology.IsPoolType(typ) {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Service '%s' is %s, not a Swarm pool", hostname, typ),
			"zerops_lease leases a container of a pool (swarm@1 or swarm-vm@1); pass the pool's hostname")
	}
	token, unresolved := liveBearer(d, hostname)
	if token != "" {
		return &resolvedPool{svc: svc, api: d.Swarm(orchestratorURL(hostname), token)}, nil
	}
	envs, err := FetchServiceEnv(ctx, d.Client, svc.ID)
	if err != nil {
		return nil, fmt.Errorf("read %s env: %w", hostname, err)
	}
	scalars, masked := EnvScalars(envs)
	if token = scalars[leaseTokenKey]; token == "" || unresolvedReference.MatchString(token) {
		reason := leaseTokenKey + " is not set on the pool"
		if slices.Contains(masked, leaseTokenKey) {
			reason = leaseTokenKey + " is masked for this zcp's API token"
		}
		return nil, noBearerError(hostname, reason, unresolved)
	}
	return &resolvedPool{svc: svc, api: d.Swarm(orchestratorURL(hostname), token)}, nil
}

// liveBearer reads a pool's bearer from the container's live env:
// <pool>_API_TOKEN, then <pool>_LEASE_TOKEN. A value that is still the text of
// a reference is skipped, and named in unresolved ("<key> is the unresolved
// text ${...}").
func liveBearer(d LeaseDeps, pool string) (token string, unresolved []string) {
	for _, key := range []string{pool + "_" + leaseTokenKey, pool + "_" + leaseTokenAliasKey} {
		v, ok := d.env(key)
		if !ok {
			continue
		}
		if unresolvedReference.MatchString(v) {
			unresolved = append(unresolved, key+" is the unresolved text "+v)
			continue
		}
		return v, nil
	}
	return "", unresolved
}

// noBearerError is the refusal of a pool zcp finds no bearer for: why the env
// API did not give it, what the live env held instead, and the different-name
// reference to add — never the same-name one, which cannot resolve.
func noBearerError(pool, reason string, unresolved []string) error {
	apiKey := pool + "_" + leaseTokenKey
	aliasKey := pool + "_" + leaseTokenAliasKey
	msg := fmt.Sprintf("Cannot reach pool '%s': %s, and this container's env has no usable %s or %s", pool, reason, apiKey, aliasKey)
	if len(unresolved) > 0 {
		msg += " (" + strings.Join(unresolved, "; ") + ")"
	}
	var suggestion strings.Builder
	fmt.Fprintf(&suggestion, "The orchestrator needs the pool's own %s as its bearer token. ", leaseTokenKey)
	if len(unresolved) > 0 && strings.HasPrefix(unresolved[0], apiKey+" ") {
		fmt.Fprintf(&suggestion, "This zcp service has a variable %s holding ${%s}: a variable named like the reference it holds never resolves, and while it exists every other reference to that name on the service stays unresolved too — delete it. ", apiKey, apiKey)
	}
	fmt.Fprintf(&suggestion, "Add a service env var %s=${%s} on this zcp service; it reaches the container's live env within about 15 s, whatever the envIsolation. Never name it %s: a variable named like the reference it holds stays the literal text", aliasKey, apiKey, apiKey)
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing, msg, suggestion.String())
}

// LeaseList reports the leases of one pool, or of every pool the project
// holds or zcp has a record for when none is named (LeaseStatus without the
// pools it could not read).
func LeaseList(ctx context.Context, d LeaseDeps, pool string) ([]LeaseView, error) {
	status, err := LeaseStatus(ctx, d, pool)
	if err != nil {
		return nil, err
	}
	return status.Leases, nil
}

// LeaseStatus reports the leases of one pool, or of every pool the project
// holds or zcp has a record for when none is named. The orchestrator is the
// truth: a record whose container is missing or not reserved is lost (never
// re-leased), and a reserved container with no record is an unknown holder
// (acquire has no idempotency key, so a lost response would otherwise leak a
// container forever). A named pool that cannot be read is an error; when
// every pool is listed, one that cannot be read (stopped, unreachable) never
// fails the rest: its leases are zcp's records, marked unverified with why,
// and the pool is named in Unread. With ssh, each lease zcp holds also
// carries what one read of its checkout found (readLeaseActivities, DP-21),
// and one nothing has happened in for leaseIdleAfter is marked idle with what
// frees it (markIdleLeases, DP-25), and a lease whose servers run wired to a
// container no lease of its group holds is marked wiring: stale
// (markStaleWiring, DP-26); status changes nothing anywhere.
func LeaseStatus(ctx context.Context, d LeaseDeps, pool string) (LeaseStatusView, error) {
	records, err := readLeaseRecords(d.StateDir)
	if err != nil {
		return LeaseStatusView{}, err
	}
	byPool := map[string][]leaseRecord{}
	for _, r := range records {
		byPool[r.PoolHostname] = append(byPool[r.PoolHostname], r)
	}
	if pool != "" {
		views, err := listPoolLeases(ctx, d, pool, byPool[pool], true)
		if err != nil {
			return LeaseStatusView{}, err
		}
		readLeaseActivities(ctx, d, records, views)
		markIdleLeases(d, records, views)
		markStaleWiring(d.StateDir, records, views)
		return LeaseStatusView{Leases: views}, nil
	}
	names := map[string]bool{}
	for name := range byPool {
		names[name] = true
	}
	services, err := ListProjectServices(ctx, d.Client, d.ProjectID)
	if err != nil {
		return LeaseStatusView{}, fmt.Errorf("list services: %w", err)
	}
	for _, svc := range services {
		if topology.IsPoolType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			names[svc.Name] = true
		}
	}
	var status LeaseStatusView
	for _, name := range slices.Sorted(maps.Keys(names)) {
		got, err := listPoolLeases(ctx, d, name, byPool[name], false)
		if err != nil {
			note := errorNote(err)
			status.Leases = append(status.Leases, unverifiedViews(byPool[name], note)...)
			status.Unread = append(status.Unread, LeasePoolUnread{Pool: name, Note: note})
			continue
		}
		status.Leases = append(status.Leases, got...)
	}
	readLeaseActivities(ctx, d, records, status.Leases)
	markIdleLeases(d, records, status.Leases)
	markStaleWiring(d.StateDir, records, status.Leases)
	return status, nil
}

// listPoolLeases reconciles one pool's records against its orchestrator.
// explicit: the caller named the pool, so an unknown pool is an error unless
// records for it remain to be reported.
func listPoolLeases(ctx context.Context, d LeaseDeps, pool string, records []leaseRecord, explicit bool) ([]LeaseView, error) {
	rp, err := resolvePool(ctx, d, pool)
	if err != nil {
		var pe *platform.PlatformError
		if errors.As(err, &pe) && pe.Code == platform.ErrServiceNotFound && len(records) > 0 {
			return lostViews(records, "the pool service no longer exists"), nil
		}
		if !explicit && errors.As(err, &pe) && pe.Code == platform.ErrServiceNotFound {
			return nil, nil
		}
		return nil, err
	}
	containers, err := d.containers(ctx, rp)
	if err != nil {
		return nil, leaseOrchestratorError(ctx, d, rp, "list containers", err)
	}
	byID := make(map[string]platform.SwarmContainer, len(containers))
	for _, c := range containers {
		byID[c.ID] = c
	}
	var views []LeaseView
	recorded := map[string]bool{}
	for _, r := range records {
		recorded[r.ContainerID] = true
		c, present := byID[r.ContainerID]
		switch {
		case r.PoolServiceID != rp.svc.ID:
			views = append(views, lostView(r, "the pool was re-created since this lease was taken"))
		case !present:
			views = append(views, lostView(r, "the container is gone from the pool"))
		case c.LockState != leaseLockReserved:
			views = append(views, lostView(r, "the orchestrator no longer reserves this container (the lease ended or was released elsewhere)"))
		default:
			v := r.view(LeaseStateHeld)
			v.AcquiredAt = r.AcquiredAt
			if gap := d.Host.gap(ctx, r.alias()); gap != "" {
				v.State = LeaseStateUnmounted
				v.Note = gap
			}
			views = append(views, v)
		}
	}
	for _, c := range containers {
		if c.LockState != leaseLockReserved || recorded[c.ID] {
			continue
		}
		views = append(views, LeaseView{
			Alias: leaseAlias(pool, c.Number), Pool: pool, ContainerHostname: c.Hostname, State: LeaseStateUnknownHolder,
			Note: "reserved on the pool but zcp holds no lease for it (a lost acquire answer leaves a container reserved); zcp never re-leases it, and only its holder or a pool admin can release it",
		})
	}
	slices.SortFunc(views, func(a, b LeaseView) int { return cmp.Compare(leaseNumber(a.Alias), leaseNumber(b.Alias)) })
	return views, nil
}

// leaseLockReserved is the orchestrator's lockState of a held container.
const leaseLockReserved = "reserved"

func lostView(r leaseRecord, why string) LeaseView {
	v := r.view(LeaseStateLost)
	v.AcquiredAt = r.AcquiredAt
	v.Note = why + "; zcp does not re-lease it. zerops_lease action=release lease=" + r.alias() + " drops the record"
	return v
}

func lostViews(records []leaseRecord, why string) []LeaseView {
	views := make([]LeaseView, 0, len(records))
	for _, r := range records {
		views = append(views, lostView(r, why))
	}
	return views
}

// leaseNumber is the container number of an alias (0 when malformed).
func leaseNumber(alias string) int64 {
	i := strings.LastIndexByte(alias, '-')
	n, err := strconv.ParseInt(alias[i+1:], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// leaseOrchestratorError says what a failed orchestrator call on the pool
// means to the agent: a transport failure is a pool that is not running
// (its status, read by id, says so) or the "outside the project" case; a
// refusal is the orchestrator's own answer, with the platform's cause when it
// passed one on.
func leaseOrchestratorError(ctx context.Context, d LeaseDeps, rp *resolvedPool, op string, err error) error {
	pool := rp.svc.Name
	var pe *platform.PlatformError
	if errors.As(err, &pe) && pe.Code == platform.ErrNetworkError {
		if status, running := poolRunStatus(ctx, d, rp); !running {
			return poolNotRunningError(pool, op, status, err)
		}
		return platform.NewPlatformError(platform.ErrNetworkError,
			fmt.Sprintf("Cannot %s on pool '%s': %s", op, pool, pe.Message),
			fmt.Sprintf("The orchestrator (%s) answers only inside the project: run zcp in the project's container, or connect the project VPN", orchestratorURL(pool)))
	}
	var se *platform.SwarmError
	if errors.As(err, &se) {
		msg := fmt.Sprintf("Pool '%s' orchestrator refused (%s): %s", pool, op, se.Message)
		if se.Cause != "" && se.Cause != se.Message {
			msg += " (platform: " + se.Cause + ")"
		}
		out := platform.NewPlatformError(platform.ErrAPIError, msg, swarmSuggestion(se))
		out.APICode = se.Reason
		out.Cause = err
		return out
	}
	return fmt.Errorf("%s %s: %w", op, pool, err)
}

// LeaseCallTimeout bounds one orchestrator call of a lease: a start or a
// reset restarts a container, and on a VM pool creating one takes minutes.
// It is also the timeout of the HTTP client the lease tool is registered with.
const LeaseCallTimeout = 10 * time.Minute

// leaseAcquireError is leaseOrchestratorError for an acquire, except a
// timeout and a full pool. A timeout on a running pool may have reserved a
// container before the answer was lost, so the agent learns where it shows
// up, not that zcp runs outside the project. A full pool (all-reserved) says
// how full it is and what frees a container (leasePoolFullError).
func leaseAcquireError(ctx context.Context, d LeaseDeps, rp *resolvedPool, err error) error {
	pool := rp.svc.Name
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		if status, running := poolRunStatus(ctx, d, rp); !running {
			return poolNotRunningError(pool, "acquire", status, err)
		}
		out := platform.NewPlatformError(platform.ErrNetworkError,
			fmt.Sprintf("The orchestrator of pool '%s' did not answer the acquire in time; it may have reserved a container whose lease zcp never received", pool),
			"zerops_lease action=status lists such a container as unknown-holder; it stays reserved until a pool admin releases it. Acquire again for a container of your own")
		out.Cause = err
		return out
	}
	var se *platform.SwarmError
	if errors.As(err, &se) && se.Reason == platform.SwarmReasonAllReserved {
		return leasePoolFullError(ctx, d, rp, se)
	}
	return leaseOrchestratorError(ctx, d, rp, "acquire", err)
}

// swarmSuggestion is what to do about an orchestrator refusal. A full pool
// (all-reserved) is retryable on the orchestrator's word but frees up only
// on a release, so it never reads "in a moment" (leasePoolFullError).
func swarmSuggestion(se *platform.SwarmError) string {
	switch {
	case se.Status == http.StatusUnauthorized || se.Code == "notAuthorized":
		return "The pool's API_TOKEN was refused; re-read it with zerops_env get on the pool"
	case se.Reason == platform.SwarmReasonAllReserved:
		return "Every container that can serve the call is leased and the orchestrator queues nothing: release a lease you no longer need (zerops_lease action=status lists them)"
	case se.Retryable:
		return "The orchestrator marks this retryable: the same call may succeed in a moment"
	}
	return ""
}

// LeaseAcquire leases a container of the pool and puts its /var/www on the
// branch of repo (DP-6): a group acquire of the one pool (LeaseAcquireGroup),
// so a branch zcp already holds on the pool returns that lease, reused. It
// always asks for a clean, started container with no expiry
// (leaseTtlSeconds 0): an expiring lease frees the container silently and
// unreset — a clean acquire resets only a container marked dirty, which ssh
// writes never mark — so the lease ends only by an explicit release, which
// resets it. An empty repo is the pool's default repository (the first
// acquire with a repo records it); with neither, or with a repo or branch that
// is not usable, the acquire refuses before it takes a container. A checkout
// that fails hands the container straight back.
func LeaseAcquire(ctx context.Context, d LeaseDeps, pool, repo, branch string) (LeaseView, error) {
	views, err := LeaseAcquireGroup(ctx, d, []string{pool}, repo, branch)
	if err != nil {
		return LeaseView{}, err
	}
	return views[0], nil
}

// leaseRecord is zcp's own record of a lease it holds
// (.zcp/state/leases/<alias>.json, mode 0600). The orchestrator is the truth
// for the lease; the record carries the secret and what zcp needs to find its
// own leases again. It is never a ServiceMeta.
type leaseRecord struct {
	PoolHostname      string `json:"poolHostname"`
	PoolServiceID     string `json:"poolServiceId"`
	ContainerID       string `json:"containerId"`
	ContainerNumber   int64  `json:"containerNumber"`
	ContainerHostname string `json:"containerHostname"`
	Lease             string `json:"lease"`
	Branch            string `json:"branch,omitempty"`
	Repo              string `json:"repo,omitempty"` // the repository's identity (topology.CanonicalRepoURL), never cloned from nor a credential; "" in an older record
	// Base is the main commit the branch was cut from when it was new to
	// origin, so a lease joining its group later is cut from it too (DP-17);
	// "" when the branch was tracked from origin, or in an older record.
	Base       string `json:"base,omitempty"`
	AcquiredAt string `json:"acquiredAt"`
}

func (r leaseRecord) alias() string { return leaseAlias(r.PoolHostname, r.ContainerNumber) }

// view is the record as the agent sees it: never the lease.
func (r leaseRecord) view(state string) LeaseView {
	return LeaseView{
		Alias: r.alias(), Pool: r.PoolHostname, ContainerHostname: r.ContainerHostname,
		Branch: r.Branch, State: state, AcquiredAt: r.AcquiredAt,
	}
}

// isLeaseAliasShape is the shape of an alias (<pool>-<number>), owned by
// platform.ValidateLeaseAlias; it is also what keeps an alias from naming a
// path outside the lease directory.
func isLeaseAliasShape(alias string) bool { return platform.ValidateLeaseAlias(alias) == nil }

func leaseDir(stateDir string) string { return filepath.Join(stateDir, "leases") }

// writeLeaseRecord writes the record atomically with mode 0600.
func writeLeaseRecord(stateDir string, rec leaseRecord) error {
	if stateDir == "" {
		return errors.New("no state directory")
	}
	if !isLeaseAliasShape(rec.alias()) {
		return fmt.Errorf("alias %q is not <pool>-<number>", rec.alias())
	}
	dir := leaseDir(stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".lease-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	defer os.Remove(tmp.Name()) // best effort: already gone after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, rec.alias()+".json")); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// readLeaseRecords reads every lease record. A record that cannot be read or
// parsed is skipped: it names no lease zcp could use.
func readLeaseRecords(stateDir string) ([]leaseRecord, error) {
	if stateDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(leaseDir(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read lease records: %w", err)
	}
	var out []leaseRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(leaseDir(stateDir), e.Name()))
		if err != nil {
			continue
		}
		var r leaseRecord
		if json.Unmarshal(raw, &r) != nil || r.alias()+".json" != e.Name() || !isLeaseAliasShape(r.alias()) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// LeaseRelease is LeaseReleaseChange's one lease.
func LeaseRelease(ctx context.Context, d LeaseDeps, alias string, force bool) (LeaseView, error) {
	change, err := LeaseReleaseChange(ctx, d, alias, force)
	if err != nil {
		return LeaseView{}, err
	}
	return change.Leases[0], nil
}

// LeaseReleaseChange releases the lease addressed by alias and removes its
// record. Unless force is set it first reads the container over ssh and
// refuses while it holds commits on no remote branch or uncommitted/untracked
// files (DP-4); the refusal leaves the mount, the record and the lease as they
// were. Every release resets the container (processes end). A container ssh
// cannot reach is released as lost, unchecked. If the orchestrator fails the
// reservation is kept and so is the record. A lease the orchestrator no longer
// knows (it ended, or the container is gone) has nothing left to release: the
// record is dropped and the view says so. Before the orchestrator is asked,
// every other lease of its branch is handed the sibling overlay without it —
// the container goes to the pool's next acquirer under the same hostname — and
// a release that then fails, the orchestrator still reserving the container,
// hands it back with the lease still in (DP-26).
func LeaseReleaseChange(ctx context.Context, d LeaseDeps, alias string, force bool) (LeaseChange, error) {
	rec, err := loadLeaseRecord(d.StateDir, alias)
	if err != nil {
		return LeaseChange{}, err
	}
	var guard leaseGuardResult
	if !force {
		// The guard reaches the container through its alias: put back a
		// stanza another call or a redeploy lost before it runs.
		restoreNote := d.Host.refreshAliases(ctx, d)
		if guard, err = guardLeaseRelease(ctx, d, rec); err != nil {
			return LeaseChange{}, err
		}
		guard.note = joinNotes(restoreNote, guard.note)
	}
	var siblings []leaseRecord
	var rewired []LeaseRewire
	if d.SSH != nil {
		siblings = siblingRecords(d.StateDir, rec)
		rewired = rewireLeases(ctx, d.SSH, d.StateDir, siblings, map[string]bool{alias: true}, "without "+alias)
	}
	view, err := releaseGuarded(ctx, d, rec, guard)
	if err != nil {
		return LeaseChange{}, handBackOverlay(ctx, d, rec, siblings, rewired, err)
	}
	return LeaseChange{Leases: []LeaseView{view}, Rewired: rewired}, nil
}

// releaseGuarded releases a lease whose guard passed (or was skipped by
// force): unmount, release with a reset — or delete the container when it
// cannot be reset (DP-15) — and drop the record and its alias stanza; a
// container the guard could not reach is reported lost, and the guard's note
// leads the view's. It is the one release step of a lease, alone or as a
// member of its branch's release (DP-23).
func releaseGuarded(ctx context.Context, d LeaseDeps, rec leaseRecord, guard leaseGuardResult) (LeaseView, error) {
	view, err := releaseAndDetach(ctx, d, rec, rec.alias())
	if err != nil {
		return LeaseView{}, err
	}
	if guard.unreachable && view.State == LeaseStateReleased {
		view.State = LeaseStateLost
	}
	view.Note = joinNotes(guard.note, view.Note)
	return view, nil
}

// releaseAndDetach unmounts the lease, releases the container with a reset and
// drops the record and its alias stanza.
func releaseAndDetach(ctx context.Context, d LeaseDeps, rec leaseRecord, alias string) (LeaseView, error) {
	// The mount goes first: a reset or a release pulls the container out from
	// under it. A mount that will not unmount is a note, never a reason to
	// keep the container reserved.
	unmountNote := d.Host.detachMount(ctx, d, rec)
	view, err := releaseLease(ctx, d, rec, alias, true)
	if err != nil {
		return LeaseView{}, err
	}
	// The record is gone: take its stanza out of the alias file.
	view.Note = joinNotes(view.Note, unmountNote, d.Host.refreshAliases(ctx, d))
	return view, nil
}

// joinNotes joins the non-empty notes.
func joinNotes(notes ...string) string {
	var kept []string
	for _, n := range notes {
		if n != "" {
			kept = append(kept, n)
		}
	}
	return strings.Join(kept, "; ")
}

// releaseLease releases the container with the orchestrator and drops the
// record (every successful outcome drops it). A pool that is gone or being
// deleted holds nothing of the lease: its record is dropped as lost, saying
// why (DP-20), whichever read tells — the search, or the read by id once the
// orchestrator could not be reached or asked.
func releaseLease(ctx context.Context, d LeaseDeps, rec leaseRecord, alias string, reset bool) (LeaseView, error) {
	gone := func(why string) (LeaseView, error) {
		return dropLeaseRecord(d.StateDir, rec, LeaseStateLost, why+", so there was nothing to release; the record is dropped")
	}
	rp, err := resolvePool(ctx, d, rec.PoolHostname)
	if err != nil {
		var pe *platform.PlatformError
		if errors.As(err, &pe) && pe.Code == platform.ErrServiceNotFound {
			return gone("the pool service no longer exists")
		}
		if why := leasePoolGoneWhy(ctx, d, rec); why != "" {
			return gone(why)
		}
		return LeaseView{}, err
	}
	if rp.svc.ID != rec.PoolServiceID {
		return dropLeaseRecord(d.StateDir, rec, LeaseStateLost, "the pool was re-created since this lease was taken, so there was nothing to release; the record is dropped")
	}
	if state := poolDeletionState(rp.svc.Status); state != "" {
		return gone(fmt.Sprintf("the pool service %s (status %s)", state, rp.svc.Status))
	}
	if _, err := rp.api.Release(ctx, rec.ContainerID, rec.Lease, reset); err != nil {
		var se *platform.SwarmError
		if reset && errors.As(err, &se) && se.Reason == platform.SwarmReasonNoPrimarySnapshot {
			// Never released unreset: the next holder would get its files (DP-15).
			return deleteUnresettable(ctx, d, rp, rec, alias)
		}
		if errors.As(err, &se) && (se.Reason == "unknown-lease" || se.Reason == "container-not-found") {
			return dropLeaseRecord(d.StateDir, rec, LeaseStateLost, "the orchestrator no longer held this container for this lease, so there was nothing to release; the record is dropped")
		}
		// An orchestrator that could not be reached may be one of a pool
		// deleted since the search was read.
		if !errors.As(err, &se) {
			if why := leasePoolGoneWhy(ctx, d, rec); why != "" {
				return gone(why)
			}
		}
		return LeaseView{}, leaseOrchestratorError(ctx, d, rp, "release "+alias, err)
	}
	return dropLeaseRecord(d.StateDir, rec, LeaseStateReleased, "")
}

// dropLeaseRecord removes the record and returns its view in the given state.
func dropLeaseRecord(stateDir string, rec leaseRecord, state, note string) (LeaseView, error) {
	path := filepath.Join(leaseDir(stateDir), rec.alias()+".json")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return LeaseView{}, fmt.Errorf("remove lease record %s: %w", rec.alias(), err)
	}
	v := rec.view(state)
	v.Note = note
	return v, nil
}

// loadLeaseRecord reads the record of one alias.
func loadLeaseRecord(stateDir, alias string) (leaseRecord, error) {
	if !isLeaseAliasShape(alias) {
		return leaseRecord{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("'%s' is not a lease alias", alias),
			"A lease is addressed by <pool>-<container number>, e.g. devpool-3; zerops_lease action=status lists them")
	}
	raw, err := os.ReadFile(filepath.Join(leaseDir(stateDir), alias+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return leaseRecord{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("No lease record for '%s'", alias),
			"zerops_lease action=status lists the leases this zcp holds")
	}
	if err != nil {
		return leaseRecord{}, fmt.Errorf("read lease record %s: %w", alias, err)
	}
	var rec leaseRecord
	if err := json.Unmarshal(raw, &rec); err != nil || rec.alias() != alias {
		return leaseRecord{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("The lease record for '%s' is unreadable", alias), "Remove .zcp/state/leases/"+alias+".json and check zerops_lease action=status")
	}
	return rec, nil
}
