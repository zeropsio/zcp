package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestGitPush_DefaultBranchFromMeta pins GF-7 (docs/spec-workflows.md
// §12.6): the container git-push default branch reads meta.TrackedRef —
// falling back to "main" only when meta carries none — instead of the old
// hardcoded "main" literal. An explicit input branch always overrides.
func TestGitPush_DefaultBranchFromMeta(t *testing.T) {
	t.Parallel()

	t.Run("recorded tracked ref wins over the hardcoded default", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname:         "appdev",
			Mode:             topology.PlanModeSimple,
			TrackedRef:       "release",
			BootstrapSession: "t",
			BootstrappedAt:   "2026-09-15",
		}); err != nil {
			t.Fatalf("WriteServiceMeta: %v", err)
		}
		if got := resolveTrackedBranch(stateDir, "appdev", ""); got != "release" {
			t.Errorf("resolveTrackedBranch = %q, want %q (recorded trackedRef)", got, "release")
		}
	})

	t.Run("no recorded trackedRef falls back to main", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname:         "appdev",
			Mode:             topology.PlanModeSimple,
			BootstrapSession: "t",
			BootstrappedAt:   "2026-09-15",
		}); err != nil {
			t.Fatalf("WriteServiceMeta: %v", err)
		}
		if got := resolveTrackedBranch(stateDir, "appdev", ""); got != "main" {
			t.Errorf("resolveTrackedBranch = %q, want %q (fallback)", got, "main")
		}
	})

	t.Run("no meta at all falls back to main", func(t *testing.T) {
		t.Parallel()
		if got := resolveTrackedBranch(t.TempDir(), "unknown-host", ""); got != "main" {
			t.Errorf("resolveTrackedBranch = %q, want %q (fallback, no meta)", got, "main")
		}
	})

	t.Run("explicit input branch overrides the recorded trackedRef", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname:         "appdev",
			Mode:             topology.PlanModeSimple,
			TrackedRef:       "release",
			BootstrapSession: "t",
			BootstrappedAt:   "2026-09-15",
		}); err != nil {
			t.Fatalf("WriteServiceMeta: %v", err)
		}
		if got := resolveTrackedBranch(stateDir, "appdev", "hotfix"); got != "hotfix" {
			t.Errorf("resolveTrackedBranch = %q, want %q (explicit override)", got, "hotfix")
		}
	})
}

// TestTrackedRefOrDefault_NilMeta_ReturnsMain pins the nil-safety of the
// single "main" fallback owner shared by every GF-7 reader.
func TestTrackedRefOrDefault_NilMeta_ReturnsMain(t *testing.T) {
	t.Parallel()
	if got := trackedRefOrDefault(nil); got != "main" {
		t.Errorf("trackedRefOrDefault(nil) = %q, want %q", got, "main")
	}
}

// giteaWiringSSH is stubSSHWithCommands with the wiring's own two commands
// answered the way a container answers them: the identity seed reports what
// it seeded, and the branch step refuses with refusal while it is set.
type giteaWiringSSH struct {
	*stubSSHWithCommands
	refusal string
}

func (s *giteaWiringSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	switch {
	case strings.Contains(command, "cur_email=$(git config user.email)"):
		s.commands = append(s.commands, command)
		return []byte("ZCP_EMAIL_SEEDED\nZCP_NAME_SEEDED\n"), nil
	case strings.Contains(command, `commit-tree "HEAD^{tree}"`):
		s.commands = append(s.commands, command)
		if s.refusal != "" {
			return []byte("ZCP_BRANCH_REFUSED:" + s.refusal + "\n"), errors.New("exit status 5")
		}
		return []byte("ok"), nil
	}
	return s.stubSSHWithCommands.ExecSSH(ctx, host, command)
}

// giteaGitPushTool registers zerops_deploy against a fake Gitea for a pair
// whose meta is written by writeMeta, with the Mate's Gitea variables set.
func giteaGitPushTool(t *testing.T, fake *fakeGitea, writeMeta func(stateDir, remote string)) (*mcp.Server, *giteaWiringSSH, string) {
	t.Helper()
	gitea := fake.start(t)
	stateDir := t.TempDir()
	writeMeta(stateDir, gitea.URL+"/acme/appdev")
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
	ssh := &giteaWiringSSH{stubSSHWithCommands: &stubSSHWithCommands{
		tokenOutput: []byte("1"), committedOutput: []byte("1"), pushOutput: []byte("ok"),
	}}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, gitea.Client(), "proj-1", ssh, authInfo, nil,
		runtime.Info{InContainer: true, ProjectID: "proj-1", GiteaURL: gitea.URL},
		stateDir, testDeployEngine(t), nil)
	return srv, ssh, stateDir
}

// unwiredGiteaPairMeta writes a pair whose git-push-setup stamped remote on
// this Mate's Gitea but whose branch step never completed.
func unwiredGiteaPairMeta(t *testing.T, stateDir, remote string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, StageHostname: "appstage",
		BootstrapSession: "test", BootstrappedAt: "2026-09-16",
		GitPushState: topology.GitPushConfigured, RemoteURL: remote, TrackedRef: "main",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// pushedCommand returns the first push the stub saw, "" when none; the
// git-push-setup auth probe's `push --dry-run` sends nothing and is not one.
func pushedCommand(ssh *giteaWiringSSH) string {
	for _, cmd := range ssh.commands {
		if strings.Contains(cmd, " push ") && !strings.Contains(cmd, "push --dry-run") {
			return cmd
		}
	}
	return ""
}

// assertNothingPushed fails when any command the stub saw was a push.
func assertNothingPushed(t *testing.T, ssh *giteaWiringSSH, text string) {
	t.Helper()
	if cmd := pushedCommand(ssh); cmd != "" {
		t.Errorf("nothing may be pushed, but ran:\n%s\nresponse:\n%s", cmd, text)
	}
}

// TestGitPush_AnUnwiredGiteaPairRetriesItsWiringAndNeverPushes is D2's other
// half: a pair whose remote is this Mate's Gitea but which was never recorded
// as wired aimed every git-push at the protected main (test - Gita,
// 2026-09-24), and the agent was then offered a force push over it. The push
// now retries the wiring once, and while it stays incomplete says so and
// pushes nothing.
func TestGitPush_AnUnwiredGiteaPairRetriesItsWiringAndNeverPushes(t *testing.T) {
	fake := newFakeGitea()
	fake.repoStatus = http.StatusBadGateway
	fake.repoBody = `{"error":"down"}`
	srv, ssh, _ := giteaGitPushTool(t, fake, func(stateDir, remote string) {
		unwiredGiteaPairMeta(t, stateDir, remote)
	})

	text := getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
	}))
	if len(fake.repoRequests) != 1 {
		t.Errorf("the push must retry the wiring once, broker asked %d times", len(fake.repoRequests))
	}
	if !strings.Contains(text, "wiring incomplete") {
		t.Errorf("the refusal must say the wiring is incomplete:\n%s", text)
	}
	if strings.Contains(text, "replace-remote") || strings.Contains(text, "--force") {
		t.Errorf("a Gitea pair is never offered a force push:\n%s", text)
	}
	assertNothingPushed(t, ssh, text)
}

// TestGitPush_TheProtectedBaseOfAGiteaRemoteIsRefused: a wired pair asked to
// push its repository's main is refused before git runs, and told which
// branch is its own.
func TestGitPush_TheProtectedBaseOfAGiteaRemoteIsRefused(t *testing.T) {
	fake := newFakeGitea()
	srv, ssh, _ := giteaGitPushTool(t, fake, func(stateDir, remote string) {
		writeWiredGiteaPairMeta(t, stateDir, remote)
	})

	text := getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
		"branch":        "main",
	}))
	if !strings.Contains(text, "protected") || !strings.Contains(text, "mate/mate-p1") {
		t.Errorf("the refusal must name the protected base and the Mate's own branch:\n%s", text)
	}
	if strings.Contains(text, "replace-remote") || strings.Contains(text, "PUSHED") {
		t.Errorf("a push to the base must be refused outright:\n%s", text)
	}
	assertNothingPushed(t, ssh, text)
}

// TestGitPush_ARefusedBranchStepNamesItsRemedyAndThePushAfterItWires: a
// refusal the push guard only repeated left a pair with no way forward —
// every push refused, and nothing a retry could change. The refusal names the
// one thing to do, and the push after it is done wires the pair and pushes.
func TestGitPush_ARefusedBranchStepNamesItsRemedyAndThePushAfterItWires(t *testing.T) {
	fake := newFakeGitea()
	srv, ssh, stateDir := giteaGitPushTool(t, fake, func(stateDir, remote string) {
		unwiredGiteaPairMeta(t, stateDir, remote)
	})
	ssh.refusal = "ZCP_BASE_NOT_SEED"
	push := map[string]any{"targetService": "appdev", "strategy": "git-push"}

	text := getTextContent(t, callTool(t, srv, "zerops_deploy", push))
	var refusal struct {
		Suggestion string `json:"suggestion"`
	}
	if err := json.Unmarshal([]byte(text), &refusal); err != nil {
		t.Fatalf("decode refusal: %v\n%s", err, text)
	}
	if !strings.Contains(refusal.Suggestion, "git merge --allow-unrelated-histories --no-edit FETCH_HEAD") {
		t.Errorf("the refusal's suggestion must be its remedy:\n%s", text)
	}
	if strings.Contains(text, "next pass") {
		t.Errorf("the refusal must not be told to wait for a pass:\n%s", text)
	}
	assertNothingPushed(t, ssh, text)

	ssh.refusal = ""
	text = getTextContent(t, callTool(t, srv, "zerops_deploy", push))
	if meta, _ := workflow.FindServiceMeta(stateDir, "appdev"); meta == nil || meta.Gitea == nil {
		t.Fatalf("the push after the remedy must wire the pair, got %+v:\n%s", meta, text)
	}
	if cmd := pushedCommand(ssh); !strings.Contains(cmd, "mate/mate-p1") {
		t.Errorf("the push after the remedy must go to the Mate's branch, pushed %q:\n%s", cmd, text)
	}
}

// TestGitPush_AGiteaRemoteNamingAnotherRepositoryIsTheUsersAndNeverReachesTheBroker:
// a remote on this Gitea that is not named after the pair is one the user
// chose; the guard does not ask the broker — which would create the pair's
// repository — neither wires over it nor refuses its push.
func TestGitPush_AGiteaRemoteNamingAnotherRepositoryIsTheUsersAndNeverReachesTheBroker(t *testing.T) {
	fake := newFakeGitea()
	var theirs string
	srv, ssh, stateDir := giteaGitPushTool(t, fake, func(stateDir, remote string) {
		theirs = strings.Replace(remote, "/acme/appdev", "/someone/their-app.git", 1)
		unwiredGiteaPairMeta(t, stateDir, theirs)
	})

	text := getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev", "strategy": "git-push",
	}))
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta == nil || meta.Gitea != nil || meta.RemoteURL != theirs {
		t.Fatalf("the user's remote must be left as it was, got %+v", meta)
	}
	if strings.Contains(text, "wiring incomplete") {
		t.Errorf("the user's own remote is not a wiring to complete:\n%s", text)
	}
	if len(fake.repoRequests) != 0 {
		t.Errorf("the broker must not be asked for a repository, asked %d times", len(fake.repoRequests))
	}
	if pushedCommand(ssh) == "" {
		t.Errorf("the push to the user's own remote must run:\n%s", text)
	}
}
