package recipe

import (
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Assembler renders the structural content surfaces — root README, env
// READMEs, per-codebase README + CLAUDE.md — from templates living under
// internal/recipe/content/templates/ and the fragment bodies the in-phase
// agents recorded on the plan.
//
// Structural tokens (slug, framework, hostname, tier label, tier suffix,
// tier list) are substituted in from the plan. Fragment markers of the
// form <!-- #ZEROPS_EXTRACT_START:NAME# --> / <!-- #ZEROPS_EXTRACT_END:NAME# -->
// receive their body from Plan.Fragments keyed by the fragment id the
// surface declares (see surfaces.go). Missing fragments are reported on
// the assemble return — callers gate on a non-empty list.
//
// Plan §2.A (run-8-readiness): "engine owns structural templates and runs
// an assembler"; "missing fragment → gate failure, not silent empty".

// Marker literals. Go's regexp lacks backreferences so the scanner
// pairs start and end markers by explicit string search on the name.
const (
	extractStartPrefix = "<!-- #ZEROPS_EXTRACT_START:"
	extractStartSuffix = "# -->"
	extractEndPrefix   = "<!-- #ZEROPS_EXTRACT_END:"
	extractEndSuffix   = "# -->"
)

// unreplacedTokenRE spots leftover {UPPER_SNAKE} tokens in a rendered
// template. Called after substitution to surface templates that carry
// tokens the assembler didn't bind.
var unreplacedTokenRE = regexp.MustCompile(`\{[A-Z][A-Z0-9_]*\}`)

// engineBoundKeys is the full set of {TOKEN} keys the engine binds in
// any surface template. A post-render match whose key is in this set is
// a real defect (unreplaced engine token); a match whose key is NOT in
// this set is fragment-authored code (JS template literal `${API_URL}`,
// Handlebars `{FILENAME}`, Svelte `{#if}`) and is legitimate.
var engineBoundKeys = map[string]bool{
	"SLUG":             true,
	"NAME":             true,
	"ROLE_LABEL":       true,
	"FRAMEWORK":        true,
	"HOSTNAME":         true,
	"TIER_LABEL":       true,
	"TIER_LABEL_LOWER": true, // v9.79.0 — sentence-form lowercased label, acronyms preserved
	"TIER_ARTICLE":     true, // v9.79.0 — "a"/"an" before TIER_LABEL_LOWER
	"TIER_SUFFIX":      true,
	"TIER_LIST":        true,
}

// AssembleRootREADME renders the root README for a recipe. Returns the
// rendered body, the list of fragment ids that were declared by markers
// but not supplied on plan.Fragments, and any rendering error.
func AssembleRootREADME(plan *Plan) (string, []string, error) {
	tpl, err := readTemplate("root_readme.md.tmpl")
	if err != nil {
		return "", nil, err
	}
	body := renderRootTokens(tpl, plan)
	body, missing, err := substituteFragmentMarkers(body, plan.Fragments, "root")
	if err != nil {
		return "", nil, fmt.Errorf("assemble root README: %w", err)
	}
	if err := checkUnreplacedTokens(body); err != nil {
		return "", nil, fmt.Errorf("assemble root README: %w", err)
	}
	return body, missing, nil
}

// AssembleEnvREADME renders the env README for one tier.
func AssembleEnvREADME(plan *Plan, tierIndex int) (string, []string, error) {
	tier, ok := TierAt(tierIndex)
	if !ok {
		return "", nil, fmt.Errorf("unknown tier index %d", tierIndex)
	}
	tpl, err := readTemplate("env_readme.md.tmpl")
	if err != nil {
		return "", nil, err
	}
	body := replaceTokens(tpl, map[string]string{
		"SLUG":             plan.Slug,
		"NAME":             HumanName(plan),
		"FRAMEWORK":        plan.Framework,
		"TIER_LABEL":       tier.Label,
		"TIER_LABEL_LOWER": tierLabelLower(tier.Label),
		"TIER_ARTICLE":     tierArticle(tier.Label),
		"TIER_SUFFIX":      tierDeploySuffix(tier),
	})
	prefix := fmt.Sprintf("env/%d", tierIndex)
	body, missing, err := substituteFragmentMarkers(body, plan.Fragments, prefix)
	if err != nil {
		return "", nil, fmt.Errorf("assemble env/%d README: %w", tierIndex, err)
	}
	if err := checkUnreplacedTokens(body); err != nil {
		return "", nil, fmt.Errorf("assemble env/%d README: %w", tierIndex, err)
	}
	return body, missing, nil
}

// AssembleCodebaseREADME renders the per-codebase README for one hostname.
//
// Integration-guide item #1 is engine-generated: a fenced yaml block
// containing the committed `<cb.SourceRoot>/zerops.yaml` verbatim, with
// inline comments preserved. Matches the reference apps-repo at
// `/Users/fxck/www/laravel-showcase-app/README.md` where the IG opens
// with the full yaml. Fragment-authored items start at #2; the missing-
// fragment gate still reports when the sub-agent didn't author them.
// Run-10-readiness §M.
func AssembleCodebaseREADME(plan *Plan, hostname string) (string, []string, error) {
	if !codebaseKnown(plan, hostname) {
		return "", nil, fmt.Errorf("unknown codebase %q", hostname)
	}
	tpl, err := readTemplate("codebase_readme.md.tmpl")
	if err != nil {
		return "", nil, err
	}
	// V3 fix — codebase README title is `# Zerops x <NAME>[ <ROLE>]`
	// (jetstream-shape, slug-stem stripped). NAME is the human-readable
	// recipe name; ROLE_LABEL adds " API"/" Frontend"/" Worker" only on
	// multi-codebase recipes. Single-line H1 — markers are redundant
	// because the engine already knows the human name from plan.json,
	// and downstream tooling (recipe page render) reads the plan
	// directly rather than extracting from rendered markdown.
	body := replaceTokens(tpl, map[string]string{
		"SLUG":       plan.Slug,
		"NAME":       HumanName(plan),
		"ROLE_LABEL": CodebaseRoleLabel(plan, hostname),
		"FRAMEWORK":  plan.Framework,
		"HOSTNAME":   hostname,
	})
	prefix := "codebase/" + hostname
	// Run-16 §6.7 — slotted IG fragments: walk
	// `codebase/<h>/integration-guide/<n>` keys and synthesize the legacy
	// single-fragment id. Slotted form takes precedence; legacy form is
	// used only when no slotted fragments exist (back-compat).
	fragments := mergeSlottedIGFragments(plan.Fragments, hostname)
	body, missing, err := substituteFragmentMarkers(body, fragments, prefix)
	if err != nil {
		return "", nil, fmt.Errorf("assemble codebase/%s README: %w", hostname, err)
	}
	// Run-21-prep — IG #1 embeds the whole commented zerops.yaml. The
	// codebase-content sub-agent records `codebase/<host>/zerops-yaml`
	// (whole-yaml fragment); use that body directly. Falls back to the
	// on-disk bare yaml when no fragment is recorded yet (early phase,
	// pre-codebase-content) so the README still renders something.
	//
	// Run-11 M-2 hardening: when the codebase HAS a SourceRoot, an
	// empty/zero-byte on-disk yaml AND no fragment is a real corruption
	// signal — fail loud rather than silently shipping a README without
	// IG #1. Pre-scaffold (SourceRoot empty) still soft-skips so
	// research/provision-phase renders don't error.
	var yamlBody string
	if frag, ok := plan.Fragments[fragmentIDCodebaseZeropsYAML(hostname)]; ok {
		yamlBody = frag
	} else {
		raw, err := readCodebaseYAMLForHost(plan, hostname)
		if err != nil {
			return "", nil, fmt.Errorf("assemble codebase/%s README: %w", hostname, err)
		}
		if strings.TrimSpace(raw) == "" && hasNonEmptySourceRoot(plan, hostname) {
			return "", nil, fmt.Errorf("assemble codebase/%s README: on-disk zerops.yaml is empty/whitespace AND no `codebase/%s/zerops-yaml` fragment recorded — IG #1 cannot render. Either author the whole-yaml fragment or restore the bare scaffold yaml at <SourceRoot>/zerops.yaml",
				hostname, hostname)
		}
		yamlBody = raw
	}
	if yamlBody != "" {
		body = injectIGItem1(body, yamlBody)
	}
	if err := checkUnreplacedTokens(body); err != nil {
		return "", nil, fmt.Errorf("assemble codebase/%s README: %w", hostname, err)
	}
	return body, missing, nil
}

// hasNonEmptySourceRoot reports whether the named codebase has a non-
// empty SourceRoot in plan. Used by AssembleCodebaseREADME to decide
// whether an empty on-disk yaml + missing fragment is a real corruption
// (codebase scaffolded but yaml lost) vs an early-phase soft-skip (no
// scaffold yet → no SourceRoot → no on-disk yaml expected).
func hasNonEmptySourceRoot(plan *Plan, hostname string) bool {
	for _, cb := range plan.Codebases {
		if cb.Hostname == hostname {
			return cb.SourceRoot != ""
		}
	}
	return false
}

// readCodebaseYAMLForHost returns the committed zerops.yaml bytes for
// the named codebase. Run-11 M-2: when SourceRoot is non-empty AND
// the yaml is missing or unreadable, return an error — soft-fail-to-
// empty-string was the reason injectIGItem1 silently no-op'd in run
// 10. Empty SourceRoot still returns ("", nil) so genuinely
// pre-scaffold renders (early-phase paths) don't error.
func readCodebaseYAMLForHost(plan *Plan, hostname string) (string, error) {
	for _, cb := range plan.Codebases {
		if cb.Hostname != hostname {
			continue
		}
		if cb.SourceRoot == "" {
			return "", nil
		}
		raw, err := readCodebaseYAML(cb.SourceRoot)
		if err != nil {
			return "", fmt.Errorf("read codebase/%s zerops.yaml at %q: %w", hostname, cb.SourceRoot, err)
		}
		return raw, nil
	}
	return "", nil
}

// mergeSlottedIGFragments returns a copy of fragments with slotted IG
// entries merged into the legacy single-fragment shape. Run-16 §6.7 —
// when `codebase/<h>/integration-guide/<n>` slots exist (any n), they
// concatenate in numeric order. When no slots exist, the legacy
// fragment is preserved (back-compat).
//
// Run-34 Fix 2 — un-slotted body always appended. Pre-fix, the legacy
// `codebase/<h>/integration-guide` body was OVERWRITTEN whenever any
// slotted slot existed; that silently dropped un-slotted append-mode
// content. Now: slots concatenate first, then any un-slotted body
// appends after, so order-of-recording can never silently lose content.
//
// Engine-emitted IG #1 (Adding zerops.yaml) is stamped by injectIGItem1
// AFTER substitution, so author slots are always n>=2 in practice; the
// merge sorts numerically and concatenates, so any starting index works.
func mergeSlottedIGFragments(fragments map[string]string, hostname string) map[string]string {
	if fragments == nil {
		return nil
	}
	prefix := "codebase/" + hostname + "/integration-guide/"
	type slot struct {
		index int
		body  string
	}
	var slots []slot
	for k, v := range fragments {
		rest, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		idx, err := parseTierIndex(rest)
		if err != nil {
			continue
		}
		slots = append(slots, slot{index: idx, body: v})
	}
	if len(slots) == 0 {
		return fragments
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].index < slots[j].index })
	var b strings.Builder
	for i, s := range slots {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimSpace(s.body))
	}
	// Append un-slotted body (if any). Append, not overwrite — slotted
	// items carry numbered IG steps; the un-slotted body preserves
	// legacy single-fragment content recorded beside numbered slots.
	unslottedKey := "codebase/" + hostname + "/integration-guide"
	if existing := strings.TrimSpace(fragments[unslottedKey]); existing != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(existing)
	}
	out := make(map[string]string, len(fragments))
	maps.Copy(out, fragments)
	out[unslottedKey] = b.String()
	return out
}

// injectIGItem1 rewrites the rendered README's integration-guide extract
// block to open with the engine-generated item #1 (yaml block) followed
// by whatever the sub-agent authored. The injection happens after
// fragment substitution so the missing-fragment gate still fires when the
// sub-agent never recorded items #2+. Idempotent — if item #1 is already
// present the body is returned unchanged.
func injectIGItem1(body, yamlBody string) string {
	const (
		start = "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->"
		end   = "<!-- #ZEROPS_EXTRACT_END:integration-guide# -->"
	)
	_, after, ok := strings.Cut(body, start)
	if !ok {
		return body
	}
	inside, tail, ok := strings.Cut(after, end)
	if !ok {
		return body
	}
	item1 := codebaseIGItem1(yamlBody)
	if strings.Contains(inside, "### 1. Adding `zerops.yaml`") {
		return body
	}
	head := body[:len(body)-len(after)-len(start)] + start + "\n"
	var innerBody string
	if trimmed := strings.TrimSpace(inside); trimmed != "" {
		innerBody = item1 + "\n\n" + trimmed + "\n"
	} else {
		innerBody = item1 + "\n"
	}
	return head + innerBody + end + tail
}

// codebaseIGItem1 formats the engine-owned first item of a codebase's
// Integration Guide — an "### 1. Adding zerops.yaml" heading, an intro
// sentence derived from the yaml body (which setups are declared, whether
// initCommands run migrations / seeding / scout-import, whether
// readinessCheck / healthCheck ship), and a fenced yaml code block
// carrying the committed yaml verbatim. The yaml is never re-wrapped or
// re-parsed, so inline comments survive byte-identical.
func codebaseIGItem1(yamlBody string) string {
	var b strings.Builder
	b.WriteString("### 1. Adding `zerops.yaml`\n\n")
	b.WriteString(yamlIntroSentence(yamlBody))
	b.WriteString("\n\n```yaml\n")
	b.WriteString(yamlBody)
	if !strings.HasSuffix(yamlBody, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```")
	return b.String()
}

// yamlIntroSentence composes the one-or-two-sentence intro for IG item
// #1 by inspecting the yaml body for known stanzas. The first sentence
// always frames the file; subsequent clauses name the behaviors that
// are present (setups declared, initCommands, readiness / health). The
// detection is a simple substring probe — the yaml is canonical Zerops
// shape, so the stanza names are stable — and never re-parses the body.
func yamlIntroSentence(yamlBody string) string {
	const intro = "The main configuration file — place at repository root. It tells Zerops how to build, deploy and run your app."
	lower := strings.ToLower(yamlBody)
	var setups []string
	for _, s := range []string{"dev", "prod", "stage", "worker"} {
		if strings.Contains(yamlBody, "- setup: "+s) {
			setups = append(setups, s)
		}
	}
	var detail []string
	if len(setups) == 1 {
		detail = append(detail, fmt.Sprintf("declares one setup (`%s`)", setups[0]))
	} else if len(setups) > 1 {
		quoted := make([]string, len(setups))
		for i, s := range setups {
			quoted[i] = "`" + s + "`"
		}
		detail = append(detail, fmt.Sprintf("declares %d setups (%s)", len(setups), strings.Join(quoted, ", ")))
	}
	if strings.Contains(yamlBody, "initCommands:") {
		var ops []string
		if strings.Contains(lower, "migrate") || strings.Contains(lower, "migration") {
			ops = append(ops, "migrations")
		}
		if strings.Contains(lower, "seed") {
			ops = append(ops, "seed")
		}
		if strings.Contains(lower, "scout:import") || strings.Contains(lower, "scout-import") || strings.Contains(lower, "reindex") {
			ops = append(ops, "search index")
		}
		switch len(ops) {
		case 0:
			detail = append(detail, "runs `initCommands` at boot")
		default:
			detail = append(detail, "runs `initCommands` at boot ("+strings.Join(ops, ", ")+")")
		}
	}
	var gates []string
	if strings.Contains(yamlBody, "readinessCheck:") {
		gates = append(gates, "readiness")
	}
	if strings.Contains(yamlBody, "healthCheck:") {
		gates = append(gates, "health")
	}
	if len(gates) > 0 {
		detail = append(detail, "ships "+strings.Join(gates, " + ")+" checks")
	}
	if len(detail) == 0 {
		return intro
	}
	return intro + " This one " + joinClauses(detail) + "."
}

// joinClauses joins 1..N short clauses with Oxford-comma English ("a",
// "a and b", "a, b, and c"). Zerops recipe yamls rarely exceed 3 clauses
// in an intro.
func joinClauses(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// AssembleCodebaseClaudeMD renders the per-codebase CLAUDE.md for one
// hostname.
//
// Run-16 §6.7a — primary path is the single-slot
// `codebase/<h>/claude-md` fragment authored by the dedicated
// claudemd-author sub-agent. The template carries one extract marker
// (`claude-md`) that resolves to that fragment. Legacy sub-slots
// (`claude-md/service-facts`, `claude-md/notes`) stay accepted by
// isValidFragmentID for back-compat but are NOT substituted by this
// stitch path; recipes still relying on legacy sub-slots ship the
// pre-run-16 template via a separate render path (none today).
func AssembleCodebaseClaudeMD(plan *Plan, hostname string) (string, []string, error) {
	if !codebaseKnown(plan, hostname) {
		return "", nil, fmt.Errorf("unknown codebase %q", hostname)
	}
	tpl, err := readTemplate("codebase_claude.md.tmpl")
	if err != nil {
		return "", nil, err
	}
	body := replaceTokens(tpl, map[string]string{
		"SLUG":      plan.Slug,
		"FRAMEWORK": plan.Framework,
		"HOSTNAME":  hostname,
	})
	// Run-16 §6.7a — primary (and only) path is the single-slot
	// `codebase/<h>/claude-md` fragment authored by the dedicated
	// claudemd-author sub-agent at phase 5. Legacy sub-slot ids
	// (`claude-md/service-facts`, `claude-md/notes`) stay accepted by
	// isValidFragmentID for record-time back-compat, but stitch does
	// NOT synthesize a single-slot fragment from them. The earlier
	// synthesizer reconstructed a body opening with `## Zerops service
	// facts` — the very heading the run-16 slot-shape refusal +
	// finalize validator both reject, making the legacy back-compat
	// path dead-on-arrival (reviewer D-6). Recipes still on the legacy
	// shape fail loudly here with "missing fragment
	// codebase/<h>/claude-md", which is the migration signal the user
	// needs to author the single-slot form via claudemd-author.
	prefix := "codebase/" + hostname
	body, missing, err := substituteFragmentMarkers(body, plan.Fragments, prefix)
	if err != nil {
		return "", nil, fmt.Errorf("assemble codebase/%s CLAUDE.md: %w", hostname, err)
	}
	if err := checkUnreplacedTokens(body); err != nil {
		return "", nil, fmt.Errorf("assemble codebase/%s CLAUDE.md: %w", hostname, err)
	}
	return body, missing, nil
}

// renderRootTokens resolves the root-template tokens. Tier list is
// emitted as a bulleted list, one row per tier, each row showing an
// info link + a deploy-with-one-click link whose URL encodes the tier's
// Folder into the path segment. Tier links carry the `environments/`
// prefix because the user-facing root README ships at recipe-root level
// while tier folders live nested under `environments/<folder>/` (sim +
// production v3 layout). Pre-fix, `[[info]](/folder)` was root-relative
// to filesystem root → 404s; intermediate fix `[[info]](folder)` was
// document-relative without the prefix → resolves to `<root>/<folder>`
// (also doesn't exist). Run-21-prep §RC6.
//
// The engine also writes the same body to `<outputRoot>/README.md`
// (i.e. `environments/README.md` in production + sim). That duplicate
// is a dead artifact — the orchestration / sim driver writes the
// user-facing copy at recipe-root. The duplicate's links resolve to
// `environments/environments/<folder>` and 404, which is fine because
// nothing reads it.
func renderRootTokens(tpl string, plan *Plan) string {
	tiers := Tiers()
	var rows strings.Builder
	for i, t := range tiers {
		if i > 0 {
			rows.WriteByte('\n')
		}
		folderURL := url.PathEscape(t.Folder)
		fmt.Fprintf(&rows, "- **%s** [[info]](environments/%s) — [[deploy with one click]](https://app.zerops.io/recipes/%s?environment=%s)",
			t.Label, folderURL, plan.Slug, tierDeploySuffix(t))
	}
	return replaceTokens(tpl, map[string]string{
		"SLUG":      plan.Slug,
		"NAME":      HumanName(plan),
		"FRAMEWORK": plan.Framework,
		"TIER_LIST": rows.String(),
	})
}

// tierLabelLower returns the sentence-form of a tier label. Preserves
// acronyms (all-uppercase words like "AI", "(CDE)") and lowercases the
// first letter of words that contain any lowercase letter. Used by the
// env README L2 banner sentence ("This is a stage environment for…",
// "This is an AI agent environment for…") so the article + label
// combine into natural English.
//
// Examples:
//
//	"AI Agent"                    → "AI agent"
//	"Remote (CDE)"                → "remote (CDE)"
//	"Stage"                       → "stage"
//	"Small Production"            → "small production"
//	"Highly-available Production" → "highly-available production"
func tierLabelLower(label string) string {
	parts := strings.Fields(label)
	for i, p := range parts {
		if hasLowerASCII(p) {
			parts[i] = lowerFirstASCII(p)
		}
	}
	return strings.Join(parts, " ")
}

// tierArticle returns "a" or "an" depending on whether the tier label's
// first ASCII letter is a vowel. Conservative — pronunciation-aware
// English ("an honest", "a university") is out of scope; the six
// canonical tier labels don't trigger any silent-h or vowel-sound-but-
// consonant-letter cases.
func tierArticle(label string) string {
	for _, r := range label {
		if r >= 'a' && r <= 'z' {
			return articleForVowel(r)
		}
		if r >= 'A' && r <= 'Z' {
			return articleForVowel(r - 'A' + 'a')
		}
	}
	return "a"
}

func articleForVowel(r rune) string {
	switch r {
	case 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}

// hasLowerASCII reports whether s contains any lowercase ASCII letter.
// Used by tierLabelLower to skip all-uppercase acronyms ("AI", "(CDE)")
// when sentence-casing tier labels.
func hasLowerASCII(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

// lowerFirstASCII returns s with its first ASCII letter lowercased.
// Stdlib-only — golang.org/x/text/cases is unnecessary for the six
// canonical tier labels.
func lowerFirstASCII(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}

// tierDeploySuffix returns the tier-suffix form used as the deploy URL's
// ?environment= query value. Maps the Suffix field to the canonical
// recipe-page deploy slug (matches the reference laravel-showcase).
func tierDeploySuffix(t Tier) string {
	switch t.Suffix {
	case "agent":
		return "ai-agent"
	case "remote":
		return "remote-cde"
	case "local":
		return "local"
	case "stage":
		return "stage"
	case "small-prod":
		return "small-production"
	case "ha-prod":
		return "highly-available-production"
	}
	return t.Suffix
}

// replaceTokens performs one pass of string-replace for every {TOKEN} in
// tokens. Order-independent because no token value contains another
// token's key — the plan data is framework/hostname/tier text, never
// uppercase-snake placeholders.
func replaceTokens(tpl string, tokens map[string]string) string {
	out := tpl
	for k, v := range tokens {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

// substituteFragmentMarkers scans a rendered template for every
// <!-- #ZEROPS_EXTRACT_START:NAME# --> ... <!-- #ZEROPS_EXTRACT_END:NAME# -->
// block and replaces the body between markers with
// fragments[prefix/NAME]. Missing fragments are collected and returned
// so the caller can gate on a non-empty list rather than silently
// shipping empty marker blocks. Malformed marker pairs (missing end,
// mismatched names) are preserved verbatim — the unreplaced-token scan
// doesn't catch them, so downstream validators (Workstream D) must.
//
// Run-14 §B.2 (R-13-19): fragment bodies are scanned for unbound engine
// tokens (engineBoundKeys keys spelled `{KEY}` or `${KEY}`) outside
// fenced markdown blocks. Fenced occurrences are intentional code
// examples and pass through; unfenced occurrences return an error
// naming the offending fragment id so the author can locate the
// trigger without spelunking the rendered surface.
func substituteFragmentMarkers(body string, fragments map[string]string, idPrefix string) (string, []string, error) {
	var out strings.Builder
	var missing []string
	cursor := 0
	for {
		start := strings.Index(body[cursor:], extractStartPrefix)
		if start < 0 {
			out.WriteString(body[cursor:])
			break
		}
		absStart := cursor + start
		out.WriteString(body[cursor:absStart])

		nameStart := absStart + len(extractStartPrefix)
		suffixOff := strings.Index(body[nameStart:], extractStartSuffix)
		if suffixOff < 0 {
			out.WriteString(body[absStart:])
			break
		}
		name := body[nameStart : nameStart+suffixOff]
		startMarkerEnd := nameStart + suffixOff + len(extractStartSuffix)
		endMarker := extractEndPrefix + name + extractEndSuffix
		endOff := strings.Index(body[startMarkerEnd:], endMarker)
		if endOff < 0 {
			out.WriteString(body[absStart:])
			break
		}
		absEndStart := startMarkerEnd + endOff
		absEndClose := absEndStart + len(endMarker)

		fragmentID := idPrefix + "/" + name
		frag, ok := fragments[fragmentID]
		if !ok || strings.TrimSpace(frag) == "" {
			// Run-48 KB-optional — spec §Surface 5 permits an empty
			// codebase/<h>/knowledge-base fragment when the IG, yaml
			// comments, and CLAUDE.md cover everything the porter needs.
			// Skip the missing-list add ONLY for this fragment id pattern.
			// Render the marker pair with a blank body so the rendered
			// output is `START\n\nEND` and the assembler's downstream
			// validators see well-formed but empty markers.
			if isKBFragmentID(fragmentID) {
				out.WriteString(extractStartPrefix)
				out.WriteString(name)
				out.WriteString(extractStartSuffix)
				out.WriteString("\n\n")
				out.WriteString(endMarker)
			} else {
				missing = append(missing, fragmentID)
				out.WriteString(body[absStart:absEndClose])
			}
		} else {
			if hits := unfencedEngineTokens(frag); len(hits) > 0 {
				return "", nil, fmt.Errorf("fragment %q contains unbound engine token(s) %s outside a fenced code block — wrap the example in ``` fences or remove the literal", fragmentID, strings.Join(hits, ", "))
			}
			// v9.80.0 Fix 7b — emit a blank line between the START
			// marker and the fragment body. Run-38 evidence: the
			// Zerops dashboard's KB extractor renders content correctly
			// when the START marker is followed by a blank line
			// (laravel-minimal `### Gotchas` shape) but silently drops
			// content when the marker is followed directly by the
			// fragment body (nestjs-showcase `## Tips and Others`
			// shape). The blank line is structural — without it, some
			// markdown parsers treat the next block as a continuation
			// of the HTML comment. Engine-emitted blank line is
			// invariant regardless of fragment shape.
			out.WriteString(extractStartPrefix)
			out.WriteString(name)
			out.WriteString(extractStartSuffix)
			out.WriteString("\n\n")
			out.WriteString(strings.TrimSpace(frag))
			out.WriteByte('\n')
			out.WriteString(endMarker)
		}
		cursor = absEndClose
	}
	return out.String(), missing, nil
}

// isKBFragmentID reports whether the fragment id names a codebase
// knowledge-base fragment (`codebase/<hostname>/knowledge-base`).
// Run-48 spec recalibration permits KB to be empty when the IG, yaml
// comments, and CLAUDE.md cover everything the porter needs;
// substituteFragmentMarkers skips the missing-list add for KB only and
// renders well-formed empty markers in place.
func isKBFragmentID(fragmentID string) bool {
	if !strings.HasPrefix(fragmentID, "codebase/") {
		return false
	}
	if !strings.HasSuffix(fragmentID, "/knowledge-base") {
		return false
	}
	// Reject codebase//knowledge-base (empty hostname) and any path with
	// extra segments between codebase/ and /knowledge-base.
	mid := strings.TrimSuffix(strings.TrimPrefix(fragmentID, "codebase/"), "/knowledge-base")
	if mid == "" || strings.Contains(mid, "/") {
		return false
	}
	return true
}

// unfencedEngineTokens returns engine-bound `{KEY}` matches in body
// that fall OUTSIDE any markdown fenced code block (``` ... ``` or
// `inline`). Run-14 §B.2 — fragment authors writing
// `${HOSTNAME}` inside a fenced example don't trip the pre-processor;
// only bare references in prose are flagged. Plan §7 open question 4:
// inline backtick spans count as fences too.
func unfencedEngineTokens(body string) []string {
	masked := maskFencedRegions(body)
	var hits []string
	seen := map[string]bool{}
	for _, m := range unreplacedTokenRE.FindAllStringIndex(masked, -1) {
		key := masked[m[0]+1 : m[1]-1]
		if !engineBoundKeys[key] {
			continue
		}
		text := body[m[0]:m[1]]
		if seen[text] {
			continue
		}
		seen[text] = true
		hits = append(hits, text)
	}
	return hits
}

// maskFencedRegions returns body with every fenced region replaced by
// equal-length spaces so byte indices remain stable. Triple-backtick
// blocks (``` … ``` on their own lines or terminating mid-line) and
// inline backtick spans (`...`) both qualify.
func maskFencedRegions(body string) string {
	out := []byte(body)
	i := 0
	for i < len(out) {
		if i+3 <= len(out) && out[i] == '`' && out[i+1] == '`' && out[i+2] == '`' {
			end := strings.Index(string(out[i+3:]), "```")
			if end < 0 {
				blank(out[i:])
				return string(out)
			}
			blank(out[i : i+3+end+3])
			i += 3 + end + 3
			continue
		}
		if out[i] == '`' {
			end := strings.Index(string(out[i+1:]), "`")
			if end < 0 {
				i++
				continue
			}
			blank(out[i : i+1+end+1])
			i += 1 + end + 1
			continue
		}
		i++
	}
	return string(out)
}

func blank(b []byte) {
	for i := range b {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}

// checkUnreplacedTokens returns a non-nil error when the rendered body
// still contains {UPPER_SNAKE} patterns whose key is in engineBoundKeys
// — a template carrying an unbound engine token would otherwise ship a
// broken surface. Fragment bodies routinely contain {UPPER} or ${UPPER}
// in code examples (JS template literals, Handlebars, Svelte, Go html/
// template); those keys are NOT in engineBoundKeys and pass the scan.
//
// Run-14 §B.2: occurrences inside markdown fenced regions (``` blocks
// or backtick inline spans) are intentional code examples and pass the
// scan. The pre-substitute hook in substituteFragmentMarkers catches
// fragment-body unfenced literals and names the offending fragment id;
// this post-substitute scan is the safety net for template-side defects.
func checkUnreplacedTokens(body string) error {
	masked := maskFencedRegions(body)
	leftover := unreplacedTokenRE.FindAllStringIndex(masked, -1)
	if len(leftover) == 0 {
		return nil
	}
	var unbound []string
	seen := map[string]bool{}
	for _, m := range leftover {
		key := masked[m[0]+1 : m[1]-1]
		if !engineBoundKeys[key] {
			continue
		}
		text := body[m[0]:m[1]]
		if seen[text] {
			continue
		}
		seen[text] = true
		unbound = append(unbound, text)
	}
	if len(unbound) == 0 {
		return nil
	}
	return fmt.Errorf("template has unbound engine tokens: %s", strings.Join(unbound, ", "))
}

// codebaseKnown reports whether a hostname matches one of the plan's
// codebases. Used to gate fragment writes that reference a codebase
// (any codebase/<hostname>/<name> fragment id).
func codebaseKnown(plan *Plan, hostname string) bool {
	return validateCodebaseHostname(plan, hostname) == nil
}

// validateCodebaseHostname returns nil when hostname matches a Plan
// codebase, an actionable error otherwise. Run-11 gap N-1 — the error
// names the Plan codebase list AND the slot-vs-codebase distinction
// (slot hostnames like `appdev` are SSHFS mount names; the codebase
// hostname is the bare logical name).
func validateCodebaseHostname(plan *Plan, hostname string) error {
	if plan == nil {
		return fmt.Errorf("no Plan loaded — call update-plan first")
	}
	for _, c := range plan.Codebases {
		if c.Hostname == hostname {
			return nil
		}
	}
	known := make([]string, 0, len(plan.Codebases))
	for _, c := range plan.Codebases {
		known = append(known, c.Hostname)
	}
	return fmt.Errorf(
		"unknown codebase %q (Plan codebases: %v); if you used a slot hostname like 'appdev'/'appstage', use the bare codebase name (e.g. 'app') — slot is the SSHFS mount, codebase is the logical name",
		hostname, known,
	)
}

// readTemplate reads an engine template from the embedded content tree.
func readTemplate(name string) (string, error) {
	return readAtom("templates/" + name)
}

// readCodebaseYAML reads the committed zerops.yaml at <sourceRoot>. Returns
// the body verbatim so inline comments survive unchanged into the embedded
// IG item #1. Missing-file is not an error — the caller degrades gracefully.
func readCodebaseYAML(sourceRoot string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(sourceRoot, "zerops.yaml"))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
