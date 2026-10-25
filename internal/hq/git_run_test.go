package hq

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRunGit_ConfigOutputDoesNotExposeTheProjectKey(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := RunGit(t.Context(), Identity{Origin: "https://hq.example", ProjectID: "project-1", Key: "runner-test-key"}, []string{"config", "--get", "http.https://hq.example/.extraHeader"}, nil, &stdout, &stderr)
	if code != 0 || stdout.String() != "Authorization: Project <redacted>\n" || stderr.Len() != 0 {
		t.Errorf("Git config did not redact authorization (exit %d)", code)
	}
}

func TestGitSecretWriter_RedactsEverySplitWithoutChangingOtherBytes(t *testing.T) {
	t.Parallel()
	const secret = "test-key"
	const input = "before test-key test-key after test-ke" //nolint:dupword // Adjacent secret values prove each occurrence is redacted.
	want := strings.ReplaceAll(input, secret, "<redacted>")
	for split := range len(input) + 1 {
		var out bytes.Buffer
		writer := &gitSecretWriter{dst: &out, secret: []byte(secret)}
		for _, part := range []string{input[:split], input[split:]} {
			if _, err := io.WriteString(writer, part); err != nil {
				t.Fatal(err)
			}
			if len(writer.pending) > len(secret)-1 {
				t.Fatalf("split %d kept %d bytes, want at most %d", split, len(writer.pending), len(secret)-1)
			}
		}
		writer.flush()
		if out.String() != want {
			t.Errorf("split %d changed non-secret output: %q, want %q", split, out.String(), want)
		}
	}
}

func TestRunGit_HTTPKeyRefusal_RechecksOnlyTheActualHQTarget(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		code       string
		basic      bool
		external   bool
		wantChecks int32
	}{
		{"refused key", "key_refused", false, false, 1},
		{"refused key with Basic challenge", "key_refused", true, false, 1},
		{"missing key", "project_key_required", true, false, 0},
		{"external origin", "key_refused", true, true, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var checks atomic.Int32
			var selves atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/project/self" {
					selves.Add(1)
				}
				if r.URL.Path == "/api/project/key-recheck" {
					if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Project runner-test-key" {
						t.Error("recheck did not present the project authorization via POST")
					}
					checks.Add(1)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if tt.basic {
					w.Header().Set("WWW-Authenticate", `Basic realm="HQ"`)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"code":"`+tt.code+`"}`)
			}))
			t.Cleanup(srv.Close)
			origin := srv.URL
			if tt.external {
				origin = "https://hq.example"
			}
			var stdout, stderr bytes.Buffer
			status := RunGit(t.Context(), Identity{Origin: origin, ProjectID: "project-1", Key: "runner-test-key"}, []string{"-c", "credential.helper=", "ls-remote", srv.URL + "/git/org/app/web.git"}, nil, &stdout, &stderr)
			if status != 128 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "fatal:") {
				t.Fatalf("failed Git exit = %d, stdout = %q, stderr = %q", status, stdout.String(), stderr.String())
			}
			if got := checks.Load(); got != tt.wantChecks {
				t.Fatalf("project key rechecks = %d, want %d; stderr = %q", got, tt.wantChecks, stderr.String())
			}
			wantSelf := int32(1)
			if tt.external {
				wantSelf = 0
			}
			if got := selves.Load(); got != wantSelf {
				t.Errorf("project self reads = %d, want %d", got, wantSelf)
			}
			if tt.wantChecks == 1 && !strings.Contains(stderr.String(), "401 key_refused") {
				t.Errorf("exact HQ refusal absent from stderr: %q", stderr.String())
			}
		})
	}
}

func TestRunGit_DoesNotFollowRedirectsWithTheProjectKey(t *testing.T) {
	t.Parallel()
	var redirected atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(external.Close)
	hq := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+r.URL.RequestURI(), http.StatusFound)
	}))
	t.Cleanup(hq.Close)
	var stderr bytes.Buffer
	status := RunGit(t.Context(), Identity{Origin: hq.URL, ProjectID: "project-1", Key: "runner-test-key"}, []string{"ls-remote", hq.URL + "/git/org/app/web.git"}, nil, io.Discard, &stderr)
	if status != 128 || redirected.Load() != 0 {
		t.Fatalf("Git exit = %d, redirected requests = %d; stderr = %q", status, redirected.Load(), stderr.String())
	}
}

func TestRunGit_EveryHQRequestPresentsTheProjectKey(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var headers []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = append(headers, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/plain")
		switch {
		case strings.HasSuffix(r.URL.Path, "/info/refs"):
			_, _ = io.WriteString(w, strings.Repeat("a", 40)+"\trefs/heads/main\n")
		case strings.HasSuffix(r.URL.Path, "/HEAD"):
			_, _ = io.WriteString(w, "ref: refs/heads/main\n")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	var stdout, stderr bytes.Buffer
	id := Identity{Origin: srv.URL, ProjectID: "project-1", Key: "runner-test-key"}
	code := RunGit(t.Context(), id, []string{"ls-remote", srv.URL + "/git/org/app/web.git"}, nil, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), strings.Repeat("a", 40)+"\trefs/heads/main") {
		t.Fatalf("git exit = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(headers) == 0 {
		t.Fatal("git never reached HQ")
	}
	for _, header := range headers {
		if header != "Project runner-test-key" {
			t.Error("an HQ Git request did not present the project key")
		}
	}
}

func TestRunGit_PreservesArgumentsInputOutputAndExitCode(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "checkout with spaces")
	init := exec.CommandContext(t.Context(), "git", "init", "-q", dir)
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	id := Identity{Origin: "https://hq.example", ProjectID: "project-1", Key: "runner-test-key"}
	var stdout, stderr bytes.Buffer
	if code := RunGit(t.Context(), id, []string{"-C", dir, "hash-object", "--stdin"}, strings.NewReader("hello\n"), &stdout, &stderr); code != 0 || stdout.String() != "ce013625030ba8dba906f756967f9e9ca394464a\n" || stderr.Len() != 0 {
		t.Fatalf("hash-object exit = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunGit(t.Context(), id, []string{"-C", filepath.Join(dir, "missing"), "status"}, nil, &stdout, &stderr); code != 128 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "cannot change to") {
		t.Fatalf("failed git exit = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	config := exec.CommandContext(t.Context(), "git", "-C", dir, "config", "--local", "--get-regexp", "extraheader")
	if out, err := config.CombinedOutput(); err == nil || len(out) != 0 {
		t.Fatalf("Git authorization was persisted: %q, %v", out, err)
	}
}

func TestRunGit_AnotherOriginReceivesNoProjectKey(t *testing.T) {
	t.Parallel()
	var headers []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = append(headers, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	var stderr bytes.Buffer
	_ = RunGit(t.Context(), Identity{Origin: "https://hq.example", ProjectID: "project-1", Key: "runner-test-key"}, []string{"ls-remote", srv.URL + "/external.git"}, nil, io.Discard, &stderr)
	mu.Lock()
	defer mu.Unlock()
	if len(headers) == 0 {
		t.Fatal("git never reached the external origin")
	}
	for _, header := range headers {
		if header != "" {
			t.Error("external origin received authorization")
		}
	}
}

func TestRunGit_PreservesTheCallersWorkingDirectory(t *testing.T) {
	// non-parallel: t.Chdir changes the process's working directory.
	dir := t.TempDir()
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	code := RunGit(t.Context(), Identity{Origin: "https://hq.example", ProjectID: "project-1", Key: "runner-test-key"}, []string{"rev-parse", "--show-toplevel"}, nil, &stdout, &stderr)
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || strings.TrimSpace(stdout.String()) != want {
		t.Fatalf("git exit = %d, directory = %q, want %q; stderr = %q", code, stdout.String(), want, stderr.String())
	}
	got, err := os.Getwd()
	if err != nil || got != want {
		t.Fatalf("caller directory changed: %q, %v; want %q", got, err, want)
	}
}
