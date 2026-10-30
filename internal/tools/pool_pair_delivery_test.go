// Tests for: tools/workflow_release.go, tools/workflow_build_integration.go,
// tools/resolve_build_target.go, tools/deploy_repo_delivery.go,
// tools/deploy_git_push.go, tools/workflow_develop.go,
// tools/deploy_subdomain.go, tools/launch_source_control_gate.go,
// tools/launch_build_integration_earn.go, tools/launch_promotables.go,
// tools/workflow_launch_production.go, tools/workflow_pool_migrate.go — the
// delivery and launch readers of a pair keyed by its dev pool
// (docs/spec-workflows.md §13 PM-10, §8 E8, §10): nothing reaches the pool's
// hostname over ssh, since it answers from a random container, and no
// pointer names the pool as a push source or a build target. Launch,
// release and the production Actions block read such a pair from its
// origin (launch_pool_part_test.go, launch_pool_part_delivery_test.go).
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolDeliveryPair writes an appdev/appstage pair kept on GitHub — git-push
// configured, an Actions integration, deployed, auto public access on both
// halves with the dev's route stamped — and keys it on its dev pool, as a
// pool migration's retire does. It answers the state dir.
func poolDeliveryPair(t *testing.T) string {
	t.Helper()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		Stages:           []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		BootstrapSession: "test",
		BootstrappedAt:   "2026-10-01T00:00:00Z",
		FirstDeployedAt:  "2026-10-01T01:00:00Z",
		CloseDeployMode:  topology.CloseModeAuto,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/acme/app.git",
		TrackedRef:       "main",
		BuildIntegration: topology.BuildIntegrationActions,
		PrimarySetupName: "dev",
		PublicAccess: map[string]topology.PublicAccessRecord{
			"appdev":   {Intent: topology.PublicAccessAuto, SubdomainEnabledByZcpAt: "2026-10-01T01:00:00Z"},
			"appstage": {Intent: topology.PublicAccessAuto, SubdomainEnabledByZcpAt: "2026-10-01T02:00:00Z"},
		},
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	keyOnPool(t, stateDir)
	return stateDir
}

// poolDeliveryMeta is the pool-keyed pair as it is on disk.
func poolDeliveryMeta(t *testing.T, stateDir string) *workflow.ServiceMeta {
	t.Helper()
	m, err := workflow.FindServiceMeta(stateDir, testPool)
	if err != nil || m == nil || !m.DevIsPool() {
		t.Fatalf("the pool-keyed pair: %+v, %v", m, err)
	}
	return m
}

func decodeJSONResult(t *testing.T, text string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return out
}

// TestAnticipatedBuilds_PoolDevHalf_BuildsEveryStage: the CI a pair keyed by
// its dev pool wires builds its stages, each with its own setup — never the
// pool, whose image is a deploy of its own and no push rebuilds.
func TestAnticipatedBuilds_PoolDevHalf_BuildsEveryStage(t *testing.T) {
	t.Parallel()
	meta := poolDeliveryMeta(t, poolDeliveryPair(t))

	got := anticipatedBuilds(meta)

	want := []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}}
	if !slices.Equal(got, want) {
		t.Errorf("anticipatedBuilds = %+v, want %+v", got, want)
	}
}

// TestResolveBuildTargetForHost_PoolDevHalf_NoRedirect: verify and
// record-deploy redirect nothing on either half of a pool-keyed pair — the
// stage deploys from a lease, and nothing builds the pool from a push.
func TestResolveBuildTargetForHost_PoolDevHalf_NoRedirect(t *testing.T) {
	t.Parallel()
	stateDir := poolDeliveryPair(t)
	for _, host := range []string{testPool, "appstage"} {
		if target, setup := resolveBuildTargetForHost(stateDir, host); target != "" || setup != "" {
			t.Errorf("resolveBuildTargetForHost(%s) = (%q, %q), want no redirect", host, target, setup)
		}
	}
}

// TestBuildIntegration_PoolDevHalf_NoSSHAndBuildsTheStages: wiring Actions
// for a pool-keyed pair reads no origin over ssh (a pool has no checkout of
// its own), renders no gh command that reads GIT_TOKEN over ssh from the
// pool, and points CI at the stage.
func TestBuildIntegration_PoolDevHalf_NoSSHAndBuildsTheStages(t *testing.T) {
	t.Parallel()
	for _, service := range []string{testPool, "appstage"} {
		t.Run(service, func(t *testing.T) {
			t.Parallel()
			stateDir := poolDeliveryPair(t)
			ssh := &hostLogSSH{}
			mock := platform.NewMock().WithServices(poolPairServices())

			result, _, _ := handleBuildIntegration(context.Background(), mock, ssh, "proj-1",
				WorkflowInput{Service: service, Integration: "actions"}, stateDir, runtime.Info{InContainer: true})

			text := extractText(result)
			if result.IsError {
				t.Fatalf("build-integration: %s", text)
			}
			if ssh.reached(testPool) != 0 {
				t.Errorf("build-integration reached the pool over ssh: %v", ssh.hosts)
			}
			body := decodeJSONResult(t, text)
			if body["buildTarget"] != "appstage" {
				t.Errorf("buildTarget = %v, want appstage", body["buildTarget"])
			}
			secrets, _ := body["secrets"].([]any)
			for _, s := range secrets {
				cmd, _ := s.(map[string]any)["command"].(string)
				if strings.Contains(cmd, "ssh ") {
					t.Errorf("a gh command reads its token over ssh, where the pool has no container of its own: %s", cmd)
				}
			}
			conveyance, _ := body["ghTokenConveyance"].(map[string]any)
			if note, _ := conveyance["description"].(string); !strings.Contains(note, testPool) || strings.Contains(note, "over SSH from the push-source container") {
				t.Errorf("the conveyance note does not say the pool holds no container to read the token from: %q", note)
			}
		})
	}
}

// TestRepoDeliveryRedirect_PoolDevHalf_NoRedirect: a direct deploy to the
// stage of a pool-keyed pair — from a lease of the pool — is the stage's
// delivery: no redirect sends it to git-push from the pool, and the deploy
// carries no "reconcile with a push from the pool" warning.
func TestRepoDeliveryRedirect_PoolDevHalf_NoRedirect(t *testing.T) {
	t.Parallel()
	stateDir := poolDeliveryPair(t)
	for _, target := range []string{"appstage", testPool} {
		if r := repoDeliveryRedirect(stateDir, target, "", false); r != nil {
			t.Errorf("deploy to %s redirected: %s", target, extractText(r))
		}
		if w := repoDeliveryDivergenceWarning(context.Background(), stateDir, target, deployShipped{}); w != "" {
			t.Errorf("deploy to %s warns %q", target, w)
		}
	}
}

// TestGitPush_PoolDevHalf_RefusedBeforeSSH: git-push names the pool's
// hostname as the push source of a pool-keyed pair; it reaches a random
// container, so either hostname is refused by name — pointing at a lease —
// before any ssh call.
func TestGitPush_PoolDevHalf_RefusedBeforeSSH(t *testing.T) {
	t.Parallel()
	for _, target := range []string{testPool, "appstage"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			stateDir := poolDeliveryPair(t)
			ssh := &hostLogSSH{}

			result, _, _ := handleGitPush(context.Background(), platform.NewMock().WithServices(poolPairServices()), "proj-1",
				ssh, nil, nil, DeploySSHInput{TargetService: target, Strategy: deployStrategyGitPush}, stateDir, runtime.Info{InContainer: true})

			text := extractText(result)
			if !result.IsError {
				t.Fatalf("git-push on the pool-keyed pair was not refused: %s", text)
			}
			for _, want := range []string{testPool, "zerops_lease"} {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal does not name %q: %s", want, text)
				}
			}
			if len(ssh.hosts) != 0 {
				t.Errorf("the refusal came after ssh to %v", ssh.hosts)
			}
		})
	}
}

// TestDevelop_PoolDevHalf_NotARuntimeOfTheScope: develop's runtime list is
// the services it deploys and verifies; the pool keying a pair is none —
// its stage deploys from a lease of it — so a scope naming the pool is
// refused by name, pointing at the stage and a lease, and the stage alone
// opens the session.
func TestDevelop_PoolDevHalf_NotARuntimeOfTheScope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		scope   []string
		refused bool
	}{
		{name: "the pool", scope: []string{testPool}, refused: true},
		{name: "the pool and its stage", scope: []string{testPool, "appstage"}, refused: true},
		{name: "the stage", scope: []string{"appstage"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := poolDeliveryPair(t)
			engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
			mock := platform.NewMock().WithServices(poolPairServices())
			t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })

			result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj-1",
				WorkflowInput{Intent: "fix the header", Scope: tt.scope}, runtime.Info{InContainer: true}, nil)
			if err != nil {
				t.Fatalf("handleDevelopBriefing: %v", err)
			}
			text := extractText(result)
			ws, _ := workflow.CurrentWorkSession(stateDir)
			if !tt.refused {
				if result.IsError {
					t.Fatalf("scope %v refused: %s", tt.scope, text)
				}
				if ws == nil || !slices.Equal(ws.Services, []string{"appstage"}) {
					t.Errorf("session services = %+v, want [appstage]", ws)
				}
				return
			}
			if !result.IsError {
				t.Fatalf("scope %v naming the pool opened a session: %s", tt.scope, text)
			}
			for _, want := range []string{testPool, "appstage", "zerops_lease"} {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal does not name %q: %s", want, text)
				}
			}
			if ws != nil {
				t.Errorf("a refused scope opened a session: %+v", ws)
			}
		})
	}
}

// TestDevPoolInScope_SeveralStages_ScopeRenderedAsAList: the refusal of a
// scope naming a pool keyed by a pair of several stages names the stages to
// scope instead as one list, written the way the tool call takes it.
func TestDevPoolInScope_SeveralStages_ScopeRenderedAsAList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		stages []topology.StageRef
		want   string
	}{
		{name: "one stage", stages: []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}}, want: `scope=["appstage"]`},
		{name: "two stages", stages: []topology.StageRef{{Hostname: "apistage", SetupName: "api"}, {Hostname: "webstage", SetupName: "web"}}, want: `scope=["apistage","webstage"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &workflow.ServiceMeta{Hostname: testPool, Stages: tt.stages}
			perr := devPoolInScope([]string{testPool}, map[string]*workflow.ServiceMeta{testPool: m})
			if perr == nil {
				t.Fatal("a scope naming the pool was not refused")
			}
			if !strings.Contains(perr.Suggestion, tt.want) {
				t.Errorf("the refusal does not name %s: %s", tt.want, perr.Suggestion)
			}
			if strings.Contains(perr.Suggestion, `\"`) {
				t.Errorf("the refusal carries an escaped quote: %s", perr.Suggestion)
			}
		})
	}
}

// TestDevelopBriefing_PoolPairStage_LeaseNoOrchestratorVouchesFor_NeverTheSource:
// develop's start reads the project's leases as status does (PM-10). Here the
// pool's orchestrator cannot be asked (the pool holds no bearer), so zcp's own
// record stands in for the lease, unverified — maybe another holder's
// container by now (docs/spec-workflows.md §8 DP-2) — and the stage's step
// never names a deploy from it; the envelope still carries the lease. A
// verified held lease is the stage's source
// (TestRenderDevelopBriefing_LeaseReaderOption_ReplacesTheDefault).
func TestDevelopBriefing_PoolPairStage_LeaseNoOrchestratorVouchesFor_NeverTheSource(t *testing.T) {
	t.Parallel()
	stateDir := poolDeliveryPair(t)
	writePoolLeaseFile(t, stateDir)
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	mock := platform.NewMock().WithServices(poolPairServices())
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj-1",
		WorkflowInput{Intent: "fix the header", Scope: []string{"appstage"}}, runtime.Info{InContainer: true}, nil)
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	text := extractText(result)
	if result.IsError {
		t.Fatalf("develop start refused: %s", text)
	}
	if strings.Contains(text, `sourceService="`+testPool+`-3"`) {
		t.Errorf("the briefing names a deploy from a lease no orchestrator vouched for:\n%s", text)
	}
	env, ok := workflow.ExtractEnvelope(text)
	if !ok || !slices.ContainsFunc(env.LeaseList(), func(l workflow.LeaseSnapshot) bool { return l.Alias == testPool+"-3" }) {
		t.Errorf("the briefing's envelope carries no lease %s-3: %+v", testPool, env.Leases)
	}
}

// TestRenderDevelopBriefing_LeaseReaderOption_ReplacesTheDefault: a lease
// reader passed to the briefing replaces the default one, as it does for
// freshEnvelope, and the stage's step follows what it answers: the deploy
// from the lease it names held, else acquiring one.
func TestRenderDevelopBriefing_LeaseReaderOption_ReplacesTheDefault(t *testing.T) {
	t.Parallel()
	held := workflow.LeaseSnapshot{Alias: testPool + "-7", Pool: testPool, Branch: "feat-y", State: "held"}
	tests := []struct {
		name   string
		leases []workflow.LeaseSnapshot
		want   []string
	}{
		{name: "a held lease", leases: []workflow.LeaseSnapshot{held}, want: []string{"zerops_deploy", `sourceService="` + held.Alias + `"`, `targetService="appstage"`}},
		{name: "none held", want: []string{"zerops_lease", `action="acquire"`, `pool="` + testPool + `"`}},
		{name: "held but unverified: never the source", leases: []workflow.LeaseSnapshot{{Alias: held.Alias, Pool: testPool, Branch: "feat-y", State: "held", Unverified: true}},
			want: []string{"zerops_lease", `pool="` + testPool + `"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := poolDeliveryPair(t)
			engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
			ws := workflow.NewWorkSession("proj-1", string(workflow.EnvContainer), "fix the header", []string{"appstage"})
			if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })
			reader := func(context.Context) ([]workflow.LeaseSnapshot, error) { return tt.leases, nil }

			result, _, err := renderDevelopBriefing(context.Background(), engine, platform.NewMock().WithServices(poolPairServices()), "proj-1",
				runtime.Info{InContainer: true}, nil, workflow.WithLeaseReader(reader))
			if err != nil {
				t.Fatalf("renderDevelopBriefing: %v", err)
			}
			var primary string
			for line := range strings.SplitSeq(extractText(result), "\n") {
				if strings.Contains(line, "▸ Primary:") {
					primary = line
				}
			}
			for _, want := range tt.want {
				if !strings.Contains(primary, want) {
					t.Errorf("primary %q does not name %s", primary, want)
				}
			}
		})
	}
}

// writePoolLeaseFile records lease apppool-3 of the pool apppool the way
// zerops_lease does.
func writePoolLeaseFile(t *testing.T, stateDir string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := `{"poolHostname":"` + testPool + `","poolServiceId":"` + testPoolID + `","containerId":"c-3","containerNumber":3,` +
		`"containerHostname":"node-id-3.runtime.apppool.zerops","lease":"lease-secret","branch":"feat-x","acquiredAt":"2026-10-23T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, testPool+"-3.json"), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestEnsurePublicAccess_LeaseStampsPoolRecord: a lease's PA-1 decision acts
// on its pool, and once the pool keys a pair the pool's own record — the
// dev's intent moved under the pool's key with its stamp cleared — takes the
// stamp; the stage's record is untouched and the retired dev gets none.
func TestEnsurePublicAccess_LeaseStampsPoolRecord(t *testing.T) {
	// non-parallel: overrides the package's HTTP readiness tuning.
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()
	stateDir := poolDeliveryPair(t)
	writePoolLeaseFile(t, stateDir)
	pool := platform.ServiceStack{
		ID: testPoolID, Name: testPool, ProjectID: "proj-1", Status: "ACTIVE",
		Ports:                []platform.Port{{Port: 9001, Protocol: "tcp", HTTPSupport: true}},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "swarm@1"},
	}
	mock := platform.NewMock().
		WithServices(append([]platform.ServiceStack{pool}, poolPairServices()[1:]...)).
		WithService(&pool).
		WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive, SubdomainHost: "abc1.prg1.zerops.app"}).
		WithProcess(&platform.Process{ID: "proc-subdomain-enable-" + testPoolID, Status: statusFinished})
	stageBefore := poolDeliveryMeta(t, stateDir).PublicAccessFor("appstage")

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", stateDir, testPool+"-3", &ops.DeployResult{}, true)

	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Fatalf("EnableSubdomainAccess calls = %d, want 1 (on the pool)", mock.CallCounts["EnableSubdomainAccess"])
	}
	meta := poolDeliveryMeta(t, stateDir)
	if rec := meta.PublicAccessFor(testPool); rec.SubdomainEnabledByZcpAt == "" || rec.Intent != topology.PublicAccessAuto {
		t.Errorf("the pool's record = %+v, want the moved auto intent stamped", rec)
	}
	if got := meta.PublicAccessFor("appstage"); got != stageBefore {
		t.Errorf("the stage's record changed: %+v, was %+v", got, stageBefore)
	}
	assertNoDevStub(t, stateDir)
}

// TestBuildIntegrationEarn_PoolDevHalf_ReadsNothing: the earn probe reads
// the Actions file in the push source's checkout, or the build target's
// integration status; for a pool-keyed pair it reads neither and says why.
func TestBuildIntegrationEarn_PoolDevHalf_ReadsNothing(t *testing.T) {
	t.Parallel()
	for _, bi := range []topology.BuildIntegration{topology.BuildIntegrationActions, topology.BuildIntegrationWebhook} {
		t.Run(string(bi), func(t *testing.T) {
			t.Parallel()
			meta := poolDeliveryMeta(t, poolDeliveryPair(t))
			meta.BuildIntegration = bi
			ssh := &hostLogSSH{}
			mock := platform.NewMock().WithServices(poolPairServices())

			earned, evidence := readBuildIntegrationEarn(context.Background(),
				earnProbeDeps{client: mock, sshDeployer: ssh, rt: runtime.Info{InContainer: true}, projectID: "proj-1"}, meta.Hostname, meta)

			if earned || !strings.Contains(evidence, testPool) {
				t.Errorf("earn = (%v, %q), want unearned naming the pool", earned, evidence)
			}
			if len(ssh.hosts) != 0 || mock.CallCount("GetServiceStackIntegrationStatus") != 0 {
				t.Errorf("earn read the pair: ssh %v, %d integration reads", ssh.hosts, mock.CallCount("GetServiceStackIntegrationStatus"))
			}
		})
	}
}

// TestPoolMigrate_StageOfPoolDevPair_RefusedWithoutSSH: pool-migrate named
// by the stage of a pair already keyed by its dev pool reaches the pool
// through the meta; the pair is refused as a pool's, before any census over
// ssh.
func TestPoolMigrate_StageOfPoolDevPair_RefusedWithoutSSH(t *testing.T) {
	t.Parallel()
	f := newPoolMigrateFixture()
	f.stateDir = t.TempDir()
	if err := workflow.WriteServiceMeta(f.stateDir, f.meta); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	if _, err := workflow.MigratePairToPool(f.stateDir, workflow.PoolPairMove{Dev: "appdev", Pool: testPool, PoolServiceID: testPoolID}); err != nil {
		t.Fatalf("MigratePairToPool: %v", err)
	}
	f.services = []platform.ServiceStack{poolMigratePool(testPool, testPoolID), poolMigrateRuntime("appstage", "svc-stage")}
	f.input.Service = "appstage"

	out, _ := f.run(t)

	if out["status"] != "not-a-candidate" || !slices.Contains(poolMigrateReasons(out), "pool-or-box") {
		t.Fatalf("out = %v, want not-a-candidate for pool-or-box", out)
	}
	if msg := poolMigrateRefusalMessage(out, "pool-or-box"); !strings.Contains(msg, testPool) {
		t.Errorf("the refusal does not name the pool: %q", msg)
	}
	if cmds := f.ssh.recorded(); len(cmds) != 0 {
		t.Errorf("the refusal came after ssh: %v", cmds)
	}
}

// TestDevelop_StagelessPoolDevHalf_RefusedNamingTheLease: a dev-mode pair
// moved onto its dev pool keeps no stage, so nothing in it is a runtime of
// develop's; a scope naming the pool is refused by name pointing at a lease,
// never answered as a project with nothing bootstrapped.
func TestDevelop_StagelessPoolDevHalf_RefusedNamingTheLease(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "test", BootstrappedAt: "2026-10-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	keyOnPool(t, stateDir)
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })

	result, _, err := handleDevelopBriefing(context.Background(), engine, platform.NewMock().WithServices(poolPairServices()[:1]), "proj-1",
		WorkflowInput{Intent: "fix the header", Scope: []string{testPool}}, runtime.Info{InContainer: true}, nil)
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	text := extractText(result)
	if !result.IsError || !strings.Contains(text, testPool) || !strings.Contains(text, "zerops_lease") {
		t.Fatalf("scope naming the stageless pool was not refused naming its lease: %s", text)
	}
}
