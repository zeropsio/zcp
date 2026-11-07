package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRelayOnly_ADevServicesPushToHQIsAnsweredThroughTheRelayWithTheProjectKey:
// a dev service's git push to HQ is answered through the relay with the
// project key; no Git credential is stored in the dev service. HQ here admits
// the project key alone — the Git credential its server holds is one no git
// may present — so the wiring's own fetch and the agent's `zcp git` push
// land only through the relay, and the dev service holds no GIT_TOKEN and its
// checkout's configuration no secret.
// witness: internal/tools/hq_wiring.go | Relay: ops.BrokeredGitRelay{Identity: hqIdentity(rt), HTTPClient: httpClient}}, nil → Relay: ops.BrokeredGitRelay{HTTPClient: httpClient}}, nil
func TestRelayOnly_ADevServicesPushToHQIsAnsweredThroughTheRelayWithTheProjectKey(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.hq.mu.Lock()
	lab.hq.credential = "a-credential-no-git-presents"
	lab.hq.mu.Unlock()

	if report := lab.wire(); len(report) != 1 || !strings.Contains(report[0], `repository "appdev" wired in HQ`) {
		t.Fatalf("the pass did not wire the pair through the relay: %q", report)
	}
	lab.git("checkout", "-q", "-b", "add-signup")
	lab.write(map[string]string{"signup.js": "the signup\n"})
	lab.git("add", "-A")
	lab.git("-c", "user.name=agent", "-c", "user.email=agent@example.invalid", "commit", "-q", "-m", "Add signup")
	if out, err := lab.devGit().ExecSSH(t.Context(), "appdev", "cd /var/www && git push -q origin add-signup"); err != nil {
		t.Fatalf("the dev service's push through the relay: %v\n%s", err, out)
	}
	if got, want := lab.remoteHead("add-signup"), lab.git("rev-parse", "HEAD"); got != want {
		t.Errorf("HQ's add-signup = %q, want the pushed %q", got, want)
	}

	if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != "" {
		t.Errorf("the dev service holds GIT_TOKEN %q, want no Git credential stored there", held)
	}
	config, err := os.ReadFile(filepath.Join(lab.pair, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{labKey, labCredential} {
		if strings.Contains(string(config), secret) {
			t.Errorf("the checkout's configuration holds a secret:\n%s", config)
		}
	}
}
