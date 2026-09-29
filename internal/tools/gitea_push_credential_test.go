// Tests for: tools/gitea_push_credential.go — a wired pair's push credential
// kept at this Mate's current Gitea token. The broker rotates the bot token
// (a new generation as GITEA_TOKEN, the older ones revoked ten minutes later),
// while the push source holds a copy git-push-setup wrote as GIT_TOKEN; before
// this, nothing moved the copy, and every push after a rotation was refused.
package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	credentialGiteaURL   = "https://gitea.example.invalid"
	credentialRemote     = credentialGiteaURL + "/acme/appdev.git"
	rotatedAwayGitToken  = "an-older-generation-of-the-bot-token"
	credentialPushSource = "svc-appdev"
)

// narrowSessionProbe makes the fresh-session probe answer at once. Package
// globals: tests that call it are not parallel.
func narrowSessionProbe(t *testing.T) {
	t.Helper()
	prevAttempts, prevDelay := gitPushSessionAuthAttempts, gitPushSessionAuthDelay
	gitPushSessionAuthAttempts, gitPushSessionAuthDelay = 2, 0
	t.Cleanup(func() { gitPushSessionAuthAttempts, gitPushSessionAuthDelay = prevAttempts, prevDelay })
}

// credentialMock is the platform holding the push source, and on it the copy
// of the token git-push-setup wrote ("" for none).
func credentialMock(held string) *platform.Mock {
	mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: credentialPushSource, Name: "appdev"}})
	if held != "" {
		mock = mock.WithServiceEnv(credentialPushSource, []platform.ServiceEnvVar{
			{ID: "ud-git-token", Key: ops.GitTokenEnvKey, Content: held, Sensitive: true},
		})
	}
	return mock
}

// heldGitToken is what the push source holds now.
func heldGitToken(t *testing.T, mock *platform.Mock) string {
	t.Helper()
	envs, err := mock.GetServiceEnv(context.Background(), credentialPushSource)
	if err != nil {
		t.Fatalf("GetServiceEnv: %v", err)
	}
	for _, env := range envs {
		if env.Key == ops.GitTokenEnvKey {
			return env.Content
		}
	}
	return ""
}

func probeCount(commands []string) int {
	n := 0
	for _, cmd := range commands {
		if strings.Contains(cmd, "ls-remote") {
			n++
		}
	}
	return n
}

// TestGiteaEnsurePushCredential is the step's table: a copy that is the live
// token costs nothing; a rotated or missing one is rewritten and proven with a
// fresh session before anything pushes; a pair an earlier refusal marked is
// healed the moment its credential works again; and a credential that still
// does not work marks the pair and says so, never silently.
func TestGiteaEnsurePushCredential(t *testing.T) {
	narrowSessionProbe(t)

	tests := []struct {
		name       string
		held       string
		state      topology.GitPushState
		remote     string
		unwired    bool
		probeFails bool
		writeFails bool

		wantHeld   string
		wantWrites int
		wantProbes int
		wantState  topology.GitPushState
		wantErr    string
	}{
		{
			name: "the copy is the current token", held: giteaBotToken, state: topology.GitPushConfigured,
			wantHeld: giteaBotToken, wantState: topology.GitPushConfigured,
		},
		{
			name: "the broker rotated the token", held: rotatedAwayGitToken, state: topology.GitPushConfigured,
			wantHeld: giteaBotToken, wantWrites: 1, wantProbes: 1, wantState: topology.GitPushConfigured,
		},
		{
			name: "the push source holds no copy", state: topology.GitPushConfigured,
			wantHeld: giteaBotToken, wantWrites: 1, wantProbes: 1, wantState: topology.GitPushConfigured,
		},
		{
			name: "a pair an earlier refusal marked heals once its credential works", held: giteaBotToken, state: topology.GitPushBroken,
			wantHeld: giteaBotToken, wantProbes: 1, wantState: topology.GitPushConfigured,
		},
		{
			name: "a marked pair still refused stays marked and says so", held: giteaBotToken, state: topology.GitPushBroken,
			probeFails: true,
			wantHeld:   giteaBotToken, wantProbes: 1, wantState: topology.GitPushBroken,
			wantErr: "refuses this Mate's current token",
		},
		{
			name: "the current token does not authenticate either", held: rotatedAwayGitToken, state: topology.GitPushConfigured,
			probeFails: true,
			wantHeld:   giteaBotToken, wantWrites: 1, wantProbes: gitPushSessionAuthAttempts, wantState: topology.GitPushBroken,
			wantErr: "refuses this Mate's current token",
		},
		{
			name: "the platform refuses the write", held: rotatedAwayGitToken, state: topology.GitPushConfigured,
			writeFails: true,
			wantHeld:   "", wantState: topology.GitPushBroken,
			wantErr: "writing this Mate's current Gitea token onto appdev failed",
		},
		{
			name: "a remote of the user's own is theirs", held: rotatedAwayGitToken, state: topology.GitPushConfigured,
			remote:   "https://code.example.invalid/someone/appdev.git",
			wantHeld: rotatedAwayGitToken, wantState: topology.GitPushConfigured,
		},
		{
			name: "no Gitea wiring on this Mate", held: rotatedAwayGitToken, state: topology.GitPushConfigured, unwired: true,
			wantHeld: rotatedAwayGitToken, wantState: topology.GitPushConfigured,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			remote := tt.remote
			if remote == "" {
				remote = credentialRemote
			}
			if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeStandard, StageHostname: "appstage",
				BootstrapSession: "test", BootstrappedAt: "2026-09-29",
				GitPushState: tt.state, RemoteURL: remote,
				Gitea: &workflow.GiteaRepoRef{FullName: "acme/appdev", Branch: "mate/mate-p1", DefaultBranch: "main"},
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			mock := credentialMock(tt.held)
			if tt.writeFails {
				mock = mock.WithError("CreateServiceEnvVar", errors.New("platform said no"))
			}
			ssh := &containerSSHStub{}
			if tt.probeFails {
				ssh.errOn = map[string]error{"ls-remote": &platform.SSHExecError{
					Hostname: "appdev", Output: "fatal: Authentication failed for '" + credentialRemote + "'", Err: errors.New("exit status 128"),
				}}
			}
			wiring := ops.GiteaWiring{GiteaURL: credentialGiteaURL, BrokerURL: credentialGiteaURL, Token: giteaBotToken}
			if tt.unwired {
				wiring = ops.GiteaWiring{}
			}
			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")

			err := giteaEnsurePushCredential(context.Background(), mock, ssh, "proj-1", stateDir, wiring, meta)

			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want one saying %q", err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), giteaBotToken) {
				t.Errorf("the error carries the token: %v", err)
			}
			if got := heldGitToken(t, mock); got != tt.wantHeld {
				t.Errorf("the push source holds %q, want %q", got, tt.wantHeld)
			}
			if got := mock.CallCounts["CreateServiceEnvVar"]; got != tt.wantWrites && !tt.writeFails {
				t.Errorf("GIT_TOKEN writes = %d, want %d", got, tt.wantWrites)
			}
			if got := probeCount(ssh.commands); got != tt.wantProbes {
				t.Errorf("fresh-session probes = %d, want %d (commands %q)", got, tt.wantProbes, ssh.commands)
			}
			onDisk, _ := workflow.FindServiceMeta(stateDir, "appdev")
			if onDisk.GitPushState != tt.wantState || meta.GitPushState != tt.wantState {
				t.Errorf("state on disk %q, in memory %q, want %q", onDisk.GitPushState, meta.GitPushState, tt.wantState)
			}
			assertNoTokenOnDisk(t, stateDir)
		})
	}
}

// TestADeliveryBringsItsCredentialToTheCurrentTokenFirst: after a rotation,
// the stage deploy writes the current token onto the push source and proves it
// before the push — the first delivery after a rotation is delivered, not
// refused.
func TestADeliveryBringsItsCredentialToTheCurrentTokenFirst(t *testing.T) {
	narrowSessionProbe(t)
	fake := newFakeGitea()
	gitea := fake.start(t)
	stateDir := t.TempDir()
	writeWiredGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	mock := credentialMock(rotatedAwayGitToken)
	ssh := &containerSSHStub{}
	delivery := deliverGiteaPair(context.Background(), mock, gitea.Client(), ssh,
		runtime.Info{InContainer: true, ProjectID: "proj-1"}, stateDir, "appstage")
	if delivery == nil || delivery.PullRequest == nil {
		t.Fatalf("want a delivery with its pull request, got %+v", delivery)
	}
	if got := heldGitToken(t, mock); got != giteaBotToken {
		t.Errorf("the push source holds %q, want the current token", got)
	}
	probe, push := -1, -1
	for i, cmd := range ssh.commands {
		switch {
		case strings.Contains(cmd, "ls-remote") && probe < 0:
			probe = i
		case strings.Contains(cmd, "push") && strings.Contains(cmd, "mate/mate-p1") && push < 0:
			push = i
		}
	}
	if probe < 0 || push < 0 || probe > push {
		t.Errorf("the credential must be proven before the push (probe at %d, push at %d): %q", probe, push, ssh.commands)
	}
}

// TestADeliveryRefusedForItsCredentialMarksThePair: a push Gitea refuses for
// its credential marks the pair the way a plain git-push does, and the line
// says so — never a generic failure that leaves the pair looking configured.
// The next stage deploy of the marked pair checks the credential again and
// delivers once it works.
func TestADeliveryRefusedForItsCredentialMarksThePair(t *testing.T) {
	narrowSessionProbe(t)
	fake := newFakeGitea()
	gitea := fake.start(t)
	stateDir := t.TempDir()
	writeWiredGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)
	rt := runtime.Info{InContainer: true, ProjectID: "proj-1"}

	refused := &platform.SSHExecError{
		Hostname: "appdev",
		Output:   "remote: Invalid username or token.\nfatal: Authentication failed for '" + gitea.URL + "/acme/appdev.git/'",
		Err:      errors.New("exit status 128"),
	}
	ssh := &containerSSHStub{errOn: map[string]error{"push": refused}}
	delivery := deliverGiteaPair(context.Background(), credentialMock(giteaBotToken), gitea.Client(), ssh, rt, stateDir, "appstage")
	if delivery == nil {
		t.Fatal("want a delivery line")
	}
	for _, want := range []string{"has not reached acme/appdev", "refused", "credential", "next stage deploy"} {
		if !strings.Contains(delivery.Line, want) {
			t.Errorf("the line misses %q:\n%s", want, delivery.Line)
		}
	}
	if meta, _ := workflow.FindServiceMeta(stateDir, "appdev"); meta.GitPushState != topology.GitPushBroken {
		t.Fatalf("a refused credential must mark the pair, got %q", meta.GitPushState)
	}

	// The credential works again: the next stage deploy heals the pair and
	// delivers.
	healed := &containerSSHStub{}
	again := deliverGiteaPair(context.Background(), credentialMock(giteaBotToken), gitea.Client(), healed, rt, stateDir, "appstage")
	if again == nil || again.PullRequest == nil {
		t.Fatalf("the healed pair must deliver, got %+v", again)
	}
	if meta, _ := workflow.FindServiceMeta(stateDir, "appdev"); meta.GitPushState != topology.GitPushConfigured {
		t.Errorf("a credential that works again must heal the pair, got %q", meta.GitPushState)
	}
}

// TestGitPushDeploy_BringsItsCredentialToTheCurrentTokenFirst: a git-push of
// a wired pair to this Mate's Gitea checks the push credential before
// anything else — a rotated copy is rewritten and proven before the push, a
// pair an earlier refusal marked is healed rather than refused as "not
// configured", and one still refused does not push and says why.
func TestGitPushDeploy_BringsItsCredentialToTheCurrentTokenFirst(t *testing.T) {
	narrowSessionProbe(t)
	tests := []struct {
		name       string
		held       string
		state      topology.GitPushState
		probeFails bool

		wantPushed bool
		wantState  topology.GitPushState
		wantText   []string
	}{
		{
			name: "a rotated copy is rewritten before the push", held: rotatedAwayGitToken, state: topology.GitPushConfigured,
			wantPushed: true, wantState: topology.GitPushConfigured, wantText: []string{`"pullRequest"`},
		},
		{
			name: "a marked pair whose credential works again is healed and pushes", held: giteaBotToken, state: topology.GitPushBroken,
			wantPushed: true, wantState: topology.GitPushConfigured, wantText: []string{`"pullRequest"`},
		},
		{
			name: "a marked pair still refused does not push", held: giteaBotToken, state: topology.GitPushBroken, probeFails: true,
			wantState: topology.GitPushBroken,
			wantText:  []string{"PREREQUISITE_MISSING", "did not run", "refuses this Mate's current token", "the broker's to deliver"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeGitea()
			gitea := fake.start(t)
			stateDir := t.TempDir()
			writeWiredGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
			if err := workflow.UpdateServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta) error {
				m.GitPushState = tt.state
				return nil
			}); err != nil {
				t.Fatalf("UpdateServiceMeta: %v", err)
			}
			t.Setenv("GITEA_URL", gitea.URL)
			t.Setenv("MATE_BROKER_URL", gitea.URL)
			t.Setenv("GITEA_TOKEN", giteaBotToken)

			mock := credentialMock(tt.held)
			ssh := &stubSSHWithCommands{tokenOutput: []byte("1"), committedOutput: []byte("1")}
			if tt.probeFails {
				ssh.probeErr = errors.New("exit status 128")
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDeploySSH(srv, mock, gitea.Client(), "proj-1", ssh, &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}, nil,
				runtime.Info{InContainer: true, ProjectID: "proj-1", GiteaURL: gitea.URL},
				stateDir, testDeployEngine(t), nil)

			text := getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{
				"targetService": "appdev",
				"strategy":      "git-push",
			}))
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("the answer misses %q:\n%s", want, text)
				}
			}
			if (ssh.pushCalls > 0) != tt.wantPushed {
				t.Errorf("pushed = %v, want %v:\n%s", ssh.pushCalls > 0, tt.wantPushed, text)
			}
			if got := heldGitToken(t, mock); got != giteaBotToken {
				t.Errorf("the push source holds %q, want the current token", got)
			}
			if ssh.probeCalls == 0 && tt.held != giteaBotToken {
				t.Error("a rewritten credential must be proven with a fresh session before the push")
			}
			if meta, _ := workflow.FindServiceMeta(stateDir, "appdev"); meta.GitPushState != tt.wantState {
				t.Errorf("state = %q, want %q", meta.GitPushState, tt.wantState)
			}
			assertNoTokenOnDisk(t, stateDir)
		})
	}
}
