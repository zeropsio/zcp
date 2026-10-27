// Tests for: tools/hq_wiring.go, tools/hq_origin_kept.go, tools/hq_change.go,
// tools/workflow_git_push_setup.go, tools/adopt_gitpush_reconcile.go — the HQ
// and adopt readers of a pair keyed by its dev pool (docs/spec-workflows.md
// §13 PM-10, §8 E8): the credential is renewed on the pool by the id the pair
// records, DP-16's per-pool record is taken over, and nothing reaches the
// pool's hostname over ssh, since it answers from a random container.
package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// hostLogSSH records every host a caller reaches over ssh, then runs the
// command through inner — or answers nothing when inner is nil.
type hostLogSSH struct {
	inner ops.SSHDeployer
	mu    sync.Mutex
	hosts []string
}

func (s *hostLogSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	s.hosts = append(s.hosts, hostname)
	s.mu.Unlock()
	if s.inner == nil {
		return nil, nil
	}
	return s.inner.ExecSSH(ctx, hostname, command)
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *hostLogSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *hostLogSSH) ExecSSHBackground(ctx context.Context, hostname, command string, timeout time.Duration) ([]byte, error) {
	s.mu.Lock()
	s.hosts = append(s.hosts, hostname)
	s.mu.Unlock()
	if s.inner == nil {
		return nil, nil
	}
	return s.inner.ExecSSHBackground(ctx, hostname, command, timeout)
}

// reached is how many times host was reached.
func (s *hostLogSSH) reached(host string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, h := range s.hosts {
		if h == host {
			n++
		}
	}
	return n
}

const (
	testPool   = "apppool"
	testPoolID = "svc-apppool"
)

// poolPairServices are the services of a project whose appdev/appstage pair
// was moved onto its dev pool: the dev service is gone.
func poolPairServices() []platform.ServiceStack {
	return []platform.ServiceStack{
		{ID: testPoolID, Name: testPool, Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
	}
}

// keyOnPool re-keys the pair stateDir keys by appdev onto its dev pool, as a
// pool migration's retire does, and answers it.
func keyOnPool(t *testing.T, stateDir string) *workflow.ServiceMeta {
	t.Helper()
	m, err := workflow.MigratePairToPool(stateDir, workflow.PoolPairMove{Dev: "appdev", Pool: testPool, PoolServiceID: testPoolID, PoolSetup: "pool"})
	if err != nil {
		t.Fatalf("MigratePairToPool: %v", err)
	}
	return m
}

// keyPairOnPool moves the lab's wired pair onto its dev pool and its project
// to the services that run after: the pool and the stage. Every later ssh
// call is recorded by host.
func (l *hqLab) keyPairOnPool() *hostLogSSH {
	l.t.Helper()
	keyOnPool(l.t, l.stateDir)
	l.mock.WithServices(poolPairServices())
	return &hostLogSSH{inner: l.ssh}
}

// poolPass runs a repository pass with ssh.
func (l *hqLab) poolPass(ssh ops.SSHDeployer) []string {
	l.t.Helper()
	return reconcileHQRepositories(l.t.Context(), l.mock, l.hq.srv.Client(), ssh, l.rt, l.stateDir, hqPassBackedOff)
}

// poolMeta is the pool-keyed pair as it is on disk.
func (l *hqLab) poolMeta() *workflow.ServiceMeta {
	l.t.Helper()
	m, err := workflow.FindServiceMeta(l.stateDir, testPool)
	if err != nil || m == nil || !m.DevIsPool() {
		l.t.Fatalf("the pool-keyed pair: %+v, %v", m, err)
	}
	return m
}

// assertNoDevStub fails when a meta keyed by the retired dev service is back.
func assertNoDevStub(t *testing.T, stateDir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(stateDir, "services", "appdev.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a meta keyed by the retired dev service is on disk (%v)", err)
	}
}

func poolRecordPath(stateDir string) string {
	return filepath.Join(stateDir, "leases", "credentials", testPool+".json")
}

// TestHQPass_PoolDevHalf_RenewsOnPoolNoSSH: once a wired pair is keyed by its
// dev pool, the repository pass gives the pool — by the id the pair records —
// the Mate credential the enrollment holds, as its ZCP_HQ_GIT_TOKEN, and
// records its fingerprint on the pair; it reaches neither the pool nor the
// retired dev service over ssh (no origin to point, no `main` to sync: a pool
// has no checkout of its own), and writes no meta keyed by the retired dev.
// Outside a Mate zcp never writes a pool's HQ credential, and nowhere its
// GIT_TOKEN.
func TestHQPass_PoolDevHalf_RenewsOnPoolNoSSH(t *testing.T) {
	// Non-parallel: newHQLab sets HOME and package tuning knobs.
	const renewed = "the-credential-after-the-move"
	tests := []struct {
		name      string
		mate      bool
		wantRenew bool
	}{
		{name: "in a Mate", mate: true, wantRenew: true},
		{name: "outside a Mate", mate: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			ssh := lab.keyPairOnPool()
			lab.rt.MateEnabled = tt.mate
			lab.relink(renewed)

			report := lab.poolPass(ssh)

			token, held := poolHQCredential(t, lab.mock, testPoolID)
			switch {
			case tt.wantRenew && (!held || token.Content != renewed || !token.Sensitive):
				t.Errorf("the pool's %s = %+v (held %v), want the renewed credential as a sensitive secret", ops.PoolHQCredentialEnvKey, token, held)
			case !tt.wantRenew && held:
				t.Errorf("a pass outside a Mate wrote the pool's %s: %+v", ops.PoolHQCredentialEnvKey, token)
			}
			if token, held := serviceGitToken(t, lab.mock, testPoolID); held {
				t.Errorf("the pass wrote the pool's GIT_TOKEN, the user's: %+v", token)
			}
			if tt.wantRenew {
				if got, want := lab.poolMeta().HQ.CredentialFingerprint, hq.GitCredentialFingerprint(renewed); got != want {
					t.Errorf("the pair records fingerprint %q, want the renewed credential's %q", got, want)
				}
			}
			for _, host := range []string{testPool, "appdev"} {
				if n := ssh.reached(host); n != 0 {
					t.Errorf("the pass reached %s over ssh %d time(s): %v", host, n, report)
				}
			}
			assertNoDevStub(t, lab.stateDir)
			assertNoSecretOnDisk(t, lab.stateDir, renewed)
		})
	}
}

// TestHQPass_PoolDevHalf_RenewalLeavesThePoolsGitTokenTheUsers: a pool's
// GIT_TOKEN is the user's — what a lease of a repository outside HQ there
// authenticates with — and zcp never writes over it. Renewing a pool-keyed
// pair's Mate credential gives the pool the new credential as a secret of its
// own and leaves the user's GIT_TOKEN as it was.
func TestHQPass_PoolDevHalf_RenewalLeavesThePoolsGitTokenTheUsers(t *testing.T) {
	// Non-parallel: newHQLab sets HOME and package tuning knobs.
	const (
		renewed    = "the-credential-after-the-move"
		usersToken = "the-users-own-github-token"
	)
	lab := newHQLab(t)
	lab.wire()
	ssh := lab.keyPairOnPool()
	lab.rt.MateEnabled = true
	lab.mock.WithServiceEnv(testPoolID, []platform.ServiceEnvVar{{ID: "e-git", Key: ops.GitTokenEnvKey, Content: usersToken, Sensitive: true}})
	lab.relink(renewed)

	lab.poolPass(ssh)

	if token, held := serviceGitToken(t, lab.mock, testPoolID); !held || token.Content != usersToken {
		t.Errorf("the pool's GIT_TOKEN = %+v (held %v), want the user's own token, never written over", token, held)
	}
	envs, err := lab.mock.GetServiceEnv(t.Context(), testPoolID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(envs, func(e platform.ServiceEnvVar) bool { return e.Content == renewed && e.Sensitive }) {
		t.Errorf("the pool holds no secret with the renewed credential: %+v", envs)
	}
	if got, want := lab.poolMeta().HQ.CredentialFingerprint, hq.GitCredentialFingerprint(renewed); got != want {
		t.Errorf("the pair records fingerprint %q, want the renewed credential's %q", got, want)
	}
}

// TestHQPass_PoolDevHalf_TakesOverThePoolsCredentialRecord: an acquire keeps
// a fingerprint per pool of the Mate credential it wrote (DP-16); once the
// pool keys a pair, the pair's record is that fingerprint's one home. The
// pass adopts the per-pool record when it names the pool service the pair
// records and tells more than the pair does, removes it, and renews the pool
// once, never twice; a record of another pool service is left to the
// per-pool renewal.
func TestHQPass_PoolDevHalf_TakesOverThePoolsCredentialRecord(t *testing.T) {
	// Non-parallel: newHQLab sets HOME and package tuning knobs.
	tests := []struct {
		name string
		// recordID and recordFP are the per-pool record an acquire left.
		recordID, recordFP string
		wantWrites         int
		wantRecordGone     bool
	}{
		{name: "the record names the pool and the credential held now", recordID: testPoolID, recordFP: hq.GitCredentialFingerprint(labCredential), wantWrites: 0, wantRecordGone: true},
		{name: "the record names the pool and a revoked credential", recordID: testPoolID, recordFP: hq.GitCredentialFingerprint("a-revoked-credential"), wantWrites: 1, wantRecordGone: true},
		{name: "the record names another pool service", recordID: "svc-an-older-pool", recordFP: hq.GitCredentialFingerprint(labCredential), wantWrites: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			ssh := lab.keyPairOnPool()
			lab.rt.MateEnabled = true
			writePoolCredentialFingerprint(t, lab.stateDir, testPool, tt.recordID, tt.recordFP)
			before := lab.mock.CallCount("CreateServiceEnvVar")

			lab.poolPass(ssh)

			if n := lab.mock.CallCount("CreateServiceEnvVar") - before; n != tt.wantWrites {
				t.Errorf("HQ credential writes = %d, want %d", n, tt.wantWrites)
			}
			if got, want := lab.poolMeta().HQ.CredentialFingerprint, hq.GitCredentialFingerprint(labCredential); got != want {
				t.Errorf("the pair records fingerprint %q, want the enrollment's %q", got, want)
			}
			_, err := os.Stat(poolRecordPath(lab.stateDir))
			if gone := errors.Is(err, os.ErrNotExist); gone != tt.wantRecordGone {
				t.Errorf("per-pool record gone = %v, want %v", gone, tt.wantRecordGone)
			}
			if n := ssh.reached(testPool); n != 0 {
				t.Errorf("the pass reached the pool over ssh %d time(s)", n)
			}
		})
	}
}

// TestHQPass_PoolDevHalf_NeverWiredOverSSH: a pass wires a pair by running
// git-push setup and git on its dev service's checkout, which a pair keyed by
// its dev pool has not. Such a pair is never one to wire, and when HQ holds
// the Mate in another application it is not wired there either: the pass
// says so, asks HQ for no repository and reaches the pool over no ssh.
func TestHQPass_PoolDevHalf_NeverWiredOverSSH(t *testing.T) {
	// Non-parallel: newHQLab sets HOME and package tuning knobs.
	tests := []struct {
		name     string
		arrange  func(t *testing.T, lab *hqLab)
		wantLine string
	}{
		{
			name: "git-push is no longer configured on the pair",
			arrange: func(t *testing.T, lab *hqLab) {
				t.Helper()
				if err := workflow.UpdateServiceMeta(lab.stateDir, testPool, func(m *workflow.ServiceMeta) error {
					m.GitPushState = topology.GitPushUnconfigured
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:     "HQ holds the Mate in another application",
			arrange:  func(_ *testing.T, lab *hqLab) { lab.hq.moveMate("app-2") },
			wantLine: testPool,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			ssh := lab.keyPairOnPool()
			lab.rt.MateEnabled = true
			tt.arrange(t, lab)
			asked := lab.hq.callCount("POST /api/mate/repos")

			report := strings.Join(lab.poolPass(ssh), "\n")

			if n := lab.hq.callCount("POST /api/mate/repos") - asked; n != 0 {
				t.Errorf("the pass asked HQ for a repository %d time(s): %s", n, report)
			}
			if n := ssh.reached(testPool); n != 0 {
				t.Errorf("the pass reached the pool over ssh %d time(s): %s", n, report)
			}
			if tt.wantLine != "" && !strings.Contains(report, tt.wantLine) {
				t.Errorf("the report does not name %q: %s", tt.wantLine, report)
			}
			if got := lab.poolMeta().HQ.AppID; got != labApp {
				t.Errorf("the pair's repository moved to application %q", got)
			}
			assertNoDevStub(t, lab.stateDir)
		})
	}
}

// TestHQUpsertCallbacks_MissingMeta_SkipWrite: every HQ record write a pass
// makes on a pair it read earlier finds the pair gone when a pool migration
// re-keyed it meanwhile — or anything else removed it. None of them writes a
// stub meta under the hostname it read: a dev-keyed stub would bring a
// retired dev service back as a pair of its own.
func TestHQUpsertCallbacks_MissingMeta_SkipWrite(t *testing.T) {
	// Non-parallel: newHQLab sets HOME and package tuning knobs.
	lab := newHQLab(t)
	hqc, ok := openHQ(lab.hq.srv.Client(), lab.rt)
	if !ok {
		t.Fatal("the lab's Mate is not enrolled")
	}
	app := labApp
	read := func() *workflow.ServiceMeta {
		return &workflow.ServiceMeta{
			Hostname: "appdev", Mode: topology.PlanModeStandard, GitPushState: topology.GitPushConfigured,
			HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", CredentialFingerprint: "fp-old"},
		}
	}
	tests := []struct {
		name  string
		write func(stateDir string)
	}{
		{name: "recordHQOrigin", write: func(stateDir string) {
			recordHQOrigin(stateDir, read(), hq.MateState{AppID: &app, Repos: []hq.MateRepo{{Name: "appdev"}}})
		}},
		{name: "renewHQPairCredential", write: func(stateDir string) {
			if line := renewHQPairCredential(t.Context(), lab.mock, labMate, stateDir, hqc, read()); line != "" {
				t.Fatalf("the renewal failed: %s", line)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()

			tt.write(stateDir)

			metas, err := workflow.ListServiceMetas(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(metas) != 0 {
				t.Errorf("%s wrote %d meta(s) for a pair that is gone: %+v", tt.name, len(metas), metas[0])
			}
		})
	}
}

// TestStatus_PoolDevHalf_ReadsNoCheckoutOriginOverSSH: status reads each
// wired pair's checkout origin on its dev half; a pair keyed by its dev pool
// has no checkout there — the pool's hostname answers from a random
// container, possibly someone's lease — so status reads none, and develop's
// start names no checkout of the pool's for code kept on GitHub.
func TestStatus_PoolDevHalf_ReadsNoCheckoutOriginOverSSH(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeHQWiredPairMeta(t, stateDir)
	if err := workflow.UpdateServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.HQ.Origin = &workflow.HQOrigin{Mode: hq.OriginOnGitHub, URL: "https://github.com/acme/app"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	keyOnPool(t, stateDir)
	services := []workflow.ServiceSnapshot{{Hostname: testPool, HQRepo: "appdev", DevIsPool: true}, {Hostname: "appstage"}}
	ssh := &hostLogSSH{}

	attachHQCheckoutOrigin(t.Context(), services, ssh, runtime.Info{InContainer: true}, stateDir)

	if n := ssh.reached(testPool); n != 0 {
		t.Errorf("status reached the pool over ssh %d time(s)", n)
	}
	if services[0].HQCheckoutOrigin != "" || services[0].HQOriginPending != "" {
		t.Errorf("status names a checkout origin of the pool: %+v", services[0])
	}
	for _, line := range hqKeptOnGitHub(stateDir) {
		if strings.Contains(line, testPool) {
			t.Errorf("develop's start names a checkout of the pool: %s", line)
		}
	}
}

// TestGitPushSetup_PoolDevHalf_Refused: git-push setup works on the dev
// service's checkout — a probe, origin and helper over ssh, then GIT_TOKEN on
// it. A pair keyed by its dev pool has no such checkout, so the action
// refuses either of its hostnames before any ssh call or env write, and says
// where git runs instead: in a lease of the pool. zcp's own wiring path
// (confirmGitPushSetupContainer) refuses the same way.
func TestGitPushSetup_PoolDevHalf_Refused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		service string
		remote  string
	}{
		{name: "the pool, confirming", service: testPool, remote: "https://github.com/acme/app.git"},
		{name: "the pool, walkthrough", service: testPool},
		{name: "the stage, confirming", service: "appstage", remote: "https://github.com/acme/app.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeHQWiredPairMeta(t, stateDir)
			keyOnPool(t, stateDir)
			mock := platform.NewMock().WithServices(poolPairServices())
			ssh := &hostLogSSH{}
			input := WorkflowInput{Action: "git-push-setup", Service: tt.service, RemoteURL: tt.remote, GitToken: "ghp_pooltoken"}

			res, _, _ := handleGitPushSetup(t.Context(), mock, nil, ssh, "p1", input, stateDir, runtime.Info{InContainer: true, ProjectID: "p1"})

			text := getTextContent(t, res)
			if !res.IsError {
				t.Fatalf("git-push-setup on a pair keyed by its dev pool was not refused: %s", text)
			}
			for _, want := range []string{testPool, "zerops_lease"} {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal does not name %q: %s", want, text)
				}
			}
			if len(ssh.hosts) != 0 || mock.CallCount("CreateServiceEnvVar") != 0 {
				t.Errorf("the refusal came after ssh %v or an env write (%d)", ssh.hosts, mock.CallCount("CreateServiceEnvVar"))
			}
		})
	}

	t.Run("zcp's own wiring path", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		writeHQWiredPairMeta(t, stateDir)
		m := keyOnPool(t, stateDir)
		mock := platform.NewMock().WithServices(poolPairServices())
		ssh := &hostLogSSH{}
		input := WorkflowInput{Service: testPool, RemoteURL: testHQAddress + "/git/" + testHQOrg + "/" + labApp + "/app.git", GitToken: "the-mate-credential"}

		res, _, _ := confirmGitPushSetupContainer(t.Context(), mock, nil, ssh, "p1", stateDir, testHQAddress, input, m)

		if res == nil || !res.IsError {
			t.Fatal("confirmGitPushSetupContainer ran on a pair keyed by its dev pool")
		}
		if len(ssh.hosts) != 0 || mock.CallCount("CreateServiceEnvVar") != 0 {
			t.Errorf("the refusal came after ssh %v or an env write (%d)", ssh.hosts, mock.CallCount("CreateServiceEnvVar"))
		}
	})
}

// TestAdoptGitPushReconcile_PoolDevHalf_NoSSH: the adopt pass reflects a live
// origin and GIT_TOKEN into a pair's git-push state by reading its dev
// service's checkout over ssh. A pair keyed by its dev pool has no such
// checkout — the pool's hostname answers from a random container — so the
// pass reads nothing there and stamps nothing.
func TestAdoptGitPushReconcile_PoolDevHalf_NoSSH(t *testing.T) {
	t.Parallel()
	stateDir := adoptReconcileFixture(t)
	keyOnPool(t, stateDir)
	services := poolPairServices()
	client := platform.NewMock().
		WithServices(services).
		WithServiceEnv(testPoolID, []platform.ServiceEnvVar{{ID: "e1", Key: "GIT_TOKEN", Content: "a-users-token"}})
	ssh := &hostLogSSH{inner: &routedSSH{responses: map[string]string{"git remote get-url": "https://github.com/me/app"}}}

	got := reconcileAdoptedGitPush(t.Context(), client, ssh, runtime.Info{InContainer: true}, stateDir, services)

	if len(got) != 0 {
		t.Errorf("reconciled = %v, want none", got)
	}
	if n := ssh.reached(testPool); n != 0 {
		t.Errorf("the adopt pass reached the pool over ssh %d time(s)", n)
	}
	m, err := workflow.FindServiceMeta(stateDir, testPool)
	if err != nil || m == nil {
		t.Fatalf("the pool-keyed pair: %v", err)
	}
	if m.GitPushState == topology.GitPushConfigured || m.RemoteURL != "" {
		t.Errorf("the pair was stamped from a pool container's checkout: %s %q", m.GitPushState, m.RemoteURL)
	}
}

// poolPairProject is a Mate's live project after its appdev/appstage pair
// moved onto its dev pool: the pool — scaled and holding variables of its own,
// its orchestrator's credentials among them — the stage, the data it uses
// and zcp.
func poolPairProject() *platform.Mock {
	pool := liveRuntime(testPoolID, testPool, "swarm@1")
	pool.SubdomainAccess = false
	pool.CurrentAutoscaling = &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: 3, CPUMode: "SHARED", MinRAM: 1}
	stage := liveRuntime("svc-appstage", "appstage", "nodejs@22")
	stage.CurrentAutoscaling = &platform.CustomAutoscaling{HorizontalMinCount: 1, HorizontalMaxCount: 2, CPUMode: "SHARED", MinRAM: 0.5}
	db := platform.ServiceStack{ID: "svc-db", Name: "db", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17", ServiceStackTypeCategoryName: "USER"}}
	services := []platform.ServiceStack{pool, stage, db, liveRuntime("svc-zcp", "zcp", "zcp@1")}
	return platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "acme-mate-1", Status: "ACTIVE", Mode: "SERIOUS"}).
		WithServicesDirect(services).
		WithServices(services).
		WithServiceEnv(testPoolID, []platform.ServiceEnvVar{
			{Key: "NODE_ENV", Content: "development", Type: platform.ServiceEnvUser},
			{Key: "API_TOKEN", Content: "pool-api-token", Type: platform.ServiceEnvUser, Sensitive: true},
			{Key: "ADMIN_TOKEN", Content: "pool-admin-token", Type: platform.ServiceEnvUser, Sensitive: true},
			{Key: "sshIsolation", Content: "vpn service@zcp", Type: platform.ServiceEnvUser},
		}).
		WithServiceEnv("svc-appstage", []platform.ServiceEnvVar{
			{Key: "NODE_ENV", Content: "production", Type: platform.ServiceEnvUser},
		})
}

// TestGroupRecipeInputs_PoolDevHalf_CarriedAsThePartsDevPool: a Mate made
// from the recipe gets the project's shape — a pair keyed by its dev pool is
// carried as that pool, keying its stage: the pool's hostname, type and
// setup, its own scale and service variables but never its orchestrator's
// credentials, no zerops.yaml read from a mount of the pool's (a random
// container); and the stage as it runs, its own type, scale and access. The
// pool keying the pair is no service left out.
func TestGroupRecipeInputs_PoolDevHalf_CarriedAsThePartsDevPool(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeHQWiredPairMeta(t, stateDir)
	keyOnPool(t, stateDir)
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	mountRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mountRoot, testPool), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mountRoot, testPool, "zerops.yaml"), []byte("zerops:\n  - setup: from-a-pool-container\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	inputs, warnings, err := composeGroupRecipeInputs(t.Context(), poolPairProject(), "p1", "acme", mountRoot, testHQAddress, testHQOrg, labApp, metas, metas, nil)
	if err != nil {
		t.Fatalf("composeGroupRecipeInputs: %v (warnings %v)", err, warnings)
	}
	if inputs.MateProjectID != "p1" {
		t.Errorf("MateProjectID = %q, want the Mate's project, which the tiers' mark names", inputs.MateProjectID)
	}
	if len(inputs.Runtimes) != 1 {
		t.Fatalf("runtimes = %+v, want the one pair", inputs.Runtimes)
	}
	got := inputs.Runtimes[0]
	if got.DevHostname != testPool || !got.DevPool || got.ServiceType != "swarm@1" || got.SetupName != "pool" {
		t.Errorf("dev half = %q pool %v %q setup %q, want the part's dev pool: %s swarm@1 setup pool", got.DevHostname, got.DevPool, got.ServiceType, got.SetupName, testPool)
	}
	if strings.Contains(got.ZeropsYAMLBody, "from-a-pool-container") {
		t.Error("the composer read a zerops.yaml from a mount of the pool's")
	}
	if got.Scaling == nil || got.Scaling.MaxContainers != 3 || got.Scaling.MinRAM != 1 {
		t.Errorf("pool scaling = %+v, want the pool's own (up to 3, 1 GB)", got.Scaling)
	}
	envs := envMap(got.ServiceEnvs)
	for _, key := range []string{"API_TOKEN", "ADMIN_TOKEN"} {
		if _, carried := envs[key]; carried {
			t.Errorf("the pool carries its orchestrator's %s", key)
		}
	}
	if envs["sshIsolation"] != "vpn service@zcp" || envs["NODE_ENV"] != "development" {
		t.Errorf("pool variables = %v, want its own sshIsolation and NODE_ENV", envs)
	}
	if len(got.Stages) != 1 {
		t.Fatalf("stages = %+v, want appstage", got.Stages)
	}
	stage := got.Stages[0]
	if stage.Hostname != "appstage" || stage.SetupName != "prod" || stage.ServiceType != "nodejs@22" || !stage.SubdomainEnabled {
		t.Errorf("stage = %+v, want appstage building prod, nodejs@22 as it runs, reachable", stage)
	}
	if stage.Scaling == nil || stage.Scaling.MinContainers != 1 || stage.Scaling.MaxContainers != 2 {
		t.Errorf("stage scaling = %+v, want the stage's own (1..2)", stage.Scaling)
	}
	if envMap(stage.ServiceEnvs)["NODE_ENV"] != "production" {
		t.Errorf("stage variables = %v, want the stage's", envMap(stage.ServiceEnvs))
	}
	if warningsHave(warnings, `"`+testPool+`"`) {
		t.Errorf("warnings %v leave out the pool keying the pair", warnings)
	}
}

// TestGroupRecipeInputs_PoolDevHalf_EveryPoolKeyingAPairIsCarried: a pair
// provisioned as a pool, never moved from a dev service, is carried as its
// pool like any other; one whose stage does not run still carries its stage
// from the pair's record — not read, so the composer types it by its setup —
// and a pool keying no pair stays out of the recipe and said.
func TestGroupRecipeInputs_PoolDevHalf_EveryPoolKeyingAPairIsCarried(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		arrange   func(t *testing.T, stateDir string)
		mock      func() *platform.Mock
		stageRead bool
	}{
		{
			name: "provisioned as a pool",
			arrange: func(t *testing.T, stateDir string) {
				t.Helper()
				if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
					Hostname: testPool, Mode: topology.PlanModeStandard,
					Stages:           []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
					BootstrapSession: "test", BootstrappedAt: "2026-09-16",
					GitPushState: topology.GitPushConfigured, PrimarySetupName: "pool",
					HQ:      &workflow.HQRepoRef{AppID: labApp, Repo: "app"},
					DevPool: &workflow.DevPoolRef{ServiceID: testPoolID},
				}); err != nil {
					t.Fatal(err)
				}
			},
			mock:      poolPairProject,
			stageRead: true,
		},
		{
			name: "its stage does not run",
			arrange: func(t *testing.T, stateDir string) {
				t.Helper()
				writeHQWiredPairMeta(t, stateDir)
				keyOnPool(t, stateDir)
			},
			mock: func() *platform.Mock {
				services := []platform.ServiceStack{
					liveRuntime(testPoolID, testPool, "swarm@1"),
					liveRuntime("svc-otherpool", "otherpool", "swarm@1"),
					liveRuntime("svc-zcp", "zcp", "zcp@1"),
				}
				return platform.NewMock().
					WithProject(&platform.Project{ID: "p1", Name: "acme-mate-1", Status: "ACTIVE", Mode: "SERIOUS"}).
					WithServicesDirect(services).WithServices(services)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			tt.arrange(t, stateDir)
			metas, err := workflow.ListServiceMetas(stateDir)
			if err != nil {
				t.Fatal(err)
			}

			inputs, warnings, err := composeGroupRecipeInputs(t.Context(), tt.mock(), "p1", "acme", t.TempDir(), testHQAddress, testHQOrg, labApp, metas, metas, nil)
			if err != nil {
				t.Fatalf("composeGroupRecipeInputs: %v (warnings %v)", err, warnings)
			}
			if len(inputs.Runtimes) != 1 || inputs.Runtimes[0].DevHostname != testPool || !inputs.Runtimes[0].DevPool {
				t.Fatalf("runtimes = %+v, want the pair carried as its pool", inputs.Runtimes)
			}
			stages := inputs.Runtimes[0].Stages
			if len(stages) != 1 || stages[0].Hostname != "appstage" || stages[0].SetupName != "prod" || (stages[0].ServiceType != "") != tt.stageRead {
				t.Errorf("stages = %+v, want appstage building prod, read %v", stages, tt.stageRead)
			}
			if !tt.stageRead && !warningsHave(warnings, `"otherpool"`) {
				t.Errorf("warnings %v do not say the pool keying no pair is left out", warnings)
			}
		})
	}
}

// TestGroupRecipeInputs_SplitPoolPairs_EveryPartItsPoolAndStage: a dev
// service serving several parts, moved onto one pool per part, leaves one
// pool-keyed pair per part on the one repository. The recipe carries every
// part as its pool and its stage — never the retired dev service standing in
// for them.
func TestGroupRecipeInputs_SplitPoolPairs_EveryPartItsPoolAndStage(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard,
		Stages:           []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}},
		BootstrapSession: "test", BootstrappedAt: "2026-09-16",
		GitPushState: topology.GitPushConfigured, RemoteURL: testHQAddress + "/git/" + testHQOrg + "/" + labApp + "/appdev.git",
		PrimarySetupName: "dev",
		HQ:               &workflow.HQRepoRef{AppID: labApp, Repo: "appdev"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.MigratePairToPools(stateDir, "appdev", []workflow.PoolPartMove{
		{Stage: "webstage", Pool: "webpool", PoolServiceID: "svc-webpool", PoolSetup: "webpool"},
		{Stage: "apistage", Pool: "apipool", PoolServiceID: "svc-apipool", PoolSetup: "apipool"},
	}); err != nil {
		t.Fatal(err)
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	services := []platform.ServiceStack{
		liveRuntime("svc-apipool", "apipool", "swarm@1"),
		liveRuntime("svc-webpool", "webpool", "swarm@1"),
		liveRuntime("svc-webstage", "webstage", "static"),
		liveRuntime("svc-apistage", "apistage", "nodejs@22"),
		liveRuntime("svc-zcp", "zcp", "zcp@1"),
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "acme-mate-1", Status: "ACTIVE", Mode: "SERIOUS"}).
		WithServicesDirect(services).WithServices(services)

	inputs, warnings, err := composeGroupRecipeInputs(t.Context(), mock, "p1", "acme", t.TempDir(), testHQAddress, testHQOrg, labApp, metas, metas, nil)
	if err != nil {
		t.Fatalf("composeGroupRecipeInputs: %v (warnings %v)", err, warnings)
	}
	got := make([]string, 0, len(inputs.Runtimes))
	for _, r := range inputs.Runtimes {
		parts := make([]string, 0, len(r.Stages))
		for _, s := range r.Stages {
			parts = append(parts, s.Hostname+":"+s.SetupName+":"+s.ServiceType)
		}
		got = append(got, r.DevHostname+"("+r.SetupName+") "+strings.Join(parts, ","))
		if !r.DevPool || !strings.HasSuffix(r.RepoURL, "/appdev") {
			t.Errorf("runtime %q = pool %v repo %q, want a pool on the one repository", r.DevHostname, r.DevPool, r.RepoURL)
		}
	}
	slices.Sort(got)
	want := []string{"apipool(apipool) apistage:api:nodejs@22", "webpool(webpool) webstage:web:static"}
	if !slices.Equal(got, want) {
		t.Errorf("runtimes = %v, want %v", got, want)
	}
}

// TestGroupRecipeInputs_ZeropsYAMLFromHQ: a pool holds no checkout of its
// own, and a dev checkout can still be empty when the pass runs (a classic
// bootstrap closes before develop puts code in), so the pair's zerops.yaml
// is read from its repository's main in HQ when the mount has none — never
// from a mount of the pool's.
func TestGroupRecipeInputs_ZeropsYAMLFromHQ(t *testing.T) {
	t.Parallel()
	const onMain = "zerops:\n  - setup: pool\n  - setup: prod\n"
	readMain := func(_ context.Context, repo string) string {
		if repo == "appdev" {
			return onMain
		}
		return ""
	}
	tests := []struct {
		name   string
		pooled bool
		mount  string
		want   string
	}{
		{name: "a pool-keyed pair", pooled: true, want: onMain},
		{name: "a dev checkout with no zerops.yaml yet", want: onMain},
		{name: "a dev checkout holding one", mount: "zerops:\n  - setup: api\n  - setup: prod\n", want: "zerops:\n  - setup: api\n  - setup: prod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeHQWiredPairMeta(t, stateDir)
			mock := poolPairProject()
			if tt.pooled {
				keyOnPool(t, stateDir)
			} else {
				services := []platform.ServiceStack{liveRuntime("svc-appdev", "appdev", "nodejs@22"), liveRuntime("svc-appstage", "appstage", "nodejs@22")}
				mock = platform.NewMock().
					WithProject(&platform.Project{ID: "p1", Name: "acme-mate-1", Status: "ACTIVE"}).
					WithServicesDirect(services).WithServices(services)
			}
			mountRoot := t.TempDir()
			if tt.mount != "" {
				if err := os.MkdirAll(filepath.Join(mountRoot, "appdev"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(mountRoot, "appdev", "zerops.yml"), []byte(tt.mount), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			metas, err := workflow.ListServiceMetas(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			inputs, warnings, err := composeGroupRecipeInputs(t.Context(), mock, "p1", "acme", mountRoot, testHQAddress, testHQOrg, labApp, metas, metas, readMain)
			if err != nil {
				t.Fatalf("composeGroupRecipeInputs: %v (warnings %v)", err, warnings)
			}
			if len(inputs.Runtimes) != 1 || inputs.Runtimes[0].ZeropsYAMLBody != tt.want {
				t.Errorf("zerops.yaml = %q, want %q", inputs.Runtimes[0].ZeropsYAMLBody, tt.want)
			}
		})
	}
}
