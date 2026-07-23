package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
)

const hardcodedServiceVersionPattern = `[a-z][a-z0-9.+-]*@[0-9]`

// TestPlaybookOnboarding_ContentPins_CoreContract pins the load-bearing
// conversation contract from docs/spec-onboarding.md §2-§4 at the public
// embedded-knowledge seam.
func TestPlaybookOnboarding_ContentPins_CoreContract(t *testing.T) {
	t.Parallel()

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://playbooks/onboarding")
	if err != nil {
		t.Fatalf("Get onboarding playbook: %v", err)
	}
	body := strings.Join(strings.Fields(doc.Content), " ")

	tests := []struct {
		name       string
		needle     string
		wantAbsent bool
	}{
		{name: "tool-call-free opening", needle: "no tool calls first"},
		{name: "immediate greeting", needle: "Greet immediately"},
		{name: "fresh classification", needle: `adoptionState: "zcp-self"`},
		{name: "complete fresh predicate", needle: "no live `activity`, no warnings"},
		{name: "status services warning", needle: "never classify from the status Services line"},
		{name: "bring app option", needle: "**Bring an app**"},
		{name: "start new option", needle: "**Start something new**"},
		{name: "quick tour option", needle: "**Take a quick tour**"},
		{name: "continue project option", needle: "**Continue this project**"},
		{name: "freeform escape", needle: "Or tell me the outcome you want."},
		{name: "source question", needle: "Where should I get the app's source?"},
		{name: "single source question", needle: "ask exactly one question"},
		{name: "recipe consent", needle: "explicit yes BEFORE committing"},
		{name: "recipe route", needle: `route="recipe"`},
		{name: "recipe slug", needle: "recipeSlug="},
		{name: "model knowledge fetch", needle: `zerops_knowledge uri="zerops://themes/model"`},
		{name: "mutation boundary", needle: "Nothing mutates before an explicit choice"},
		{name: "obsolete infrastructure scope", needle: `scope="infrastructure"`, wantAbsent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := strings.Contains(body, tt.needle)
			if got == tt.wantAbsent {
				if tt.wantAbsent {
					t.Errorf("playbook contains forbidden text %q", tt.needle)
					return
				}
				t.Errorf("playbook missing required text %q", tt.needle)
			}
		})
	}

	t.Run("no hardcoded service version", func(t *testing.T) {
		t.Parallel()

		if match := regexp.MustCompile(hardcodedServiceVersionPattern).FindString(body); match != "" {
			t.Errorf("playbook contains hardcoded service version %q", match)
		}
	})

	t.Run("status precedes discover", func(t *testing.T) {
		t.Parallel()

		statusIndex := strings.Index(body, `zerops_workflow action="status"`)
		discoverIndex := strings.Index(body, "zerops_discover")
		if statusIndex < 0 || discoverIndex < 0 || statusIndex >= discoverIndex {
			t.Errorf("state resolution order invalid: status index = %d, discover index = %d", statusIndex, discoverIndex)
		}
	})

	t.Run("prefix before branches uses read only tool allowlist", func(t *testing.T) {
		t.Parallel()

		prefix, _, found := strings.Cut(body, "## 3. Branches")
		if !found {
			t.Fatal("playbook missing ## 3. Branches heading")
		}

		remaining := prefix
		for {
			_, after, found := strings.Cut(remaining, "zerops_")
			if !found {
				break
			}
			occurrence := "zerops_" + after
			if !strings.HasPrefix(occurrence, `zerops_workflow action="status"`) &&
				!strings.HasPrefix(occurrence, "zerops_discover") {
				mention, _, _ := strings.Cut(occurrence, " ")
				t.Errorf("pre-branch prefix contains non-allowlisted tool mention %q", mention)
			}
			remaining = after
		}
	})
}
