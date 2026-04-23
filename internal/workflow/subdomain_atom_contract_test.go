package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestSubdomainAtomContract pins the removal of per-atom imperative
// zerops_subdomain guidance. After Plan 2 (plans/archive/subdomain-auto-enable.md),
// zerops_deploy auto-enables the L7 subdomain on first deploy for
// eligible modes and the agent does NOT emit an explicit enable call.
// Atoms that still tell the agent to "run zerops_subdomain action=enable"
// reintroduce the redundant call the platform rejects with a garbage
// FAILED process (plans/archive/subdomain-robustness.md §1.1) — a
// regression that's invisible to unit tests of the handler code.
//
// The test fails if any of the listed atoms contain a forbidden phrase.
// Explanatory negative statements ("No manual zerops_subdomain call is
// needed…") are permitted — the forbidden phrases are the imperative
// patterns that ask the agent to act, not mentions of the tool name.
//
// Update this test — do NOT edit the atom file to re-add the phrase —
// whenever the subdomain-activation contract changes. The atom corpus
// and the deploy handler must ship the same contract in lockstep, or
// recipes will drift back to the broken pattern silently.
func TestSubdomainAtomContract(t *testing.T) {
	t.Parallel()

	// Forbidden imperative phrases. Matching is a substring check against
	// the atom body; phrases are chosen so they only hit imperative
	// guidance, not incidental mentions of the tool name.
	type rule struct {
		atomID      string
		forbidden   []string
		explanation string
	}

	rules := []rule{
		{
			atomID:      "develop-first-deploy-verify",
			forbidden:   []string{`zerops_subdomain serviceHostname`, `zerops_subdomain action="enable"`, `zerops_subdomain action=enable`},
			explanation: "first-deploy verify must rely on the auto-enabled subdomain, not an imperative enable call",
		},
		{
			atomID:      "develop-first-deploy-execute",
			forbidden:   []string{`zerops_subdomain serviceHostname`, `zerops_subdomain action="enable"`, `zerops_subdomain action=enable`, `enable the subdomain`, `enable its subdomain`},
			explanation: "first-deploy execute must not pre-empt the deploy handler's auto-enable",
		},
		{
			atomID:      "develop-first-deploy-intro",
			forbidden:   []string{`subdomains are disabled`, `subdomain is disabled`, `zerops_subdomain action="enable"`},
			explanation: "first-deploy intro must not claim subdomains start disabled — the deploy handler activates them",
		},
		{
			atomID:      "develop-first-deploy-promote-stage",
			forbidden:   []string{`zerops_subdomain serviceHostname`, `zerops_subdomain action="enable"`, `zerops_subdomain action=enable`},
			explanation: "stage promote must rely on the deploy handler auto-enabling the stage subdomain cross-deploy",
		},
		{
			atomID:      "develop-implicit-webserver",
			forbidden:   []string{`zerops_subdomain serviceHostname`, `zerops_subdomain action="enable"`, `zerops_subdomain action=enable`, `zerops_subdomain get`},
			explanation: "implicit-webserver atom must use zerops_discover for status reads and rely on auto-enable for activation",
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
			t.Errorf("atom %q not found in corpus — remove the rule or add the atom", r.atomID)
			continue
		}
		for _, phrase := range r.forbidden {
			if strings.Contains(body, phrase) {
				t.Errorf("atom %q must not contain %q — %s (see plans/archive/subdomain-auto-enable.md)",
					r.atomID, phrase, r.explanation)
			}
		}
	}
}
