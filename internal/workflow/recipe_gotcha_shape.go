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
var platformTerms = []string{
	"zerops",
	"l7 ",
	"l7 balancer",
	"balancer",
	"execonce",
	"zsc ",
	"httpsupport",
	"subdomain",
	"0.0.0.0",
	"vxlan",
	"base:",
	"setup: dev",
	"setup: prod",
	"setup: worker",
	"initcommands",
	"readinesscheck",
	"healthcheck",
	"reverse proxy",
	"trust proxy",
	"cold start",
	"dns resolution",
	"path-style",
	"virtual-hosted",
	"advisory lock",
	"os-level env",
	"build time",
	"build-time",
	"deployfiles",
	"node runtime",
	"static base",
	"static runtime",
	"nginx",
	"valkey",
	"vite dev",
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
//   - +2 for any platform term in body or stem
//   - +2 for any failure-mode term in body
//   - −1 for a synthetic opener in stem
//   - −2 for a credential-description stem pattern
//   - −2 for an obvious-restatement stem pattern
//
// Score >= 1 → authentic. Score < 1 → synthetic. The cuts are tuned
// against the v12 audit set.
func ClassifyGotcha(stem, body string) GotchaShape {
	stemLower := strings.ToLower(stem)
	bodyLower := strings.ToLower(body)
	combined := stemLower + " " + bodyLower

	score := 0

	for _, term := range platformTerms {
		if strings.Contains(combined, term) {
			score += 2
			break
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

	for _, opener := range syntheticOpeners {
		if strings.HasPrefix(stemLower, opener) {
			score--
			break
		}
	}

	if credentialDescriptionPattern.MatchString(strings.TrimSpace(stem)) {
		score -= 2
	}

	if obviousRestatementPattern.MatchString(stem) {
		score -= 2
	}

	if descriptivePatternSuffix.MatchString(stem) {
		score -= 2
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
