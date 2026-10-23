package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
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
	AcquiredAt        string `json:"acquiredAt,omitempty"`
	Note              string `json:"note,omitempty"`
}

// leaseTokenKey is the pool service's own env var that its orchestrator takes
// as the bearer token.
const leaseTokenKey = "API_TOKEN"

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
// name, and reads its API_TOKEN through the ops env read.
func resolvePool(ctx context.Context, d LeaseDeps, hostname string) (*resolvedPool, error) {
	svc, err := LookupService(ctx, d.Client, d.ProjectID, hostname)
	if err != nil {
		return nil, err
	}
	typ := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	if !topology.IsPoolType(typ) {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Service '%s' is %s, not a Swarm pool", hostname, typ),
			"zerops_lease leases a container of a pool (swarm@1 or swarm-vm@1); pass the pool's hostname")
	}
	envs, err := FetchServiceEnv(ctx, d.Client, svc.ID)
	if err != nil {
		return nil, fmt.Errorf("read %s env: %w", hostname, err)
	}
	scalars, masked := EnvScalars(envs)
	token := scalars[leaseTokenKey]
	if token == "" {
		reason := leaseTokenKey + " is not set on the pool"
		for _, k := range masked {
			if k == leaseTokenKey {
				reason = leaseTokenKey + " is masked for this token"
			}
		}
		return nil, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Cannot reach pool '%s': %s", hostname, reason),
			"The orchestrator needs the pool's own "+leaseTokenKey+" as its bearer token")
	}
	return &resolvedPool{svc: svc, api: d.Swarm(orchestratorURL(hostname), token)}, nil
}

// LeaseList reports the leases of one pool, or of every pool the project
// holds or zcp has a record for when none is named. The orchestrator is the
// truth: a record whose container is missing or not reserved is lost (never
// re-leased), and a reserved container with no record is an unknown holder
// (acquire has no idempotency key, so a lost response would otherwise leak a
// container forever).
func LeaseList(ctx context.Context, d LeaseDeps, pool string) ([]LeaseView, error) {
	records, err := readLeaseRecords(d.StateDir)
	if err != nil {
		return nil, err
	}
	byPool := map[string][]leaseRecord{}
	for _, r := range records {
		byPool[r.PoolHostname] = append(byPool[r.PoolHostname], r)
	}
	if pool != "" {
		return listPoolLeases(ctx, d, pool, byPool[pool], true)
	}
	names := map[string]bool{}
	for name := range byPool {
		names[name] = true
	}
	services, err := ListProjectServices(ctx, d.Client, d.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	for _, svc := range services {
		if topology.IsPoolType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			names[svc.Name] = true
		}
	}
	var views []LeaseView
	for _, name := range slices.Sorted(maps.Keys(names)) {
		got, err := listPoolLeases(ctx, d, name, byPool[name], false)
		if err != nil {
			return nil, err
		}
		views = append(views, got...)
	}
	return views, nil
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
	containers, err := rp.api.Containers(ctx)
	if err != nil {
		return nil, leaseOrchestratorError(pool, "list containers", err)
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

// leaseOrchestratorError says what a failed orchestrator call means to the
// agent: a transport failure is the "outside the project" case, a refusal is
// the orchestrator's own answer.
func leaseOrchestratorError(pool, op string, err error) error {
	var pe *platform.PlatformError
	if errors.As(err, &pe) && pe.Code == platform.ErrNetworkError {
		return platform.NewPlatformError(platform.ErrNetworkError,
			fmt.Sprintf("Cannot %s on pool '%s': %s", op, pool, pe.Message),
			fmt.Sprintf("The orchestrator (%s) answers only inside the project: run zcp in the project's container, or connect the project VPN", orchestratorURL(pool)))
	}
	var se *platform.SwarmError
	if errors.As(err, &se) {
		out := platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Pool '%s' orchestrator refused (%s): %s", pool, op, se.Message), swarmSuggestion(se))
		out.APICode = se.Reason
		out.Cause = err
		return out
	}
	return fmt.Errorf("%s %s: %w", op, pool, err)
}

// swarmSuggestion is what to do about an orchestrator refusal.
func swarmSuggestion(se *platform.SwarmError) string {
	switch {
	case se.Status == http.StatusUnauthorized || se.Code == "notAuthorized":
		return "The pool's API_TOKEN was refused; re-read it with zerops_env get on the pool"
	case se.Retryable:
		return "The orchestrator marks this retryable: the same call may succeed in a moment"
	}
	return ""
}

// LeaseAcquire leases a container of the pool and puts its /var/www on the
// branch of repo (DP-6). It always asks for a clean, started container with no
// expiry (leaseTtlSeconds 0): an expiring lease frees the container silently
// and the next clean acquire wipes it, so the lease ends only by an explicit
// release. An empty repo is the pool's default repository (the first acquire
// with a repo records it); with neither, or with a repo or branch that is not
// usable, the acquire refuses before it takes a container. A checkout that
// fails hands the container straight back.
func LeaseAcquire(ctx context.Context, d LeaseDeps, pool, repo, branch string) (LeaseView, error) {
	rp, err := resolvePool(ctx, d, pool)
	if err != nil {
		return LeaseView{}, err
	}
	branch = cmp.Or(branch, leaseDefaultBranch)
	if err := validateLeaseBranch(branch); err != nil {
		return LeaseView{}, err
	}
	repoURL, becomesDefault, err := resolveLeaseRepo(d.StateDir, pool, repo)
	if err != nil {
		return LeaseView{}, err
	}
	got, err := rp.api.Acquire(ctx, platform.SwarmAcquireRequest{Start: true, Clean: true, LeaseTTLSeconds: 0})
	if err != nil {
		return LeaseView{}, leaseOrchestratorError(pool, "acquire", err)
	}
	rec := leaseRecord{
		PoolHostname: pool, PoolServiceID: rp.svc.ID,
		ContainerID: got.Container.ID, ContainerNumber: got.Container.Number, ContainerHostname: got.Container.Hostname,
		Lease: got.Lease.Secret(), Branch: branch, AcquiredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeLeaseRecord(d.StateDir, rec); err != nil {
		// Without a record the lease is unusable and, with no expiry, the
		// container would stay reserved for good: hand it straight back (it is
		// clean, so no reset).
		_, relErr := rp.api.Release(context.WithoutCancel(ctx), rec.ContainerID, rec.Lease, false)
		if relErr != nil {
			return LeaseView{}, fmt.Errorf("record lease %s: %w (and handing the container back failed: %s stays reserved on %s)", rec.alias(), err, rec.ContainerHostname, pool)
		}
		return LeaseView{}, fmt.Errorf("record lease %s: %w (the container was handed back)", rec.alias(), err)
	}
	v := rec.view(LeaseStateHeld)
	if note := d.Host.attach(ctx, d, rec); note != "" {
		// The lease is real and stays recorded; zcp init (or the next status)
		// finds the alias or mount missing and restores it.
		v.State = LeaseStateUnmounted
		v.Note = note
	}
	if d.SSH != nil {
		if err := checkoutLease(ctx, d, rec, repoURL, branch); err != nil {
			return LeaseView{}, abandonLease(ctx, d, rec, err)
		}
	}
	if becomesDefault {
		if err := storePoolDefaultRepo(d.StateDir, pool, repoURL); err != nil {
			v.Note = joinNotes(v.Note, fmt.Sprintf("the repository could not be recorded as the pool's default: %v", err))
		}
	}
	return v, nil
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
	AcquiredAt        string `json:"acquiredAt"`
}

func (r leaseRecord) alias() string { return leaseAlias(r.PoolHostname, r.ContainerNumber) }

// view is the record as the agent sees it: never the lease.
func (r leaseRecord) view(state string) LeaseView {
	return LeaseView{
		Alias: r.alias(), Pool: r.PoolHostname, ContainerHostname: r.ContainerHostname,
		Branch: r.Branch, State: state,
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

// LeaseRelease releases the lease addressed by alias and removes its record.
// Unless force is set it first reads the container over ssh and refuses while
// it holds commits on no remote branch or uncommitted/untracked files (DP-4);
// the refusal leaves the mount, the record and the lease as they were. Every
// release resets the container (processes end). A container ssh cannot reach
// is released as lost, unchecked. If the orchestrator fails the reservation
// is kept and so is the record. A lease the orchestrator no longer knows (it
// ended, or the container is gone) has nothing left to release: the record is
// dropped and the view says so.
func LeaseRelease(ctx context.Context, d LeaseDeps, alias string, force bool) (LeaseView, error) {
	rec, err := loadLeaseRecord(d.StateDir, alias)
	if err != nil {
		return LeaseView{}, err
	}
	var guard leaseGuardResult
	if !force {
		// The guard reaches the container through its alias: put back a
		// stanza another call or a redeploy lost before it runs.
		restoreNote := d.Host.refreshAliases(ctx, d)
		if guard, err = guardLeaseRelease(ctx, d, rec); err != nil {
			return LeaseView{}, err
		}
		guard.note = joinNotes(restoreNote, guard.note)
	}
	view, err := releaseAndDetach(ctx, d, rec, alias)
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
// record (every successful outcome drops it).
func releaseLease(ctx context.Context, d LeaseDeps, rec leaseRecord, alias string, reset bool) (LeaseView, error) {
	rp, err := resolvePool(ctx, d, rec.PoolHostname)
	if err != nil {
		var pe *platform.PlatformError
		if errors.As(err, &pe) && pe.Code == platform.ErrServiceNotFound {
			return dropLeaseRecord(d.StateDir, rec, LeaseStateLost, "the pool service no longer exists, so there was nothing to release; the record is dropped")
		}
		return LeaseView{}, err
	}
	if rp.svc.ID != rec.PoolServiceID {
		return dropLeaseRecord(d.StateDir, rec, LeaseStateLost, "the pool was re-created since this lease was taken, so there was nothing to release; the record is dropped")
	}
	if _, err := rp.api.Release(ctx, rec.ContainerID, rec.Lease, reset); err != nil {
		var se *platform.SwarmError
		if errors.As(err, &se) && (se.Reason == "unknown-lease" || se.Reason == "container-not-found") {
			return dropLeaseRecord(d.StateDir, rec, LeaseStateLost, "the orchestrator no longer held this container for this lease, so there was nothing to release; the record is dropped")
		}
		return LeaseView{}, leaseOrchestratorError(rec.PoolHostname, "release "+alias, err)
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
