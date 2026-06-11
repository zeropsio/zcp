package recipe

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// Surface-test failure-mode literals. Match the JSON wire shape the
// audit sub-agent emits; centralising them removes the goconst-flagged
// repeated string-literal pattern that emerged once Item G added a
// third reader/writer at the auto-override decision.
const (
	FailureModeDROP    = "DROP"
	FailureModeREWRITE = "REWRITE"
)

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
//
// Run-47 Item G — ClassificationAmbiguous + AutoOverrideRefused.
// When the fact's CandidateClass is framework-quirk and the fact's
// topic carries cross-surface evidence (≥2 surfaces OR citation-map
// topic match), the engine REFUSES to apply its DISCARD-class auto-
// override (mode→DROP). The finding's Mode passes through unchanged
// from the sub-agent's emission; ClassificationAmbiguous flags the
// case for the main agent's surface test (S4) to settle disposition.
// The engine does NOT re-classify on ambiguity — that would replace
// one brittle classifier with another. Ambiguity refuses, not
// promotes.
type EnrichedFinding struct {
	SlimFinding
	FragmentID           string `json:"fragmentId,omitempty"`
	Classification       string `json:"classification,omitempty"`
	SuggestedReplacement string `json:"suggestedReplacement,omitempty"`

	// ClassificationAmbiguous — Run-47 Item G. The engine resolved a
	// DISCARD-leaning candidate class (framework-quirk) on a fact
	// whose topic shows cross-surface evidence, so it declined to
	// auto-override the sub-agent's emitted mode. Main agent's surface
	// test settles disposition.
	ClassificationAmbiguous bool `json:"classificationAmbiguous,omitempty"`
	// AutoOverrideRefused — non-empty when ClassificationAmbiguous is
	// true; names the reason the engine refused (for telemetry +
	// audit-trail).
	AutoOverrideRefused string `json:"autoOverrideRefused,omitempty"`
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

// friendlyDisplayNames — derived from the citation-map block's
// "OR friendly `<name>`" clauses in briefs_refinement2.go. Run-46 Item 5
// exposes a public lookup via FriendlyDisplayName so the citation
// validator can do display-text↔URL agreement.
var friendlyDisplayNames = map[string]string{
	"rolling-deploys":         "zero-downtime deploys with multi-container setups",
	"init-commands":           "zsc execOnce + per-deploy key model",
	"object-storage":          "S3-compatible storage on the MinIO backend",
	"env-var-model":           "per-key env shape and cross-service aliases",
	"http-support":            "Zerops L7 balancer + subdomain access",
	"deploy-files":            "deploy-files tilde syntax + static runtime",
	"readiness-health-checks": "readiness + health checks",
}

// managedFriendlyNames — managed-service rows use the row label as the
// link text — pinned in the citation-map block as `managed NATS broker`
// etc.
var managedFriendlyNames = map[string]string{
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

// FriendlyDisplayName returns the friendly display name for a guide id
// (e.g. "rolling-deploys" → "zero-downtime deploys with multi-container
// setups"). Empty when the guide is unknown. Used by the Run-46 Item 5
// citation-display-text validator + the engine-side suggested-
// replacement renderer.
func FriendlyDisplayName(guide string) string {
	if d := friendlyDisplayNames[guide]; d != "" {
		return d
	}
	if d := managedFriendlyNames[guide]; d != "" {
		return d
	}
	return ""
}

// suggestedReplacementForTopic renders the canonical form-(b) markdown
// link for a citation topic. Returns empty when topic is unknown.
//
// Form (b) per `briefs_refinement2.go::citationMapBlock`: friendly
// display name as link text, host+path-matching URL. Friendly display
// names + URLs are pinned (run-43 P7) so the sub-agent doesn't have to
// compose them.
func suggestedReplacementForTopic(topic string) string {
	display := FriendlyDisplayName(topic)
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
	return EnrichFindingsWithPlan(env, facts, nil)
}

// EnrichFindingsWithPlan is the plan-aware variant. When `plan` is
// non-nil and a fact's CandidateClass is framework-quirk, the engine
// checks `hasCrossSurfaceEvidence(fact, plan)`; when evidence exists,
// the DISCARD-class auto-override is REFUSED and the finding is
// emitted with `ClassificationAmbiguous=true` +
// `AutoOverrideRefused=<reason>`; the sub-agent's emitted mode passes
// through unchanged. Main-agent S4 surface test settles disposition.
//
// Plan = nil reproduces pre-Run-47 behavior (no ambiguity check; auto-
// override fires on every DISCARD-class hit). Used by call sites that
// don't have a plan in scope (tests + back-compat for the bare
// EnrichFindings entry point).
//
// Run-47 Item G design — ambiguity REFUSES, doesn't re-classify. The
// engine never decides "this framework-quirk fact is actually
// intersection"; it decides "this fact's classification is unreliable,
// let the main-agent surface test settle it." Replacing one brittle
// classifier with another is exactly the monkey-patch pattern codex
// flagged.
func EnrichFindingsWithPlan(env *FindingsEnvelope, facts []FactRecord, plan *Plan) *EnrichedFindingsEnvelope {
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
			ambiguous := false
			if factClass != "" && plan != nil &&
				Classification(factClass) == ClassFrameworkQuirk &&
				!classCompatibleWithAuditSurface(Classification(factClass), slim.Surface) {
				// Run-47 Item G — before the DISCARD-class auto-override
				// fires for framework-quirk, check whether the fact's
				// topic carries cross-surface evidence. If yes, refuse
				// the override; the main-agent S4 surface test will
				// settle disposition. The engine does NOT re-classify.
				fact := findFactByRef(facts, slim.FactRef)
				if fact != nil && hasCrossSurfaceEvidence(*fact, plan) {
					ambiguous = true
				}
			}
			switch {
			case ambiguous:
				// Mode untouched (slim's mode passes through via
				// EnrichedFinding embedding); Classification stays
				// empty (engine declines to decide).
				ef.ClassificationAmbiguous = true
				ef.AutoOverrideRefused = "framework-quirk class has cross-surface evidence; main-agent S4 surface test settles disposition"
				ef.Classification = ""
			case factClass != "" && !classCompatibleWithAuditSurface(Classification(factClass), slim.Surface):
				// Issue 2 — DISCARD-class override. When the resolved
				// fact's class can't legally land on this finding's
				// surface (e.g. self-inflicted on CODEBASE_KB), the
				// finding's correct disposition is DROP. Force the
				// failure mode and leave classification empty so the
				// main agent's verbatim copy doesn't trigger a
				// downstream surface-incompatibility reject.
				ef.SurfaceTestFailureMode = FailureModeDROP
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
		if ef.SurfaceTestFailureMode == FailureModeREWRITE && slim.Topic != "" {
			ef.SuggestedReplacement = suggestedReplacementForTopic(slim.Topic)
		}

		out.Findings = append(out.Findings, ef)
	}
	return out
}

// findFactByRef looks up a fact record by the audit's FactRef
// (`<topic>@<recordedAt>` or bare `<topic>`). Returns the first
// matching record, or nil. Deterministic — first-match-wins under
// stable iteration of the input slice.
//
// Used by Run-47 Item G's ambiguity check: when the engine considers
// refusing the DISCARD-class override, it needs the fact's topic to
// observe cross-surface evidence in the plan. classificationFromFactRef
// returns only the candidate class; this helper returns the whole
// record so the ambiguity check can read `Topic` (and any other
// fields it might consult in future) directly.
func findFactByRef(facts []FactRecord, factRef string) *FactRecord {
	if factRef == "" {
		return nil
	}
	topic, recordedAt, hasAt := strings.Cut(factRef, "@")
	if hasAt {
		for i := range facts {
			if facts[i].Topic == topic && facts[i].RecordedAt == recordedAt {
				return &facts[i]
			}
		}
		return nil
	}
	for i := range facts {
		if facts[i].Topic == topic {
			return &facts[i]
		}
	}
	return nil
}

// hasCrossSurfaceEvidence reports whether the fact's topic appears
// across enough plan surfaces (or matches a citation-map topic) to
// make the fact's DISCARD-class candidateClass unreliable. The check
// is deterministic + observation-only: it walks the plan's existing
// fragment bodies + EnvComments and counts surfaces whose topic
// candidates include the fact's topic. Returns true iff:
//
//   - The fact's topic resolves to a citation-map guide id (
//     `CitationGuideURL[fact.Topic]` OR `CitationMap[fact.Topic]`
//     hit). Documented platform topics are NOT framework quirks by
//     definition.
//   - OR the fact's topic appears in ≥2 distinct surfaces (S3 env
//     comments / S4 IG / S5 KB / S7 zerops.yaml) via
//     `DetectBulletTopicCandidates`.
//
// The helper does NOT re-classify or promote the fact; it observes
// evidence presence. Run-47 Item G — codex specifically flagged:
// "if `hasCrossSurfaceEvidence` does any classification decision-
// making beyond 'evidence exists / doesn't exist', it's monkey-patch."
func hasCrossSurfaceEvidence(fact FactRecord, plan *Plan) bool {
	if plan == nil || fact.Topic == "" {
		return false
	}
	// Citation-map topic match — directly or via the CitationMap
	// remapping (e.g. "execOnce" → "init-commands" guide).
	if _, ok := CitationGuideURL[fact.Topic]; ok {
		return true
	}
	if guide, ok := CitationMap[fact.Topic]; ok && guide != "" {
		if _, ok2 := CitationGuideURL[guide]; ok2 {
			return true
		}
	}
	// Cross-surface appearance count. A surface "carries" the fact's
	// topic when its fragment body's DetectBulletTopicCandidates output
	// includes the fact's topic (or the citation-map mapped guide id).
	targets := map[string]bool{fact.Topic: true}
	if guide, ok := CitationMap[fact.Topic]; ok && guide != "" {
		targets[guide] = true
	}
	surfaces := 0

	// S3 — EnvComments (project + per-service per tier).
	for _, ec := range plan.EnvComments {
		matched := false
		if strings.TrimSpace(ec.Project) != "" {
			for _, t := range DetectBulletTopicCandidates(ec.Project) {
				if targets[t] {
					matched = true
					break
				}
			}
		}
		if !matched {
			for _, body := range ec.Service {
				if strings.TrimSpace(body) == "" {
					continue
				}
				for _, t := range DetectBulletTopicCandidates(body) {
					if targets[t] {
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
		}
		if matched {
			surfaces++
			break // EnvComments collectively count as a single S3 surface
		}
	}
	// S4 / S5 / S7 — per-codebase fragments.
	for _, cb := range plan.Codebases {
		host := cb.Hostname
		for _, surfKey := range []string{
			"codebase/" + host + "/integration-guide",
			"codebase/" + host + "/knowledge-base",
			"codebase/" + host + "/zerops-yaml",
		} {
			// IG slotted entries — collect IG slot bodies separately
			// since they live under nested keys (`integration-guide/<n>`).
			// We count this surface once per codebase per surface key.
			if surfKey == "codebase/"+host+"/integration-guide" {
				if matchedIG := igTopicAppears(plan.Fragments, host, targets); matchedIG {
					surfaces++
					if surfaces >= 2 {
						return true
					}
					continue
				}
			}
			body := plan.Fragments[surfKey]
			if strings.TrimSpace(body) == "" {
				continue
			}
			matched := false
			for _, t := range DetectBulletTopicCandidates(body) {
				if targets[t] {
					matched = true
					break
				}
			}
			if matched {
				surfaces++
				if surfaces >= 2 {
					return true
				}
			}
		}
	}
	return surfaces >= 2
}

// igTopicAppears reports whether ANY IG fragment (unslotted or
// slotted) for the codebase carries one of the target topics.
// Returns true on first match; cheap-exit deterministic walk.
func igTopicAppears(fragments map[string]string, host string, targets map[string]bool) bool {
	base := "codebase/" + host + "/integration-guide"
	if body := fragments[base]; strings.TrimSpace(body) != "" {
		for _, t := range DetectBulletTopicCandidates(body) {
			if targets[t] {
				return true
			}
		}
	}
	// Slotted: codebase/<host>/integration-guide/<n>. Walk all matching
	// keys deterministically by sorting them first.
	prefix := base + "/"
	var slotKeys []string
	for k := range fragments {
		if strings.HasPrefix(k, prefix) {
			slotKeys = append(slotKeys, k)
		}
	}
	sort.Strings(slotKeys)
	for _, k := range slotKeys {
		body := fragments[k]
		if strings.TrimSpace(body) == "" {
			continue
		}
		for _, t := range DetectBulletTopicCandidates(body) {
			if targets[t] {
				return true
			}
		}
	}
	return false
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
