// Tests for: ops/origin_tip.go — the release of a pool-keyed part tags the
// commit its prompt showed on the origin, only while it is still the tip
// (docs/spec-workflows.md §10 P-LP-21), pushing with the token in git's
// environment; and no failing origin read ever surfaces the token (P-LP-20).
package ops

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/origintest"
)

// TestTagOriginTip_PushesAnAnnotatedTagAtTheTip: the tag is an annotated tag
// on the ref's tip, pushed to the origin; the origin's version tags then list
// it, and the same tag again is refused, never moved.
func TestTagOriginTip_PushesAnAnnotatedTagAtTheTip(t *testing.T) {
	t.Parallel()
	srv, originURL, heads := originFixture(t, map[string]string{"zerops.yaml": originTipYAML}, nil)

	tip, err := TagOriginTip(t.Context(), originURL, "main", heads["main"], "v1.0.0", "release v1.0.0", "")

	if err != nil {
		t.Fatalf("TagOriginTip: %v", err)
	}
	if tip.CommitSHA != heads["main"] {
		t.Errorf("tagged %q, want main's tip %q", tip.CommitSHA, heads["main"])
	}
	if kind := srv.TagKind(t, originRepo, "v1.0.0"); kind != "tag" {
		t.Errorf("v1.0.0 on the origin is a %q, want an annotated tag", kind)
	}
	if at := srv.TagAt(t, originRepo, "v1.0.0"); at != heads["main"] {
		t.Errorf("v1.0.0 points at %q, want main's tip %q", at, heads["main"])
	}
	tags, err := ReadOriginTags(t.Context(), originURL, "v*", "")
	if err != nil || !slices.Equal(tags, []string{"v1.0.0"}) {
		t.Errorf("ReadOriginTags = %v, %v; want [v1.0.0]", tags, err)
	}
	if _, err := TagOriginTip(t.Context(), originURL, "main", heads["main"], "v1.0.0", "release v1.0.0", ""); err == nil {
		t.Error("tagging v1.0.0 again was not refused")
	}
}

// TestTagOriginTip_TipMoved_RefusedNothingTagged: a release tags the commit it
// was shown; when the tip moved since — a lease pushed — the tag is refused
// naming both commits, and nothing lands on the origin.
func TestTagOriginTip_TipMoved_RefusedNothingTagged(t *testing.T) {
	t.Parallel()
	srv, originURL, heads := originFixture(t, map[string]string{"zerops.yaml": originTipYAML}, nil)
	moved := srv.Commit(t, originRepo, map[string]string{"later.txt": "pushed from a lease\n"})

	_, err := TagOriginTip(t.Context(), originURL, "main", heads["main"], "v1.0.0", "release v1.0.0", "")

	if !errors.Is(err, ErrOriginTipMoved) {
		t.Fatalf("err = %v, want ErrOriginTipMoved", err)
	}
	for _, want := range []string{heads["main"][:10], moved[:10]} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
	if at := srv.TagAt(t, originRepo, "v1.0.0"); at != "" {
		t.Errorf("v1.0.0 landed on %q despite the moved tip", at)
	}
}

// TestTagOriginTip_OverHTTPSWithTheToken: an origin that requires the token
// gets it — the exact credential zcp's helper sends — and the tag is pushed.
func TestTagOriginTip_OverHTTPSWithTheToken(t *testing.T) {
	t.Parallel()
	const token = "release-push-token-77a"
	srv, originURL, heads := originFixture(t, map[string]string{"zerops.yaml": originTipYAML}, nil)
	srv.RequireToken(originRepo, token)

	if _, err := TagOriginTip(t.Context(), originURL, "main", heads["main"], "v2.0.0", "release v2.0.0", ""); err == nil {
		t.Fatal("a push without the token was not refused")
	}
	tip, err := TagOriginTip(t.Context(), originURL, "main", heads["main"], "v2.0.0", "release v2.0.0", token)

	if err != nil {
		t.Fatalf("TagOriginTip with the token: %v", err)
	}
	if !srv.SawToken(originRepo) {
		t.Error("the origin was never sent the token")
	}
	if at := srv.TagAt(t, originRepo, "v2.0.0"); at != heads["main"] || tip.CommitSHA != heads["main"] {
		t.Errorf("v2.0.0 points at %q, want main's tip %q", at, heads["main"])
	}
}

// originPktLine frames s as one git pkt-line.
func originPktLine(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

// TestReadOriginTip_FailingFetch_NeverSurfacesTheToken: a fetch that fails —
// a remote refusing the credential, one echoing it back in its own error, an
// unreachable host — surfaces git's words without the token: not verbatim,
// not URL-escaped, not as the Basic credential it rode in. The remote is
// proven to have been sent the exact credential, and the echo proven to have
// been taken out of what is surfaced.
func TestReadOriginTip_FailingFetch_NeverSurfacesTheToken(t *testing.T) {
	t.Parallel()
	const token = "tok3n/leak+canary=origin"
	basic := origintest.BasicCredential(token)
	forms := []string{token, strings.TrimPrefix(basic, "Basic "), url.QueryEscape(token), url.PathEscape(token),
		base64.RawStdEncoding.EncodeToString([]byte("oauth2:" + token))}

	tests := []struct {
		name     string
		echo     bool
		reach    bool
		wantSaid string
	}{
		{"remote refuses the credential", false, true, "Authentication failed"},
		{"remote echoes the credential in its error", true, true, "<redacted>"},
		{"host unreachable", false, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var sent atomic.Bool
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth := r.Header.Get("Authorization")
				if auth == basic {
					sent.Store(true)
				}
				if auth == "" || !tt.echo {
					w.Header().Set("WWW-Authenticate", `Basic realm="origin"`)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
				_, _ = io.WriteString(w, originPktLine("# service=git-upload-pack\n")+"0000"+
					originPktLine("ERR denied for "+token+" sent as "+auth+"\n"))
			}))
			t.Cleanup(srv.Close)
			remote := srv.URL + "/acme/app.git"
			if !tt.reach {
				remote = "https://zcp-origin-unreachable.invalid/acme/app.git"
			}

			_, err := ReadOriginTip(t.Context(), remote, "main", token)

			if err == nil {
				t.Fatal("the failing fetch answered no error")
			}
			if tt.reach && !sent.Load() {
				t.Fatal("the remote was never sent the exact credential; the test proves nothing")
			}
			if tt.wantSaid != "" && !strings.Contains(err.Error(), tt.wantSaid) {
				t.Errorf("the surfaced error does not carry %q: %v", tt.wantSaid, err)
			}
			for _, form := range forms {
				if strings.Contains(err.Error(), form) {
					t.Errorf("the surfaced error carries the token as %q: %v", form, err)
				}
			}
		})
	}
}
