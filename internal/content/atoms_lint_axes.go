package content

import (
	"regexp"
	"slices"
	"strings"
)

// This file holds the four content-quality axis lint rules introduced
// by atom-corpus-hygiene cycles 2 (axes K/L/M) and 3 (axis N), pinned
// here per engine plan E4. Spec: docs/spec-knowledge-distribution.md
// §11.5/§11.6.
//
// Axis L is HARD-FORBID — no marker escape valve, only the per-line
// allowlist (rare) keyed `<atom>::<line>` in axisLAllowlist for
// per-atom rationale entries.
//
// Axes K, M, N use the comment-marker convention: an atom author who
// wants to keep a phrase that the lint flags adds an inline HTML
// comment like `<!-- axis-k-keep: signal-#3 -->` on the SAME line, the
// IMMEDIATELY PREVIOUS non-blank line, or the IMMEDIATELY FOLLOWING
// non-blank line. Markers suppress the lint hit; reviewers see exactly
// which axis was reviewed. `<!-- axis-{k,m,n}-drop -->` is also
// accepted as an explicit marker for "this should be dropped, leaving
// here pending edit" cases.
//
// Markers are stripped from rendered atom bodies by StripAxisMarkers
// (called from internal/workflow/atom.go::ParseAtom) so they never
// reach the agent — they exist purely as author-side metadata.
//
// Per-axis allowlists (axisLAllowlist, axisKAllowlist, axisMAllowlist,
// axisNAllowlist) carry pre-marker grandfathered entries from the
// initial corpus seed audit. Each entry MUST carry a one-line rationale.
// Their declarations live in atoms_lint_seed_allowlist.go.

// ============================================================================
// Axis L — TITLE-OVER-QUALIFIED (HARD-FORBID env-only title qualifiers)
// ============================================================================

// envOnlyQualifiers are tokens that — when they appear as a STANDALONE
// segment of a title or heading qualifier (split on `—`, parens, commas,
// `+`) — convey nothing the axis-filter framing already conveys. Per
// spec §11.5 Axis L token-level rule.
var envOnlyQualifiers = map[string]struct{}{
	"container":     {},
	"local":         {},
	"container env": {},
	"local env":     {},
}

// titleQualifierSplitter splits header text on the punctuation that
// authors use to introduce qualifiers: em-dash, parens, commas, `+`.
// Each resulting trimmed token is checked against envOnlyQualifiers.
var titleQualifierSplitter = regexp.MustCompile(`[—()\,\+]`)

// headingPattern captures markdown headings (#, ##, ..., ######).
var headingPattern = regexp.MustCompile(`^\s*(#{1,6})\s+(.+?)\s*$`)

// axisLViolations checks the frontmatter `title:` plus every markdown
// heading in the body (outside code fences) for env-only qualifier
// tokens. Hard-forbidden — no marker escape; allowlist key is
// "<atomFile>::<trimmed-line>".
func axisLViolations(ctx atomLintCtx) []AtomLintViolation {
	var out []AtomLintViolation

	// 1. frontmatter title — synthetic line number 2 (right after the
	//    opening `---`); the exact frontmatter line position is rarely
	//    consequential since editors land near the file head anyway.
	if title := ctx.frontmatter["title"]; title != "" {
		if tok := envOnlyQualifierIn(title); tok != "" {
			snippet := `title: "` + title + `"`
			key := ctx.file + "::" + snippet
			if _, allowed := atomLintAllowlist[key]; !allowed {
				if _, allowed := axisLAllowlist[key]; !allowed {
					out = append(out, AtomLintViolation{
						AtomFile: ctx.file,
						Category: "axis-l",
						Pattern:  "title-env-only-qualifier:" + tok,
						Line:     2,
						Snippet:  snippet,
					})
				}
			}
		}
	}

	// 2. body headings.
	for i, line := range ctx.bodyLines {
		if ctx.inCodeFence[i] {
			continue
		}
		m := headingPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		text := m[2]
		tok := envOnlyQualifierIn(text)
		if tok == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		key := ctx.file + "::" + trimmed
		if _, allowed := atomLintAllowlist[key]; allowed {
			continue
		}
		if _, allowed := axisLAllowlist[key]; allowed {
			continue
		}
		out = append(out, AtomLintViolation{
			AtomFile: ctx.file,
			Category: "axis-l",
			Pattern:  "heading-env-only-qualifier:" + tok,
			Line:     ctx.frontmatterLines + i + 1,
			Snippet:  trimmed,
		})
	}
	return out
}

// envOnlyQualifierIn returns the matched env-only token if any token
// from a tokenized title/heading is exactly an env-only qualifier;
// returns "" otherwise. Tokenization splits on em-dash, parens, commas,
// and `+` (for paired qualifiers like "local + push-git").
func envOnlyQualifierIn(text string) string {
	parts := titleQualifierSplitter.Split(text, -1)
	for _, p := range parts {
		t := strings.ToLower(strings.TrimSpace(p))
		if t == "" {
			continue
		}
		if _, hit := envOnlyQualifiers[t]; hit {
			return t
		}
	}
	return ""
}

// ============================================================================
// Axis K — ABSTRACTION-LEAK (candidate detection + marker convention)
// ============================================================================

// axisKLeakPatterns captures the HIGH-signal leak shapes spelled out in
// spec §11.5 Axis K. Each match is a CANDIDATE; the author either
// preserves it with `<!-- axis-k-keep: signal-#N -->` (default for
// HIGH-signal guardrails) or removes it. Without a marker — lint fails.
var axisKLeakPatterns = []*regexp.Regexp{
	// Negation tied to a tool/action (signals #1, #3, #5 from spec).
	// `Don't run X`, `Do NOT use Y`, `Never call Z`, `Never hand-roll …`.
	regexp.MustCompile(`(?i)\b(don't|do not|never)\s+(use|run|call|invoke|hand-roll|edit|push|commit|deploy)\b`),
	// "no SSHFS" / "no dev container" / "no stage pair" / "no GIT_TOKEN"
	// — guardrail-shaped negations (signals #1, #2 from spec).
	regexp.MustCompile(`(?i)\bno\s+(SSHFS|dev container|new container|build container|runtime container|Zerops container|stage pair|GIT_TOKEN|\.netrc)\b`),
	// "X is container-only" / "Y is local-only" — tool-selection guidance
	// where the env exclusion IS the guardrail (signal #3).
	regexp.MustCompile(`(?i)\b(container-only|local-only)\b`),
}

// axisKViolations runs the leak-candidate patterns against each body
// line outside code fences. A match is suppressed iff ANY of:
//   - line has an inline `<!-- axis-k-{keep,drop} -->` marker
//   - immediately previous non-blank line has the marker
//   - immediately next non-blank line has the marker
//   - allowlist entry exists
//
// The three-line marker window matches author intuition: place the
// marker on the line, above, or below the flagged phrase.
func axisKViolations(ctx atomLintCtx) []AtomLintViolation {
	return axisMarkerViolations(ctx, "axis-k", "k", axisKLeakPatterns, axisKAllowlist)
}

// ============================================================================
// Axis M — TERMINOLOGY-DRIFT (canonical-violation detection)
// ============================================================================

// axisMDriftPatterns captures the cluster-#1, #3, #4, #5 canonical
// violations called out in spec §11.5. Cluster-#2 (deploy/redeploy
// per-occurrence) is too contextual for a single regex — left to
// per-cycle Codex review.
var axisMDriftPatterns = []*regexp.Regexp{
	// Cluster #1: "the container" / "service container" without a
	// canonical disambiguating prefix (dev / runtime / build / new /
	// Zerops). RE2 has no look-behind so the canonical-prefix screen
	// runs in axisMViolations after match.
	regexp.MustCompile(`(?i)\bthe container\b`),
	regexp.MustCompile(`(?i)\bservice container\b`),
	// Cluster #3: bare "the platform" without disambiguation.
	regexp.MustCompile(`(?i)\bthe platform\b`),
	// Cluster #4: bare "the tool" — should be `zerops_<name>` or
	// "MCP tool" to disambiguate.
	regexp.MustCompile(`(?i)\bthe tool\b`),
	// Cluster #5: "the agent" / "the LLM" — author-perspective drift;
	// canonical address is "you".
	regexp.MustCompile(`(?i)\bthe agent\b`),
	regexp.MustCompile(`(?i)\bthe LLM\b`),
}

// axisMCanonicalContainerPrefixes accepts a "the container" / "service
// container" usage if a canonical prefix word precedes it within the
// same line. RE2 doesn't support look-behind so we screen post-match.
var axisMCanonicalContainerPrefixes = regexp.MustCompile(
	`(?i)\b(dev|runtime|build|new|Zerops|the dev|the runtime|the build|the new)\s+(the\s+)?container\b`,
)

// axisMViolations runs the drift patterns. Suppressed iff a marker
// `<!-- axis-m-{keep,drop} -->` exists on the line / above / below, or
// allowlist entry exists, or the cluster-#1 canonical-prefix screen
// passes.
func axisMViolations(ctx atomLintCtx) []AtomLintViolation {
	var out []AtomLintViolation
	for i, line := range ctx.bodyLines {
		if ctx.inCodeFence[i] {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, pat := range axisMDriftPatterns {
			match := pat.FindString(line)
			if match == "" {
				continue
			}
			lowered := strings.ToLower(match)
			// Cluster-#1 screen: if the line uses a canonical prefix
			// (`dev container`, `runtime container`, …), allow it.
			if (lowered == "the container" || lowered == "service container") &&
				axisMCanonicalContainerPrefixes.MatchString(line) {
				continue
			}
			if hasNearbyMarker(ctx, i, "m") {
				continue
			}
			key := ctx.file + "::" + trimmed
			if _, allowed := atomLintAllowlist[key]; allowed {
				continue
			}
			if _, allowed := axisMAllowlist[key]; allowed {
				continue
			}
			out = append(out, AtomLintViolation{
				AtomFile: ctx.file,
				Category: "axis-m",
				Pattern:  "drift:" + lowered,
				Line:     ctx.frontmatterLines + i + 1,
				Snippet:  trimmed,
			})
		}
	}
	return out
}

// ============================================================================
// Axis N — UNIVERSAL-ATOM PER-ENV LEAK (env-token detection on universal atoms)
// ============================================================================

// axisNEnvTokens are the per-env phrases spec §11.6 calls out as HIGH-
// risk leak surface for universal atoms.
var axisNEnvTokens = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(locally|your machine|your editor|your IDE)\b`),
	regexp.MustCompile(`(?i)\bSSHFS\b`),
	regexp.MustCompile(`/var/www/`),
	regexp.MustCompile(`(?i)\b(container env|local env)\b`),
	regexp.MustCompile(`(?i)\b(on your CWD|on the mount|via SSH|over SSH)\b`),
}

// axisNViolations runs only on atoms whose `environments:` axis is
// EITHER unset OR carries both values (i.e. universal atoms). Per-env
// atoms (`environments: [local]` only or `environments: [container]`
// only) are exempt — env-specific detail is on-axis there.
//
// Suppressed iff a marker `<!-- axis-n-{keep,drop} -->` exists on the
// line / above / below, or allowlist entry exists.
func axisNViolations(ctx atomLintCtx) []AtomLintViolation {
	if !atomIsUniversal(ctx) {
		return nil
	}
	return axisMarkerViolations(ctx, "axis-n", "n", axisNEnvTokens, axisNAllowlist)
}

// axisHotShellViolations flags raw shell-backgrounding patterns
// (`nohup ...`, `disown`, trailing ` &` on a command line) that should
// route through the dev-server canonical primitive instead. C5 closure
// (audit-prerelease-internal-testing-2026-04-29): the asset-pipeline
// atom used to instruct `ssh ... 'nohup npm run dev > ... &'`, which
// the dev-server-canonical-primitive plan (archived 2026-04-24)
// explicitly forbade for container-env dev-server lifecycle. Sibling
// atoms that legitimately call out the anti-pattern (anti-pattern
// callouts in platform-rules / dev-server-reason-codes) tag with
// `<!-- axis-hot-shell-keep: anti-pattern -->` to keep the prose.
//
// Heuristic patterns (false-positive surface is small — most atoms
// don't invoke shell at all):
//   - `\bnohup\b` — backgrounded persistent process spawn
//   - `\bdisown\b` — explicit detach
//   - `&['"]?\s*$` — trailing ampersand on a non-blank command line,
//     including the `... &'` SSH-remote-quoted form. Excludes `&&`
//     (logical AND) because a `&&` line tail wouldn't end with a single
//     `&` followed by quote/whitespace.
//
// Suppressed iff a marker `<!-- axis-hot-shell-{keep,drop} -->`
// exists on the line / prior non-blank / next non-blank, or the
// shared allowlist contains a matching entry.
func axisHotShellViolations(ctx atomLintCtx) []AtomLintViolation {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\bnohup\b`),
		regexp.MustCompile(`\bdisown\b`),
		regexp.MustCompile(`[^&]&['"]?\s*$`),
	}
	return axisMarkerViolations(ctx, "axis-hot-shell", "hot-shell", patterns, axisHotShellAllowlist)
}

// atomIsUniversal returns true when the atom has no `environments:`
// frontmatter axis or both env values are listed (i.e. the atom fires
// on either env).
func atomIsUniversal(ctx atomLintCtx) bool {
	raw, ok := ctx.frontmatter["environments"]
	if !ok || raw == "" {
		return true
	}
	hasLocal := strings.Contains(strings.ToLower(raw), "local")
	hasContainer := strings.Contains(strings.ToLower(raw), "container")
	return hasLocal && hasContainer
}

// ============================================================================
// Shared marker-driven dispatch (axes K, N share the same shape)
// ============================================================================

// axisMarkerViolations is the shared body-line walker for axes that use
// the inline-marker convention. category is the violation Category
// label ("axis-k" / "axis-n"); markerSuffix is the single-letter axis
// id used in `<!-- axis-X-... -->` markers.
func axisMarkerViolations(
	ctx atomLintCtx,
	category, markerSuffix string,
	patterns []*regexp.Regexp,
	allowlist map[string]string,
) []AtomLintViolation {
	var out []AtomLintViolation
	for i, line := range ctx.bodyLines {
		if ctx.inCodeFence[i] {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, pat := range patterns {
			match := pat.FindString(line)
			if match == "" {
				continue
			}
			if hasNearbyMarker(ctx, i, markerSuffix) {
				continue
			}
			key := ctx.file + "::" + trimmed
			if _, allowed := atomLintAllowlist[key]; allowed {
				continue
			}
			if _, allowed := allowlist[key]; allowed {
				continue
			}
			out = append(out, AtomLintViolation{
				AtomFile: ctx.file,
				Category: category,
				Pattern:  "leak-candidate:" + strings.ToLower(match),
				Line:     ctx.frontmatterLines + i + 1,
				Snippet:  trimmed,
			})
		}
	}
	return out
}

// ============================================================================
// Axis O — STATE-DECLARATIVE-LEAK (narrow, post-Phase-2)
// ============================================================================
//
// Spec §11.2 bullet 5 + §11.8: configuration-conditional state asserted
// as universal fact. Phase 2 of the atom-corpus-verification plan
// surfaced 13+ instances corpus-wide — many already fixed in Cycles
// 1-3, but the regression guard belongs at commit time so re-introduced
// drift fails fast.
//
// Five HIGH-signal anti-phrases. NOT including `status="..."` because
// post-Phase-0b the export status atoms legitimately encode their
// status via the exportStatus: axis — surfacing the value in body
// prose would false-positive on tightly-axed atoms.
var axisOLeakPatterns = []*regexp.Regexp{
	// "is already running" / "is already configured" — deploy-state /
	// readiness assumptions that depend on a redeploy-volatile signal.
	regexp.MustCompile(`(?i)\b(?:is|are)\s+already\s+running\b`),
	// "every service ACTIVE" / "every service is ACTIVE" — service-
	// status assertion that matches managed-service status RUNNING too.
	regexp.MustCompile(`(?i)\bevery\s+service\b[^.]*\bACTIVE\b`),
	// "is empty" / "are empty" tied to a working-tree / mount /
	// container — implies a starting state the axis can't guarantee.
	regexp.MustCompile(`(?i)\b(?:tree|mount|container|workspace)\s+is\s+empty\b`),
	// "landed and verified" — verify-state assertion (see §11.9 rule).
	// The axis model does not pin verify-pass; deployStates:[deployed]
	// covers verify-pass AND verify-fail, so the prose lies in the
	// fail case.
	regexp.MustCompile(`(?i)\blanded\s+and\s+verified\b`),
	// "Bootstrap does NOT ship" — recipe/bootstrap state assertion that
	// holds for some routes but not others. Variant of the pattern
	// captured by the master defect ledger.
	regexp.MustCompile(`(?i)\bBootstrap\s+does\s+NOT\s+ship\b`),
}

// axisOViolations runs the axis-O patterns against each body line.
// Markers are the only escape mechanism (no per-axis allowlist —
// post-Phase-2 the corpus passed clean and seeded allowlists invite
// drift).
func axisOViolations(ctx atomLintCtx) []AtomLintViolation {
	return axisMarkerViolations(ctx, "axis-o", "o", axisOLeakPatterns, nil)
}

// ============================================================================
// Marker extraction + lookup
// ============================================================================

// axisMarkerPattern matches `<!-- axis-{k,m,n,o,hot-shell}-keep[:signal-#N] -->`
// and `<!-- axis-{k,m,n,o,hot-shell}-drop -->`. The captured suffix
// (k / m / n / o / hot-shell) tells the caller which axis a marker
// applies to; the trailing free-form text (signal annotation,
// rationale) is captured but ignored by lint. axis-hot-shell is the
// C5 closure axis flagging raw `nohup`/`disown`/`& *$` SSH
// backgrounding patterns (audit-prerelease-internal-testing-2026-04-29).
// axis-o (added 2026-05-02 per atom-corpus-verification Phase 4) is
// the state-declarative-leak guard; sibling atoms that LEGITIMATELY
// name a state assertion (forbidden-list tables, anti-pattern
// callouts) tag with `axis-o-keep`.
var axisMarkerPattern = regexp.MustCompile(`<!--\s*axis-([kmno]|hot-shell)-(?:keep|drop)(?:\s*:[^>]*)?\s*-->`)

// extractAxisMarkers walks every body line and records which axis
// markers (k/m/n) appear on it. Markers SUPPRESS lint hits on the same
// line, the line immediately above, and the line immediately below.
// Returned map is keyed on body-line index; the value is the slice of
// axis suffixes ("k", "m", "n") active on that line.
func extractAxisMarkers(bodyLines []string) map[int][]string {
	markers := make(map[int][]string)
	for i, line := range bodyLines {
		matches := axisMarkerPattern.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			markers[i] = append(markers[i], m[1])
		}
	}
	return markers
}

// hasNearbyMarker returns true if a marker for axis `suffix` exists on
// `lineIdx`, the prior non-blank line, or the next non-blank line.
func hasNearbyMarker(ctx atomLintCtx, lineIdx int, suffix string) bool {
	if markerHas(ctx.markers[lineIdx], suffix) {
		return true
	}
	for j := lineIdx - 1; j >= 0; j-- {
		if strings.TrimSpace(ctx.bodyLines[j]) == "" {
			continue
		}
		if markerHas(ctx.markers[j], suffix) {
			return true
		}
		break
	}
	for j := lineIdx + 1; j < len(ctx.bodyLines); j++ {
		if strings.TrimSpace(ctx.bodyLines[j]) == "" {
			continue
		}
		if markerHas(ctx.markers[j], suffix) {
			return true
		}
		break
	}
	return false
}

// markerHas reports whether the given marker-axis-letter slice contains
// suffix. nil-safe via slices.Contains.
func markerHas(haystack []string, suffix string) bool {
	return slices.Contains(haystack, suffix)
}

// StripAxisMarkers removes `<!-- axis-{k,m,n}-... -->` comments from a
// rendered atom body. Markers are author-side metadata for the lint
// engine; they MUST NOT leak into the agent-visible synthesized output.
// Lines that become whitespace-only after stripping are dropped; lines
// that retain prose are emitted with the marker substring (and any
// leading whitespace immediately before it) consumed.
//
// Called from internal/workflow/atom.go::ParseAtom right before
// trimming the body.
func StripAxisMarkers(body string) string {
	if !strings.Contains(body, "<!--") {
		return body
	}
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		stripped := axisMarkerStripPattern.ReplaceAllString(line, "")
		if strings.TrimSpace(stripped) == "" && strings.TrimSpace(line) != "" {
			// Marker-only line: drop entirely so we don't accumulate
			// blank lines where prose used to flow.
			continue
		}
		out = append(out, stripped)
	}
	return strings.Join(out, "\n")
}

// axisMarkerStripPattern eats any leading whitespace before a marker on
// the same line, so an inline marker like `… text. <!-- axis-k-keep -->`
// collapses to `… text.` cleanly. Standalone marker lines are removed
// entirely by the StripAxisMarkers caller.
var axisMarkerStripPattern = regexp.MustCompile(`[ \t]*<!--\s*axis-(?:[kmno]|hot-shell)-(?:keep|drop)(?:\s*:[^>]*)?\s*-->`)
