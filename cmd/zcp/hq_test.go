package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
)

// non-parallel: the command reads process environment and writes os.Stdout.
func TestRunHQCmd_Status_NamesAddressedOriginAndUsesProjectMode(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			var path string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				_, _ = io.WriteString(w, `{}`)
			}))
			defer server.Close()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("ZCP_MATE_ENABLED", fmt.Sprint(enabled))
			t.Setenv("projectId", "project")
			t.Setenv("ZCP_API_KEY", "test-key")
			t.Setenv(hq.EnvOrigin, "")
			packagePath := filepath.Join(mate.CurrentLink(), "node_modules", mate.PackageName, "package.json")
			if err := os.MkdirAll(filepath.Dir(packagePath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(packagePath, fmt.Appendf(nil, `{"mate":{"hqOrigin":%q}}`, server.URL), 0o600); err != nil {
				t.Fatal(err)
			}
			stdout := captureStdout(t, func() { runHQCmd([]string{"status", "--json"}) })
			var status hqStatus
			if err := json.Unmarshal([]byte(stdout), &status); err != nil {
				t.Fatal(err)
			}
			if status.HQ != server.URL || !status.Linked {
				t.Errorf("status = %+v, want linked HQ %s", status, server.URL)
			}
			wantPath := "/api/project/self"
			if enabled {
				wantPath = "/api/mate/self"
			}
			if path != wantPath {
				t.Errorf("probe path = %q, want %q", path, wantPath)
			}
		})
	}
}

type stubHQ struct {
	err error
}

func (s stubHQ) check(context.Context) error { return s.err }

// withHQ points runHQCmd at svc for the test; not parallel — it swaps a
// package seam.
func withHQ(t *testing.T, svc hqService, err error) {
	t.Helper()
	orig := newHQService
	newHQService = func(context.Context) (hqService, string, error) { return svc, "", err }
	t.Cleanup(func() { newHQService = orig })
}

func TestRunHQCmd_UnknownSubcommand_Fails(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"bogus"}, {"--json"}} {
		if got := runHQCmd(args); got != 1 {
			t.Errorf("runHQCmd(%v) = %d, want 1", args, got)
		}
	}
}

func TestRunHQCmd_Status_JSONExitsZero(t *testing.T) {
	tests := []struct {
		name string
		svc  hqService
		err  error
		want map[string]any
	}{
		{"linked", stubHQ{}.check, nil, map[string]any{"linked": true}},
		{"HQ refuses the key", stubHQ{err: errors.New("hq refused: 401 key_not_recognized")}.check, nil, map[string]any{
			"linked": false, "error": "hq refused: 401 key_not_recognized",
		}},
		{"no key", nil, errors.New("hq: no identity"), map[string]any{"linked": false, "error": "hq: no identity"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withHQ(t, tt.svc, tt.err)
			var code int
			stdout := captureStdout(t, func() { code = runHQCmd([]string{"status", "--json"}) })
			if code != 0 {
				t.Fatalf("exit = %d, want 0", code)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout %q: %v", stdout, err)
			}
			wantRaw, _ := json.Marshal(tt.want)
			gotRaw, _ := json.Marshal(got)
			if string(gotRaw) != string(wantRaw) {
				t.Errorf("status = %s, want %s", gotRaw, wantRaw)
			}
		})
	}
}

func TestCLIDispatch_HQ_IsAVerb(t *testing.T) {
	t.Parallel()
	if _, ok := cliDispatch()["hq"]; !ok {
		t.Fatal(`"hq" is not a CLI verb: zcp hq would start the MCP server instead`)
	}
}

// TestRunHQGitCredential: `zcp hq git-credential` is the credential helper
// git runs on every fetch and push from the Mate's shell — it prints the Mate
// credential under HQ's git user for the linked HQ, and for a repository on
// GitHub the token HQ mints for it at that call, and nothing else on stdout.
// Declining exits non-zero with nothing printed, so a helper chain goes on to
// its next answer and never sends an empty password; why it declined goes to
// stderr, never the credential.
func TestRunHQGitCredential(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("path") != "acme/shop.git" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"reason":"not_mounted"}`)
			return
		}
		_, _ = io.WriteString(w, `{"username":"x-access-token","password":"ghs_minted","expiresAt":"2026-10-21T13:00:00Z"}`)
	}))
	t.Cleanup(srv.Close)
	hqHost := strings.TrimPrefix(srv.URL, "http://")
	path := filepath.Join(t.TempDir(), "git-credential")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := hq.Identity{Origin: srv.URL, ProjectID: "p1", Key: "the-key", GitCredentialPath: path}
	tests := []struct {
		name       string
		args       []string
		stdin      string
		want       string
		wantCode   int
		wantStderr string
	}{
		{"get for the HQ", []string{"get"}, "protocol=http\nhost=" + hqHost + "\n\n", "username=mate\npassword=secret\n", 0, ""},
		{"get for a mounted GitHub repository", []string{"get"}, "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", "username=x-access-token\npassword=ghs_minted\n", 0, ""},
		{"get for a GitHub repository HQ does not mount", []string{"get"}, "protocol=https\nhost=github.com\npath=acme/other.git\n\n", "", 1, "mounts no repository"},
		{"get for GitHub naming no repository", []string{"get"}, "protocol=https\nhost=github.com\n\n", "", 1, "useHttpPath"},
		{"get for another host", []string{"get"}, "protocol=https\nhost=gitlab.com\n\n", "", 1, ""},
		{"store keeps nothing", []string{"store"}, "protocol=https\nhost=github.com\nusername=x-access-token\npassword=ghs_minted\n\n", "", 0, ""},
		{"erase keeps nothing", []string{"erase"}, "protocol=https\nhost=github.com\n\n", "", 0, ""},
		{"no action", nil, "", "", 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out, errOut strings.Builder
			if code := runHQGitCredential(tt.args, strings.NewReader(tt.stdin), &out, &errOut, id, srv.Client()); code != tt.wantCode {
				t.Fatalf("exit %d, want %d (stderr %q)", code, tt.wantCode, errOut.String())
			}
			if out.String() != tt.want {
				t.Errorf("stdout = %q, want %q", out.String(), tt.want)
			}
			if tt.wantStderr == "" && errOut.Len() > 0 || !strings.Contains(errOut.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want %q", errOut.String(), tt.wantStderr)
			}
			if strings.Contains(errOut.String(), "secret") || strings.Contains(errOut.String(), "ghs_") {
				t.Errorf("stderr %q carries a credential", errOut.String())
			}
		})
	}
}

// TestIsHQGitCredential: the helper's verb is answered before the CLI's
// telemetry, whose one-time notice goes to stdout and whose flush would hold
// every git operation.
func TestIsHQGitCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"hq", "git-credential", "get"}, true},
		{[]string{"hq", "git-credential"}, true},
		{[]string{"hq", "status"}, false},
		{[]string{"hq"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := isHQGitCredential(tt.args); got != tt.want {
			t.Errorf("isHQGitCredential(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}
