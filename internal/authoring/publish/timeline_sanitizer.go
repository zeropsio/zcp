package publish

import (
	"fmt"
	"regexp"
)

// Run-40 ENG-2 — TIMELINE.md sanitizer.
//
// The recipe agent free-authors TIMELINE.md from its session-log
// metadata, which carries the author's real Zerops project ID,
// hostname hashes, machine paths, and machine-path-anchored URLs.
// Run-39 shipped these verbatim to the porter-facing tarball:
// `7HfLxoquTxiNEg1fD4Xo7w` (project ID), `apidev-2304-3000.prg1.zerops.app`
// (hostname-with-project-hash), `/var/www/zcprecipator/nestjs-showcase/`
// (author machine path). The deliverable is meant to generalize across
// porters; embedding author data leaks identity and breaks the
// "recipe ships to any project" guarantee.
//
// SanitizeTimeline rewrites these patterns to anonymized placeholders
// before the file enters the export tarball. The on-disk TIMELINE.md
// at the recipe-author's location is not mutated — sanitization
// happens between read and tar-write so the user's local copy is
// preserved while the shipped artifact is clean.
//
// Service count substitution (S8 closure): when ServiceCount is
// non-zero the sanitizer replaces "provisioned N services" prose
// claims with the plan-derived count. Run-39 claimed "14 services"
// when 11 actually shipped — the agent miscounted free-text. The
// engine has the correct number; trust the plan.
//
// Diagnosed in plans/run-40-evidence-grounded-plan.md §"S2-2", §"S3-1",
// §"S3-2", §"S8".

// SanitizeTimelineOpts carries optional substitutions the sanitizer
// applies on top of the always-on redactions.
type SanitizeTimelineOpts struct {
	// ServiceCount, when non-zero, substitutes any "provisioned N
	// services" prose with the canonical count derived from
	// plan.Services + plan.Codebases.
	ServiceCount int
}

// SanitizeTimeline returns a copy of body with author-data leaks and
// engine-vocabulary redacted. Idempotent — running it twice yields
// the same output.
//
// Redactions, in order:
//  1. Project IDs in `(id `xxx`)` form OR the keyword-less
//     `Zerops project `<slug>` (`<id>`)` form the TIMELINE prompt
//     actually emits (run-40 regression: the keyword-anchored
//     redactor missed the actual shape).
//  2. Session UUIDs in `Session: `<uuid>“ lines — the TIMELINE
//     prompt emits the session ID alongside the project ID;
//     run-40 leaked the UUID because the original sanitizer
//     scope didn't cover it.
//  3. Hostname-with-project-hash pattern `<host>(dev|stage)-<digits>
//     -<port>.<zone>.zerops.app` AND the no-port `base: static`
//     variant `<host>stage-<digits>.<zone>.zerops.app` (run-40
//     gap: regex required `-<port>` segment, missed static-runtime
//     stage URLs).
//  4. Author machine paths under `/var/www/zcprecipator/<slug>/` —
//     the zcprecipator output-root convention. Replaced with
//     `<output-root>/`.
//  5. Author machine paths under `/Users/<name>/` — macOS dev paths.
//     Replaced with `<machine-path>/`.
//  6. Engine-vocabulary tokens — MCP tool names (`zerops_*`), phase
//     commands (`complete-phase`, `enter-phase`, `record-fragment`,
//     etc.), validator IDs the agent free-cites in TIMELINE
//     narrative. Replaced with porter-readable placeholder
//     `<engine-detail>` because the surrounding prose remains
//     readable while the engine-internal vocabulary disappears.
//     Run-40 plan §ENG-2 listed these as part of the spec; the
//     original implementation didn't ship them.
//  7. Optional: service count substitution (see SanitizeTimelineOpts).
func SanitizeTimeline(body []byte, opts SanitizeTimelineOpts) []byte {
	out := string(body)

	// Two project-ID redactors — keyword-anchored "(id `xxx`)" AND
	// the actual TIMELINE emit shape "Zerops project `<slug>`
	// (`<id>`)". The latter must run BEFORE the former because the
	// keyword-anchored regex doesn't match the latter at all (they
	// don't overlap), but ordering keeps idempotence simple.
	out = projectIDProjectKeywordRedactor.ReplaceAllString(out, "Zerops project `<slug>` (`<project-id>`)")
	out = projectIDRedactor.ReplaceAllString(out, "(id `<project-id>`)")
	out = sessionUUIDRedactor.ReplaceAllString(out, "Session: `<session-id>`")
	out = hostnameHashWithPortRedactor.ReplaceAllString(out, "${1}<id>${3}${4}")
	out = hostnameHashNoPortRedactor.ReplaceAllString(out, "${1}<id>${3}")
	out = zcprecipatorPathRedactor.ReplaceAllString(out, "<output-root>/")
	out = usersPathRedactor.ReplaceAllString(out, "<machine-path>/")
	out = engineMCPToolRedactor.ReplaceAllString(out, "<engine-detail>")
	out = enginePhaseCommandRedactor.ReplaceAllString(out, "<engine-detail>")

	if opts.ServiceCount > 0 {
		out = provisionedServicesRedactor.ReplaceAllString(out, fmt.Sprintf("provisioned %d services", opts.ServiceCount))
	}

	return []byte(out)
}

// projectIDRedactor matches the keyword-anchored parenthetical idiom
// `(id `<22 base62 chars>`)`. Some Zerops IDs use _ or - so the
// character class is base62 + URL-safe punctuation. Kept for back-
// compat with older TIMELINE author shapes; the run-40+ shape is
// covered by projectIDProjectKeywordRedactor.
var projectIDRedactor = regexp.MustCompile("\\(id `[A-Za-z0-9_-]{20,28}`\\)")

// projectIDProjectKeywordRedactor matches the actual TIMELINE prompt
// emit shape: `Zerops project `<slug>` (`<id>`)`. Run-40 gap — the
// original projectIDRedactor required the literal `id ` keyword
// inside the parentheses, but the prompt produces bare backticked
// IDs. Anchoring on the preceding "Zerops project `<slug>`" keeps
// the regex narrow enough to avoid false positives on documentation
// or quoted strings.
var projectIDProjectKeywordRedactor = regexp.MustCompile("Zerops project `[a-zA-Z0-9_-]+` \\(`[A-Za-z0-9_-]{20,28}`\\)")

// sessionUUIDRedactor matches `Session: `<uuid>“ lines from the
// TIMELINE prompt's run-header. UUIDs are 36 chars with hyphens at
// positions 8-12-16-20-12 (the canonical 8-4-4-4-12 shape). Anchored
// on the literal `Session: ` prefix the prompt emits so this redactor
// doesn't strip UUIDs that legitimately appear in porter-facing
// content elsewhere.
var sessionUUIDRedactor = regexp.MustCompile("Session: `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`")

// hostnameHashWithPortRedactor matches the Zerops generated-subdomain
// shape `<host>(dev|stage)-<project-hash>-<port>.<zone>.zerops.app`.
// Project hash is 3-5 numeric digits; zone is 3-8 alphanumerics.
//
// Capture groups:
//
//	1 — host + role suffix (e.g. "apidev-")
//	2 — project hash digits (redacted)
//	3 — "-<port>" (e.g. "-3000")
//	4 — ".<zone>.zerops.app"
var hostnameHashWithPortRedactor = regexp.MustCompile(`([a-z][a-z0-9-]+(?:dev|stage)-)(\d{3,5})(-\d{2,5})(\.[a-z0-9]+\.zerops\.app)`)

// hostnameHashNoPortRedactor matches the no-port stage subdomain
// shape `<host>stage-<project-hash>.<zone>.zerops.app` — what
// `base: static` runtimes mint (no port suffix because Nginx serves
// 80/443 implicitly). Run-40 gap: hostnameHashWithPortRedactor
// required `-<port>`, so `appstage-2311.prg1.zerops.app` passed
// through unchanged.
//
// Capture groups:
//
//	1 — host + role suffix
//	2 — project hash digits (redacted)
//	3 — ".<zone>.zerops.app"
var hostnameHashNoPortRedactor = regexp.MustCompile(`([a-z][a-z0-9-]+(?:dev|stage)-)(\d{3,5})(\.[a-z0-9]+\.zerops\.app)`)

// zcprecipatorPathRedactor matches `/var/www/zcprecipator/<slug>/`
// (the zcprecipator engine's output-root convention). The slug
// segment is 2-64 chars of recipe-name shape (lowercase + hyphens).
var zcprecipatorPathRedactor = regexp.MustCompile(`/var/www/zcprecipator/[a-z][a-z0-9-]{1,63}/`)

// usersPathRedactor matches macOS dev paths `/Users/<name>/`. The
// name segment is 1-32 chars of unix-username shape.
var usersPathRedactor = regexp.MustCompile(`/Users/[a-zA-Z][a-zA-Z0-9._-]{0,31}/`)

// provisionedServicesRedactor matches the free-text service-count
// claim from the TIMELINE prompt's provision section: "provisioned N
// services" where N is a small integer. Run-39 wrote "14" when the
// real count was 11; the engine substitutes the plan-derived count.
var provisionedServicesRedactor = regexp.MustCompile(`\bprovisioned \d{1,3} services\b`)

// engineMCPToolRedactor matches the Zerops MCP tool names the agent
// free-cites in TIMELINE narrative. The porter doesn't operate these
// tools — they're engine-author internal. Run-40 plan §ENG-2 listed
// them in the strip-list. The replacement `<engine-detail>` keeps the
// surrounding prose grammatical while removing the engine-vocab leak.
//
// Whitelist of legitimate porter-facing Zerops tokens that must NOT
// match: `zerops.app`, `zerops.io`, `zerops.yaml` (the file), `zsc`
// (the runtime binary). The regex anchors on `zerops_` (underscore)
// which is the MCP-tool-naming convention; the dotted/dashed forms
// stay untouched.
var engineMCPToolRedactor = regexp.MustCompile(`\bzerops_[a-z_]+\b`)

// enginePhaseCommandRedactor matches the engine's recipe-state-machine
// commands the agent free-cites in TIMELINE narrative. These are
// MCP-action verbs (`complete-phase`, `enter-phase`, `record-fragment`,
// `stitch-content`, `build-subagent-prompt`, etc.) — engine-author
// internal vocabulary that doesn't belong on porter-facing surfaces.
// Run-40 plan §ENG-2 listed these in the strip-list; the implementation
// gap shipped them verbatim to porter.
//
// Anchored as exact tokens; case-sensitive. The replacement
// `<engine-detail>` makes the surrounding prose read as "an internal
// engine step" without naming the step.
var enginePhaseCommandRedactor = regexp.MustCompile(`\b(?:complete-phase|enter-phase|record-fragment|record-fact|stitch-content|build-subagent-prompt|build-brief|verify-subagent-dispatch|fill-fact-slot|resolve-chain|emit-yaml|update-plan)\b`)
