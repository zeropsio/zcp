package adapters_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
)

func stubOpenCodeLookPathFound(_ string) (string, error) { return "/usr/local/bin/opencode", nil }
func stubOpenCodeLookPathMissing(_ string) (string, error) {
	return "", &exec.Error{Name: "opencode", Err: exec.ErrNotFound}
}

func stubOpenCodeVersionOutput(_ string, _ ...string) ([]byte, error) {
	return []byte("1.18.34\n"), nil
}

// newOpenCodeEnv builds an Env for OpenCode's container init. OpenCode passes
// its own process env to a local MCP server, so ContainerInit writes NO env
// block — RT is populated only to mirror a real container Env.
func newOpenCodeEnv(t *testing.T, home string) adapters.Env {
	t.Helper()
	return adapters.Env{
		BaseDir: t.TempDir(),
		Home:    home,
		RT: runtime.Info{
			InContainer: true,
			ServiceName: "appdev",
			ServiceID:   "svc-123",
			ProjectID:   "proj-456",
		},
		VSCodeWorkDir: "/var/www",
		CommandRunner: func(_ string, _ ...string) error { return nil },
		CommandOutput: stubOpenCodeVersionOutput,
		LookPath:      stubOpenCodeLookPathFound,
	}
}

func openCodeConfigPath(home string) string {
	return filepath.Join(home, ".config", "opencode", "opencode.json")
}

func TestOpenCode_Name(t *testing.T) {
	t.Parallel()
	if got := adapters.NewOpenCode().Name(); got != "opencode" {
		t.Errorf("Name() = %q, want %q", got, "opencode")
	}
}

// TestOpenCode_Detect covers the two places an OpenCode binary lives: on PATH
// (npm / package-manager installs) and the official installer's
// ~/.opencode/bin/opencode, which only a login shell's rc puts on PATH.
func TestOpenCode_Detect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		onPath        bool
		installerFile os.FileMode // 0 = absent
		want          bool
	}{
		{name: "on PATH", onPath: true, want: true},
		{name: "installer dir only", installerFile: 0o755, want: true},
		{name: "installer file not executable", installerFile: 0o644, want: false},
		{name: "absent everywhere", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			env := newOpenCodeEnv(t, home)
			if !tt.onPath {
				env.LookPath = stubOpenCodeLookPathMissing
			}
			if tt.installerFile != 0 {
				bin := filepath.Join(home, ".opencode", "bin", "opencode")
				if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), tt.installerFile); err != nil {
					t.Fatal(err)
				}
			}
			if got := adapters.NewOpenCode().Detect(env); got != tt.want {
				t.Errorf("Detect() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOpenCode_Validate_ProbesResolvedBinary pins that the version probe runs
// the binary Detect found — the installer path when it is not on PATH — and
// that a failing or empty probe is a soft warning, never a hard error.
func TestOpenCode_Validate_ProbesResolvedBinary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		onPath       bool
		out          string
		err          error
		wantBinary   string // "" = the installer path under home
		wantWarnings bool
	}{
		{name: "PATH binary ok", onPath: true, out: "1.18.34\n", wantBinary: "/usr/local/bin/opencode"},
		{name: "installer binary ok", out: "1.18.34\n"},
		{name: "probe error warns", onPath: true, err: errors.New("boom"), wantBinary: "/usr/local/bin/opencode", wantWarnings: true},
		{name: "empty output warns", onPath: true, out: "  \n", wantBinary: "/usr/local/bin/opencode", wantWarnings: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			env := newOpenCodeEnv(t, home)
			installer := filepath.Join(home, ".opencode", "bin", "opencode")
			if !tt.onPath {
				env.LookPath = stubOpenCodeLookPathMissing
				if err := os.MkdirAll(filepath.Dir(installer), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(installer, []byte("#!/bin/sh\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			want := tt.wantBinary
			if want == "" {
				want = installer
			}
			var probed string
			env.CommandOutput = func(name string, args ...string) ([]byte, error) {
				probed = name + " " + strings.Join(args, " ")
				return []byte(tt.out), tt.err
			}

			warnings, err := adapters.NewOpenCode().Validate(env)
			if err != nil {
				t.Fatalf("Validate err = %v, want nil (probe trouble is a warning)", err)
			}
			if probed != want+" --version" {
				t.Errorf("probed %q, want %q", probed, want+" --version")
			}
			if (len(warnings) > 0) != tt.wantWarnings {
				t.Errorf("warnings = %v, want warnings: %v", warnings, tt.wantWarnings)
			}
		})
	}
}

// TestOpenCode_ContainerInit_WritesConfig is the fixture table: a fresh home,
// an existing file with other servers / keys, and the shapes `permission` can
// take. Each row states the whole resulting document, so an unexpected key or
// a reordered user rule fails the row.
func TestOpenCode_ContainerInit_WritesConfig(t *testing.T) {
	t.Parallel()
	const zerops = `{"type":"local","command":["zcp","serve"],"enabled":true}`
	tests := []struct {
		name    string
		initial string // "" = no file
		want    string
	}{
		{
			name: "fresh home",
			want: `{"mcp":{"zerops":` + zerops + `},"permission":{"zerops_*":"allow"}}`,
		},
		{
			name:    "empty file",
			initial: "",
			want:    `{"mcp":{"zerops":` + zerops + `},"permission":{"zerops_*":"allow"}}`,
		},
		{
			name: "other servers and keys survive in their order",
			initial: `{"$schema":"https://opencode.ai/config.json","model":"anthropic/x",` +
				`"mcp":{"github":{"type":"remote","url":"https://gh.example/mcp","enabled":false},"ast":{"type":"local","command":["ast-mcp"]}},` +
				`"agent":{"build":{"permission":{"*":"ask","read":"allow"}}}}`,
			want: `{"$schema":"https://opencode.ai/config.json","model":"anthropic/x",` +
				`"mcp":{"github":{"type":"remote","url":"https://gh.example/mcp","enabled":false},"ast":{"type":"local","command":["ast-mcp"]},"zerops":` + zerops + `},` +
				`"agent":{"build":{"permission":{"*":"ask","read":"allow"}}},"permission":{"zerops_*":"allow"}}`,
		},
		{
			name:    "a stale zerops entry is replaced in place",
			initial: `{"mcp":{"zerops":{"type":"local","command":["old-zcp"],"enabled":false,"environment":{"X":"1"}},"b":{"type":"local","command":["b"]}}}`,
			want:    `{"mcp":{"zerops":` + zerops + `,"b":{"type":"local","command":["b"]}},"permission":{"zerops_*":"allow"}}`,
		},
		{
			// OpenCode evaluates permission rules in key order, last match wins:
			// the user's rules keep their order and zerops_* lands last.
			name:    "permission object keeps the user's rule order",
			initial: `{"permission":{"bash":"ask","*":"deny","edit":{"*.md":"allow"}}}`,
			want:    `{"permission":{"bash":"ask","*":"deny","edit":{"*.md":"allow"},"zerops_*":"allow"},"mcp":{"zerops":` + zerops + `}}`,
		},
		{
			name:    "an existing zerops_* rule is overwritten where it stands",
			initial: `{"permission":{"zerops_*":"ask","*":"deny"}}`,
			want:    `{"permission":{"zerops_*":"allow","*":"deny"},"mcp":{"zerops":` + zerops + `}}`,
		},
		{
			// A blanket action is the user's whole policy; turning it into an
			// object would rewrite it, so it is left exactly as it is.
			name:    "a blanket permission string is left alone",
			initial: `{"permission":"ask"}`,
			want:    `{"permission":"ask","mcp":{"zerops":` + zerops + `}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			path := openCodeConfigPath(home)
			if tt.initial != "" || tt.name == "empty file" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tt.initial), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			if err := adapters.NewOpenCode().ContainerInit(newOpenCodeEnv(t, home)); err != nil {
				t.Fatalf("ContainerInit: %v", err)
			}

			if got := compactJSON(t, readFile(t, path)); got != tt.want {
				t.Errorf("opencode.json\n  got:  %s\n  want: %s", got, tt.want)
			}
		})
	}
}

// TestOpenCode_ContainerInit_Rerun pins idempotency: a second run over its own
// output — and over a file with user content — is byte-identical.
func TestOpenCode_ContainerInit_Rerun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		initial string
	}{
		{name: "fresh"},
		{name: "with user content", initial: `{"model":"a/b","mcp":{"x":{"type":"local","command":["x"]}},"permission":{"*":"ask"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			path := openCodeConfigPath(home)
			if tt.initial != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tt.initial), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			env := newOpenCodeEnv(t, home)
			if err := adapters.NewOpenCode().ContainerInit(env); err != nil {
				t.Fatal(err)
			}
			first := readFile(t, path)
			if err := adapters.NewOpenCode().ContainerInit(env); err != nil {
				t.Fatal(err)
			}
			if second := readFile(t, path); second != first {
				t.Errorf("rerun changed opencode.json:\n  first:  %s\n  second: %s", first, second)
			}
		})
	}
}

// TestOpenCode_ContainerInit_NoEnvBlock_NeverBakesSecret pins the passthrough
// contract: OpenCode spawns a local MCP server with its own process env, so the
// entry carries no `environment` and the file never holds ZCP_API_KEY or the
// runtime ids.
func TestOpenCode_ContainerInit_NoEnvBlock_NeverBakesSecret(t *testing.T) {
	// Not parallel — sets ZCP_API_KEY to assert it is NOT written.
	home := t.TempDir()
	t.Setenv("ZCP_API_KEY", "super-secret-should-never-be-in-file")
	if err := adapters.NewOpenCode().ContainerInit(newOpenCodeEnv(t, home)); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}
	raw := readFile(t, openCodeConfigPath(home))
	for _, leak := range []string{"super-secret-should-never-be-in-file", "svc-123", "proj-456", "environment"} {
		if strings.Contains(raw, leak) {
			t.Errorf("opencode.json contains %q:\n%s", leak, raw)
		}
	}
}

// TestOpenCode_ContainerInit_RefusesUnreadable pins the merge contract: a file
// ZCP cannot parse (JSONC comments, a non-object root) is never overwritten.
func TestOpenCode_ContainerInit_RefusesUnreadable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		initial string
	}{
		{name: "comments", initial: "// mine\n{\"model\":\"a/b\"}\n"},
		{name: "array root", initial: `[1,2]`},
		{name: "mcp not an object", initial: `{"mcp":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			path := openCodeConfigPath(home)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.initial), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := adapters.NewOpenCode().ContainerInit(newOpenCodeEnv(t, home)); err == nil {
				t.Error("ContainerInit should refuse a file it cannot merge")
			}
			if got := readFile(t, path); got != tt.initial {
				t.Errorf("file was touched:\n%s", got)
			}
		})
	}
}

func TestOpenCode_ContainerInit_EmptyHomeReturnsError(t *testing.T) {
	t.Parallel()
	env := newOpenCodeEnv(t, t.TempDir())
	env.Home = ""
	if err := adapters.NewOpenCode().ContainerInit(env); err == nil {
		t.Error("ContainerInit should error when Env.Home is empty")
	}
}

// --- helpers ---

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// compactJSON strips insignificant whitespace while keeping key order — the
// order is part of what these fixtures pin.
func compactJSON(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(s)); err != nil {
		t.Fatalf("compact %q: %v", s, err)
	}
	return b.String()
}
