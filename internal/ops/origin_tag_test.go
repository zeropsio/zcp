// Tests for: ops/origin_tip.go — the release of a pool-keyed part tags its
// origin's tracked tip from a scratch repository of zcp's own
// (docs/spec-workflows.md §10 P-LP-20), and no failing origin read ever
// surfaces the token (P-LP-19).
package ops

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// TestTagOriginTip_PushesAnAnnotatedTagAtTheTip: the tag is an annotated tag
// on the ref's tip, pushed to the origin; the origin's version tags then list
// it, and the same tag again is refused, never moved.
func TestTagOriginTip_PushesAnAnnotatedTagAtTheTip(t *testing.T) {
	t.Parallel()
	requireOriginGit(t)
	originURL, heads := originFixture(t, map[string]string{"zerops.yaml": originTipYAML}, nil)
	originDir := strings.TrimPrefix(originURL, "file://")

	tip, err := TagOriginTip(t.Context(), originURL, "main", "v1.0.0", "release v1.0.0", "")

	if err != nil {
		t.Fatalf("TagOriginTip: %v", err)
	}
	if tip.CommitSHA != heads["main"] {
		t.Errorf("tagged %q, want main's tip %q", tip.CommitSHA, heads["main"])
	}
	if kind := originRepoGit(t, originDir, "cat-file", "-t", "v1.0.0"); kind != "tag" {
		t.Errorf("v1.0.0 on the origin is a %q, want an annotated tag", kind)
	}
	if at := originRepoGit(t, originDir, "rev-parse", "v1.0.0^{commit}"); at != heads["main"] {
		t.Errorf("v1.0.0 points at %q, want main's tip %q", at, heads["main"])
	}
	tags, err := ReadOriginTags(t.Context(), originURL, "v*", "")
	if err != nil || !slices.Equal(tags, []string{"v1.0.0"}) {
		t.Errorf("ReadOriginTags = %v, %v; want [v1.0.0]", tags, err)
	}
	if _, err := TagOriginTip(t.Context(), originURL, "main", "v1.0.0", "release v1.0.0", ""); err == nil {
		t.Error("tagging v1.0.0 again was not refused")
	}
}

// originPktLine frames s as one git pkt-line.
func originPktLine(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

// TestReadOriginTip_FailingFetch_NeverSurfacesTheToken: a fetch that fails —
// a remote refusing the credential, one echoing it back in its own error, an
// unreachable host — surfaces git's words without the token: not verbatim,
// not URL-escaped, not as the Basic credential it rode in; and nothing is
// written to stderr. The remote is proven to have been sent the credential,
// so the test is not passing on a token never used.
func TestReadOriginTip_FailingFetch_NeverSurfacesTheToken(t *testing.T) {
	// non-parallel: t.Setenv and the stderr redirect touch the process.
	requireOriginGit(t)
	t.Setenv("GIT_SSL_NO_VERIFY", "1")
	const token = "tok3n/leak+canary=origin"
	basic := base64.StdEncoding.EncodeToString([]byte("oauth2:" + token))
	forms := []string{token, basic, url.QueryEscape(token), url.PathEscape(token)}

	tests := []struct {
		name     string
		echo     bool
		reach    bool
		wantSaid string
	}{
		{"remote refuses the credential", false, true, "Authentication failed"},
		{"remote echoes the credential in its error", true, true, "remote error"},
		{"host unreachable", false, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent atomic.Bool
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth := r.Header.Get("Authorization")
				if auth == "" || !tt.echo {
					if auth != "" {
						sent.Store(true)
					}
					w.Header().Set("WWW-Authenticate", `Basic realm="origin"`)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				sent.Store(true)
				w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
				_, _ = io.WriteString(w, originPktLine("# service=git-upload-pack\n")+"0000"+
					originPktLine("ERR denied for "+token+" sent as "+auth+"\n"))
			}))
			t.Cleanup(srv.Close)
			remote := srv.URL + "/acme/app.git"
			if !tt.reach {
				remote = "https://zcp-origin-unreachable.invalid/acme/app.git"
			}

			var err error
			stderr := captureOriginStderr(t, func() {
				_, err = ReadOriginTip(t.Context(), remote, "main", token)
			})

			if err == nil {
				t.Fatal("the failing fetch answered no error")
			}
			if tt.reach && !sent.Load() {
				t.Fatal("the remote was never sent the credential; the test proves nothing")
			}
			if tt.wantSaid != "" && !strings.Contains(err.Error(), tt.wantSaid) {
				t.Errorf("the error does not carry git's words %q: %v", tt.wantSaid, err)
			}
			for _, form := range forms {
				if strings.Contains(err.Error(), form) {
					t.Errorf("the error carries the token as %q: %v", form, err)
				}
				if strings.Contains(stderr, form) {
					t.Errorf("stderr carries the token as %q: %s", form, stderr)
				}
			}
			if stderr != "" {
				t.Errorf("the read wrote to stderr: %q", stderr)
			}
		})
	}
}

// captureOriginStderr answers what fn wrote to the process's stderr.
func captureOriginStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = orig
	out, _ := io.ReadAll(r)
	return string(out)
}
