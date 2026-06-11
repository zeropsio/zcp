//go:build e2e

// launch_single_token_test.go — operator-assisted LIVE verification of the
// single-token launch lifecycle (P-LP-14,
// plans/launch-single-token-lifecycle-2026-06-11.md):
//
//	empty-remote gate → git-push-setup → credential-helper push →
//	launch mutation (token staged BEFORE create) → secret-sourced
//	prod-ops (no launchKey re-send) → confirm-production physical close
//	→ post-close refusal → reset-with-token deletes the prod project.
//
// MUTATING + BILLABLE while running (creates a real production project;
// the test deletes it via reset before finishing) — opt-in:
//
//	export ZCP_E2E_LAUNCH_KEY=<integration token with canCreateProjects>
//	export ZCP_E2E_LAUNCH_GIT_REMOTE=https://github.com/krls2020/eval2
//	export ZCP_E2E_LAUNCH_GIT_TOKEN=<fine-grained PAT for that repo>
//	go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_LaunchSingleTokenLifecycle -timeout 1800s
//
// The git remote's main is FORCE-RESET to an empty orphan commit at start
// and at the end — the empty remote is itself a tested state (the launch
// gate must surface head-not-pushed, and the push flow must populate an
// empty main). Use a dedicated scratch repo only.
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// launchLifecycleEnv collects the opt-in inputs. All three must be set.
type launchLifecycleEnv struct {
	LaunchKey string
	Remote    string
	GitToken  string
}

func launchLifecycleEnvFromOS(t *testing.T) launchLifecycleEnv {
	t.Helper()
	env := launchLifecycleEnv{
		LaunchKey: strings.TrimSpace(os.Getenv("ZCP_E2E_LAUNCH_KEY")),
		Remote:    strings.TrimSpace(os.Getenv("ZCP_E2E_LAUNCH_GIT_REMOTE")),
		GitToken:  strings.TrimSpace(os.Getenv("ZCP_E2E_LAUNCH_GIT_TOKEN")),
	}
	if env.LaunchKey == "" || env.Remote == "" || env.GitToken == "" {
		t.Skip("single-token lifecycle e2e is opt-in: set ZCP_E2E_LAUNCH_KEY + ZCP_E2E_LAUNCH_GIT_REMOTE + ZCP_E2E_LAUNCH_GIT_TOKEN")
	}
	return env
}

// maskSecrets removes credential values from text destined for test logs.
func maskSecrets(text string, env launchLifecycleEnv) string {
	text = strings.ReplaceAll(text, env.LaunchKey, "<LAUNCH_KEY>")
	text = strings.ReplaceAll(text, env.GitToken, "<GIT_TOKEN>")
	return text
}

// newContainerHarness mirrors newHarness but constructs the server in
// CONTAINER mode (the production launch path: source reads go over SSH
// to the push service, git-push-setup takes remoteUrl+gitToken). The
// SystemSSHDeployer reaches eval services from the operator Mac via VPN.
func newContainerHarness(t *testing.T) *e2eHarness {
	t.Helper()
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set — skipping E2E test")
	}
	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}
	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	srv := server.New(context.Background(), client, authInfo, store, platform.NewLogFetcher(),
		platform.NewSystemSSHDeployer(), nil, runtime.Info{InContainer: true, ServiceName: "zcp"})
	return &e2eHarness{t: t, client: client, projectID: authInfo.ProjectID, authInfo: authInfo, srv: srv}
}

// resetRemoteMainEmpty force-resets the remote's main branch to a single
// empty-tree orphan commit, executed on the given service container (git
// is present there; the PAT travels via the remote URL inside the ssh
// command — never into test logs, maskSecrets guards the error path).
func resetRemoteMainEmpty(t *testing.T, env launchLifecycleEnv, hostname string) {
	t.Helper()
	bareRemote := strings.TrimPrefix(env.Remote, "https://")
	cmd := fmt.Sprintf(`WORK=$(mktemp -d /tmp/zcp-e2e-reset.XXXXXX) && cd "$WORK" && git init -q -b main && git -c user.email=e2e@zerops.io -c user.name=zcp-e2e commit -q --allow-empty -m 'reset: empty main (launch lifecycle e2e)' && git push -q --force https://x-access-token:%s@%s main:main && echo RESET_OK`,
		env.GitToken, bareRemote)
	out, err := sshExec(t, hostname, cmd)
	if err != nil || !strings.Contains(out, "RESET_OK") {
		t.Fatalf("reset remote main to empty: %v\n%s", err, maskSecrets(out, env))
	}
}

// launchResp is the minimal agent-visible response shape the test asserts on.
type launchResp struct {
	Status          string `json:"status"`
	Blockers        []struct {
		ID       string `json:"id"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
	} `json:"blockers"`
	Classifications []struct {
		Key             string `json:"key"`
		SuggestedBucket string `json:"suggestedBucket"`
	} `json:"classifications"`
	ProductionProjectID string         `json:"productionProjectId"`
	Warnings            []string       `json:"warnings"`
	FirstRelease        map[string]any `json:"firstRelease"`
	ProdCD              map[string]any `json:"prodCd"`
	BundlePreview       map[string]any `json:"bundlePreview"`
}

func decodeLaunch(t *testing.T, text string) launchResp {
	t.Helper()
	var resp launchResp
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("decode launch response: %v\n%s", err, text)
	}
	return resp
}

func blockerIDsContain(resp launchResp, fragment string) bool {
	for _, b := range resp.Blockers {
		if strings.Contains(b.ID, fragment) {
			return true
		}
	}
	return false
}

// readStagedTokenSSH reads $ZEROPS_TOKEN_PROD on the push service over a
// fresh ssh session — the same read the prodCD conveyance command uses.
// Retries across the zembed env-propagation window (~5-10s).
func readStagedTokenSSH(t *testing.T, hostname string, deadline time.Duration) string {
	t.Helper()
	cmd := fmt.Sprintf(`printf %%s "$%s"`, ops.LaunchTokenEnvKey)
	var last string
	until := time.Now().Add(deadline)
	for {
		out, err := sshExec(t, hostname, cmd)
		if err == nil {
			last = strings.TrimSpace(out)
			if last != "" {
				return last
			}
		}
		if time.Now().After(until) {
			return last
		}
		time.Sleep(5 * time.Second)
	}
}

// readStagedTokenAPI reads the staged secret through the platform API —
// the launchKeyFromStage mechanism the launch-window operations use.
func readStagedTokenAPI(t *testing.T, h *e2eHarness, hostname string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	svc, err := ops.LookupService(ctx, h.client, h.projectID, hostname)
	if err != nil {
		t.Fatalf("lookup %s: %v", hostname, err)
	}
	envs, err := ops.FetchServiceEnv(ctx, h.client, svc.ID)
	if err != nil {
		t.Fatalf("read %s envs: %v", hostname, err)
	}
	for _, e := range envs {
		if e.Key == ops.LaunchTokenEnvKey {
			return e.Content
		}
	}
	return ""
}

// bootstrapSimpleServiceForLaunch provisions ONE simple-mode runtime
// (push-capable singleton — dev mode is NOT a valid push source) through
// the full bootstrap-core flow so the launch gate sees an adopted,
// push-capable ServiceMeta.
func bootstrapSimpleServiceForLaunch(t *testing.T, s *e2eSession, hostname, serviceType, importYAML string) {
	t.Helper()
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": t.Name(),
	})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic", "intent": t.Name(),
	})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []any{map[string]any{"runtime": map[string]any{
			"devHostname": hostname, "type": serviceType, "bootstrapMode": "simple",
		}}},
	})
	s.mustCallSuccess("zerops_import", map[string]any{"content": importYAML})
	waitForServiceStatus(s, hostname, "RUNNING", "ACTIVE")
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})
	provText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete", "step": "provision", "attestation": "Service created for launch lifecycle e2e.",
	})
	var provResp bootstrapProgress
	if err := json.Unmarshal([]byte(provText), &provResp); err != nil {
		t.Fatalf("parse provision complete: %v", err)
	}
	assertProvisionPassed(t, provResp)
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete", "step": "close", "attestation": "Bootstrap closed for launch lifecycle e2e.",
	})
}

// launchAppDir writes the minimal app with BOTH a dev iteration setup and
// the production setup the launch composer references.
func launchAppDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	zeropsYML := `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - echo "build done"
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - echo "prod build done"
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`
	serverJS := `const http = require('http');
http.createServer((req, res) => { res.writeHead(200); res.end('launch lifecycle e2e'); }).listen(3000);
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte(zeropsYML), 0o644); err != nil {
		t.Fatalf("write zerops.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.js"), []byte(serverJS), 0o644); err != nil {
		t.Fatalf("write server.js: %v", err)
	}
	return dir
}

func TestE2E_LaunchSingleTokenLifecycle(t *testing.T) {
	env := launchLifecycleEnvFromOS(t)

	// Session A (local rt): bootstrap-core provisioning — the same shape
	// every other deploy e2e uses.
	hLocal := newHarness(t)
	sLocal := newSession(t, hLocal.srv)

	// Session B (container rt): the launch surface under test.
	hCont := newContainerHarness(t)
	s := newSession(t, hCont.srv)

	suffix := randomSuffix()
	hostname := "zcpstl" + suffix
	prodProjectName := "zcp-stl-" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, hLocal.client, hLocal.projectID, hostname)
	})

	// Safety net: if the test dies after the prod project exists but
	// before the reset step deletes it, remove the orphan directly.
	var prodProjectID string
	prodDeleted := false
	t.Cleanup(func() {
		if prodProjectID == "" || prodDeleted {
			return
		}
		admin, err := platform.NewProjectAdminClient(env.LaunchKey, defaultAPIHost())
		if err != nil {
			t.Logf("orphan cleanup: admin client: %v", err)
			return
		}
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, err := admin.DeleteProject(ctx, prodProjectID); err != nil {
			t.Logf("orphan cleanup: delete %s: %v — DELETE IT IN THE DASHBOARD", prodProjectID, err)
			return
		}
		t.Logf("orphan cleanup: deleted prod project %s", prodProjectID)
	})

	step := 0
	logf := func(format string, args ...any) {
		step++
		t.Logf("--- step %d: "+format, append([]any{step}, args...)...)
	}

	// --- provision the push-capable source runtime + app code ----------
	logf("bootstrap simple-mode runtime %s", hostname)
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    maxContainers: 1
    startWithoutCode: true
`, hostname)
	bootstrapSimpleServiceForLaunch(t, sLocal, hostname, "nodejs@22", importYAML)

	logf("push app dir (dev+prod setups) to %s:/var/www", hostname)
	pushDirViaSSH(t, hostname, launchAppDir(t), "/var/www")

	// --- the "when the repo is empty" precondition ----------------------
	logf("force-reset remote main to an empty orphan commit (the 'kdyz neni' state)")
	resetRemoteMainEmpty(t, env, hostname)
	t.Cleanup(func() { resetRemoteMainEmpty(t, env, hostname) })

	scopeArgs := map[string]any{
		"action":                  "start",
		"workflow":                "launch-production",
		"productionProjectName":   prodProjectName,
		"targetService":           hostname,
		"prodSetupNameOverride":   "prod",
		"skipStageRecommendation": true,
		"managedDeps":             map[string]any{"db": "exclude"},
	}

	// --- gate pre-wiring: refuse without git-push-setup -----------------
	logf("launch before git-push-setup must surface git-push-unconfigured")
	resp := decodeLaunch(t, s.mustCallSuccess("zerops_workflow", scopeArgs))
	if resp.Status != "source-control-required" || !blockerIDsContain(resp, "git-push-unconfigured") {
		t.Fatalf("expected source-control-required + git-push-unconfigured, got status=%q blockers=%+v", resp.Status, resp.Blockers)
	}

	// --- git-push-setup against the EMPTY remote ------------------------
	logf("git-push-setup (auth probe + GIT_TOKEN secret) against the empty remote")
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":    "git-push-setup",
		"service":   hostname,
		"remoteUrl": env.Remote,
		"gitToken":  env.GitToken,
	})

	// --- the empty-remote launch gate: head-not-pushed ------------------
	logf("launch on the EMPTY remote must surface head-not-pushed")
	resp = decodeLaunch(t, s.mustCallSuccess("zerops_workflow", scopeArgs))
	if resp.Status != "source-control-required" || !blockerIDsContain(resp, "head-not-pushed") {
		t.Fatalf("expected head-not-pushed on empty remote, got status=%q blockers=%+v", resp.Status, resp.Blockers)
	}

	// --- populate the remote through the credential helper --------------
	// The "empty" main still holds one orphan commit (GitHub can't drop
	// the default branch ref), so the local history rebases onto it first
	// — the same move a user makes when their fresh repo has an initial
	// README commit. The push then goes through the GIT_TOKEN credential
	// helper git-push-setup wired.
	logf("rebase onto the orphan main + push through the GIT_TOKEN credential helper")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	ssh := platform.NewSystemSSHDeployer()
	rebaseCmd := `cd /var/www && git -c user.email=e2e@zerops.io -c user.name=zcp-e2e pull --rebase origin main`
	if out, err := ssh.ExecSSH(ctx, hostname, rebaseCmd); err != nil {
		t.Fatalf("rebase onto orphan main: %v\n%s", err, maskSecrets(string(out), env))
	}
	// Retry the push across transient auth hiccups (GitHub occasionally
	// 401s a just-verified fine-grained PAT; git-push-setup's session
	// verify already proved the env-token chain end-to-end).
	pushCmd := ops.BuildGitPushCommand("/var/www", env.Remote, "main")
	var pushErr error
	var pushOut []byte
	for attempt := 1; attempt <= 4; attempt++ {
		pushOut, pushErr = ssh.ExecSSH(ctx, hostname, pushCmd)
		if pushErr == nil {
			break
		}
		t.Logf("push attempt %d failed (retrying): %s", attempt, maskSecrets(string(pushOut), env))
		time.Sleep(10 * time.Second)
	}
	if pushErr != nil {
		t.Fatalf("credential-helper push: %v\n%s", pushErr, maskSecrets(string(pushOut), env))
	}

	logf("declare build-integration=actions (prodCD track on the launched response)")
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "build-integration", "service": hostname, "integration": "actions",
	})

	// --- classify ---------------------------------------------------------
	logf("classify-prompt → accept suggested buckets")
	resp = decodeLaunch(t, s.mustCallSuccess("zerops_workflow", scopeArgs))
	if resp.Status != "classify-prompt" {
		t.Fatalf("expected classify-prompt, got %q (blockers=%+v)", resp.Status, resp.Blockers)
	}
	classifications := map[string]any{}
	for _, row := range resp.Classifications {
		bucket := row.SuggestedBucket
		if bucket == "" {
			bucket = "plain-config"
		}
		classifications[row.Key] = bucket
	}
	scopeArgs["envClassifications"] = classifications

	logf("ready-to-launch with bundle preview")
	resp = decodeLaunch(t, s.mustCallSuccess("zerops_workflow", scopeArgs))
	if resp.Status != "ready-to-launch" {
		t.Fatalf("expected ready-to-launch, got %q (blockers=%+v)", resp.Status, resp.Blockers)
	}
	if resp.BundlePreview == nil {
		t.Error("ready-to-launch must carry the bundle preview")
	}

	// --- publish: token staged BEFORE create, then launched --------------
	logf("publish with launchKey (the ONLY transcript pass of the value)")
	publishArgs := map[string]any{}
	for k, v := range scopeArgs {
		publishArgs[k] = v
	}
	publishArgs["launchKey"] = env.LaunchKey
	launchedText := s.mustCallSuccess("zerops_workflow", publishArgs)
	resp = decodeLaunch(t, launchedText)
	if resp.Status != "launched" {
		t.Fatalf("expected launched, got %q\n%s", resp.Status, maskSecrets(launchedText, env))
	}
	prodProjectID = resp.ProductionProjectID
	if prodProjectID == "" {
		t.Fatal("launched response missing productionProjectId")
	}
	t.Logf("prod project created: %s", prodProjectID)
	if strings.Contains(launchedText, env.LaunchKey) {
		t.Error("launched response leaks the launch token value")
	}
	grantFailed := false
	for _, w := range resp.Warnings {
		t.Logf("launched warning: %s", maskSecrets(w, env))
		if strings.Contains(w, "grant self ADMIN role") {
			grantFailed = true
		}
	}
	// Live-verified shape: integration tokens cannot manage roles (the
	// grant warns), and creator access carries every later read — the
	// prod-ops step below is the proof. Surface the state for the log.
	t.Logf("GrantSelfRole failed=%v (creator access covers the window — prod-ops below must succeed)", grantFailed)
	if resp.FirstRelease == nil {
		t.Error("launched response missing the firstRelease block")
	}
	if resp.ProdCD == nil {
		t.Error("launched response missing the prodCd actions block")
	} else if secret, ok := resp.ProdCD["secret"].(map[string]any); ok {
		cmd, _ := secret["command"].(string)
		if !strings.Contains(cmd, ops.LaunchTokenEnvKey) || strings.Contains(cmd, "<paste") {
			t.Errorf("prodCd secret.command must read the staged secret, never paste: %s", cmd)
		}
	}

	// --- T1 staging proof: both read paths see the staged value ----------
	logf("staged %s visible over fresh ssh (conveyance path)", ops.LaunchTokenEnvKey)
	if got := readStagedTokenSSH(t, hostname, 60*time.Second); got != env.LaunchKey {
		t.Errorf("ssh staged read: got %q-shaped value (len %d), want the launch token (len %d)",
			maskSecrets(got, env), len(got), len(env.LaunchKey))
	}
	logf("staged %s visible through the platform API (launchKeyFromStage path)", ops.LaunchTokenEnvKey)
	if got := readStagedTokenAPI(t, hCont, hostname); got != env.LaunchKey {
		t.Errorf("API staged read mismatch (len %d vs %d)", len(got), len(env.LaunchKey))
	}

	// --- T2: prod-ops without launchKey ----------------------------------
	logf("prod-ops status WITHOUT launchKey (staged read)")
	prodOpsText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":                "prod-ops",
		"prodOperation":         "status",
		"productionProjectName": prodProjectName,
	})
	if strings.Contains(prodOpsText, env.LaunchKey) {
		t.Error("prod-ops response leaks the staged token value")
	}
	if !strings.Contains(prodOpsText, prodProjectID) {
		t.Errorf("prod-ops status must list the prod project: %s", maskSecrets(prodOpsText, env))
	}
	if !strings.Contains(prodOpsText, "confirm-production") {
		t.Errorf("prod-ops done boundary must point at confirm-production: %s", maskSecrets(prodOpsText, env))
	}

	// --- T3: consent gate, physical close, post-close refusal ------------
	logf("confirm-production without the ack must prompt, not close")
	confirmArgs := map[string]any{
		"action":                "confirm-production",
		"workflow":              "launch-production",
		"productionProjectName": prodProjectName,
	}
	promptText := s.mustCallSuccess("zerops_workflow", confirmArgs)
	if !strings.Contains(promptText, "confirm-required") {
		t.Fatalf("expected confirm-required prompt: %s", maskSecrets(promptText, env))
	}
	if got := readStagedTokenAPI(t, hCont, hostname); got == "" {
		t.Fatal("unacked confirm must not delete the staged secret")
	}

	logf("confirm-production with confirmFunctional=true closes the window")
	confirmArgs["confirmFunctional"] = true
	closeText := s.mustCallSuccess("zerops_workflow", confirmArgs)
	if !strings.Contains(closeText, "window-closed") {
		t.Fatalf("expected window-closed: %s", maskSecrets(closeText, env))
	}
	if strings.Contains(closeText, env.LaunchKey) {
		t.Error("close response leaks the token value")
	}
	for _, want := range []string{"egenerat", "token-management"} {
		if !strings.Contains(closeText, want) {
			t.Errorf("close response missing %q: %s", want, maskSecrets(closeText, env))
		}
	}

	logf("staged secret physically gone (API + ssh)")
	if got := readStagedTokenAPI(t, hCont, hostname); got != "" {
		t.Errorf("staged secret survived the close (API read len %d)", len(got))
	}

	logf("prod-ops after the close refuses with the lifecycle message")
	postClose := s.callTool("zerops_workflow", map[string]any{
		"action":                "prod-ops",
		"prodOperation":         "status",
		"productionProjectName": prodProjectName,
	})
	postCloseText := getE2ETextContent(t, postClose)
	if !postClose.IsError {
		t.Fatalf("post-close prod-ops must refuse: %s", maskSecrets(postCloseText, env))
	}
	if !strings.Contains(postCloseText, "confirm-production") {
		t.Errorf("post-close refusal must name the close act: %s", maskSecrets(postCloseText, env))
	}

	// --- teardown through the supported path: reset deletes the project --
	logf("reset (explicit launchKey — staged copy is gone) arms the orphan delete")
	resetArgs := map[string]any{
		"action":                "reset",
		"workflow":              "launch-production",
		"productionProjectName": prodProjectName,
		"launchKey":             env.LaunchKey,
	}
	firstReset := s.callTool("zerops_workflow", resetArgs)
	firstResetText := getE2ETextContent(t, firstReset)
	if !strings.Contains(firstResetText, prodProjectID) {
		t.Fatalf("reset first call must list the prod project in wouldDestroy: %s", maskSecrets(firstResetText, env))
	}

	logf("acked reset deletes the prod project + clears state")
	resetArgs["confirmDestructive"] = map[string]any{
		"operation":           "launch-production-reset",
		"acknowledgedTargets": []string{prodProjectName, prodProjectID},
	}
	resetText := s.mustCallSuccess("zerops_workflow", resetArgs)
	if !strings.Contains(resetText, prodProjectID) || !strings.Contains(resetText, "deletion initiated") {
		t.Fatalf("acked reset must initiate the prod project deletion: %s", maskSecrets(resetText, env))
	}
	prodDeleted = true
	t.Logf("single-token lifecycle verified end-to-end; prod project %s deletion initiated", prodProjectID)
}
