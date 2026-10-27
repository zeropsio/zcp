package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// retireEvents is one ordered log across the dev service's ssh, the platform
// and the mount, so a test can pin what happened before what.
type retireEvents struct {
	mu  sync.Mutex
	log []string
}

func (e *retireEvents) add(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.log = append(e.log, event)
}

func (e *retireEvents) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.log)
}

func (e *retireEvents) index(prefix string) int {
	return slices.IndexFunc(e.all(), func(s string) bool { return strings.HasPrefix(s, prefix) })
}

// retireSSH is the dev service and the pool's lease. The census, the image
// read and the env read answer as the read side's fixture does; the retire's
// own scripts — the bundle, its stream, the fingerprint and the cleanup — run
// in a local shell against repo, a real repository standing for /var/www,
// with the scratch directory under work. The lease answers its probe unless
// leaseDown. afterStream runs once, right after the bundle streamed; onBundle
// runs when the bundle script starts.
type retireSSH struct {
	*poolMigrateSSH
	repo      string
	events    *retireEvents
	leaseDown bool
	// leases are the aliases of further leases that answer like apppool-1,
	// down those that do not.
	leases      []string
	down        map[string]bool
	afterStream func()
	onBundle    func()
	// onLeaseProbe runs when the lease's probe does: while the guards read.
	onLeaseProbe func()
}

const retireLeaseAlias = "apppool-1"

func (s *retireSSH) local(ctx context.Context, command string) *exec.Cmd {
	command = strings.ReplaceAll(command, "'"+topology.CodeMountRoot+"'", "'"+s.repo+"'")
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	return cmd
}

func (s *retireSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	switch {
	case hostname == retireLeaseAlias || slices.Contains(s.leases, hostname):
		s.events.add("ssh lease: " + command)
		if s.leaseDown || s.down[hostname] {
			refused := "ssh: connect to host " + hostname + " port 22: Connection refused"
			return []byte(refused), &platform.SSHExecError{Hostname: hostname, Output: refused, Err: errors.New("exit status 255")}
		}
		if command == "true" {
			if s.onLeaseProbe != nil {
				s.onLeaseProbe()
			}
			return nil, nil
		}
		return nil, errors.New("no supervisor answer in this test")
	case strings.Contains(command, "'zcp-bundle 1'"), strings.Contains(command, "'zcp-fingerprint 1'"), strings.HasPrefix(command, "rm -rf "):
		kind := "cleanup"
		switch {
		case strings.Contains(command, "'zcp-bundle 1'"):
			kind = "bundle"
			if s.onBundle != nil {
				s.onBundle()
			}
		case strings.Contains(command, "'zcp-fingerprint 1'"):
			kind = "fingerprint"
		}
		s.events.add("ssh " + kind)
		out, err := s.local(ctx, command).CombinedOutput()
		if err != nil {
			return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
		}
		return out, nil
	}
	return s.poolMigrateSSH.ExecSSH(ctx, hostname, command)
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *retireSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *retireSSH) StreamSSH(ctx context.Context, hostname, command string, stdout io.Writer) error {
	s.events.add("ssh stream")
	cmd := s.local(ctx, command)
	cmd.Stdout = stdout
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if s.afterStream != nil {
		s.afterStream()
		s.afterStream = nil
	}
	if err != nil {
		return &platform.SSHExecError{Hostname: hostname, Output: stderr.String(), Err: err}
	}
	return nil
}

// retireClient is the platform with the dev service's state by id under the
// test's control: live, deleting or gone, and what a delete leaves.
type retireClient struct {
	*platform.Mock
	events      *retireEvents
	mu          sync.Mutex
	devState    string
	afterDelete string
	deleted     []string
	// onDelete runs once a delete is sent; procStatus answers a process's
	// status by id over the mock's own.
	onDelete   func()
	procStatus map[string]string
}

const (
	devLive     = "live"
	devDeleting = "deleting"
	devGone     = "gone"
)

func (c *retireClient) GetService(ctx context.Context, id string) (*platform.ServiceStack, error) {
	if id == "svc-dev" {
		c.mu.Lock()
		state := c.devState
		c.mu.Unlock()
		switch state {
		case devGone:
			return nil, platform.NewPlatformError(platform.ErrServiceNotFound, "service not found", "")
		case devDeleting:
			svc, err := c.Mock.GetService(ctx, id)
			if err != nil {
				return nil, err
			}
			out := *svc
			out.Status = "DELETING"
			return &out, nil
		}
	}
	return c.Mock.GetService(ctx, id)
}

func (c *retireClient) DeleteService(ctx context.Context, id string) (*platform.Process, error) {
	c.events.add("delete " + id)
	c.mu.Lock()
	c.deleted = append(c.deleted, id)
	if id == "svc-dev" {
		c.devState = c.afterDelete
	}
	onDelete := c.onDelete
	c.mu.Unlock()
	if onDelete != nil {
		onDelete()
	}
	return c.Mock.DeleteService(ctx, id)
}

func (c *retireClient) GetProcess(ctx context.Context, id string) (*platform.Process, error) {
	c.mu.Lock()
	status, set := c.procStatus[id]
	c.mu.Unlock()
	if set {
		return &platform.Process{ID: id, ActionName: "delete", Status: status}, nil
	}
	return c.Mock.GetProcess(ctx, id)
}

func (c *retireClient) setProcStatus(id, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.procStatus == nil {
		c.procStatus = map[string]string{}
	}
	c.procStatus[id] = status
}

func (c *retireClient) deletes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.deleted)
}

// retireMounter is the zcp side's mount of the dev service.
type retireMounter struct {
	events     *retireEvents
	mu         sync.Mutex
	mounted    map[string]bool
	unmountErr error
}

func (m *retireMounter) state(host string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mounted[host]
}

func (m *retireMounter) CheckMount(_ context.Context, path string) (platform.MountState, error) {
	if m.state(filepath.Base(path)) {
		return platform.MountStateActive, nil
	}
	return platform.MountStateNotMounted, nil
}

func (m *retireMounter) Mount(_ context.Context, hostname, _ string) error {
	m.events.add("mount " + hostname)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mounted[hostname] = true
	return nil
}

func (m *retireMounter) Unmount(_ context.Context, hostname, _ string) error {
	m.events.add("unmount " + hostname)
	if m.unmountErr != nil {
		return m.unmountErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mounted[hostname] = false
	return nil
}

func (m *retireMounter) ForceUnmount(ctx context.Context, hostname, path string) error {
	return m.Unmount(ctx, hostname, path)
}
func (m *retireMounter) IsWritable(context.Context, string) (bool, error)        { return true, nil }
func (m *retireMounter) ListMountDirs(context.Context, string) ([]string, error) { return nil, nil }
func (m *retireMounter) HasUnit(context.Context, string) (bool, error)           { return false, nil }
func (m *retireMounter) CleanupUnit(context.Context, string) error               { return nil }

// retireSwarm is the pool's orchestrator: it lists its containers.
// retireSwarm is the pool's orchestrator as the retire reads it: its listing,
// a read by id, and the renew of a lease — answered as live (2026-10-24): a
// container listed reserved renews, a free one is refused unknown-lease, a
// missing one container-not-found; renewErr, when set, answers every renew.
type retireSwarm struct {
	containers []platform.SwarmContainer
	renewErr   error
}

var errRetireSwarmUnused = errors.New("the retire only lists the pool's containers")

func (s retireSwarm) Pool(context.Context) (platform.SwarmPool, error) {
	return platform.SwarmPool{}, errRetireSwarmUnused
}
func (s retireSwarm) Containers(context.Context) ([]platform.SwarmContainer, error) {
	return s.containers, nil
}
func (s retireSwarm) Acquire(context.Context, platform.SwarmAcquireRequest) (platform.SwarmAcquired, error) {
	return platform.SwarmAcquired{}, errRetireSwarmUnused
}
func (s retireSwarm) Release(context.Context, string, string, bool) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, errRetireSwarmUnused
}
func (s retireSwarm) Renew(ctx context.Context, id, _ string) (platform.SwarmContainer, error) {
	if s.renewErr != nil {
		return platform.SwarmContainer{}, s.renewErr
	}
	c, err := s.Container(ctx, id)
	if err != nil {
		return platform.SwarmContainer{}, err
	}
	if c.LockState != "reserved" {
		return platform.SwarmContainer{}, &platform.SwarmError{Status: http.StatusBadRequest, Code: "containerAction", Reason: platform.SwarmReasonUnknownLease,
			Message: "no reservation of this container is held by that lease - acquire it again"}
	}
	return c, nil
}
func (s retireSwarm) Delete(context.Context, string, string) error { return errRetireSwarmUnused }
func (s retireSwarm) Rollout(context.Context) (platform.SwarmRollout, error) {
	return platform.SwarmRollout{}, errRetireSwarmUnused
}
func (s retireSwarm) Create(context.Context, int) ([]platform.SwarmContainer, error) {
	return nil, errRetireSwarmUnused
}
func (s retireSwarm) Container(_ context.Context, id string) (platform.SwarmContainer, error) {
	for _, c := range s.containers {
		if c.ID == id {
			return c, nil
		}
	}
	return platform.SwarmContainer{}, &platform.SwarmError{Status: http.StatusBadRequest, Code: "containerAction", Reason: platform.SwarmReasonContainerNotFound,
		Message: "container not found"}
}
func (s retireSwarm) Stop(context.Context, string, string) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, errRetireSwarmUnused
}
func (s retireSwarm) Start(context.Context, string, string) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, errRetireSwarmUnused
}

// retireGit runs git in dir with a fixed identity and no user config.
func retireGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// retireRepo is the dev service's repository: main committed and pushed to a
// local origin, a stash entry and an unstaged edit — work the bundle keeps.
func retireRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "www")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	retireGit(t, root, "init", "-q", "-b", "main")
	write("app.txt", "v1\n")
	retireGit(t, root, "add", "-A")
	retireGit(t, root, "commit", "-q", "-m", "c1")
	retireGit(t, base, "init", "-q", "--bare", "-b", "main", filepath.Join(base, "origin.git"))
	retireGit(t, root, "remote", "add", "origin", filepath.Join(base, "origin.git"))
	retireGit(t, root, "push", "-q", "-u", "origin", "main")
	write("app.txt", "v1\nstashed\n")
	retireGit(t, root, "stash", "push", "-q", "-m", "kept-for-later")
	write("app.txt", "v1\nunstaged\n")
	return root
}

// writeRetireLeaseFile records zcp's lease apppool-1 of the pool on main of
// the dev's origin.
func writeRetireLeaseFile(t *testing.T, stateDir string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := `{"poolHostname":"apppool","poolServiceId":"svc-pool","containerId":"c-1","containerNumber":1,` +
		`"containerHostname":"node-id-1.runtime.apppool.zerops","lease":"secret-lease-value","branch":"main",` +
		`"repo":"https://github.com/example/app.git","acquiredAt":"2026-10-24T09:30:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, retireLeaseAlias+".json"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
}

// retireRig is a pair ready to retire: the pool provisioned, holding the dev
// service's env and running the commit's pool setup; zcp holding a lease of
// it on main that answers; the dev mounted, clean by its census, its
// repository real.
type retireRig struct {
	f       poolMigrateFixture
	client  *retireClient
	ssh     *retireSSH
	mounter *retireMounter
	swarm   ops.SwarmFactory
	events  *retireEvents
	work    string
}

func newRetireRig(t *testing.T, mutate func(*poolMigrateFixture)) *retireRig {
	t.Helper()
	events := &retireEvents{}
	f := poolImageFixture(t, func(f *poolMigrateFixture) {
		poolImageRunning("2026-10-24T09:00:00Z")(f)
		if mutate != nil {
			mutate(f)
		}
	})
	f.mock.WithProcess(&platform.Process{ID: "proc-delete-svc-dev", Status: platform.ProcessStatusFinished})
	writeRetireLeaseFile(t, f.stateDir)
	rig := &retireRig{
		f:       f,
		client:  &retireClient{Mock: f.mock, events: events, devState: devLive, afterDelete: devGone},
		ssh:     &retireSSH{poolMigrateSSH: f.ssh, repo: retireRepo(t), events: events},
		mounter: &retireMounter{events: events, mounted: map[string]bool{"appdev": true}},
		events:  events,
		work:    filepath.Join(t.TempDir(), "zcp-pool-migrate"),
	}
	rig.swarm = func(string, string) ops.SwarmAPI {
		return retireSwarm{containers: []platform.SwarmContainer{{ID: "c-1", Number: 1, Hostname: "node-id-1.runtime.apppool.zerops", LockState: "reserved"}}}
	}
	return rig
}

func (r *retireRig) deps() poolRetireDeps {
	d := r.f.deps()
	d.client = r.client
	d.ssh = r.ssh
	return poolRetireDeps{
		poolMigrateDeps: d,
		mounter:         r.mounter,
		swarm:           r.swarm,
		leaseEnv: func(key string) (string, bool) {
			if key == "apppool_API_TOKEN" {
				return "pool-api-token", true
			}
			return "", false
		},
		workDir: r.work,
	}
}

func retireAck() *DestructiveAck {
	return &DestructiveAck{Operation: poolRetireOperation, AcknowledgedTargets: []string{"appdev"}}
}

func retireInput(ack bool, force bool) WorkflowInput {
	in := WorkflowInput{Workflow: "pool-migrate", Action: "retire", Service: "appdev"}
	if ack {
		in.ConfirmDestructive = retireAck()
	}
	if force {
		in.Force = FlexBool(true)
	}
	return in
}

// retire calls action="retire" and answers its JSON — a result or an error
// wire — and whether it was an error.
func (r *retireRig) retire(t *testing.T, input WorkflowInput) (map[string]any, bool) {
	t.Helper()
	return r.retireCtx(context.Background(), t, input)
}

// retireCtx is retire under ctx: a call whose context ends mid-poll.
func (r *retireRig) retireCtx(ctx context.Context, t *testing.T, input WorkflowInput) (map[string]any, bool) {
	t.Helper()
	result, typed, err := poolMigrateRetireRun(ctx, r.deps(), input)
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if typed != nil {
		t.Fatalf("typed output must stay nil, got %T", typed)
	}
	text := getTextContent(t, result)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return out, result.IsError
}

func retireRecordOf(t *testing.T, stateDir string) *poolRetireRecord {
	t.Helper()
	rec, err := readPoolRetireRecord(stateDir, "appdev")
	if err != nil {
		t.Fatalf("read the retire record: %v", err)
	}
	return rec
}

func retireBlock(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	block, ok := out["retire"].(map[string]any)
	if !ok {
		t.Fatalf("no retire block in %v", out)
	}
	return block
}

func retireStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// assertNothingRetired pins that a call changed nothing: no record, no
// delete, no unmount, no bundle, the pair still keyed by the dev.
func (r *retireRig) assertNothingRetired(t *testing.T) {
	t.Helper()
	if rec, _ := readPoolRetireRecord(r.f.stateDir, "appdev"); rec != nil {
		t.Errorf("a retire record was written: %+v", rec)
	}
	if got := r.client.deletes(); len(got) > 0 {
		t.Errorf("deleted %q", got)
	}
	for _, e := range []string{"unmount", "ssh bundle", "ssh stream"} {
		if r.events.index(e) >= 0 {
			t.Errorf("%s happened: %q", e, r.events.all())
		}
	}
	if meta, _ := workflow.FindServiceMeta(r.f.stateDir, "appdev"); meta == nil || meta.Hostname != "appdev" || meta.DevIsPool() {
		t.Errorf("the pair is no longer keyed by appdev: %+v", meta)
	}
}

func TestPoolMigrateRetire_DiagnoseThenConfirm(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	hqState := hqPairStatePath(r.f.stateDir, "appdev")
	if err := os.MkdirAll(filepath.Dir(hqState), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hqState, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	out, isErr := r.retire(t, retireInput(false, false))
	if !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("first call = %v, want DIAGNOSIS_REQUIRED", out)
	}
	wd, _ := out["wouldDestroy"].(map[string]any)
	if wd["operation"] != poolRetireOperation || !slices.Equal(retireStrings(wd["targets"]), []string{"appdev"}) {
		t.Fatalf("wouldDestroy = %v", wd)
	}
	loss, _ := wd["wouldDestroy"].(map[string]any)
	if !slices.Equal(retireStrings(loss["serviceStacks"]), []string{"appdev"}) || !slices.Equal(retireStrings(loss["serviceIds"]), []string{"svc-dev"}) {
		t.Fatalf("loss = %v, want the dev service by name and the id it is deleted by", loss)
	}
	if notKept := strings.Join(retireStrings(loss["notKept"]), " "); !strings.Contains(notKept, "subdomain") {
		t.Errorf("notKept = %q, want the dev's own URL named", notKept)
	}
	if retry, _ := wd["retryCall"].(map[string]any); retry["tool"] != "zerops_workflow" {
		t.Errorf("retryCall = %v", wd["retryCall"])
	}
	if s, _ := out["suggestion"].(string); !strings.Contains(s, "agreed") || !strings.Contains(s, poolRetireOperation) {
		t.Errorf("suggestion = %q, want it to ask for the person's agreement by name and name the acknowledgment", s)
	}
	r.assertNothingRetired(t)

	out, isErr = r.retire(t, retireInput(true, false))
	if isErr || out["status"] != "step" || out["step"] != "retired" {
		t.Fatalf("confirmed call = %v, want the retired step", out)
	}
	if got := r.client.deletes(); !slices.Equal(got, []string{"svc-dev"}) {
		t.Fatalf("deletes = %q, want svc-dev once", got)
	}
	rec := retireRecordOf(t, r.f.stateDir)
	if rec.Phase != poolRetireRetired || rec.DevServiceID != "svc-dev" || rec.Pool != "apppool" || rec.Bundle == nil {
		t.Fatalf("record = %+v", rec)
	}
	info, err := os.Stat(rec.Bundle.Path)
	if err != nil || info.Mode().Perm() != 0o600 || !strings.HasPrefix(rec.Bundle.Path, filepath.Join(r.f.stateDir, "migrate")) {
		t.Fatalf("bundle %s: %v %v, want a 0600 file under the state dir's migrate/", rec.Bundle.Path, info, err)
	}
	meta, err := workflow.FindServiceMeta(r.f.stateDir, "appstage")
	if err != nil || meta == nil || meta.Hostname != "apppool" || !meta.DevIsPool() || meta.DevPool.MigratedFrom.Hostname != "appdev" || meta.DevPool.ServiceID != "svc-pool" {
		t.Fatalf("the stage's pair = %+v, want it keyed by the pool, moved from appdev", meta)
	}
	if m, _ := workflow.FindServiceMeta(r.f.stateDir, "appdev"); m != nil {
		t.Errorf("a pair is still keyed by appdev: %+v", m)
	}
	if _, err := os.Stat(hqState); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the dev's HQ backoff record outlives it: %v", err)
	}
	block := retireBlock(t, out)
	if restore := retireStrings(block["restore"]); len(restore) == 0 || !strings.Contains(strings.Join(restore, "\n"), rec.Bundle.Path) {
		t.Errorf("restore = %q, want the steps naming the bundle", restore)
	}
	if gaps := strings.Join(retireStrings(block["gaps"]), " "); !strings.Contains(gaps, "recipe") || !strings.Contains(gaps, "launch") {
		t.Errorf("gaps = %q, want the recipe and launch follow-ups named", gaps)
	}
}

func TestPoolMigrateRetire_FenceBeforeBundle(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	var devHeld, poolHeld bool
	r.ssh.onBundle = func() {
		ctx := context.Background()
		if release, err := workflow.LockPair(ctx, r.f.stateDir, "appdev", 0); err == nil {
			release()
		} else {
			devHeld = errors.Is(err, workflow.ErrPairBusy)
		}
		if release, err := workflow.LockPair(ctx, r.f.stateDir, "apppool", 0); err == nil {
			release()
		} else {
			poolHeld = errors.Is(err, workflow.ErrPairBusy)
		}
	}
	if out, isErr := r.retire(t, retireInput(true, false)); isErr || out["step"] != "retired" {
		t.Fatalf("retire = %v", out)
	}
	order := []string{"unmount appdev", "ssh bundle", "ssh stream", "ssh fingerprint", "delete svc-dev"}
	last := -1
	for _, e := range order {
		i := r.events.index(e)
		if i < 0 || i < last {
			t.Fatalf("events = %q, want %q in that order", r.events.all(), order)
		}
		last = i
	}
	if !devHeld || !poolHeld {
		t.Errorf("pair locks during the bundle: appdev held %v, apppool held %v; want both", devHeld, poolHeld)
	}
}

func TestPoolMigrateRetire_FingerprintChanged_Aborts(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	r.ssh.afterStream = func() {
		if err := os.WriteFile(filepath.Join(r.ssh.repo, "app.txt"), []byte("v1\nwritten after the bundle\n"), 0o644); err != nil {
			t.Error(err)
		}
	}
	out, isErr := r.retire(t, retireInput(true, false))
	if isErr || out["status"] != "aborted" {
		t.Fatalf("retire = %v, want aborted", out)
	}
	if reasons := poolMigrateReasons(out); !slices.Equal(reasons, []string{"fingerprint-changed"}) {
		t.Fatalf("reasons = %q", reasons)
	}
	if msg := poolMigrateRefusalMessage(out, "fingerprint-changed"); !strings.Contains(msg, r.ssh.repo) {
		t.Errorf("message = %q, want it to name the checkout that moved", msg)
	}
	if got := r.client.deletes(); len(got) > 0 {
		t.Fatalf("deleted %q after the fingerprint changed", got)
	}
	rec := retireRecordOf(t, r.f.stateDir)
	if rec.Phase != poolRetireAborted || rec.AbortReason == "" || rec.Bundle == nil {
		t.Fatalf("record = %+v, want aborted with its reason and the bundle kept", rec)
	}
	meta, _ := workflow.FindServiceMeta(r.f.stateDir, "appstage")
	if meta == nil || meta.Hostname != "appdev" || meta.DevIsPool() {
		t.Fatalf("the pair after an abort = %+v, want it keyed by appdev again", meta)
	}
	if !r.mounter.state("appdev") {
		t.Error("the dev's mount was not put back")
	}
	// A new retire starts a new record.
	r.ssh.afterStream = nil
	if out, _ := r.retire(t, retireInput(true, false)); out["step"] != "retired" {
		t.Fatalf("a retire after the abort = %v, want it retired", out)
	}
}

func TestPoolMigrateRetire_DeletesByRecordedID(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	// The lagging search lists another service under the dev's hostname.
	r.f.mock.WithServices([]platform.ServiceStack{
		poolMigrateRuntime("appdev", "svc-impostor"),
		poolMigrateRuntime("appstage", "svc-stage"),
		poolMigratePool("apppool", "svc-pool"),
	})
	if out, isErr := r.retire(t, retireInput(true, false)); isErr || out["step"] != "retired" {
		t.Fatalf("retire = %v", out)
	}
	if got := r.client.deletes(); !slices.Equal(got, []string{"svc-dev"}) {
		t.Fatalf("deletes = %q, want only the id read directly", got)
	}
}

func TestPoolMigrateRetire_UnsavedWork_NeedsForceAfterTheBundle(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, func(f *poolMigrateFixture) { f.ssh.output = poolMigrateUnsavedCensus() })

	out, isErr := r.retire(t, retireInput(false, false))
	if !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("diagnose = %v", out)
	}
	wd, _ := out["wouldDestroy"].(map[string]any)
	loss, _ := wd["wouldDestroy"].(map[string]any)
	unsaved := strings.Join(retireStrings(loss["unsavedWork"]), "\n")
	for _, want := range []string{"stash@{0}", "feat", ".env.local"} {
		if !strings.Contains(unsaved, want) {
			t.Errorf("unsavedWork = %q, want %q named", unsaved, want)
		}
	}
	if loss["uncommittedCode"] != true {
		t.Errorf("uncommittedCode = %v", loss["uncommittedCode"])
	}

	out, _ = r.retire(t, retireInput(true, false))
	if out["status"] != "retire-refused" || !slices.Equal(poolMigrateReasons(out), []string{"unsaved-work"}) {
		t.Fatalf("ack without force = %v, want refused for the unsaved work", out)
	}
	if msg := poolMigrateRefusalMessage(out, "unsaved-work"); !strings.Contains(msg, "force") {
		t.Errorf("message = %q, want force named", msg)
	}
	r.assertNothingRetired(t)

	out, isErr = r.retire(t, retireInput(true, true))
	if isErr || out["step"] != "retired" {
		t.Fatalf("forced = %v", out)
	}
	if r.events.index("ssh stream") > r.events.index("delete svc-dev") {
		t.Fatalf("the bundle was not written before the delete: %q", r.events.all())
	}
	block := retireBlock(t, out)
	if block["forced"] != true {
		t.Errorf("forced = %v", block["forced"])
	}
	kept := strings.Join(retireStrings(block["keptInBundle"]), "\n")
	if !strings.Contains(kept, "stash@{0}") || strings.Contains(kept, ".env.local") {
		t.Errorf("keptInBundle = %q, want the stash and never the .env file", kept)
	}
	if notKept := strings.Join(retireStrings(block["notKept"]), "\n"); !strings.Contains(notKept, ".env.local") {
		t.Errorf("notKept = %q, want the .env file named", notKept)
	}
}

// TestPoolMigrateRetire_Submodule_NotKept_NeedsForce: a submodule is a
// repository of its own the bundle does not carry, and work inside it — a
// stash, a commit on a branch of its own — leaves the superproject clean. So a
// census naming one holds the retire as unsaved work: the refusal and the
// diagnosis name the submodule with what to do (push its work to its own
// remote), and only the person's agreement (force) goes past it, the record
// naming the submodule as not kept — never as kept in the bundle (PM-3, PM-6).
func TestPoolMigrateRetire_Submodule_NotKept_NeedsForce(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, func(f *poolMigrateFixture) {
		f.ssh.output = poolMigrateCensusFor("main", "origin", "main", "", "TG vendor/lib", "TS 1 .M SC.. 160000 160000 160000 aaaa bbbb vendor/lib")
	})

	out, isErr := r.retire(t, retireInput(false, false))
	if !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("diagnose = %v", out)
	}
	wd, _ := out["wouldDestroy"].(map[string]any)
	loss, _ := wd["wouldDestroy"].(map[string]any)
	if unsaved := strings.Join(retireStrings(loss["unsavedWork"]), "\n"); !strings.Contains(unsaved, "vendor/lib") {
		t.Errorf("unsavedWork = %q, want the submodule named", unsaved)
	}

	out, _ = r.retire(t, retireInput(true, false))
	if out["status"] != "retire-refused" || !slices.Equal(poolMigrateReasons(out), []string{"unsaved-work"}) {
		t.Fatalf("ack without force = %v, want refused for the submodule", out)
	}
	if msg := poolMigrateRefusalMessage(out, "unsaved-work"); !strings.Contains(msg, "vendor/lib") || !strings.Contains(msg, "its own remote") {
		t.Errorf("message = %q, want the submodule named with pushing to its own remote", msg)
	}
	r.assertNothingRetired(t)

	out, isErr = r.retire(t, retireInput(true, true))
	if isErr || out["step"] != "retired" {
		t.Fatalf("forced = %v", out)
	}
	block := retireBlock(t, out)
	if kept := strings.Join(retireStrings(block["keptInBundle"]), "\n"); strings.Contains(kept, "vendor/lib") {
		t.Errorf("keptInBundle = %q names the submodule, which the bundle does not carry", kept)
	}
	if notKept := strings.Join(retireStrings(block["notKept"]), "\n"); !strings.Contains(notKept, "vendor/lib") {
		t.Errorf("notKept = %q, want the submodule named", notKept)
	}
}

func TestPoolMigrateRetire_ReflogOnlyCommits_NeverHoldTheRetire(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, func(f *poolMigrateFixture) {
		f.ssh.output = poolMigrateCensusFor("main", "origin", "main", "",
			"RN 2", "RS 3333333333333333333333333333333333333333\tamended away", "RS 4444444444444444444444444444444444444444\treset away")
		// The census counts them as unsaved; the retire does not.
		f.ssh.output = strings.Replace(f.ssh.output, "RN 0\n", "", 1)
	})
	out, isErr := r.retire(t, retireInput(true, false))
	if isErr || out["step"] != "retired" {
		t.Fatalf("retire without force = %v, want retired: every reflog tip rides the bundle", out)
	}
	kept := strings.Join(retireStrings(retireBlock(t, out)["keptInBundle"]), "\n")
	if !strings.Contains(kept, "2 commits only a reflog reaches") {
		t.Errorf("keptInBundle = %q, want the reflog-only commits named as kept in the bundle", kept)
	}
}

func TestPoolMigrateRetire_GuardFails_Refused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*poolMigrateFixture)
		rig    func(*retireRig)
		reason string
		want   string
	}{
		{name: "live process", mutate: func(f *poolMigrateFixture) {
			f.processes = append(f.processes, platform.Process{ID: "proc-restart", ActionName: "stack.restart", Status: "RUNNING",
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-dev", Name: "appdev"}}, Created: "2026-10-24T09:00:00Z"})
		}, reason: "dev-busy", want: "proc-restart"},
		{name: "deleting by id", rig: func(r *retireRig) { r.client.devState = devDeleting }, reason: "dev-deleting", want: "DELETING"},
		{name: "env reference", mutate: func(f *poolMigrateFixture) {
			f.env["svc-stage"] = append(f.env["svc-stage"], platform.ServiceEnvVar{ID: "s-ref", Key: "API_URL", Content: "http://${appdev_hostname}:3000", Type: platform.ServiceEnvUser})
		}, reason: "env-reference", want: "appstage API_URL"},
		{name: "domain", rig: func(r *retireRig) {
			r.f.mock.WithPublicHTTPRoutings(platform.PublicHTTPRouting{
				Domains:   []platform.PublicHTTPDomain{{Name: "dev.example.com"}},
				Locations: []platform.PublicHTTPLocation{{Path: "/", Port: 3000, ServiceID: "svc-dev"}},
			})
		}, reason: "domain", want: "dev.example.com"},
		{name: "active launch", rig: func(r *retireRig) {
			if err := writeLaunchState(r.f.stateDir, &launchState{
				LaunchID: "l-1", SourceProjectID: "proj-1", TargetProjectName: "prod", TargetServiceHostname: "appdev",
				Status: topology.LaunchProductionStatus("launching"), CreatedAt: time.Now(), LastUpdate: time.Now(),
			}); err != nil {
				t.Fatal(err)
			}
		}, reason: "launch-active", want: "prod"},
		{name: "no lease", rig: func(r *retireRig) {
			if err := os.Remove(filepath.Join(r.f.stateDir, "leases", retireLeaseAlias+".json")); err != nil {
				t.Fatal(err)
			}
		}, reason: "no-lease", want: "zerops_lease"},
		{name: "lease does not answer", rig: func(r *retireRig) { r.ssh.leaseDown = true }, reason: "lease-unverified", want: "Connection refused"},
		{name: "lease released elsewhere", rig: func(r *retireRig) {
			r.swarm = func(string, string) ops.SwarmAPI {
				return retireSwarm{containers: []platform.SwarmContainer{{ID: "c-1", Number: 1, LockState: "free"}}}
			}
		}, reason: "no-lease", want: "apppool-1"},
		{name: "no orchestrator", rig: func(r *retireRig) { r.swarm = nil }, reason: "lease-unreadable", want: "orchestrator"},
		{name: "image not done", mutate: func(f *poolMigrateFixture) { poolImageRunning("2026-10-24T07:00:00Z")(f) }, reason: "not-ready", want: "image"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRetireRig(t, tt.mutate)
			if tt.rig != nil {
				tt.rig(r)
			}
			// The guards are re-read in the confirming call: an
			// acknowledgment never carries a retire past one.
			out, isErr := r.retire(t, retireInput(true, true))
			if isErr || out["status"] != "retire-refused" {
				t.Fatalf("retire = %v, want retire-refused", out)
			}
			if !slices.Contains(poolMigrateReasons(out), tt.reason) {
				t.Fatalf("reasons = %q, want %q", poolMigrateReasons(out), tt.reason)
			}
			if msg := poolMigrateRefusalMessage(out, tt.reason); !strings.Contains(msg, tt.want) {
				t.Errorf("%s message = %q, want %q named", tt.reason, msg, tt.want)
			}
			r.assertNothingRetired(t)
		})
	}
}

// TestPoolMigrateRetire_OwnPortSubdomain_NoDomainRefusal: the routing list
// carries the dev's own port subdomain (`<host>-<sub>-<port>.prg1.zerops.app`)
// beside any custom domain. It dies with the dev, as the diagnosis says, so
// the retire never refuses on it — nor on a meta intent "domain" an earlier
// reconcile recorded from it while the live routing names no custom domain.
func TestPoolMigrateRetire_OwnPortSubdomain_NoDomainRefusal(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, func(f *poolMigrateFixture) {
		f.meta.SetPublicAccess("appdev", topology.PublicAccessRecord{Intent: topology.PublicAccessDomain})
	})
	r.f.mock.WithPublicHTTPRoutings(platform.PublicHTTPRouting{
		Domains:   []platform.PublicHTTPDomain{{Name: "appdev-38ff-3000.prg1.zerops.app"}},
		Locations: []platform.PublicHTTPLocation{{Path: "/", Port: 3000, ServiceID: "svc-dev"}},
	})
	out, isErr := r.retire(t, retireInput(false, false))
	if !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("first call = %v, want DIAGNOSIS_REQUIRED, no domain refusal", out)
	}
	r.assertNothingRetired(t)
}

// TestPoolMigrateRetire_LeaseUnverified_RefusedNeverReached: the orchestrator
// does not answer whether the lease is still zcp's (docs/spec-workflows.md §8
// DP-2): the container may be another holder's by now, so the retire refuses
// before it reaches the lease over ssh — the probe would read, and the retire
// would rely on, a container zcp cannot vouch for.
func TestPoolMigrateRetire_LeaseUnverified_RefusedNeverReached(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	r.swarm = func(string, string) ops.SwarmAPI {
		return retireSwarm{
			containers: []platform.SwarmContainer{{ID: "c-1", Number: 1, Hostname: "node-id-1.runtime.apppool.zerops", LockState: "reserved"}},
			renewErr:   &platform.SwarmError{Status: http.StatusBadGateway, Message: "bad gateway"},
		}
	}
	out, isErr := r.retire(t, retireInput(true, true))
	if isErr || out["status"] != "retire-refused" {
		t.Fatalf("retire = %v, want retire-refused", out)
	}
	if !slices.Contains(poolMigrateReasons(out), poolRetireRefuseLeaseUnreadable) {
		t.Fatalf("reasons = %q, want %q", poolMigrateReasons(out), poolRetireRefuseLeaseUnreadable)
	}
	if msg := poolMigrateRefusalMessage(out, poolRetireRefuseLeaseUnreadable); !strings.Contains(msg, retireLeaseAlias) {
		t.Errorf("message = %q, want %s named", msg, retireLeaseAlias)
	}
	for _, e := range r.events.all() {
		if strings.HasPrefix(e, "ssh lease") {
			t.Errorf("the retire reached a lease zcp cannot verify over ssh: %q", e)
		}
	}
	r.assertNothingRetired(t)
}

func TestPoolMigrateRetire_UnmountFails_AbortsAndRestores(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	r.mounter.unmountErr = errors.New("device busy")
	out, _ := r.retire(t, retireInput(true, false))
	if out["status"] != "aborted" || !slices.Equal(poolMigrateReasons(out), []string{"fence-failed"}) {
		t.Fatalf("retire = %v, want aborted at the fence", out)
	}
	if r.events.index("ssh bundle") >= 0 || len(r.client.deletes()) > 0 {
		t.Fatalf("the retire went on past a failed fence: %q", r.events.all())
	}
	if rec := retireRecordOf(t, r.f.stateDir); rec.Phase != poolRetireAborted {
		t.Fatalf("record = %+v", rec)
	}
}

func TestPoolMigrateRetire_DeleteNotTaken_CallAgain(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	r.client.afterDelete = devLive
	out, isErr := r.retire(t, retireInput(true, false))
	if isErr || out["step"] != "retiring" {
		t.Fatalf("retire = %v, want retiring while the dev still reads live", out)
	}
	if why, _ := out["nextUnavailable"].(string); !strings.Contains(why, "call again") {
		t.Errorf("nextUnavailable = %q", why)
	}
	if rec := retireRecordOf(t, r.f.stateDir); rec.Phase != poolRetireDeleting {
		t.Fatalf("record = %+v, want it kept at deleting", rec)
	}
	r.client.afterDelete = devGone
	if out, _ := r.retire(t, retireInput(true, false)); out["step"] != "retired" {
		t.Fatalf("the next call = %v, want retired", out)
	}
	if got := r.client.deletes(); !slices.Equal(got, []string{"svc-dev", "svc-dev"}) {
		t.Errorf("deletes = %q, want the recorded id deleted again", got)
	}
}

// TestPoolMigrateRetire_ResumeWhileTheDeleteIsQueued_NeverAborts: a call that
// sent the dev's delete and gave up before it ran — its context ended
// mid-poll, the delete queued behind another process — leaves the record at
// deleting, the delete's process id in it. A resume while that process is
// still PENDING says call again: it never re-fences, re-reads the fingerprint
// or aborts, so a write meanwhile never re-keys the pair onto a dev the queued
// delete then removes, nor says "Nothing was deleted" (PM-7). Once the
// delete ran, the next call finishes; a delete whose process FAILED took
// nothing, and the resume goes on as before — a moved repository aborts.
func TestPoolMigrateRetire_ResumeWhileTheDeleteIsQueued_NeverAborts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// unrecorded drops the process id from the record, as a cut
		// between the send and the save leaves it: the project's live
		// processes answer instead.
		unrecorded bool
	}{{name: "recorded"}, {name: "unrecorded", unrecorded: true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRetireRig(t, nil)
			const procID = "proc-delete-svc-dev"
			r.client.setProcStatus(procID, platform.ProcessStatusPending)
			r.client.afterDelete = devLive
			ctx, cancel := context.WithCancel(context.Background())
			r.client.onDelete = cancel
			out, _ := r.retireCtx(ctx, t, retireInput(true, false))
			cancel()
			r.client.onDelete = nil
			if out["step"] != "retiring" {
				t.Fatalf("first call = %v, want retiring while the delete is queued", out)
			}
			rec := retireRecordOf(t, r.f.stateDir)
			if rec.Phase != poolRetireDeleting {
				t.Fatalf("record phase = %s, want deleting", rec.Phase)
			}
			if tc.unrecorded {
				rec.DeletesSent = map[string]string{"svc-dev": ""}
				if err := savePoolRetireRecord(r.f.stateDir, rec); err != nil {
					t.Fatal(err)
				}
				r.f.mock.WithProjectProcesses([]platform.Process{{ID: procID, ActionName: "delete", Status: platform.ProcessStatusPending,
					ServiceStacks: []platform.ServiceStackRef{{ID: "svc-dev"}}}})
			} else if rec.DeletesSent["svc-dev"] != procID {
				t.Fatalf("record deletesSent = %v, want the delete's process id", rec.DeletesSent)
			}
			// A write lands while the delete is queued.
			if err := os.WriteFile(filepath.Join(r.ssh.repo, "app.txt"), []byte("v1\nwritten while the delete is queued\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			r.events.log = nil
			out, _ = r.retire(t, retireInput(true, false))
			if out["status"] == poolMigrateStatusAborted || out["step"] != "retiring" {
				t.Fatalf("resume while the delete is queued = %v, want retiring, never aborted", out)
			}
			if why, _ := out["nextUnavailable"].(string); !strings.Contains(why, procID) && !tc.unrecorded {
				t.Errorf("nextUnavailable = %q, want the queued delete's process named", why)
			}
			if rec := retireRecordOf(t, r.f.stateDir); rec.Phase != poolRetireDeleting {
				t.Fatalf("record phase = %s, want it kept at deleting", rec.Phase)
			}
			if meta, _ := workflow.FindServiceMeta(r.f.stateDir, "appstage"); meta == nil || meta.Hostname != "apppool" {
				t.Fatalf("the pair = %+v, want it still keyed by the pool", meta)
			}
			if r.events.index("mount appdev") >= 0 || r.events.index("ssh fingerprint") >= 0 || len(r.client.deletes()) != 1 {
				t.Fatalf("the resume acted while the delete was queued: %q, deletes %q", r.events.all(), r.client.deletes())
			}
			// The queued delete runs: the next call finishes.
			r.client.setProcStatus(procID, platform.ProcessStatusFinished)
			r.f.mock.WithProjectProcesses(nil)
			r.client.mu.Lock()
			r.client.devState = devGone
			r.client.mu.Unlock()
			if out, _ := r.retire(t, retireInput(true, false)); out["step"] != "retired" {
				t.Fatalf("after the delete ran = %v, want retired", out)
			}
		})
	}
	t.Run("failed", func(t *testing.T) {
		t.Parallel()
		r := newRetireRig(t, nil)
		r.client.setProcStatus("proc-delete-svc-dev", platform.ProcessStatusFailed)
		r.client.afterDelete = devLive
		if out, _ := r.retire(t, retireInput(true, false)); out["step"] != "retiring" {
			t.Fatalf("first call = %v, want retiring", out)
		}
		if err := os.WriteFile(filepath.Join(r.ssh.repo, "app.txt"), []byte("v1\nwritten after the delete failed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _ := r.retire(t, retireInput(true, false))
		if out["status"] != poolMigrateStatusAborted {
			t.Fatalf("resume after a failed delete with the repository moved = %v, want aborted", out)
		}
	})
}

// seedRetireRecord writes the record a retire left in phase, with the bundle
// a real bundle of the rig's repository; from rewritten on, the pair is
// keyed by the pool as the retire leaves it.
func (r *retireRig) seedRetireRecord(t *testing.T, phase string) {
	t.Helper()
	r.seedRetireRecordBundle(t, phase, true)
}

// seedRetireRecordBundle is seedRetireRecord with the bundle or without one:
// a record whose bundle was cut short, or never recorded, holds none.
func (r *retireRig) seedRetireRecordBundle(t *testing.T, phase string, withBundle bool) {
	t.Helper()
	var b *ops.PoolMigrateBundle
	if withBundle {
		var err error
		b, err = ops.BuildPoolMigrateBundle(context.Background(), r.ssh, ops.PoolMigrateBundleInput{
			Dev: "appdev", Root: topology.CodeMountRoot, WorkDir: r.work, Path: poolRetireBundlePath(r.f.stateDir, "appdev"),
		})
		if err != nil {
			t.Fatalf("seed bundle: %v", err)
		}
	}
	rec := &poolRetireRecord{
		Dev: "appdev", DevServiceID: "svc-dev", Pool: "apppool", PoolServiceID: "svc-pool", PoolSetup: "apppool",
		Stages: []string{"appstage"}, Phase: phase, StartedAt: "2026-10-24T10:00:00Z", Bundle: b,
	}
	if phase == poolRetireRewritten || phase == poolRetireDeleting {
		if _, err := workflow.MigratePairToPool(r.f.stateDir, workflow.PoolPairMove{Dev: "appdev", Pool: "apppool", PoolServiceID: "svc-pool", PoolSetup: "apppool"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := savePoolRetireRecord(r.f.stateDir, rec); err != nil {
		t.Fatal(err)
	}
	r.events.log = nil
}

// TestPoolMigrateRetire_ResumeTable walks every phase a retire can stop in
// against every state the dev reads by id: live resumes from the phase,
// DELETING keeps the record and says call again, gone finishes — the pair
// re-keyed onto the pool whatever the phase, never left under a dead key.
func TestPoolMigrateRetire_ResumeTable(t *testing.T) {
	t.Parallel()
	phases := []string{poolRetireStarted, poolRetireFenced, poolRetireBundled, poolRetireRewritten, poolRetireDeleting}
	for _, phase := range phases {
		for _, state := range []string{devLive, devDeleting, devGone} {
			t.Run(phase+"/"+state, func(t *testing.T) {
				t.Parallel()
				r := newRetireRig(t, nil)
				r.seedRetireRecord(t, phase)
				r.client.devState = state

				out, isErr := r.retire(t, retireInput(true, false))
				rec := retireRecordOf(t, r.f.stateDir)
				switch state {
				case devDeleting:
					if isErr || out["step"] != "retiring" || rec.Phase != phase {
						t.Fatalf("out %v, record phase %s; want retiring, the record kept at %s", out, rec.Phase, phase)
					}
					if len(r.client.deletes()) > 0 || r.events.index("ssh") >= 0 || r.events.index("unmount") >= 0 {
						t.Fatalf("a DELETING dev was acted on: %q, deletes %q", r.events.all(), r.client.deletes())
					}
					return
				case devGone:
					if len(r.client.deletes()) > 0 {
						t.Fatalf("a gone dev was deleted again: %q", r.client.deletes())
					}
				case devLive:
					if got := r.client.deletes(); !slices.Equal(got, []string{"svc-dev"}) {
						t.Fatalf("deletes = %q, want the recorded id", got)
					}
					if r.events.index("unmount appdev") < 0 && phase != poolRetireDeleting && phase != poolRetireRewritten {
						t.Errorf("no re-fence on resume: %q", r.events.all())
					}
					if r.events.index("ssh fingerprint") < 0 {
						t.Errorf("no fingerprint re-check before the delete: %q", r.events.all())
					}
					// From started, fenced or bundled the phase is done
					// again — a new bundle; past bundled the recorded
					// bundle stands, checked by the fingerprint.
					rebundled := r.events.index("ssh bundle") >= 0
					if want := phase == poolRetireStarted || phase == poolRetireFenced || phase == poolRetireBundled; rebundled != want {
						t.Errorf("a new bundle on the resume from %s = %v, want %v: %q", phase, rebundled, want, r.events.all())
					}
				}
				if isErr || out["step"] != "retired" || rec.Phase != poolRetireRetired {
					t.Fatalf("out %v, record %+v; want retired", out, rec)
				}
				meta, _ := workflow.FindServiceMeta(r.f.stateDir, "appstage")
				if meta == nil || meta.Hostname != "apppool" || !meta.DevIsPool() {
					t.Fatalf("the stage's pair = %+v, want it keyed by the pool", meta)
				}
			})
		}
	}
}

// TestPoolMigrateRetire_CutBeforeTheRekey_ResumeReadsTheGuards: each phase is
// saved before its work, so a retire cut between saving rewritten and the
// re-key leaves a record at rewritten over a pair still keyed by the dev.
// Nothing about the pair moved yet, so the resume is still before the split:
// it reads every guard again — the lease of the pool held and verified among
// them (PM-12) — exactly as a resume from bundled does, and a guard that fails
// now aborts the retire, deleting nothing (PM-7).
func TestPoolMigrateRetire_CutBeforeTheRekey_ResumeReadsTheGuards(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	r.seedRetireRecord(t, poolRetireBundled)
	rec := retireRecordOf(t, r.f.stateDir)
	rec.Phase = poolRetireRewritten
	if err := savePoolRetireRecord(r.f.stateDir, rec); err != nil {
		t.Fatal(err)
	}
	// The lease ended since: zcp holds no lease of the pool any more.
	if err := os.Remove(filepath.Join(r.f.stateDir, "leases", retireLeaseAlias+".json")); err != nil {
		t.Fatal(err)
	}

	out, _ := r.retire(t, retireInput(true, false))
	if got := r.client.deletes(); len(got) > 0 {
		t.Fatalf("deletes = %q, want none: the resume never read that the pool's lease ended", got)
	}
	if out["status"] != "aborted" || !slices.Contains(poolMigrateReasons(out), "no-lease") {
		t.Fatalf("resume = %v, want aborted for the lease that is no longer held", out)
	}
	if meta, _ := workflow.FindServiceMeta(r.f.stateDir, "appstage"); meta == nil || meta.Hostname != "appdev" || meta.DevIsPool() {
		t.Fatalf("the stage's pair = %+v, want it still keyed by appdev", meta)
	}
}

// TestPoolMigrateRetire_CutWhileBundling_ResumeBundlesAgain replays a retire
// cut while its bundle ran: each phase is saved before its work, so the
// record reads bundled and holds no bundle. The acknowledged resume does the
// phase again — a new bundle — and retires the dev, never aborting on the
// bundle the cut left unwritten (PM-7).
func TestPoolMigrateRetire_CutWhileBundling_ResumeBundlesAgain(t *testing.T) {
	t.Parallel()
	first := newRetireRig(t, nil)
	var cut []byte
	first.ssh.onBundle = func() {
		if cut == nil {
			cut, _ = os.ReadFile(poolRetireRecordPath(first.f.stateDir, "appdev"))
		}
	}
	if out, isErr := first.retire(t, retireInput(true, false)); isErr || out["step"] != "retired" {
		t.Fatalf("retire = %v", out)
	}
	var atCut poolRetireRecord
	if err := json.Unmarshal(cut, &atCut); err != nil {
		t.Fatalf("the record while the bundle ran: %v\n%s", err, cut)
	}
	if atCut.Phase != poolRetireBundled || atCut.Bundle != nil {
		t.Fatalf("the record while the bundle ran = phase %s, bundle %+v; want bundled and no bundle yet", atCut.Phase, atCut.Bundle)
	}

	r := newRetireRig(t, nil)
	path := poolRetireRecordPath(r.f.stateDir, "appdev")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, cut, 0o600); err != nil {
		t.Fatal(err)
	}
	out, isErr := r.retire(t, retireInput(true, false))
	rec := retireRecordOf(t, r.f.stateDir)
	if isErr || out["step"] != "retired" || rec.Phase != poolRetireRetired {
		t.Fatalf("resume = %v, record phase %s (%s); want retired from a new bundle", out, rec.Phase, rec.AbortReason)
	}
	if rec.Bundle == nil {
		t.Fatal("the retired record names no bundle")
	}
	if info, err := os.Stat(rec.Bundle.Path); err != nil || info.Size() != rec.Bundle.Bytes {
		t.Fatalf("bundle %s: %v %v, want the file the record names", rec.Bundle.Path, info, err)
	}
	order := []string{"unmount appdev", "ssh bundle", "ssh stream", "ssh fingerprint", "delete svc-dev"}
	last := -1
	for _, e := range order {
		i := r.events.index(e)
		if i < 0 || i < last {
			t.Fatalf("events = %q, want %q in that order", r.events.all(), order)
		}
		last = i
	}
	if got := r.client.deletes(); !slices.Equal(got, []string{"svc-dev"}) {
		t.Errorf("deletes = %q, want the recorded id once", got)
	}
}

// TestPoolMigrateRetire_ResumeWithoutAVerifiedBundle_BundlesAgain: a resume
// whose record holds no verified bundle — none recorded, or its file gone —
// takes one again before anything is deleted, whatever phase the record
// reads; nothing is deleted that no bundle on this container holds (PM-3,
// PM-7).
func TestPoolMigrateRetire_ResumeWithoutAVerifiedBundle_BundlesAgain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		phase      string
		withBundle bool // a bundle recorded, its file then removed
	}{
		{name: "bundled, none recorded", phase: poolRetireBundled},
		{name: "rewritten, none recorded", phase: poolRetireRewritten},
		{name: "deleting, none recorded", phase: poolRetireDeleting},
		{name: "deleting, the recorded bundle's file gone", phase: poolRetireDeleting, withBundle: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRetireRig(t, nil)
			r.seedRetireRecordBundle(t, tt.phase, tt.withBundle)
			if tt.withBundle {
				if err := os.Remove(poolRetireBundlePath(r.f.stateDir, "appdev")); err != nil {
					t.Fatal(err)
				}
			}
			out, isErr := r.retire(t, retireInput(true, false))
			rec := retireRecordOf(t, r.f.stateDir)
			if isErr || out["step"] != "retired" || rec.Phase != poolRetireRetired {
				t.Fatalf("resume = %v, record phase %s (%s); want retired from a new bundle", out, rec.Phase, rec.AbortReason)
			}
			bundled, deleted := r.events.index("ssh stream"), r.events.index("delete svc-dev")
			if bundled < 0 || deleted < bundled {
				t.Fatalf("events = %q, want a new bundle streamed before the delete", r.events.all())
			}
			if rec.Bundle == nil {
				t.Fatal("the retired record names no bundle")
			}
			if info, err := os.Stat(rec.Bundle.Path); err != nil || info.Size() != rec.Bundle.Bytes {
				t.Fatalf("bundle %s: %v %v, want the file the record names", rec.Bundle.Path, info, err)
			}
			meta, _ := workflow.FindServiceMeta(r.f.stateDir, "appstage")
			if meta == nil || meta.Hostname != "apppool" || !meta.DevIsPool() {
				t.Fatalf("the stage's pair = %+v, want it keyed by the pool", meta)
			}
		})
	}
}

// TestPoolMigrateRetire_Resume_NeedsTheAcknowledgment: a live dev's retire is
// resumed only on the acknowledged call; without it the call is the
// diagnosis again and changes nothing.
func TestPoolMigrateRetire_Resume_NeedsTheAcknowledgment(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	r.seedRetireRecord(t, poolRetireDeleting)
	out, isErr := r.retire(t, retireInput(false, false))
	if !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("resume without the acknowledgment = %v", out)
	}
	if len(r.client.deletes()) > 0 || retireRecordOf(t, r.f.stateDir).Phase != poolRetireDeleting {
		t.Fatal("an unacknowledged call resumed the retire")
	}
}

func TestPoolMigrate_ReadSide_LeaseAndRetireSteps(t *testing.T) {
	t.Parallel()
	step := func(t *testing.T, r *retireRig, service string) map[string]any {
		t.Helper()
		in := WorkflowInput{Workflow: "pool-migrate", Service: service}
		d := r.deps().poolMigrateDeps
		result, _, err := poolMigrateRun(context.Background(), d, in)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(getTextContent(t, result)), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	t.Run("no lease: lease", func(t *testing.T) {
		t.Parallel()
		r := newRetireRig(t, nil)
		if err := os.Remove(filepath.Join(r.f.stateDir, "leases", retireLeaseAlias+".json")); err != nil {
			t.Fatal(err)
		}
		out := step(t, r, "appdev")
		next, _ := out["next"].(map[string]any)
		args, _ := next["args"].(map[string]any)
		if out["step"] != "lease" || next["tool"] != "zerops_lease" || args["action"] != "acquire" || args["pool"] != "apppool" || args["branch"] != "main" {
			t.Fatalf("out = %v, want the lease step naming the acquire", out)
		}
		if states := poolImageStepStates(out); states["move"] != "done" {
			t.Errorf("steps = %v, want move done with nothing to move", states)
		}
	})
	t.Run("unsaved work: move", func(t *testing.T) {
		t.Parallel()
		r := newRetireRig(t, func(f *poolMigrateFixture) { f.ssh.output = poolMigrateUnsavedCensus() })
		if err := os.Remove(filepath.Join(r.f.stateDir, "leases", retireLeaseAlias+".json")); err != nil {
			t.Fatal(err)
		}
		out := step(t, r, "appdev")
		if out["step"] != "move" || out["next"] != nil {
			t.Fatalf("out = %v, want the move step", out)
		}
		if why, _ := out["nextUnavailable"].(string); !strings.Contains(why, "zerops_lease") {
			t.Errorf("nextUnavailable = %q, want the lease named for when the work is moved", why)
		}
	})
	t.Run("a lease on the work's branch: ready-to-retire", func(t *testing.T) {
		t.Parallel()
		r := newRetireRig(t, nil)
		out := step(t, r, "appdev")
		next, _ := out["next"].(map[string]any)
		args, _ := next["args"].(map[string]any)
		if out["step"] != "ready-to-retire" || next["tool"] != "zerops_workflow" || args["action"] != "retire" || args["service"] != "appdev" {
			t.Fatalf("out = %v, want ready-to-retire naming the retire", out)
		}
		if !strings.Contains(out["guidance"].(string), "pool-retire") {
			t.Error("the retire atom does not ride the answer")
		}
		r.assertNothingRetired(t)
	})
	t.Run("a retire in flight: retiring", func(t *testing.T) {
		t.Parallel()
		r := newRetireRig(t, nil)
		r.seedRetireRecord(t, poolRetireRewritten)
		for _, service := range []string{"appdev", "appstage"} {
			out := step(t, r, service)
			if out["step"] != "retiring" || retireBlock(t, out)["phase"] != poolRetireRewritten {
				t.Fatalf("%s: out = %v, want retiring from the record", service, out)
			}
		}
		if len(r.client.deletes()) > 0 || r.events.index("ssh") >= 0 {
			t.Fatal("the read side acted on the retire")
		}
	})
	t.Run("retired: retired", func(t *testing.T) {
		t.Parallel()
		r := newRetireRig(t, nil)
		if out, _ := r.retire(t, retireInput(true, false)); out["step"] != "retired" {
			t.Fatalf("retire = %v", out)
		}
		out := step(t, r, "appstage")
		if out["step"] != "retired" || retireBlock(t, out)["bundle"] == nil {
			t.Fatalf("out = %v, want retired with the bundle named", out)
		}
	})
}

func TestDeleteTool_RetireRecord_PointsAtRetire(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		phase   string
		refused bool
	}{
		{poolRetireStarted, true}, {poolRetireBundled, true}, {poolRetireRewritten, true}, {poolRetireDeleting, true},
		{poolRetireAborted, false}, {poolRetireRetired, false},
	} {
		t.Run(tt.phase, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if err := savePoolRetireRecord(stateDir, &poolRetireRecord{Dev: "appdev", DevServiceID: "svc-dev", Pool: "apppool", Phase: tt.phase}); err != nil {
				t.Fatal(err)
			}
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-dev", Name: "appdev"}}).
				WithProcess(&platform.Process{ID: "proc-delete-svc-dev", Status: platform.ProcessStatusFinished})
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDelete(srv, mock, "proj-1", stateDir, nil, runtime.Info{})
			result := callTool(t, srv, "zerops_delete", map[string]any{"serviceHostname": "appdev", "consent": "appdev"})
			text := getTextContent(t, result)
			if tt.refused {
				if !result.IsError || !strings.Contains(text, `action=\"retire\"`) {
					t.Fatalf("delete during a %s retire = %s, want a refusal pointing at the retire", tt.phase, text)
				}
				if mock.CallCounts["DeleteService"] > 0 {
					t.Fatal("the service was deleted")
				}
				return
			}
			if result.IsError || mock.CallCounts["DeleteService"] != 1 {
				t.Fatalf("delete beside a %s record = %s, want it to delete as always", tt.phase, text)
			}
		})
	}
}

// TestPoolMigrateRetire_RekeyRefused_AbortsCleanly: a pool another pair is
// keyed by is refused before the retire starts (pool-hostname-taken, nothing
// written). One that becomes another pair's while the guards read stops the
// re-key, changing nothing: the retire aborts with the dev's pair as it was
// and deletes nothing.
func TestPoolMigrateRetire_RekeyRefused_AbortsCleanly(t *testing.T) {
	t.Parallel()
	otherPair := func(t *testing.T, stateDir string) {
		t.Helper()
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname: "apppool", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-01T00:00:00Z",
			DevPool: &workflow.DevPoolRef{ServiceID: "svc-pool", MigratedFrom: &workflow.MigratedDev{Hostname: "otherdev"}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("another pair's before the retire", func(t *testing.T) {
		t.Parallel()
		r := newRetireRig(t, nil)
		otherPair(t, r.f.stateDir)
		out, _ := r.retire(t, retireInput(true, false))
		if out["status"] != "not-a-candidate" || !slices.Equal(poolMigrateReasons(out), []string{"pool-hostname-taken"}) {
			t.Fatalf("retire = %v, want refused before it starts: the pool is another pair's", out)
		}
		r.assertNothingRetired(t)
	})

	t.Run("another pair's while the guards read", func(t *testing.T) {
		t.Parallel()
		r := newRetireRig(t, nil)
		r.ssh.onLeaseProbe = func() {
			otherPair(t, r.f.stateDir)
			r.ssh.onLeaseProbe = nil
		}
		out, isErr := r.retire(t, retireInput(true, false))
		if isErr || out["status"] != "aborted" || !slices.Equal(poolMigrateReasons(out), []string{"rekey-failed"}) {
			t.Fatalf("retire = %v, want aborted at the re-key", out)
		}
		if len(r.client.deletes()) > 0 {
			t.Fatal("deleted after a refused re-key")
		}
		if rec := retireRecordOf(t, r.f.stateDir); rec.Phase != poolRetireAborted {
			t.Fatalf("record = %+v, want aborted, never left in flight", rec)
		}
		if meta, _ := workflow.FindServiceMeta(r.f.stateDir, "appstage"); meta == nil || meta.Hostname != "appdev" || meta.DevIsPool() {
			t.Fatalf("the dev's pair = %+v, want it as it was", meta)
		}
	})
}

// TestPoolMigrateRetire_Dispatch_ReachesTheRetire pins that zerops_workflow
// action="retire" reaches the retire through the registered tool, with or
// without workflow="pool-migrate", and that the schema names it.
func TestPoolMigrateRetire_Dispatch_ReachesTheRetire(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	// No live env hands zcp the pool's bearer here: a token that reads the
	// pool's API_TOKEN in clear does.
	poolEnv, _ := r.f.mock.GetServiceEnv(context.Background(), "svc-pool")
	for i := range poolEnv {
		if poolEnv[i].Key == "API_TOKEN" {
			poolEnv[i].Content = "pool-api-token"
		}
	}
	r.f.mock.WithServiceEnv("svc-pool", poolEnv)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(r.f.stateDir, workflow.EnvContainer, nil)
	RegisterWorkflow(srv, r.client, nil, "proj-1", nil, engine, nil, r.f.stateDir, "zcp", r.mounter, r.ssh, r.f.rt, "", WithPoolOrchestratorsForWorkflow(r.swarm))
	for _, args := range []map[string]any{
		{"workflow": "pool-migrate", "action": "retire", "service": "appdev"},
		{"action": "retire", "service": "appdev"},
	} {
		result := callTool(t, srv, "zerops_workflow", args)
		text := getTextContent(t, result)
		if !result.IsError || !strings.Contains(text, platform.ErrDiagnosisRequired) {
			t.Fatalf("%v = %s, want the retire's diagnosis", args, text)
		}
	}
	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "export", "action": "retire", "service": "appdev"})
	if text := getTextContent(t, result); !result.IsError || !strings.Contains(text, "pool-migrate") {
		t.Fatalf("retire under another workflow = %s, want it refused naming pool-migrate", text)
	}
}

func TestWorkflowInputSchema_RetireNamed(t *testing.T) {
	t.Parallel()
	s := workflowInputSchema()
	for prop, want := range map[string]string{"action": "retire", "force": "action=retire", "confirmDestructive": `action=\"retire\"`} {
		p := s.Properties[prop]
		if p == nil {
			t.Fatalf("property %s missing", prop)
		}
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("%s = %s, want it to name %s", prop, b, want)
		}
	}
}

// TestPoolMigrateRetire_StartedElsewhereMeanwhile_NoSecondStart: a retire
// another call started while this one read its guards answers from that
// record once the pair is held — never a second record, never a second
// fence or bundle.
func TestPoolMigrateRetire_StartedElsewhereMeanwhile_NoSecondStart(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	r.ssh.onLeaseProbe = func() {
		if err := savePoolRetireRecord(r.f.stateDir, &poolRetireRecord{
			Dev: "appdev", DevServiceID: "svc-dev", Pool: "apppool", PoolServiceID: "svc-pool", Phase: poolRetireFenced, StartedAt: "2026-10-24T10:00:00Z",
		}); err != nil {
			t.Error(err)
		}
		r.ssh.onLeaseProbe = nil
	}
	out, isErr := r.retire(t, retireInput(true, false))
	if isErr || out["step"] != "retiring" {
		t.Fatalf("retire = %v, want retiring from the other call's record", out)
	}
	if rec := retireRecordOf(t, r.f.stateDir); rec.Phase != poolRetireFenced || rec.StartedAt != "2026-10-24T10:00:00Z" {
		t.Fatalf("record = %+v, want the other call's, untouched", rec)
	}
	if r.events.index("unmount") >= 0 || r.events.index("ssh bundle") >= 0 || len(r.client.deletes()) > 0 {
		t.Fatalf("a second retire started: %q", r.events.all())
	}
}

// TestPoolMigrateRetire_NameRetaken_RetiresTheNewService: a dev hostname
// retired once and then taken by a new service (another id) retires the new
// one. The old record answers nothing for it — not before the pair lock and
// not under it — so the new service is bundled and deleted by its own id, and
// the old dev's bundle, the only copy of its work, is kept apart with its
// record rather than overwritten by the new bundle (PM-7).
func TestPoolMigrateRetire_NameRetaken_RetiresTheNewService(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	oldBundle := filepath.Join(r.f.stateDir, "migrate", "appdev.bundle")
	if err := os.MkdirAll(filepath.Dir(oldBundle), 0o700); err != nil {
		t.Fatal(err)
	}
	const oldBytes = "the old dev's bundle"
	if err := os.WriteFile(oldBundle, []byte(oldBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := savePoolRetireRecord(r.f.stateDir, &poolRetireRecord{
		Dev: "appdev", DevServiceID: "svc-dev-old", Pool: "apppool", PoolServiceID: "svc-pool", PoolSetup: "apppool",
		Stages: []string{"appstage"}, Phase: poolRetireRetired, StartedAt: "2026-10-20T10:00:00Z",
		Bundle: &ops.PoolMigrateBundle{Path: oldBundle, Bytes: int64(len(oldBytes))},
	}); err != nil {
		t.Fatal(err)
	}

	out, isErr := r.retire(t, retireInput(true, false))
	if isErr || out["step"] != "retired" {
		t.Fatalf("retire = %v, want the retired step", out)
	}
	if got := r.client.deletes(); !slices.Equal(got, []string{"svc-dev"}) {
		t.Fatalf("deletes = %q, want the new appdev (svc-dev) deleted by its own id: the old record answered for it", got)
	}
	rec := retireRecordOf(t, r.f.stateDir)
	if rec.DevServiceID != "svc-dev" || rec.Phase != poolRetireRetired || rec.Bundle == nil {
		t.Fatalf("record = %+v, want the new service's retire, retired with its bundle", rec)
	}
	paths, _ := filepath.Glob(filepath.Join(r.f.stateDir, "migrate", "*.json"))
	var kept *poolRetireRecord
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var other poolRetireRecord
		if json.Unmarshal(raw, &other) == nil && other.DevServiceID == "svc-dev-old" {
			kept = &other
		}
	}
	if kept == nil || kept.Bundle == nil {
		t.Fatalf("the old dev's retire record is gone (records %q)", paths)
	}
	if kept.Bundle.Path == rec.Bundle.Path {
		t.Fatalf("the old record names the new bundle %s", kept.Bundle.Path)
	}
	if got, err := os.ReadFile(kept.Bundle.Path); err != nil || string(got) != oldBytes {
		t.Fatalf("the old dev's bundle at %s = %q (%v), want it kept as it was", kept.Bundle.Path, got, err)
	}
}

// TestPoolMigrateRetire_AbortedRecordOfAnotherID_KeptApart: an aborted
// retire keeps its bundle — the abort says so — and the dev it bundled may
// go later, outside zcp, its hostname then taken by a new service. A retire
// of that service keeps the aborted record of the old id and its bundle
// apart, as it keeps a retired one, never writing over what may be the only
// copy of the old dev's work (PM-7).
func TestPoolMigrateRetire_AbortedRecordOfAnotherID_KeptApart(t *testing.T) {
	t.Parallel()
	r := newRetireRig(t, nil)
	oldBundle := poolRetireBundlePath(r.f.stateDir, "appdev")
	if err := os.MkdirAll(filepath.Dir(oldBundle), 0o700); err != nil {
		t.Fatal(err)
	}
	const oldBytes = "the aborted retire's bundle of the old dev"
	if err := os.WriteFile(oldBundle, []byte(oldBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := savePoolRetireRecord(r.f.stateDir, &poolRetireRecord{
		Dev: "appdev", DevServiceID: "svc-dev-old", Pool: "apppool", PoolServiceID: "svc-pool", PoolSetup: "apppool",
		Stages: []string{"appstage"}, Phase: poolRetireAborted, StartedAt: "2026-10-20T10:00:00Z", AbortReason: "fingerprint-changed: moved",
		Bundle: &ops.PoolMigrateBundle{Path: oldBundle, Bytes: int64(len(oldBytes))},
	}); err != nil {
		t.Fatal(err)
	}

	if out, isErr := r.retire(t, retireInput(true, false)); isErr || out["step"] != "retired" {
		t.Fatalf("retire = %v, want the new service retired", out)
	}
	raw, err := os.ReadFile(filepath.Join(r.f.stateDir, "migrate", "appdev.svc-dev-old.json"))
	if err != nil {
		t.Fatalf("the aborted record of the old id was not kept apart: %v", err)
	}
	var kept poolRetireRecord
	if err := json.Unmarshal(raw, &kept); err != nil {
		t.Fatal(err)
	}
	if kept.Phase != poolRetireAborted || kept.Bundle == nil || kept.Bundle.Path == oldBundle {
		t.Fatalf("kept record = %s, want the aborted record, its bundle moved off %s", raw, oldBundle)
	}
	if got, err := os.ReadFile(kept.Bundle.Path); err != nil || string(got) != oldBytes {
		t.Fatalf("the old dev's bundle at %s = %q (%v), want it kept as it was", kept.Bundle.Path, got, err)
	}
}

// TestPoolMigrateRetire_NameRetaken_KeepsTheVolumeBundleApart: a variant-B
// retire forced past a code volume whose checkout moved holds two bundles —
// <dev>.bundle and <dev>.volume.bundle, the newer and only copy of the late
// writes. Kept apart for a new service under the old hostname, both move to
// the old id's names and both records' restore steps name them there: a
// later variant-B retire of the new service writes neither over (PM-7).
func TestPoolMigrateRetire_NameRetaken_KeepsTheVolumeBundleApart(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	dir := filepath.Join(stateDir, "migrate")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	devBundle, volBundle := poolRetireBundlePath(stateDir, "appdev"), filepath.Join(dir, "appdev"+poolRetireVolumeBundleSuffix)
	for path, body := range map[string]string{devBundle: "the old dev's bundle", volBundle: "the old volume's late writes"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rec := &poolRetireRecord{
		Dev: "appdev", DevServiceID: "svc-dev-old", Phase: poolRetireRetired, StartedAt: "2026-10-20T10:00:00Z",
		Bundle:  &ops.PoolMigrateBundle{Path: devBundle, Bytes: 20},
		Restore: []string{"git clone " + devBundle + " restored"},
		CodeVolume: &poolRetireVolume{Hostname: "vol", ServiceID: "svc-vol-old", MountPath: "/var/www/ws",
			Bundle:  &ops.PoolMigrateBundle{Path: volBundle, Bytes: 28},
			Restore: []string{"git clone " + volBundle + " restored"}},
	}
	if err := keepRetiredApart(stateDir, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(volBundle); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the old volume bundle still lies at %s, where a new retire writes its own (%v)", volBundle, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "appdev.svc-dev-old.json"))
	if err != nil {
		t.Fatalf("the kept record: %v", err)
	}
	var kept poolRetireRecord
	if err := json.Unmarshal(raw, &kept); err != nil {
		t.Fatal(err)
	}
	if kept.CodeVolume == nil || kept.CodeVolume.Bundle == nil {
		t.Fatalf("kept record = %s, want its code volume's bundle", raw)
	}
	want := filepath.Join(dir, "appdev.svc-dev-old"+poolRetireVolumeBundleSuffix)
	if kept.CodeVolume.Bundle.Path != want {
		t.Errorf("kept volume bundle path = %s, want %s", kept.CodeVolume.Bundle.Path, want)
	}
	if got, err := os.ReadFile(want); err != nil || string(got) != "the old volume's late writes" {
		t.Errorf("the old volume bundle at %s = %q (%v), want it moved there as it was", want, got, err)
	}
	if steps := strings.Join(kept.CodeVolume.Restore, "\n"); !strings.Contains(steps, want) || strings.Contains(steps, volBundle+" ") {
		t.Errorf("kept volume restore = %q, want it naming %s", steps, want)
	}
}
