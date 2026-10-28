// Tests for: tools/launch_source_control_gate.go, tools/launch_promotables.go,
// tools/launch_bundle_compose.go, tools/workflow_launch_production.go,
// tools/pool_part_origin.go — a part whose pair is keyed by its dev pool
// launches from its origin and its stage (docs/spec-workflows.md §10
// P-LP-17, P-LP-19): the source is the recorded origin at the tracked ref,
// read in a scratch repository of zcp's own, and nothing reaches the pool,
// a lease or the stage over ssh.
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/ops/origintest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolPartYAML is the part's zerops.yaml on its origin: the stage's setup
// `prod` and the pool's image setup.
const poolPartYAML = `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: ./
    run:
      base: nodejs@22
      start: node server.js
  - setup: pool
    build:
      base: nodejs@22
      deployFiles: ./
    run:
      base: nodejs@22
      start: zcp-devsup run
`

// poolPartGitToken is the pool's GIT_TOKEN as the env API answers it: a
// value that must never reach a response, a state file or an audit record.
const poolPartGitToken = "pool-git-token-leak-canary-5a1f"

// requirePoolPartGit skips a test that needs the git binary.
func requirePoolPartGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// poolPartOriginRepo is the name every test's origin takes on its server.
const poolPartOriginRepo = "app.git"

// poolPartOriginOn serves the part's origin over HTTPS (origintest; the
// package's TestMain trusts its certificate) — a bare repository whose main
// holds files, each named branch main's files with its own over them — and
// answers the server, its URL and each branch's head, main's under "main".
func poolPartOriginOn(t *testing.T, files map[string]string, branches map[string]map[string]string) (*origintest.Server, string, map[string]string) {
	t.Helper()
	srv := origintest.New(t)
	url, heads := srv.Repo(t, poolPartOriginRepo, files, branches)
	return srv, url, heads
}

// poolPartOrigin is poolPartOriginOn without its server.
func poolPartOrigin(t *testing.T, files map[string]string, branches map[string]map[string]string) (string, map[string]string) {
	t.Helper()
	_, url, heads := poolPartOriginOn(t, files, branches)
	return url, heads
}

// poolPartMissingOrigin is an HTTPS origin that holds no repository.
func poolPartMissingOrigin(t *testing.T) string {
	t.Helper()
	return origintest.New(t).URL + "/no-such-origin.git"
}

// poolPartPair writes the appdev/appstage pair — git-push configured to
// remoteURL tracking trackedRef, an Actions integration, deployed — and keys
// it on its dev pool, as a pool migration's retire does. It answers the
// state dir.
func poolPartPair(t *testing.T, remoteURL, trackedRef string) string {
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
		RemoteURL:        remoteURL,
		TrackedRef:       trackedRef,
		BuildIntegration: topology.BuildIntegrationActions,
		PrimarySetupName: "dev",
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	keyOnPool(t, stateDir)
	return stateDir
}

// poolPartZcpService is zcp's own service, the container's (`hostname` zcp).
func poolPartZcpService() platform.ServiceStack {
	return platform.ServiceStack{ID: "svc-zcp", Name: "zcp", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "zcp@1", ServiceStackTypeCategoryName: "USER"}}
}

// poolPartClient is the project after the move: the pool, holding the
// dev's GIT_TOKEN, the stage and zcp's own service; one project env to
// classify.
func poolPartClient() *platform.Mock {
	return newLaunchMockClient().
		WithProject(&platform.Project{ID: "proj-1", Name: "app", Status: statusActive}).
		WithServices(append(poolPairServices(), poolPartZcpService())).
		WithServiceEnv(testPoolID, []platform.ServiceEnvVar{{Key: "GIT_TOKEN", Content: poolPartGitToken, Sensitive: true}}).
		WithProjectEnv([]platform.ProjectEnvVar{{Key: "LOG_LEVEL", Content: "info"}})
}

// poolPartRuntime is zcp's container, naming its own service.
func poolPartRuntime() runtime.Info { return runtime.Info{InContainer: true, ServiceName: "zcp"} }

// TestResolveLaunchRuntimes_PoolKeyedPart_StageIsTheRuntime: either hostname
// of a pool-keyed pair resolves to the pair, keyed by the pool, and the
// production runtime is named after the stage — never after the pool.
func TestResolveLaunchRuntimes_PoolKeyedPart_StageIsTheRuntime(t *testing.T) {
	t.Parallel()
	stateDir := poolPartPair(t, "https://github.com/acme/app.git", "main")
	for _, choice := range []string{testPool, "appstage"} {
		rs := resolveLaunchRuntimes(stateDir, WorkflowInput{TargetService: choice})
		if len(rs) != 1 {
			t.Fatalf("resolveLaunchRuntimes(%s) = %+v, want one runtime", choice, rs)
		}
		if rs[0].PushHostname != testPool || !rs[0].DevIsPool {
			t.Errorf("resolveLaunchRuntimes(%s) = %+v, want the pair keyed by %s", choice, rs[0], testPool)
		}
		if rs[0].ProdHostname != "app" {
			t.Errorf("resolveLaunchRuntimes(%s).ProdHostname = %q, want app (from the stage appstage)", choice, rs[0].ProdHostname)
		}
	}
}

// TestLaunchSourceControlGate_PoolKeyedPart_ReadsTheOriginNotThePool: the
// gate passes a pool-keyed pair whose origin holds the tracked ref, from
// either hostname, and reaches nothing over ssh.
func TestLaunchSourceControlGate_PoolKeyedPart_ReadsTheOriginNotThePool(t *testing.T) {
	t.Parallel()
	url, _ := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	stateDir := poolPartPair(t, url, "main")
	ssh := &hostLogSSH{}
	for _, host := range []string{testPool, "appstage"} {
		check, blockers, err := validateLaunchSourceControl(context.Background(), poolPartClient(), ssh,
			poolPartRuntime(), stateDir, "proj-1", host, nil)
		if err != nil {
			t.Fatalf("gate(%s): %v", host, err)
		}
		if gateHasBlockingFailure(blockers) || check == nil {
			t.Fatalf("gate(%s) refused the pool-keyed part: check %+v, blockers %+v", host, check, blockers)
		}
		if check.PushHostname != testPool || check.MetaRemoteURL != url {
			t.Errorf("gate(%s) check = push %q remote %q, want %s and the recorded origin", host, check.PushHostname, check.MetaRemoteURL, testPool)
		}
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("the gate reached %v over ssh", ssh.hosts)
	}
}

// TestLaunchSourceControlGate_StagelessPoolPair_RefusedBeforeRead: a
// pool-keyed pair with no stage has no runtime to promote; the gate refuses
// it by name before it reads anything — the origin included.
func TestLaunchSourceControlGate_StagelessPoolPair_RefusedBeforeRead(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "test", BootstrappedAt: "2026-10-01T00:00:00Z",
		GitPushState: topology.GitPushConfigured,
		// An origin no read would find: a read would answer origin-unreadable.
		RemoteURL: poolPartMissingOrigin(t),
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	keyOnPool(t, stateDir)
	ssh := &hostLogSSH{}

	check, blockers, err := validateLaunchSourceControl(context.Background(), poolPartClient(), ssh,
		poolPartRuntime(), stateDir, "proj-1", testPool, nil)

	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if check != nil {
		t.Errorf("the gate passed a check for the stageless pair: %+v", check)
	}
	if len(blockers) != 1 || blockers[0].ID != "pool-dev-pair-no-stage-"+testPool || blockers[0].Severity != topology.BlockerSeverityBlock {
		t.Fatalf("blockers = %+v, want the one pool-dev-pair-no-stage block", blockers)
	}
	if !strings.Contains(blockers[0].Message, testPool) {
		t.Errorf("the blocker does not name the pool: %s", blockers[0].Message)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("the gate reached %v over ssh", ssh.hosts)
	}
}

// TestLaunchSourceControlGate_PoolKeyedPart_UnreadableOrigin_NamesWhatToFix:
// an origin the gate cannot read is the origin-unreadable block, naming the
// origin, the ref and what to fix — never a remote mismatch or an unpushed
// head, which name a checkout the pair does not have.
func TestLaunchSourceControlGate_PoolKeyedPart_UnreadableOrigin_NamesWhatToFix(t *testing.T) {
	t.Parallel()
	missing := poolPartMissingOrigin(t)
	present, _ := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	tests := []struct {
		name       string
		remoteURL  string
		trackedRef string
		want       []string
	}{
		{"origin unreachable", missing, "main", []string{missing, "main", "GIT_TOKEN"}},
		{"tracked ref absent", present, "release", []string{present, "release", "zerops_lease"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := poolPartPair(t, tt.remoteURL, tt.trackedRef)
			ssh := &hostLogSSH{}

			_, blockers, err := validateLaunchSourceControl(context.Background(), poolPartClient(), ssh,
				poolPartRuntime(), stateDir, "proj-1", "appstage", nil)

			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			var found *topology.Blocker
			for i := range blockers {
				switch blockers[i].ID {
				case "origin-unreadable-" + testPool:
					found = &blockers[i]
				case "remote-mismatch-" + testPool, "head-not-pushed-" + testPool, "source-read-failed-" + testPool:
					t.Errorf("a checkout's blocker for the pool-keyed part: %+v", blockers[i])
				}
			}
			if found == nil || found.Severity != topology.BlockerSeverityBlock {
				t.Fatalf("blockers = %+v, want the origin-unreadable block", blockers)
			}
			for _, w := range tt.want {
				if !strings.Contains(found.Message, w) {
					t.Errorf("the blocker does not name %q: %s", w, found.Message)
				}
			}
			if strings.Contains(found.Message, poolPartGitToken) {
				t.Errorf("the blocker carries the pool's GIT_TOKEN: %s", found.Message)
			}
			if len(ssh.hosts) != 0 {
				t.Errorf("the gate reached %v over ssh", ssh.hosts)
			}
		})
	}
}

// TestComposeLaunchBundleInputs_PoolKeyedPart_StageAndOrigin: the bundle's
// runtime is the stage — its type, the production hostname from its name —
// built from the origin's commit and zerops.yaml the gate read, with the
// recorded origin as its repository; nothing reached over ssh.
func TestComposeLaunchBundleInputs_PoolKeyedPart_StageAndOrigin(t *testing.T) {
	t.Parallel()
	url, heads := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	stateDir := poolPartPair(t, url, "main")
	client := poolPartClient()
	ssh := &hostLogSSH{}

	inputs := composePoolPart(t, client, ssh, stateDir)

	if len(inputs.Runtimes) != 1 {
		t.Fatalf("runtimes = %+v, want one", inputs.Runtimes)
	}
	r := inputs.Runtimes[0]
	if r.ProdHostname != "app" || r.ServiceType != "nodejs@22" {
		t.Errorf("runtime = %q %q, want app nodejs@22 (the stage's)", r.ProdHostname, r.ServiceType)
	}
	if r.GitCommitSHA != heads["main"] {
		t.Errorf("GitCommitSHA = %q, want the origin's main %q", r.GitCommitSHA, heads["main"])
	}
	if r.ZeropsYAMLBody != strings.TrimRight(poolPartYAML, "\n") {
		t.Errorf("ZeropsYAMLBody = %q, want the origin's zerops.yaml", r.ZeropsYAMLBody)
	}
	if topology.CanonicalRepoURL(r.RepoURL) != topology.CanonicalRepoURL(url) {
		t.Errorf("RepoURL = %q, want the recorded origin %q", r.RepoURL, url)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("compose reached %v over ssh", ssh.hosts)
	}
}

// TestComposeLaunchBundleInputs_PoolKeyedPart_TrackedRefElseDefaultBranch:
// the commit launch carries is the recorded tracked ref's tip, else the
// origin's default branch's.
func TestComposeLaunchBundleInputs_PoolKeyedPart_TrackedRefElseDefaultBranch(t *testing.T) {
	t.Parallel()
	release := strings.Replace(poolPartYAML, "node server.js", "node release.js", 1)
	url, heads := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML},
		map[string]map[string]string{"release": {"zerops.yaml": release}})
	tests := []struct {
		trackedRef string
		wantSHA    string
		wantBody   string
	}{
		{"", heads["main"], poolPartYAML},
		{"release", heads["release"], release},
	}
	for _, tt := range tests {
		t.Run("tracked="+tt.trackedRef, func(t *testing.T) {
			t.Parallel()
			stateDir := poolPartPair(t, url, tt.trackedRef)

			inputs := composePoolPart(t, poolPartClient(), &hostLogSSH{}, stateDir)

			if got := inputs.Runtimes[0]; got.GitCommitSHA != tt.wantSHA || got.ZeropsYAMLBody != strings.TrimRight(tt.wantBody, "\n") {
				t.Errorf("runtime read %q, want %q with its zerops.yaml", got.GitCommitSHA, tt.wantSHA)
			}
		})
	}
}

// composePoolPart runs the gate on the stage of the pool-keyed pair and
// composes the bundle inputs from its check, as a publish does.
func composePoolPart(t *testing.T, client *platform.Mock, ssh *hostLogSSH, stateDir string) bundle.LaunchBundleInputs {
	t.Helper()
	ctx := context.Background()
	check, blockers, err := validateLaunchSourceControl(ctx, client, ssh, poolPartRuntime(), stateDir, "proj-1", "appstage", nil)
	if err != nil || check == nil || gateHasBlockingFailure(blockers) {
		t.Fatalf("gate: check %+v, blockers %+v, err %v", check, blockers, err)
	}
	runtimes := resolveLaunchRuntimes(stateDir, WorkflowInput{TargetService: "appstage"})
	inputs, _, err := composeLaunchBundleInputs(ctx, client, ssh, poolPartRuntime(), "proj-1", "app-prod",
		runtimes, []*LaunchSourceControlCheck{check}, nil, nil, nil, nil, bundle.VariantLaunchNew)
	if err != nil {
		t.Fatalf("composeLaunchBundleInputs: %v", err)
	}
	return inputs
}

// TestLaunchProduction_PoolKeyedPart_LaunchesFromOrigin: launch-production
// on either hostname of a pool-keyed pair reaches ready-to-launch, its
// baseline carrying the origin's commit, and reaches nothing over ssh.
func TestLaunchProduction_PoolKeyedPart_LaunchesFromOrigin(t *testing.T) {
	t.Parallel()
	url, heads := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	for _, target := range []string{"appstage", testPool} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			stateDir := poolPartPair(t, url, "main")
			ssh := &hostLogSSH{}
			input := WorkflowInput{
				Workflow: workflowLaunchProduction, ProductionProjectName: "app-prod", TargetService: target,
				EnvClassifications: map[string]string{"LOG_LEVEL": "plain-config"}, SkipBuildIntegration: []string{testPool},
			}

			result, _, err := handleLaunchProduction(context.Background(), "proj-1", poolPartClient(), nil, nil, input, stateDir,
				poolPartRuntime(), ssh, "")

			if err != nil {
				t.Fatalf("launch(%s): %v", target, err)
			}
			resp := decodeLaunchResp(t, []byte(extractText(result)))
			if resp.Status != topology.LaunchStatusReadyToLaunch {
				t.Fatalf("launch(%s) = %s %+v, want ready-to-launch\n%s", target, resp.Status, resp.Blockers, extractText(result))
			}
			state, err := readLaunchState(stateDir, generateLaunchID("proj-1", "app-prod"))
			if err != nil {
				t.Fatalf("read the launch's baseline: %v", err)
			}
			// The snapshot folds each promoted runtime's commit as
			// "<prod hostname>:<commit>" (bundle.computeSourceSnapshotMulti).
			sum := sha256.Sum256([]byte("app:" + heads["main"]))
			if want := hex.EncodeToString(sum[:]); state.SourceSnapshot.GitCommitSHA != want {
				t.Errorf("baseline commit digest = %q, want the digest of app at the origin's main %q", state.SourceSnapshot.GitCommitSHA, heads["main"])
			}
			if len(ssh.hosts) != 0 {
				t.Errorf("launch reached %v over ssh", ssh.hosts)
			}
		})
	}
}

// TestLaunchProduction_PoolKeyedPart_TokenNeverInTheResponse: the pool's
// GIT_TOKEN the origin read uses reaches no response, state file or audit
// record — on a launch that reads the origin and on one refused for an
// origin it could not read.
func TestLaunchProduction_PoolKeyedPart_TokenNeverInTheResponse(t *testing.T) {
	t.Parallel()
	url, _ := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	tests := []struct {
		name       string
		trackedRef string
		want       topology.LaunchProductionStatus
	}{
		{"origin read", "main", topology.LaunchStatusReadyToLaunch},
		{"origin unreadable", "release", topology.LaunchStatusSourceControlRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := poolPartPair(t, url, tt.trackedRef)
			input := WorkflowInput{
				Workflow: workflowLaunchProduction, ProductionProjectName: "app-prod", TargetService: "appstage",
				EnvClassifications: map[string]string{"LOG_LEVEL": "plain-config"}, SkipBuildIntegration: []string{testPool},
			}

			result, _, err := handleLaunchProduction(context.Background(), "proj-1", poolPartClient(), nil, nil, input, stateDir,
				poolPartRuntime(), &hostLogSSH{}, "")

			if err != nil {
				t.Fatalf("launch: %v", err)
			}
			text := extractText(result)
			if resp := decodeLaunchResp(t, []byte(text)); resp.Status != tt.want {
				t.Fatalf("launch = %s, want %s\n%s", resp.Status, tt.want, text)
			}
			if strings.Contains(text, poolPartGitToken) {
				t.Errorf("the response carries the pool's GIT_TOKEN: %s", text)
			}
			scanLaunchStateDirForSentinels(t, stateDir, poolPartGitToken)
		})
	}
}
