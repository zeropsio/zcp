package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// v8.81 §4.6 — recipe_architecture_narrative check.
//
// For showcase-tier recipes with ≥2 runtime codebases, the root README.md
// must contain a section that (a) names each codebase's dev hostname,
// (b) names each codebase's role, and (c) names at least one inter-codebase
// contract (publish/consume/route/proxy/call/subscribe — how the codebases
// talk to each other).
//
// Motivation: v22 qualitative read surfaced that the three codebases in
// multi-codebase recipes read as "islands with bridges, not a narrative
// arc". A reader landing on `workerdev/README.md` learns queue groups but
// never sees the publisher shape from apidev. The root README was a link
// aggregator, not an architecture narrator.
//
// The check lives at finalize because the root README only becomes a
// published deliverable when all per-codebase content is stable; it
// shouldn't gate earlier steps. Failure is HARD — the root README is the
// gateway to the recipe, and its cross-codebase coherence is load-bearing.

// archSectionHeaderRe matches any H2 header whose first word is architecture-flavored.
// Common synonyms a reasonable writer would reach for: "Architecture",
// "Service Topology", "Service Map", "How it fits together", "Services and
// contracts", "System overview", "Three-service shape", etc. The regex is
// intentionally permissive — we want to catch the section, not police its title.
var archSectionHeaderRe = regexp.MustCompile(`(?mi)^##\s+(architecture|service[\s\-]topology|service[\s\-]map|services?[\s\-]and[\s\-]contracts|system[\s\-]overview|how[\s\-]it[\s\-]fits|three[\s\-]service|recipe[\s\-]architecture|shape[\s\-]and[\s\-]contracts)`)

// contractVerbRe matches verbs that describe inter-service communication.
// Any ONE of these in the architecture section qualifies as a "contract
// named". The list is intentionally wide to avoid false negatives on
// creative phrasings.
var contractVerbRe = regexp.MustCompile(`(?i)\b(publishes?|consumes?|subscribes?|proxies|proxy|routes?[\s\-]to|calls?|fetches?|forwards?|dispatches?|emits?|receives?|reads?[\s\-]from|writes?[\s\-]to|posts?[\s\-]to|queue[\s\-]groups?)\b`)

// checkArchitectureNarrative fires on showcase recipes with ≥2 runtime
// codebases. It reads the root README.md at outputDir and verifies the
// narrative is present + load-bearing.
func checkArchitectureNarrative(outputDir string, plan *workflow.RecipePlan) []workflow.StepCheck {
	if plan == nil || plan.Tier != workflow.RecipeTierShowcase {
		return nil
	}
	hostnames := runtimeCodebaseHostnames(plan)
	if len(hostnames) < 2 {
		return nil
	}
	const checkName = "recipe_architecture_narrative"

	data, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if err != nil {
		return []workflow.StepCheck{{
			Name:   checkName,
			Status: statusFail,
			Detail: fmt.Sprintf("root README.md not found at %s — the root README is the gateway reader lands on. For showcase recipes with %d codebases, it must include an Architecture section that names each codebase, its role, and the inter-codebase contracts.", outputDir, len(hostnames)),
		}}
	}
	readme := string(data)

	// 1. Section header present?
	loc := archSectionHeaderRe.FindStringIndex(readme)
	if loc == nil {
		return []workflow.StepCheck{{
			Name:   checkName,
			Status: statusFail,
			Detail: fmt.Sprintf(
				"root README.md missing an architecture-narrative section for %d-codebase showcase recipe. Add an `## Architecture` section (or equivalent: `## Service Topology`, `## Services and Contracts`, `## How It Fits Together`) that (a) names each runtime codebase by hostname — %s — with its role, and (b) names at least one inter-codebase contract (publish/consume/subscribe/proxy/call/route-to). Without this section, a reader who lands on one codebase's README never sees how it contracts with the others; v22 landed at a C+ cross-codebase coherence grade specifically because this section was absent. Example shape:\n"+
					"\n"+
					"## Architecture\n"+
					"\n"+
					"Three codebases, one project.\n"+
					"\n"+
					"- **apidev** — NestJS API. Publishes jobs to NATS; serves CRUD + search endpoints to appdev over CORS.\n"+
					"- **appdev** — Svelte SPA. Calls apidev over `${DEV_API_URL}`; never speaks to NATS, DB, or the worker directly.\n"+
					"- **workerdev** — NATS consumer (queue group `workers`). Subscribes to `jobs.process`, writes results back to the shared `db`.\n"+
					"\n"+
					"The contract boundary: appdev → apidev is HTTP/JSON; apidev → workerdev is NATS publish/subscribe; workerdev → db is SQL.",
				len(hostnames), strings.Join(hostnames, ", "),
			),
		}}
	}

	// Extract the architecture-section body: from the header to the next
	// H2 (or EOF).
	sectionStart := loc[0]
	nextH2Re := regexp.MustCompile(`(?m)^##\s+`)
	restAfterHeader := readme[loc[1]:]
	nextLoc := nextH2Re.FindStringIndex(restAfterHeader)
	var sectionBody string
	if nextLoc == nil {
		sectionBody = readme[sectionStart:]
	} else {
		sectionBody = readme[sectionStart : loc[1]+nextLoc[0]]
	}

	// 2. Every codebase hostname named in the section?
	var missing []string
	bodyLower := strings.ToLower(sectionBody)
	for _, h := range hostnames {
		if !strings.Contains(bodyLower, strings.ToLower(h)) {
			missing = append(missing, h)
		}
	}
	if len(missing) > 0 {
		return []workflow.StepCheck{{
			Name:   checkName,
			Status: statusFail,
			Detail: fmt.Sprintf("root README Architecture section exists but does not name every codebase. Missing: %s. Every runtime codebase (%s) must appear by its dev hostname so a reader can trace the service name across the three-service story.", strings.Join(missing, ", "), strings.Join(hostnames, ", ")),
		}}
	}

	// 3. At least one contract verb?
	if !contractVerbRe.MatchString(sectionBody) {
		return []workflow.StepCheck{{
			Name:   checkName,
			Status: statusFail,
			Detail: "root README Architecture section names every codebase but does NOT describe any inter-codebase contract. The section must include at least one verb describing how the codebases talk to each other (publish/consume/subscribe/proxy/call/route-to/queue-group). Without contract verbs, the section is a list of services, not an architecture narrative — and a reader still has to guess how apidev's CORS config relates to appdev's fetch calls, or how apidev's NATS publish relates to workerdev's queue-group subscription.",
		}}
	}

	return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
}

// runtimeCodebaseHostnames returns the dev hostnames of every runtime target
// that represents a distinct codebase (i.e. not a managed-service dep, not a
// worker-sharing-codebase). Sorted for deterministic output.
func runtimeCodebaseHostnames(plan *workflow.RecipePlan) []string {
	if plan == nil {
		return nil
	}
	seen := map[string]bool{}
	var hostnames []string
	for _, t := range plan.Targets {
		if !workflow.IsRuntimeType(t.Type) {
			continue
		}
		// Workers that share a codebase don't count as a separate codebase —
		// they live inside an existing runtime's repo. IsWorker + non-empty
		// SharesCodebaseWith marks this case.
		if t.IsWorker && t.SharesCodebaseWith != "" {
			continue
		}
		if seen[t.Hostname] {
			continue
		}
		seen[t.Hostname] = true
		hostnames = append(hostnames, t.Hostname)
	}
	sort.Strings(hostnames)
	return hostnames
}
