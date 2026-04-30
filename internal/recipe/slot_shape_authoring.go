package recipe

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// slot_shape_authoring.go — Run-18 §3.1 authoring-discipline refusals.
// Extends slot_shape.go with cross-cutting catches that survived to the
// run-17 published surfaces despite brief-side teaching:
//
//   1. Self-inflicted KB bullets (deployFiles narrowing, "we chose X
//      over Y", "That's intentional" defenses).
//   2. Recipe-internal scaffold references in KB (SvelteKit route shapes,
//      same-origin proxy nouns, UI-noun stems).
//   3. Internal zerops_knowledge slug citations leaking as porter-facing
//      references ("See: <slug> guide", `see `<slug>``).
//   4. IG fusion — multiple distinct managed-service hostnames cited
//      inside one slotted IG body (one-mechanism-per-item rule).
//
// Spec references: §195-218 (Surface 5 contract), §380 (self-inflicted),
// §770-790 (one-mechanism IG), §216 (citation shape).

var (
	// Run-18 §3.1 check 1 — self-inflicted KB bullet patterns.
	//
	// `.{0,200}?` (lazy) widens the window past inline punctuation
	// without becoming greedy across paragraphs. `\w*` after each verb
	// root catches conjugations (wipe/wipes/wiped/wiping,
	// narrow/narrows/narrowed/narrowing, etc.).
	selfInflictedDeployFilesRE = regexp.MustCompile(
		`(?is)\bdeployFiles\b.{0,200}?\b(narrow|wipe|empty|replace|strip)\w*\b|\b(narrow|wipe|empty|replace|strip)\w*\b.{0,200}?\bdeployFiles\b`)
	selfInflictedChoiceRE = regexp.MustCompile(
		`(?is)\bwe (chose|picked|use|opted|went with)\b.{0,200}?\b(over|instead of|rather than)\b`)
	selfInflictedDefenseRE = regexp.MustCompile(
		`(?i)\b(That'?s intentional|This is correct|Not a problem)\b`)

	// Run-18 §3.1 check 2 — recipe-internal scaffold references.
	scaffoldSvelteKitRouteRE = regexp.MustCompile(`\+page\.svelte\b|\+server\.js\b|\+layout\.`)
	scaffoldWildcardProxyRE  = regexp.MustCompile(`/api/\[\.\.\.path\]`)
	scaffoldUINounStemRE     = regexp.MustCompile(`(?i)\b(panel|tab|dashboard|widget)s?\b`)

	// Run-18 §3.1 check 3 — internal-slug citation leakage.
	//
	// Trailing form: a citation verb (See/Cite/Per/Ref/cf) followed by
	// colon, optional backticked slug, optional "guide" suffix. Catches:
	//
	//   See: env-var-model guide.        (run-17 form)
	//   Cite: managed-services-nats.     (run-18 worker form)
	//   Cite: `managed-services-nats`.   (run-18 worker backticked)
	//   Cite: `rolling-deploys`, `managed-services-nats`.   (multi-slug)
	//   Per: rolling-deploys             (variant)
	//   Ref: env-var-model               (variant)
	//   cf: object-storage               (variant)
	//
	// Inline prose ("see the http-support guide") does NOT match because
	// no colon follows the verb — that shape is legitimate per spec §216.
	// Run-18 caught only `See: <slug> guide`; run-19 prep extends to
	// every colon-prefixed citation-verb shape because the agent
	// rephrased to evade the narrower regex (catalog drift signature
	// per docs/zcprecipator3/system.md §4).
	slugTrailingCitationRE = regexp.MustCompile(
		"(?i)\\b(?:See|Cite|Per|Ref|cf):\\s*`?[a-z][a-z0-9-]+`?")
	// Backtick form: a citation verb (see / cf / per / cited in) within
	// a short window of a backticked slug. The window allows optional
	// articles/connective words ("see the `foo`", "Cited in the `foo`
	// platform topic"). Slugs are filtered to the engine's known
	// CitationMap value set in matchKnownBacktickCitation so random
	// inline-code references don't false-positive.
	slugBacktickCitationVerbRE = regexp.MustCompile(
		"(?i)\\b(see|cf|per|cited in)\\b[^\\n`]{0,30}`([a-z][a-z0-9-]+)`")
)

// knownCitationSlugs returns the deduplicated set of citation guide
// slugs the engine knows about (CitationMap values). Used to filter the
// backtick-citation regex so a bullet that just contains "`foo`" as
// inline code doesn't false-positive.
func knownCitationSlugs() map[string]bool {
	out := map[string]bool{}
	for _, g := range CitationMap {
		out[g] = true
	}
	return out
}

// hasBacktickKnownSlugCitation reports whether body contains a citation
// verb followed by a backticked known-slug. Returns the matched slug
// for refusal-message diagnostics.
func hasBacktickKnownSlugCitation(body string) (slug string, ok bool) {
	known := knownCitationSlugs()
	matches := slugBacktickCitationVerbRE.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		if known[m[2]] {
			return m[2], true
		}
	}
	return "", false
}

// kbBulletAuthoringRefusals walks one bullet collecting authoring-
// discipline violations. The stem (text inside leading `**...**`) is
// passed separately because some checks scope to the stem only —
// notably the UI-noun discriminator: a stem starting with "The cache
// panel renders empty" is recipe-internal scaffold, but a body that
// merely mentions a panel as illustrative context is fine.
func kbBulletAuthoringRefusals(stem, body string) []string {
	var out []string

	// Check 1 — self-inflicted KB.
	if selfInflictedDeployFilesRE.MatchString(body) {
		out = append(out,
			"knowledge-base bullet teaches around the recipe's own deployFiles narrowing — self-inflicted (spec §380); the fix is in the codebase's zerops.yaml comment, not on KB. Discard.")
	}
	if selfInflictedChoiceRE.MatchString(body) {
		out = append(out,
			"knowledge-base bullet frames the topic as 'we chose X over Y' — that's a scaffold-decision (Surface 7 zerops.yaml comment) or an IG diff item (Surface 4), not a Surface 5 platform trap. See spec §380, §216-220.")
	}
	if selfInflictedDefenseRE.MatchString(body) {
		out = append(out,
			"knowledge-base bullet defends a recipe design choice (\"That's intentional\" / \"This is correct\" / \"Not a problem\") — that's authoring narrative, not a porter-relevant trap. Discard or reshape as IG.")
	}

	// Check 2 — recipe-internal scaffold references.
	if scaffoldSvelteKitRouteRE.MatchString(body) {
		out = append(out,
			"knowledge-base bullet references SvelteKit route shapes (`+page.svelte`/`+server.js`/`+layout.*`) — recipe-internal scaffold the porter doesn't necessarily share. KB teaches platform traps that hit regardless of these specifics. Reshape as IG (porter literally copies the diff) or discard.")
	}
	if scaffoldWildcardProxyRE.MatchString(body) {
		out = append(out,
			"knowledge-base bullet references the recipe's `/api/[...path]` same-origin proxy noun — recipe-internal architecture, not a platform-invariant trap. Reshape as IG or discard.")
	}
	if scaffoldUINounStemRE.MatchString(stem) {
		out = append(out,
			"knowledge-base bullet stem names a UI element (panel/tab/dashboard/widget) — recipe-internal UI scaffold the porter may not have. KB stems describe symptoms a porter searches for; the underlying platform trap belongs on KB only if porter-agnostic. Reshape stem to platform-symptom or discard.")
	}

	// Check 3 — internal-slug citation leakage.
	if slugTrailingCitationRE.MatchString(body) {
		out = append(out,
			"knowledge-base bullet contains a trailing citation label (`See:`/`Cite:`/`Per:`/`Ref:`/`cf:` followed by a `zerops_knowledge` tool slug). Porters cannot resolve the slug as a docs URL. Cite by inline prose instead (e.g. \"the L7 balancer doc explains…\") or drop the trailing label. Spec §216.")
	}
	if slug, ok := hasBacktickKnownSlugCitation(body); ok {
		out = append(out, fmt.Sprintf(
			"knowledge-base bullet uses backticked `%s` as a citation reference — that's the agent's `zerops_knowledge` tool slug, which porters cannot resolve as a docs URL. Convert to inline prose mention. Spec §216.",
			slug))
	}

	return out
}

// igSlotAuthoringRefusals checks an integration-guide slot body for:
//   - Multi-managed-service fusion (Check 4) — needs plan-scoped hostnames
//   - Internal-slug citation leakage (Check 3, IG variant)
//
// The single-H3 cap is enforced by checkSlottedIG; this runs after.
// hostnames is the deduplicated managed-service hostname set from
// Plan.Services; nil (test path) skips the fusion check.
//
// Fusion check counts hostnames in PROSE only — yaml ``` ``` fences
// (notably IG #1's "Adding zerops.yaml" verbatim block) legitimately
// list every service.
func igSlotAuthoringRefusals(body string, hostnames []string) []string {
	var out []string

	// Check 4 — fusion: count distinct managed-service hostnames in
	// the prose body. Strip yaml fences first so the engine-emitted
	// IG #1 (which embeds the full zerops.yaml) doesn't FP.
	bodyForFusion := stripCodeFences(body)
	hits := map[string]bool{}
	for _, h := range hostnames {
		if h == "" {
			continue
		}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(h) + `\b`)
		if re.MatchString(bodyForFusion) {
			hits[h] = true
		}
	}
	if len(hits) > 1 {
		names := make([]string, 0, len(hits))
		for h := range hits {
			names = append(names, h)
		}
		sort.Strings(names)
		out = append(out, fmt.Sprintf(
			"integration-guide slot fuses %d managed services (%s) into one item; spec §770-790 mandates one mechanism per IG item. Split into separate slots — one per service.",
			len(hits), strings.Join(names, ", ")))
	}

	// Check 3 — slug citation in IG body.
	if slugTrailingCitationRE.MatchString(body) {
		out = append(out,
			"integration-guide slot contains a trailing citation label (`See:`/`Cite:`/`Per:`/`Ref:`/`cf:` followed by a `zerops_knowledge` tool slug). Porter-facing surfaces cite by inline prose. Spec §216.")
	}
	if slug, ok := hasBacktickKnownSlugCitation(body); ok {
		out = append(out, fmt.Sprintf(
			"integration-guide slot uses backticked `%s` as a citation reference. Convert to inline prose mention. Spec §216.",
			slug))
	}

	return out
}

// commentSurfaceSlugCitationRefusals is shared by env import-comments,
// codebase zerops-yaml-comments — both porter-facing yaml comment
// surfaces where slug-citation is the same anti-pattern.
func commentSurfaceSlugCitationRefusals(body, surfaceName string) []string {
	var out []string
	if slugTrailingCitationRE.MatchString(body) {
		out = append(out, fmt.Sprintf(
			"%s contains a trailing citation label (`See:`/`Cite:`/`Per:`/`Ref:`/`cf:` followed by a `zerops_knowledge` tool slug). Porter-facing comments cite by inline prose. Spec §216.",
			surfaceName))
	}
	if slug, ok := hasBacktickKnownSlugCitation(body); ok {
		out = append(out, fmt.Sprintf(
			"%s uses backticked `%s` as a citation reference. Convert to inline prose mention. Spec §216.",
			surfaceName, slug))
	}
	return out
}

// stripCodeFences removes ``` ... ``` blocks from body so prose-only
// content remains. Used by the IG fusion check to ignore yaml-verbatim
// blocks that legitimately list every service.
func stripCodeFences(body string) string {
	const fence = "```"
	out := body
	for {
		i := strings.Index(out, fence)
		if i < 0 {
			break
		}
		j := strings.Index(out[i+len(fence):], fence)
		if j < 0 {
			// Unterminated fence — drop the rest.
			return out[:i]
		}
		out = out[:i] + out[i+len(fence)+j+len(fence):]
	}
	return out
}

// managedServiceHostnames extracts the deduplicated managed-service
// hostname set from a plan, used for the IG-fusion hostname check.
// Returns nil for nil plan; ServiceKindManaged + ServiceKindStorage
// both count (the porter's "different managed thing" mental model).
func managedServiceHostnames(plan *Plan) []string {
	if plan == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range plan.Services {
		if s.Hostname == "" {
			continue
		}
		if s.Kind != ServiceKindManaged && s.Kind != ServiceKindStorage {
			continue
		}
		if seen[s.Hostname] {
			continue
		}
		seen[s.Hostname] = true
		out = append(out, s.Hostname)
	}
	sort.Strings(out)
	return out
}
