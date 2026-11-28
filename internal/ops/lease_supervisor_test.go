// Tests for: ops/lease_supervisor.go, the lease cases of dev_server.go — a
// lease always has its supervisor (docs/spec-workflows.md §8 DP-13). A pool
// container released with reset=true comes back as its image and Zerops never
// runs its run.start again, so zcp starts `zcp-devsup run` itself: after an
// acquire's checkout, and once when a dev-server call on a lease finds none
// answering. A dev service is never started this way.
package ops

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

const (
	imageSupervisorVersion   = "v9.300.0"
	releaseSupervisorVersion = "v9.301.0"
	releaseSupervisorBytes   = "verified release executable"
)

type releaseLeaseContainer struct {
	installed      string
	running        string
	pid            int
	busy           bool
	restartsOnExit bool
	copyErr        error
	received       string
	probed         bool
	resumed        bool
}

// releaseLeaseSSH answers the executable's version separately from the
// running supervisor's. Only the stream sent over SSH can replace the former.
type releaseLeaseSSH struct {
	leaseSSH
	mu    sync.Mutex
	hosts map[string]*releaseLeaseContainer
}

func newReleaseLeaseSSH(hosts map[string]*releaseLeaseContainer) *releaseLeaseSSH {
	ssh := &releaseLeaseSSH{hosts: hosts}
	ssh.setHandler(ssh.answer)
	return ssh
}

func (c *releaseLeaseContainer) status(marker string) []byte {
	if c.running == "" {
		return []byte("@@zcp-devsup " + marker + " 2\n")
	}
	servers := "{}"
	if c.busy {
		servers = `{"web":{"status":"running","desired":"running","pid":42,"pgid":42,"source":"declared"}}`
	}
	return fmt.Appendf(nil, "{\"supervisor\":{\"version\":%q,\"pid\":%d},\"servers\":%s}\n@@zcp-devsup %s 0\n", c.running, c.pid, servers, marker)
}

func (s *releaseLeaseSSH) answer(host, command string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.hosts[host]
	if c == nil {
		return nil, fmt.Errorf("unknown container %s", host)
	}
	switch {
	case command == leaseSupervisorVersionRead:
		c.probed = true
		return fmt.Appendf(nil, "{\"version\":%q}\n@@zcp-devsup version 0\n", c.installed), nil
	case command == leaseSupervisorStatusRead:
		return c.status("status"), nil
	case strings.Contains(command, "@@zcp-devsup declaration"):
		return []byte("@@zcp-devsup declaration 1\n"), nil
	case strings.Contains(command, "zcp-devsup run"):
		if strings.Contains(command, "flock -w ") {
			return nil, errors.New("flock: unrecognized option: w")
		}
		if c.running != "" {
			return c.status("check"), nil
		}
		c.running, c.pid = c.installed, 21
		return append([]byte("@@zcp-devsup check 2\n"), c.status("started")...), nil
	case strings.Contains(command, ctlCall):
		return c.status("ctl"), nil
	case c.resumed && strings.Contains(command, leaseResumedMarker):
		return []byte(leaseResumedMarker + "\n"), nil
	}
	return nil, nil
}

func (s *releaseLeaseSSH) ExecSSHStdin(_ context.Context, host, command string, stdin []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.hosts[host]
	if c == nil {
		return nil, fmt.Errorf("unknown container %s", host)
	}
	c.received = string(stdin)
	if c.copyErr != nil {
		return nil, c.copyErr
	}
	if strings.Contains(command, "flock -w ") {
		return nil, errors.New("flock: unrecognized option: w")
	}
	publish := strings.Index(command, `mv -f "$tmp" "$bin"`)
	if c.received != releaseSupervisorBytes || publish < 0 {
		return nil, errors.New("release bytes were not installed at the image path")
	}
	if c.running != "" {
		stop := strings.Index(command, `kill -TERM "$pid"`)
		if stop < 0 {
			return nil, errors.New("image supervisor still answers")
		}
		if c.restartsOnExit {
			c.running, c.pid = c.installed, c.pid+1
			if publish < stop {
				c.running = releaseSupervisorVersion
			}
		} else {
			c.running, c.pid = "", 0
		}
	}
	c.installed = releaseSupervisorVersion
	return []byte(c.installed + "\n"), nil
}

func releaseSupervisorFixture(t *testing.T, cached bool) LeaseSupervisorRelease {
	t.Helper()
	r := NewLeaseSupervisorRelease(releaseSupervisorVersion)
	r.dir = t.TempDir()
	if cached {
		if err := os.WriteFile(r.path(), []byte(releaseSupervisorBytes), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func acquireWithReleaseSupervisor(t *testing.T, release LeaseSupervisorRelease, c *releaseLeaseContainer) LeaseView {
	t.Helper()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	fx.deps.SSH = newReleaseLeaseSSH(map[string]*releaseLeaseContainer{"devpool-3": c})
	fx.deps.SupervisorRelease = release
	view, err := LeaseAcquire(t.Context(), fx.deps, testPoolHost, testRepo, "feature")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	return view
}

// witness: internal/ops/lease_supervisor.go | steps = append(append(steps, publish), stop...) → steps = append(append(steps, stop...), publish)
func TestLeaseAcquire_LPSupAcquire_StartsReleaseSupervisor(t *testing.T) {
	t.Parallel()
	c := &releaseLeaseContainer{installed: imageSupervisorVersion, running: imageSupervisorVersion, pid: 11, restartsOnExit: true}
	view := acquireWithReleaseSupervisor(t, releaseSupervisorFixture(t, true), c)
	if view.State != LeaseStateHeld || view.SupervisorVersion != releaseSupervisorVersion || c.installed != releaseSupervisorVersion || c.running != releaseSupervisorVersion || c.pid != 12 {
		t.Errorf("acquire left %s reporting %q, installed %s, running %s (pid %d), note %q; want the platform restart to run the release",
			view.State, view.SupervisorVersion, c.installed, c.running, c.pid, view.Note)
	}
}

// witness: internal/ops/lease_supervisor.go | if installed.Version == want { → if installed.Version == "" {
func TestLeaseAcquire_LPSupSame_KeepsCurrentSupervisor(t *testing.T) {
	t.Parallel()
	c := &releaseLeaseContainer{installed: releaseSupervisorVersion, running: releaseSupervisorVersion, pid: 11}
	view := acquireWithReleaseSupervisor(t, releaseSupervisorFixture(t, true), c)
	if view.SupervisorVersion != releaseSupervisorVersion || c.received != "" || c.pid != 11 {
		t.Errorf("acquire reported %q, transferred %q and left pid %d; want the same release and process without a transfer",
			view.SupervisorVersion, c.received, c.pid)
	}
}

// witness: internal/ops/lease_group.go | if !m.checkedOut { → if m.reserved {
func TestLeaseAcquire_LPSupHeld_KeepsHeldSupervisor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		resumed bool
	}{{name: "reused checkout", resumed: true}, {name: "cut short checkout"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-3", 3)}
			recordLeaseAt(t, fx.stateDir, testPoolHost, "svc-pool", 3, "feature", testRepo, parkNow)
			c := &releaseLeaseContainer{installed: imageSupervisorVersion, running: imageSupervisorVersion, pid: 11, resumed: tc.resumed}
			fx.deps.SSH = newReleaseLeaseSSH(map[string]*releaseLeaseContainer{"devpool-3": c})
			fx.deps.SupervisorRelease = releaseSupervisorFixture(t, true)

			view, err := LeaseAcquire(t.Context(), fx.deps, testPoolHost, testRepo, "feature")
			if err != nil || view.State != LeaseStateHeld || view.Alias != "devpool-3" || view.Reused != tc.resumed {
				t.Fatalf("acquire of the held lease = %+v, %v", view, err)
			}
			if c.installed != imageSupervisorVersion || c.pid != 11 || c.received != "" || c.probed {
				t.Errorf("held container changed: %+v", c)
			}
		})
	}
}

// witness: internal/ops/lease_park.go | if restarted && d.SupervisorRelease.checker != nil { → if !restarted && d.SupervisorRelease.checker != nil {
func TestLeaseUnpark_LPSupUnpark_StartsReleaseSupervisor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		parked bool
		want   string
	}{
		{name: "parked", parked: true, want: releaseSupervisorVersion},
		{name: "already running", want: imageSupervisorVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pf := newParkFixture(t, tc.parked)
			c := &releaseLeaseContainer{installed: imageSupervisorVersion}
			if !tc.parked {
				c.running, c.pid = imageSupervisorVersion, 11
			}
			pf.deps.SSH = newReleaseLeaseSSH(map[string]*releaseLeaseContainer{"devpool-3": c})
			pf.deps.SupervisorRelease = releaseSupervisorFixture(t, true)

			view, err := pf.unpark(t)
			if err != nil || view.State != LeaseStateHeld || view.SupervisorVersion != tc.want || c.running != tc.want || c.installed != tc.want {
				t.Errorf("unpark left %s reporting %q and container %+v; want %s running, note %q, err %v",
					view.State, view.SupervisorVersion, c, tc.want, view.Note, err)
			}
		})
	}
}

// witness: internal/ops/lease_warm_clone.go | if d.SupervisorRelease.checker != nil { → if d.SupervisorRelease.checker == nil {
func TestLeaseAcquire_LPSupWarm_DeliversToWarmTopUp(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(3, "", true, false, 1)
	wf := newWarmFixture(t, orch)
	wf.collect = true
	warmHost := nodeHostname(4)
	warm := &releaseLeaseContainer{installed: imageSupervisorVersion, running: imageSupervisorVersion, pid: 12}
	wf.deps.SSH = newReleaseLeaseSSH(map[string]*releaseLeaseContainer{
		"devpool-3": {installed: imageSupervisorVersion},
		warmHost:    warm,
	})
	wf.deps.SupervisorRelease = releaseSupervisorFixture(t, true)

	wf.acquire(t, "feature")
	wf.runSpawned()

	free, err := orch.Container(t.Context(), "c-4")
	if err != nil || !isWarm(free) || warm.installed != releaseSupervisorVersion || warm.running != "" {
		t.Errorf("new warm container = %+v, %v, executable and process = %+v; want a free warm container with the release binary", free, err, warm)
	}
}

type releaseAssetTransport func(*http.Request) (*http.Response, error)

func (f releaseAssetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// witness: internal/ops/lease_group.go | m.view.Note = joinNotes(m.view.Note, scrubSecrets(leaseSupervisorSwapNote(err), leaseSecrets(d, m.pool))) → m.view.Note = joinNotes(m.view.Note, scrubSecrets(leaseSupervisorSwapNote(nil), leaseSecrets(d, m.pool)))
func TestLeaseAcquire_LPSupFallback_KeepsImageAndExplainsFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		cached bool
		want   string
	}{
		{name: "missing asset", want: "release asset answered HTTP 404"},
		{name: "copy failure", cached: true, want: "cannot copy the release binary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			release := releaseSupervisorFixture(t, tc.cached)
			release.checker.HTTPClient.Transport = releaseAssetTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody}, nil
			})
			c := &releaseLeaseContainer{installed: imageSupervisorVersion, running: imageSupervisorVersion, pid: 11}
			if tc.cached {
				c.copyErr = errors.New(tc.want)
			}
			view := acquireWithReleaseSupervisor(t, release, c)
			if view.State != LeaseStateHeld || view.SupervisorVersion != imageSupervisorVersion || c.installed != imageSupervisorVersion || c.pid != 11 ||
				!strings.Contains(view.Note, tc.want) {
				t.Errorf("acquire left %s reporting %q with image process %+v, note %q; want held on the image with %q",
					view.State, view.SupervisorVersion, c, view.Note, tc.want)
			}
		})
	}
}

// witness: internal/ops/lease_supervisor.go | if server.Status != devsup.StatusStopped || server.Desired != devsup.DesiredStopped || server.Pid != 0 || server.Pgid != 0 { → if server.Status == devsup.StatusStopped && server.Pgid != 0 {
func TestLeaseAcquire_LPSupActive_LeavesDeclaredServersRunning(t *testing.T) {
	t.Parallel()
	c := &releaseLeaseContainer{installed: imageSupervisorVersion, running: imageSupervisorVersion, pid: 11, busy: true}
	view := acquireWithReleaseSupervisor(t, releaseSupervisorFixture(t, true), c)
	if view.State != LeaseStateHeld || view.SupervisorVersion != imageSupervisorVersion || c.pid != 11 || c.received != "" ||
		!strings.Contains(view.Note, "existing supervisor has active dev servers") {
		t.Errorf("acquire left %s reporting %q with image process %+v and note %q; want its declared server left running",
			view.State, view.SupervisorVersion, c, view.Note)
	}
}

// witness: internal/ops/lease_supervisor.go | if !update.IsRelease(version) { → if version == "" {
func TestLeaseAcquire_LPSupDev_SkipsReleaseDelivery(t *testing.T) {
	t.Parallel()
	c := &releaseLeaseContainer{installed: imageSupervisorVersion}
	release := NewLeaseSupervisorRelease("v9.301.0-1-gabcdef")
	release.dir = t.TempDir()
	if err := os.WriteFile(release.path(), []byte(releaseSupervisorBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	view := acquireWithReleaseSupervisor(t, release, c)
	if view.State != LeaseStateHeld || view.SupervisorVersion != imageSupervisorVersion || c.probed || c.received != "" {
		t.Errorf("development acquire left %s reporting %q with image process %+v; want the image without a delivery probe",
			view.State, view.SupervisorVersion, c)
	}
}

// witness: internal/update/asset.go | if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), sum) { → if strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), sum) {
func TestLeaseAcquire_LPSupChecksum_HoldsOnlyVerifiedReleaseBytes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		valid bool
	}{{name: "valid", valid: true}, {name: "corrupt"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			release := releaseSupervisorFixture(t, false)
			release.checker.ReleasesURL = "https://releases.example.test"
			assetPath := "/download/" + releaseSupervisorVersion + "/" + DevsupAsset
			digest := sha256.Sum256([]byte(releaseSupervisorBytes))
			if !tc.valid {
				digest[0] ^= 0xff
			}
			release.checker.HTTPClient.Transport = releaseAssetTransport(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case assetPath + ".sha256":
					checksum := fmt.Sprintf("%x  %s\n", digest, DevsupAsset)
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(checksum))}, nil
				case assetPath:
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(releaseSupervisorBytes))}, nil
				}
				return nil, fmt.Errorf("unexpected release request %s", req.URL.Path)
			})

			err := release.Hold(t.Context())
			if !tc.valid {
				if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
					t.Fatalf("hold corrupt release = %v; want checksum failure", err)
				}
				if _, err := os.Stat(release.path()); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("corrupt release left a held asset: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("hold verified release: %v", err)
			}
			release.checker.HTTPClient.Transport = releaseAssetTransport(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("release source offline after boot")
			})
			c := &releaseLeaseContainer{installed: imageSupervisorVersion}
			view := acquireWithReleaseSupervisor(t, release, c)
			if view.SupervisorVersion != releaseSupervisorVersion || c.received != releaseSupervisorBytes {
				t.Errorf("acquire after boot reported %q with %q transferred, note %q; want the held release",
					view.SupervisorVersion, c.received, view.Note)
			}
		})
	}
}

// supervisorAnswer is a leaseSSH handler: the start of the supervisor answers
// answer (or fails with err), the declaration read finds no file, and every
// other command (the checkout) succeeds.
func supervisorAnswer(answer string, err error) func(host, command string) ([]byte, error) {
	return func(_, command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "zcp-devsup run"):
			return []byte(answer), err
		case strings.Contains(command, "@@zcp-devsup declaration"):
			return []byte("@@zcp-devsup declaration 1\n"), nil
		case strings.Contains(command, ctlCall):
			return []byte(ctlTook), nil
		}
		return nil, nil
	}
}

func TestLeaseAcquire_EnsuresTheSupervisorAfterTheCheckout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		answer   string
		err      error
		wantNote []string // empty: no note
	}{
		{name: "a reused container: none runs, zcp starts it", answer: "@@zcp-devsup check 2\n@@zcp-devsup started 0\n"},
		{name: "a fresh container: run.start's supervisor answers and is reloaded", answer: "@@zcp-devsup check 0\n"},
		{
			name: "no zcp-devsup in the pool's image: not a dev pool", answer: "@@zcp-devsup check 127\n",
			wantNote: []string{"zcp-devsup", "not a dev pool", "no dev servers"},
		},
		{
			name: "the started supervisor does not come up", answer: "@@zcp-devsup check 2\n@@zcp-devsup started 2\nzcp-devsup: boom\n@@zcp-devsup log 0\n",
			wantNote: []string{"boom", "zerops_dev_server"},
		},
		{
			name: "a supervisor holds the socket but does not answer", answer: "@@zcp-devsup check 5\n",
			wantNote: []string{"did not answer"},
		},
		{
			name: "ssh fails", err: sshFailure("kex_exchange_identification: read: Connection reset", exitError(t, 255)),
			wantNote: []string{"could not", "Connection reset", "zerops_dev_server"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.acquired = acquiredAs(3)
			ssh := &leaseSSH{}
			ssh.setHandler(supervisorAnswer(tt.answer, tt.err))
			fx.deps.SSH = ssh

			view, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
			if err != nil {
				t.Fatalf("acquire: %v (a supervisor that is missing or will not start never fails the acquire)", err)
			}
			if view.State != LeaseStateHeld {
				t.Errorf("state = %q, want held", view.State)
			}
			cmds := ssh.commands()
			starts, checkout := -1, -1
			for i, c := range cmds {
				switch {
				case strings.Contains(c, "zcp-devsup run"):
					if starts >= 0 {
						t.Errorf("the supervisor was ensured twice: %v", cmds)
					}
					starts = i
				case strings.Contains(c, "fetch -q --prune origin"):
					checkout = i
				}
			}
			if starts < 0 || checkout < 0 || starts < checkout {
				t.Fatalf("commands = %v, want the checkout, then the supervisor ensured", cmds)
			}
			if ssh.calls[starts].hostname != "devpool-3" {
				t.Errorf("the supervisor was ensured on %q, want the alias devpool-3", ssh.calls[starts].hostname)
			}
			if len(tt.wantNote) == 0 && view.Note != "" {
				t.Errorf("note = %q, want none", view.Note)
			}
			for _, want := range tt.wantNote {
				if !strings.Contains(view.Note, want) {
					t.Errorf("note %q lacks %q", view.Note, want)
				}
			}
			if strings.Contains(view.Note, testLease) {
				t.Errorf("note leaks the lease: %q", view.Note)
			}
			if _, err := loadLeaseRecord(fx.stateDir, "devpool-3"); err != nil {
				t.Errorf("the lease record is gone: %v", err)
			}
		})
	}
}

func TestDevServer_SupervisorStartedByZcpOnlyInALease(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		lease   bool
		running bool // a supervisor answers already
		action  string
		wantErr bool
	}{
		{name: "lease start", lease: true, action: "start"},
		// One that answers never gets a second beside it.
		{name: "lease whose supervisor answers", lease: true, running: true, action: "status"},
		{name: "lease restart", lease: true, action: "restart"},
		{name: "lease status", lease: true, action: "status"},
		{name: "lease stop", lease: true, action: "stop"},
		{name: "lease logs", lease: true, action: "logs"},
		// A dev service keeps today's behaviour: its supervisor is its dev
		// setup's run.start, so a start answers NoSupervisorError (the move
		// onto the supervisor) and nothing is started over ssh.
		{name: "dev service start", lease: false, action: "start", wantErr: true},
		{name: "dev service status", lease: false, action: "status"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &devservertest.Container{Installed: true, SupervisorVersion: "v9.300.0", Formats: []int{1}, Declaration: previewDeclaration}
			if tt.running {
				c = devservertest.NewSupervised(previewDeclaration)
			}
			c.Logs = map[string][]string{"web": {"ready"}}
			p := DevServerParams{Action: tt.action, Hostname: "appdev", Name: "web", Command: "pnpm dev", Port: 3000}
			client := devPoolProjectClient()
			if tt.lease {
				p.Hostname, p.StateDir = "devpool-3", leaseState(t)
			}

			result, err := ExecuteDevServer(context.Background(), c, client, "p1", p)

			if !tt.lease {
				if c.SupervisorStarts != 0 || sentWith(c, "zcp-devsup run") != 0 {
					t.Errorf("zcp started a supervisor on a dev service: %v", c.Sent())
				}
				var noSup *NoSupervisorError
				if tt.wantErr && !errors.As(err, &noSup) {
					t.Errorf("err = %v, want NoSupervisorError (the move onto the supervisor)", err)
				}
				if !tt.wantErr && (err != nil || result.Reason != reasonNoSupervisor) {
					t.Errorf("result = %+v, %v; want the no-supervisor answer", result, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s on a lease whose supervisor is gone: %v", tt.action, err)
			}
			wantStarts := 1
			if tt.running {
				wantStarts = 0
			}
			if c.SupervisorStarts != wantStarts || sentWith(c, "zcp-devsup run") != wantStarts {
				t.Errorf("supervisors started = %d (%d start commands), want %d", c.SupervisorStarts, sentWith(c, "zcp-devsup run"), wantStarts)
			}
			if result.Reason == reasonNoSupervisor {
				t.Errorf("result = %+v, want the supervisor's answer after zcp started it", result)
			}
			if result.Pool != "devpool" {
				t.Errorf("pool = %q, want devpool", result.Pool)
			}
		})
	}
}

// devPoolProjectClient holds the pool devpool and a dev service appdev.
func devPoolProjectClient() *platform.Mock {
	return poolClient(0).WithServices([]platform.ServiceStack{
		{
			ID: "svc-pool", Name: "devpool", ProjectID: "p1", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "swarm@1"},
		},
		{
			ID: "svc-app", Name: "appdev", ProjectID: "p1", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "nodejs@22"},
		},
	})
}

func TestDevServer_LeaseSupervisorMissingOrNotComingUp_SaysWhy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		installed bool
		action    string
		want      []string
	}{
		{name: "no zcp-devsup: start refused", installed: false, action: "start", want: []string{"devpool", "zcp-devsup", "not a dev pool", "run.prepareCommands"}},
		{name: "no zcp-devsup: status", installed: false, action: "status", want: []string{"devpool", "not a dev pool"}},
		{name: "will not come up: start refused", installed: true, action: "start", want: []string{"permission denied", "supervisor.log"}},
		{name: "will not come up: status", installed: true, action: "status", want: []string{"permission denied"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &devservertest.Container{
				Installed: tt.installed, SupervisorVersion: "v9.300.0", Formats: []int{1}, Declaration: previewDeclaration,
				FailStart: true, StartOutput: "zcp-devsup: control socket: permission denied",
			}

			result, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
				DevServerParams{Action: tt.action, Hostname: "devpool-3", StateDir: leaseState(t), Command: "pnpm dev", Port: 3000})

			var text string
			switch {
			case tt.action == "start":
				var pe *platform.PlatformError
				if !errors.As(err, &pe) {
					t.Fatalf("err = %v, want a refusal", err)
				}
				text = pe.Message + " " + pe.Suggestion
			case err != nil:
				t.Fatalf("status: %v", err)
			default:
				if result.Reason != reasonNoSupervisor {
					t.Errorf("reason = %q, want %q", result.Reason, reasonNoSupervisor)
				}
				text = result.Message
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("answer %q lacks %q", text, want)
				}
			}
			wantStarts := 0
			if tt.installed {
				wantStarts = 1
			}
			if c.SupervisorStarts != wantStarts {
				t.Errorf("supervisors started = %d, want %d (once per call, never retried in a loop)", c.SupervisorStarts, wantStarts)
			}
		})
	}
}

func TestLeaseDevServerChecks_NoSupervisor_SaysWhatALeaseNeeds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		installed bool
		want      []string
	}{
		{name: "no zcp-devsup in the image", installed: false, want: []string{"not a dev pool"}},
		{name: "installed, nothing runs it", installed: true, want: []string{"devpool-3", `zerops_dev_server action=status hostname="devpool-3"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &devservertest.Container{Installed: tt.installed, SupervisorVersion: "v9.300.0", Formats: []int{1}, Declaration: previewDeclaration}

			checks := leaseDevServerChecks(context.Background(), c, "devpool-3", "devpool", LeasePreviewProbe{})

			if len(checks) != 1 || checks[0].Name != "dev_servers" {
				t.Fatalf("checks = %+v, want one dev_servers note", checks)
			}
			for _, want := range tt.want {
				if !strings.Contains(checks[0].Detail, want) {
					t.Errorf("detail %q lacks %q", checks[0].Detail, want)
				}
			}
			// A lease's supervisor is never a matter of its run.start.
			if strings.Contains(checks[0].Detail, "run.start is not") {
				t.Errorf("detail %q blames run.start", checks[0].Detail)
			}
			// Verify reads; it starts nothing.
			if c.SupervisorStarts != 0 || sentWith(c, "zcp-devsup run") != 0 {
				t.Errorf("verify started a supervisor: %v", c.Sent())
			}
		})
	}
}

func TestVerifyLease_ReadsItsPoolsEntries(t *testing.T) {
	t.Parallel()
	// The repository's declaration holds every part's servers; the lease's
	// supervisor runs those naming its pool, so verify checks web, never hq.
	c := devservertest.NewSupervised(`{"version":1,"servers":[` +
		`{"name":"web","port":3000,"command":"pnpm dev","service":"devpool"},` +
		`{"name":"hq","port":8080,"command":"go run .","service":"hqpool"}]}`)
	lease := LeaseTargetInfo{Alias: "devpool-3", Pool: "devpool", ContainerHostname: "node-id-3.runtime.devpool.zerops", Branch: "feat-x"}

	result := VerifyLease(context.Background(), c, lease, "")

	var servers []string
	for _, check := range result.Checks {
		if check.Name == "dev_server" {
			servers = append(servers, check.Server)
		}
	}
	if len(servers) != 1 || servers[0] != "web" {
		t.Errorf("checked servers = %v, want web, devpool's one entry (checks %+v)", servers, result.Checks)
	}
}
