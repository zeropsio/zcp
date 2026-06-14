package recipe

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Run-48 Surface 5 recalibration — kb-self-inflicted-reversible gate.
//
// Refuses KB content (H3 sections OR `### Gotchas` bullets) whose
// symptom fires only when the porter UNDOES a directive the recipe
// already ships. The IG itself prevents the symptom; the KB entry
// teaches what would go wrong if the porter removed code the recipe
// ships. Per spec §Counter-examples "Self-inflicted-reversible",
// these belong in CLAUDE.md or yaml comments at most, never on the
// recipe-page KB.
//
// Design (per the brief's Q4 guidance):
//
//   - JOIN KEY: (codebase_hostname, surface_kind, directive_path).
//     surface_kind ∈ {zerops_yaml, integration_guide_slot, claude_md}.
//   - TRIGGER DETECTION: scan KB body for verbal patterns; each
//     pattern maps to a (surface_kind, directive_locator) hypothesis.
//   - DISCRIMINATOR: if the recipe SHIPS the directive on that surface
//     for that codebase, the KB symptom is self-inflicted-reversible.
//     Refuse with a message naming the directive + surface + KB span.
//
// v1 scope: pattern-driven detection covering the run-48 audit's six
// known cases. False-negatives are acceptable at v1 (porter pays them
// in unhelpful-but-not-wrong KB content); false-positives are the
// cost to minimize (any pattern hit must be backed by a verifiable
// directive scan of recipe artifacts before firing).

// kbSelfInflictedSurfaceKind names the recipe-side surface the
// directive ships on. Used in violation messages so the agent can
// jump to the right surface to confirm.
type kbSelfInflictedSurfaceKind string

const (
	kbSelfInflictedSurfaceZeropsYAML kbSelfInflictedSurfaceKind = "zerops_yaml"
	kbSelfInflictedSurfaceIGSlot     kbSelfInflictedSurfaceKind = "integration_guide_slot"
	kbSelfInflictedSurfaceClaudeMD   kbSelfInflictedSurfaceKind = "claude_md"
)

// kbSelfInflictedPattern is one verbal hypothesis: when the KB body
// matches Trigger, check whether the recipe ships the matching
// directive (via Directive) on the named codebase. If it does, fire.
type kbSelfInflictedPattern struct {
	// Name is a human-readable label for the pattern, used in the
	// violation message ("self-inflicted-reversible: <name>").
	Name string
	// Trigger is a case-insensitive regex run against the KB candidate
	// body (one bullet, or one H3 section). At least one signal phrase
	// must match for the discriminator to run.
	Trigger *regexp.Regexp
	// Surface names the recipe-side surface where the directive ships.
	// Used in the refusal message so the agent can locate the artifact
	// that proves the symptom is self-inflicted-reversible.
	Surface kbSelfInflictedSurfaceKind
	// DirectiveDescription is the human-readable directive name shown
	// in the refusal message (e.g. "ignoreEnvFile: true",
	// "exposedHeaders").
	DirectiveDescription string
	// HasDirective reports whether the recipe ships the directive for
	// this codebase. plan + hostname uniquely identify the codebase;
	// the function reads plan.Fragments OR the on-disk
	// <SourceRoot>/zerops.yaml as appropriate for the surface. Return
	// true when the directive is present; the gate refuses on true.
	HasDirective func(plan *Plan, hostname string) bool
}

// kbSelfInflictedPatterns is the v1 pattern table. Order doesn't
// matter — every pattern runs on every KB candidate body; multiple
// hits produce multiple violations.
var kbSelfInflictedPatterns = []kbSelfInflictedPattern{
	{
		Name:                 "env-file-in-deployed-tree",
		Trigger:              regexp.MustCompile(`(?i)(no\s+` + "`" + `\.env` + "`" + `\s+file|\` + ".env" + `\s+file\s+(?:in\s+the\s+)?deployed|porter\s+creates?\s+(?:a\s+)?\.env)`),
		Surface:              kbSelfInflictedSurfaceZeropsYAML,
		DirectiveDescription: "`ignoreEnvFile: true` (or equivalent directive that prevents the porter from shipping a `.env` file)",
		HasDirective:         hasIgnoreEnvFileShipped,
	},
	{
		Name:                 "custom-response-headers-undefined-from-spa",
		Trigger:              regexp.MustCompile(`(?i)(custom\s+response\s+headers?.{0,40}(?:undefined|return\s+(?:as\s+)?` + "`" + `?undefined)|exposed[Hh]eaders|access-control-expose-headers)`),
		Surface:              kbSelfInflictedSurfaceIGSlot,
		DirectiveDescription: "`exposedHeaders` (the SPA's CORS expose-headers allowlist is shipped via the IG / yaml; removing it is the only way to hit this symptom)",
		HasDirective:         hasExposedHeadersShipped,
	},
	{
		Name:                 "start-directive-on-base-static",
		Trigger:              regexp.MustCompile(`(?i)` + "`" + `?start:` + "`" + `?\s+(?:directive\s+)?on\s+` + "`" + `?base:\s*static`),
		Surface:              kbSelfInflictedSurfaceZeropsYAML,
		DirectiveDescription: "`base: static` without a `start:` directive (the recipe ships the static runtime without `start:`; the symptom only fires when the porter adds `start:`)",
		HasDirective:         hasStaticBaseWithoutStartShipped,
	},
	{
		Name:                 "execonce-key-collision",
		Trigger:              regexp.MustCompile(`(?i)(relation\s+["` + "`" + `']\w+["` + "`" + `']\s+already\s+exists|already\s+exists.{0,30}(?:co-deployed|migrator\s+race|shared\s+execOnce))`),
		Surface:              kbSelfInflictedSurfaceZeropsYAML,
		DirectiveDescription: "per-codebase `execOnce` keys in `initCommands` (the recipe scopes init-command keys per codebase; the collision only fires when the porter adds a sibling codebase reusing a key)",
		HasDirective:         hasScopedExecOnceKeysShipped,
	},
	// NOTE: the run-48 "ioredis-auth-against-unauth-valkey" pattern was
	// DELETED 2026-06-14 — it had the platform fact inverted. Valkey on
	// Zerops ENFORCES auth (requirepass on the `default` user; an
	// unauthenticated connection returns NOAUTH), so omitting the cache
	// password is a real deploy-breaking defect, NOT a self-inflicted-
	// reversible trap. The correct check is positive: a cache consumer
	// MUST wire ${cache_password} / ${cache_connectionString}. See
	// gateCacheConsumerWiresPassword in gate_cache_auth.go.
}

// gateKBSelfInflictedReversible runs the kb-self-inflicted-reversible
// patrol on every codebase's KB fragment. Severity: blocking.
//
// One violation per (codebase, pattern) match. The message names the
// directive the recipe ships, the surface it ships on, and the
// matched KB span so the agent can locate the offending content.
func gateKBSelfInflictedReversible(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.Hostname == "" {
			continue
		}
		fragID := fmt.Sprintf("codebase/%s/knowledge-base", cb.Hostname)
		body, ok := ctx.Plan.Fragments[fragID]
		if !ok {
			continue
		}
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		path := filepath.Join(cb.SourceRoot, "README.md")
		for _, pat := range kbSelfInflictedPatterns {
			if !pat.Trigger.MatchString(body) {
				continue
			}
			if !pat.HasDirective(ctx.Plan, cb.Hostname) {
				continue
			}
			matched := pat.Trigger.FindString(body)
			out = append(out, Violation{
				Code:     "kb-self-inflicted-reversible",
				Path:     path,
				Severity: SeverityBlocking,
				Message: fmt.Sprintf(
					"codebase/%s KB content (%q) names a symptom that only fires when the porter UNDOES a directive the recipe SHIPS: %s on surface %s. Per spec §Surface 5 \"Self-inflicted-reversible\", this belongs in CLAUDE.md or yaml comments at most — the KB serves porters EVALUATING the recipe or arriving via search after a real platform trap; a trap whose precondition is undoing the recipe's own fix has a null reader.",
					cb.Hostname, trimForKBMessage(matched), pat.DirectiveDescription, pat.Surface,
				),
			})
		}
	}
	return out
}

// trimForKBMessage shortens a matched KB span for the refusal message.
// Mirrors trimForMessage in validators_codebase.go but stays local so
// the gate stays self-contained.
func trimForKBMessage(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

// ─── HasDirective scanners ────────────────────────────────────────────
//
// Each scanner reads the recipe's shipped artifacts (plan.Fragments
// for IG slots / KB / CLAUDE.md, on-disk yaml for whole-yaml) and
// returns true when the directive the pattern hypothesizes is shipped
// for the given codebase.
//
// Limitations of v1 (per the brief — false-negatives acceptable):
//   - Some scanners fall through to "true" by default for codebases
//     whose scaffold the gate cannot peek into (chain-parent codebases,
//     early-phase plans). The fall-through is safe under the gate's
//     refusal-on-true semantics — only direct evidence triggers a
//     refusal; absence-of-evidence never does.
//   - The scanners are textual scans of recipe artifacts, not AST
//     analyses. Comment-only mentions can produce false positives;
//     comment-stripped scans handle the common cases. Document any
//     known FP in the pattern's Name + violation message so the
//     agent has the context to disambiguate.

// codebaseYAMLBody returns the codebase's shipped zerops.yaml body —
// the agent's whole-yaml fragment when present (Run-21-prep contract),
// falling back to on-disk <SourceRoot>/zerops.yaml otherwise.
func codebaseYAMLBody(plan *Plan, hostname string) string {
	if plan == nil {
		return ""
	}
	fragID := fragmentIDCodebaseZeropsYAML(hostname)
	if frag, ok := plan.Fragments[fragID]; ok && strings.TrimSpace(frag) != "" {
		return frag
	}
	// Fall through to on-disk yaml.
	raw, err := readCodebaseYAMLForHost(plan, hostname)
	if err != nil {
		return ""
	}
	return raw
}

// stripAllYAMLComments drops `# ... end-of-line` content from every
// line of a yaml body — including inline trailing comments on data
// lines — so directive-presence scans don't false-positive on prose
// inside comments. Distinct from stitch_yaml.go::stripYAMLComments,
// which preserves inline trailing comments because stitch-idempotency
// only needs to strip standalone comment lines; the directive-scan
// callsite here needs both stripped (an inline comment can still
// mention a directive name that the gate would otherwise read as
// "shipped").
//
// Crude: doesn't handle `#` inside quoted strings, but yaml env-var
// declarations and directive keys in the recipe corpus don't place
// comment chars inside quoted values. Acceptable for v1 directive
// scans. Closes the F4 false-positive flagged by codex code-review:
// a yaml body whose ONLY mention of `exposedHeaders` was a `#`
// comment previously made `hasExposedHeadersShipped` return true even
// when the directive wasn't actually shipped in executable yaml.
func stripAllYAMLComments(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for _, line := range strings.SplitAfter(s, "\n") {
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = line[:idx]
		}
		out.WriteString(line)
	}
	return out.String()
}

// codebaseClaudeMDBody returns the codebase's shipped CLAUDE.md
// fragment body when present (empty string otherwise).
func codebaseClaudeMDBody(plan *Plan, hostname string) string {
	if plan == nil {
		return ""
	}
	fragID := "codebase/" + hostname + "/claude-md"
	return plan.Fragments[fragID]
}

// codebaseIGBody returns the concatenation of every IG slot fragment
// the codebase ships (run-16 §6.7 slotted form) + the legacy single
// fragment form for back-compat. Used by IG-surface scanners.
func codebaseIGBody(plan *Plan, hostname string) string {
	if plan == nil {
		return ""
	}
	prefix := "codebase/" + hostname + "/integration-guide"
	var out strings.Builder
	if legacy, ok := plan.Fragments[prefix]; ok {
		out.WriteString(legacy)
		out.WriteByte('\n')
	}
	for k, v := range plan.Fragments {
		if strings.HasPrefix(k, prefix+"/") {
			out.WriteString(v)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

var (
	// hasIgnoreEnvFileShipped — the recipe ships
	// `ignoreEnvFile: true` (or equivalent) which prevents the .env
	// file shadowing the platform-injected env. Matched against the
	// codebase's whole-yaml fragment OR CLAUDE.md hints.
	ignoreEnvFileRE = regexp.MustCompile(`(?i)ignoreenvfile\s*:\s*true`)
)

func hasIgnoreEnvFileShipped(plan *Plan, hostname string) bool {
	if ignoreEnvFileRE.MatchString(stripAllYAMLComments(codebaseYAMLBody(plan, hostname))) {
		return true
	}
	// CLAUDE.md is markdown — yaml comment-strip does not apply; the
	// mention of the directive in CLAUDE.md prose is the signal we want.
	if ignoreEnvFileRE.MatchString(codebaseClaudeMDBody(plan, hostname)) {
		return true
	}
	return false
}

var (
	// exposedHeadersRE — recipe ships an `exposedHeaders` allowlist
	// (typically in the SPA's CORS config under IG or yaml).
	exposedHeadersRE = regexp.MustCompile(`(?i)(exposed[hH]eaders|access-control-expose-headers)`)
)

func hasExposedHeadersShipped(plan *Plan, hostname string) bool {
	// IG body is markdown; yaml comment-strip does not apply (markdown
	// `#` chars are headings, not comments). The yaml-body scan strips
	// comments so a `# exposedHeaders is required for ...` comment in
	// the recipe yaml does not false-positive as a shipped directive.
	if exposedHeadersRE.MatchString(codebaseIGBody(plan, hostname)) {
		return true
	}
	if exposedHeadersRE.MatchString(stripAllYAMLComments(codebaseYAMLBody(plan, hostname))) {
		return true
	}
	return false
}

// baseStaticRE — the codebase's yaml has a `base: static` line
// (the static runtime, which silently ignores `start:`). Once the
// recipe ships `base: static`, the symptom fires only when the
// porter ADDS `start:`; we don't need to check the porter's
// hypothetical `start:` — it would BE the trigger.
var baseStaticRE = regexp.MustCompile(`(?im)^\s*base\s*:\s*static\b`)

func hasStaticBaseWithoutStartShipped(plan *Plan, hostname string) bool {
	yaml := stripAllYAMLComments(codebaseYAMLBody(plan, hostname))
	if !baseStaticRE.MatchString(yaml) {
		return false
	}
	// `base: static` is present. The trap fires only when the porter
	// ADDS `start:`. The recipe ships base: static; the discriminator
	// is true regardless of whether start: appears (a recipe shipping
	// both base: static + start: is a recipe defect, not a KB topic).
	return true
}

var (
	// initCommandsKeyRE — match `initCommands:` followed by a block of
	// commands. Per-codebase scoping is hard to verify without parsing
	// yaml, but presence of initCommands at all signals the recipe
	// ships init-commands.
	initCommandsRE = regexp.MustCompile(`(?im)^\s*initCommands\s*:`)
	// execOnceWithKeyRE — `zsc execOnce <key>` (anywhere in the yaml)
	// signals per-codebase key scoping.
	execOnceWithKeyRE = regexp.MustCompile(`(?i)zsc\s+execOnce\s+\S+`)
)

func hasScopedExecOnceKeysShipped(plan *Plan, hostname string) bool {
	yaml := stripAllYAMLComments(codebaseYAMLBody(plan, hostname))
	// Discriminator: the recipe ships initCommands AND zsc execOnce
	// with explicit keys (the per-codebase scoping). Both required;
	// initCommands without explicit zsc execOnce keys doesn't ship the
	// scoping. False-negative path: a recipe whose initCommands uses
	// a different command name (not `zsc execOnce`) but still scopes
	// keys won't be detected; v1 acceptable.
	if !initCommandsRE.MatchString(yaml) {
		return false
	}
	if !execOnceWithKeyRE.MatchString(yaml) {
		return false
	}
	return true
}
