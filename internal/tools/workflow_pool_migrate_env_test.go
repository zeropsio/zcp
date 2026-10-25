package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// Secret values the dev container holds; none may surface anywhere zcp
// answers, logs or stores.
const (
	poolEnvSecret   = "poolMigrateSecretValue0123456789 'q' $HOME\nline2"
	poolEnvGitToken = "ghp_poolMigrateDevToken0123456789"
)

// poolEnvFixture is a pair whose pool exists and is settled, the dev holding a
// plain value and a secret the API shows masked.
func poolEnvFixture() poolMigrateFixture {
	f := newPoolMigrateFixture()
	f.services = append(f.services, poolMigratePool("apppool", "svc-pool"))
	f.env["svc-dev"] = []platform.ServiceEnvVar{
		{ID: "d-url", Key: "APP_URL", Content: "${zeropsSubdomain}", Type: platform.ServiceEnvUser},
		{ID: "d-secret", Key: "STRIPE_SECRET", Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true},
	}
	f.env["svc-stage"] = []platform.ServiceEnvVar{
		{ID: "s-url", Key: "APP_URL", Content: "https://stage", Type: platform.ServiceEnvUser},
		{ID: "s-only", Key: "STAGE_ONLY", Content: "x", Type: platform.ServiceEnvUser},
	}
	f.ssh.env = map[string]string{"STRIPE_SECRET": poolEnvSecret, poolEnvGitTokenKey: poolEnvGitToken}
	return f
}

const poolEnvGitTokenKey = "GIT_TOKEN"

// poolEnvRows is the pool's service env as the mock holds it now, by key.
func poolEnvRows(t *testing.T, mock *platform.Mock) map[string]platform.ServiceEnvVar {
	t.Helper()
	rows, err := mock.GetServiceEnv(context.Background(), "svc-pool")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]platform.ServiceEnvVar{}
	for _, r := range rows {
		out[r.Key] = r
	}
	return out
}

func poolEnvBlock(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	env, ok := out["env"].(map[string]any)
	if !ok {
		t.Fatalf("no env block in %v", out)
	}
	return env
}

func poolEnvKeys(env map[string]any, field string) []string {
	var keys []string
	list, _ := env[field].([]any)
	for _, item := range list {
		switch v := item.(type) {
		case string:
			keys = append(keys, v)
		case map[string]any:
			keys = append(keys, v["key"].(string))
		}
	}
	slices.Sort(keys)
	return keys
}

func TestPoolMigrate_StepFromLiveState_EnvDone_Image(t *testing.T) {
	t.Parallel()
	f := poolEnvFixture()
	f.env["svc-pool"] = append(poolMigratePoolOwnEnv(),
		platform.ServiceEnvVar{ID: "p-url", Key: "APP_URL", Content: "${zeropsSubdomain}", Type: platform.ServiceEnvUser},
		platform.ServiceEnvVar{ID: "p-secret", Key: "STRIPE_SECRET", Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true},
	)
	f.build(t)
	if got := ops.SeedPoolDefaultRepo(f.stateDir, "apppool", f.meta.RemoteURL); got.State != ops.PoolDefaultRepoSeeded {
		t.Fatalf("seed: %+v", got)
	}
	out, mock := f.run(t)

	if out["step"] != "image" {
		t.Fatalf("step = %v, want image once the pool holds the dev's env; env %v", out["step"], out["env"])
	}
	if next, _ := out["next"].(map[string]any); next["tool"] != "zerops_deploy" {
		t.Fatalf("next = %v, want the image step's deploy", out["next"])
	}
	steps, _ := out["steps"].([]any)
	for i, want := range []string{"done", "done", "current"} {
		if got := steps[i].(map[string]any)["state"]; got != want {
			t.Errorf("steps[%d] = %v, want %s", i, got, want)
		}
	}
	env := poolEnvBlock(t, out)
	if env["done"] != true || !reflect.DeepEqual(poolEnvKeys(env, "present"), []string{"STRIPE_SECRET"}) {
		t.Errorf("env = %v", env)
	}
	if n := mock.CallCount("CreateServiceEnvVar") + mock.CallCount("DeleteUserData"); n != 0 {
		t.Fatalf("the read side wrote env (%d calls)", n)
	}
	if cmds := f.ssh.recorded(); len(cmds) != 2 || strings.Contains(strings.Join(cmds, "\n"), poolMigrateEnvReadMarker) {
		t.Fatalf("ssh commands = %v, want the census and the image step's read of the commit: the read side reads no secret", cmds)
	}
}

func TestPoolMigrateEnv_ActionCopiesThenTheFlowNamesImage(t *testing.T) {
	t.Parallel()
	f := poolEnvFixture()
	f.input.Action = "pool-env"
	out, mock := f.runEnv(t)

	if out["action"] != "pool-env" || out["step"] != "image" {
		t.Fatalf("action/step = %v/%v, want pool-env/image; env %v", out["action"], out["step"], out["env"])
	}
	env := poolEnvBlock(t, out)
	if got := poolEnvKeys(env, "written"); !reflect.DeepEqual(got, []string{"APP_URL", "STRIPE_SECRET"}) {
		t.Fatalf("written = %v", got)
	}
	rows := poolEnvRows(t, mock)
	if r := rows["APP_URL"]; r.Content != "${zeropsSubdomain}" || r.Sensitive {
		t.Errorf("APP_URL on the pool = %+v, want the dev's plain value verbatim", r)
	}
	if r := rows["STRIPE_SECRET"]; r.Content != poolEnvSecret || !r.Sensitive {
		t.Errorf("STRIPE_SECRET on the pool: sensitive=%v exact=%v", r.Sensitive, r.Content == poolEnvSecret)
	}
	if repo, _ := env["defaultRepo"].(map[string]any); repo["state"] != ops.PoolDefaultRepoSeeded || repo["repo"] != "https://github.com/example/app.git" {
		t.Errorf("defaultRepo = %v, want the dev's origin seeded", env["defaultRepo"])
	}
	diff, _ := env["stageDiff"].([]any)
	if len(diff) != 1 {
		t.Fatalf("stageDiff = %v", env["stageDiff"])
	}
	if d := diff[0].(map[string]any); d["stage"] != "appstage" || !reflect.DeepEqual(d["stageOnly"], []any{"STAGE_ONLY"}) || !reflect.DeepEqual(d["poolOnly"], []any{"STRIPE_SECRET"}) {
		t.Errorf("stageDiff = %v", d)
	}
	// Nothing restarts: the only platform writes are the userData delete and create.
	for method, n := range mock.CallCounts {
		if n > 0 && !poolMigrateReadOnlyCalls[method] && method != "CreateServiceEnvVar" && method != "DeleteUserData" {
			t.Errorf("the env step called %s %d times", method, n)
		}
	}

	// The read side now lands on the same step, and a second step call writes nothing.
	f.input.Action = ""
	if again, _ := f.run(t); again["step"] != "image" {
		t.Fatalf("read side after the step: step = %v", again["step"])
	}
	creates := mock.CallCount("CreateServiceEnvVar")
	f.input.Action = "pool-env"
	second, _ := f.runEnv(t)
	if got := poolEnvKeys(poolEnvBlock(t, second), "written"); len(got) != 0 || mock.CallCount("CreateServiceEnvVar") != creates {
		t.Fatalf("a repeated step wrote %v", got)
	}
}

func TestPoolMigrateEnv_MaskedBecomesRequest(t *testing.T) {
	t.Parallel()
	f := poolEnvFixture()
	delete(f.ssh.env, "STRIPE_SECRET") // the container does not show it
	out, mock := f.runEnv(t)

	reqs, _ := out["envRequests"].([]any)
	if len(reqs) != 1 {
		t.Fatalf("envRequests = %v, want one zerops_env request", out["envRequests"])
	}
	req := reqs[0].(map[string]any)
	args, _ := req["args"].(map[string]any)
	if req["tool"] != "zerops_env" || args["action"] != "request" || args["serviceHostname"] != "apppool" ||
		args["key"] != "STRIPE_SECRET" || args["label"] != "Stripe secret" || args["sensitive"] != "true" {
		t.Fatalf("request call = %v", req)
	}
	if why, _ := args["why"].(string); why == "" || len(why) > 160 {
		t.Errorf("why = %q", why)
	}
	if _, has := poolEnvRows(t, mock)["STRIPE_SECRET"]; has {
		t.Fatal("a secret zcp could not read was written")
	}
	if out["step"] != "image" {
		t.Errorf("step = %v: a secret asked of the person must not hold the flow", out["step"])
	}
	if _, has := poolEnvBlock(t, out)["requests"]; has {
		t.Error("requests ride envRequests as calls, not the env block")
	}
	// The read side keeps naming the ask until the pool holds the key.
	f.input.Action = ""
	read, _ := f.run(t)
	if reqs, _ := read["envRequests"].([]any); len(reqs) != 1 {
		t.Fatalf("read side envRequests = %v", read["envRequests"])
	}
}

func TestPoolMigrateEnv_GitToken_AskedThenCopiedOnYes(t *testing.T) {
	t.Parallel()
	f := poolEnvFixture()
	f.env["svc-dev"] = append(f.env["svc-dev"], platform.ServiceEnvVar{ID: "d-git", Key: poolEnvGitTokenKey, Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true})
	out, mock := f.runEnv(t)

	git, _ := poolEnvBlock(t, out)["gitToken"].(map[string]any)
	if git["state"] != "ask" || !strings.Contains(git["message"].(string), "copyGitToken=true") {
		t.Fatalf("gitToken = %v, want ask naming copyGitToken=true", git)
	}
	if _, has := poolEnvRows(t, mock)[poolEnvGitTokenKey]; has {
		t.Fatal("GIT_TOKEN reached the pool before the person said yes")
	}
	for _, cmd := range f.ssh.recorded() {
		if strings.Contains(cmd, "'"+poolEnvGitTokenKey+"'") {
			t.Fatalf("GIT_TOKEN was read before the person said yes: %s", cmd)
		}
	}

	f.input.CopyGitToken = true
	yes, _ := f.runEnv(t)
	if git, _ := poolEnvBlock(t, yes)["gitToken"].(map[string]any); git["state"] != "copied" {
		t.Fatalf("gitToken after yes = %v", git)
	}
	if r := poolEnvRows(t, mock)[poolEnvGitTokenKey]; r.Content != poolEnvGitToken || !r.Sensitive {
		t.Errorf("the pool's GIT_TOKEN: sensitive=%v exact=%v", r.Sensitive, r.Content == poolEnvGitToken)
	}
}

func TestPoolMigrateEnv_InAMate_GitTokenNeverCopied_DefaultRepoIsHQ(t *testing.T) {
	t.Parallel()
	f := poolEnvFixture()
	f.rt.MateEnabled = true
	f.meta.GitPushState, f.meta.RemoteURL = "", ""
	f.meta.HQ = &workflow.HQRepoRef{AppID: "app-1", Repo: "appdev"}
	f.hqAddress = func() string { return "https://hq.example" }
	f.input.CopyGitToken = true
	f.env["svc-dev"] = append(f.env["svc-dev"], platform.ServiceEnvVar{ID: "d-git", Key: poolEnvGitTokenKey, Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true})
	out, mock := f.runEnv(t)

	env := poolEnvBlock(t, out)
	if git, _ := env["gitToken"].(map[string]any); git["state"] != "mate-credential" {
		t.Fatalf("gitToken = %v, want mate-credential", git)
	}
	if _, has := poolEnvRows(t, mock)[poolEnvGitTokenKey]; has {
		t.Fatal("in a Mate the dev's GIT_TOKEN reached the pool")
	}
	for _, cmd := range f.ssh.recorded() {
		if strings.Contains(cmd, "'"+poolEnvGitTokenKey+"'") {
			t.Fatalf("in a Mate the dev's GIT_TOKEN was read: %s", cmd)
		}
	}
	if repo, _ := env["defaultRepo"].(map[string]any); repo["repo"] != "https://hq.example/git/app-1/appdev.git" || repo["state"] != ops.PoolDefaultRepoSeeded {
		t.Errorf("defaultRepo = %v, want the pair's HQ repository", repo)
	}
}

func TestPoolMigrateEnv_BeforeThePoolIsReady_WritesNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*poolMigrateFixture)
	}{
		{name: "no pool yet", mutate: func(f *poolMigrateFixture) {
			f.services = f.services[:2]
		}},
		{name: "the pool's import still runs", mutate: func(f *poolMigrateFixture) {
			f.processes = []platform.Process{{ID: "proc-import", Status: "RUNNING", ServiceStacks: []platform.ServiceStackRef{{ID: "svc-pool", Name: "apppool"}}, Created: "2026-10-23T10:00:00Z"}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := poolEnvFixture()
			tt.mutate(&f)
			out, mock := f.runEnv(t)
			if out["step"] != "provision" || out["action"] != "pool-env" {
				t.Fatalf("step/action = %v/%v, want provision/pool-env", out["step"], out["action"])
			}
			if w, _ := out["warnings"].([]any); len(w) == 0 || !strings.Contains(w[len(w)-1].(string), "Nothing was written") {
				t.Errorf("warnings = %v, want the step to say it wrote nothing", out["warnings"])
			}
			if n := mock.CallCount("CreateServiceEnvVar") + mock.CallCount("DeleteUserData"); n != 0 {
				t.Fatalf("env written before the pool was ready (%d calls)", n)
			}
			if cmds := f.ssh.recorded(); len(cmds) != 1 {
				t.Fatalf("ssh commands = %v, want only the census", cmds)
			}
		})
	}
}

func TestPoolMigrateEnv_NotACandidate_WritesNothing(t *testing.T) {
	t.Parallel()
	f := poolEnvFixture()
	f.meta.GitPushState, f.meta.RemoteURL = "", ""
	out, mock := f.runEnv(t)
	if out["status"] != "not-a-candidate" || !slices.Contains(poolMigrateReasons(out), "no-origin") {
		t.Fatalf("status = %v reasons %v", out["status"], poolMigrateReasons(out))
	}
	if n := mock.CallCount("CreateServiceEnvVar") + mock.CallCount("DeleteUserData"); n != 0 {
		t.Fatalf("a non-candidate's pool got env (%d calls)", n)
	}
	if cmds := f.ssh.recorded(); len(cmds) != 0 {
		t.Fatalf("a refusal the meta decides reached the dev: %v", cmds)
	}
}

func TestPoolMigrateEnv_OtherWorkflow_Refused(t *testing.T) {
	t.Parallel()
	f := poolEnvFixture()
	f.input.Workflow = "develop"
	f.build(t)
	result, _, err := poolMigrateEnvRun(context.Background(), f.deps(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(getTextContent(t, result), "pool-migrate") {
		t.Fatalf("result = %s, want an error naming pool-migrate", getTextContent(t, result))
	}
	if n := f.mock.CallCount("CreateServiceEnvVar"); n != 0 {
		t.Fatal("env written under another workflow")
	}
}

// poolEnvServer registers zerops_workflow over the fixture, as the MCP server
// runs it.
func poolEnvServer(t *testing.T, f *poolMigrateFixture) *mcp.Server {
	t.Helper()
	f.build(t)
	engine := workflow.NewEngine(f.stateDir, workflow.EnvContainer, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, f.mock, nil, "proj-1", nil, engine, nil, f.stateDir, "zcp", nil, f.ssh, f.rt, "")
	return srv
}

func TestPoolMigrateEnv_Dispatch_ReachesTheStep(t *testing.T) {
	t.Parallel()
	for _, args := range []map[string]any{
		{"workflow": "pool-migrate", "action": "pool-env", "service": "appdev"},
		{"action": "pool-env", "service": "appdev"},
	} {
		f := poolEnvFixture()
		srv := poolEnvServer(t, &f)
		result := callTool(t, srv, "zerops_workflow", args)
		text := getTextContent(t, result)
		if result.IsError {
			t.Fatalf("args %v: %s", args, text)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("args %v: %v", args, err)
		}
		if out["action"] != "pool-env" || out["step"] != "image" {
			t.Fatalf("args %v: action/step = %v/%v", args, out["action"], out["step"])
		}
	}
}

// TestPoolMigrateEnv_SecretNeverInAnyToolOutput drives the env step, the read
// side, a failed secret read and the status call through the MCP server, and
// finds no secret value in any answer, error, ssh command or file zcp keeps.
func TestPoolMigrateEnv_SecretNeverInAnyToolOutput(t *testing.T) {
	t.Parallel()
	f := poolEnvFixture()
	f.env["svc-dev"] = append(f.env["svc-dev"], platform.ServiceEnvVar{ID: "d-git", Key: poolEnvGitTokenKey, Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true})
	srv := poolEnvServer(t, &f)

	var texts []string
	call := func(args map[string]any) {
		result := callTool(t, srv, "zerops_workflow", args)
		for _, c := range result.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				texts = append(texts, tc.Text)
			}
		}
		if result.StructuredContent != nil {
			b, err := json.Marshal(result.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			texts = append(texts, string(b))
		}
	}
	call(map[string]any{"workflow": "pool-migrate", "action": "pool-env", "service": "appdev", "copyGitToken": true})
	if rows := poolEnvRows(t, f.mock); rows["STRIPE_SECRET"].Content != poolEnvSecret || rows[poolEnvGitTokenKey].Content != poolEnvGitToken {
		t.Fatal("the fixture must copy both secrets for the scan to mean anything")
	}
	call(map[string]any{"workflow": "pool-migrate", "service": "appdev"})
	call(map[string]any{"action": "status"})

	// A second pool whose secret read fails after the session printed a value.
	f.ssh.mu.Lock()
	f.ssh.envErr = errors.New("exit status 255")
	f.ssh.envOutput = "zcp-env 1\x00STRIPE_SECRET\x00" + poolEnvSecret
	f.ssh.mu.Unlock()
	f.mock.WithServiceEnv("svc-pool", poolMigratePoolOwnEnv())
	call(map[string]any{"workflow": "pool-migrate", "action": "pool-env", "service": "appdev", "copyGitToken": true})
	if last := texts[len(texts)-1]; !strings.Contains(last, "exit status 255") {
		t.Errorf("the failed read is not named: %s", last)
	}

	haystacks := map[string]string{}
	for i, text := range texts {
		haystacks["answer "+string(rune('0'+i))] = text
	}
	for i, cmd := range f.ssh.recorded() {
		haystacks["ssh command "+string(rune('0'+i))] = cmd
	}
	_ = filepath.WalkDir(f.stateDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := os.ReadFile(path)
			haystacks[path] = string(data)
		}
		return nil
	})
	for where, text := range haystacks {
		for _, secret := range []string{poolEnvSecret, "poolMigrateSecretValue0123456789", "line2", poolEnvGitToken} {
			if strings.Contains(text, secret) {
				t.Errorf("a secret value appears in %s", where)
			}
		}
	}
}

// TestPoolMigrateEnv_PoolAnotherPairHolds_RefusedBeforeAnyWrite: a pool the
// part's name lands on that is another pair's — a pair keyed by it (another dev
// moved onto it, or a part provisioned as a pool), or the pool another dev's
// migration already gave its env (two dev services, `app` and `appdev`, both
// name `apppool`) — is refused as pool-hostname-taken before anything is
// written: no env key, no secret read on the dev, no default repository, on
// the read side and on the env step alike (PM-4, PM-9).
func TestPoolMigrateEnv_PoolAnotherPairHolds_RefusedBeforeAnyWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		owner string // the pair the refusal names
		seed  func(t *testing.T, stateDir string)
	}{
		{
			name:  "a pair is keyed by the pool",
			owner: "apppool",
			seed: func(t *testing.T, stateDir string) {
				t.Helper()
				if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
					Hostname: "apppool", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "otherstage", SetupName: "prod"}},
					DevPool: &workflow.DevPoolRef{ServiceID: "svc-pool", MigratedFrom: &workflow.MigratedDev{Hostname: "app"}}, BootstrappedAt: "2026-10-01T00:00:00Z",
				}); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:  "another dev's migration gave the pool its env",
			owner: "app",
			seed: func(t *testing.T, stateDir string) {
				t.Helper()
				path := filepath.Join(stateDir, "migrate", "pool-env", "apppool.json")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"poolServiceId":"svc-pool","dev":"app","fingerprints":{"STRIPE_SECRET":"0123"}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, action := range []string{"", "pool-env"} {
				f := poolEnvFixture()
				f.input.Action = action
				f.build(t)
				tt.seed(t, f.stateDir)
				var out map[string]any
				var mock *platform.Mock
				if action == "" {
					out, mock = f.run(t)
				} else {
					out, mock = f.runEnv(t)
				}
				if out["status"] != "not-a-candidate" || !slices.Contains(poolMigrateReasons(out), "pool-hostname-taken") {
					t.Fatalf("action %q = %v, want pool-hostname-taken", action, out)
				}
				if msg := poolMigrateRefusalMessage(out, "pool-hostname-taken"); !strings.Contains(msg, "apppool") || !strings.Contains(msg, tt.owner) || !strings.Contains(msg, `pool="`) {
					t.Errorf("message %q names neither the pool, the pair holding it (%s) nor how to name another pool", msg, tt.owner)
				}
				if n := mock.CallCount("CreateServiceEnvVar") + mock.CallCount("DeleteUserData"); n != 0 {
					t.Fatalf("action %q wrote the other pair's pool's env (%d calls)", action, n)
				}
				if strings.Contains(strings.Join(f.ssh.recorded(), "\n"), poolMigrateEnvReadMarker) {
					t.Fatalf("action %q read the dev's secrets for another pair's pool", action)
				}
				if _, err := os.Stat(filepath.Join(f.stateDir, "leases", "pools", "apppool.json")); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("action %q seeded the pool's default repository (%v)", action, err)
				}
			}
		})
	}
}
