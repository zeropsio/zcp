// Tests for: ops/origin_tip.go — the read of an origin's ref without any
// checkout (docs/spec-workflows.md §10 P-LP-20): one shallow fetch in a
// scratch repository of zcp's own, the fetched commit and its root
// zerops.yaml (else zerops.yml), the credential in git's environment alone,
// the branch named so a like-named tag never wins, https alone, a named and
// isolated scratch, and bounds that end a stalling origin.
package ops

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops/origintest"
)

// originRepo is the name every test's origin takes on its own server.
const originRepo = "app.git"

// originFixture serves a bare origin over HTTPS whose main holds files and
// each named branch main's files with its own over them; it answers the
// server, the origin's URL and each branch's head, main's under "main".
func originFixture(t *testing.T, files map[string]string, branches map[string]map[string]string) (*origintest.Server, string, map[string]string) {
	t.Helper()
	srv := origintest.New(t)
	url, heads := srv.Repo(t, originRepo, files, branches)
	return srv, url, heads
}

const originTipYAML = "zerops:\n  - setup: prod\n    run:\n      base: nodejs@22\n"

// TestReadOriginTip_ReadsTheTipCommitAndItsZeropsYAML: the commit read is the
// tip of the ref, and the zerops.yaml is the one that commit holds — its root
// zerops.yaml, else its zerops.yml, else none, which is no error.
func TestReadOriginTip_ReadsTheTipCommitAndItsZeropsYAML(t *testing.T) {
	t.Parallel()
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
			_, url, heads := originFixture(t, tt.files, nil)

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
	release := "zerops:\n  - setup: prod\n    run:\n      base: nodejs@24\n"
	_, url, heads := originFixture(t, map[string]string{"zerops.yaml": originTipYAML},
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

// TestReadOriginTip_TagNamedLikeTheBranch_TheBranchWins: git ranks a tag
// above a branch of the same short name, so the tracked branch is fetched by
// its full name — a tag `main` on another commit never stands in for the
// branch `main`; a full refs/ name is fetched as given.
func TestReadOriginTip_TagNamedLikeTheBranch_TheBranchWins(t *testing.T) {
	t.Parallel()
	srv, url, heads := originFixture(t, map[string]string{"zerops.yaml": originTipYAML},
		map[string]map[string]string{"other": {"other.txt": "other\n"}})
	srv.Tag(t, originRepo, "main", heads["other"])

	tip, err := ReadOriginTip(t.Context(), url, "main", "")
	if err != nil {
		t.Fatalf("ReadOriginTip(main): %v", err)
	}
	if tip.CommitSHA != heads["main"] {
		t.Errorf("main read %q, want the branch's tip %q, not the tag's %q", tip.CommitSHA, heads["main"], heads["other"])
	}
	tagged, err := ReadOriginTip(t.Context(), url, "refs/tags/main", "")
	if err != nil || tagged.CommitSHA != heads["other"] {
		t.Errorf("refs/tags/main read %q (%v), want the tag's %q", tagged.CommitSHA, err, heads["other"])
	}
}

// TestReadOriginTip_RefAbsent_NamedNotEmpty: a ref the origin does not hold is
// ErrOriginRefMissing — never an empty commit or an empty zerops.yaml.
func TestReadOriginTip_RefAbsent_NamedNotEmpty(t *testing.T) {
	t.Parallel()
	_, url, _ := originFixture(t, map[string]string{"zerops.yaml": originTipYAML}, nil)

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
	srv := origintest.New(t)
	url := srv.URL + "/no-such-origin.git"

	_, err := ReadOriginTip(t.Context(), url, "HEAD", "")

	if err == nil || errors.Is(err, ErrOriginRefMissing) {
		t.Fatalf("err = %v, want an unreadable origin", err)
	}
	if !strings.Contains(err.Error(), "no-such-origin") {
		t.Errorf("the error does not say git's words about the origin: %v", err)
	}
}

// TestReadOriginTip_HTTPSOnly: the scratch speaks https alone — a file://
// origin (or any other transport) is refused by git, never read.
func TestReadOriginTip_HTTPSOnly(t *testing.T) {
	t.Parallel()
	srv, _, _ := originFixture(t, map[string]string{"zerops.yaml": originTipYAML}, nil)

	_, err := ReadOriginTip(t.Context(), "file://"+srv.Dir(originRepo), "HEAD", "")

	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("err = %v, want git refusing the file transport", err)
	}
}

// TestReadOriginTip_ExplicitBareRepository_StillReads: the scratch is named
// (--git-dir), so a git configured with safe.bareRepository=explicit still
// reads, and an inherited GIT_DIR or object directory never redirects it.
func TestReadOriginTip_ExplicitBareRepository_StillReads(t *testing.T) {
	// non-parallel: t.Setenv changes the process environment.
	_, url, heads := originFixture(t, map[string]string{"zerops.yaml": originTipYAML}, nil)
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[safe]\n\tbareRepository = explicit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	elsewhere := t.TempDir()
	t.Setenv("GIT_DIR", elsewhere)
	t.Setenv("GIT_OBJECT_DIRECTORY", elsewhere)

	tip, err := ReadOriginTip(t.Context(), url, "main", "")

	if err != nil {
		t.Fatalf("ReadOriginTip: %v", err)
	}
	if tip.CommitSHA != heads["main"] {
		t.Errorf("CommitSHA = %q, want main's tip %q", tip.CommitSHA, heads["main"])
	}
}

// TestReadOriginTip_StallingOrigin_EndsWithinItsBound: an origin that answers
// and then stops feeding ends the read within its bound — git's stall bound,
// the call's deadline — with an error, never a hang.
func TestReadOriginTip_StallingOrigin_EndsWithinItsBound(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "001e# service=git-upload-pack\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	start := time.Now()
	_, err := readOriginTipWithin(context.Background(), originBounds{call: 4 * time.Second, stallSeconds: 1}, srv.URL+"/app.git", "main", "")

	if err == nil {
		t.Fatal("a stalling origin answered no error")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("the read took %s, want it ended by its bound", took)
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
