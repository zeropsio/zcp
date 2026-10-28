// Tests for: ops/origin_tip.go — the read of an origin's ref without any
// checkout (docs/spec-workflows.md §10 P-LP-19): one shallow fetch in a
// scratch repository of zcp's own, the fetched commit and its root
// zerops.yaml (else zerops.yml), and the credential in git's environment
// alone.
package ops

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// requireOriginGit skips a test that needs the git binary.
func requireOriginGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// originRepoGit runs git in dir for a fixture, failing the test on error.
func originRepoGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...) //nolint:gosec // G204: a test's own git
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// originFixture makes a bare origin whose main holds files and each named
// branch holds main's files with its own written over them. It answers the
// origin's file:// URL and each branch's head, main's under "main".
func originFixture(t *testing.T, files map[string]string, branches map[string]map[string]string) (string, map[string]string) {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	originRepoGit(t, work, "init", "-q", "-b", "main")
	write := func(fs map[string]string) {
		for name, body := range fs {
			if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		originRepoGit(t, work, "add", "-A")
		originRepoGit(t, work, "commit", "-q", "-m", "fixture")
	}
	write(files)
	heads := map[string]string{"main": originRepoGit(t, work, "rev-parse", "HEAD")}
	for branch, fs := range branches {
		originRepoGit(t, work, "checkout", "-q", "-b", branch, "main")
		write(fs)
		heads[branch] = originRepoGit(t, work, "rev-parse", "HEAD")
		originRepoGit(t, work, "checkout", "-q", "main")
	}
	origin := filepath.Join(root, "origin")
	originRepoGit(t, root, "clone", "-q", "--bare", work, origin)
	return "file://" + origin, heads
}

const originTipYAML = "zerops:\n  - setup: prod\n    run:\n      base: nodejs@22\n"

// TestReadOriginTip_ReadsTheTipCommitAndItsZeropsYAML: the commit read is the
// tip of the ref, and the zerops.yaml is the one that commit holds — its root
// zerops.yaml, else its zerops.yml, else none, which is no error.
func TestReadOriginTip_ReadsTheTipCommitAndItsZeropsYAML(t *testing.T) {
	t.Parallel()
	requireOriginGit(t)
	tests := []struct {
		name     string
		files    map[string]string
		ref      string
		wantBody string
	}{
		{"zerops.yaml at the default branch", map[string]string{"zerops.yaml": originTipYAML}, "HEAD", strings.TrimRight(originTipYAML, "\n")},
		{"zerops.yaml at main by name", map[string]string{"zerops.yaml": originTipYAML}, "main", strings.TrimRight(originTipYAML, "\n")},
		{"zerops.yml when there is no zerops.yaml", map[string]string{"zerops.yml": originTipYAML}, "HEAD", strings.TrimRight(originTipYAML, "\n")},
		{"zerops.yaml before zerops.yml", map[string]string{"zerops.yaml": originTipYAML, "zerops.yml": "zerops: []\n"}, "HEAD", strings.TrimRight(originTipYAML, "\n")},
		{"neither", map[string]string{"README.md": "hello\n"}, "HEAD", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			url, heads := originFixture(t, tt.files, nil)

			tip, err := ReadOriginTip(t.Context(), url, tt.ref, "")

			if err != nil {
				t.Fatalf("ReadOriginTip: %v", err)
			}
			if tip.CommitSHA != heads["main"] {
				t.Errorf("CommitSHA = %q, want main's tip %q", tip.CommitSHA, heads["main"])
			}
			if tip.Ref != tt.ref {
				t.Errorf("Ref = %q, want %q", tip.Ref, tt.ref)
			}
			if tip.ZeropsYAML != tt.wantBody {
				t.Errorf("ZeropsYAML = %q, want %q", tip.ZeropsYAML, tt.wantBody)
			}
		})
	}
}

// TestReadOriginTip_ReadsTheNamedBranch: a tracked branch other than the
// default is read at its own tip, with its own zerops.yaml.
func TestReadOriginTip_ReadsTheNamedBranch(t *testing.T) {
	t.Parallel()
	requireOriginGit(t)
	release := "zerops:\n  - setup: prod\n    run:\n      base: nodejs@24\n"
	url, heads := originFixture(t, map[string]string{"zerops.yaml": originTipYAML},
		map[string]map[string]string{"release": {"zerops.yaml": release}})

	tip, err := ReadOriginTip(t.Context(), url, "release", "")

	if err != nil {
		t.Fatalf("ReadOriginTip: %v", err)
	}
	if tip.CommitSHA != heads["release"] || tip.CommitSHA == heads["main"] {
		t.Errorf("CommitSHA = %q, want release's tip %q", tip.CommitSHA, heads["release"])
	}
	if tip.ZeropsYAML != strings.TrimRight(release, "\n") {
		t.Errorf("ZeropsYAML = %q, want the release branch's", tip.ZeropsYAML)
	}
}

// TestReadOriginTip_RefAbsent_NamedNotEmpty: a ref the origin does not hold is
// ErrOriginRefMissing — never an empty commit or an empty zerops.yaml.
func TestReadOriginTip_RefAbsent_NamedNotEmpty(t *testing.T) {
	t.Parallel()
	requireOriginGit(t)
	url, _ := originFixture(t, map[string]string{"zerops.yaml": originTipYAML}, nil)

	tip, err := ReadOriginTip(t.Context(), url, "release", "")

	if !errors.Is(err, ErrOriginRefMissing) {
		t.Fatalf("err = %v, want ErrOriginRefMissing", err)
	}
	if tip.CommitSHA != "" || tip.ZeropsYAML != "" {
		t.Errorf("tip = %+v, want none", tip)
	}
	if !strings.Contains(err.Error(), "release") {
		t.Errorf("the error does not name the ref: %v", err)
	}
}

// TestReadOriginTip_UnreachableOrigin_SaysGitsWords: an origin that cannot be
// read is an error carrying git's own words, never ErrOriginRefMissing.
func TestReadOriginTip_UnreachableOrigin_SaysGitsWords(t *testing.T) {
	t.Parallel()
	requireOriginGit(t)
	url := "file://" + filepath.Join(t.TempDir(), "no-such-origin")

	_, err := ReadOriginTip(t.Context(), url, "HEAD", "")

	if err == nil || errors.Is(err, ErrOriginRefMissing) {
		t.Fatalf("err = %v, want an unreadable origin", err)
	}
	if !strings.Contains(err.Error(), "no-such-origin") {
		t.Errorf("the error does not say git's words about the origin: %v", err)
	}
}

// TestReadOriginTip_CredentialInTheEnvironmentOnly: the token reaches git as
// GIT_TOKEN in its environment, answered by a helper scoped to the origin's
// host — never in an argument a process listing shows; with no token there is
// no GIT_TOKEN at all, not even one zcp's own process inherited, and no git
// tracing that could print what the helper answers.
func TestReadOriginTip_CredentialInTheEnvironmentOnly(t *testing.T) {
	// non-parallel: t.Setenv changes the process environment.
	t.Setenv("GIT_TOKEN", "inherited-token")
	t.Setenv("GIT_TRACE_CURL", "1")
	const token = "origin-secret-token"

	args, env := originTipGit("https://github.com/acme/app.git", token)

	for _, a := range args {
		if strings.Contains(a, token) {
			t.Errorf("an argument carries the token: %q", a)
		}
	}
	if !slices.Contains(args, "credential.helper=") {
		t.Errorf("args %q do not reset the configured helpers", args)
	}
	if !slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "credential.https://github.com.helper=") }) {
		t.Errorf("args %q carry no helper scoped to the origin's host", args)
	}
	if !slices.Contains(env, "GIT_TOKEN="+token) || slices.Contains(env, "GIT_TOKEN=inherited-token") {
		t.Errorf("env does not carry the token alone as GIT_TOKEN")
	}
	if !slices.Contains(env, "GIT_TERMINAL_PROMPT=0") {
		t.Error("env lets git prompt")
	}
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_TRACE") {
			t.Errorf("env keeps tracing: %q", e)
		}
	}

	args, env = originTipGit("https://github.com/acme/app.git", "")
	if slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, "GIT_TOKEN=") }) {
		t.Error("with no token, env still carries a GIT_TOKEN")
	}
	if slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, ".helper=!") }) {
		t.Errorf("with no token, args %q still name a helper", args)
	}
}
