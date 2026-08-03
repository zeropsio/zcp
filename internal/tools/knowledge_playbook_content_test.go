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
		{name: "verbatim-render directive", needle: "Render the menu block below verbatim"},
		{name: "menu greeting line", needle: "Welcome to Zerops! Zerops builds and runs apps and their supporting services, connects them on a private project network, and can expose web services at a public URL. I'm an agent that drives it through ZCP."},
		{name: "menu bullet: build something", needle: `**Build something** — describe an idea in one line, with a technology if you care ("a weather dashboard in Bun"); I set up the environment from a ready-made recipe and build it with you to a live URL.`},
		{name: "menu bullet: try a ready-made recipe", needle: "**Try a ready-made recipe** — a complete working app (Node, Python, PHP, Laravel, Go, Rust, …) running in minutes — and it becomes yours to develop further."},
		{name: "menu bullet: what are zerops and zcp", needle: "**What are Zerops & ZCP?** — a short explanation before we change anything."},
		{name: "menu escape line", needle: `Or just tell me what you want, in plain words — that works for everything here: "scale the cpu to 4 cores", "show me the logs", "add a Postgres database".`},
		{name: "fresh classification", needle: `adoptionState: "zcp-self"`},
		{name: "complete fresh predicate", needle: "no live `activity`, no warnings"},
		{name: "status services warning", needle: "never classify from the status Services line"},
		{name: "continue project option", needle: "**Continue this project**"},
		{name: "mapping slug: nodejs", needle: "nodejs-hello-world"},
		{name: "mapping slug: laravel", needle: "laravel-minimal"},
		{name: "mapping slug: bun", needle: "bun-hello-world"},
		{name: "dev-only narrowing", needle: `recipeNarrow="dev-only"`},
		{name: "consent phase: footprint", needle: "**Footprint consent**"},
		{name: "consent phase: commit", needle: "**Commit + derive/confirm**"},
		{name: "consent phase: exists disclosure", needle: "**Renewed consent on EXISTS**"},
		{name: "consent phase: narrate then import", needle: "**Narrate, then import**"},
		{name: "recipe route", needle: `route="recipe"`},
		{name: "recipe slug", needle: "recipeSlug="},
		{name: "stage handoff / dev idles 502", needle: "dev service idles by design (`zsc noop`) and answers 502"},
		{name: "failure ending heading", needle: "Failure ending"},
		{name: "failure ending never touches pre-existing dependency", needle: "never offer to delete or rewrite a service that already existed before this attempt"},
		{name: "orientation fetch directive", needle: `zerops_knowledge uri="zerops://playbooks/orientation"`},
		{name: "obsolete infrastructure scope", needle: `scope="infrastructure"`, wantAbsent: true},
		{name: "retired v2 bring-an-app label", needle: "**Bring an app**", wantAbsent: true},
		{name: "retired v2 start-something-new label", needle: "**Start something new**", wantAbsent: true},
		{name: "retired v2 take-a-quick-tour label", needle: "**Take a quick tour**", wantAbsent: true},
		{name: "retired v2 model knowledge fetch", needle: `zerops_knowledge uri="zerops://themes/model"`, wantAbsent: true},
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

	t.Run("staged consent order: footprint before commit before EXISTS before narrate/import", func(t *testing.T) {
		t.Parallel()

		footprintIndex := strings.Index(body, "**Footprint consent**")
		commitIndex := strings.Index(body, "**Commit + derive/confirm**")
		existsIndex := strings.Index(body, "**Renewed consent on EXISTS**")
		narrateIndex := strings.Index(body, "**Narrate, then import**")
		if footprintIndex < 0 || commitIndex < 0 || existsIndex < 0 || narrateIndex < 0 {
			t.Fatalf("consent phase markers missing: footprint=%d commit=%d exists=%d narrate=%d",
				footprintIndex, commitIndex, existsIndex, narrateIndex)
		}
		if footprintIndex >= commitIndex || commitIndex >= existsIndex || existsIndex >= narrateIndex {
			t.Errorf("staged consent order invalid: footprint=%d commit=%d exists=%d narrate=%d",
				footprintIndex, commitIndex, existsIndex, narrateIndex)
		}
	})
}

// TestPlaybookOrientation_ContentPins_CoreContract pins the newcomer
// orientation contract from docs/spec-onboarding.md §5 at the public
// embedded-knowledge seam.
func TestPlaybookOrientation_ContentPins_CoreContract(t *testing.T) {
	t.Parallel()

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://playbooks/orientation")
	if err != nil {
		t.Fatalf("Get orientation playbook: %v", err)
	}
	body := strings.Join(strings.Fields(doc.Content), " ")

	tests := []struct {
		name       string
		needle     string
		wantAbsent bool
	}{
		{name: "private network concept", needle: "private network"},
		{name: "hostname concept", needle: "hostname"},
		{name: "build deploy run concept", needle: "build → deploy → run"},
		{name: "subdomain concept", needle: "subdomain"},
		{name: "stage-serves rule", needle: "its subdomain URL is the live one"},
		{name: "consent boundary: destructive/irreversible", needle: "anything destructive or hard to undo"},
		{name: "consent boundary: user-held credentials", needle: "a credential you hold"},
		{name: "closing option: build something", needle: "**Build something**"},
		{name: "closing option: try a ready-made recipe", needle: "**Try a ready-made recipe**"},
		{name: "no blanket deploy-approval language", needle: "every deployment", wantAbsent: true},
		{name: "no blanket deploy-approval language variant", needle: "every deploy needs", wantAbsent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := strings.Contains(body, tt.needle)
			if got == tt.wantAbsent {
				if tt.wantAbsent {
					t.Errorf("orientation playbook contains forbidden text %q", tt.needle)
					return
				}
				t.Errorf("orientation playbook missing required text %q", tt.needle)
			}
		})
	}

	t.Run("no hardcoded service version", func(t *testing.T) {
		t.Parallel()

		if match := regexp.MustCompile(hardcodedServiceVersionPattern).FindString(body); match != "" {
			t.Errorf("orientation playbook contains hardcoded service version %q", match)
		}
	})
}

// TestPlaybookMapping_SlugsResolveInCorpus is an authored foreign-keys guard:
// every recipe slug the onboarding playbook's language→slug mapping
// (docs/spec-onboarding.md §4) names must resolve in the embedded corpus to a
// document carrying a non-empty import YAML, or the recipe branches would
// hand the agent a slug `zerops_import` can never provision.
func TestPlaybookMapping_SlugsResolveInCorpus(t *testing.T) {
	t.Parallel()
	if !knowledge.SyncedCorpusPresent() {
		t.Skip(knowledge.UnsyncedCorpusMessage)
	}

	// Independent oracle: the language→slug mapping authored in
	// docs/spec-onboarding.md §4 (mirrored in the playbook's mapping table).
	slugs := []string{
		"nodejs-hello-world",
		"python-hello-world",
		"php-hello-world",
		"laravel-minimal",
		"go-hello-world",
		"rust-hello-world",
		"bun-hello-world",
		"deno-hello-world",
		"ruby-hello-world",
		"java-hello-world",
		"dotnet-hello-world",
		"gleam-hello-world",
		"nestjs-minimal",
	}

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}

	for _, slug := range slugs {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()

			doc, err := store.Get("zerops://recipes/" + slug)
			if err != nil {
				t.Fatalf("mapped slug %q does not resolve in the embedded corpus: %v", slug, err)
			}
			if doc.ImportYAML == "" {
				t.Errorf("mapped slug %q resolves but carries no import YAML", slug)
			}
		})
	}
}
