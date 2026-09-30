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
// whose setups are named after them, no priority between them — their order
// is what the storefront's build reads (standupZeropsYAML) — a public-build
// mailpit and a managed database.
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
  - hostname: medusastage
    type: nodejs@22
    buildFromGit: GITEA/beviro/medusadev
    zeropsSetup: medusaprod
    enableSubdomainAccess: true
  - hostname: mailpit
    type: alpine@3.20
    buildFromGit: https://github.com/zeropsio/recipe-mailpit
  - hostname: db
    type: postgresql@17
    mode: NON_HA
    priority: 10
`

// standupZeropsYAML is each dev half's zerops.yaml as main carries it: the
// dev setup deploys the whole repository and idles on a no-op keepalive; the
// storefront's stage build pre-renders from the API's stage.
func standupZeropsYAML(pair string) string {
	buildEnv := ""
	if pair == "nextstore" {
		buildEnv = "\n      envVariables:\n        NEXT_PUBLIC_MEDUSA_BACKEND_URL: https://${medusastage_zeropsSubdomain}"
	}
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
      deployFiles: [.]%[2]s
    run:
      base: nodejs@22
      ports:
        - port: 9000
          httpSupport: true
      start: npm run start
`, pair, buildEnv)
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
			// A bot's token lacks read:organization (measured 2026-09-30).
			write(http.StatusForbidden, map[string]string{"message": "token does not have at least one of required scope(s), required=[read:user read:organization]"})
		case path == "/api/v1/user/repos":
			repos := []map[string]any{}
			for _, org := range g.orgs {
				repos = append(repos, map[string]any{"name": "group", "full_name": org + "/group", "owner": map[string]any{"login": org}})
			}
			write(http.StatusOK, repos)
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
	// together names the service ids whose pushes must be in flight at
	// once: each waits for the others, and one that waits in vain is apart.
	together map[string]bool
	arrived  map[string]bool
	allIn    chan struct{}
	apart    []string
}

type standupSSHCall struct{ host, cmd string }

func (s *standupSSH) ExecSSH(_ context.Context, host, cmd string) ([]byte, error) {
	s.mu.Lock()
	s.commands = append(s.commands, standupSSHCall{host, cmd})
	s.mu.Unlock()
	if strings.Contains(cmd, "zcli push") {
		s.meet(flagValue(cmd, "--service-id"))
	}
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

// meet holds a push to one of the together ids until every one of them is in
// flight, or records it as apart once that does not happen within a second.
func (s *standupSSH) meet(id string) {
	s.mu.Lock()
	if !s.together[id] {
		s.mu.Unlock()
		return
	}
	s.arrived[id] = true
	if len(s.arrived) == len(s.together) {
		close(s.allIn)
	}
	allIn := s.allIn
	s.mu.Unlock()
	select {
	case <-allIn:
	case <-time.After(time.Second):
		s.mu.Lock()
		s.apart = append(s.apart, id)
		s.mu.Unlock()
	}
}

// expectTogether makes the pushes to ids meet (meet).
func (s *standupSSH) expectTogether(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.together, s.arrived, s.allIn = map[string]bool{}, map[string]bool{}, make(chan struct{})
	for _, id := range ids {
		s.together[id] = true
	}
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
	// importing, when set, answers the service list as the browser's
	// runtime import is creating it.
	importing *importingClient
	// building, when set, is the platform with a stage build still running.
	building *buildingClient
}

// importingClient is the platform while the browser's runtime import is still
// running: its first lists hold no runtime yet, the next a dev half still
// starting and a stage half not created, and only then the whole project.
type importingClient struct {
	*platform.Mock
	mu    sync.Mutex
	reads int
	// stages are the lists answered in turn; the last one stays.
	stages [][]platform.ServiceStack
}

func (c *importingClient) ListServicesDirect(ctx context.Context, projectID string) ([]platform.ServiceStack, error) {
	c.mu.Lock()
	stage := c.stages[min(c.reads, len(c.stages)-1)]
	c.reads++
	c.mu.Unlock()
	if stage == nil {
		return c.Mock.ListServicesDirect(ctx, projectID)
	}
	return stage, nil
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
	var client platform.Client = f.mock
	if f.importing != nil {
		client = f.importing
	}
	if f.building != nil {
		client = f.building
	}
	registerStandup(srv, standupDeps{
		batch: batchDeployer{
			client:      client,
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
		runtimeWait: 200 * time.Millisecond,
		runtimePoll: 5 * time.Millisecond,
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
// group's tier over its two calls: every pair adopted exactly as adopt records
// it, wired to the repository its recipe names, its dev half deployed on the
// first call and its stage cross-deployed from it on the second — and nothing
// committed, pushed or proposed.
func TestStandup_StandsUpEveryPairFromTheRecipe(t *testing.T) {
	t.Parallel()
	f := newStandupFixture(t)
	if result, body := f.run(t); result.IsError || body.StandUp != standupDevelopment {
		t.Fatalf("first call: %s", getTextContent(t, result))
	}
	f.devsDeployed()
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

	// Every half deployed once, over the two calls: each stage cross-deployed
	// from its dev half.
	want := []string{
		"medusadev → svc-medusadev (medusadev)", "medusadev → svc-medusastage (medusaprod)",
		"nextstoredev → svc-nextstoredev (nextstoredev)", "nextstoredev → svc-nextstorestage (nextstoreprod)",
	}
	if got := f.ssh.pushes(); !slices.Equal(sorted(got), want) {
		t.Errorf("deploys =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
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
	if dev.Role != standupRoleDev || dev.Pair != "medusastage" || dev.Deploy == nil || dev.Deploy.Status != standupAlreadyDeployed {
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
	if slices.ContainsFunc(f.gitea.read, func(path string) bool { return path != workflow.MateTierImportPath }) {
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
	if result, body := f.run(t); result.IsError || body.StandUp != standupDevelopment {
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
		wantNext    map[string]string // hostname → substring of its next step
		// wantDeployed are the halves deployed even so.
		wantDeployed []string
		wantPushes   []string
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
			name:        "a dev build fails: its stage is skipped, and the stage that reads it waits",
			setup:       func(f *standupFixture) { f.failBuild("svc-medusadev") },
			wantStandUp: standupPartial,
			want: map[string]string{
				"medusadev":      "BUILD_FAILED",
				"medusastage":    "medusadev did not deploy",
				"nextstorestage": standupQueued,
			},
			wantDeployed: []string{"nextstoredev"},
			wantPushes: []string{
				"medusadev → svc-medusadev (medusadev)",
				"nextstoredev → svc-nextstoredev (nextstoredev)",
			},
		},
		{
			name: "a stage build fails: the stage that reads it says what it waited for",
			setup: func(f *standupFixture) {
				f.devsDeployed()
				f.failBuild("svc-medusastage")
			},
			wantStandUp: standupPartial,
			want: map[string]string{
				"medusastage":    "BUILD_FAILED",
				"nextstorestage": "waits for medusastage, which did not stand up",
			},
			wantPushes: []string{"medusadev → svc-medusastage (medusaprod)"},
		},
		{
			name:        "the recipe names a repository that is not there",
			setup:       func(f *standupFixture) { f.gitea.repos = []string{"beviro/medusadev"} },
			wantStandUp: standupPartial,
			want: map[string]string{
				"nextstoredev":   "beviro/nextstoredev is not on Gitea",
				"nextstorestage": "nextstoredev did not stand up",
				"medusastage":    standupQueued,
			},
			wantPushes: []string{"medusadev → svc-medusadev (medusadev)"},
		},
		{
			name: "a pair that does not stand up holds only the stages below it",
			setup: func(f *standupFixture) {
				f.devsDeployed()
				f.gitea.repos = []string{"beviro/nextstoredev"}
			},
			wantStandUp: standupPartial,
			want: map[string]string{
				"medusadev":      "beviro/medusadev is not on Gitea",
				"nextstorestage": "waits for medusastage, which did not stand up",
			},
		},
		{
			name:        "a dev half the import did not create, after the wait",
			setup:       func(f *standupFixture) { f.mock.WithServices(withoutService(f.services, "nextstoredev")) },
			wantStandUp: standupPartial,
			want:        map[string]string{"nextstoredev": "not in this project after", "medusastage": standupQueued},
			wantNext: map[string]string{
				"nextstoredev":   `zerops_import content="services: [{hostname: nextstoredev, type: nodejs@22, startWithoutCode: true}]"`,
				"nextstorestage": "zerops_import",
			},
			wantPushes: []string{"medusadev → svc-medusadev (medusadev)"},
		},
		{
			name:        "a stage half the import did not create, after the wait",
			setup:       func(f *standupFixture) { f.mock.WithServices(withoutService(f.services, "medusastage")) },
			wantStandUp: standupPartial,
			want: map[string]string{
				"medusastage":    "not in this project after",
				"nextstorestage": standupQueued,
			},
			wantNext: map[string]string{
				"medusastage": `zerops_import content="services: [{hostname: medusastage, type: nodejs@22}]"`,
			},
			wantDeployed: []string{"nextstoredev"},
			wantPushes:   []string{"nextstoredev → svc-nextstoredev (nextstoredev)"},
		},
		{
			name:        "a dev half imported with a build instead of empty",
			setup:       func(f *standupFixture) { f.ssh.collide["nextstoredev"] = true },
			wantStandUp: standupPartial,
			want:        map[string]string{"nextstoredev": "move or delete them"},
			wantPushes:  []string{"medusadev → svc-medusadev (medusadev)"},
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
			for _, host := range tt.wantDeployed {
				if d := body.service(t, host).Deploy; d == nil || d.Status != standupDeployed {
					t.Errorf("%s deploy = %+v, want it deployed", host, d)
				}
			}
			for host, want := range tt.wantNext {
				if next := body.service(t, host).Next; !strings.Contains(next, want) {
					t.Errorf("%s's next step = %q, want %q", host, next, want)
				}
			}
			if got := sorted(f.ssh.pushes()); !slices.Equal(got, tt.wantPushes) {
				t.Errorf("deploys = %v, want %v", got, tt.wantPushes)
			}
			if !strings.Contains(body.Next, "zerops_standup") {
				t.Errorf("a stand-up that stopped short says it can be called again: %q", body.Next)
			}
		})
	}
}

// devsDeployed has the platform report code in both dev halves, as it does
// after the stand-up's first call.
func (f *standupFixture) devsDeployed() {
	for i := range f.services {
		if strings.HasSuffix(f.services[i].Name, "dev") {
			f.services[i].ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-" + f.services[i].Name, Source: "CLI", Built: true}
		}
	}
	f.mock.WithServices(f.services)
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

// sorted is a sorted copy of s: deploys that run at once land in any order.
func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
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

// TestStandup_WaitsForTheRuntimesTheBrowserIsImporting: the browser imports
// the runtimes just before the person signs the agent in, so the first turn
// can arrive while they are still being created. The stand-up waits — for
// every dev half to run and every stage half to exist — and then stands the
// project up as if they had been there all along.
func TestStandup_WaitsForTheRuntimesTheBrowserIsImporting(t *testing.T) {
	t.Parallel()
	f := newStandupFixture(t)
	noRuntimes := withoutService(withoutService(withoutService(withoutService(f.services,
		"medusadev"), "medusastage"), "nextstoredev"), "nextstorestage")
	starting := slices.Clone(withoutService(f.services, "nextstorestage"))
	for i := range starting {
		if starting[i].Name == "medusadev" {
			starting[i].Status = "CREATING"
		}
	}
	f.importing = &importingClient{Mock: f.mock, stages: [][]platform.ServiceStack{noRuntimes, noRuntimes, starting, nil}}

	result, body := f.run(t)
	if result.IsError || body.StandUp != standupDevelopment {
		t.Fatalf("stand-up after the import = %s", getTextContent(t, result))
	}
	if f.importing.reads < 4 {
		t.Errorf("the service list was read %d times, want it read until the import finished", f.importing.reads)
	}
	if len(f.ssh.pushes()) != 2 {
		t.Errorf("deploys = %v, want both dev halves deployed", f.ssh.pushes())
	}
}

// buildingClient is the platform while an earlier call's build of a stage is
// still running: the process is live until it is first read, and from then on
// the stage runs the code it built.
type buildingClient struct {
	*platform.Mock
	stage   string
	mu      sync.Mutex
	waited  bool
	running []platform.Process
}

func (c *buildingClient) GetProcess(ctx context.Context, id string) (*platform.Process, error) {
	c.mu.Lock()
	c.waited = true
	c.mu.Unlock()
	return c.Mock.GetProcess(ctx, id)
}

func (c *buildingClient) GetProjectProcessesDirect(ctx context.Context, projectID string) ([]platform.Process, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.waited {
		return nil, nil
	}
	return c.running, nil
}

func (c *buildingClient) ListServicesDirect(ctx context.Context, projectID string) ([]platform.ServiceStack, error) {
	services, err := c.Mock.ListServicesDirect(ctx, projectID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.waited {
		return services, err
	}
	out := slices.Clone(services)
	for i := range out {
		if out[i].Name == c.stage {
			out[i].Status = statusActive
			out[i].ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-built", Source: "CLI", Built: true}
		}
	}
	return out, err
}

// TestStandup_ReturnsOnceDevelopmentIsUp: the first call deploys every dev
// half at once and answers as soon as they stand, the stages queued for a
// second call; the second deploys the stages through the same order — a
// stage after its dev half and the stages above it — and waits for a build
// an earlier call left running rather than starting another.
func TestStandup_ReturnsOnceDevelopmentIsUp(t *testing.T) {
	t.Parallel()
	const (
		medusadev      = "medusadev → svc-medusadev (medusadev)"
		medusastage    = "medusadev → svc-medusastage (medusaprod)"
		nextstoredev   = "nextstoredev → svc-nextstoredev (nextstoredev)"
		nextstorestage = "nextstoredev → svc-nextstorestage (nextstoreprod)"
	)
	tests := []struct {
		name        string
		setup       func(f *standupFixture)
		wantStandUp string
		wantPushes  []string
		// wantBefore are pushes that come before others, in order.
		wantBefore [][2]string
		// wantStatus is each half's deploy status, wantSaid a substring of
		// its reason.
		wantStatus map[string]string
		wantSaid   map[string]string
		wantNext   []string
		wantWaited bool
	}{
		{
			name:        "every dev half stands: the answer comes with the stages queued",
			setup:       func(f *standupFixture) { f.ssh.expectTogether("svc-medusadev", "svc-nextstoredev") },
			wantStandUp: standupDevelopment,
			wantPushes:  []string{medusadev, nextstoredev},
			wantStatus: map[string]string{
				"medusadev": standupDeployed, "nextstoredev": standupDeployed,
				"medusastage": standupQueued, "nextstorestage": standupQueued,
			},
			wantNext: []string{"development is up", "zerops_dev_server", "zerops_standup again", "READY_TO_DEPLOY"},
		},
		{
			name:        "a dev half fails: its stage is skipped and says why",
			setup:       func(f *standupFixture) { f.failBuild("svc-nextstoredev") },
			wantStandUp: standupPartial,
			wantPushes:  []string{medusadev, nextstoredev},
			wantStatus: map[string]string{
				"medusadev": standupDeployed, "nextstoredev": standupDeployFailed,
				"medusastage": standupQueued, "nextstorestage": standupNotDeployed,
			},
			wantSaid: map[string]string{"nextstorestage": "nextstoredev did not deploy"},
			wantNext: []string{"zerops_standup"},
		},
		{
			name: "a dev half whose build reads a failed dev half waits for it and says so",
			setup: func(f *standupFixture) {
				body := strings.Replace(standupZeropsYAML("nextstore"), "buildCommands: [npm ci]",
					"buildCommands: [npm ci]\n      envVariables:\n        API: ${medusadev_zeropsSubdomain}", 1)
				if err := os.WriteFile(filepath.Join(f.root, "nextstoredev", "zerops.yaml"), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				f.failBuild("svc-medusadev")
			},
			wantStandUp: standupFailed,
			wantPushes:  []string{medusadev},
			wantStatus:  map[string]string{"medusadev": standupDeployFailed, "nextstoredev": standupNotDeployed},
			wantSaid:    map[string]string{"nextstoredev": "waits for medusadev, which did not stand up"},
			wantNext:    []string{"zerops_standup"},
		},
		{
			name:        "the second call, the dev halves running: only the stages deploy, in order",
			setup:       func(f *standupFixture) { f.devsDeployed() },
			wantStandUp: standupReady,
			wantPushes:  []string{medusastage, nextstorestage},
			wantBefore:  [][2]string{{medusastage, nextstorestage}},
			wantStatus: map[string]string{
				"medusadev": standupAlreadyDeployed, "nextstoredev": standupAlreadyDeployed,
				"medusastage": standupDeployed, "nextstorestage": standupDeployed,
			},
		},
		{
			name: "the second call waits for a stage build still in flight",
			setup: func(f *standupFixture) {
				f.devsDeployed()
				f.mock.WithProcess(&platform.Process{ID: "proc-medusastage", ActionName: "stack.build", Status: "FINISHED"})
				f.building = &buildingClient{Mock: f.mock, stage: "medusastage", running: []platform.Process{{
					ID: "proc-medusastage", ActionName: "stack.build", Status: "RUNNING", Created: "2026-09-30T10:00:00Z",
					ServiceStacks: []platform.ServiceStackRef{{ID: "svc-medusastage", Name: "medusastage"}},
				}}}
			},
			wantStandUp: standupReady,
			wantPushes:  []string{nextstorestage},
			wantStatus: map[string]string{
				"medusastage": standupAlreadyDeployed, "nextstorestage": standupDeployed,
			},
			wantWaited: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newStandupFixture(t)
			tt.setup(f)
			result, body := f.run(t)
			if result.IsError {
				t.Fatalf("stand-up = %s", getTextContent(t, result))
			}
			if body.StandUp != tt.wantStandUp {
				t.Errorf("standUp = %q, want %q: %s", body.StandUp, tt.wantStandUp, body.Next)
			}
			got := f.ssh.pushes()
			if !slices.Equal(sorted(got), tt.wantPushes) {
				t.Errorf("deploys = %v, want %v", got, tt.wantPushes)
			}
			if len(f.ssh.apart) > 0 {
				t.Errorf("the dev halves did not build at once: %v waited alone", f.ssh.apart)
			}
			for _, edge := range tt.wantBefore {
				if slices.Index(got, edge[0]) > slices.Index(got, edge[1]) {
					t.Errorf("%q deployed before %q: %v", edge[1], edge[0], got)
				}
			}
			for host, want := range tt.wantStatus {
				if d := body.service(t, host).Deploy; d == nil || d.Status != want {
					t.Errorf("%s deploy = %+v, want %q", host, d, want)
				}
			}
			for host, want := range tt.wantSaid {
				if d := body.service(t, host).Deploy; d == nil || !strings.Contains(d.Reason, want) {
					t.Errorf("%s deploy = %+v, want it to say %q", host, d, want)
				}
			}
			for _, want := range tt.wantNext {
				if !strings.Contains(body.Next, want) {
					t.Errorf("next = %q, want it to say %q", body.Next, want)
				}
			}
			if tt.wantWaited && !f.building.waited {
				t.Error("the build still in flight was not waited for")
			}
		})
	}
}
