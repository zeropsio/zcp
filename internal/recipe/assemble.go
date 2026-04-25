package recipe

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
	"SLUG":        true,
	"FRAMEWORK":   true,
	"HOSTNAME":    true,
	"TIER_LABEL":  true,
	"TIER_SUFFIX": true,
	"TIER_LIST":   true,
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
	body, missing := substituteFragmentMarkers(body, plan.Fragments, "root")
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
		"SLUG":        plan.Slug,
		"FRAMEWORK":   plan.Framework,
		"TIER_LABEL":  tier.Label,
		"TIER_SUFFIX": tierDeploySuffix(tier),
	})
	prefix := fmt.Sprintf("env/%d", tierIndex)
	body, missing := substituteFragmentMarkers(body, plan.Fragments, prefix)
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
	body := replaceTokens(tpl, map[string]string{
		"SLUG":      plan.Slug,
		"FRAMEWORK": plan.Framework,
		"HOSTNAME":  hostname,
	})
	prefix := "codebase/" + hostname
	body, missing := substituteFragmentMarkers(body, plan.Fragments, prefix)
	yamlBody, err := readCodebaseYAMLForHost(plan, hostname)
	if err != nil {
		return "", nil, fmt.Errorf("assemble codebase/%s README: %w", hostname, err)
	}
	if yamlBody != "" {
		body = injectIGItem1(body, yamlBody)
	}
	if err := checkUnreplacedTokens(body); err != nil {
		return "", nil, fmt.Errorf("assemble codebase/%s README: %w", hostname, err)
	}
	return body, missing, nil
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
	prefix := "codebase/" + hostname + "/claude-md"
	body, missing := substituteFragmentMarkers(body, plan.Fragments, prefix)
	if err := checkUnreplacedTokens(body); err != nil {
		return "", nil, fmt.Errorf("assemble codebase/%s CLAUDE.md: %w", hostname, err)
	}
	return body, missing, nil
}

// renderRootTokens resolves the root-template tokens. Tier list is
// emitted as a bulleted list, one row per tier, each row showing an
// info link + a deploy-with-one-click link whose URL encodes the tier's
// Folder into the path segment. Matches the shape of the reference
// laravel-showcase root README.
func renderRootTokens(tpl string, plan *Plan) string {
	tiers := Tiers()
	var rows strings.Builder
	for i, t := range tiers {
		if i > 0 {
			rows.WriteByte('\n')
		}
		folderURL := url.PathEscape(t.Folder)
		fmt.Fprintf(&rows, "- **%s** [[info]](/%s) — [[deploy with one click]](https://app.zerops.io/recipes/%s?environment=%s)",
			t.Label, folderURL, plan.Slug, tierDeploySuffix(t))
	}
	return replaceTokens(tpl, map[string]string{
		"SLUG":      plan.Slug,
		"FRAMEWORK": plan.Framework,
		"TIER_LIST": rows.String(),
	})
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
func substituteFragmentMarkers(body string, fragments map[string]string, idPrefix string) (string, []string) {
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
			missing = append(missing, fragmentID)
			out.WriteString(body[absStart:absEndClose])
		} else {
			out.WriteString(extractStartPrefix)
			out.WriteString(name)
			out.WriteString(extractStartSuffix)
			out.WriteByte('\n')
			out.WriteString(strings.TrimSpace(frag))
			out.WriteByte('\n')
			out.WriteString(endMarker)
		}
		cursor = absEndClose
	}
	return out.String(), missing
}

// checkUnreplacedTokens returns a non-nil error when the rendered body
// still contains {UPPER_SNAKE} patterns whose key is in engineBoundKeys
// — a template carrying an unbound engine token would otherwise ship a
// broken surface. Fragment bodies routinely contain {UPPER} or ${UPPER}
// in code examples (JS template literals, Handlebars, Svelte, Go html/
// template); those keys are NOT in engineBoundKeys and pass the scan.
func checkUnreplacedTokens(body string) error {
	leftover := unreplacedTokenRE.FindAllString(body, -1)
	if len(leftover) == 0 {
		return nil
	}
	var unbound []string
	seen := map[string]bool{}
	for _, m := range leftover {
		key := m[1 : len(m)-1] // strip { and }
		if !engineBoundKeys[key] {
			continue
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		unbound = append(unbound, m)
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
