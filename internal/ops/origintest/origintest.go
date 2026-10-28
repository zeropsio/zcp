// Package origintest serves bare git repositories over smart HTTPS for the
// tests of zcp's origin reads and tags (docs/spec-workflows.md §10 P-LP-20,
// P-LP-21): git's own http-backend behind an httptest TLS server. zcp's
// origin scratch speaks https alone, so a test's origin is one of these, and
// the test process trusts httptest's fixed certificate (TrustCA, once, in the
// package's TestMain). Only tests import it.
package origintest

import (
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/cgi" //nolint:gosec // G504: serves git's own http-backend through CGI, for tests only
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Server is a TLS server of bare repositories, each by its name
// (`<URL>/<name>`).
type Server struct {
	URL  string
	root string

	mu       sync.Mutex
	works    map[string]string
	tokens   map[string]string
	sawToken map[string]bool
	fetches  map[string]int
}

// New starts a server for the test; it skips the test where git is not
// installed.
func New(tb testing.TB) *Server {
	tb.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		tb.Skip("git is not installed")
	}
	s := &Server{
		root:  tb.TempDir(),
		works: map[string]string{}, tokens: map[string]string{}, sawToken: map[string]bool{}, fetches: map[string]int{},
	}
	backend := &cgi.Handler{
		Path: gitPath, Args: []string{"http-backend"},
		Env: []string{
			"GIT_PROJECT_ROOT=" + s.root, "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=origintest",
			"HOME=" + tb.TempDir(), "GIT_CONFIG_NOSYSTEM=1",
		},
		InheritEnv: []string{"PATH"},
		// http-backend's own complaints (a repository that does not exist)
		// are the fixture's, not the code under test's.
		Stderr: io.Discard,
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serve(w, r, backend)
	}))
	tb.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// BasicCredential is the Authorization value git sends for token through
// zcp's credential helper (user oauth2).
func BasicCredential(token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("oauth2:"+token))
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request, backend http.Handler) {
	name, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	s.mu.Lock()
	token, guarded := s.tokens[name]
	s.mu.Unlock()
	if guarded {
		if r.Header.Get("Authorization") != BasicCredential(token) {
			w.Header().Set("WWW-Authenticate", `Basic realm="origin"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		s.mu.Lock()
		s.sawToken[name] = true
		s.mu.Unlock()
	}
	if strings.HasSuffix(r.URL.Path, "/info/refs") && r.URL.Query().Get("service") == "git-upload-pack" {
		s.mu.Lock()
		s.fetches[name]++
		s.mu.Unlock()
	}
	backend.ServeHTTP(w, r)
}

// Repo makes the bare repository name — main holding files, each named
// branch main's files with its own written over them — and answers its URL
// and each branch's head, main's under "main".
func (s *Server) Repo(tb testing.TB, name string, files map[string]string, branches map[string]map[string]string) (string, map[string]string) {
	tb.Helper()
	work := tb.TempDir()
	Git(tb, work, "init", "-q", "-b", "main")
	write := func(fs map[string]string) {
		for file, body := range fs {
			if err := os.WriteFile(filepath.Join(work, file), []byte(body), 0o600); err != nil {
				tb.Fatal(err)
			}
		}
		Git(tb, work, "add", "-A")
		Git(tb, work, "commit", "-q", "-m", "fixture")
	}
	write(files)
	heads := map[string]string{"main": Git(tb, work, "rev-parse", "HEAD")}
	for branch, fs := range branches {
		Git(tb, work, "checkout", "-q", "-b", branch, "main")
		write(fs)
		heads[branch] = Git(tb, work, "rev-parse", "HEAD")
		Git(tb, work, "checkout", "-q", "main")
	}
	bare := s.Dir(name)
	Git(tb, work, "clone", "-q", "--bare", work, bare)
	Git(tb, bare, "config", "http.receivepack", "true")
	s.mu.Lock()
	s.works[name] = work
	s.mu.Unlock()
	return s.URL + "/" + name, heads
}

// Dir is the bare repository name on disk.
func (s *Server) Dir(name string) string { return filepath.Join(s.root, name) }

// Commit adds a commit writing files to main of the repository name — a
// push from elsewhere, a lease's — and answers it.
func (s *Server) Commit(tb testing.TB, name string, files map[string]string) string {
	tb.Helper()
	s.mu.Lock()
	work := s.works[name]
	s.mu.Unlock()
	for file, body := range files {
		if err := os.WriteFile(filepath.Join(work, file), []byte(body), 0o600); err != nil {
			tb.Fatal(err)
		}
	}
	Git(tb, work, "add", "-A")
	Git(tb, work, "commit", "-q", "-m", "pushed from elsewhere")
	Git(tb, work, "push", "-q", s.Dir(name), "main")
	return Git(tb, work, "rev-parse", "HEAD")
}

// Tag puts a lightweight tag named tag on rev in the repository name.
func (s *Server) Tag(tb testing.TB, name, tag, rev string) {
	tb.Helper()
	Git(tb, s.Dir(name), "tag", tag, rev)
}

// TagAt is the commit the tag points at in the repository name, "" for no
// such tag.
func (s *Server) TagAt(tb testing.TB, name, tag string) string {
	tb.Helper()
	cmd := exec.CommandContext(tb.Context(), "git", "rev-parse", "--verify", "-q", "refs/tags/"+tag+"^{commit}") //nolint:gosec // G204: a test's own git
	cmd.Dir = s.Dir(name)
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

// TagKind is git's type of the tag object named tag ("tag" for an annotated
// tag, "commit" for a lightweight one).
func (s *Server) TagKind(tb testing.TB, name, tag string) string {
	tb.Helper()
	return Git(tb, s.Dir(name), "cat-file", "-t", "refs/tags/"+tag)
}

// RequireToken makes the repository name answer only requests carrying
// token as zcp's helper sends it (BasicCredential); any other is 401.
func (s *Server) RequireToken(name, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[name] = token
}

// SawToken reports whether the repository name was sent its exact token.
func (s *Server) SawToken(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sawToken[name]
}

// Fetches is how many fetches the repository name was asked for.
func (s *Server) Fetches(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetches[name]
}

// Git runs a test's own git in dir, failing the test on error, with no
// system or global configuration.
func Git(tb testing.TB, dir string, args ...string) string {
	tb.Helper()
	cmd := exec.CommandContext(tb.Context(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...) //nolint:gosec // G204: a test's own git
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		tb.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TrustCA makes every git the process runs trust httptest's fixed TLS
// certificate (GIT_SSL_CAINFO), writing it under dir. For a package's
// TestMain, before any test runs.
func TrustCA(dir string) error {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	cert := srv.Certificate().Raw
	srv.Close()
	file := filepath.Join(dir, "origintest-ca.pem")
	if err := os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), 0o600); err != nil {
		return fmt.Errorf("write the test CA: %w", err)
	}
	if err := os.Setenv("GIT_SSL_CAINFO", file); err != nil {
		return fmt.Errorf("trust the test CA: %w", err)
	}
	return nil
}
