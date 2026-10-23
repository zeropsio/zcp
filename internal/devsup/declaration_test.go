package devsup

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseDeclaration_Shapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    []Server
		wantErr string
	}{
		{
			name: "single part",
			in:   `{"version":1,"servers":[{"name":"appdev","port":3000,"command":"bun --watch index.ts","cwd":"."}]}`,
			want: []Server{{Name: "appdev", Port: 3000, Command: "bun --watch index.ts", Cwd: "."}},
		},
		{
			name: "named servers keep their order and cwd defaults to the repo root",
			in:   `{"version":1,"servers":[{"name":"web","port":3000,"command":"pnpm --filter web dev","cwd":"apps/web/"},{"name":"api","port":8080,"command":"go run ."}]}`,
			want: []Server{
				{Name: "web", Port: 3000, Command: "pnpm --filter web dev", Cwd: "apps/web"},
				{Name: "api", Port: 8080, Command: "go run .", Cwd: "."},
			},
		},
		{
			name: "a worker without a port, a watched server and a variant B service",
			in:   `{"version":1,"servers":[{"name":"worker","command":"node worker.mjs"},{"name":"api","port":8080,"command":"go build -o app . && exec ./app","cwd":"apps/api","watch":{"paths":["apps/api/"],"ignore":["apps/api/app"]},"service":"godev"}]}`,
			want: []Server{
				{Name: "worker", Command: "node worker.mjs", Cwd: "."},
				{Name: "api", Port: 8080, Command: "go build -o app . && exec ./app", Cwd: "apps/api", Watch: &Watch{Paths: []string{"apps/api"}, Ignore: []string{"apps/api/app"}}, Service: "godev"},
			},
		},
		{
			name: "unknown keys are ignored within a format version",
			in:   `{"version":1,"servers":[{"name":"app","port":3000,"command":"x","later":true}],"later":1}`,
			want: []Server{{Name: "app", Port: 3000, Command: "x", Cwd: "."}},
		},
		{
			name: "an API part that 404s on / declares the path its health probe reads",
			in:   `{"version":1,"servers":[{"name":"api","port":8080,"command":"x","healthPath":"/actuator/health"}]}`,
			want: []Server{{Name: "api", Port: 8080, Command: "x", Cwd: ".", HealthPath: "/actuator/health"}},
		},
		{name: "a health path that is not absolute", in: `{"version":1,"servers":[{"name":"api","port":8080,"command":"x","healthPath":"health"}]}`, wantErr: `"api": healthPath "health" must start with /`},
		{name: "no servers", in: `{"version":1,"servers":[]}`, want: []Server{}},
		{name: "missing version", in: `{"servers":[{"name":"app","command":"x"}]}`, wantErr: "version is missing"},
		{name: "newer format with another shape", in: `{"version":2,"servers":{"app":{}}}`, wantErr: "format 2 is not one this supervisor runs ([1])"},
		{name: "not json", in: `{`, wantErr: "parse"},
		{name: "empty command", in: `{"version":1,"servers":[{"name":"app","port":3000}]}`, wantErr: `"app": command is empty`},
		{name: "name with a capital", in: `{"version":1,"servers":[{"name":"App","command":"x"}]}`, wantErr: `"App": name must match`},
		{name: "name starting with a digit", in: `{"version":1,"servers":[{"name":"1app","command":"x"}]}`, wantErr: "name must match"},
		{name: "name over 40 characters", in: `{"version":1,"servers":[{"name":"` + strings.Repeat("a", 41) + `","command":"x"}]}`, wantErr: "name must match"},
		{name: "a name twice", in: `{"version":1,"servers":[{"name":"app","command":"x"},{"name":"app","command":"y"}]}`, wantErr: `"app" is declared twice`},
		{name: "cwd escapes the repo", in: `{"version":1,"servers":[{"name":"app","command":"x","cwd":"apps/../../other"}]}`, wantErr: "cwd: must stay inside the repo"},
		{name: "absolute cwd", in: `{"version":1,"servers":[{"name":"app","command":"x","cwd":"/etc"}]}`, wantErr: "cwd: must be relative"},
		{name: "port out of range", in: `{"version":1,"servers":[{"name":"app","command":"x","port":70000}]}`, wantErr: "port 70000 is out of range"},
		{name: "two servers on one port", in: `{"version":1,"servers":[{"name":"a","command":"x","port":3000},{"name":"b","command":"y","port":3000}]}`, wantErr: `"a" and "b" both listen on port 3000`},
		{name: "watch without paths", in: `{"version":1,"servers":[{"name":"a","command":"x","watch":{"paths":[]}}]}`, wantErr: "watch has no paths"},
		{name: "watch path outside the repo", in: `{"version":1,"servers":[{"name":"a","command":"x","watch":{"paths":["../x"]}}]}`, wantErr: "watch path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseDeclaration([]byte(tt.in))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got.Servers, tt.want) {
				t.Fatalf("servers = %+v, want %+v", got.Servers, tt.want)
			}
		})
	}
}

func TestParseDeclaration_AnotherFormatIsUnsupported(t *testing.T) {
	t.Parallel()
	_, err := ParseDeclaration([]byte(`{"version":2,"servers":[]}`))
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
}

// The shared contract fixture (docs/spec-mate.md §2.8a.7) is what the
// supervisor reads.
func TestParseDeclaration_ContractFixture(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../tools/testdata/mate-contract/dev-servers.declaration.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseDeclaration(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []Server{
		{Name: "web", Port: 3000, Command: "bun --watch index.ts", Cwd: "apps/web"},
		{Name: "api", Port: 8080, Command: "go build -o app . && exec ./app", Cwd: "apps/api", Watch: &Watch{Paths: []string{"apps/api"}, Ignore: []string{"apps/api/app*"}}},
		{Name: "worker", Command: "bun --watch src/worker.ts", Cwd: "apps/worker"},
		{Name: "admin", Port: 4200, Command: "npx ng serve --host 0.0.0.0 --port 4200", Cwd: "apps/admin", Autostart: autostart(false)},
	}
	if !reflect.DeepEqual(d.Servers, want) {
		t.Fatalf("servers = %+v", d.Servers)
	}
}

func TestLoadDeclaration_MissingFileIsEmpty(t *testing.T) {
	t.Parallel()
	d, err := LoadDeclaration(t.TempDir())
	if err != nil || len(d.Servers) != 0 {
		t.Fatalf("d = %+v, err = %v", d, err)
	}
}

func TestLoadDeclaration_CwdSymlinkedOutOfTheRepoIsInvalid(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "apps", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".zcp"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(root, DeclarationPath), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"version":1,"servers":[{"name":"web","command":"x","cwd":"escape"}]}`)
	if _, err := LoadDeclaration(root); err == nil || !strings.Contains(err.Error(), "resolves outside the repo") {
		t.Fatalf("err = %v", err)
	}
	write(`{"version":1,"servers":[{"name":"web","command":"x","cwd":"apps/web"}]}`)
	if _, err := LoadDeclaration(root); err != nil {
		t.Fatal(err)
	}
}

func TestDeclaration_ForServiceKeepsItsOwnAndUnassigned(t *testing.T) {
	t.Parallel()
	d := Declaration{Version: 1, Servers: []Server{
		{Name: "web", Command: "x", Service: "bundev"},
		{Name: "api", Command: "y", Service: "godev"},
		{Name: "tool", Command: "z"},
	}}
	got := make([]string, 0, 2)
	for _, s := range d.ForService("godev") {
		got = append(got, s.Name)
	}
	if !reflect.DeepEqual(got, []string{"api", "tool"}) {
		t.Fatalf("godev runs %v", got)
	}
}

func TestWatch_IgnoredPaths(t *testing.T) {
	t.Parallel()
	w := &Watch{Paths: []string{"apps/api", "packages/shared"}, Ignore: []string{"apps/api/app", "apps/api/tmp/", "**/*.log", "apps/api/*.test"}}
	tests := []struct {
		rel             string
		covered, ignore bool
	}{
		{"apps/api/main.go", true, false},
		{"apps/api/app", true, true},
		{"apps/api/tmp/build-1", true, true},
		{"apps/api/x.test", true, true},
		{"apps/api/.git/index", true, true},
		{"packages/shared/node_modules/a/b.js", true, true},
		{"packages/shared/index.mjs", true, false},
		{"apps/apix/main.go", false, false},
		{"apps/web/index.ts", false, false},
	}
	for _, tt := range tests {
		if got := w.covers(tt.rel); got != tt.covered {
			t.Errorf("covers(%q) = %v", tt.rel, got)
		}
		if got := w.ignored(tt.rel); got != tt.ignore {
			t.Errorf("ignored(%q) = %v", tt.rel, got)
		}
	}
}

// A `**` segment matches any number of directories, and a glob that matches a
// directory ignores everything under it: `uv run` rewrites
// src/<pkg>.egg-info deep in a watched tree on every start.
func TestWatch_AnIgnoreGlobMatchesAtAnyDepthWithDoubleStar(t *testing.T) {
	t.Parallel()
	w := &Watch{Paths: []string{"api"}, Ignore: []string{"**/*.egg-info", "**/__pycache__/**", "api/**/*.pyc", "*.log"}}
	tests := []struct {
		rel    string
		ignore bool
	}{
		{"api/providers/trace/trace-opik/src/dify_trace_opik.egg-info", true},
		{"api/providers/trace/trace-opik/src/dify_trace_opik.egg-info/PKG-INFO", true},
		{"top.egg-info/PKG-INFO", true},
		{"api/core/__pycache__/app.cpython-312.pyc", true},
		{"api/core/deep/x.pyc", true},
		{"api/x.pyc", true},
		{"server.log", true},
		{"api/core/app.py", false},
		{"api/logs/server.log", false},
		{"api/egg-info.py", false},
	}
	for _, tt := range tests {
		if got := w.ignored(tt.rel); got != tt.ignore {
			t.Errorf("ignored(%q) = %v, want %v", tt.rel, got, tt.ignore)
		}
	}
}

func TestServer_FirstDesired_FollowsAutostart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		autostart *bool
		want      Desired
	}{
		{"absent starts", nil, DesiredRunning},
		{"true starts", autostart(true), DesiredRunning},
		{"false stays stopped", autostart(false), DesiredStopped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := (Server{Name: "admin", Command: "x", Autostart: tc.autostart}).FirstDesired(); got != tc.want {
				t.Fatalf("FirstDesired = %q, want %q", got, tc.want)
			}
		})
	}
}

func autostart(b bool) *bool { return &b }

func TestDeclaration_PreviewPortBase_ParsesAndOldFilesUnchanged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		in          string
		wantBase    int
		wantPreview []string // names of the servers marked preview
		wantErr     string
	}{
		{name: "an old file has no base and no preview", in: `{"version":1,"servers":[{"name":"web","port":3000,"command":"x"}]}`},
		{name: "a base", in: `{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"x"}]}`, wantBase: 9000},
		{
			name:        "a server marked preview among several",
			in:          `{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"x","preview":true},{"name":"api","port":8080,"command":"y"}]}`,
			wantBase:    9000,
			wantPreview: []string{"web"},
		},
		{name: "a negative base", in: `{"version":1,"previewPortBase":-1,"servers":[]}`, wantErr: "previewPortBase -1 is out of range"},
		{name: "a base over the port range", in: `{"version":1,"previewPortBase":70000,"servers":[]}`, wantErr: "previewPortBase 70000 is out of range"},
		{name: "a preview server without a port", in: `{"version":1,"servers":[{"name":"worker","command":"x","preview":true}]}`, wantErr: `"worker": preview needs a port`},
		{
			name:    "two servers marked preview",
			in:      `{"version":1,"servers":[{"name":"a","port":1,"command":"x","preview":true},{"name":"b","port":2,"command":"y","preview":true}]}`,
			wantErr: `"a" and "b" are both marked preview`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, err := ParseDeclaration([]byte(tt.in))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if d.PreviewPortBase != tt.wantBase {
				t.Fatalf("PreviewPortBase = %d, want %d", d.PreviewPortBase, tt.wantBase)
			}
			var marked []string
			for _, s := range d.Servers {
				if s.Preview {
					marked = append(marked, s.Name)
				}
			}
			if !reflect.DeepEqual(marked, tt.wantPreview) {
				t.Fatalf("marked preview = %v, want %v", marked, tt.wantPreview)
			}
			// An old file written back stays an old file: neither key appears.
			if tt.wantBase == 0 && tt.wantPreview == nil {
				b, err := json.Marshal(d)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(b), "preview") {
					t.Fatalf("an old declaration marshals with a preview key: %s", b)
				}
			}
		})
	}
}

func TestPreviewServer_PicksTheOnlyPortOrTheMarkedOne(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		servers []Server
		want    string
	}{
		{name: "none", servers: nil},
		{name: "a portless worker only", servers: []Server{{Name: "worker", Command: "x"}}},
		{name: "the only server with a port", servers: []Server{{Name: "worker", Command: "x"}, {Name: "web", Port: 3000, Command: "y"}}, want: "web"},
		{name: "several with ports, none marked", servers: []Server{{Name: "web", Port: 3000, Command: "x"}, {Name: "api", Port: 8080, Command: "y"}}},
		{name: "several with ports, one marked", servers: []Server{{Name: "web", Port: 3000, Command: "x"}, {Name: "api", Port: 8080, Command: "y", Preview: true}}, want: "api"},
		{name: "one with a port, marked", servers: []Server{{Name: "web", Port: 3000, Command: "x", Preview: true}}, want: "web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := PreviewServer(tt.servers)
			if ok != (tt.want != "") || got.Name != tt.want {
				t.Fatalf("PreviewServer = %q, %v; want %q", got.Name, ok, tt.want)
			}
		})
	}
}

// siblingEnv maps a variable a part's dev server reads to the pool whose
// lease of the same branch it reaches; zcp reads it, the supervisor validates
// it with the rest of the file and never acts on it.
func TestParseDeclaration_SiblingEnv_Shapes(t *testing.T) {
	t.Parallel()
	servers := `"servers":[{"name":"web","port":3000,"command":"x"}]`
	tests := []struct {
		name    string
		sibling string // the siblingEnv value; "" leaves the key out
		want    map[string]string
		wantErr string
	}{
		{name: "an old file has none"},
		{name: "an empty map", sibling: `{}`, want: map[string]string{}},
		{
			name:    "variables naming pools",
			sibling: `{"HQ_HOST":"hqpool","_api_host2":"api2pool"}`,
			want:    map[string]string{"HQ_HOST": "hqpool", "_api_host2": "api2pool"},
		},
		{name: "a ZEROPS_ variable", sibling: `{"ZEROPS_HQ":"hqpool"}`, wantErr: `siblingEnv "ZEROPS_HQ"`},
		{name: "hostname", sibling: `{"hostname":"hqpool"}`, wantErr: `siblingEnv "hostname"`},
		{name: "serviceId", sibling: `{"serviceId":"hqpool"}`, wantErr: `siblingEnv "serviceId"`},
		{name: "projectId", sibling: `{"projectId":"hqpool"}`, wantErr: `siblingEnv "projectId"`},
		{name: "PATH", sibling: `{"PATH":"hqpool"}`, wantErr: `siblingEnv "PATH"`},
		{name: "HOME", sibling: `{"HOME":"hqpool"}`, wantErr: `siblingEnv "HOME"`},
		{name: "a name starting with a digit", sibling: `{"1HQ":"hqpool"}`, wantErr: `siblingEnv "1HQ"`},
		{name: "a name with a dash", sibling: `{"HQ-HOST":"hqpool"}`, wantErr: `siblingEnv "HQ-HOST"`},
		{name: "an empty name", sibling: `{"":"hqpool"}`, wantErr: `siblingEnv ""`},
		{name: "a pool with a capital", sibling: `{"HQ_HOST":"HQpool"}`, wantErr: `siblingEnv "HQ_HOST": "HQpool"`},
		{name: "a container hostname, not a pool", sibling: `{"HQ_HOST":"node-id-7.runtime.hqpool.zerops"}`, wantErr: `siblingEnv "HQ_HOST"`},
		{name: "an empty pool", sibling: `{"HQ_HOST":""}`, wantErr: `siblingEnv "HQ_HOST"`},
		{name: "a pool over 40 characters", sibling: `{"HQ_HOST":"` + strings.Repeat("a", 41) + `"}`, wantErr: `siblingEnv "HQ_HOST"`},
		{name: "a pool that is not a string", sibling: `{"HQ_HOST":7}`, wantErr: "parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := `{"version":1,` + servers + `}`
			if tt.sibling != "" {
				in = `{"version":1,"siblingEnv":` + tt.sibling + `,` + servers + `}`
			}
			d, err := ParseDeclaration([]byte(in))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(d.SiblingEnv, tt.want) {
				t.Fatalf("SiblingEnv = %#v, want %#v", d.SiblingEnv, tt.want)
			}
			// Written back, a file keeps its siblingEnv, and an old file gains none.
			b, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			again, err := ParseDeclaration(b)
			if err != nil || !maps.Equal(again.SiblingEnv, tt.want) {
				t.Fatalf("written back: %s (%v)", b, err)
			}
			if tt.sibling == "" && strings.Contains(string(b), "siblingEnv") {
				t.Fatalf("an old declaration marshals with a siblingEnv key: %s", b)
			}
		})
	}
}
