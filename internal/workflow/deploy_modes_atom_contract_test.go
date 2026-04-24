package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestDeployModesAtomContract pins the canonical DM-1…DM-5 claims in the
// develop atom corpus (docs/spec-workflows.md §8 Deploy Modes).
//
// The asymmetry between self-deploy (source == target, refreshes a mutable
// workspace, deployFiles MUST be [.] per DM-2) and cross-deploy (source ≠
// target OR strategy=git-push, produces an immutable artifact, deployFiles
// cherry-picks from post-build tree) is a first-class concept that lives
// in three mutually reinforcing places: spec §8 invariants, code (ops.
// ValidateZeropsYml + ClassifyDeploy), and atom corpus. If atoms drop the
// canonical phrases the LLM loses the mental model; this test prevents
// that silent regression.
//
// Authoritative reference: docs/spec-workflows.md §8 Deploy Modes (DM-1…DM-5).
func TestDeployModesAtomContract(t *testing.T) {
	t.Parallel()

	type rule struct {
		atomID      string
		required    []string // all must appear
		forbidden   []string // none may appear
		explanation string
	}

	rules := []rule{
		{
			atomID: "develop-deploy-modes",
			required: []string{
				"self-deploy",
				"cross-deploy",
				"DM-2",
				"./out/~",
				"docs/spec-workflows.md",
			},
			explanation: "canonical deploy-modes atom must teach the class split, cite DM-2 for the self-deploy [.] rule, show the tilde-extract pattern, and point at the spec anchor",
		},
		{
			atomID: "develop-deploy-files-self-deploy",
			required: []string{
				"DM-2",
				"[.]",
				"docs/spec-workflows.md",
			},
			forbidden: []string{
				// Self-deploy [.] is a hard error (INVALID_ZEROPS_YML), not
				// an advisory. Any atom wording that softens this to a
				// warning reintroduces the destruction risk in agent hands.
				"dev service should use deployFiles: [.] — ensures",
			},
			explanation: "self-deploy rule must cite DM-2 and reference the spec; must not soften to the legacy 'should use [.]' advisory wording",
		},
		{
			atomID: "develop-first-deploy-scaffold-yaml",
			required: []string{
				"tilde",
				"ContentRootPath",
				"DM-5",
			},
			explanation: "scaffold atom must document the runtime content-root gotcha and link to DM-5 so agents pick preserve-vs-extract correctly",
		},
		{
			atomID: "develop-push-dev-deploy-container",
			required: []string{
				"develop-deploy-modes",
			},
			explanation: "push-dev container atom must cross-reference the deploy-modes atom so the class distinction surfaces at the deployFiles-decision moment",
		},
	}

	atoms, err := content.ReadAllAtoms()
	if err != nil {
		t.Fatalf("ReadAllAtoms: %v", err)
	}

	byID := make(map[string]string, len(atoms))
	for _, a := range atoms {
		id := strings.TrimSuffix(a.Name, ".md")
		byID[id] = a.Content
	}

	for _, r := range rules {
		body, ok := byID[r.atomID]
		if !ok {
			t.Errorf("atom %q not found in corpus — add the atom or remove the rule (see docs/spec-workflows.md §8 Deploy Modes)",
				r.atomID)
			continue
		}
		for _, phrase := range r.required {
			if !strings.Contains(body, phrase) {
				t.Errorf("atom %q must contain %q — %s (see docs/spec-workflows.md §8 Deploy Modes)",
					r.atomID, phrase, r.explanation)
			}
		}
		for _, phrase := range r.forbidden {
			if strings.Contains(body, phrase) {
				t.Errorf("atom %q must not contain %q — %s (see docs/spec-workflows.md §8 Deploy Modes)",
					r.atomID, phrase, r.explanation)
			}
		}
	}
}
