// Tests for: ops/delivery_git.go — every git command a delivery runs against
// HQ ends within its bound, even against an HQ that never accepts it (spec-mate
// §10.10): git has no setting for its connect, so a black-holed HQ would hold
// it for curl's 300 s.
package ops

import (
	"bytes"
	"context"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/cgi" //nolint:gosec // G504: the fake serves git's own http-backend through CGI, in a test
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hqGitCommands are the commands of a delivery that reach HQ, by name.
func hqGitCommands(pair string) map[string]string {
	return map[string]string{
		"the Mate's branch":  BuildMateBranchCommand(pair, labBranch),
		"a delivery":         BuildDeliveryCommand(pair, "Add a footer", "", ""),
		"a push's sync":      BuildDeliverySyncCommand(pair, "", ""),
		"taking a change in": BuildTakeChangeInCommand(pair, labChange1),
		"pushing a change":   BuildChangePushCommand(pair, labChange1),
	}
}

// TestHQGit_EveryCommandCarriesTheStallBound: git's own low-speed bound, per
// command and never global config, ends a transfer HQ stops feeding.
func TestHQGit_EveryCommandCarriesTheStallBound(t *testing.T) {
	t.Parallel()
	for name, command := range hqGitCommands("/var/www") {
		if !strings.Contains(command, "-c http.lowSpeedLimit=1 -c http.lowSpeedTime=10") {
			t.Errorf("%s runs git against HQ without the stall bound:\n%s", name, command)
		}
	}
}

// TestHQGit_AnHQThatNeverAcceptsEndsWithinTheBound: against a listener that
// never accepts, each command ends within HQGitBound, says so, and reads as
// HQ unable to serve it.
//
// Non-parallel: it narrows HQGitBound, which every command reads.
func TestHQGit_AnHQThatNeverAcceptsEndsWithinTheBound(t *testing.T) {
	if _, err := exec.LookPath("timeout"); err != nil {
		t.Skip("the bound is `timeout`, which this machine lacks")
	}
	prev := HQGitBound
	HQGitBound = time.Second
	t.Cleanup(func() { HQGitBound = prev })
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	pair := mateBranchLab(t, nil)
	runGit(t, pair, "commit", "-q", "--allow-empty", "-m", "zcp init")
	runGit(t, pair, "remote", "set-url", "origin", "https://"+listener.Addr().String()+"/git/app-1/appdev.git")
	for name, command := range hqGitCommands(pair) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			start := time.Now()
			out, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
			if took := time.Since(start); err == nil || took > 6*time.Second {
				t.Fatalf("took %s, err %v; want it to fail within its bound\n%s", took, err, out)
			}
			if words, ok := HQGitNoAnswer(string(out)); !ok || words != "no answer within 1s" {
				t.Errorf("HQGitNoAnswer = %q, %v; want the bound named\n%s", words, ok, out)
			}
			if !GitRemoteUnavailable(string(out)) {
				t.Errorf("an HQ that never answered must read as unable to serve:\n%s", out)
			}
		})
	}
}

// pacedGitHQ serves bare's repository over git's smart HTTP at /git/x.git,
// handing the answer to the request that asks for objects — the pack — to
// pace rather than sending it at once.
func pacedGitHQ(t *testing.T, bare string, pace func(w http.ResponseWriter, r *http.Request, body []byte)) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	backend := &cgi.Handler{
		Path: gitPath, Args: []string{"http-backend"}, Root: "/git",
		Env:        []string{"GIT_PROJECT_ROOT=" + filepath.Dir(bare), "GIT_HTTP_EXPORT_ALL=1", "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1"},
		InheritEnv: []string{"PATH"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/git-upload-pack") {
			backend.ServeHTTP(w, r)
			return
		}
		asked, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(asked))
		if !bytes.Contains(asked, []byte("want ")) {
			backend.ServeHTTP(w, r)
			return
		}
		recorded := httptest.NewRecorder()
		backend.ServeHTTP(recorded, r)
		maps.Copy(w.Header(), recorded.Header())
		w.WriteHeader(recorded.Code)
		pace(w, r, recorded.Body.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/git/" + filepath.Base(bare)
}

// TestHQGit_ATransferIsBoundedByItsStallNotItsLength: a transfer HQ keeps
// feeding runs as long as it takes — past 15 s here, which once ended it —
// while one HQ stops feeding ends at the stall bound, said as a stall.
func TestHQGit_ATransferIsBoundedByItsStallNotItsLength(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("waits out a slow transfer and a stall")
	}
	tests := []struct {
		name     string
		pace     func(w http.ResponseWriter, r *http.Request, body []byte)
		wantOK   bool
		min, max time.Duration
	}{
		{"a slow transfer that keeps going", func(w http.ResponseWriter, _ *http.Request, body []byte) {
			const parts = 9
			for i := range parts {
				if i > 0 {
					time.Sleep(2 * time.Second)
				}
				_, _ = w.Write(body[i*len(body)/parts : (i+1)*len(body)/parts])
				w.(http.Flusher).Flush()
			}
		}, true, 15 * time.Second, 25 * time.Second},
		{"a transfer that stalls", func(w http.ResponseWriter, r *http.Request, body []byte) {
			_, _ = w.Write(body[:len(body)/2])
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
			case <-time.After(30 * time.Second):
			}
		}, false, 9 * time.Second, 25 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pair := mateBranchLabOn(t, func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "app.js"), strings.Repeat("the app\n", 512))
				commitAll(t, dir, "the app")
			}, nil)
			remote := strings.TrimSpace(runGit(t, pair, "remote", "get-url", "origin"))
			runGit(t, pair, "remote", "set-url", "origin", pacedGitHQ(t, remote, tt.pace))

			start := time.Now()
			out, err := exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // G204: the command under test
				"cd "+shellQuote(pair)+" && "+hqGit(hqCredentialHelperArgs()+" fetch --no-tags -q origin")).CombinedOutput()
			took := time.Since(start)
			if took < tt.min || took > tt.max {
				t.Errorf("the fetch took %s, want between %s and %s\n%s", took, tt.min, tt.max, out)
			}
			if (err == nil) != tt.wantOK {
				t.Fatalf("err = %v, want it to succeed: %v\n%s", err, tt.wantOK, out)
			}
			if tt.wantOK {
				if got, want := runGit(t, pair, "rev-parse", "origin/main"), runGit(t, remote, "rev-parse", "main"); got != want {
					t.Errorf("origin/main = %s, want %s", got, want)
				}
				return
			}
			if !HQGitStalled(string(out)) || !GitRemoteUnavailable(string(out)) {
				t.Errorf("a stalled transfer must read as a stall, HQ unable to serve it:\n%s", out)
			}
		})
	}
}

// TestBuildDeliveryCommitCommand_CommitsWithoutHQ: a delivery commits the
// deployed tree before anything reaches HQ — so the work is the checkout's
// own whether or not HQ then answers — and refuses an unignored dependency
// directory before anything is staged.
func TestBuildDeliveryCommitCommand_CommitsWithoutHQ(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		files      map[string]string
		wantCommit bool
		wantRefuse string
	}{
		{"the deployed tree", map[string]string{"footer.js": "the footer\n"}, true, ""},
		{"an unignored dependency directory", map[string]string{"node_modules/x/index.js": "x\n"}, false, "node_modules"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pair := mateBranchLab(t, nil)
			runGit(t, pair, "commit", "-q", "--allow-empty", "-m", "zcp init")
			runGit(t, pair, "remote", "set-url", "origin", "https://hq.invalid/git/app-1/appdev.git")
			for name, body := range tt.files {
				writeLabFile(t, filepath.Join(pair, name), body)
			}

			out, err := exec.CommandContext(t.Context(), "sh", "-c", BuildDeliveryCommitCommand(pair, "Add a footer")).CombinedOutput() //nolint:gosec // G204: the command under test
			if got := runGit(t, pair, "log", "-1", "--format=%s"); (got == "Add a footer") != tt.wantCommit {
				t.Errorf("HEAD reads %q after the commit (err %v)\n%s", got, err, out)
			}
			if got := DeliveryUnignored(string(out)); got != tt.wantRefuse {
				t.Errorf("refused %q, want %q\n%s", got, tt.wantRefuse, out)
			}
		})
	}
}
