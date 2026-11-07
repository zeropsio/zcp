package ops

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
)

// TestRelayedSSH_ZcpsScriptReachesHQWithTheProjectKey: a script zcp runs in
// a dev service — a fetch into its partial clone and a checkout that fetches
// the blobs lazily — reaches HQ through the relay with the Mate's project
// key, over a persisted helper the checkout still holds and with no Git
// credential in the session.
// witness: internal/ops/git_relayed_script.go | {"credential.helper", relayHelper(nonce)}, → {"credential.helper", ""},
func TestRelayedSSH_ZcpsScriptReachesHQWithTheProjectKey(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, history := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			l.git(t, checkout, "config", "credential."+l.hq.URL+".helper", "!f() { echo username=mate; echo password=stale-stored-credential; }; f")
			ssh := RelayedSSH{SSH: &shellRunner{sh: sh, env: l.env()}, Relay: l.relay()}
			script := "cd " + ShellQuote(checkout) + " && git fetch -q --filter=blob:none origin main && git checkout -q -f --detach FETCH_HEAD && cat app.txt && test -z \"$GIT_TOKEN\""
			out, err := ssh.ExecSSH(t.Context(), "appdev", script)
			if err != nil {
				t.Fatalf("the relayed script failed: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "third contents") {
				t.Errorf("the script printed %q, want main's app.txt", out)
			}
			if got := l.git(t, checkout, "rev-parse", "HEAD"); got != history.third {
				t.Errorf("HEAD = %s, want main's %s", got, history.third)
			}
			want := "Basic " + base64.StdEncoding.EncodeToString([]byte(hq.GitUser+":"+relayLabKey))
			l.mu.Lock()
			defer l.mu.Unlock()
			if len(l.sawAuth) == 0 {
				t.Fatal("HQ was never sent a credential")
			}
			for _, a := range l.sawAuth {
				if a != want {
					t.Errorf("HQ was sent %q, want only the project key", a)
				}
			}
		})
	}
}
