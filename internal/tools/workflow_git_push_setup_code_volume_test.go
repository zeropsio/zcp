package tools

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestGitPushSetup_ACodeVolumePairWiredAfterTheFillSetsUpTheVolumesRepository
// is a variant-B pair (docs/spec-mate.md §2.8a) wired once its code volume
// already holds the checkout: git-push-setup writes origin, the credential
// helper and the identity into the volume's repository, where the agent
// commits and the delivery pushes from — never into /var/www, where the
// volume is only a nested folder.
func TestGitPushSetup_ACodeVolumePairWiredAfterTheFillSetsUpTheVolumesRepository(t *testing.T) {
	lab := newHQLab(t)
	volume := lab.fillCodeVolume()

	report := lab.wire()

	if len(report) != 1 || !strings.Contains(report[0], `repository "appdev" wired in HQ`) {
		t.Errorf("report = %q", report)
	}
	meta := lab.meta()
	if meta.GitPushState != topology.GitPushConfigured {
		t.Fatalf("git-push = %s, want configured", meta.GitPushState)
	}
	inVolume := func(args ...string) string {
		out, _ := exec.CommandContext(t.Context(), "git", append([]string{"-C", volume, "config", "--local"}, args...)...).Output() //nolint:gosec // G204: a test's own git
		return strings.TrimSpace(string(out))
	}
	if got := inVolume("--get", "remote.origin.url"); got != meta.RemoteURL {
		t.Errorf("the volume's origin = %q, want the repository in HQ %q", got, meta.RemoteURL)
	}
	if got := inVolume("--get-regexp", `^credential\..*helper$`); got == "" {
		t.Errorf("the volume's repository has no credential helper")
	}
	if email, name := inVolume("--get", "user.email"), inVolume("--get", "user.name"); email == "" || name == "" {
		t.Errorf("the volume's identity = %q <%q>, want one set", name, email)
	}
	if got := lab.git("-C", volume, "rev-parse", "--abbrev-ref", "HEAD"); got != "mate/p-mate" {
		t.Errorf("the volume is on %q, want the Mate's branch", got)
	}
	if out, _ := exec.CommandContext(t.Context(), "git", "-C", lab.pair, "config", "--local", "--get", "remote.origin.url").Output(); len(out) != 0 { //nolint:gosec // G204: a test's own git
		t.Errorf("/var/www got origin %q: the setup ran there, not on the volume", strings.TrimSpace(string(out)))
	}
}
