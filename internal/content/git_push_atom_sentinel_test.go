package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAtomCorpus_NoForbiddenGitPushClaims is a narrow sentinel scan
// (not a generic atom-handler lint) against specific phrases that
// proved load-bearing wrong in live evals. Each entry is a verbatim
// substring previously found in an atom that misled agents — the
// scan refuses to pass if any current atom file contains it again.
//
// Distinct from TestAtomAuthoringLint: that lint enforces structural
// rules (axes, no spec IDs). This sentinel enforces semantic accuracy
// of specific historical claims that were factually false relative to
// the handler implementation.
func TestAtomCorpus_NoForbiddenGitPushClaims(t *testing.T) {
	t.Parallel()

	// Each entry: substring + reason it was wrong + suggested fix.
	forbidden := []struct {
		substr string
		reason string
	}{
		{
			substr: "ensures there's a fresh commit",
			reason: "the deploy handler does NOT auto-commit dirty trees — it refuses with ErrPrerequisiteMissing. Atom must spell out the SSH commit-first command.",
		},
		{
			substr: "wired by the deploy call itself",
			reason: "credential + origin sync happen at git-push-setup probe time. Deploy authenticates via the session credential helper reading the service-scope GIT_TOKEN + the stamped origin.",
		},
		{
			substr: "GIT_TOKEN + .netrc + remote URL are stamped",
			reason: "post-Phase-1 git-push-setup writes GIT_TOKEN as sensitive project env, syncs origin in working tree git config, and stamps RemoteURL — but only AFTER a successful probe. Atom must say 'probe-proven' not just 'stamped'.",
		},
		{
			substr: "deploy command handles `git init`",
			reason: "git init ran at bootstrap (InitServiceGit). The deploy handler does not run git init; it refuses to push without HEAD.",
		},
		{
			substr: "PUSH_NOT_CONFIGURED",
			reason: "ghost error code — never existed in platform.errors. Real code is PREREQUISITE_MISSING (from handleGitPush gitPushMetaPreflight).",
		},
		{
			substr: "provisions GIT_TOKEN / .netrc / remote URL",
			reason: "pre-Phase-1 description of git-push-setup as a passive state-stamper. Post-Phase-1 the handler probes auth BEFORE writing GIT_TOKEN as sensitive env. Replace with probe-first contract.",
		},
		// F1: git-push was RETIRED as a close-mode value (folds to auto;
		// delivery derives from GitPushState). It must never be enumerated as a
		// close-mode CHOICE or in the auto-close gating set again — present
		// auto/manual only; delivery is a separate dimension (git-push-setup).
		{
			substr: "closeDeployMode ∈ {auto, git-push}",
			reason: "auto-close gate is closeDeployMode == auto (work_session.go) and git-push folds to auto, so it never persists. Present `closeDeployMode = auto` (manual keeps the session open) — git-push is not a gating value.",
		},
		{
			substr: "`auto` or `git-push`",
			reason: "git-push is retired as a close-mode value (folds to auto). Auto-close requires `auto` only; don't list git-push as a close-mode the gate accepts.",
		},
		{
			substr: "`git-push`, or `manual`",
			reason: "close-mode menu must offer auto/manual only (git-push retired → folds to auto). Delivery via git push is action=\"git-push-setup\", not a close-mode choice.",
		},
		{
			substr: "`git-push` — `zerops_deploy strategy=\"git-push\"` commits",
			reason: "the close-mode DECISION menu must not list git-push as a delivery-pattern choice — it folds to auto. State delivery as a separate dimension (git-push-setup) instead.",
		},
	}

	const atomDir = "atoms"
	entries, err := os.ReadDir(atomDir)
	if err != nil {
		t.Fatalf("read atoms dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(atomDir, entry.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read atom %s: %v", path, err)
		}
		text := string(body)
		for _, rule := range forbidden {
			if strings.Contains(text, rule.substr) {
				t.Errorf("atom %s contains forbidden claim %q\n  reason: %s", entry.Name(), rule.substr, rule.reason)
			}
		}
	}
}
