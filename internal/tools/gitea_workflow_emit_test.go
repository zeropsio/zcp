// Tests for: A3 — the workflow that ships a Mate's code to the group's stage
// has to be IN the repository the Mate pushes.
//
// `.gitea/workflows/zerops.yml` was only ever emitted as text by
// `zerops_workflow action="build-integration" integration="actions"`, which
// nothing in A1 or A2 calls. So a real Mate's repository never carried it and
// the runner path never ran (measured 2026-09-16). A1 now writes it into the
// pair's working tree when it wires the repository, before anything is
// pushed.
package tools

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// runContainerCommandsIn replays the commands an SSH stub recorded against a
// real directory, standing in for the pair's working tree: the container's
// /var/www is rewritten to dir so the file the command writes can be read
// back. Commands that are not writes are skipped — this is about what landed
// in the tree, not about re-running git.
func runContainerCommandsIn(t *testing.T, dir string, commands []string) {
	t.Helper()
	for _, cmd := range commands {
		if !strings.Contains(cmd, giteaWorkflowFilePath) {
			continue
		}
		runShellIn(t, strings.Replace(cmd, "cd '"+giteaPairWorkingDir+"'", "cd '"+dir+"'", 1))
	}
}

func runShellIn(t *testing.T, command string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\ncommand: %s\noutput:\n%s", err, command, out)
	}
}

// TestReconcileGiteaRepositories_EmitsTheWorkflow pins A3: after A1 wires the
// repository the workflow file is IN the tree, and it carries no credential
// of any kind — the job asks the account's broker with its own token, which
// is the entire reason this path exists.
func TestReconcileGiteaRepositories_EmitsTheWorkflow(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaPairMeta(t, stateDir)
	fake := newFakeGitea()
	srv := fake.start(t)
	ssh := giteaReconcileSSH()
	client := platform.NewMock().WithServices([]platform.ServiceStack{{
		ID: "svc-appdev", Name: "appdev",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
	}})

	reconcileGiteaRepositories(
		context.Background(), client, srv.Client(), ssh,
		runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir,
		writeLiveEnvFile(t, map[string]string{
			"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
		}),
	)

	tree := t.TempDir()
	runContainerCommandsIn(t, tree, ssh.commands)
	raw, err := os.ReadFile(filepath.Join(tree, giteaWorkflowFilePath))
	if err != nil {
		t.Fatalf("A1 must leave the workflow in the pair's tree: %v\ncommands:\n%s",
			err, strings.Join(ssh.commands, "\n"))
	}
	body := string(raw)

	for _, want := range []string{
		"on:", "push:", "branches: [main]",
		"actions/checkout@v4",
		// The runner has no language runtime: the pair's own is set up at
		// its version before the project's tests (run 5's N2).
		"uses: actions/setup-node@v4",
		`node-version: "22"`,
		"uses: zeropsio/gitea-mate/actions/deploy@v4",
		// D27: the broker starts the same workflow for a release, a new
		// environment and whatever fell behind, and says what for.
		"workflow_dispatch:",
		"ref: ${{ inputs.sha || github.sha }}",
		// A push's job names no environment and no service: it deploys
		// whatever its branch feeds, and the broker knows which service of
		// the group's stage this repository builds (the pair's promoted
		// runtime — measured 2026-09-17, when a workflow naming the dev half
		// was answered unknown_service).
		"environment: ${{ inputs.environment }}",
		"service: ${{ inputs.service }}",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the workflow is missing %q:\n%s", want, body)
		}
	}
	// The whole point of the Gitea track: no repository secret and no Zerops
	// token in the file — the job is handed one environment's key by the
	// broker for one push (MB-14, MB-29).
	for _, forbidden := range []string{"secrets.", "ZEROPS_TOKEN", "actions/deploy@v1"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the workflow must carry no %q:\n%s", forbidden, body)
		}
	}
}

// TestReconcileGiteaRepositories_WorkflowIsIdempotent — the emit is content
// idempotent. A rewrite of identical bytes would show up as a modified file
// in the pair's `git status` and in the deploy's dirty-tree warning, telling
// the Mate it has work to commit that it does not.
func TestReconcileGiteaRepositories_WorkflowIsIdempotent(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaPairMeta(t, stateDir)
	fake := newFakeGitea()
	srv := fake.start(t)
	ssh := giteaReconcileSSH()
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	reconcileGiteaRepositories(
		context.Background(), client, srv.Client(), ssh,
		runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir,
		writeLiveEnvFile(t, map[string]string{
			"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
		}),
	)

	tree := t.TempDir()
	runContainerCommandsIn(t, tree, ssh.commands)
	path := filepath.Join(tree, giteaWorkflowFilePath)
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	stale := first.ModTime().Add(-time.Hour)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	runContainerCommandsIn(t, tree, ssh.commands)
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !after.ModTime().Equal(stale) {
		t.Errorf("an unchanged workflow was rewritten, dirtying the pair's tree")
	}
}

// giteaDeployStepToday is the workflow's deploy step, byte for byte. The
// broker grants a job its environment's key only for this workflow's deploy
// action, so whatever the template learns about a project's runtime, this
// step never moves.
const giteaDeployStepToday = `      - name: Deploy with zcli push
        # The job asks the account's broker, which hands it the environment's
        # deploy token only for the commit protected state wants there, and
        # only to the default branch's workflow. No secret and no Zerops key
        # anywhere in this file or this repository.
        uses: zeropsio/gitea-mate/actions/deploy@v4
        with:
          environment: ${{ inputs.environment }}
          service: ${{ inputs.service }}
`

type giteaWorkflowStepDoc struct {
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

func parseGiteaWorkflowSteps(t *testing.T, body string) []giteaWorkflowStepDoc {
	t.Helper()
	var doc struct {
		Jobs struct {
			Deploy struct {
				Steps []giteaWorkflowStepDoc `yaml:"steps"`
			} `yaml:"deploy"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("the workflow is not YAML: %v\n%s", err, body)
	}
	return doc.Jobs.Deploy.Steps
}

// TestGiteaWorkflowYAML_SetsUpTheServicesRuntime pins run 5's N2: the group's
// runner is a bare Ubuntu with no language runtime, so a Mate that filled the
// Test step in with `npm test` failed both deploys (exit 127) until it was
// told. Where a setup action works on the runner the workflow sets the
// service's runtime up, at its version, before Test. Where none does, the Test
// step says so and shows what works there — the distribution's packages —
// and never points at an action that fails on the runner. The Test step stays
// a no-op either way, and the deploy step never changes.
func TestGiteaWorkflowYAML_SetsUpTheServicesRuntime(t *testing.T) {
	t.Parallel()
	// The setup actions a comment must never offer: each fails on the runner
	// as it stands (no unzip, no /opt/hostedtoolcache, an apt it does not list).
	brokenOnTheRunner := []string{"setup-python", "setup-php", "setup-bun", "setup-deno"}
	// With no specific advice: the general rule — a .tar.gz setup action
	// works, anything else comes from the distribution or its own installer.
	genericExample := []string{
		"no language runtime installed", ".tar.gz", "setup-node, setup-go, setup-java",
		"sudo apt-get", "own installer", "- uses: actions/setup-node@v4", "node-version:",
	}
	genericNote := []string{"no language runtime installed", ".tar.gz", "setup-node, setup-go, setup-java", "sudo apt-get", "own installer"}
	tests := []struct {
		serviceType string
		wantUses    string
		wantWith    map[string]string
		wantComment []string
		wantNote    []string
	}{
		{serviceType: "nodejs@22", wantUses: "actions/setup-node@v4", wantWith: map[string]string{"node-version": "22"}},
		{serviceType: "ubuntu/nodejs@24", wantUses: "actions/setup-node@v4", wantWith: map[string]string{"node-version": "24"}},
		{serviceType: "go@1", wantUses: "actions/setup-go@v5", wantWith: map[string]string{"go-version": "1.x", "cache": "false"}},
		{serviceType: "go@1.22", wantUses: "actions/setup-go@v5", wantWith: map[string]string{"go-version": "1.22.x", "cache": "false"}},
		{serviceType: "java@21", wantUses: "actions/setup-java@v4", wantWith: map[string]string{"distribution": "temurin", "java-version": "21"}},
		// A runtime whose setup action fails on the runner: the
		// distribution's own packages, said to be the distribution's version.
		{serviceType: "python@3.12", wantComment: []string{"no language runtime installed", "apt-get install -y python3", "the distribution's version"}},
		{serviceType: "php-nginx@8.4", wantComment: []string{"no language runtime installed", "apt-get install -y php-cli php-mbstring php-xml php-curl php-zip unzip composer", "the distribution's version"}},
		{serviceType: "php-apache@8.3", wantComment: []string{"apt-get install -y php-cli php-mbstring php-xml php-curl php-zip unzip composer"}},
		{serviceType: "bun@1.2", wantComment: []string{"no language runtime installed", "apt-get install -y unzip", "bun.sh/install"}},
		{serviceType: "deno@2", wantComment: []string{"no language runtime installed", "apt-get install -y unzip", "deno.land/install.sh"}},
		// Nothing specific to say: the generic example.
		{serviceType: "", wantComment: genericExample, wantNote: genericNote},
		{serviceType: "nodejs@latest", wantComment: genericExample, wantNote: genericNote},
		{serviceType: "postgresql@16", wantComment: genericExample, wantNote: genericNote},
	}
	for _, tt := range tests {
		t.Run(tt.serviceType, func(t *testing.T) {
			t.Parallel()
			body := giteaWorkflowYAML(tt.serviceType)
			steps := parseGiteaWorkflowSteps(t, body)
			if len(steps) < 3 || steps[0].Uses != "actions/checkout@v4" {
				t.Fatalf("want checkout, then the project's steps, then the deploy:\n%s", body)
			}

			test := steps[1]
			if tt.wantUses != "" {
				setup := steps[1]
				if setup.Uses != tt.wantUses {
					t.Fatalf("the step after checkout uses %q, want %q:\n%s", setup.Uses, tt.wantUses, body)
				}
				got := map[string]string{}
				for k, v := range setup.With {
					got[k] = fmt.Sprint(v)
				}
				if !maps.Equal(got, tt.wantWith) {
					t.Errorf("the setup step's inputs = %v, want %v", got, tt.wantWith)
				}
				test = steps[2]
			} else {
				for _, s := range steps {
					if strings.Contains(s.Uses, "setup-") {
						t.Errorf("a runtime with no working setup gets no setup step, got %q:\n%s", s.Uses, body)
					}
				}
				comment := giteaWorkflowStep(body, giteaWorkflowTestStep)
				// The comment's prose, as one line: wrapping is not what is pinned.
				prose := strings.Join(strings.Fields(strings.ReplaceAll(comment, "#", " ")), " ")
				for _, want := range tt.wantComment {
					if !strings.Contains(prose, want) {
						t.Errorf("the Test step's comment must say %q:\n%s", want, comment)
					}
				}
				for _, broken := range brokenOnTheRunner {
					if strings.Contains(comment, broken) {
						t.Errorf("the Test step's comment offers %q, which fails on the runner:\n%s", broken, comment)
					}
				}
				note := giteaTestsNote(tt.serviceType)
				for _, want := range tt.wantNote {
					if !strings.Contains(note, want) {
						t.Errorf("the confirm's note must say %q: %s", want, note)
					}
				}
				for _, broken := range brokenOnTheRunner {
					if strings.Contains(note, broken) {
						t.Errorf("the confirm's note offers %q, which fails on the runner: %s", broken, note)
					}
				}
			}
			if test.Name != giteaWorkflowTestStep || test.Run != `echo "no test command configured"` {
				t.Errorf("the Test step stays the no-op, got %+v:\n%s", test, body)
			}
			if got := giteaWorkflowStep(body, "Deploy with zcli push"); got != giteaDeployStepToday {
				t.Errorf("the deploy step moved:\n%s\nwant:\n%s", got, giteaDeployStepToday)
			}
			if last := steps[len(steps)-1]; last.Uses != giteaBrokerDeployAction {
				t.Errorf("the deploy is the last step, got %+v", last)
			}
		})
	}
}

// giteaCurrentFilledInWorkflow is a workflow that already deploys through
// this zcp's action, whose project set its own runtime up and filled its Test
// step in — what a Mate joining an existing group checks out from main.
const giteaCurrentFilledInWorkflow = `name: Zerops deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Set up Node
        uses: actions/setup-node@v4
        with:
          node-version: "20"
      - name: Test
        run: npm ci && npm test
      - name: Deploy with zcli push
        uses: zeropsio/gitea-mate/actions/deploy@v4
`

// giteaWorkflowAsZcpWroteIt is the workflow every zcp from D27 until the
// runtime setup wrote, byte for byte — what every live group's repository
// carries. A copy of its own, so a drift of the frozen one in the code shows.
const giteaWorkflowAsZcpWroteIt = `name: Zerops deploy
on:
  push:
    branches: [main]
  # The account's broker starts this for a release, for a new environment and
  # for whatever falls behind; the inputs say what for.
  workflow_dispatch:
    inputs:
      environment:
        description: The environment to deploy. Empty deploys whatever this branch feeds.
        required: false
        default: ""
      service:
        description: The service of that environment. Empty deploys every one this repository builds.
        required: false
        default: ""
      sha:
        description: The commit to deploy. Empty deploys the branch's head.
        required: false
        default: ""
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ inputs.sha || github.sha }}
      - name: Test
        # Replace with this project's own test command; the deploy step
        # below runs only if this one passes.
        run: echo "no test command configured"
      - name: Deploy with zcli push
        # The job asks the account's broker, which hands it the environment's
        # deploy token only for the commit protected state wants there, and
        # only to the default branch's workflow. No secret and no Zerops key
        # anywhere in this file or this repository.
        uses: zeropsio/gitea-mate/actions/deploy@v4
        with:
          environment: ${{ inputs.environment }}
          service: ${{ inputs.service }}
`

// giteaHandWrittenWorkflow is a project's own workflow that does not deploy
// through the broker at all.
const giteaHandWrittenWorkflow = `name: CI
on: [push]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: make test
`

// TestReconcileGiteaRepositories_WiringWritesTheWorkflowOnlyWhereItIsNotCurrent
// — wiring reads what the pair's checkout already carries before it writes. A
// pair adopted from the group's recipe checks main out, workflow included, and
// the first delivery's `git add -A` would commit any rewrite of it: so a file
// that already deploys through this zcp's action is the project's and is left
// byte-identical, and only a missing or earlier one is written. The pair's
// type is read from the direct service list, which a just-imported stand-up
// is already on; a failed read writes the plain variant.
func TestReconcileGiteaRepositories_WiringWritesTheWorkflowOnlyWhereItIsNotCurrent(t *testing.T) {
	nodeDev := []platform.ServiceStack{{
		ID: "svc-appdev", Name: "appdev",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
	}}
	// The search index has not caught the import's type up yet.
	laggingSearch := []platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}}
	tests := []struct {
		name       string
		existing   string
		readErr    error
		client     *platform.Mock
		wantWrite  bool
		want       []string
		wantNot    []string
		wantReport string
	}{
		{
			name: "a current file is the project's", existing: giteaCurrentFilledInWorkflow,
			client: platform.NewMock().WithServices(laggingSearch).WithServicesDirect(nodeDev),
		},
		{
			name: "a current file is left alone even when the type cannot be read", existing: giteaCurrentFilledInWorkflow,
			client: platform.NewMock().WithServices(laggingSearch).WithError("ListServicesDirect", errors.New("api down")),
		},
		{
			name: "a missing file is written, set up from the direct list", existing: "",
			client:    platform.NewMock().WithServices(laggingSearch).WithServicesDirect(nodeDev),
			wantWrite: true,
			want:      []string{"- name: Set up Node.js\n", "\n        uses: actions/setup-node@v4\n", "\n          node-version: \"22\"\n", "uses: " + giteaBrokerDeployAction},
		},
		{
			name: "a failed read writes the plain variant", existing: "",
			client:    platform.NewMock().WithServices(nodeDev).WithError("ListServicesDirect", errors.New("api down")),
			wantWrite: true,
			want:      []string{"no language runtime installed", "uses: " + giteaBrokerDeployAction},
			wantNot:   []string{"- name: Set up"},
		},
		{
			// Every live group carries it: untouched, it is still zcp's own,
			// and a Mate joining the group gets the setup (run 5's N2).
			name: "the untouched pre-setup file zcp wrote is upgraded", existing: giteaWorkflowAsZcpWroteIt,
			client:    platform.NewMock().WithServices(laggingSearch).WithServicesDirect(nodeDev),
			wantWrite: true,
			want:      []string{"- name: Set up Node.js\n", "\n        uses: actions/setup-node@v4\n", "uses: " + giteaBrokerDeployAction},
		},
		{
			name:     "the pre-setup file with its Test step filled in is the project's",
			existing: strings.Replace(giteaWorkflowAsZcpWroteIt, `run: echo "no test command configured"`, `run: echo "no test command configurex"`, 1),
			client:   platform.NewMock().WithServices(laggingSearch).WithServicesDirect(nodeDev),
		},
		{
			name: "the pre-setup file with a setup step added is the project's",
			existing: strings.Replace(giteaWorkflowAsZcpWroteIt, "      - name: Test\n",
				"      - uses: actions/setup-node@v4\n      - name: Test\n", 1),
			client: platform.NewMock().WithServices(laggingSearch).WithServicesDirect(nodeDev),
		},
		{
			name: "a hand-written workflow that does not deploy through the broker is the project's", existing: giteaHandWrittenWorkflow,
			client: platform.NewMock().WithServices(laggingSearch).WithServicesDirect(nodeDev),
		},
		{
			name: "a file that cannot be read is not written", readErr: errors.New("connection reset"),
			client:     platform.NewMock().WithServices(laggingSearch).WithServicesDirect(nodeDev),
			wantReport: "could not be read",
		},
		{
			name: "an earlier zcp's file is replaced, its Test step kept", existing: oldGiteaWorkflow,
			client:    platform.NewMock().WithServices(laggingSearch).WithServicesDirect(nodeDev),
			wantWrite: true,
			want:      []string{"- name: Set up Node.js\n", "# The project's own.", "uses: " + giteaBrokerDeployAction},
			wantNot:   []string{"actions/deploy@v1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			writeGiteaPairMeta(t, stateDir)
			fake := newFakeGitea()
			srv := fake.start(t)
			ssh := giteaReconcileSSH()
			healthy := ssh.dispatch
			ssh.dispatch = func(cmd string) ([]byte, error) {
				if strings.Contains(cmd, "cat ") && strings.Contains(cmd, giteaWorkflowFilePath) {
					return []byte(tt.existing), tt.readErr
				}
				return healthy(cmd)
			}

			report := reconcileGiteaRepositories(
				context.Background(), tt.client, srv.Client(), ssh,
				runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir,
				writeLiveEnvFile(t, map[string]string{
					"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
				}),
			)

			written := ""
			for _, cmd := range ssh.commands {
				if strings.Contains(cmd, "base64 -d") && strings.Contains(cmd, giteaWorkflowFilePath) {
					written = decodeWrittenFile(t, cmd)
				}
			}
			if (written != "") != tt.wantWrite {
				t.Fatalf("workflow written = %v, want %v:\n%s", written != "", tt.wantWrite, written)
			}
			// The type is read only to write: a file left alone costs no list.
			if n := tt.client.CallCounts["ListServicesDirect"]; !tt.wantWrite && n != 0 {
				t.Errorf("no workflow was written, yet the services were listed directly %d times", n)
			}
			if tt.wantReport != "" && !strings.Contains(strings.Join(report, "\n"), tt.wantReport) {
				t.Errorf("the pass's report misses %q:\n%s", tt.wantReport, strings.Join(report, "\n"))
			}
			for _, want := range tt.want {
				if !strings.Contains(written, want) {
					t.Errorf("the workflow written misses %q:\n%s", want, written)
				}
			}
			for _, not := range tt.wantNot {
				if strings.Contains(written, not) {
					t.Errorf("the workflow written carries %q:\n%s", not, written)
				}
			}
		})
	}
}
