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
		{name: "menu bullet: build something", needle: `**Build something** — describe an idea in one line, with a technology if you care ("create a weather dashboard in Bun" — or Node.js, Python, PHP, and many more; Zerops covers a wide range of stacks, so just ask); I set up the environment from a ready-made recipe and build it with you to a live URL.`},
		{name: "menu bullet: try a ready-made recipe", needle: "**Try a ready-made recipe** — a complete working app (Node, Python, PHP, Laravel, Go, Rust, …) running in minutes — and it becomes yours to develop further."},
		{name: "menu bullet: what are zerops and zcp", needle: "**What are Zerops & ZCP?** — a short explanation of how it all works."},
		{name: "menu escape line", needle: `Or just tell me what you want, in plain words — that works for everything here: "scale the cpu to 4 cores", "show me the logs", "add a Postgres database".`},
		{name: "fresh classification", needle: `adoptionState: "zcp-self"`},
		{name: "complete fresh predicate", needle: "no live `activity`, no warnings"},
		{name: "status services warning", needle: "never classify from the status Services line"},
		{name: "continue project option", needle: "**Continue this project**"},
		{name: "mapping slug: nodejs", needle: "nodejs-hello-world"},
		{name: "mapping slug: laravel", needle: "laravel-minimal"},
		{name: "mapping slug: bun", needle: "bun-hello-world"},
		{name: "standard-flow handoff owns the steps", needle: "run the standard flow and follow its guidance"},
		{name: "one plain yes before import", needle: "get one plain yes"},
		{name: "consent shows the returned recipe plan", needle: "before running `zerops_import`, tell the person what the returned recipe"},
		{name: "stage URL exactly as reported", needle: "STAGE service's URL exactly as the workflow response"},
		{name: "never compose a URL", needle: "never compose a URL yourself"},
		{name: "recipe route", needle: `route="recipe"`},
		{name: "recipe slug", needle: "recipeSlug="},
		{name: "hands off to workflow guidance, no parallel rulebook", needle: "never replaces the guidance the"},
		{name: "orientation fetch directive", needle: `zerops_knowledge uri="zerops://playbooks/orientation"`},
		{name: "GUI recipe link is surfaced, never composed", needle: "never compose it from the corpus slug"},
		{name: "obsolete compose-it-yourself GUI link template", needle: "app.zerops.io/recipes/<slug>", wantAbsent: true},
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

func TestPlaybookMonorepo_ContentPins_PartRules(t *testing.T) {
	t.Parallel()

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://playbooks/monorepo")
	if err != nil {
		t.Fatalf("Get monorepo playbook: %v", err)
	}
	body := strings.Join(strings.Fields(doc.Content), " ")

	for _, tt := range []struct{ name, needle string }{
		{"single-part repo is out of scope", "A repo with one deployable app follows the ordinary develop flow unchanged"},
		{"parts are the zerops.yaml setups", "Parts are the setups in the repo's `zerops.yaml`"},
		{"workspace markers", "`pnpm-workspace.yaml`, `workspaces` in `package.json`, `nx.json`, `turbo.json`"},
		{"propose parts once", "Propose the parts to the person once"},
		{"variant A is the default", "**one dev service holds the whole repo** (default)"},
		{"parts that cannot share a container each get a dev pool", "**one dev pool per part**"},
		{"a part's dev pool is set up from the services theme", `zerops_knowledge uri="zerops://themes/services"`},
		{"no shared code volume is set up any more", "zcp no longer sets up a shared code volume"},
		{"an existing shared code volume moves onto pools", `zerops_workflow workflow="pool-migrate" service="<dev>"`},
		{"more people means more Mates", "More people on the repo means more Mates, never more dev services"},
		{"pnpm via prepareCommands", "install it in `build.prepareCommands`"},
		{"extends replaces lists", "`extends` replaces lists whole"},
		{"ubuntu zsc install", "`sudo zsc install <runtime>` on Ubuntu"},
		{"alpine apk add", "`sudo apk add <package>` on Alpine"},
		{"vite allowedHosts", "`server.allowedHosts`"},
		{"the dev setup installs no dependencies; the running dev service does, as the repo's docs say", "installs no dependencies: they are installed in the running dev service the way the repo's docs install them"},
		{"a repo's own dev command runs as it stands", "A repo that documents its own dev command runs that command as it stands"},
		{"the repo's own allowed-hosts setting comes first", "use the repo's own allowed-hosts setting when it has one"},
		{"no agent proxy in front of a loopback dev server", "Never put your own proxy in front of a dev server that listens on loopback"},
		{"watch, ignore and heavy servers are the dev-servers theme's", `zerops_knowledge uri="zerops://themes/dev-servers"`},
		{"stages carry hostname and setupName", `"stages": [{"hostname": "webstage", "setupName": "web"}`},
		{"adding a part never renames an existing setup or its stage", "An existing setup and its stage keep their names; only the new part gets a new setup"},
		{"the adding-a-part example keeps the existing setup name beside the new one", `"stages": [{"hostname": "appstage", "setupName": "prod"}, {"hostname": "workerstage", "setupName": "worker"}]`},
		{"worked example: a new part's dev server starts by name on the shared dev service", `zerops_dev_server action=start hostname="appdev" name="worker" command="bun worker.ts" noHttpProbe=true`},
		{"worked example: the first start declares it beside the running one", `{"name": "web", "port": 3000, "command": "bun index.ts"}, {"name": "worker", "command": "bun worker.ts"}`},
		{"static cannot proxy", "A static service cannot proxy requests to the API"},
		{"compose to native parts", "Convert compose services to native parts"},
		{"docker only with registry images", "only when its image already exists in a registry"},
		{"SPA and API in one folder is one part", "SPA and API in one folder are one app"},
		{"cross-part URLs from project variables", "project variable built on `${zeropsSubdomainHost}`"},
		{"heavy build scaling", "`zsc scale cpu max 10m`"},
		{"a build killed out of memory scales its RAM for the build", "`zsc scale ram max 10m`"},
		{"the Node run image's own pnpm is used as it stands", "never `npm i -g pnpm` in `run.prepareCommands`"},
		{"a dev server with very many unbundled modules is opened in its bundled or preview mode", "the repo's bundled or preview mode"},
	} {
		if !strings.Contains(body, tt.needle) {
			t.Errorf("%s: monorepo playbook missing %q", tt.name, tt.needle)
		}
	}
	// zcp sets up no new code volume (docs/spec-mate.md §2.8a.4a): the
	// playbook never plans one or tells the agent to deploy onto one.
	for _, gone := range []string{`"codeVolume"`, "one dev service per part on one shared Local Storage volume", `workingDir="/var/www"`} {
		if strings.Contains(body, gone) {
			t.Errorf("monorepo playbook still sets up a shared code volume: %q", gone)
		}
	}
}

// TestThemeDevServers_ContentPins_DeclarationKeys pins that the dev-servers
// theme's declaration table names every key a dev pool's lease reads
// (docs/spec-mate.md §2.8a.1; docs/spec-workflows.md §8 DP-14, DP-22, DP-24,
// DP-26, DP-31): previewPortBase and the per-container port its preview
// answers on, siblingEnv with its private hostname, server-side only and
// declared only for a server that reads it (a sibling's leave or join
// restarts the other leases' servers), a separate line saying a browser
// cannot resolve that hostname and pointing at siblingPreviewEnv, which
// hands browser code the sibling lease's https preview URL — resolved again
// at every join or leave, never hand-copied, a name in one of the two maps
// only — prepare after the checkout, the preview mark, and a declared server
// reloading by itself, so a lease sync never asks for its restart.
func TestThemeDevServers_ContentPins_DeclarationKeys(t *testing.T) {
	t.Parallel()

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/dev-servers")
	if err != nil {
		t.Fatalf("Get dev-servers theme: %v", err)
	}
	section, ok := doc.H2Sections()["The declaration"]
	if !ok {
		t.Fatal("dev-servers theme missing H2 section 'The declaration'")
	}
	body := strings.Join(strings.Fields(section), " ")
	for _, tt := range []struct{ name, needle string }{
		{"previewPortBase has its row", "| `previewPortBase` |"},
		{"the preview port is the base plus the container's stable number", "`previewPortBase + ZEROPS_StableNumber`"},
		{"the pool declares every preview port", "`previewPortBase + 1` through `previewPortBase + maxContainers`"},
		{"the lease's address is the result's previewUrl", "`previewUrl`"},
		{"a new lease may answer on another port", "a new lease of the part can answer on another port"},
		{"siblingEnv has its row", "| `siblingEnv` |"},
		{"siblingEnv gives the private container hostname", "`node-id-<n>.runtime.<pool>.zerops`"},
		{"siblingEnv is server-side only", "server-side calls only"},
		{"declare only a variable a server reads", "Declare a variable only when a dev server actually reads it"},
		{"a sibling's leave or join restarts the other leases' servers", "every lease of that pool that joins or leaves the branch restarts the dev servers running in the branch's other leases"},
		{"browser clients get their own line", "| `siblingEnv` and a browser client |"},
		{"a browser cannot resolve the private hostname", "a browser cannot resolve the private hostname"},
		{"a browser client names its variable in siblingPreviewEnv", "name the variable in `siblingPreviewEnv` instead"},
		{"siblingPreviewEnv has its row", "| `siblingPreviewEnv` |"},
		{"siblingPreviewEnv hands the sibling lease's https preview URL", "that lease's preview URL (`https://`"},
		{"siblingPreviewEnv serves code that runs in the browser", "for code that runs in the browser"},
		{"siblingEnv gives the container hostname for server-side calls", "`siblingEnv` gives the container hostname, for server-side calls"},
		{"a name is in one of the two maps, never both", "A name is in one of the two, never both"},
		{"the preview URL is resolved again at every join or leave", "resolved again whenever a lease of the branch joins or leaves"},
		{"the preview URL is never hand-copied", "never copy it by hand"},
		{"a declared server reloads by itself, so a sync never asks for its restart", "Either way the server reloads by itself, so a lease sync never asks you to restart it"},
		{"prepare has its row", "| `prepare` |"},
		{"prepare runs after the checkout, before the servers start", "after the checkout, before the declared servers start"},
		{"a failed prepare leaves the lease held", "leaves the lease held"},
		{"preview has its row", "| `servers[].preview` |"},
		{"preview marks the server the preview reaches", "when more than one server listens"},
	} {
		if !strings.Contains(body, tt.needle) {
			t.Errorf("%s: dev-servers declaration section missing %q", tt.name, tt.needle)
		}
	}
}

// TestThemeDevServers_ContentPins_WatchAndHeavyServers pins the dev-server
// rules the monorepo playbook points to rather than restates.
func TestThemeDevServers_ContentPins_WatchAndHeavyServers(t *testing.T) {
	t.Parallel()

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/dev-servers")
	if err != nil {
		t.Fatalf("Get dev-servers theme: %v", err)
	}
	body := strings.Join(strings.Fields(doc.Content), " ")

	for _, tt := range []struct{ name, needle string }{
		{"the supervisor watches a declared dev server", "The supervisor watches a declared dev server"},
		{"never a runtime's own watcher", "Never add a runtime's own watcher"},
		{"heavy servers not being worked on do not autostart", "declares the ones not being worked on `\"autostart\": false`"},
		{"watch ignore entries are repo-root paths matched folder by folder, with ** across folders", "`apps/api/dist`, `**/*.egg-info`"},
	} {
		if !strings.Contains(body, tt.needle) {
			t.Errorf("%s: dev-servers theme missing %q", tt.name, tt.needle)
		}
	}
}
