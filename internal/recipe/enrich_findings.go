package recipe

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Run-45 Pillar C — engine-side determinism for the formulaic finding
// fields. The audit sub-agent emits a SLIM findings JSON; the main
// agent dispatches `zerops_recipe action=enrich-findings` and receives
// an enriched form with three fields filled deterministically:
//
//   - `fragmentId`           — derived from `surface` + `scope` (short-
//                              form codebase name).
//   - `classification`       — read from facts.jsonl's `candidateClass`
//                              for the matching topic + scope; falls back
//                              to a surface-default when no fact matches.
//   - `suggestedReplacement` — rendered from the Citation Map as the
//                              canonical form-(b) markdown link, only
//                              when `topic` is set AND the failure mode
//                              is REWRITE (citation-shaped fix).
//
// The sub-agent never composes these fields. Pre-run-45 monkey-patch
// pattern: every brief revision added prose demanding the agent emit
// `classification` / `suggestedReplacement`; every dogfood run found the
// agent didn't. Engine-side rendering closes the cycle by removing the
// burden from the agent entirely.
//
// Slim input shape (matches `phase_entry.md §"What you produce"`):
//
//	{
//	  "findings": [
//	    {
//	      "surface": "S3"|"S4"|"S5"|"S6"|"S7",
//	      "scope":   "api"|"app"|"worker"|"" ,
//	      "itemReference": "<file:line OR fragment-id OR stem>",
//	      "surfaceTestFailureMode": "DROP"|"MOVE-TO-S<n>"|"REWRITE",
//	      "topic":  "rolling-deploys"|...|"",
//	      "severity": "blocker"|"advisory",
//	      "rationale": "<one paragraph>"
//	    }
//	  ]
//	}
//
// Enriched output shape — same fields PLUS the three filled by engine.

// SlimFinding is what the audit sub-agent emits. Fields below are
// agent-authored; the engine fills `fragmentId`, `classification`, and
// `suggestedReplacement` on the enriched form.
//
// Run-45 Issue 3 — FactRef disambiguates which `facts.jsonl` record a
// finding's rationale references. Topic-substring matching at the
// classification lookup is ambiguous when two facts share an overlapping
// topic stem (run-44 facts.jsonl carried `no-dotenv-file` and
// `worker-env-var-key-alignment-v2` on the same scope; substring match
// could land on either). The sub-agent already reads facts.jsonl when
// auditing — when its rationale cites a specific fact, it emits
// `factRef` so the engine pre-fills classification from that exact
// fact's `candidateClass`. Shape: `<topic>@<recordedAt>` —
// recordedAt is RFC3339 (auto-stamped on Append, guaranteed unique per
// topic in normal authoring flow). Without `factRef`, the engine falls
// back to the surface default (no topic-substring fuzziness).
type SlimFinding struct {
	Surface                string `json:"surface"`
	Scope                  string `json:"scope,omitempty"`
	ItemReference          string `json:"itemReference"`
	SurfaceTestFailureMode string `json:"surfaceTestFailureMode"`
	Topic                  string `json:"topic,omitempty"`
	Severity               string `json:"severity,omitempty"`
	Rationale              string `json:"rationale"`
	FactRef                string `json:"factRef,omitempty"`
}

// EnrichedFinding extends SlimFinding with engine-derived fields. The
// main agent reads this shape and copies fields verbatim onto its
// `record-fragment mode=replace` ACT path.
type EnrichedFinding struct {
	SlimFinding
	FragmentID           string `json:"fragmentId,omitempty"`
	Classification       string `json:"classification,omitempty"`
	SuggestedReplacement string `json:"suggestedReplacement,omitempty"`
}

// FindingsEnvelope is the JSON shape the audit sub-agent emits and the
// enriched response carries. Single `findings` key holds the list.
type FindingsEnvelope struct {
	Findings []SlimFinding `json:"findings"`
}

// EnrichedFindingsEnvelope is the enriched response.
type EnrichedFindingsEnvelope struct {
	Findings []EnrichedFinding `json:"findings"`
}

// EnrichFindingsInput is what `action=enrich-findings` consumes. The
// findings JSON may live in `Findings` (parsed) or `FindingsJSON` (raw
// string the main agent pasted verbatim from the sub-agent's emit). At
// least one must be non-empty.
type EnrichFindingsInput struct {
	Findings     *FindingsEnvelope `json:"findings,omitempty"`
	FindingsJSON string            `json:"findingsJson,omitempty"`
}

// fragmentIDForSurface derives the canonical short-form fragment id
// from (surface, scope). Scope is the codebase short-form host
// (`api`/`app`/`worker`) for S4/S5/S6/S7; empty (or a tier-index
// string) for S3.
func fragmentIDForSurface(surface, scope, itemReference string) string {
	switch surface {
	case "S4":
		// Per-codebase Integration Guide. Item reference may name an
		// IG slot; pass through when the reference already encodes
		// `<n>`. Default to the IG-base fragment when no slot is
		// teased out of the reference.
		if scope == "" {
			return ""
		}
		// Detect an explicit IG slot in itemReference (e.g. "#3" /
		// "IG #2" / "integration-guide/2"). When present, render the
		// slotted form; otherwise the base IG fragment.
		if slot := extractIGSlot(itemReference); slot != "" {
			return fmt.Sprintf("codebase/%s/integration-guide/%s", scope, slot)
		}
		return fmt.Sprintf("codebase/%s/integration-guide", scope)
	case "S5":
		if scope == "" {
			return ""
		}
		return fmt.Sprintf("codebase/%s/knowledge-base", scope)
	case "S6":
		if scope == "" {
			return ""
		}
		return fmt.Sprintf("codebase/%s/claude-md", scope)
	case "S7":
		if scope == "" {
			return ""
		}
		return fmt.Sprintf("codebase/%s/zerops-yaml", scope)
	case "S3":
		// Tier import.yaml comments. Scope may be a tier index ("0".."5")
		// or a host name for the per-host block, or empty for the
		// project block. When scope looks like a host, render the
		// per-host shape; otherwise render the project shape (engine
		// can't deduce the tier index from S3 + scope alone).
		if scope == "" {
			return "env/<N>/import-comments/project"
		}
		return fmt.Sprintf("env/<N>/import-comments/%s", scope)
	default:
		return ""
	}
}

// extractIGSlot finds an integer IG slot in itemReference. Looks for
// "IG #<n>" / "integration-guide/<n>" / "#<n>" anywhere in the string.
// Returns the digit string or "" when no slot present.
func extractIGSlot(itemReference string) string {
	// Try "integration-guide/<n>" first.
	if _, rest, ok := strings.Cut(itemReference, "integration-guide/"); ok {
		return leadingDigits(rest)
	}
	// Try "IG #<n>".
	if _, rest, ok := strings.Cut(itemReference, "IG #"); ok {
		return leadingDigits(rest)
	}
	// Bare "#<n>".
	if _, rest, ok := strings.Cut(itemReference, "#"); ok {
		// Avoid matching fragment URLs like `#envvariables-` — only
		// take the leading digits.
		if d := leadingDigits(rest); d != "" {
			return d
		}
	}
	return ""
}

// leadingDigits returns the run of digit characters at the start of s,
// or "" when s does not start with a digit.
func leadingDigits(s string) string {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return s[:end]
}

// suggestedReplacementForTopic renders the canonical form-(b) markdown
// link for a citation topic. Returns empty when topic is unknown.
//
// Form (b) per `briefs_refinement2.go::citationMapBlock`: friendly
// display name as link text, host+path-matching URL. Friendly display
// names + URLs are pinned (run-43 P7) so the sub-agent doesn't have to
// compose them.
func suggestedReplacementForTopic(topic string) string {
	// Friendly display names — derived from the citation-map block's
	// "OR friendly `<name>`" clauses in briefs_refinement2.go.
	friendlyDisplay := map[string]string{
		"rolling-deploys":         "zero-downtime deploys with multi-container setups",
		"init-commands":           "zsc execOnce + per-deploy key model",
		"object-storage":          "S3-compatible storage on the MinIO backend",
		"env-var-model":           "per-key env shape and cross-service aliases",
		"http-support":            "Zerops L7 balancer + subdomain access",
		"deploy-files":            "deploy-files tilde syntax + static runtime",
		"readiness-health-checks": "readiness + health checks",
	}
	// Managed-service rows use the row label as the link text — pinned
	// in the citation-map block as `managed NATS broker` etc.
	managedFriendly := map[string]string{
		"managed-services-nats":           "managed NATS broker",
		"managed-services-meilisearch":    "managed Meilisearch service",
		"managed-services-postgresql":     "managed PostgreSQL database",
		"managed-services-valkey":         "managed Valkey cache",
		"managed-services-keydb":          "managed KeyDB cache",
		"managed-services-redis":          "managed Redis cache",
		"managed-services-rabbitmq":       "managed RabbitMQ broker",
		"managed-services-kafka":          "managed Kafka broker",
		"managed-services-elasticsearch":  "managed Elasticsearch service",
		"managed-services-typesense":      "managed Typesense service",
		"managed-services-qdrant":         "managed Qdrant service",
		"managed-services-clickhouse":     "managed ClickHouse service",
		"managed-services-mariadb":        "managed MariaDB database",
		"managed-services-object-storage": "managed Object Storage",
		"managed-services-shared-storage": "managed Shared Storage",
	}
	display := friendlyDisplay[topic]
	if display == "" {
		display = managedFriendly[topic]
	}
	if display == "" {
		return ""
	}
	url, ok := CitationGuideURL[topic]
	if !ok || url == "" {
		// Map citation-map keys to guide ids when the topic IS a guide
		// id directly (e.g. "rolling-deploys" is itself a key in
		// CitationGuideURL).
		return ""
	}
	return fmt.Sprintf("[%s](https://%s)", display, url)
}

// classificationFromFactRef resolves a fact identifier of the form
// `<topic>@<recordedAt>` (or a bare topic when the audit emitted only
// the topic — backstop for sub-agent emit drift) against the facts log
// and returns the matched record's `CandidateClass`. Returns "" when
// no fact matches OR when the bare-topic backstop hits an ambiguity
// (two or more facts share the topic but disagree on `candidateClass`).
//
// Run-45 Issue 3 — replaces the run-44 topic-substring lookup, which
// could non-deterministically land on either of two facts sharing an
// overlapping topic stem. The factRef shape is unambiguous: topic +
// recordedAt is unique per fact (RecordedAt is RFC3339, stamped on
// Append). The sub-agent reads facts.jsonl when authoring its
// rationale; emitting `factRef` is the one new disambiguation it does.
//
// Run-45 Issue 3 follow-up — the bare-topic backstop (factRef without
// `@<recordedAt>`) previously returned the first-iterated record's
// class, re-introducing the same ambiguity the `@<recordedAt>` shape
// retired. Live witness in run-42 facts.jsonl: lines 4 + 19 both carry
// `topic=db-env-own-key-aliases` on `scope=api` with classes
// `platform-invariant` and `scaffold-decision`. The corrected
// behavior: when bare-topic resolves to multiple records with
// DIFFERENT candidateClass values, refuse to disambiguate — return ""
// so the engine falls to the surface default. The brief instructs the
// sub-agent to emit `<topic>@<recordedAt>` whenever the topic appears
// more than once; refuse-on-ambiguity surfaces the under-emit
// truthfully rather than manufacturing a wrong answer.
func classificationFromFactRef(facts []FactRecord, factRef string) string {
	if factRef == "" {
		return ""
	}
	// Split topic@recordedAt; fall back to bare-topic match when no
	// '@' separator is present (degraded sub-agent emit shape — still
	// strict-equal topic match, no substring fuzziness).
	topic, recordedAt, hasAt := strings.Cut(factRef, "@")
	if hasAt {
		// Disambiguated shape: topic + recordedAt is unique per fact;
		// first matching record wins (only one can match).
		for _, f := range facts {
			if f.Topic != topic {
				continue
			}
			if f.RecordedAt != recordedAt {
				continue
			}
			if f.CandidateClass == "" {
				continue
			}
			return f.CandidateClass
		}
		return ""
	}
	// Bare-topic backstop. Walk every matching record and collect the
	// distinct CandidateClass values. Ambiguity (two or more distinct
	// classes) returns "" — the engine refuses to pick, and the surface
	// default fires upstream.
	var resolved string
	for _, f := range facts {
		if f.Topic != topic {
			continue
		}
		if f.CandidateClass == "" {
			continue
		}
		if resolved == "" {
			resolved = f.CandidateClass
			continue
		}
		if f.CandidateClass != resolved {
			// Ambiguous bare-topic match — refuse to disambiguate.
			return ""
		}
	}
	return resolved
}

// defaultClassificationForSurface picks a fallback `classification`
// when the facts.jsonl lookup turns up empty. Per spec §"Fact
// classification taxonomy":
//
//   - S5 CODEBASE_KB → `intersection` (the most common KB
//     classification — platform × framework intersections).
//   - S4 CODEBASE_IG → `intersection` (IG items are Zerops-forced
//     framework integrations — intersections too).
//   - other surfaces → "" (S6 CLAUDE.md / S7 zerops-yaml /
//     S3 tier-yaml don't require classification on record-fragment).
func defaultClassificationForSurface(surface string) string {
	switch surface {
	case "S4", "S5":
		return string(ClassIntersection)
	default:
		return ""
	}
}

// EnrichFindings is the engine-side determinism step. Given the
// sub-agent's slim findings + the run's facts log, return the enriched
// shape with fragmentId / classification / suggestedReplacement filled.
//
// Inputs that fail to parse return ("", error). Empty findings list is
// not an error — returns the same empty shape.
//
// Run-45 Issue 2 — DISCARD-class enforcement. A fact whose
// `candidateClass` is DISCARD (framework-quirk / library-metadata /
// self-inflicted) is the spec's "this doesn't belong on a published
// surface" signal. The pre-Issue-2 engine populated those classes onto
// KB/IG findings, which then failed the main agent's
// `classificationCompatibleWithSurface()` check at ACT time —
// re-opening the Pillar C copy-verbatim cycle. The corrected behavior:
// when the resolved fact's candidateClass is incompatible with the
// finding's surface (which, for KB/IG, means ANY of the three DISCARD
// classes), override `surfaceTestFailureMode` to `DROP` and leave
// `classification` empty. The DISCARD class IS the surface test's
// failure rationale; DROP is the correct disposition.
func EnrichFindings(env *FindingsEnvelope, facts []FactRecord) *EnrichedFindingsEnvelope {
	if env == nil {
		return &EnrichedFindingsEnvelope{Findings: []EnrichedFinding{}}
	}
	out := &EnrichedFindingsEnvelope{
		Findings: make([]EnrichedFinding, 0, len(env.Findings)),
	}
	for _, slim := range env.Findings {
		ef := EnrichedFinding{SlimFinding: slim}

		// fragmentId — deterministic from (surface, scope).
		ef.FragmentID = fragmentIDForSurface(slim.Surface, slim.Scope, slim.ItemReference)

		// classification — only the KB/IG record-fragment paths
		// require it; S3/S6/S7 leave it empty so the engine's surface-
		// classification compatibility rule doesn't fire.
		if slim.Surface == "S4" || slim.Surface == "S5" {
			factClass := classificationFromFactRef(facts, slim.FactRef)
			// Issue 2 — DISCARD-class override. When the resolved
			// fact's class can't legally land on this finding's
			// surface (e.g. self-inflicted on CODEBASE_KB), the
			// finding's correct disposition is DROP. Force the
			// failure mode and leave classification empty so the main
			// agent's verbatim copy doesn't trigger a downstream
			// surface-incompatibility reject.
			switch {
			case factClass != "" && !classCompatibleWithAuditSurface(Classification(factClass), slim.Surface):
				ef.SurfaceTestFailureMode = "DROP"
				ef.Classification = ""
			case factClass != "":
				ef.Classification = factClass
			default:
				ef.Classification = defaultClassificationForSurface(slim.Surface)
			}
		}

		// suggestedReplacement — render only when the failure mode is
		// REWRITE AND the topic is a citation-map family. DROP /
		// MOVE-TO removal/migration don't need a replacement string;
		// the action itself is the replacement. Re-read the (possibly-
		// overridden) failure mode from ef, not the slim input.
		if ef.SurfaceTestFailureMode == "REWRITE" && slim.Topic != "" {
			ef.SuggestedReplacement = suggestedReplacementForTopic(slim.Topic)
		}

		out.Findings = append(out.Findings, ef)
	}
	return out
}

// classCompatibleWithAuditSurface maps the audit's surface string
// ("S4"/"S5") to the engine's Surface enum and asks
// `classificationCompatibleWithSurface` whether the pair is allowed
// per the spec compatibility table. Returns true on incompatible
// classes (the surface accepts a DISCARD-class fact onto the
// finding) ONLY when the engine table accepts it; DISCARD classes
// return false (no compatible surface — DROP).
//
// Defensive default: unknown audit surface returns true (no override),
// matching the engine's general "back-compat empty class passes"
// posture in classify.go.
func classCompatibleWithAuditSurface(class Classification, auditSurface string) bool {
	var s Surface
	switch auditSurface {
	case "S4":
		s = SurfaceCodebaseIG
	case "S5":
		s = SurfaceCodebaseKB
	default:
		return true
	}
	return classificationCompatibleWithSurface(class, s) == nil
}

// ParseFindingsJSON parses a raw findings JSON string into a
// FindingsEnvelope. Tolerates a leading code fence (```json ... ```)
// because the audit sub-agent emits the findings wrapped in one.
func ParseFindingsJSON(raw string) (*FindingsEnvelope, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("findings JSON is empty")
	}
	// Strip a leading ```json fence + matching trailing fence.
	if strings.HasPrefix(raw, "```") {
		// Find the first newline and slice past it.
		if nl := strings.Index(raw, "\n"); nl > 0 {
			raw = raw[nl+1:]
		}
		// Strip trailing fence.
		raw = strings.TrimRight(raw, "` \n\r\t")
	}
	var env FindingsEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, fmt.Errorf("decode findings JSON: %w", err)
	}
	return &env, nil
}
