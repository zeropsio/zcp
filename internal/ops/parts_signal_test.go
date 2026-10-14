package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	want := PartsSignal{YAMLParts: 3, DeclaredServers: 2, Workspace: true, Packages: 2}
	if sig != want {
		t.Errorf("signal = %+v, want %+v\n%s", sig, want, out)
	}
}
