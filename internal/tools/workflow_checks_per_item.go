package tools

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkPerIGItemStandalone enforces that each `### N. <title>` block in
// the integration-guide fragment carries its own teaching surface — at
// least one fenced code block, a platform-anchor token in the FIRST
// prose paragraph, AND (v8.82 §4.5) a concrete failure-mode anchor
// somewhere in the item's prose that names what breaks if the code is
// NOT applied.
//
// Rationale: aggregate IG-fragment floors invite leaning on neighbors.
// v20 apidev IG #2 ("Binding to 0.0.0.0") was 3 sentences plus 2 lines
// of code; the explanation lived in the comment block of the zerops.yaml
// shown in IG #1. The IG #2 block leaned on the IG #1 block. The
// per-item rule forces every block to stand alone.
//
// The symptom-anchor rule (v8.82 §4.5) brings IG per-item parity with
// the gotcha causal-anchor rule: gotchas must name a SPECIFIC
// mechanism + CONCRETE failure mode; IG items now must do the same.
// Without it, items ship as "add this code" without naming what
// platform failure the code prevents — the reader learns the what
// without the why.
//
// IG #1 (structural position, not content pattern) is grandfathered:
// "Adding zerops.yaml" is the config itself, not a failure prevention.
// Symptom-anchor only fires for items at index > 0.
//
// Platform-anchor tokens are intentionally inclusive — Zerops actor
// names (Zerops, L7, balancer, container, runtime, mount), Zerops
// mechanism names (zsc, execOnce, healthCheck, readinessCheck, subdomain,
// initCommands, deployFiles), and the service-discovery env-var pattern
// (${X_hostname}, etc.). The bar is "the prose names something Zerops
// does", not the much-narrower causal-anchor rule that applies to gotchas.
func checkPerIGItemStandalone(igFragment, hostname string) []workflow.StepCheck {
	if igFragment == "" {
		return nil
	}
	ig := extractFragmentContent(igFragment, "integration-guide")
	if ig == "" {
		// Caller may pass the fragment content directly (already
		// extracted) — try as-is.
		ig = igFragment
	}
	items := splitIGIntoItems(ig)
	if len(items) == 0 {
		return nil
	}

	var failures []string
	for idx, item := range items {
		issues := item.violations(idx)
		for _, msg := range issues {
			failures = append(failures, fmt.Sprintf("%q: %s", item.title, msg))
		}
	}

	checkName := hostname + "_ig_per_item_standalone"
	if len(failures) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s integration-guide items must stand alone — each `### N.` block needs (a) at least one fenced code block, (b) at least one platform-anchor token in the first prose paragraph (Zerops/L7/balancer/runtime/container/static base/zsc/execOnce/healthCheck/readinessCheck/subdomain/initCommands/deployFiles/${X_hostname}/etc.), AND (c) for items beyond IG #1, a CONCRETE failure-mode anchor in the prose body — an HTTP status code, a backtick-quoted error name, or a strong symptom verb (rejects/deadlocks/drops/crashes/times out/returns wrong content-type/breaks/hangs/throws). Items that lean on a neighboring block for the why are decorative — copy the relevant sentence into THIS block. The symptom anchor mirrors the gotcha causal-anchor rule: IG teaches the what + why + what-breaks. Findings: %s",
			hostname, strings.Join(failures, "; "),
		),
	}}
}

// igItem holds one `### N. <title>` block of an integration-guide
// fragment with its prose split from its code blocks.
//
// allProse holds every non-code line of the item concatenated — used by
// the v8.82 §4.5 symptom-anchor rule, which scans the whole item body
// (not just the first paragraph) so the author can place the mechanism
// in para 1 and the failure mode in para 2 if the shape reads better.
type igItem struct {
	title         string
	firstProsePar string
	allProse      string
	codeBlocks    int
}

// violations reports every rule this item fails. idx is the item's
// 0-based position within the integration-guide fragment; idx == 0 is
// the grandfathered zerops.yaml block and skips the symptom-anchor
// check.
func (i igItem) violations(idx int) []string {
	var msgs []string
	if i.codeBlocks == 0 {
		msgs = append(msgs, "no fenced code block — IG items must ship copy-pasteable code")
	}
	if !containsPlatformAnchor(i.firstProsePar) {
		msgs = append(msgs, "first prose paragraph names no Zerops mechanism (the why is missing)")
	}
	// v8.82 §4.5: IG causal-anchor parity. Grandfather IG #1 — it's the
	// zerops.yaml block itself, not a failure prevention step. Every
	// subsequent item must name a concrete failure mode somewhere in its
	// prose body so the reader sees the WHY this code adjustment exists.
	if idx > 0 && !hasConcreteFailureMode(i.allProse+" "+i.title) {
		msgs = append(msgs, "prose body names no concrete failure mode (HTTP status, quoted error name, or symptom verb: rejects/drops/crashes/times out/breaks/hangs/throws) — what breaks if the reader skips this step?")
	}
	return msgs
}

// splitIGIntoItems walks an integration-guide fragment and returns one
// igItem per H3 heading. Items before the first H3 are dropped.
//
// For every item we capture:
//   - title: the H3 text (with "N. " enumeration stripped)
//   - firstProsePar: the first non-empty prose paragraph outside code
//     fences (used by the platform-anchor rule)
//   - allProse: every non-empty prose line outside code fences joined
//     with single spaces (used by the v8.82 §4.5 symptom-anchor rule
//     which needs to scan the whole item body)
//   - codeBlocks: number of fenced code blocks opened inside the item
func splitIGIntoItems(ig string) []igItem {
	lines := strings.Split(ig, "\n")
	var items []igItem
	var cur *igItem
	var firstParBuf strings.Builder
	var allProseBuf strings.Builder
	firstParDone := false
	inFence := false

	flush := func() {
		if cur == nil {
			return
		}
		cur.firstProsePar = strings.TrimSpace(firstParBuf.String())
		cur.allProse = strings.TrimSpace(allProseBuf.String())
		items = append(items, *cur)
		cur = nil
		firstParBuf.Reset()
		allProseBuf.Reset()
		firstParDone = false
		inFence = false
	}

	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "### ") {
			flush()
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			// Strip enumeration "N. " prefix consistent with
			// extractIntegrationGuideHeadings.
			title = stripIGEnumeration(title)
			cur = &igItem{title: title}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			if inFence {
				cur.codeBlocks++
				firstParDone = true
			}
			continue
		}
		if inFence {
			continue
		}
		// Outside code fence. Collect the first non-empty prose
		// paragraph (until a blank line ends it) AND the full prose
		// body for the symptom-anchor rule.
		if !firstParDone {
			if trimmed == "" {
				if firstParBuf.Len() > 0 {
					firstParDone = true
				}
			} else {
				if firstParBuf.Len() > 0 {
					firstParBuf.WriteByte(' ')
				}
				firstParBuf.WriteString(trimmed)
			}
		}
		if trimmed != "" {
			if allProseBuf.Len() > 0 {
				allProseBuf.WriteByte(' ')
			}
			allProseBuf.WriteString(trimmed)
		}
	}
	flush()
	return items
}

func stripIGEnumeration(s string) string {
	digits := 0
	for digits < len(s) && s[digits] >= '0' && s[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits+1 < len(s) && s[digits] == '.' && s[digits+1] == ' ' {
		return s[digits+2:]
	}
	return s
}

// platformAnchorTokens is the inclusive list of Zerops-anchored tokens
// that count as a "this prose names something the platform does". Wider
// than specificMechanismTokens (causal-anchor) on purpose — IG items
// can introduce a step at any abstraction level; the bar is just
// "anchors to Zerops at all".
var platformAnchorTokens = []string{
	"zerops", "l7", "balancer", "container", "runtime", "mount",
	"sshfs", "zsc", "execonce", "healthcheck", "readinesscheck",
	"subdomain", "initcommands", "deployfiles", "buildcommands",
	"static base", "nginx", "managed service",
	"verticalautoscaling", "mincontainers", "corepackage",
	"buildfromgit", "envvariables", "envsecrets", "ports",
	"httpsupport",
}

var anchorEnvVarRefRe = envVarRefRe // re-use the same pattern from causal-anchor

func containsPlatformAnchor(prose string) bool {
	if prose == "" {
		return false
	}
	low := strings.ToLower(prose)
	for _, tok := range platformAnchorTokens {
		if strings.Contains(low, tok) {
			return true
		}
	}
	return anchorEnvVarRefRe.MatchString(prose)
}

// checkEnvCommentUniqueness enforces that, within a single env import
// .yaml, the lead comment block of each service is distinguishable from
// every other service's lead comment by content tokens. Catches the
// "agent copy-pastes the same rationale across services with only the
// hostname swapped" pattern.
//
// Methodology: for each service block (introduced by `- hostname: X`),
// collect the contiguous comment lines immediately preceding it. Tokenize
// (lower, stopword-strip, alphanum-only). Pairwise Jaccard. If any pair
// exceeds the templated threshold (0.6), fail with both hostnames.
//
// envName scopes the check name so multiple env files surface
// independently.
func checkEnvCommentUniqueness(yamlContent, envName string) []workflow.StepCheck {
	if yamlContent == "" {
		return nil
	}
	blocks := extractServiceCommentBlocks(yamlContent)
	if len(blocks) < 2 {
		return nil
	}
	type tok struct {
		host   string
		tokens map[string]bool
	}
	var prepared []tok
	for h, body := range blocks {
		t := tokenizeComment(body)
		if len(t) < 4 {
			continue
		}
		prepared = append(prepared, tok{host: h, tokens: t})
	}
	if len(prepared) < 2 {
		return nil
	}

	type pair struct{ a, b string }
	var collisions []pair
	for i := 0; i < len(prepared); i++ {
		for j := i + 1; j < len(prepared); j++ {
			j2 := jaccard(prepared[i].tokens, prepared[j].tokens)
			if j2 >= envCommentUniquenessThreshold {
				collisions = append(collisions, pair{a: prepared[i].host, b: prepared[j].host})
			}
		}
	}
	checkName := envName + "_service_comment_uniqueness"
	if len(collisions) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	var detail []string
	for _, p := range collisions {
		detail = append(detail, fmt.Sprintf("%s↔%s", p.a, p.b))
	}
	sort.Strings(detail)
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s service lead-comment blocks are templated — multiple services share near-identical rationale text. Each service's comment must name a mechanism unique to that service in this recipe (e.g. worker mentions queue group / NATS drain; api mentions readinessCheck / request handoff; static frontend mentions Nginx / asset serving). Colliding pairs: %s.",
			envName, strings.Join(detail, ", "),
		),
	}}
}

// envCommentUniquenessThreshold — Jaccard score above which two
// service comments are considered templated. 0.6 is the empirical
// floor that admits v20-style "minContainers: 2 because rolling
// deploys, AND service-specific reasoning" comments while catching
// pure copy-paste with only the hostname swapped.
const envCommentUniquenessThreshold = 0.6

// extractServiceCommentBlocks walks the YAML and returns a map from
// service hostname to the contiguous comment lines immediately above
// that service's `- hostname: X` declaration. Comments not adjacent to
// a service block are dropped.
func extractServiceCommentBlocks(yamlContent string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(yamlContent, "\n")
	var pendingComments []string
	hostnameRe := regexp.MustCompile(`^\s*-\s*hostname:\s*([A-Za-z0-9_-]+)\s*$`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			body := strings.TrimLeft(trimmed, "#")
			body = strings.TrimSpace(body)
			pendingComments = append(pendingComments, body)
			continue
		}
		if m := hostnameRe.FindStringSubmatch(line); m != nil {
			if len(pendingComments) > 0 {
				out[m[1]] = strings.Join(pendingComments, " ")
			}
			pendingComments = nil
			continue
		}
		// Non-comment, non-hostname line — drop pending unless the line is
		// blank (a blank line within a comment group should not flush the
		// pending comments because the agent often separates comment + block
		// with a blank line — keep the buffer until we hit non-blank non-
		// comment content or the next hostname).
		if trimmed == "" {
			continue
		}
		pendingComments = nil
	}
	return out
}

// envCommentStopWords are tokens that appear in nearly every service
// comment without conveying service-specific meaning. Strip them before
// computing Jaccard so the threshold reflects content overlap, not
// boilerplate overlap.
var envCommentStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true, "being": true,
	"this": true, "that": true, "these": true, "those": true, "of": true, "to": true,
	"in": true, "on": true, "at": true, "for": true, "with": true, "by": true,
	"from": true, "into": true, "as": true, "it": true, "its": true,
	"so": true, "because": true, "if": true, "when": true, "while": true,
	"service": true, "services": true, "container": true, "containers": true,
	"runs": true, "running": true, "managed": true, "production": true,
	"can": true, "may": true, "must": true, "should": true,
	"each": true, "all": true, "any": true, "every": true,
	"replica": true, "replicas": true, "node": true, "nodes": true,
}

var alphaNumWordRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_-]*`)

func tokenizeComment(body string) map[string]bool {
	out := map[string]bool{}
	for _, w := range alphaNumWordRe.FindAllString(body, -1) {
		w = strings.ToLower(w)
		if envCommentStopWords[w] {
			continue
		}
		if len(w) < 3 {
			continue
		}
		out[w] = true
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersect := 0
	for k := range a {
		if b[k] {
			intersect++
		}
	}
	union := len(a) + len(b) - intersect
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}
