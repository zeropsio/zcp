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
	"github.com/zeropsio/zcp/internal/mate"
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

// standupHead is the commit the dev halves' checkouts stand on.
const (
	standupHead      = "0123456789abcdef0123456789abcdef01234567"
	standupHeadShort = "0123456"
)

func (s *standupSSH) ExecSSH(_ context.Context, host, cmd string) ([]byte, error) {
	s.mu.Lock()
	s.commands = append(s.commands, standupSSHCall{host, cmd})
	s.mu.Unlock()
	if strings.Contains(cmd, "zcli push") {
		s.meet(flagValue(cmd, "--service-id"))
	}
	switch {
	case strings.Contains(cmd, "rev-parse --verify") && strings.Contains(cmd, "^{commit}"):
		return []byte(standupHead + "\n"), nil
	case strings.Contains(cmd, "git show") && strings.Contains(cmd, ":zerops.yaml"):
		return []byte(standupZeropsYAML(strings.TrimSuffix(host, "dev"))), nil
	case strings.Contains(cmd, "mktemp -d"):
		return []byte("/tmp/zcp-extract-1\n"), nil
	case strings.Contains(cmd, "ZCP:BRANCH:%s"):
		// The dev half's HEAD, on the Mate's branch, its tree dirtied by
		// a dev server.
		return []byte(standupHead + "\nZCP:BRANCH:mate/mate-p1\nM"), nil
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

// commandsFor is every command that contains substr, in order.
func (s *standupSSH) commandsFor(substr string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.commands {
		if strings.Contains(c.cmd, substr) {
			out = append(out, c.cmd)
		}
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
	// statusPath is the setup status file the stand-up writes its section of.
	statusPath string
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
	// firstRead is when the list was first read.
	firstRead time.Time
}

func (c *importingClient) ListServicesDirect(ctx context.Context, projectID string) ([]platform.ServiceStack, error) {
	c.mu.Lock()
	if c.firstRead.IsZero() {
		c.firstRead = time.Now()
	}
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
	f.statusPath = filepath.Join(f.root, "mate-status.json")
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
		statusPath:  f.statusPath,
		bootWait:    2 * time.Second,
		bootPoll:    5 * time.Millisecond,
		trackPoll:   5 * time.Millisecond,
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

	// A stage ships the dev half's HEAD commit exactly, never its working
	// tree: the dev servers the model starts between the two calls may touch
	// tracked files, and a dirty name reads as no commit.
	for _, cmd := range f.ssh.commandsFor("zcli push") {
		if !strings.Contains(cmd, "svc-medusastage") && !strings.Contains(cmd, "svc-nextstorestage") {
			continue
		}
		if !strings.Contains(cmd, "--no-git") || !strings.Contains(cmd, "--version-name 'mate/mate-p1 "+standupHeadShort+"'") {
			t.Errorf("a stage push must ship HEAD's commit under a clean name: %s", cmd)
		}
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
func (f *standupFixture) devsDeployed() { f.deployed("medusadev", "nextstoredev") }

// deployed has the platform report code in the named services.
func (f *standupFixture) deployed(hosts ...string) {
	for i := range f.services {
		if slices.Contains(hosts, f.services[i].Name) {
			f.services[i].Status = statusActive
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
			name:        "a stage deployed by hand runs its code: already deployed, never queued",
			setup:       func(f *standupFixture) { f.deployed("nextstorestage") },
			wantStandUp: standupDevelopment,
			wantPushes:  []string{medusadev, nextstoredev},
			wantStatus: map[string]string{
				"medusastage": standupQueued, "nextstorestage": standupAlreadyDeployed,
			},
		},
		{
			name: "a dev half that runs its code is already deployed, whatever it waits for",
			setup: func(f *standupFixture) {
				body := strings.Replace(standupZeropsYAML("nextstore"), "buildCommands: [npm ci]",
					"buildCommands: [npm ci]\n      envVariables:\n        API: ${medusadev_zeropsSubdomain}", 1)
				if err := os.WriteFile(filepath.Join(f.root, "nextstoredev", "zerops.yaml"), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				f.deployed("nextstoredev")
				f.failBuild("svc-medusadev")
			},
			wantStandUp: standupPartial,
			wantPushes:  []string{medusadev},
			wantStatus:  map[string]string{"medusadev": standupDeployFailed, "nextstoredev": standupAlreadyDeployed},
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

// standupSection reads the stand-up's section of the status file as
// "host=step/state" per service, sorted.
func (f *standupFixture) standupSection(t *testing.T) (mate.StandupStatus, []string) {
	t.Helper()
	st, err := mate.ReadStatus(f.statusPath)
	if err != nil {
		t.Fatalf("read the status file: %v", err)
	}
	rows := make([]string, 0, len(st.Standup.Services))
	for _, e := range st.Standup.Services {
		rows = append(rows, e.Hostname+"="+e.Step+"/"+e.State)
	}
	slices.Sort(rows)
	return st.Standup, rows
}

// TestStandup_WritesItsProgressForTheRunCard: each call writes the stand-up
// section of the status file the mate server relays to the run card — the
// phase, every half it touches with its step and state, and how the call
// ended with what failed.
func TestStandup_WritesItsProgressForTheRunCard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		setup     func(f *standupFixture)
		calls     int
		wantState string
		wantPhase string
		wantRows  []string
		wantError string
	}{
		{
			name:      "the first call deploys development, the stages wait",
			calls:     1,
			wantState: mate.StandupDone, wantPhase: mate.PhaseDevelopment,
			wantRows: []string{"medusadev=verify/done", "medusastage=build/pending", "nextstoredev=verify/done", "nextstorestage=build/pending"},
		},
		{
			name:      "the second call deploys the stages",
			calls:     2,
			wantState: mate.StandupDone, wantPhase: mate.PhaseStage,
			wantRows: []string{"medusadev=verify/done", "medusastage=verify/done", "nextstoredev=verify/done", "nextstorestage=verify/done"},
		},
		{
			name:      "a failed build fails its half and the call",
			setup:     func(f *standupFixture) { f.failBuild("svc-medusadev") },
			calls:     1,
			wantState: mate.StandupFailed, wantPhase: mate.PhaseDevelopment,
			wantRows:  []string{"medusadev=build/failed", "medusastage=build/failed", "nextstoredev=verify/done", "nextstorestage=build/pending"},
			wantError: "medusadev: the deploy ended BUILD_FAILED",
		},
		{
			name:      "a refusal fails the call with its reason",
			setup:     func(f *standupFixture) { f.env = map[string]string{} },
			calls:     1,
			wantState: mate.StandupFailed, wantPhase: mate.PhaseDevelopment,
			wantError: "Git access has not reached this Mate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newStandupFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			for call := range tt.calls {
				f.run(t)
				if call == 0 {
					f.devsDeployed()
				}
			}
			section, rows := f.standupSection(t)
			if section.State != tt.wantState || section.Phase != tt.wantPhase {
				t.Errorf("stand-up = %s/%s, want %s/%s (%q)", section.State, section.Phase, tt.wantState, tt.wantPhase, section.Error)
			}
			if !slices.Equal(rows, tt.wantRows) {
				t.Errorf("services =\n  %s\nwant\n  %s", strings.Join(rows, "\n  "), strings.Join(tt.wantRows, "\n  "))
			}
			if !strings.Contains(section.Error, tt.wantError) {
				t.Errorf("error = %q, want it to carry %q", section.Error, tt.wantError)
			}
			if section.StartedAt == "" || section.EndedAt == "" {
				t.Errorf("startedAt/endedAt = %q/%q", section.StartedAt, section.EndedAt)
			}
		})
	}
}

// TestStandup_WaitsForTheContainersImport: the container imports a new
// Mate's runtimes at boot (MATE_SETUP_RUNTIMES); a stand-up that starts while
// that import runs waits for it instead of reporting the halves missing, and
// a halt it ended in reaches the model with the import's own reason.
func TestStandup_WaitsForTheContainersImport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		runtimes   mate.RuntimesStatus
		finishWith *mate.RuntimesStatus
		wantWaited bool
		wantFail   string
	}{
		{
			name:       "an import in flight is waited for",
			runtimes:   mate.RuntimesStatus{State: mate.RuntimesImporting, Services: []mate.RuntimeService{{Hostname: "nextstorestage", State: mate.ServiceCreating}}},
			finishWith: &mate.RuntimesStatus{State: mate.RuntimesDone},
			wantWaited: true,
		},
		{
			name:     "a finished import is not waited for",
			runtimes: mate.RuntimesStatus{State: mate.RuntimesDone},
		},
		{
			name:     "a failed import names its reason for the missing half",
			runtimes: mate.RuntimesStatus{State: mate.RuntimesFailed, Services: []mate.RuntimeService{{Hostname: "nextstorestage", State: mate.ServiceFailed, Error: "serviceStackTypeNotFound: no such type"}}},
			wantFail: "serviceStackTypeNotFound: no such type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newStandupFixture(t)
			if err := mate.UpdateStatus(f.statusPath, func(s *mate.Status) { s.Runtimes = tt.runtimes }); err != nil {
				t.Fatal(err)
			}
			// The project already lists every half — what the stand-up
			// waits for is the file saying the import is over, not the list.
			present := f.services
			if tt.wantFail != "" {
				present = withoutService(f.services, "nextstorestage")
			}
			f.importing = &importingClient{Mock: f.mock, stages: [][]platform.ServiceStack{present}}
			var finishedAt time.Time
			finished := make(chan struct{})
			if tt.finishWith != nil {
				go func() {
					defer close(finished)
					time.Sleep(80 * time.Millisecond)
					finishedAt = time.Now()
					_ = mate.UpdateStatus(f.statusPath, func(s *mate.Status) { s.Runtimes = *tt.finishWith })
				}()
			} else {
				close(finished)
			}
			start := time.Now()
			result, body := f.run(t)
			<-finished
			if result.IsError {
				t.Fatalf("stand-up: %s", getTextContent(t, result))
			}
			if tt.wantWaited {
				f.importing.mu.Lock()
				firstRead := f.importing.firstRead
				f.importing.mu.Unlock()
				if firstRead.IsZero() || firstRead.Before(finishedAt) {
					t.Errorf("the stand-up read the project at +%s, before the import finished at +%s", firstRead.Sub(start), finishedAt.Sub(start))
				}
				if got := body.service(t, "nextstorestage"); got.Failed != "" {
					t.Errorf("nextstorestage after the wait = %+v", got)
				}
			}
			if tt.wantFail != "" {
				if got := body.service(t, "nextstorestage"); !strings.Contains(got.Failed, tt.wantFail) {
					t.Errorf("nextstorestage.failed = %q, want it to carry %q", got.Failed, tt.wantFail)
				}
			}
			if !tt.wantWaited && time.Since(start) > time.Second {
				t.Errorf("the stand-up waited %s on an import it should not wait for", time.Since(start))
			}
		})
	}
}

// trackingClient is the platform while a batch deploys: medusadev's build
// process builds, then deploys, then ends.
type trackingClient struct {
	*platform.Mock
	mu    sync.Mutex
	reads int
}

func (c *trackingClient) GetProjectProcessesDirect(context.Context, string) ([]platform.Process, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	ref := []platform.ServiceStackRef{{ID: "svc-medusadev"}}
	switch {
	case c.reads <= 2:
		return []platform.Process{{ID: "proc-build", ActionName: "stack.build", Status: "RUNNING", ServiceStacks: ref, AppVersion: &platform.ProcessAppVersion{Status: "BUILDING"}}}, nil
	case c.reads <= 4:
		return []platform.Process{{ID: "proc-build", ActionName: "stack.build", Status: "RUNNING", ServiceStacks: ref, AppVersion: &platform.ProcessAppVersion{Status: "DEPLOYING"}}}, nil
	}
	return nil, nil
}

// TestStandupTrackDeploys_NamesTheStepAndItsProcess: while a half deploys,
// the status names its step and the process behind it — build, then deploy,
// then verify once the process has ended and the deploy has not returned.
func TestStandupTrackDeploys_NamesTheStepAndItsProcess(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "status.json")
	client := &trackingClient{Mock: platform.NewMock()}
	d := standupDeps{batch: batchDeployer{client: client, projectID: "p1"}, trackPoll: 5 * time.Millisecond}
	status := newStandupStatus(path)
	var seen []string
	stop := d.trackDeploys(context.Background(), []string{"medusadev"},
		map[string]*platform.ServiceStack{"medusadev": {ID: "svc-medusadev", Name: "medusadev"}}, status)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := mate.ReadStatus(path); err == nil && len(st.Standup.Services) == 1 {
			e := st.Standup.Services[0]
			row := e.Step + "/" + e.State + "/" + e.ProcessID
			if len(seen) == 0 || seen[len(seen)-1] != row {
				seen = append(seen, row)
			}
			if e.Step == mate.StepVerify {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	stop()
	want := []string{"build/running/proc-build", "deploy/running/proc-build", "verify/running/proc-build"}
	if !slices.Equal(seen, want) {
		t.Errorf("steps seen = %v, want %v", seen, want)
	}
}

// TestStandupStatus_BeatsWhileRunning: a running stand-up rewrites the file
// at least every beat, so the server can read a running section the file has
// not moved for in two minutes as a stand-up that died (mate.StandupStale);
// once it ends it stops.
func TestStandupStatus_BeatsWhileRunning(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "status.json")
	status := newStandupStatus(path)
	status.begin()
	stop := status.beat(5 * time.Millisecond)
	writes := func(d time.Duration) int {
		seen := map[time.Time]bool{}
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			if info, err := os.Stat(path); err == nil {
				seen[info.ModTime()] = true
			}
			time.Sleep(time.Millisecond)
		}
		return len(seen)
	}
	if n := writes(150 * time.Millisecond); n < 4 {
		t.Errorf("the file was written %d times in 150 ms while running, want a beat every 5 ms", n)
	}
	stop()
	if n := writes(60 * time.Millisecond); n != 1 {
		t.Errorf("the file was written %d times after the stand-up ended, want none", n-1)
	}
	if mate.StandupBeat != 15*time.Second || mate.StandupStale != 2*time.Minute {
		t.Errorf("beat %s / stale %s, want 15s / 2m", mate.StandupBeat, mate.StandupStale)
	}
}

// TestStandup_NeverAsksForAnImportIntoAnOpenProject: a half missing from a
// Mate whose project is not closed off yet is not handed to the model as an
// import — the import would refuse — but as the Finish setup that closes it.
func TestStandup_NeverAsksForAnImportIntoAnOpenProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		isolation string
		plan      bool
		want      string
		wantNot   string
	}{
		{"open", "none", true, "Finish setup", "zerops_import"},
		{"closed off", "service", true, "zerops_import", "Finish setup"},
		{"open, a Mate made before the new press", "none", false, "zerops_import", "Finish setup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newStandupFixture(t)
			if tt.plan {
				f.env["MATE_SETUP_RUNTIMES"] = "c2VydmljZXM6IFtd"
			}
			f.mock.WithProjectEnv([]platform.ProjectEnvVar{{Key: "envIsolation", Content: tt.isolation, Type: platform.ProjectEnvSystem}})
			f.mock.WithServices(withoutService(f.services, "nextstorestage"))
			result, body := f.run(t)
			if result.IsError {
				t.Fatalf("stand-up: %s", getTextContent(t, result))
			}
			got := body.service(t, "nextstorestage")
			if !strings.Contains(got.Next, tt.want) || strings.Contains(got.Next, tt.wantNot) {
				t.Errorf("nextstorestage.next = %q, want it to say %q and not %q", got.Next, tt.want, tt.wantNot)
			}
		})
	}
}
