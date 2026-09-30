// Tests for: tools/standup.go — zerops_standup, a new Mate's stand-up from
// its group's recipe. Fakes only: a broker and a Gitea on httptest, an SSH
// stub that answers the way a healthy container would, the platform mock.
package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// standupTierTemplate is a real group's AI Agent tier (the Beviro trial,
// 2026-09-29) with GITEA standing for the fake Gitea's origin: two pairs
// whose setups are named after them, the API pair first by priority, a
// public-build mailpit and a managed database.
const standupTierTemplate = `#yamlPreprocessor=on
project:
  name: beviro-wren
services:
  - hostname: nextstoredev
    type: nodejs@22
    buildFromGit: GITEA/beviro/nextstoredev
    zeropsSetup: nextstoredev
  - hostname: nextstorestage
    type: nodejs@22
    buildFromGit: GITEA/beviro/nextstoredev
    zeropsSetup: nextstoreprod
    enableSubdomainAccess: true
  - hostname: medusadev
    type: nodejs@22
    buildFromGit: GITEA/beviro/medusadev
    zeropsSetup: medusadev
    priority: 5
  - hostname: medusastage
    type: nodejs@22
    buildFromGit: GITEA/beviro/medusadev
    zeropsSetup: medusaprod
    enableSubdomainAccess: true
    priority: 5
  - hostname: mailpit
    type: alpine@3.20
    buildFromGit: https://github.com/zeropsio/recipe-mailpit
  - hostname: db
    type: postgresql@17
    mode: NON_HA
    priority: 10
`

// standupZeropsYAML is each dev half's zerops.yaml as main carries it: the
// dev setup deploys the whole repository and idles on a no-op keepalive.
func standupZeropsYAML(pair string) string {
	return fmt.Sprintf(`zerops:
  - setup: %[1]sdev
    build:
      base: nodejs@22
      buildCommands: [npm ci]
      deployFiles: [.]
    run:
      base: nodejs@22
      ports:
        - port: 9000
          httpSupport: true
      start: zsc noop --silent
  - setup: %[1]sprod
    build:
      base: nodejs@22
      buildCommands: [npm ci, npm run build]
      deployFiles: [.]
    run:
      base: nodejs@22
      ports:
        - port: 9000
          httpSupport: true
      start: npm run start
`, pair)
}

// standupGitea is the broker and the group's Gitea in one TLS server — two
// origins in production; the code never assumes they are one host.
type standupGitea struct {
	mu sync.Mutex
	// tier is the group repo's AI Agent tier on main, "" when main lacks it.
	tier string
	// orgs is what the bot's org list answers.
	orgs []string
	// repos are the service repositories that exist, as org/name.
	repos []string
	// asked is every repository the broker was asked for, in order.
	asked []string
	// pullCreates counts pull requests opened — a stand-up opens none.
	pullCreates int
	// read is every group-repo file read, by path.
	read []string
	url  string
}

func (g *standupGitea) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token "+giteaBotToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		write := func(status int, body any) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(body)
		}
		path := r.URL.Path
		switch {
		case path == "/api/v1/user":
			write(http.StatusOK, map[string]any{"login": "mate-p1", "id": 7})
		case path == "/api/v1/user/orgs":
			orgs := []map[string]any{}
			for _, name := range g.orgs {
				orgs = append(orgs, map[string]any{"name": name, "username": name})
			}
			write(http.StatusOK, orgs)
		case strings.HasPrefix(path, "/api/v1/repos/beviro/group/contents/"):
			file := strings.TrimPrefix(path, "/api/v1/repos/beviro/group/contents/")
			g.read = append(g.read, file)
			if file != workflow.MateTierImportPath || g.tier == "" || r.URL.Query().Get("ref") != "main" {
				write(http.StatusNotFound, map[string]string{"message": "The target couldn't be found."})
				return
			}
			write(http.StatusOK, map[string]any{"type": "file", "encoding": "base64",
				"content": base64.StdEncoding.EncodeToString([]byte(g.tier))})
		case strings.HasPrefix(path, "/api/v1/repos/") && strings.Count(strings.TrimPrefix(path, "/api/v1/repos/"), "/") == 1:
			if slices.Contains(g.repos, strings.TrimPrefix(path, "/api/v1/repos/")) {
				write(http.StatusOK, map[string]any{"full_name": strings.TrimPrefix(path, "/api/v1/repos/")})
				return
			}
			write(http.StatusNotFound, map[string]string{"message": "not found"})
		case path == "/mate/repository":
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.asked = append(g.asked, body.Name)
			write(http.StatusOK, map[string]any{"fullName": "beviro/" + body.Name,
				"cloneUrl": g.url + "/beviro/" + body.Name, "defaultBranch": "main", "created": false})
		case strings.Contains(path, "/branches/"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(path, "/pulls"):
			if r.Method == http.MethodPost {
				// No branch is ever pushed here, and Gitea opens no pull
				// request from a branch it does not have.
				write(http.StatusNotFound, map[string]string{"message": "head branch does not exist"})
				return
			}
			write(http.StatusOK, []any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	g.url = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// standupSSH answers the stand-up's SSH round trips the way healthy
// containers would, and records each command by host.
type standupSSH struct {
	mu       sync.Mutex
	commands []standupSSHCall
	// collide names dev halves whose checkout already holds files main carries.
	collide map[string]bool
}

type standupSSHCall struct{ host, cmd string }

func (s *standupSSH) ExecSSH(_ context.Context, host, cmd string) ([]byte, error) {
	s.mu.Lock()
	s.commands = append(s.commands, standupSSHCall{host, cmd})
	s.mu.Unlock()
	switch {
	case strings.Contains(cmd, "cur_email=$(git config user.email)"):
		return []byte("ZCP_EMAIL_SEEDED\nZCP_NAME_SEEDED\n"), nil
	case strings.Contains(cmd, "rev-parse --verify HEAD") && strings.Contains(cmd, "git status --porcelain"):
		return nil, errStubSSHNoRepo
	case strings.Contains(cmd, "ZCP_BRANCH_ELSEWHERE") && s.collide[host]:
		out := []byte("ZCP_BRANCH_REFUSED:ZCP_UNTRACKED_COLLISION package.json \n")
		return out, &platform.SSHExecError{Hostname: host, Output: string(out), Err: fmt.Errorf("exit status 5")}
	}
	return []byte("ok"), nil
}

func (s *standupSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return []byte("ok"), nil
}

// pushes is every `zcli push` in order: "<host> → <service id> (<setup>)".
func (s *standupSSH) pushes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.commands {
		if !strings.Contains(c.cmd, "zcli push") {
			continue
		}
		id := flagValue(c.cmd, "--service-id")
		setup := strings.Trim(flagValue(c.cmd, "--setup"), "'")
		out = append(out, fmt.Sprintf("%s → %s (%s)", c.host, id, setup))
	}
	return out
}

func (s *standupSSH) ran(substr string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.commands {
		if strings.Contains(c.cmd, substr) {
			return true
		}
	}
	return false
}

func flagValue(cmd, flag string) string {
	_, rest, ok := strings.Cut(cmd, flag+" ")
	if !ok {
		return ""
	}
	value, _, _ := strings.Cut(rest, " ")
	return value
}

// standupMounter mounts nothing and says so.
type standupMounter struct {
	mu      sync.Mutex
	mounted []string
}

func (m *standupMounter) CheckMount(_ context.Context, _ string) (platform.MountState, error) {
	return platform.MountStateNotMounted, nil
}
func (m *standupMounter) Mount(_ context.Context, hostname, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mounted = append(m.mounted, hostname)
	return nil
}
func (m *standupMounter) Unmount(_ context.Context, _, _ string) error         { return nil }
func (m *standupMounter) ForceUnmount(_ context.Context, _, _ string) error    { return nil }
func (m *standupMounter) IsWritable(_ context.Context, _ string) (bool, error) { return true, nil }
func (m *standupMounter) ListMountDirs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (m *standupMounter) HasUnit(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *standupMounter) CleanupUnit(_ context.Context, _ string) error     { return nil }

// standupHTTP sends Gitea's and the broker's calls to the fake and answers
// every other request — an L7 readiness probe — with 200.
type standupHTTP struct {
	gitea *http.Client
	host  string
}

func (h standupHTTP) Do(req *http.Request) (*http.Response, error) {
	if req.URL.Host == h.host {
		return h.gitea.Do(req)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
}

// standupFixture is a Mate's project the moment its person has signed the
// agent in: the tier on the group repo's main, the services the browser
// imported (dev halves running and empty, stage halves waiting for a first
// deploy), the Gitea variables on the container.
type standupFixture struct {
	root, stateDir string
	gitea          *standupGitea
	srv            *httptest.Server
	mock           *platform.Mock
	ssh            *standupSSH
	mounter        *standupMounter
	env            map[string]string
	events         []platform.AppVersionEvent
	services       []platform.ServiceStack
}

func newStandupFixture(t *testing.T) *standupFixture {
	t.Helper()
	f := &standupFixture{
		gitea: &standupGitea{orgs: []string{"beviro"}, repos: []string{"beviro/medusadev", "beviro/nextstoredev"}},
		ssh:   &standupSSH{collide: map[string]bool{}},
	}
	f.srv = f.gitea.start(t)
	f.gitea.tier = strings.ReplaceAll(standupTierTemplate, "GITEA", f.srv.URL)
	f.root = t.TempDir()
	f.stateDir = filepath.Join(f.root, ".zcp", "state")
	f.mounter = &standupMounter{}
	f.env = map[string]string{"GITEA_URL": f.srv.URL, "MATE_BROKER_URL": f.srv.URL, "GITEA_TOKEN": giteaBotToken}
	// The branch cut puts main's files in each dev half; the mount shows them.
	for _, pair := range []string{"medusa", "nextstore"} {
		dir := filepath.Join(f.root, pair+"dev")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(standupZeropsYAML(pair)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runtimeType := func(v string) platform.ServiceTypeInfo {
		return platform.ServiceTypeInfo{ServiceStackTypeVersionName: v, ServiceStackTypeCategoryName: "USER"}
	}
	empty := &platform.ActiveAppVersionDigest{ID: "av-placeholder", Source: platform.AppVersionSourceNone}
	f.services = []platform.ServiceStack{
		{ID: "svc-zcp", Name: "zcp", Status: statusActive, ServiceStackTypeInfo: runtimeType("zcp@1")},
		{ID: "svc-medusadev", Name: "medusadev", Status: statusActive, ServiceStackTypeInfo: runtimeType("nodejs@22"), ActiveAppVersion: empty},
		{ID: "svc-medusastage", Name: "medusastage", Status: "READY_TO_DEPLOY", ServiceStackTypeInfo: runtimeType("nodejs@22")},
		{ID: "svc-nextstoredev", Name: "nextstoredev", Status: statusActive, ServiceStackTypeInfo: runtimeType("nodejs@22"), ActiveAppVersion: empty},
		{ID: "svc-nextstorestage", Name: "nextstorestage", Status: "READY_TO_DEPLOY", ServiceStackTypeInfo: runtimeType("nodejs@22")},
		{ID: "svc-mailpit", Name: "mailpit", Status: statusActive, ServiceStackTypeInfo: runtimeType("alpine@3.20"),
			ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-mailpit", Source: "GIT", Built: true}},
		{ID: "svc-db", Name: "db", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17", ServiceStackTypeCategoryName: "STANDARD"}},
	}
	for _, id := range []string{"svc-medusadev", "svc-medusastage", "svc-nextstoredev", "svc-nextstorestage"} {
		f.events = append(f.events, platform.AppVersionEvent{ID: "av-" + strings.TrimPrefix(id, "svc-"), ServiceStackID: id, Status: statusActive, Sequence: 2})
	}
	f.mock = platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "beviro-wren"}).
		WithServices(f.services).
		WithAppVersionEvents(f.events)
	return f
}

// run calls zerops_standup once and returns its result and parsed body.
func (f *standupFixture) run(t *testing.T) (*mcp.CallToolResult, standupResponse) {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	registerStandup(srv, standupDeps{
		batch: batchDeployer{
			client:      f.mock,
			httpClient:  standupHTTP{gitea: f.srv.Client(), host: strings.TrimPrefix(f.srv.URL, "https://")},
			projectID:   "p1",
			sshDeployer: f.ssh,
			authInfo:    &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
			rtInfo:      runtime.Info{InContainer: true, MateEnabled: true, ProjectID: "p1", ServiceName: "zcp"},
			stateDir:    f.stateDir,
		},
		mounter:     f.mounter,
		liveEnvPath: writeLiveEnvFile(t, f.env),
		gitWait:     50 * time.Millisecond,
		gitPoll:     10 * time.Millisecond,
	})
	result := callTool(t, srv, "zerops_standup", map[string]any{})
	var body standupResponse
	if !result.IsError {
		if err := json.Unmarshal([]byte(getTextContent(t, result)), &body); err != nil {
			t.Fatalf("parse stand-up result: %v\n%s", err, getTextContent(t, result))
		}
	}
	return result, body
}

func (r standupResponse) service(t *testing.T, hostname string) standupService {
	t.Helper()
	for _, s := range r.Services {
		if s.Hostname == hostname {
			return s
		}
	}
	t.Fatalf("no %s in the stand-up's services: %+v", hostname, r.Services)
	return standupService{}
}

// TestStandup_StandsUpEveryPairFromTheRecipe is the whole stand-up on a real
// group's tier: every pair adopted exactly as adopt records it, wired to the
// repository its recipe names, its dev half deployed and then its stage
// cross-deployed from it, in priority waves — and nothing committed, pushed
// or proposed.
func TestStandup_StandsUpEveryPairFromTheRecipe(t *testing.T) {
	t.Parallel()
	f := newStandupFixture(t)
	result, body := f.run(t)
	if result.IsError {
		t.Fatalf("stand-up failed: %s", getTextContent(t, result))
	}
	if body.StandUp != standupReady {
		t.Errorf("standUp = %q, want %q: %s", body.StandUp, standupReady, getTextContent(t, result))
	}
	if body.GroupRepo != "beviro/group" {
		t.Errorf("groupRepo = %q", body.GroupRepo)
	}

	// Waves by priority: the medusa pair (5) before the nextstore pair (0),
	// each pair's dev push before its stage cross-deploy from the dev half.
	wantPushes := []string{
		"medusadev → svc-medusadev (medusadev)",
		"medusadev → svc-medusastage (medusaprod)",
		"nextstoredev → svc-nextstoredev (nextstoredev)",
		"nextstoredev → svc-nextstorestage (nextstoreprod)",
	}
	if got := f.ssh.pushes(); !slices.Equal(got, wantPushes) {
		t.Errorf("deploys =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(wantPushes, "\n  "))
	}

	// The broker was asked for the repositories the recipe names.
	if !slices.Equal(f.gitea.asked, []string{"medusadev", "nextstoredev"}) && !slices.Equal(f.gitea.asked, []string{"nextstoredev", "medusadev"}) {
		t.Errorf("broker asked for %v, want medusadev and nextstoredev", f.gitea.asked)
	}
	for _, pair := range [][3]string{{"medusadev", "medusastage", "medusaprod"}, {"nextstoredev", "nextstorestage", "nextstoreprod"}} {
		meta, _ := workflow.ReadServiceMeta(f.stateDir, pair[0])
		if meta == nil || !meta.IsAdopted() || meta.StageHostname != pair[1] || meta.Mode != topology.PlanModeStandard {
			t.Errorf("%s not adopted as a standard pair: %+v", pair[0], meta)
			continue
		}
		if meta.PrimarySetupName != pair[0] || meta.StageSetupName != pair[2] {
			t.Errorf("%s setups = %q/%q", pair[0], meta.PrimarySetupName, meta.StageSetupName)
		}
		if meta.Gitea == nil || meta.Gitea.FullName != "beviro/"+pair[0] || meta.Gitea.Branch != "mate/mate-p1" || meta.GitPushState != topology.GitPushConfigured {
			t.Errorf("%s not wired: gitea=%+v gitPush=%s", pair[0], meta.Gitea, meta.GitPushState)
		}
		// No work session records a stand-up's deploys, so the durable
		// first-deploy mark is stamped directly.
		if !meta.IsDeployed() {
			t.Errorf("%s deployed but not marked deployed", pair[0])
		}
	}
	if !slices.Contains(f.mounter.mounted, "medusadev") || !slices.Contains(f.mounter.mounted, "nextstoredev") {
		t.Errorf("dev halves mounted = %v", f.mounter.mounted)
	}

	// Nothing is delivered: the stage runs main as it is.
	if f.ssh.ran("HEAD:refs/heads/mate/") || f.gitea.pullCreates != 0 {
		t.Errorf("a stand-up must not push or open a pull request (pushed=%v, pulls=%d)", f.ssh.ran("HEAD:refs/heads/mate/"), f.gitea.pullCreates)
	}

	dev := body.service(t, "medusadev")
	if dev.Role != standupRoleDev || dev.Pair != "medusastage" || dev.Deploy == nil || dev.Deploy.Status != standupDeployed {
		t.Errorf("medusadev = %+v", dev)
	}
	if dev.DevServer == nil || dev.DevServer.State != standupDevServerNotStarted || dev.DevServer.Port != 9000 ||
		!strings.Contains(dev.Next, `zerops_dev_server action="start" hostname="medusadev"`) || !strings.Contains(dev.Next, "port=9000") {
		t.Errorf("medusadev's dev server and next step = %+v / %q", dev.DevServer, dev.Next)
	}
	stage := body.service(t, "medusastage")
	if stage.Role != standupRoleStage || stage.Deploy == nil || stage.Deploy.Status != standupDeployed || !strings.Contains(stage.Next, "zerops_verify") {
		t.Errorf("medusastage = %+v", stage)
	}
	if s := body.service(t, "mailpit"); s.Role != standupRolePlatformBuild || !strings.Contains(s.Next, "github.com/zeropsio/recipe-mailpit") {
		t.Errorf("mailpit = %+v", s)
	}
	if s := body.service(t, "db"); s.Role != standupRoleManaged {
		t.Errorf("db = %+v", s)
	}
	if body.Envelope == nil {
		t.Error("the stand-up changes the project; its answer carries the envelope")
	}
	// The group's own environments are the broker's: only the Mate's tier
	// is read, never the stage's or production's.
	if !slices.Equal(f.gitea.read, []string{workflow.MateTierImportPath}) {
		t.Errorf("group repo reads = %v, want only the AI Agent tier", f.gitea.read)
	}
	assertNoTokenOnDisk(t, f.stateDir)

	agents, _ := os.ReadFile(filepath.Join(f.root, "AGENTS.md"))
	if !strings.Contains(string(agents), "medusadev") || !strings.Contains(string(agents), "ZEROPS:REFLOG") {
		t.Errorf("the adoption is recorded in AGENTS.md's reflog:\n%s", agents)
	}
}

// TestStandup_ASecondCallContinuesAndSkipsWhatIsDone: nothing is adopted,
// wired or deployed twice.
func TestStandup_ASecondCallContinuesAndSkipsWhatIsDone(t *testing.T) {
	t.Parallel()
	f := newStandupFixture(t)
	if result, body := f.run(t); result.IsError || body.StandUp != standupReady {
		t.Fatalf("first call: %s", getTextContent(t, result))
	}
	// The platform now reports code in all four halves.
	deployed := slices.Clone(f.services)
	for i := range deployed {
		if strings.HasSuffix(deployed[i].Name, "dev") || strings.HasSuffix(deployed[i].Name, "stage") {
			deployed[i].Status = statusActive
			deployed[i].ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-" + deployed[i].Name, Source: "CLI", Built: true}
		}
	}
	f.mock.WithServices(deployed)
	asked, pushes := len(f.gitea.asked), len(f.ssh.pushes())

	result, body := f.run(t)
	if result.IsError || body.StandUp != standupReady {
		t.Fatalf("second call: %s", getTextContent(t, result))
	}
	if len(f.gitea.asked) != asked || len(f.ssh.pushes()) != pushes {
		t.Errorf("a second call asked the broker %d more times and deployed %d more times", len(f.gitea.asked)-asked, len(f.ssh.pushes())-pushes)
	}
	dev := body.service(t, "medusadev")
	if dev.Adopted != standupAlready || dev.Wired != standupAlready || dev.Deploy == nil || dev.Deploy.Status != standupAlreadyDeployed {
		t.Errorf("medusadev on the second call = %+v", dev)
	}
}

// TestStandup_TheModelIsTheBackup is every way the stand-up can stop: it
// says exactly what failed, does nothing past it for that pair, and hands
// the model the next step.
func TestStandup_TheModelIsTheBackup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(f *standupFixture)
		// wantErr is a refusal before anything is touched: the tool errs.
		wantErr []string
		// otherwise the per-service state after a partial stand-up.
		wantStandUp string
		want        map[string]string // hostname → substring of its failure or reason
		wantPushes  []string
	}{
		{
			name:    "Git access never arrives",
			setup:   func(f *standupFixture) { f.env = map[string]string{"GITEA_URL": f.srv.URL} },
			wantErr: []string{"PREREQUISITE_MISSING", "MATE_BROKER_URL", "GITEA_TOKEN", "route=\\\"adopt\\\""},
		},
		{
			name:    "main has no AI Agent tier",
			setup:   func(f *standupFixture) { f.gitea.tier = "" },
			wantErr: []string{"beviro/group", "0 — AI Agent/import.yaml"},
		},
		{
			name:    "a tier with no pair",
			setup:   func(f *standupFixture) { f.gitea.tier = "services:\n  - hostname: db\n    type: postgresql@17\n" },
			wantErr: []string{"INVALID_IMPORT_YML", "db (managed)"},
		},
		{
			name:    "a tier that does not read",
			setup:   func(f *standupFixture) { f.gitea.tier = "services: [\n" },
			wantErr: []string{"INVALID_IMPORT_YML"},
		},
		{
			name:        "a dev build fails: its stage waits, the next wave does not start",
			setup:       func(f *standupFixture) { f.failBuild("svc-medusadev") },
			wantStandUp: standupFailed,
			want: map[string]string{
				"medusadev":      "BUILD_FAILED",
				"medusastage":    "medusadev did not deploy",
				"nextstoredev":   "an earlier wave did not stand up (medusadev)",
				"nextstorestage": "an earlier wave did not stand up (medusadev)",
			},
			wantPushes: []string{"medusadev → svc-medusadev (medusadev)"},
		},
		{
			name:        "the recipe names a repository that is not there",
			setup:       func(f *standupFixture) { f.gitea.repos = []string{"beviro/medusadev"} },
			wantStandUp: standupPartial,
			want: map[string]string{
				"nextstoredev":   "beviro/nextstoredev is not on Gitea",
				"nextstorestage": "nextstoredev did not stand up",
			},
			wantPushes: []string{
				"medusadev → svc-medusadev (medusadev)",
				"medusadev → svc-medusastage (medusaprod)",
			},
		},
		{
			name:        "a pair that does not stand up holds every lower wave",
			setup:       func(f *standupFixture) { f.gitea.repos = []string{"beviro/nextstoredev"} },
			wantStandUp: standupFailed,
			want: map[string]string{
				"medusadev":      "beviro/medusadev is not on Gitea",
				"nextstoredev":   "an earlier wave did not stand up (medusadev)",
				"nextstorestage": "an earlier wave did not stand up (medusadev)",
			},
		},
		{
			name:        "a dev half the import did not create",
			setup:       func(f *standupFixture) { f.mock.WithServices(withoutService(f.services, "nextstoredev")) },
			wantStandUp: standupPartial,
			want:        map[string]string{"nextstoredev": "not in this project"},
			wantPushes: []string{
				"medusadev → svc-medusadev (medusadev)",
				"medusadev → svc-medusastage (medusaprod)",
			},
		},
		{
			name:        "a dev half imported with a build instead of empty",
			setup:       func(f *standupFixture) { f.ssh.collide["nextstoredev"] = true },
			wantStandUp: standupPartial,
			want:        map[string]string{"nextstoredev": "move or delete them"},
			wantPushes: []string{
				"medusadev → svc-medusadev (medusadev)",
				"medusadev → svc-medusastage (medusaprod)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newStandupFixture(t)
			tt.setup(f)
			result, body := f.run(t)
			text := getTextContent(t, result)
			if len(tt.wantErr) > 0 {
				if !result.IsError {
					t.Fatalf("want a refusal, got: %s", text)
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(text, want) {
						t.Errorf("refusal does not say %q: %s", want, text)
					}
				}
				if metas, _ := workflow.ListServiceMetas(f.stateDir); len(metas) != 0 {
					t.Errorf("a refusal touches nothing, but %d services were adopted", len(metas))
				}
				return
			}
			if result.IsError {
				t.Fatalf("a partial stand-up still answers: %s", text)
			}
			if body.StandUp != tt.wantStandUp {
				t.Errorf("standUp = %q, want %q", body.StandUp, tt.wantStandUp)
			}
			for host, want := range tt.want {
				s := body.service(t, host)
				said := s.Failed
				if s.Deploy != nil {
					said += " " + s.Deploy.Status + " " + s.Deploy.Reason
				}
				if !strings.Contains(said, want) {
					t.Errorf("%s says %q, want %q", host, said, want)
				}
				if s.Next == "" {
					t.Errorf("%s has no next step for the model", host)
				}
			}
			if got := f.ssh.pushes(); !slices.Equal(got, tt.wantPushes) {
				t.Errorf("deploys = %v, want %v", got, tt.wantPushes)
			}
			if !strings.Contains(body.Next, "zerops_standup") {
				t.Errorf("a stand-up that stopped short says it can be called again: %q", body.Next)
			}
		})
	}
}

// failBuild makes the build of service id fail.
func (f *standupFixture) failBuild(id string) {
	for i := range f.events {
		if f.events[i].ServiceStackID == id {
			f.events[i].Status = statusBuildFailed
		}
	}
	f.mock.WithAppVersionEvents(f.events)
}

func withoutService(services []platform.ServiceStack, hostname string) []platform.ServiceStack {
	out := make([]platform.ServiceStack, 0, len(services))
	for _, s := range services {
		if s.Name != hostname {
			out = append(out, s)
		}
	}
	return out
}

// TestStandupProgress_TheAnswerNeverFollowsANotificationAtOnce: a result sent
// in the same chunk as a progress notification breaks Claude Code's client, so
// the answer waits out the gap after the last notification — and does not
// wait at all when none was sent.
func TestStandupProgress_TheAnswerNeverFollowsANotificationAtOnce(t *testing.T) {
	t.Parallel()
	silent := newStandupProgress(func(string, float64, float64) {})
	start := time.Now()
	silent.quiesce(context.Background())
	if time.Since(start) > 100*time.Millisecond {
		t.Error("a stand-up that sent no notification must not wait")
	}

	var got []float64
	spoken := newStandupProgress(func(_ string, progress, _ float64) { got = append(got, progress) })
	spoken.say("one")
	spoken.callback()("a build poll's own percentage", 97, 100)
	spoken.quiesce(context.Background())
	if since := time.Since(spoken.last); since < standupProgressGap {
		t.Errorf("answered %s after the last notification, want at least %s", since, standupProgressGap)
	}
	if !slices.Equal(got, []float64{1, 2}) {
		t.Errorf("progress = %v, want it to grow by one per notification", got)
	}
}
