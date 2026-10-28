// Tests for: tools/workflow_release.go, tools/workflow_launch_production.go,
// tools/pool_part_origin.go — a launched pool-keyed part is delivered as
// every other part is (docs/spec-workflows.md §10 P-LP-20): its release tags
// the origin's tracked tip from a scratch repository of zcp's own, and the
// launched response carries the production Actions block, its launch token
// conveyed from the stage. The origin is fetched once per tool call, and no
// failing fetch surfaces the pool's GIT_TOKEN (P-LP-19).
package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cgi" //nolint:gosec // G504: a test serves its own fixture through git-http-backend; Go is far past 1.6.3
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolPartHTTPOrigin serves the bare origin behind fileURL over smart HTTP
// (git-http-backend) and answers its URL and how many fetches it was asked
// for — one GET of info/refs for git-upload-pack per fetch.
func poolPartHTTPOrigin(t *testing.T, fileURL string) (string, *atomic.Int32) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "--exec-path").Output()
	if err != nil {
		t.Skip("git has no exec path")
	}
	backend := filepath.Join(strings.TrimSpace(string(out)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend is not installed")
	}
	dir := strings.TrimPrefix(fileURL, "file://")
	handler := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + filepath.Dir(dir), "GIT_HTTP_EXPORT_ALL=1"}}
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/info/refs") && r.URL.Query().Get("service") == "git-upload-pack" {
			fetches.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/" + filepath.Base(dir), &fetches
}

// poolPartReadyInput is the launch input that reaches ready-to-launch.
func poolPartReadyInput(target string) WorkflowInput {
	return WorkflowInput{
		Workflow: workflowLaunchProduction, ProductionProjectName: "app-prod", TargetService: target,
		EnvClassifications: map[string]string{"LOG_LEVEL": "plain-config"}, SkipBuildIntegration: []string{testPool},
	}
}

// TestLaunchProduction_PoolKeyedPart_FetchesTheOriginOncePerCall: one call
// reads the origin once — its read-side gate, source read and baseline share
// the fetch — and the next call fetches again.
func TestLaunchProduction_PoolKeyedPart_FetchesTheOriginOncePerCall(t *testing.T) {
	t.Parallel()
	fileURL, _ := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	httpURL, fetches := poolPartHTTPOrigin(t, fileURL)
	stateDir := poolPartPair(t, httpURL, "main")

	for call := int32(1); call <= 2; call++ {
		result, _, err := handleLaunchProduction(context.Background(), "proj-1", poolPartClient(), nil, nil,
			poolPartReadyInput("appstage"), stateDir, poolPartRuntime(), &hostLogSSH{}, "")
		if err != nil {
			t.Fatalf("launch: %v", err)
		}
		if resp := decodeLaunchResp(t, []byte(extractText(result))); resp.Status != topology.LaunchStatusReadyToLaunch {
			t.Fatalf("launch = %s, want ready-to-launch\n%s", resp.Status, extractText(result))
		}
		if got := fetches.Load(); got != call {
			t.Errorf("after call %d the origin was fetched %d times, want %d", call, got, call)
		}
	}
}

// TestLaunchProduction_PoolKeyedPart_RefusingOrigin_NeverSurfacesTheToken: an
// origin that refuses the pool's GIT_TOKEN and echoes it back in its error
// makes launch and export refuse naming the origin — and the token reaches
// no answer, state file or log line in any form.
func TestLaunchProduction_PoolKeyedPart_RefusingOrigin_NeverSurfacesTheToken(t *testing.T) {
	// non-parallel: t.Setenv and the stderr capture touch the process.
	requirePoolPartGit(t)
	t.Setenv("GIT_SSL_NO_VERIFY", "1")
	var sent atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
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
		base64.StdEncoding.EncodeToString([]byte("oauth2:" + poolPartGitToken)),
		url.QueryEscape(poolPartGitToken),
	}

	var launchText, exportText string
	stderr := captureStderr(t, func() {
		result, _, err := handleLaunchProduction(context.Background(), "proj-1", poolPartClient(), nil, nil,
			poolPartReadyInput("appstage"), stateDir, poolPartRuntime(), &hostLogSSH{}, "")
		if err != nil {
			t.Errorf("launch: %v", err)
			return
		}
		launchText = extractText(result)
		exported, _, err := handleExport(context.Background(), "proj1", nil, poolPartExportClient(), WorkflowInput{
			Workflow: workflowExport, TargetService: "appstage",
			EnvClassifications: map[string]string{"LOG_LEVEL": "plain-config"},
		}, &hostLogSSH{}, stateDir, poolPartRuntime())
		if err != nil {
			t.Errorf("export: %v", err)
			return
		}
		exportText = extractText(exported)
	})

	if !sent.Load() {
		t.Fatal("the origin was never sent the pool's GIT_TOKEN; the test proves nothing")
	}
	if !strings.Contains(launchText, "origin-unreadable-"+testPool) || !strings.Contains(launchText, srv.URL) {
		t.Errorf("launch did not refuse naming the origin: %s", launchText)
	}
	if !strings.Contains(exportText, srv.URL) {
		t.Errorf("export did not refuse naming the origin: %s", exportText)
	}
	for _, form := range forms {
		for where, text := range map[string]string{"launch": launchText, "export": exportText, "stderr": stderr} {
			if strings.Contains(text, form) {
				t.Errorf("the %s output carries the token as %q: %s", where, form, text)
			}
		}
	}
	scanLaunchStateDirForSentinels(t, stateDir, forms...)
}

// poolPartPktLine frames s as one git pkt-line.
func poolPartPktLine(s string) string {
	const hex = "0123456789abcdef"
	n := len(s) + 4
	return string([]byte{hex[n>>12&0xf], hex[n>>8&0xf], hex[n>>4&0xf], hex[n&0xf]}) + s
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

// poolPartRelease calls action=release on service, with version when set.
func poolPartRelease(t *testing.T, srv *mcp.Server, service, version string) (map[string]any, string, bool) {
	t.Helper()
	args := map[string]any{"action": "release", "service": service}
	if version != "" {
		args["releaseVersion"] = version
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

// originTagAt answers the commit the origin's tag points at, "" for none.
func originTagAt(t *testing.T, fileURL, tag string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "--verify", "-q", tag+"^{commit}") //nolint:gosec // G204: a test's own git
	cmd.Dir = strings.TrimPrefix(fileURL, "file://")
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

// TestRelease_PoolKeyedPart_TagsTheOriginTip: a release of a pool-keyed
// pair, from either hostname, names the origin's tracked tip and its next
// version, then tags that tip on the origin; nothing reaches the pool, a
// lease or the stage over ssh.
func TestRelease_PoolKeyedPart_TagsTheOriginTip(t *testing.T) {
	t.Parallel()
	fileURL, heads := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	stateDir := poolPartPair(t, fileURL, "main")
	ssh := &hostLogSSH{}
	srv := poolPartWorkflowServer(t, poolPartClient(), stateDir, ssh)

	prompt, text, isErr := poolPartRelease(t, srv, "appstage", "")
	if isErr || prompt["status"] != "release-prompt" {
		t.Fatalf("release prompt = %s, want release-prompt", text)
	}
	if prompt["suggestedVersion"] != "v1.0.0" || prompt["head"] != shortSHA(heads["main"]) {
		t.Errorf("prompt = %v, want v1.0.0 at the origin's main %s", prompt, shortSHA(heads["main"]))
	}

	released, text, isErr := poolPartRelease(t, srv, testPool, "v1.0.0")
	if isErr || released["status"] != "released" || released["version"] != "v1.0.0" {
		t.Fatalf("release = %s, want released v1.0.0", text)
	}
	if at := originTagAt(t, fileURL, "v1.0.0"); at != heads["main"] {
		t.Errorf("v1.0.0 on the origin points at %q, want main's tip %q", at, heads["main"])
	}

	next, text, _ := poolPartRelease(t, srv, "appstage", "")
	if next["suggestedVersion"] != "v1.0.1" {
		t.Errorf("the next prompt = %s, want v1.0.1 suggested after v1.0.0", text)
	}
	if dup, text, isErr := poolPartRelease(t, srv, "appstage", "v1.0.0"); !isErr {
		t.Errorf("a second v1.0.0 was not refused: %v %s", dup, text)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("release reached %v over ssh", ssh.hosts)
	}
}

// TestRelease_PoolKeyedPart_RefusedOrigin_NamesWhatToFix: a release whose
// origin cannot be read is refused naming the origin and what to fix, with
// no ssh and no token in the answer.
func TestRelease_PoolKeyedPart_RefusedOrigin_NamesWhatToFix(t *testing.T) {
	t.Parallel()
	requirePoolPartGit(t)
	missing := "file://" + filepath.Join(t.TempDir(), "no-such-origin")
	stateDir := poolPartPair(t, missing, "main")
	ssh := &hostLogSSH{}
	srv := poolPartWorkflowServer(t, poolPartClient(), stateDir, ssh)

	_, text, isErr := poolPartRelease(t, srv, "appstage", "v1.0.0")

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

// TestProdCDActionsBlock_PoolKeyedPart_ConveysFromTheStage: the production
// Actions block of a launched pool-keyed part carries the same workflow
// file; its secret command conveys the launch token staged on the stage and
// takes GH_TOKEN as the PAT the person gives — never reaching the pool —
// and it names the lease the workflow file is committed from.
func TestProdCDActionsBlock_PoolKeyedPart_ConveysFromTheStage(t *testing.T) {
	t.Parallel()
	stateDir := poolPartPair(t, "https://github.com/acme/app.git", "main")
	state := &launchState{
		TargetServiceHostname: "appstage",
		ImportedServices:      []importedServiceEntry{{ID: "svc-prod-app", Name: "app"}},
		RuntimeProds:          []launchRuntimeProd{{ProdHostname: "app", SetupName: "prod"}},
	}

	block := prodCDActionsBlock(stateDir, state)

	if block == nil {
		t.Fatal("no production Actions block for the launched pool-keyed part")
	}
	file, _ := block["workflowFile"].(map[string]any)
	if content, _ := file["content"].(string); !strings.Contains(content, "svc-prod-app") || !strings.Contains(content, "--setup \"prod\"") {
		t.Errorf("the workflow file does not deploy the production runtime: %v", file)
	}
	secret, _ := block["secret"].(map[string]any)
	cmd, _ := secret["command"].(string)
	for _, want := range []string{"appstage", ops.LaunchTokenEnvKey, "acme/app", "PAT"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("the secret command does not carry %q: %s", want, cmd)
		}
	}
	if strings.Contains(cmd, testPool) {
		t.Errorf("the secret command reaches the pool: %s", cmd)
	}
	if via, _ := block["commitVia"].(string); !strings.Contains(via, "zerops_lease") || !strings.Contains(via, testPool) {
		t.Errorf("commitVia = %q, want the lease of the pool the workflow file is committed from", via)
	}
}

// TestLaunchProduction_PoolKeyedPart_ThenReleaseShipsTheFirstBuild: a
// pool-keyed part launches to launched — its token staged on the stage, the
// production Actions block on the answer, the release step naming the stage
// — and that release step then tags the origin's tip, the first build.
func TestLaunchProduction_PoolKeyedPart_ThenReleaseShipsTheFirstBuild(t *testing.T) {
	// non-parallel: swaps the package's project-admin client factory.
	fileURL, heads := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	stateDir := poolPartPair(t, fileURL, "main")
	client := poolPartClient()
	defer installMockAdminFactory(t, happyMockAdmin())()
	ssh := &hostLogSSH{}
	input := poolPartReadyInput("appstage")
	input.Region = "eu-central"
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
	state, err := readLaunchState(stateDir, generateLaunchID("proj-1", "app-prod"))
	if err != nil {
		t.Fatalf("read the launch's state: %v", err)
	}
	if state.TargetServiceHostname != "appstage" {
		t.Errorf("the launch token is staged on %q, want the stage", state.TargetServiceHostname)
	}

	srv := poolPartWorkflowServer(t, client, stateDir, ssh)
	released, releaseText, isErr := poolPartRelease(t, srv, "appstage", "v1.0.0")
	if isErr || released["status"] != "released" {
		t.Fatalf("the first release = %s, want released", releaseText)
	}
	if at := originTagAt(t, fileURL, "v1.0.0"); at != heads["main"] {
		t.Errorf("the first release tagged %q, want the origin's main %q", at, heads["main"])
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("launch or release reached %v over ssh", ssh.hosts)
	}
	scanLaunchStateDirForSentinels(t, stateDir, poolPartGitToken, sentinelLaunchKey)
}
