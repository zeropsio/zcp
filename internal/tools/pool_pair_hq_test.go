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
// the Mate credential the enrollment holds, and records its fingerprint on
// the pair; it reaches neither the pool nor the retired dev service over ssh
// (no origin to point, no `main` to sync: a pool has no checkout of its own),
// and writes no meta keyed by the retired dev. Outside a Mate zcp never
// writes a pool's GIT_TOKEN.
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
			lab.reenroll(renewed)

			report := lab.poolPass(ssh)

			token, held := serviceGitToken(t, lab.mock, testPoolID)
			switch {
			case tt.wantRenew && (!held || token.Content != renewed || !token.Sensitive):
				t.Errorf("the pool's GIT_TOKEN = %+v (held %v), want the renewed credential as a sensitive secret", token, held)
			case !tt.wantRenew && held:
				t.Errorf("a pass outside a Mate wrote the pool's GIT_TOKEN: %+v", token)
			}
			if tt.wantRenew {
				if got, want := lab.poolMeta().HQ.CredentialFingerprint, hq.CredentialFingerprint(renewed); got != want {
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
		{name: "the record names the pool and the credential held now", recordID: testPoolID, recordFP: hq.CredentialFingerprint(labCredential), wantWrites: 0, wantRecordGone: true},
		{name: "the record names the pool and a revoked credential", recordID: testPoolID, recordFP: hq.CredentialFingerprint("a-revoked-credential"), wantWrites: 1, wantRecordGone: true},
		{name: "the record names another pool service", recordID: "svc-an-older-pool", recordFP: hq.CredentialFingerprint(labCredential), wantWrites: 1},
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
				t.Errorf("GIT_TOKEN writes = %d, want %d", n, tt.wantWrites)
			}
			if got, want := lab.poolMeta().HQ.CredentialFingerprint, hq.CredentialFingerprint(labCredential); got != want {
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
	hqc, ok := openHQ(lab.hq.srv.Client())
	if !ok {
		t.Fatal("the lab's Mate is not enrolled")
	}
	app := labApp
	read := func() *workflow.ServiceMeta {
		return &workflow.ServiceMeta{
			Hostname: "appdev", Mode: topology.PlanModeStandard, GitPushState: topology.GitPushConfigured,
			HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", Change: 3, Branch: hqReleasedBranch(hqc), CredentialFingerprint: "fp-old"},
		}
	}
	tests := []struct {
		name  string
		write func(stateDir string)
	}{
		{name: "recordChange", write: func(stateDir string) { recordChange(stateDir, read(), 4, "feat") }},
		{name: "upgradeReleasedBranch", write: func(stateDir string) { upgradeReleasedBranch(stateDir, hqc, read()) }},
		{name: "clearChange", write: func(stateDir string) { clearChange(stateDir, read(), 3) }},
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
		input := WorkflowInput{Service: testPool, RemoteURL: testHQAddress + "/git/" + labApp + "/app.git", GitToken: "the-mate-credential"}

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

// TestGroupRecipeInputs_PoolDevHalf_CarriesThePartAsItsDevService: the
// recipe never carries a dev pool (it is no application runtime), and a Mate
// made from the recipe gets a dev service. So a pair keyed by its dev pool is
// carried as the dev service it was moved from — that service's hostname
// and setup — with the type, scale, public access and variables of its
// stage, never the pool's: no swarm type, no pool scale, none of the pool's
// own variables, and no zerops.yaml read from a mount of the pool's.
func TestGroupRecipeInputs_PoolDevHalf_CarriesThePartAsItsDevService(t *testing.T) {
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

	inputs, warnings, err := composeGroupRecipeInputs(t.Context(), poolPairProject(), "p1", "acme", mountRoot, testHQAddress, labApp, metas, metas)
	if err != nil {
		t.Fatalf("composeGroupRecipeInputs: %v (warnings %v)", err, warnings)
	}
	if len(inputs.Runtimes) != 1 {
		t.Fatalf("runtimes = %+v, want the one pair", inputs.Runtimes)
	}
	got := inputs.Runtimes[0]
	if got.DevHostname != "appdev" || got.ServiceType != "nodejs@22" || got.SetupName != "api" {
		t.Errorf("dev half = %q %q setup %q, want the dev service it was moved from: appdev nodejs@22 setup api", got.DevHostname, got.ServiceType, got.SetupName)
	}
	if len(got.Stages) != 1 || got.Stages[0].Hostname != "appstage" || got.Stages[0].SetupName != "prod" {
		t.Errorf("stages = %+v, want appstage building prod", got.Stages)
	}
	if strings.Contains(got.ZeropsYAMLBody, "from-a-pool-container") {
		t.Error("the composer read a zerops.yaml from a mount of the pool's")
	}
	if got.Scaling == nil || got.Scaling.MinContainers != 1 || got.Scaling.MaxContainers != 2 {
		t.Errorf("scaling = %+v, want the stage's (1..2), never the pool's", got.Scaling)
	}
	envs := envMap(got.ServiceEnvs)
	for _, key := range []string{"API_TOKEN", "ADMIN_TOKEN", "sshIsolation"} {
		if _, carried := envs[key]; carried {
			t.Errorf("the dev half carries the pool's own %s", key)
		}
	}
	if envs["NODE_ENV"] != "production" {
		t.Errorf("dev half variables = %v, want the stage's", envs)
	}
	if !warningsHave(warnings, `"`+testPool+`"`) || !warningsHave(warnings, "not carried") {
		t.Errorf("warnings %v do not say the pool is not carried", warnings)
	}
}

// TestGroupRecipeInputs_PoolDevHalf_NothingToCarryIsSaid: a pair keyed by a
// dev pool it was never moved onto from a dev service has no dev service for
// the recipe to carry, and one with no stage running has nothing to stand
// for it: either is left out and said, never composed from the pool.
func TestGroupRecipeInputs_PoolDevHalf_NothingToCarryIsSaid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		arrange func(t *testing.T, stateDir string)
		mock    func() *platform.Mock
	}{
		{
			name: "provisioned as a pool",
			arrange: func(t *testing.T, stateDir string) {
				t.Helper()
				if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
					Hostname: testPool, Mode: topology.PlanModeStandard,
					Stages:           []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
					BootstrapSession: "test", BootstrappedAt: "2026-09-16",
					GitPushState: topology.GitPushConfigured,
					HQ:           &workflow.HQRepoRef{AppID: labApp, Repo: "app"},
					DevPool:      &workflow.DevPoolRef{ServiceID: testPoolID},
				}); err != nil {
					t.Fatal(err)
				}
			},
			mock: poolPairProject,
		},
		{
			name: "no stage runs",
			arrange: func(t *testing.T, stateDir string) {
				t.Helper()
				writeHQWiredPairMeta(t, stateDir)
				keyOnPool(t, stateDir)
			},
			mock: func() *platform.Mock {
				services := []platform.ServiceStack{liveRuntime(testPoolID, testPool, "swarm@1"), liveRuntime("svc-zcp", "zcp", "zcp@1")}
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

			inputs, warnings, _ := composeGroupRecipeInputs(t.Context(), tt.mock(), "p1", "acme", t.TempDir(), testHQAddress, labApp, metas, metas)

			for _, r := range inputs.Runtimes {
				if r.DevHostname == testPool || strings.HasPrefix(r.ServiceType, "swarm") {
					t.Errorf("the recipe carries the pool as a dev service: %+v", r)
				}
			}
			if !warningsHave(warnings, "dev pool") {
				t.Errorf("warnings %v do not say why the pair is left out", warnings)
			}
		})
	}
}
