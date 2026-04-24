package recipe

import (
	"fmt"
	"net/url"
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
		return "", nil, err
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
		return "", nil, err
	}
	return body, missing, nil
}

// AssembleCodebaseREADME renders the per-codebase README for one hostname.
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
	if err := checkUnreplacedTokens(body); err != nil {
		return "", nil, err
	}
	return body, missing, nil
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
		return "", nil, err
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
// still contains {UPPER_SNAKE} patterns — a template carrying an
// unbound token would otherwise ship a broken surface.
func checkUnreplacedTokens(body string) error {
	leftover := unreplacedTokenRE.FindAllString(body, -1)
	if len(leftover) == 0 {
		return nil
	}
	return fmt.Errorf("template has unbound tokens: %s", strings.Join(dedupe(leftover), ", "))
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// codebaseKnown reports whether a hostname matches one of the plan's
// codebases. Used to gate fragment writes that reference a codebase
// (any codebase/<hostname>/<name> fragment id).
func codebaseKnown(plan *Plan, hostname string) bool {
	if plan == nil {
		return false
	}
	for _, c := range plan.Codebases {
		if c.Hostname == hostname {
			return true
		}
	}
	return false
}

// readTemplate reads an engine template from the embedded content tree.
func readTemplate(name string) (string, error) {
	return readAtom("templates/" + name)
}
