package hq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// git asks the helper with the remote's protocol and host; only the HQ this
// Mate enrolled with gets its credential, under the user HQ's git reads.
func TestGitCredential_AnswersOnlyTheEnrolledHQ(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hq       string
		enrolled bool
		request  string
		want     string
		wantOK   bool
	}{
		{"the enrolled HQ", "https://hq.example", true, "protocol=https\nhost=hq.example\npath=git/app/appdev.git\n\n", "secret", true},
		{"https's own port named", "https://hq.example", true, "protocol=https\nhost=hq.example:443\n\n", "secret", true},
		{"an HQ on a port of its own", "http://127.0.0.1:8080/", true, "protocol=http\nhost=127.0.0.1:8080\n\n", "secret", true},
		{"the host in another case", "https://HQ.example", true, "protocol=https\nhost=hq.EXAMPLE\n\n", "secret", true},
		{"git's other attributes ignored", "https://hq.example", true, "capability[]=authtype\nprotocol=https\nhost=hq.example\nwwwauth[]=Basic realm=\"hq\"\n\n", "secret", true},
		{"another port on the HQ's host", "https://hq.example", true, "protocol=https\nhost=hq.example:8443\n\n", "", false},
		{"the HQ's host over plain http", "https://hq.example", true, "protocol=http\nhost=hq.example\n\n", "", false},
		{"another host", "https://hq.example", true, "protocol=https\nhost=gitlab.com\npath=team/app.git\n\n", "", false},
		{"no host at all", "https://hq.example", true, "protocol=https\n\n", "", false},
		{"not enrolled", "https://hq.example", false, "protocol=https\nhost=hq.example\n\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "enrollment.json")
			if tt.enrolled {
				if err := SaveEnrollment(path, Enrollment{HQ: tt.hq, HQProjectID: "hq1", ProjectID: "p1", Credential: "secret"}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := AnswerGit(context.Background(), strings.NewReader(tt.request), path, http.DefaultClient)
			if got.Password != tt.want || (err == nil) != tt.wantOK {
				t.Errorf("AnswerGit = %q, %v; want %q, ok %v", got.Password, err, tt.want, tt.wantOK)
			}
			if tt.wantOK && got.Username != GitUser {
				t.Errorf("username = %q, want %q", got.Username, GitUser)
			}
		})
	}
}

// A repository on GitHub gets the token HQ mints for it at that moment — the
// Mate's own credential never leaves for GitHub, and nothing is kept between
// calls; HQ's refusal is said in words, never answered with a password.
func TestAnswerGit_AGitHubRepositoryGetsTheTokenHQMintsForIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		request    string
		status     int
		body       string
		wantToken  string
		wantAsked  bool
		wantReason string
	}{
		{"a mounted repository gets HQ's token", "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", 200,
			`{"username":"x-access-token","password":"ghs_write","expiresAt":"2026-10-21T13:00:00Z"}`, "ghs_write", true, ""},
		{"a followed repository gets HQ's read token", "protocol=https\nhost=github.com\npath=acme/lib\n\n", 200,
			`{"username":"x-access-token","password":"ghs_read","expiresAt":"2026-10-21T13:00:00Z"}`, "ghs_read", true, ""},
		{"a repository HQ does not mount", "protocol=https\nhost=github.com\npath=acme/other.git\n\n", 404,
			`{"reason":"not_mounted"}`, "", true, "mounts no repository github.com/acme/other.git"},
		{"a Mate whose project access is gone", "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", 403,
			`{"reason":"no_access"}`, "", true, "no longer has access"},
		{"an installation that no longer reaches it", "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", 409,
			`{"reason":"app_not_installed"}`, "", true, "GitHub App no longer reaches"},
		{"GitHub not answering HQ", "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", 503,
			`{}`, "", true, "try again"},
		{"an answer with no password", "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", 200,
			`{"username":"x-access-token","password":""}`, "", true, "no password"},
		{"git named no repository", "protocol=https\nhost=github.com\n\n", 200,
			`{}`, "", false, "credential.https://github.com.useHttpPath"},
		{"GitHub over plain http", "protocol=http\nhost=github.com\npath=acme/shop.git\n\n", 200,
			`{}`, "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var asked atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asked.Store(true)
				if r.Method != http.MethodGet || r.URL.Path != "/api/mate/git-credential" || r.Header.Get("Authorization") != "Mate secret" {
					t.Errorf("asked %s %s as %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
				}
				if got := r.URL.Query().Get("host"); got != "github.com" {
					t.Errorf("host = %q", got)
				}
				if got, want := r.URL.Query().Get("path"), requestPath(tt.request); got != want {
					t.Errorf("path = %q, want %q", got, want)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)
			path := filepath.Join(t.TempDir(), "enrollment.json")
			if err := SaveEnrollment(path, Enrollment{HQ: srv.URL, HQProjectID: "hq1", ProjectID: "p1", Credential: "secret"}); err != nil {
				t.Fatal(err)
			}

			got, err := AnswerGit(context.Background(), strings.NewReader(tt.request), path, srv.Client())
			if asked.Load() != tt.wantAsked {
				t.Errorf("HQ asked = %v, want %v", asked.Load(), tt.wantAsked)
			}
			if got.Password != tt.wantToken {
				t.Errorf("password = %q, want %q", got.Password, tt.wantToken)
			}
			if tt.wantToken != "" {
				if err != nil || got.Username != "x-access-token" {
					t.Errorf("answer = %q, %v", got.Username, err)
				}
				return
			}
			var declined *DeclinedError
			if !errors.As(err, &declined) {
				t.Fatalf("err = %v, want a decline", err)
			}
			if !strings.Contains(declined.Reason, tt.wantReason) {
				t.Errorf("reason = %q, want it to say %q", declined.Reason, tt.wantReason)
			}
			if strings.Contains(declined.Reason, "secret") {
				t.Errorf("reason %q carries the Mate credential", declined.Reason)
			}
		})
	}
}

// git waits on the helper: an HQ that never answers, once the call is given
// up, is said as "try again" and answered with nothing.
func TestAnswerGit_AnHQThatNeverAnswersIsGivenUp(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	path := filepath.Join(t.TempDir(), "enrollment.json")
	if err := SaveEnrollment(path, Enrollment{HQ: srv.URL, HQProjectID: "hq1", ProjectID: "p1", Credential: "secret"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := AnswerGit(ctx, strings.NewReader("protocol=https\nhost=github.com\npath=acme/shop.git\n\n"), path, srv.Client())
	var declined *DeclinedError
	if !errors.As(err, &declined) || !strings.Contains(declined.Reason, "try again") {
		t.Fatalf("err = %v, want a decline saying to try again", err)
	}
}

// requestPath is the path attribute of a credential request, "" without one.
func requestPath(request string) string {
	for line := range strings.SplitSeq(request, "\n") {
		if value, ok := strings.CutPrefix(line, "path="); ok {
			return value
		}
	}
	return ""
}
