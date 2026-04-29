package recipe

import "strings"

// EmittedFactsForCodebase returns engine-pre-emitted fact shells for a
// single codebase at scaffold dispatch time. Run-17 §6 — Class B
// (universal-for-role), Class C umbrella (own-key-aliases), and per-
// managed-service connect shells are retracted in favour of agent-
// recorded porter_change facts captured during deploy phases. Worked
// examples for the deploy-phase shape live in
// `briefs/scaffold/decision_recording.md` and
// `briefs/feature/decision_recording.md`.
//
// The function signature is retained so the brief composer
// (`briefs_content_phase.go`) and dispatch wiring (`workflow.go`)
// continue compiling. The returned slice is always nil; the agent-
// recorded fact stream and engine-emitted tier_decision facts (still
// active via EmittedTierDecisionFacts) are the only fact sources after
// run-17.
//
// Pinned by TestEmittedFactsForCodebase_ReturnsEmpty.
func EmittedFactsForCodebase(_ *Plan, _ Codebase) []FactRecord {
	return nil
}

// citationGuideForServiceType maps a managed-service type (e.g.
// "postgresql@18", "valkey@7", "nats@2.12") to the topic id of its
// per-service knowledge atom in CitationMap. Family-prefix matching;
// version trailers ignored (the connection idiom is family-stable).
//
// Per-service shells are retracted in run-17, but the function is
// retained so the family↔guide contract continues to be pinned by
// TestEmittedFactShell_CitationGuideMatchesCitationMap. Future callers
// (e.g. agent-side citation lookup helpers) read the same mapping.
func citationGuideForServiceType(serviceType string) string {
	family := serviceType
	if i := strings.IndexByte(serviceType, '@'); i > 0 {
		family = serviceType[:i]
	}
	switch family {
	case "postgresql":
		return "managed-services-postgresql"
	case "mariadb":
		return "managed-services-mariadb"
	case "keydb":
		return "managed-services-keydb"
	case "valkey":
		return "managed-services-valkey"
	case "redis":
		return "managed-services-redis"
	case "nats":
		return "managed-services-nats"
	case "rabbitmq":
		return "managed-services-rabbitmq"
	case "kafka":
		return "managed-services-kafka"
	case "meilisearch":
		return "managed-services-meilisearch"
	case "elasticsearch":
		return "managed-services-elasticsearch"
	case "typesense":
		return "managed-services-typesense"
	case "qdrant":
		return "managed-services-qdrant"
	case "clickhouse":
		return "managed-services-clickhouse"
	case "object-storage":
		return "managed-services-object-storage"
	case "shared-storage":
		return "managed-services-shared-storage"
	}
	return ""
}

// EmittedTierDecisionFacts pre-emits one tier_decision fact per cross-
// tier delta plus one per per-service mode change. Run-16 §5.3 — engine
// is 100% the recorder of tier_decision facts (no agent recording site
// during research/provision). Phase-6 sub-agent extends TierContext via
// fill-fact-slot when the auto-derived prose is insufficient.
//
// Whole-tier deltas come from tiers.go::Diff (covers RuntimeMinContainers,
// CPUMode, CorePackage, MinFreeRAMGB, RuntimeMinRAM, ManagedMinRAM,
// RunsDevContainer, plus a whole-tier ServiceMode baseline). Per-service
// deltas come from TierServiceModeDelta — Diff carries one whole-tier
// ServiceMode change but the per-service downgrade rule (§5.3) splits
// the picture across managed services that don't support HA.
func EmittedTierDecisionFacts(plan *Plan) []FactRecord {
	if plan == nil {
		return nil
	}
	tiers := Tiers()
	var out []FactRecord
	for i := 1; i < len(tiers); i++ {
		from := tiers[i-1]
		to := tiers[i]

		// Whole-tier scalar deltas — runtime/CPU/RAM fields. ServiceMode
		// is replaced below by the per-service set, so skip it here.
		for _, change := range Diff(from, to).Changes {
			if change.Field == "ServiceMode" {
				continue
			}
			out = append(out, FactRecord{
				Topic:            "tier-" + tierIndexStr(to.Index) + "-" + tierFieldSlug(change.Field),
				Kind:             FactKindTierDecision,
				Scope:            "env/" + tierIndexStr(to.Index),
				Phase:            "research",
				Tier:             to.Index,
				FieldPath:        change.Field,
				ChosenValue:      change.To,
				Alternatives:     change.From + " (at tier " + tierIndexStr(from.Index) + ")",
				TierContext:      "Tier " + tierIndexStr(to.Index) + " (" + to.Label + ") — " + change.Field + " moves " + change.From + " → " + change.To + ".",
				CandidateClass:   "scaffold-decision",
				CandidateSurface: "ENV_IMPORT_COMMENTS",
				CandidateHeading: change.Field + " at tier " + tierIndexStr(to.Index),
				EngineEmitted:    true,
			})
		}

		// Per-service mode deltas (the §5.3 helper).
		for _, delta := range TierServiceModeDelta(from, to, plan) {
			out = append(out, FactRecord{
				Topic:            "tier-" + tierIndexStr(to.Index) + "-" + delta.Service + "-mode",
				Kind:             FactKindTierDecision,
				Scope:            "env/" + tierIndexStr(to.Index) + "/services." + delta.Service,
				Phase:            "research",
				Tier:             to.Index,
				Service:          delta.Service,
				FieldPath:        "services[name=" + delta.Service + "].mode",
				ChosenValue:      delta.To,
				Alternatives:     delta.From + " (at tier " + tierIndexStr(from.Index) + ")",
				TierContext:      "Tier " + tierIndexStr(to.Index) + " (" + to.Label + ") — " + delta.Service + " mode moves " + delta.From + " → " + delta.To + ".",
				CandidateClass:   "scaffold-decision",
				CandidateSurface: "ENV_IMPORT_COMMENTS",
				CandidateHeading: delta.Service + " " + delta.To + " at tier " + tierIndexStr(to.Index),
				EngineEmitted:    true,
			})
		}
	}
	return out
}

// tierIndexStr renders a tier index as decimal — separated for re-use
// in topic ids and TierContext prose.
func tierIndexStr(i int) string {
	switch i {
	case 0:
		return "0"
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	case 4:
		return "4"
	case 5:
		return "5"
	}
	return "?"
}

// tierFieldSlug normalizes a FieldChange.Field to a topic-friendly slug.
// Camel-case fields stay readable; slug stays stable for topic-id
// uniqueness across runs.
func tierFieldSlug(field string) string {
	return strings.ToLower(field)
}
