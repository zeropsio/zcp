// Tests for: internal/mate — verified release delivery plus the shared constants,
// `mate serve` argv, and unit environment contract used by every piece of ZCP
// that installs, supervises, or publishes the mate (Zerops Mate) agent server.
//
// NOT parallel at the top level — every path in this package is derived from
// HOME (see runtime.HomeDir), and the subtests use t.Setenv.
package mate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
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
				"/bundle/mate", "serve",
				"--mode", "web",
				"--host", "127.0.0.1",
				"--port", "3773",
				"--base-path", "/mate",
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
				"/bundle/mate", "serve",
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
			got := mate.ServeArgv("/bundle/mate", tt.withBasePath)
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
// value would make mate bootstrap the unit's launch directory instead.
func TestServeArgv_CwdIsPositional(t *testing.T) {
	t.Setenv("HOME", "/home/zerops")
	argv := mate.ServeArgv("/bundle/mate", true)
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
			if got := mate.SupportsBasePath(tt.bin); got != tt.want {
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
			got := mate.EnvLines(tt.projectID, tt.apiHost, tt.allowedOrigins)
			if !slices.Equal(got, tt.want) {
				t.Errorf("EnvLines:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestParseEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mate.env")
	body := "# written by zcp init\n\nT3CODE_ZEROPS_PROJECT_ID=p1\nT3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	got, err := mate.ParseEnvFile(path)
	if err != nil {
		t.Fatalf("ParseEnvFile: %v", err)
	}
	want := []string{"T3CODE_ZEROPS_PROJECT_ID=p1", "T3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io"}
	if !slices.Equal(got, want) {
		t.Errorf("ParseEnvFile:\n got %q\nwant %q", got, want)
	}

	if _, err := mate.ParseEnvFile(filepath.Join(dir, "absent")); err == nil {
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
			if got := mate.ResolveAPIHost(tt.raw); got != tt.want {
				t.Errorf("ResolveAPIHost(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestPaths_DeriveFromHome locks that every mate path follows HOME. Production
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
		{"prefix", mate.Prefix(), filepath.Join(home, ".zcp", "mate")},
		{"versions dir", mate.VersionsDir(), filepath.Join(home, ".zcp", "mate", "versions")},
		{"one version dir", mate.VersionDir("0.1.0"), filepath.Join(home, ".zcp", "mate", "versions", "0.1.0")},
		{"current link", mate.CurrentLink(), filepath.Join(home, ".zcp", "mate", "current")},
		{"bundle entry point", mate.BinPath(), filepath.Join(home, ".zcp", "mate", "current", "node_modules", ".bin", mate.BinName)},
		{"unit env file", mate.EnvFilePath(), filepath.Join(home, ".zcp", "mate.env")},
		{"server data dir", mate.BaseDir(), filepath.Join(home, ".t3")},
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
	if strings.HasPrefix(mate.EnvFilePath(), mate.Prefix()+string(os.PathSeparator)) {
		t.Errorf("env file %q must not live inside the npm prefix %q", mate.EnvFilePath(), mate.Prefix())
	}
}

// TestInstallArgs_UsesPinnedReleaseAsset locks both sides of delivery: the
// remote asset is derived from the fork's package name and pinned version,
// while npm receives only the already-downloaded local tarball path.
func TestInstallArgs_UsesPinnedReleaseAsset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	metadata := []struct {
		name string
		got  string
		want string
	}{
		{"published package name", mate.PackageName, "zerops-mate"},
		{"published executable name", mate.BinName, "mate"},
		{"published version", mate.PinnedVersion, "0.2.1"},
		{"published asset name", mate.ReleaseAssetName, "zerops-mate-0.2.1.tgz"},
		{"published release URL", mate.ReleaseURL, "https://github.com/zeropsio/mate/releases/download/v0.2.1/zerops-mate-0.2.1.tgz"},
		{"published asset digest", mate.PinnedSHA256, "749071c18705ff1e5fa9a45339a6f22e1b7916aad87222a1f228b98284eb03d9"},
	}
	for _, tt := range metadata {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}

	t.Run("pin is canonical lowercase 64-hex", func(t *testing.T) {
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(mate.PinnedSHA256) {
			t.Errorf("PinnedSHA256 = %q, want 64 lowercase hex characters", mate.PinnedSHA256)
		}
	})

	tarballPath := filepath.Join(t.TempDir(), mate.ReleaseAssetName)
	prefix := mate.VersionDir(mate.PinnedVersion)
	got := mate.InstallArgs(prefix, tarballPath)
	want := []string{
		"npm", "install",
		"--prefix", prefix,
		"--no-audit", "--no-fund", "--loglevel=error",
		tarballPath,
	}
	if !slices.Equal(got, want) {
		t.Errorf("InstallArgs():\n got %q\nwant %q", got, want)
	}
}

func TestInstallRelease_ChecksumMismatch_RefusesInstall(t *testing.T) {
	body := []byte("corrupted release tarball")
	server, client := newPipeHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))

	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "npm-ran")
	t.Setenv("NPM_MARKER", marker)
	t.Setenv("PATH", binDir)
	writeFakeBin(t, filepath.Join(binDir, "npm"), "#!/bin/sh\n: > \"$NPM_MARKER\"\n")

	expected := strings.Repeat("0", sha256.Size*2)
	actual := fmt.Sprintf("%x", sha256.Sum256(body))
	err := mate.InstallRelease(context.Background(), client, server.URL+"/"+mate.ReleaseAssetName, expected, t.TempDir())
	if err == nil {
		t.Fatal("InstallRelease(): expected checksum mismatch")
	}
	for _, want := range []string{expected, actual} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("checksum error must contain digest %q, got %q", want, err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("npm must not run for a corrupt download, marker stat err=%v", err)
	}
}

func TestInstallRelease_DownloadFailure_RefusesInstall(t *testing.T) {
	server, client := newPipeHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "release not found", http.StatusNotFound)
	}))

	t.Setenv("HOME", t.TempDir())
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "npm-ran")
	t.Setenv("NPM_MARKER", marker)
	t.Setenv("PATH", binDir)
	writeFakeBin(t, filepath.Join(binDir, "npm"), "#!/bin/sh\n: > \"$NPM_MARKER\"\n")

	err := mate.InstallRelease(
		context.Background(),
		client,
		server.URL+"/releases/download/v"+mate.PinnedVersion+"/"+mate.ReleaseAssetName,
		strings.Repeat("0", sha256.Size*2),
		t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404 Not Found") {
		t.Fatalf("InstallRelease(): expected named 404, got %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("npm must not run after a failed download, marker stat err=%v", err)
	}
}

func TestInstallRelease_ValidDigest_InstallsDownloadedTarball(t *testing.T) {
	body := []byte("valid release tarball")
	requestedPath := make(chan string, 1)
	server, client := newPipeHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath <- r.URL.Path
		_, _ = w.Write(body)
	}))

	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "npm-args")
	t.Setenv("NPM_ARGS", argsPath)
	t.Setenv("PATH", binDir)
	writeFakeBin(t, filepath.Join(binDir, "npm"), `#!/bin/sh
last=
for arg do
  last=$arg
done
test -f "$last" || exit 70
printf '%s\n' "$@" > "$NPM_ARGS"
`)

	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	url := server.URL + "/releases/download/v" + mate.PinnedVersion + "/" + mate.ReleaseAssetName
	prefix := t.TempDir()
	if err := mate.InstallRelease(context.Background(), client, url, digest, prefix); err != nil {
		t.Fatalf("InstallRelease(): %v", err)
	}

	wantRequestPath := "/releases/download/v" + mate.PinnedVersion + "/" + mate.ReleaseAssetName
	if got := <-requestedPath; got != wantRequestPath {
		t.Errorf("download path = %q, want %q", got, wantRequestPath)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read npm argv: %v", err)
	}
	args := strings.Fields(string(data))
	if len(args) == 0 {
		t.Fatal("npm received no arguments")
	}
	tarballPath := args[len(args)-1]
	wantArgs := mate.InstallArgs(prefix, tarballPath)[1:]
	if !slices.Equal(args, wantArgs) {
		t.Errorf("npm argv:\n got %q\nwant %q", args, wantArgs)
	}
	if strings.HasPrefix(tarballPath, "http") {
		t.Errorf("npm must receive a downloaded local tarball, got %q", tarballPath)
	}
}

func TestInstallRelease_UnsetPinnedDigest_RefusesInstall(t *testing.T) {
	var requests atomic.Int32
	server, client := newPipeHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("future release tarball"))
	}))

	t.Setenv("HOME", t.TempDir())
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "npm-ran")
	t.Setenv("NPM_MARKER", marker)
	t.Setenv("PATH", binDir)
	writeFakeBin(t, filepath.Join(binDir, "npm"), "#!/bin/sh\n: > \"$NPM_MARKER\"\n")

	err := mate.InstallRelease(context.Background(), client, server.URL+"/"+mate.ReleaseAssetName, "", t.TempDir())
	if err == nil {
		t.Fatal("InstallRelease(): an unset integrity pin must fail closed")
	}
	if !strings.Contains(err.Error(), "pinned SHA-256 digest is unset") {
		t.Errorf("InstallRelease(): expected named unset-pin error, got %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("HTTP requests = %d, want 0 when the digest pin is unset", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("npm must not run without an integrity authority, marker stat err=%v", err)
	}
}

// TestLoadLiveEnv locks the reader for the container's live env store
// (/etc/zerops-zembed/env.json in production, see mate.LiveEnvStorePath): a
// flat JSON object of string → string, rendered as sorted KEY=VALUE lines so
// a caller merging it into a child's environment gets a deterministic order.
// A missing file, malformed JSON, and a non-string value are all errors — the
// caller (service.mateExtraEnv) decides how to degrade, this function never
// swallows a problem silently.
func TestLoadLiveEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string // empty means: do not create the file at all
		absent  bool
		want    []string
		wantErr bool
	}{
		{
			name: "valid store",
			body: `{"ZCP_API_KEY":"key-1","PATH":"/opt/zerops/bin:/usr/bin","HOME":"/home/zerops"}`,
			want: []string{
				"HOME=/home/zerops",
				"PATH=/opt/zerops/bin:/usr/bin",
				"ZCP_API_KEY=key-1",
			},
		},
		{
			name:    "missing file",
			absent:  true,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			body:    `{"PATH": `,
			wantErr: true,
		},
		{
			name:    "non-string value",
			body:    `{"PATH":"/usr/bin","ZEROPS_projectId":123}`,
			wantErr: true,
		},
		{
			name: "empty object",
			body: `{}`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "env.json")
			if !tt.absent {
				if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
					t.Fatalf("write store: %v", err)
				}
			}

			got, err := mate.LoadLiveEnv(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("LoadLiveEnv(%s): expected error, got lines %q", tt.name, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadLiveEnv(%s): unexpected error: %v", tt.name, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("LoadLiveEnv(%s):\n got %q\nwant %q", tt.name, got, tt.want)
			}
		})
	}
}

func writeFakeBin(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return path
}

// pipeListener lets httptest.Server exercise a real HTTP exchange in
// sandboxes that forbid binding even a loopback TCP port. Its connections are
// net.Pipe pairs supplied by the paired client's DialContext, so no network
// socket exists or can escape the test process.
type pipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
}

func newPipeHTTPTestServer(t *testing.T, handler http.Handler) (*httptest.Server, *http.Client) {
	t.Helper()
	listener := &pipeListener{
		connections: make(chan net.Conn),
		closed:      make(chan struct{}),
	}
	server := &httptest.Server{
		Listener: listener,
		Config: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: time.Second,
		},
	}
	server.Start()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			serverConn, clientConn := net.Pipe()
			select {
			case listener.connections <- serverConn:
				return clientConn, nil
			case <-ctx.Done():
				_ = serverConn.Close()
				_ = clientConn.Close()
				return nil, ctx.Err()
			case <-listener.closed:
				_ = serverConn.Close()
				_ = clientConn.Close()
				return nil, net.ErrClosed
			}
		},
	}
	client := &http.Client{Transport: transport}
	t.Cleanup(func() {
		client.CloseIdleConnections()
		server.Close()
	})
	return server, client
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

// TestNativeAddonProbeArgs_OpensTheLazilyLoadedAddons locks the probe that runs
// beside `mate --version` before a staged version goes live.
//
// `--version` proves the entry point and everything it imports STATICALLY
// resolves. The addons the server reaches through a runtime import — the PTY
// behind every agent terminal, and msgpackr's native encoder — are not on that
// path, so a release whose manifest stopped declaring one would install, pass
// `--version`, go live, and fail the first time somebody opened a terminal.
func TestNativeAddonProbeArgs_OpensTheLazilyLoadedAddons(t *testing.T) {
	got := mate.NativeAddonProbeArgs()
	want := []string{
		"node", "--input-type=module", "-e",
		`await import("node-pty"); await import("msgpackr-extract");`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("NativeAddonProbeArgs():\n got %q\nwant %q", got, want)
	}
}

// TestDefaultSmokeTestInstall_FailsWhenAnAddonIsMissing runs the real probe
// against a version directory that has a working entry point and no addons —
// the exact shape a manifest regression would install. Without this the probe
// could be wired to the wrong directory and nothing would notice: a `node -e`
// that resolves from the wrong cwd fails the same way it would if it ran
// nowhere at all.
func TestDefaultSmokeTestInstall_FailsWhenAnAddonIsMissing(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH; the probe cannot be exercised here")
	}

	versionDir := t.TempDir()
	binDir := filepath.Join(versionDir, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", binDir, err)
	}
	bin := filepath.Join(binDir, mate.BinName)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'mate v0.0.0-test'\n"), 0o700); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	err := mate.DefaultSmokeTestInstall(t.Context(), versionDir)
	if err == nil {
		t.Fatal("DefaultSmokeTestInstall() = nil, want an error naming the addon that would not load")
	}
	if !strings.Contains(err.Error(), "node-pty") {
		t.Errorf("DefaultSmokeTestInstall() error = %v, want it to name node-pty", err)
	}
}
