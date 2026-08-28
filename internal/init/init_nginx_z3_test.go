// Tests for: the nginx locations that publish z3 (Zerops Code) and the
// container's readiness, on the same 8080 origin code-server already owns.
//
// NOT parallel — RunNginx reads VSCODE_PASSWORD and writes through
// package-level paths.
package init_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/z3"
)

// renderNginx runs RunNginx into a temp file and returns the rendered config.
// An empty password renders the no-auth shape.
func renderNginx(t *testing.T, password string) string {
	t.Helper()
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "nginx.conf")
	zcpinit.SetNginxOutputPath(outputPath)
	zcpinit.SetNginxDirs([]string{filepath.Join(tmpDir, "log")})
	zcpinit.SetNginxLogFiles(nil)
	zcpinit.SetNginxOwner(os.Geteuid(), os.Getegid())
	t.Cleanup(func() {
		zcpinit.ResetNginxOutputPath()
		zcpinit.ResetNginxDirs()
		zcpinit.ResetNginxLogFiles()
		zcpinit.ResetNginxOwner()
	})
	t.Setenv("VSCODE_PASSWORD", password)

	if err := zcpinit.RunNginx(); err != nil {
		t.Fatalf("RunNginx(): %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read nginx.conf: %v", err)
	}
	return string(data)
}

// locationBlock returns the body of the location whose header line starts with
// header, so a test can assert about ONE block rather than the whole file.
func locationBlock(t *testing.T, conf, header string) string {
	t.Helper()
	idx := strings.Index(conf, header)
	if idx < 0 {
		t.Fatalf("nginx.conf has no %q block:\n%s", header, conf)
	}
	rest := conf[idx:]
	depth := 0
	for i, r := range rest {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:i+1]
			}
		}
	}
	t.Fatalf("unbalanced braces after %q", header)
	return ""
}

// TestRunNginx_Z3OutsideCookieGate is the whole point of D2's "one door": the
// z3 server owns its own authentication (a Zerops identity, not a shared
// container password), and a browser client has to reach it BEFORE it holds
// any code-server cookie. So the location renders identically with and without
// VSCODE_PASSWORD, and never consults the cookie map.
func TestRunNginx_Z3OutsideCookieGate(t *testing.T) {
	for _, password := range []string{"alnum123token", ""} {
		conf := renderNginx(t, password)
		block := locationBlock(t, conf, "location "+z3.BasePath+"/ {")
		if strings.Contains(block, "zcp_cookie_ok") {
			t.Errorf("the z3 location must not sit behind the code-server cookie gate:\n%s", block)
		}
	}
}

// TestRunNginx_Z3ProxyStripsThePrefix locks the upstream form S0.5 verified end
// to end (mint → token → ws-ticket → 101 through nginx and the Zerops L7). The
// TRAILING SLASH is load-bearing: it strips the prefix, so the z3 server's own
// routes stay at the loopback root. Without it z3's SPA catch-all answers any
// mis-prefixed API call with 200 index.html — a base-path bug that looks like
// "the app loads but nothing works" instead of a 404.
func TestRunNginx_Z3ProxyStripsThePrefix(t *testing.T) {
	conf := renderNginx(t, "alnum123token")
	block := locationBlock(t, conf, "location "+z3.BasePath+"/ {")

	tests := []struct {
		name     string
		contains string
	}{
		{"proxies to the loopback port with the trailing slash", "proxy_pass http://127.0.0.1:3773/;"},
		{"HTTP/1.1 for the websocket upgrade", "proxy_http_version 1.1;"},
		{"forwards the upgrade header", "proxy_set_header Upgrade $http_upgrade;"},
		{"forwards the connection header", "proxy_set_header Connection $connection_upgrade;"},
		{"keeps a long-lived socket open", "proxy_read_timeout 86400s;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(block, tt.contains) {
				t.Errorf("the z3 location must contain %q:\n%s", tt.contains, block)
			}
		})
	}
}

// TestRunNginx_ClosesCodeServerProxyDoorToZ3: code-server's own /proxy/<port>/
// and /absproxy/<port>/ reach any loopback port for anyone holding the
// container cookie — a second, differently-authenticated door into the same
// server. Owner decision: one door. The regex location is evaluated before the
// prefix `location /` that hands requests to code-server, so this closes it.
func TestRunNginx_ClosesCodeServerProxyDoorToZ3(t *testing.T) {
	for _, password := range []string{"alnum123token", ""} {
		conf := renderNginx(t, password)
		block := locationBlock(t, conf, "location ~ ^/(abs)?proxy/3773(/|$) {")
		if !strings.Contains(block, "return 404;") {
			t.Errorf("the second door must be closed, not merely rerouted:\n%s", block)
		}
	}
}

// TestRunNginx_HealthzServesTheInitMarker locks the readiness contract: the
// marker `zcp init` wrote is served verbatim, outside the auth gate, so a
// client can tell "still initializing" from "broken" before it holds any
// credential — and can watch initAt move across a restart. No proxy and no
// process is involved, which is why it answers even when everything else in
// the container is down.
func TestRunNginx_HealthzServesTheInitMarker(t *testing.T) {
	for _, password := range []string{"alnum123token", ""} {
		conf := renderNginx(t, password)
		block := locationBlock(t, conf, "location = /healthz {")

		if strings.Contains(block, "zcp_cookie_ok") {
			t.Errorf("/healthz must stay outside the auth gate:\n%s", block)
		}
		if !strings.Contains(block, "alias "+z3.InitMarkerPath+";") {
			t.Errorf("/healthz must serve the init marker at %s:\n%s", z3.InitMarkerPath, block)
		}
		if !strings.Contains(block, "application/json") {
			t.Errorf("/healthz must answer as JSON:\n%s", block)
		}
	}
}

// TestRunNginx_HealthzFallbackIsValidJSON: before the first `zcp init`
// finishes there is no marker to serve. The fallback must still be a parseable
// answer with initComplete false — a bare 404 would leave a polling client
// unable to tell "not yet" from "no such route".
func TestRunNginx_HealthzFallbackIsValidJSON(t *testing.T) {
	conf := renderNginx(t, "alnum123token")
	block := locationBlock(t, conf, "location @zcp_healthz_uninitialized {")

	start := strings.Index(block, "'")
	end := strings.LastIndex(block, "'")
	if start < 0 || end <= start {
		t.Fatalf("the fallback must return a literal body:\n%s", block)
	}
	body := block[start+1 : end]

	var marker struct {
		InitComplete bool    `json:"initComplete"`
		InitAt       *string `json:"initAt"`
	}
	if err := json.Unmarshal([]byte(body), &marker); err != nil {
		t.Fatalf("the fallback body must parse as JSON: %v (body %q)", err, body)
	}
	if marker.InitComplete {
		t.Error("a container with no marker has not completed init")
	}
	if marker.InitAt != nil {
		t.Errorf("a container with no marker has no init time, got %q", *marker.InitAt)
	}
}
