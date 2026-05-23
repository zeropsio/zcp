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
			reason: "the .netrc + origin sync now happen at git-push-setup probe time (Phase 1). Deploy uses the project-level GIT_TOKEN + stamped origin.",
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
	}

	atomDir := filepath.Join("atoms")
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
