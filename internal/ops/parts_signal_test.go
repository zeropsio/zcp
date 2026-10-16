package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

type partsSSH struct{ out string }

func (p partsSSH) ExecSSH(_ context.Context, _, command string) ([]byte, error) {
	if !strings.Contains(command, "@@yaml") {
		return nil, nil
	}
	return []byte(p.out), nil
}

func (p partsSSH) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// TestReadPartsSignal: a repo is multi-part when its zerops.yaml declares
// more than one part (setups other than the dev setup and a base only
// `extends` names), its declaration lists more than one dev server, or
// workspace markers name several deployable packages — and only then.
func TestReadPartsSignal(t *testing.T) {
	t.Parallel()
	const singlePair = "zerops:\n  - setup: dev\n    run: {start: zsc noop}\n  - setup: prod\n    run: {start: bun index.ts}\n"
	tests := []struct {
		name, out, devSetup string
		want                bool
	}{
		{name: "a dev setup and one part", out: "@@yaml\n" + singlePair + "@@declaration\n@@markers\n@@packages\n"},
		{name: "a dev setup and three parts", want: true, out: "@@yaml\nzerops:\n  - setup: dev\n  - setup: web\n  - setup: api\n  - setup: worker\n@@declaration\n@@markers\n@@packages\n"},
		{name: "a base only extends names is not a part", out: "@@yaml\nzerops:\n  - setup: base\n  - setup: dev\n    extends: base\n  - setup: prod\n    extends: [base]\n@@declaration\n@@markers\n@@packages\n"},
		{name: "the dev setup the meta names", devSetup: "appdev", out: "@@yaml\nzerops:\n  - setup: appdev\n  - setup: prod\n@@declaration\n@@markers\n@@packages\n"},
		{name: "two declared dev servers", want: true, out: "@@yaml\n" + singlePair + "@@declaration\n{\"version\":1,\"servers\":[{\"name\":\"web\"},{\"name\":\"api\"}]}\n@@markers\n@@packages\n"},
		{name: "one declared dev server", out: "@@yaml\n" + singlePair + "@@declaration\n{\"version\":1,\"servers\":[{\"name\":\"appdev\"}]}\n@@markers\n@@packages\n"},
		{name: "a workspace with two deployable packages", want: true, out: "@@yaml\n@@declaration\n@@markers\npnpm-workspace.yaml\n@@packages\napps/web/package.json\napps/api/package.json\n"},
		{name: "a workspace with one app and libraries", out: "@@yaml\n@@declaration\n@@markers\nturbo.json\n@@packages\napps/web/package.json\n"},
		{name: "two packages without a workspace marker", out: "@@yaml\n@@declaration\n@@markers\n@@packages\nweb/package.json\napi/go.mod\n"},
		{name: "no repo", out: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sig, err := ReadPartsSignal(context.Background(), partsSSH{out: tt.out}, "appdev", tt.devSetup)
			if err != nil {
				t.Fatalf("ReadPartsSignal: %v", err)
			}
			if got := sig.MultiPart(); got != tt.want {
				t.Errorf("MultiPart = %v, want %v (%+v)", got, tt.want, sig)
			}
		})
	}
}

// TestReadPartsSignal_DeclaresBase: the repo read says the zerops.yaml
// already declares a base when any setup names a build.base or a run.base —
// a scalar or a list — and not when the file is absent or names none.
func TestReadPartsSignal_DeclaresBase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, yaml string
		want       bool
	}{
		{name: "bare bases on a shared base setup", want: true, yaml: "zerops:\n  - setup: base\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n  - setup: dev\n    extends: base\n"},
		{name: "a build base list", want: true, yaml: "zerops:\n  - setup: dev\n    build:\n      base: [php@8.4, nodejs@22]\n"},
		{name: "a run base only", want: true, yaml: "zerops:\n  - setup: dev\n    run:\n      base: ubuntu/nodejs@22\n"},
		{name: "setups without a base", yaml: "zerops:\n  - setup: dev\n    run:\n      start: zsc noop\n"},
		{name: "no zerops.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := "@@yaml\n" + tt.yaml + "@@declaration\n@@markers\n@@packages\n"
			sig, err := ReadPartsSignal(context.Background(), partsSSH{out: out}, "appdev", "")
			if err != nil {
				t.Fatalf("ReadPartsSignal: %v", err)
			}
			if sig.DeclaresBase != tt.want {
				t.Errorf("DeclaresBase = %v, want %v", sig.DeclaresBase, tt.want)
			}
		})
	}
}

// TestReadPartsSignal_RunParts: a part runs a process when its setup, or a
// setup it extends, names a run.start or run.startCommands; a static part
// that names neither runs none.
func TestReadPartsSignal_RunParts(t *testing.T) {
	t.Parallel()
	yamlDoc := `zerops:
  - setup: base
    run:
      start: node server.js
  - setup: dev
    run:
      start: zsc noop
  - setup: web
    extends: base
  - setup: api
    run:
      start: bun api.ts
  - setup: worker
    run:
      startCommands:
        - command: node worker.js
          name: worker
  - setup: docs
    build:
      deployFiles: dist
`
	sig, err := ReadPartsSignal(context.Background(), partsSSH{out: "@@yaml\n" + yamlDoc + "@@declaration\n@@markers\n@@packages\n"}, "appdev", "")
	if err != nil {
		t.Fatalf("ReadPartsSignal: %v", err)
	}
	if want := []string{"web", "api", "worker", "docs"}; !reflect.DeepEqual(sig.YAMLParts, want) {
		t.Errorf("YAMLParts = %v, want %v", sig.YAMLParts, want)
	}
	if want := []string{"web", "api", "worker"}; !reflect.DeepEqual(sig.RunParts, want) {
		t.Errorf("RunParts = %v, want %v", sig.RunParts, want)
	}
}

// TestPartsSignalScript_ReadsARealRepo runs the script itself in a shell over
// a repo on disk: three parts in zerops.yaml, two declared dev servers, a
// pnpm workspace with two apps that start, a library without a start script
// and a dependency under node_modules that is never counted.
func TestPartsSignalScript_ReadsARealRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := map[string]string{
		"zerops.yaml":                        "zerops:\n  - setup: dev\n  - setup: web\n  - setup: api\n  - setup: worker\n",
		".zcp/dev-servers.json":              `{"version":1,"servers":[{"name":"web","port":3000,"command":"pnpm dev"},{"name":"api","port":8080,"command":"pnpm start"}]}`,
		"pnpm-workspace.yaml":                "packages: ['apps/*', 'packages/*']\n",
		"apps/web/package.json":              `{"scripts": {"dev": "vite"}}`,
		"apps/api/package.json":              `{"scripts": {"start": "node main.js"}}`,
		"packages/lib/package.json":          `{"name": "lib"}`,
		"node_modules/dep/package.json":      `{"scripts": {"start": "x"}}`,
		"apps/web/node_modules/package.json": `{"scripts": {"start": "x"}}`,
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := strings.Replace(partsSignalScript, "cd "+defaultWorkingDir, "cd "+dir, 1)
	out, err := exec.CommandContext(context.Background(), "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}
	sig := parsePartsSignal(string(out), "appdev", "dev")
	want := PartsSignal{ConfigFile: "zerops.yaml", YAMLParts: []string{"web", "api", "worker"}, YAMLSetups: []string{"dev", "web", "api", "worker"}, DeclaredServers: []string{"web", "api"}, Workspace: true, Packages: 2}
	if !reflect.DeepEqual(sig, want) {
		t.Errorf("signal = %+v, want %+v\n%s", sig, want, out)
	}
}

// TestPartsSignalScript_NamesTheConfigFileTheRepoKeeps: the read names the
// root config file by the name the repo gave it — zerops.yml stays
// zerops.yml — and a repo without one names none.
func TestPartsSignalScript_NamesTheConfigFileTheRepoKeeps(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"zerops.yml", "zerops.yaml", ""} {
		dir := t.TempDir()
		if file != "" {
			if err := os.WriteFile(filepath.Join(dir, file), []byte("zerops:\n  - setup: dev\n  - setup: web\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		script := strings.Replace(partsSignalScript, "cd "+defaultWorkingDir, "cd "+dir, 1)
		out, err := exec.CommandContext(context.Background(), "sh", "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("script: %v\n%s", err, out)
		}
		if sig := parsePartsSignal(string(out), "appdev", "dev"); sig.ConfigFile != file {
			t.Errorf("repo with %q: ConfigFile = %q", file, sig.ConfigFile)
		}
	}
}

// TestPartsWithoutDevServer: a pair whose stages build several parts of the
// repo names each part that runs a process (its setup, or a base it extends,
// has a run.start) and has no dev server declared on the dev service. The
// pair's first part is served by the server named after the dev hostname
// (the one a start without a name declares); a part without run.start (a
// static output) and a pair with one stage name nothing; a server named after
// no part may serve one, so as many unnamed parts stay unnamed.
func TestPartsWithoutDevServer(t *testing.T) {
	t.Parallel()
	const appAndWorker = "zerops:\n  - setup: dev\n    run: {start: zcp-devsup run}\n  - setup: prod\n    run: {start: bun index.ts}\n  - setup: worker\n    run: {start: bun worker.ts}\n"
	twoStages := []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}, {Hostname: "workerstage", SetupName: "worker"}}
	tests := []struct {
		name, yaml, declaration string
		stages                  []topology.StageRef
		want                    []string
	}{
		{name: "a worker added beside the app with no dev server is named", yaml: appAndWorker,
			declaration: `{"version":1,"servers":[{"name":"appdev","port":3000,"command":"bun --watch index.ts"}]}`,
			stages:      twoStages, want: []string{"worker"}},
		{name: "a repo with no declared server names every part that runs", yaml: appAndWorker,
			stages: twoStages, want: []string{"prod", "worker"}},
		{name: "every part with a dev server of its name names nothing", yaml: appAndWorker,
			declaration: `{"version":1,"servers":[{"name":"prod","command":"a"},{"name":"worker","command":"b"}]}`,
			stages:      twoStages},
		{name: "the app's server under the dev hostname and the worker's under its own name leave nothing unnamed", yaml: appAndWorker,
			declaration: `{"version":1,"servers":[{"name":"appdev","command":"a"},{"name":"worker","command":"b"}]}`,
			stages:      twoStages},
		{name: "a server named after no part may serve the part it leaves unnamed", yaml: appAndWorker,
			declaration: `{"version":1,"servers":[{"name":"appdev","command":"a"},{"name":"jobs","command":"b"}]}`,
			stages:      twoStages},
		{name: "a static part without run.start is not named",
			yaml:        "zerops:\n  - setup: dev\n  - setup: api\n    run: {start: ./app}\n  - setup: web\n    run: {base: static}\n",
			declaration: `{"version":1,"servers":[{"name":"appdev","command":"a"}]}`,
			stages:      []topology.StageRef{{Hostname: "apistage", SetupName: "api"}, {Hostname: "webstage", SetupName: "web"}}},
		{name: "a part whose start comes from the base it extends is named",
			yaml:        "zerops:\n  - setup: base\n    run: {start: node main.js}\n  - setup: dev\n  - setup: api\n    run: {start: ./app}\n  - setup: jobs\n    extends: base\n",
			declaration: `{"version":1,"servers":[{"name":"appdev","command":"a"}]}`,
			stages:      []topology.StageRef{{Hostname: "apistage", SetupName: "api"}, {Hostname: "jobsstage", SetupName: "jobs"}}, want: []string{"jobs"}},
		{name: "a pair with one stage names nothing", yaml: appAndWorker,
			stages: twoStages[:1]},
		{name: "a stage whose setup the file does not declare yet is not named", yaml: appAndWorker,
			declaration: `{"version":1,"servers":[{"name":"appdev","command":"a"}]}`,
			stages:      []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}, {Hostname: "apistage", SetupName: "api"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := "@@yaml\n" + tt.yaml + "@@declaration\n" + tt.declaration + "\n@@markers\n@@packages\n"
			sig, err := ReadPartsSignal(context.Background(), partsSSH{out: out}, "appdev", "dev")
			if err != nil {
				t.Fatalf("ReadPartsSignal: %v", err)
			}
			if got := sig.PartsWithoutDevServer(tt.stages, "appdev"); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PartsWithoutDevServer = %q, want %q", got, tt.want)
			}
		})
	}
}
