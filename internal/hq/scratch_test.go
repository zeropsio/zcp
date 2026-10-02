// Tests for: hq/scratch.go — git on this container against HQ carries the
// Mate credential in its environment only, scoped to HQ.
package hq

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"
)

// TestGitEnv_CarriesTheCredentialScopedToHQ: the credential reaches git as an
// Authorization header for HQ's address alone, through the environment —
// never as an argument a process listing shows, nor in a URL.
func TestGitEnv_CarriesTheCredentialScopedToHQ(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		wantKey string
	}{
		{"HQ on 443", "https://hq.example.com", "http.https://hq.example.com/.extraHeader"},
		{"HQ on another port, trailing slash", "https://hq.example.com:8443/", "http.https://hq.example.com:8443/.extraHeader"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := Client{enrollment: Enrollment{HQ: tt.address, Credential: "the-credential"}}
			env := c.gitEnv()
			want := []string{
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=" + tt.wantKey,
				"GIT_CONFIG_VALUE_0=Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(GitUser+":the-credential")),
				"GIT_TERMINAL_PROMPT=0",
			}
			for _, w := range want {
				if !slices.Contains(env, w) {
					t.Errorf("env lacks %q", w)
				}
			}
			if url := c.RepoURL("app-1", "group"); strings.Contains(url, "the-credential") {
				t.Errorf("the repository's URL %q carries the credential", url)
			}
		})
	}
}
