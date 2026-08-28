// Tests for: internal/z3 — the constants and the two computed things every
// piece of ZCP that installs, supervises or publishes the z3 (Zerops Code)
// agent server shares: the `t3 serve` argv and the unit's environment
// contract.
//
// NOT parallel at the top level — every path in this package is derived from
// HOME (see runtime.HomeDir), and the subtests use t.Setenv.
package z3_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/z3"
)

func TestServeArgv(t *testing.T) {
	t.Setenv("HOME", "/home/zerops")

	tests := []struct {
		name         string
		withBasePath bool
		want         []string
	}{
		{
			name:         "base path advertised",
			withBasePath: true,
			want: []string{
				"/bundle/t3", "serve",
				"--mode", "web",
				"--host", "127.0.0.1",
				"--port", "3773",
				"--base-path", "/z3",
				"--base-dir", "/home/zerops/.t3",
				"--no-browser",
				"--auto-bootstrap-project-from-cwd",
				"/var/www",
			},
		},
		{
			// An installed bundle that predates --base-path must still start:
			// the flag is dropped, never passed blind (an unknown flag is a
			// fatal parse error, and the unit would crash-loop at boot).
			name:         "base path not advertised",
			withBasePath: false,
			want: []string{
				"/bundle/t3", "serve",
				"--mode", "web",
				"--host", "127.0.0.1",
				"--port", "3773",
				"--base-dir", "/home/zerops/.t3",
				"--no-browser",
				"--auto-bootstrap-project-from-cwd",
				"/var/www",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := z3.ServeArgv("/bundle/t3", tt.withBasePath)
			if !slices.Equal(got, tt.want) {
				t.Errorf("ServeArgv:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestServeArgv_CwdIsPositional locks the one flag shape that a CLI-surface
// drift would silently break: `--auto-bootstrap-project-from-cwd` is a
// BOOLEAN flag and the working directory is a positional argument
// (apps/server/src/cli/config.ts: autoBootstrapProjectFromCwdFlag is
// Flag.boolean, cwd is Argument.string). Passing the directory as the flag's
// value would make t3 bootstrap the unit's launch directory instead.
func TestServeArgv_CwdIsPositional(t *testing.T) {
	t.Setenv("HOME", "/home/zerops")
	argv := z3.ServeArgv("/bundle/t3", true)
	if argv[len(argv)-1] != "/var/www" {
		t.Errorf("workspace must be the trailing positional argument, got %q", argv[len(argv)-1])
	}
	if argv[len(argv)-2] != "--auto-bootstrap-project-from-cwd" {
		t.Errorf("the boolean flag must sit directly before the positional, got %q", argv[len(argv)-2])
	}
}

func TestSupportsBasePath(t *testing.T) {
	dir := t.TempDir()

	advertises := writeFakeBin(t, filepath.Join(dir, "with"), "#!/bin/sh\necho '  --base-path   Public path prefix'\n")
	silent := writeFakeBin(t, filepath.Join(dir, "without"), "#!/bin/sh\necho '  --base-dir   Data directory'\n")
	broken := writeFakeBin(t, filepath.Join(dir, "broken"), "#!/bin/sh\nexit 1\n")

	tests := []struct {
		name string
		bin  string
		want bool
	}{
		{"help advertises the flag", advertises, true},
		{"help does not advertise it", silent, false},
		{"binary fails", broken, false},
		{"binary does not exist", filepath.Join(dir, "absent"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := z3.SupportsBasePath(tt.bin); got != tt.want {
				t.Errorf("SupportsBasePath(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestEnvLines(t *testing.T) {
	tests := []struct {
		name           string
		projectID      string
		apiHost        string
		allowedOrigins string
		want           []string
	}{
		{
			// The optional membership TTL is never written: an unset key is
			// what makes the server keep its own default.
			name:      "project id and api host only",
			projectID: "nTV3oMB2SS634ImDJnQckg",
			apiHost:   "api.app-prg1.zerops.io",
			want: []string{
				"T3CODE_ZEROPS_PROJECT_ID=nTV3oMB2SS634ImDJnQckg",
				"T3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io",
			},
		},
		{
			name:           "allowed origins configured",
			projectID:      "p1",
			apiHost:        "api.app-prg1.zerops.io",
			allowedOrigins: "https://code.example.com,https://staging.example.com",
			want: []string{
				"T3CODE_ZEROPS_PROJECT_ID=p1",
				"T3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io",
				"T3CODE_ZEROPS_ALLOWED_ORIGINS=https://code.example.com,https://staging.example.com",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := z3.EnvLines(tt.projectID, tt.apiHost, tt.allowedOrigins)
			if !slices.Equal(got, tt.want) {
				t.Errorf("EnvLines:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestParseEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "z3.env")
	body := "# written by zcp init\n\nT3CODE_ZEROPS_PROJECT_ID=p1\nT3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	got, err := z3.ParseEnvFile(path)
	if err != nil {
		t.Fatalf("ParseEnvFile: %v", err)
	}
	want := []string{"T3CODE_ZEROPS_PROJECT_ID=p1", "T3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io"}
	if !slices.Equal(got, want) {
		t.Errorf("ParseEnvFile:\n got %q\nwant %q", got, want)
	}

	if _, err := z3.ParseEnvFile(filepath.Join(dir, "absent")); err == nil {
		t.Error("a missing env file must be reported, not silently treated as empty")
	}
}

func TestResolveAPIHost(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty falls back to the canonical host", "", "api.app-prg1.zerops.io"},
		{"bare host passes through", "api.app-prg1.zerops.io", "api.app-prg1.zerops.io"},
		{"scheme is stripped", "https://api.app-znojmo1.zerops.io", "api.app-znojmo1.zerops.io"},
		{"trailing slash is stripped", "https://api.app-prg1.zerops.io/", "api.app-prg1.zerops.io"},
		{"port is preserved", "http://localhost:3000", "localhost:3000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := z3.ResolveAPIHost(tt.raw); got != tt.want {
				t.Errorf("ResolveAPIHost(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestPaths_DeriveFromHome locks that every z3 path follows HOME. Production
// and tests agree because the container's service user is `zerops` with
// HOME=/home/zerops; a test that redirects HOME never touches the real one.
func TestPaths_DeriveFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"prefix", z3.Prefix(), filepath.Join(home, ".zcp", "z3")},
		{"bundle entry point", z3.BinPath(), filepath.Join(home, ".zcp", "z3", "node_modules", ".bin", "t3")},
		{"unit env file", z3.EnvFilePath(), filepath.Join(home, ".zcp", "z3.env")},
		{"server data dir", z3.BaseDir(), filepath.Join(home, ".t3")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}

	// The env file must sit OUTSIDE the npm prefix: `npm install --prefix`
	// owns everything under it (the §4a dev loop reinstalls the bundle there).
	if strings.HasPrefix(z3.EnvFilePath(), z3.Prefix()+string(os.PathSeparator)) {
		t.Errorf("env file %q must not live inside the npm prefix %q", z3.EnvFilePath(), z3.Prefix())
	}
}

// TestInstallArgs locks the one npm invocation, so a version bump has exactly
// one place to touch (PinnedVersion) and the install never resolves `latest`.
func TestInstallArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := z3.InstallArgs()
	joined := strings.Join(got, " ")
	for _, want := range []string{"install", "--prefix", z3.Prefix(), z3.PackageSpec} {
		if !strings.Contains(joined, want) {
			t.Errorf("InstallArgs() = %q, must contain %q", got, want)
		}
	}
	if strings.Contains(joined, "latest") {
		t.Errorf("InstallArgs() must pin a version, got %q", got)
	}
	if z3.PackageSpec != "t3@"+z3.PinnedVersion {
		t.Errorf("PackageSpec %q must derive from PinnedVersion %q", z3.PackageSpec, z3.PinnedVersion)
	}
}

func writeFakeBin(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return path
}
