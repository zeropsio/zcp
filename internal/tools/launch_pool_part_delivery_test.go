// Tests for: tools/workflow_release.go, tools/workflow_launch_production.go,
// tools/launch_reset.go, tools/pool_part_origin.go — a launched pool-keyed
// part is delivered as every other part is (docs/spec-workflows.md §10
// P-LP-21): its release tags the commit its prompt showed on the origin,
// only while it is still the tip, and the launched response carries the
// production Actions block. The launch token is staged on zcp's own service
// and recorded where it lands (P-LP-17); the origin is fetched once per tool
// call over HTTPS, and no failing fetch surfaces the pool's GIT_TOKEN
// (P-LP-20). Every origin is served over HTTPS (origintest), with the pool's
// token required where the test says so.
package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/origintest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolPartReadyInput is the launch input that reaches ready-to-launch.
func poolPartReadyInput(target string) WorkflowInput {
	return WorkflowInput{
		Workflow: workflowLaunchProduction, ProductionProjectName: "app-prod", TargetService: target,
		EnvClassifications: map[string]string{"LOG_LEVEL": "plain-config"}, SkipBuildIntegration: []string{testPool},
	}
}

// poolPartGuardedOrigin serves the part's origin requiring the pool's
// GIT_TOKEN, exactly as zcp's helper sends it.
func poolPartGuardedOrigin(t *testing.T) (*origintest.Server, string, map[string]string) {
	t.Helper()
	srv, url, heads := poolPartOriginOn(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	srv.RequireToken(poolPartOriginRepo, poolPartGitToken)
	return srv, url, heads
}

// poolPartLaunchState reads the launch's state file as written, by its JSON
// keys.
func poolPartLaunchState(t *testing.T, stateDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateDir, launchStateDir, generateLaunchID("proj-1", "app-prod")+".json"))
	if err != nil {
		t.Fatalf("read the launch's state: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode the launch's state: %v", err)
	}
	return state
}

// poolPartStagedToken reports whether the service holds a staged launch
// token in the mock's env store.
func poolPartStagedToken(t *testing.T, client *platform.Mock, serviceID string) bool {
	t.Helper()
	envs, err := client.GetServiceEnv(context.Background(), serviceID)
	if err != nil {
		t.Fatalf("read %s's env: %v", serviceID, err)
	}
	for _, e := range envs {
		if e.Key == ops.LaunchTokenEnvKey {
			return true
		}
	}
	return false
}

// TestLaunchProduction_PoolKeyedPart_FetchesTheOriginOncePerCall: one call
// reads the origin once — its read-side gate, source read and baseline share
// the fetch — and the next call fetches again.
func TestLaunchProduction_PoolKeyedPart_FetchesTheOriginOncePerCall(t *testing.T) {
	t.Parallel()
	srv, url, _ := poolPartGuardedOrigin(t)
	stateDir := poolPartPair(t, url, "main")

	for call := 1; call <= 2; call++ {
		result, _, err := handleLaunchProduction(context.Background(), "proj-1", poolPartClient(), nil, nil,
			poolPartReadyInput("appstage"), stateDir, poolPartRuntime(), &hostLogSSH{}, "")
		if err != nil {
			t.Fatalf("launch: %v", err)
		}
		if resp := decodeLaunchResp(t, []byte(extractText(result))); resp.Status != topology.LaunchStatusReadyToLaunch {
			t.Fatalf("launch = %s, want ready-to-launch\n%s", resp.Status, extractText(result))
		}
		if got := srv.Fetches(poolPartOriginRepo); got != call {
			t.Errorf("after call %d the origin was fetched %d times, want %d", call, got, call)
		}
	}
	if !srv.SawToken(poolPartOriginRepo) {
		t.Error("the origin was never sent the pool's GIT_TOKEN")
	}
}

// poolPartPktLine frames s as one git pkt-line.
func poolPartPktLine(s string) string {
	const hex = "0123456789abcdef"
	n := len(s) + 4
	return string([]byte{hex[n>>12&0xf], hex[n>>8&0xf], hex[n>>4&0xf], hex[n&0xf]}) + s
}

// TestLaunchProduction_PoolKeyedPart_RefusingOrigin_NeverSurfacesTheToken: an
// HTTPS origin that is sent the pool's GIT_TOKEN — the exact credential — and
// echoes it back in its error makes launch and export refuse naming the
// origin; what they surface has the echo taken out, and the token reaches no
// answer, state file or audit record in any form.
func TestLaunchProduction_PoolKeyedPart_RefusingOrigin_NeverSurfacesTheToken(t *testing.T) {
	t.Parallel()
	basic := origintest.BasicCredential(poolPartGitToken)
	var sent atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != basic {
			w.Header().Set("WWW-Authenticate", `Basic realm="origin"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sent.Store(true)
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = io.WriteString(w, "001e# service=git-upload-pack\n0000"+
			poolPartPktLine("ERR denied for "+poolPartGitToken+" sent as "+auth+"\n"))
	}))
	t.Cleanup(srv.Close)
	stateDir := poolPartPair(t, srv.URL+"/acme/app.git", "main")
	forms := []string{
		poolPartGitToken,
		strings.TrimPrefix(basic, "Basic "),
		base64.RawStdEncoding.EncodeToString([]byte("oauth2:" + poolPartGitToken)),
		url.QueryEscape(poolPartGitToken),
	}

	result, _, err := handleLaunchProduction(context.Background(), "proj-1", poolPartClient(), nil, nil,
		poolPartReadyInput("appstage"), stateDir, poolPartRuntime(), &hostLogSSH{}, "")
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	launchText := extractText(result)
	exported, _, err := handleExport(context.Background(), "proj1", nil, poolPartExportClient(), WorkflowInput{
		Workflow: workflowExport, TargetService: "appstage",
		EnvClassifications: map[string]string{"LOG_LEVEL": "plain-config"},
	}, &hostLogSSH{}, stateDir, poolPartRuntime())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	exportText := extractText(exported)

	if !sent.Load() {
		t.Fatal("the origin was never sent the pool's exact GIT_TOKEN credential; the test proves nothing")
	}
	for where, text := range map[string]string{"launch": launchText, "export": exportText} {
		redacted := strings.Contains(text, "<redacted>") || strings.Contains(text, `\u003credacted\u003e`)
		if !strings.Contains(text, srv.URL) || !redacted {
			t.Errorf("%s does not refuse naming the origin with the echo taken out: %s", where, text)
		}
		for _, form := range forms {
			if strings.Contains(text, form) {
				t.Errorf("the %s answer carries the token as %q: %s", where, form, text)
			}
		}
	}
	scanLaunchStateDirForSentinels(t, stateDir, forms...)
}

// poolPartWorkflowServer registers zerops_workflow over the pool-keyed
// pair's project.
func poolPartWorkflowServer(t *testing.T, client platform.Client, stateDir string, ssh *hostLogSSH) *mcp.Server {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	RegisterWorkflow(srv, client, nil, "proj-1", nil, engine, nil, stateDir, "", nil, ssh, poolPartRuntime(), "")
	return srv
}

// poolPartRelease calls action=release on service — with version and the
// commit the prompt named, when set.
func poolPartRelease(t *testing.T, srv *mcp.Server, service, version, commit string) (map[string]any, string, bool) {
	t.Helper()
	args := map[string]any{"action": "release", "service": service}
	if version != "" {
		args["releaseVersion"] = version
	}
	if commit != "" {
		args["releaseCommit"] = commit
	}
	result := callTool(t, srv, "zerops_workflow", args)
	text := getTextContent(t, result)
	var out map[string]any
	if !result.IsError {
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("release answer is not JSON: %v\n%s", err, text)
		}
	}
	return out, text, result.IsError
}

// poolPartPromptedCommit asks the stage's release prompt and answers the
// full commit it names.
func poolPartPromptedCommit(t *testing.T, srv *mcp.Server) (map[string]any, string) {
	t.Helper()
	prompt, text, isErr := poolPartRelease(t, srv, "appstage", "", "")
	if isErr || prompt["status"] != "release-prompt" {
		t.Fatalf("release prompt = %s, want release-prompt", text)
	}
	commit, _ := prompt["commit"].(string)
	if commit == "" {
		t.Fatalf("the release prompt names no commit: %s", text)
	}
	return prompt, commit
}

// TestRelease_PoolKeyedPart_TagsTheOriginTip: a release of a pool-keyed
// pair, from either hostname, names the origin's tracked tip — its full
// commit — and the next version, then tags that commit on the origin over
// HTTPS with the pool's GIT_TOKEN; nothing reaches the pool, a lease or the
// stage over ssh.
func TestRelease_PoolKeyedPart_TagsTheOriginTip(t *testing.T) {
	t.Parallel()
	origin, url, heads := poolPartGuardedOrigin(t)
	stateDir := poolPartPair(t, url, "main")
	ssh := &hostLogSSH{}
	srv := poolPartWorkflowServer(t, poolPartClient(), stateDir, ssh)

	prompt, commit := poolPartPromptedCommit(t, srv)
	if prompt["suggestedVersion"] != "v1.0.0" || commit != heads["main"] {
		t.Errorf("prompt = %v, want v1.0.0 at the origin's main %s", prompt, heads["main"])
	}

	released, text, isErr := poolPartRelease(t, srv, testPool, "v1.0.0", commit)
	if isErr || released["status"] != "released" || released["version"] != "v1.0.0" {
		t.Fatalf("release = %s, want released v1.0.0", text)
	}
	if at := origin.TagAt(t, poolPartOriginRepo, "v1.0.0"); at != heads["main"] {
		t.Errorf("v1.0.0 on the origin points at %q, want main's tip %q", at, heads["main"])
	}
	if !origin.SawToken(poolPartOriginRepo) {
		t.Error("the origin was never sent the pool's GIT_TOKEN")
	}

	next, _ := poolPartPromptedCommit(t, srv)
	if next["suggestedVersion"] != "v1.0.1" {
		t.Errorf("the next prompt = %v, want v1.0.1 suggested after v1.0.0", next)
	}
	if dup, text, isErr := poolPartRelease(t, srv, "appstage", "v1.0.0", commit); !isErr {
		t.Errorf("a second v1.0.0 was not refused: %v %s", dup, text)
	}
	if strings.Contains(text, poolPartGitToken) {
		t.Errorf("a release answer carries the pool's GIT_TOKEN: %s", text)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("release reached %v over ssh", ssh.hosts)
	}
}

// TestRelease_PoolKeyedPart_TipMovedSinceThePrompt_Refused: the confirming
// call tags the commit its prompt showed or nothing — without that commit it
// is refused naming it, and when a lease pushed since the prompt it is
// refused naming both commits, tip moved X→Y; nothing lands on the origin.
func TestRelease_PoolKeyedPart_TipMovedSinceThePrompt_Refused(t *testing.T) {
	t.Parallel()
	origin, url, heads := poolPartGuardedOrigin(t)
	stateDir := poolPartPair(t, url, "main")
	srv := poolPartWorkflowServer(t, poolPartClient(), stateDir, &hostLogSSH{})
	_, shown := poolPartPromptedCommit(t, srv)

	if _, text, isErr := poolPartRelease(t, srv, "appstage", "v1.0.0", ""); !isErr || !strings.Contains(text, "releaseCommit") {
		t.Errorf("a confirm without the prompt's commit = %s, want refused naming releaseCommit", text)
	}
	moved := origin.Commit(t, poolPartOriginRepo, map[string]string{"later.txt": "pushed from a lease\n"})
	_, text, isErr := poolPartRelease(t, srv, "appstage", "v1.0.0", shown)

	if !isErr || !strings.Contains(text, "tip moved") {
		t.Fatalf("the release of a moved tip = %s, want refused: tip moved", text)
	}
	for _, want := range []string{heads["main"][:10], moved[:10]} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not name %s: %s", want, text)
		}
	}
	if at := origin.TagAt(t, poolPartOriginRepo, "v1.0.0"); at != "" {
		t.Errorf("v1.0.0 landed on %q despite the moved tip", at)
	}
}

// TestRelease_PoolKeyedPart_RefusedOrigin_NamesWhatToFix: a release whose
// origin cannot be read is refused naming the origin and what to fix, with
// no ssh and no token in the answer.
func TestRelease_PoolKeyedPart_RefusedOrigin_NamesWhatToFix(t *testing.T) {
	t.Parallel()
	missing := poolPartMissingOrigin(t)
	stateDir := poolPartPair(t, missing, "main")
	ssh := &hostLogSSH{}
	srv := poolPartWorkflowServer(t, poolPartClient(), stateDir, ssh)

	_, text, isErr := poolPartRelease(t, srv, "appstage", "", "")

	if !isErr {
		t.Fatalf("release of an unreadable origin = %s, want a refusal", text)
	}
	for _, want := range []string{missing, "GIT_TOKEN"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not name %q: %s", want, text)
		}
	}
	if strings.Contains(text, poolPartGitToken) {
		t.Errorf("the refusal carries the pool's GIT_TOKEN: %s", text)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("release reached %v over ssh", ssh.hosts)
	}
}

// TestProdCDActionsBlock_PoolKeyedPart_ConveysFromZcpsOwnService: the
// production Actions block of a launched pool-keyed part carries the same
// workflow file; its secret command reads the launch token from zcp's own
// service, where it is staged (`zcp self launch-token`, no ssh), takes
// GH_TOKEN as the PAT the person gives, and names the lease the workflow
// file is committed from.
func TestProdCDActionsBlock_PoolKeyedPart_ConveysFromZcpsOwnService(t *testing.T) {
	t.Parallel()
	stateDir := poolPartPair(t, "https://github.com/acme/app.git", "main")
	var state launchState
	if err := json.Unmarshal([]byte(`{"targetServiceHostname":"appstage","tokenStagedOn":"zcp",`+
		`"importedServices":[{"id":"svc-prod-app","name":"app"}],"runtimeProds":[{"prodHostname":"app","setupName":"prod"}]}`), &state); err != nil {
		t.Fatalf("state: %v", err)
	}

	block := prodCDActionsBlock(stateDir, &state)

	if block == nil {
		t.Fatal("no production Actions block for the launched pool-keyed part")
	}
	file, _ := block["workflowFile"].(map[string]any)
	if content, _ := file["content"].(string); !strings.Contains(content, "svc-prod-app") || !strings.Contains(content, "--setup \"prod\"") {
		t.Errorf("the workflow file does not deploy the production runtime: %v", file)
	}
	secret, _ := block["secret"].(map[string]any)
	cmd, _ := secret["command"].(string)
	for _, want := range []string{"zcp self launch-token", "acme/app", "PAT"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("the secret command does not carry %q: %s", want, cmd)
		}
	}
	if strings.Contains(cmd, "ssh") || strings.Contains(cmd, testPool) || strings.Contains(cmd, "appstage") {
		t.Errorf("the secret command reaches a container of the part: %s", cmd)
	}
	if via, _ := block["commitVia"].(string); !strings.Contains(via, "zerops_lease") || !strings.Contains(via, testPool) {
		t.Errorf("commitVia = %q, want the lease of the pool the workflow file is committed from", via)
	}
}

// TestLaunchProduction_PoolKeyedPart_BaselineRecordsWhereTheTokenLands: the
// ready-to-launch baseline records the pair's runtime — the stage — and the
// service the launch token will be staged on — zcp's own — so a reset after
// a cut publish deletes it where it lands.
func TestLaunchProduction_PoolKeyedPart_BaselineRecordsWhereTheTokenLands(t *testing.T) {
	t.Parallel()
	_, url, _ := poolPartGuardedOrigin(t)
	stateDir := poolPartPair(t, url, "main")

	result, _, err := handleLaunchProduction(context.Background(), "proj-1", poolPartClient(), nil, nil,
		poolPartReadyInput(testPool), stateDir, poolPartRuntime(), &hostLogSSH{}, "")
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if resp := decodeLaunchResp(t, []byte(extractText(result))); resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Fatalf("launch = %s, want ready-to-launch\n%s", resp.Status, extractText(result))
	}
	state := poolPartLaunchState(t, stateDir)
	if state["targetServiceHostname"] != "appstage" || state["tokenStagedOn"] != "zcp" {
		t.Errorf("baseline = target %v, token on %v; want the stage appstage and zcp's own service", state["targetServiceHostname"], state["tokenStagedOn"])
	}
}

// TestLaunchReset_PoolKeyedPart_DeletesTheTokenWhereItLanded: a reset
// deletes the staged launch token from the service the state records it
// landed on — zcp's own — not from the pair's runtime.
func TestLaunchReset_PoolKeyedPart_DeletesTheTokenWhereItLanded(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	launchID := generateLaunchID("proj-1", "app-prod")
	if err := writeLaunchState(stateDir, &launchState{
		LaunchID: launchID, SourceProjectID: "proj-1", TargetProjectName: "app-prod",
		TargetServiceHostname: "appstage", Status: topology.LaunchStatusReadyToLaunch,
	}); err != nil {
		t.Fatalf("write state: %v", err)
	}
	path := filepath.Join(stateDir, launchStateDir, launchID+".json")
	var raw map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	raw["tokenStagedOn"] = "zcp"
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("rewrite state: %v", err)
	}
	client := poolPartClient().
		WithServiceEnv("svc-zcp", []platform.ServiceEnvVar{{ID: "udata-zcp-token", Key: ops.LaunchTokenEnvKey, Content: "staged", Sensitive: true}})

	result, _, err := handleLaunchReset(context.Background(), stateDir, "proj-1", client, WorkflowInput{
		ProductionProjectName: "app-prod",
		ConfirmDestructive:    &DestructiveAck{Operation: launchResetOperation, AcknowledgedTargets: []string{"app-prod"}},
	}, "")

	if err != nil || result.IsError {
		t.Fatalf("reset: %v %s", err, extractText(result))
	}
	if poolPartStagedToken(t, client, "svc-zcp") {
		t.Error("the staged launch token outlived the reset on zcp's own service")
	}
}

// TestLaunchProduction_PoolKeyedPart_TokenStagedOnZcpsOwnService: a launch of
// a pool-keyed part stages the launch token on zcp's own service — never the
// pool or the stage, which run code a lease deploys — and records it there;
// the launched answer's Actions block reads it from there.
func TestLaunchProduction_PoolKeyedPart_TokenStagedOnZcpsOwnService(t *testing.T) {
	// non-parallel: swaps the package's project-admin client factory.
	_, url, _ := poolPartGuardedOrigin(t)
	stateDir := poolPartPair(t, url, "main")
	client := poolPartClient()
	defer installMockAdminFactory(t, happyMockAdmin())()
	input := poolPartReadyInput("appstage")
	input.Region = poolPartRegion
	input.LaunchKey = sentinelLaunchKey

	result, _, err := handleLaunchProduction(context.Background(), "proj-1", client, nil, nil, input, stateDir,
		poolPartRuntime(), &hostLogSSH{}, "")

	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusLaunched {
		t.Fatalf("launch = %s %+v, want launched\n%s", resp.Status, resp.Blockers, text)
	}
	if !poolPartStagedToken(t, client, "svc-zcp") {
		t.Error("the launch token is not staged on zcp's own service")
	}
	for _, id := range []string{"svc-appstage", testPoolID} {
		if poolPartStagedToken(t, client, id) {
			t.Errorf("the launch token is staged on %s, which runs code a lease deploys", id)
		}
	}
	if state := poolPartLaunchState(t, stateDir); state["tokenStagedOn"] != "zcp" || state["targetServiceHostname"] != "appstage" {
		t.Errorf("state = token on %v, target %v; want zcp and the stage", state["tokenStagedOn"], state["targetServiceHostname"])
	}
	secret, _ := resp.ProdCD["secret"].(map[string]any)
	if cmd, _ := secret["command"].(string); !strings.Contains(cmd, "zcp self launch-token") {
		t.Errorf("the Actions secret command does not read the token where it is staged: %s", cmd)
	}
	scanLaunchStateDirForSentinels(t, stateDir, poolPartGitToken, sentinelLaunchKey)
}

// TestLaunchProduction_PoolKeyedPart_ThenReleaseShipsTheFirstBuild: a
// pool-keyed part launches to launched — the production Actions block on the
// answer, the release step naming the stage — and that release step then
// tags the origin's tip, the first build.
func TestLaunchProduction_PoolKeyedPart_ThenReleaseShipsTheFirstBuild(t *testing.T) {
	// non-parallel: swaps the package's project-admin client factory.
	origin, url, heads := poolPartGuardedOrigin(t)
	stateDir := poolPartPair(t, url, "main")
	client := poolPartClient()
	defer installMockAdminFactory(t, happyMockAdmin())()
	ssh := &hostLogSSH{}
	input := poolPartReadyInput("appstage")
	input.Region = poolPartRegion
	input.LaunchKey = sentinelLaunchKey

	result, _, err := handleLaunchProduction(context.Background(), "proj-1", client, nil, nil, input, stateDir,
		poolPartRuntime(), ssh, "")

	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != topology.LaunchStatusLaunched {
		t.Fatalf("launch = %s %+v, want launched\n%s", resp.Status, resp.Blockers, text)
	}
	if resp.ProdCD == nil {
		t.Error("the launched answer carries no production Actions block")
	}
	releaseStep := ""
	if resp.FirstRelease != nil {
		releaseStep = strings.Join(resp.FirstRelease.NextSteps, "\n")
	}
	if !strings.Contains(releaseStep, `action="release" service="appstage"`) {
		t.Errorf("the first release does not name the stage's release: %s", releaseStep)
	}

	srv := poolPartWorkflowServer(t, client, stateDir, ssh)
	_, commit := poolPartPromptedCommit(t, srv)
	released, releaseText, isErr := poolPartRelease(t, srv, "appstage", "v1.0.0", commit)
	if isErr || released["status"] != "released" {
		t.Fatalf("the first release = %s, want released", releaseText)
	}
	if at := origin.TagAt(t, poolPartOriginRepo, "v1.0.0"); at != heads["main"] {
		t.Errorf("the first release tagged %q, want the origin's main %q", at, heads["main"])
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("launch or release reached %v over ssh", ssh.hosts)
	}
	scanLaunchStateDirForSentinels(t, stateDir, poolPartGitToken, sentinelLaunchKey)
}

// poolPartTwoStagePair writes a pair keyed by the dev pool with two stages
// — webstage (setup web) and apistage (setup api) — at remoteURL.
func poolPartTwoStagePair(t *testing.T, remoteURL string) string {
	t.Helper()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         testPool,
		Mode:             topology.PlanModeStandard,
		Stages:           []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}},
		BootstrapSession: "test",
		BootstrappedAt:   "2026-10-01T00:00:00Z",
		FirstDeployedAt:  "2026-10-01T01:00:00Z",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        remoteURL,
		TrackedRef:       "main",
		BuildIntegration: topology.BuildIntegrationNone,
		PrimarySetupName: "pool",
		DevPool:          &workflow.DevPoolRef{ServiceID: testPoolID},
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	return stateDir
}

// TestLaunchProduction_PoolKeyedPart_NamedStageCarriedThrough: a launch named
// by the second stage of a pool-keyed pair promotes that stage — never the
// pair's first — and records it as the launch's runtime.
func TestLaunchProduction_PoolKeyedPart_NamedStageCarriedThrough(t *testing.T) {
	t.Parallel()
	const twoStageYAML = "zerops:\n" +
		"  - setup: web\n    build:\n      base: nodejs@22\n      deployFiles: ./\n    run:\n      base: nodejs@22\n      start: node web.js\n" +
		"  - setup: api\n    build:\n      base: nodejs@22\n      deployFiles: ./\n    run:\n      base: nodejs@22\n      start: node api.js\n"
	_, url, _ := poolPartOriginOn(t, map[string]string{"zerops.yaml": twoStageYAML}, nil)
	stateDir := poolPartTwoStagePair(t, url)
	services := append([]platform.ServiceStack{poolPairServices()[0]},
		platform.ServiceStack{ID: "svc-webstage", Name: "webstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		platform.ServiceStack{ID: "svc-apistage", Name: "apistage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		poolPartZcpService())
	client := newLaunchMockClient().
		WithProject(&platform.Project{ID: "proj-1", Name: "app", Status: statusActive}).
		WithServices(services).
		WithProjectEnv([]platform.ProjectEnvVar{{Key: "LOG_LEVEL", Content: "info"}})

	result, _, err := handleLaunchProduction(context.Background(), "proj-1", client, nil, nil,
		poolPartReadyInput("apistage"), stateDir, poolPartRuntime(), &hostLogSSH{}, "")

	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if resp := decodeLaunchResp(t, []byte(extractText(result))); resp.Status != topology.LaunchStatusReadyToLaunch {
		t.Fatalf("launch = %s %+v, want ready-to-launch\n%s", resp.Status, resp.Blockers, extractText(result))
	}
	if state := poolPartLaunchState(t, stateDir); state["targetServiceHostname"] != "apistage" {
		t.Errorf("the launch's runtime = %v, want the named stage apistage", state["targetServiceHostname"])
	}
}
