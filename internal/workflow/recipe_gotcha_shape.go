package workflow

import (
	"regexp"
	"strings"
)

// GotchaShape classifies a gotcha entry as authentic or synthetic.
//
// The v12 audit of nestjs-showcase found roughly half of emitted gotchas
// were scaffold-self-referential: architectural narration ("Shared database
// with the API"), credential descriptions ("NATS authentication"), or
// quirks of the scaffold's own seed script ("TypeORM's afterInsert hooks
// don't fire during raw SQL seeding"). These pass the predecessor-floor
// check — their tokens don't overlap the injected predecessor — but would
// never be encountered by a user integrating from scratch because the
// quirk exists only inside the scaffold's own code.
//
// Authentic gotchas fall into three buckets:
//
//  1. Platform constraint (`Zerops` + a verb, `L7`, `execOnce`, `zsc`,
//     `httpSupport`, `balancer`, `${env_var}` references, `base:`, etc.)
//  2. Concrete failure mode (`fails`, `error`, `crashes`, `blocked`,
//     `returns`, `DNS`, `silent`, `drops`, `502`, `Blocked request`, etc.)
//  3. Framework × platform intersection (a framework name together with a
//     platform term — e.g. "NestJS on Zerops needs 0.0.0.0 bind")
//
// Synthetic gotchas are classified by absence of the above markers and the
// presence of architectural-narration openers (`Shared`, `Uses`, `Same as`).
type GotchaShape int

const (
	// ShapeAuthentic means the gotcha describes a real platform/framework
	// trap a user would hit when integrating from scratch.
	ShapeAuthentic GotchaShape = iota
	// ShapeSynthetic means the gotcha documents the scaffold's own code
	// rather than a real integration trap — architectural narration,
	// credential descriptions, obvious restatements, or quirks that only
	// exist because the scaffold chose a specific implementation.
	ShapeSynthetic
)

// GotchaEntry is a bolded stem paired with the body text that follows it.
// Extracted per bullet from a knowledge-base Gotchas section.
type GotchaEntry struct {
	Stem string
	Body string
}

// platformTerms are Zerops-specific and cross-service platform terms that
// anchor a gotcha to the platform layer. Presence of any of these in the
// body is strong evidence of an authentic platform/framework-intersection
// gotcha rather than narration.
//
// Expanded in the v17 content pass: the v7 gold-standard set contained
// deep insights like "Auto-indexing skips on redeploy seed runs" and
// "NATS queue group mandatory for HA" that the original list did not
// classify as authentic because it only covered ~30 terms. The fix is
// to cover every Zerops mechanism name the agent is likely to reference
// when describing a real platform × framework interaction.
var platformTerms = []string{
	// Core Zerops identity and routing layer
	"zerops",
	"l7 ",
	"l7 balancer",
	"balancer",
	"vxlan",
	"subdomain",
	"reverse proxy",
	"trust proxy",
	"0.0.0.0",

	// zerops.yaml shape — setup, base, build/run phases
	"zerops.yaml",
	"base:",
	"setup: dev",
	"setup: prod",
	"setup: worker",
	"zeropssetup",
	"build.base",
	"run.base",
	"deployfiles",
	"buildcommands",
	"preparecommands",
	"initcommands",
	"readinesscheck",
	"healthcheck",
	"httpsupport",
	"httpget",
	"node runtime",
	"static base",
	"static runtime",
	"nginx",

	// Deploy and container lifecycle primitives
	"appversionid",
	"execonce",
	"zsc ",
	"zsc execonce",
	"retryuntilsuccessful",
	"buildfromgit",
	"cold start",
	"container restart",
	"deploy-time",
	"deployment-time",

	// Env var injection and cross-service references
	"os-level env",
	"project-level env",
	"envsecrets",
	"envvariables",
	"${db_",
	"${redis_",
	"${queue_",
	"${storage_",
	"${search_",
	"${cache_",
	"_hostname}",
	"_port}",
	"_user}",
	"_password}",
	"_apiurl}",
	"_accesskeyid}",
	"_secretaccesskey}",
	"_bucketname}",
	"_masterkey}",
	"_connectionstring}",
	"cross-service",
	"cross-service reference",
	"service-level",
	"project-level",

	// Horizontal and vertical scaling
	"mincontainers",
	"maxcontainers",
	"verticalautoscaling",
	"verticalautoscale",
	"minfreeramgb",
	"horizontal",
	"ha mode",
	"non_ha",
	"rolling deploy",
	"rolling restart",
	"drain connections",

	// Managed service types (context-anchoring). We intentionally keep
	// only Zerops-naming-specific values here — valkey is Zerops' own
	// term, as is the vite-dev phrase. Bare service TYPE names like
	// postgresql/nats/meilisearch/kafka are excluded: those are product
	// names, not Zerops mechanisms, and their presence alone is not
	// enough to anchor a gotcha to the platform layer.
	"valkey",

	// Integration pitfalls surfaced by Zerops' managed-service wiring
	"dns resolution",
	"path-style",
	"virtual-hosted",
	"advisory lock",
	"queue group",
	"noauth",
	"authorization_violation",
	"forcepathstyle",
	"trustproxy",
	"preprocessor",
	"generaterandomstring",

	// Build-time vs run-time distinction (important for SPA recipes)
	"build time",
	"build-time",
	"runtime injection",
	"build.envvariables",
	"run.envvariables",
	"import.meta.env",

	// Dev loop specifics
	"vite dev",
	"startwithoutcode",
	"startwithoutcode:",
	"hot-reload",

	// Production correctness vocabulary — these words anchor a gotcha
	// to a concrete HA / concurrency / lifecycle concern.
	"sigterm",
	"graceful shutdown",
	"in-flight",
	"concurrent containers",
	"race condition",
	"double-process",
	"double process",
	"exactly once",
	"at-most-once",
	"reconnect",
	"backoff",
	"advisory lock contention",
}

// frameworkXPlatformTerms are framework library, SDK, and tool names
// that on their own don't signal authenticity (a gotcha about "Meilisearch"
// in isolation could easily be narration) but when combined with a
// platform term in the same body indicate a genuine framework × platform
// intersection insight. The v7 "Meilisearch SDK is ESM-only" and the
// "Auto-indexing skips on redeploy seed runs" insights are the target
// class here — deep framework-integration knowledge that the
// predecessor-floor filter was wrongly penalizing in v16.
var frameworkXPlatformTerms = []string{
	"sdk",
	"esm",
	"commonjs",
	"cjs",
	"rolldown",
	"rollup",
	"webpack",
	"esbuild",
	"bundler",
	"typeorm",
	"sequelize",
	"prisma",
	"mongoose",
	"activerecord",
	"eloquent",
	"sqlalchemy",
	"gorm",
	"diesel",
	"hibernate",
	"afterinsert",
	"afterupdate",
	"save hooks",
	"save-hooks",
	"entity manager",
	"aws sdk",
	"aws-sdk",
	"putobject",
	"listobjects",
	"getobject",
	"signaturev4",
	"client library",
	"peer dependency",
	"peer-dependency",
	"lockfile",
	"package-lock",
	"composer.json",
	"pyproject",
	"cargo.toml",
	"swagger",
	"openapi",
}

// failureModeTerms signal that a gotcha describes a concrete symptom the
// user would observe. Matched with word boundaries — `crash` matches
// `crashes` but not `crashing` (which typically appears in "prevents X
// from crashing", a description of a feature, not a trap). Even when a
// gotcha lacks platform anchors, a clearly described failure mode is
// enough to qualify as authentic.
var failureModeTerms = []string{
	"fails",
	"fail",
	"crashes",
	"blocked",
	"returns error",
	"returns empty",
	"silently",
	"opaque",
	"drops",
	"empty results",
	"breaks",
	"502",
	"401",
	"dns error",
	"dns resolution",
	"connection refused",
	"unreachable",
	"shadows",
	"shadow the",
	"not reachable",
	"error messages",
	"with errors",
	"errors occur",
}

// failureNegationMarkers flag bodies that describe "prevents X" or
// "avoids Y" — these are descriptions of a feature that avoids a trap,
// not a trap itself. When present, the failure-mode score is suppressed.
var failureNegationMarkers = []string{
	"prevents",
	"avoids",
	"avoid",
	"keeps",
	"stops the",
}

// syntheticOpeners are stem prefixes that signal architectural narration
// rather than a gotcha. "Shared X with Y" describes a fact; it does not
// warn about a trap. Used only to break ties when platform/failure
// evidence is weak — a stem starting with "Shared" can still be authentic
// if the body describes a concrete failure mode.
var syntheticOpeners = []string{
	"shared ",
	"same as",
	"identical to",
}

// credentialDescriptionPattern matches stems like "NATS authentication",
// "S3 credentials", "database credentials" — names of a topic without
// any trap described. These tend to be pure descriptions.
var credentialDescriptionPattern = regexp.MustCompile(`(?i)^(nats|redis|valkey|s3|db|database|postgres|postgresql|mysql|meilisearch|search)\s+(auth|authentication|credentials?|connection)$`)

// obviousRestatementPattern matches stems that restate the base name, e.g.
// "Static base has no Node runtime" — this is the definition of "static"
// and is redundant with understanding what the word means.
var obviousRestatementPattern = regexp.MustCompile(`(?i)\b(static|nodejs|php|python|go)\b\s+(base|runtime)\s+(has|contains|provides)`)

// descriptivePatternSuffix flags stems ending with "pattern" or
// "configuration" — these are descriptive nouns, not warnings. "Lazy
// connection pattern" describes an approach; it does not warn about a
// failure mode. Combined with the negation-marker check, this catches
// the "description-dressed-as-gotcha" shape.
var descriptivePatternSuffix = regexp.MustCompile(`(?i)\b(pattern|configuration|setup|description)\s*$`)

// ClassifyGotcha returns the shape of a gotcha bullet given its stem and
// body. Authentic when there is evidence of platform anchoring, a concrete
// failure mode, or a framework × platform intersection; synthetic otherwise.
//
// Scoring (authenticity points):
//
//   - +2 for the first distinct platform term match in body or stem
//   - +1 for each additional distinct platform term (cap at +5 total)
//   - +2 for any failure-mode term in body (negated by prevent/avoid markers)
//   - +2 for framework × platform intersection — a framework/SDK/ORM
//     term and a platform term co-occur in the same body. Catches the
//     "Meilisearch SDK is ESM-only"-class deep insights the original
//     scoring wrongly filtered out.
//   - −1 for a synthetic opener in stem
//   - −2 for a credential-description stem pattern
//   - −2 for an obvious-restatement stem pattern
//   - −2 for a descriptive-pattern suffix on the stem
//
// Score >= 1 → authentic. Score < 1 → synthetic. The cuts are tuned
// against the v12 audit set plus the v17 expansion (framework × platform
// intersections like "Meilisearch SDK is ESM-only" and "Auto-indexing
// skips on redeploy seed runs").
func ClassifyGotcha(stem, body string) GotchaShape {
	stemLower := strings.ToLower(stem)
	bodyLower := strings.ToLower(body)
	combined := stemLower + " " + bodyLower

	// Hard-synthetic overrides — stem shapes that are high-confidence
	// narration regardless of what's in the body. A stem like "NATS
	// authentication" or "Static base has no Node runtime" describes
	// a design fact rather than a trap, and no amount of body text
	// around it turns that into an authentic integration gotcha.
	// These checks moved from score modifiers to hard overrides in
	// the v17 pass because the expanded platformTerms list made it
	// too easy to accumulate points on body mentions alone.
	stemTrim := strings.TrimSpace(stem)
	if credentialDescriptionPattern.MatchString(stemTrim) {
		return ShapeSynthetic
	}
	if obviousRestatementPattern.MatchString(stem) {
		return ShapeSynthetic
	}
	if descriptivePatternSuffix.MatchString(stem) {
		return ShapeSynthetic
	}

	score := 0

	// Platform terms — accumulate distinct matches, cap at +5 so a
	// single gotcha that name-drops every Zerops mechanism doesn't
	// dominate. First match is +2, each additional distinct match +1.
	platformHits := 0
	const maxPlatformScore = 5
	for _, term := range platformTerms {
		if strings.Contains(combined, term) {
			platformHits++
		}
	}
	if platformHits > 0 {
		score += 2
		if platformHits > 1 {
			bonus := min(platformHits-1, maxPlatformScore-2)
			score += bonus
		}
	}

	hasFailureTerm := false
	for _, term := range failureModeTerms {
		if strings.Contains(bodyLower, term) {
			hasFailureTerm = true
			break
		}
	}
	if hasFailureTerm {
		// Suppress the failure-mode score when the body describes
		// prevention/avoidance — "prevents X from crashing" is a
		// feature description, not a trap.
		negated := false
		for _, marker := range failureNegationMarkers {
			if strings.Contains(bodyLower, marker) {
				negated = true
				break
			}
		}
		if !negated {
			score += 2
		}
	}

	// Framework × platform intersection bonus — catches deep insights
	// where a framework/SDK behavior interacts with a Zerops mechanism.
	// Requires BOTH classes of terms to be present in the same gotcha.
	if platformHits > 0 {
		for _, term := range frameworkXPlatformTerms {
			if strings.Contains(combined, term) {
				score += 2
				break
			}
		}
	}

	for _, opener := range syntheticOpeners {
		if strings.HasPrefix(stemLower, opener) {
			score -= 2
			break
		}
	}

	if score >= 1 {
		return ShapeAuthentic
	}
	return ShapeSynthetic
}

// ExtractGotchaEntries parses a knowledge-base markdown fragment and
// returns both the bolded stem and the body text for each bullet under a
// Gotchas section. Used by the shape classifier, which needs the full body
// (not just the stem) to detect failure-mode signals.
//
// The body is everything after the closing `**` of the bold stem up to
// the next bullet or end-of-section — with leading dash/em-dash separators
// and whitespace stripped.
func ExtractGotchaEntries(content string) []GotchaEntry {
	var entries []GotchaEntry
	lines := strings.Split(content, "\n")

	sectionLevel := 0
	var currentStem string
	var currentBody strings.Builder
	flush := func() {
		if currentStem == "" {
			return
		}
		body := strings.TrimSpace(currentBody.String())
		body = strings.TrimLeft(body, "-—– \t")
		body = strings.TrimSpace(body)
		entries = append(entries, GotchaEntry{Stem: currentStem, Body: body})
		currentStem = ""
		currentBody.Reset()
	}

	for _, line := range lines {
		level := markdownHeadingLevel(line)
		if sectionLevel == 0 {
			if level > 0 && strings.TrimSpace(strings.TrimLeft(line, "# ")) == "Gotchas" {
				sectionLevel = level
			}
			continue
		}
		if level > 0 && level <= sectionLevel {
			break
		}
		trimmed := strings.TrimSpace(line)
		// A new bullet starts with - or * + space + ** — flush the previous
		// entry, capture the stem, and begin accumulating body.
		isBullet := strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ")
		if isBullet {
			rest := strings.TrimSpace(trimmed[2:])
			if strings.HasPrefix(rest, "**") {
				flush()
				inner := rest[2:]
				closeIdx := strings.Index(inner, "**")
				if closeIdx > 0 {
					currentStem = strings.TrimSpace(inner[:closeIdx])
					currentBody.WriteString(inner[closeIdx+2:])
					currentBody.WriteString(" ")
					continue
				}
			}
		}
		if currentStem != "" {
			currentBody.WriteString(trimmed)
			currentBody.WriteString(" ")
		}
	}
	flush()

	return entries
}

// CountAuthenticGotchas returns the number of gotcha entries classified
// as ShapeAuthentic. Mirrors CountNetNewGotchas in shape — both are
// "floor" measurements the predecessor-floor check layer consumes.
func CountAuthenticGotchas(entries []GotchaEntry) int {
	n := 0
	for _, e := range entries {
		if ClassifyGotcha(e.Stem, e.Body) == ShapeAuthentic {
			n++
		}
	}
	return n
}
