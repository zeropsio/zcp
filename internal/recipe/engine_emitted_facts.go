package recipe

import "strings"

// EmittedFactsForCodebase returns the engine-emitted fact set for a
// single codebase at scaffold dispatch time. Run-16 Tranche 2 — the
// engine pre-fills facts whose Why prose is mechanism-level (Class B
// universal-for-role) or whose shape is recipe-stable (Class C umbrella
// + per-service shells per §7.1, §7.2). The agent is responsible for
// filling slots on engine-emitted shells (CandidateHeading on the
// worker no-HTTP fact, Why+Heading on per-managed-service shells) via
// fill-fact-slot at codebase-content phase.
//
// Plan must be non-nil; cb must be a member of plan.Codebases. Result
// order is deterministic (Class B before Class C umbrella before per-
// service shells, sorted by service hostname).
func EmittedFactsForCodebase(plan *Plan, cb Codebase) []FactRecord {
	if plan == nil {
		return nil
	}
	var facts []FactRecord
	facts = append(facts, classBFacts(cb)...)

	consumed := managedServicesConsumedBy(plan, cb)
	if len(consumed) > 0 {
		facts = append(facts, classCUmbrellaFact(cb))
		facts = append(facts, perServiceShells(cb, consumed)...)
	}

	return facts
}

// classBFacts emits universal-for-role facts derived from cb.Role and
// cb.BaseRuntime. Why prose is engine-curated mechanism-level explanation;
// framework-specific code-diff slots are agent-filled via fill-fact-slot.
func classBFacts(cb Codebase) []FactRecord {
	var out []FactRecord

	if cb.Role == RoleAPI || cb.Role == RoleFrontend || cb.Role == RoleMonolith {
		out = append(out, FactRecord{
			Topic:            cb.Hostname + "-bind-and-trust-proxy",
			Kind:             FactKindPorterChange,
			Scope:            cb.Hostname + "/code",
			ChangeKind:       "code-addition",
			Why:              "Default bind to 127.0.0.1 is unreachable from the L7 balancer (which routes to the container's VXLAN IP). Trust the X-Forwarded-* headers so request.ip / request.protocol reflect the real caller.",
			CandidateClass:   "platform-invariant",
			CandidateHeading: "Bind 0.0.0.0 and trust the L7 proxy",
			CandidateSurface: "CODEBASE_IG",
			CitationGuide:    "http-support",
			EngineEmitted:    true,
		})

		if strings.HasPrefix(cb.BaseRuntime, "nodejs") {
			out = append(out, FactRecord{
				Topic:            cb.Hostname + "-sigterm-drain",
				Kind:             FactKindPorterChange,
				Scope:            cb.Hostname + "/code",
				ChangeKind:       "code-addition",
				Why:              "Rolling deploys send SIGTERM to the old container while the new one warms up. Without explicit shutdown handling, in-flight requests fail mid-response.",
				CandidateClass:   "platform-invariant",
				CandidateHeading: "Drain in-flight requests on SIGTERM",
				CandidateSurface: "CODEBASE_IG",
				CitationGuide:    "rolling-deploys",
				EngineEmitted:    true,
			})
		}
	}

	if cb.Role == RoleWorker && strings.HasPrefix(cb.BaseRuntime, "nodejs") {
		// Heading is intentionally agent-filled — system.md §4 keeps
		// framework-specific names on DISCOVER (NestJS application
		// context vs BullMQ Worker vs plain process loop).
		out = append(out, FactRecord{
			Topic:            cb.Hostname + "-no-http-surface",
			Kind:             FactKindPorterChange,
			Scope:            cb.Hostname + "/code",
			ChangeKind:       "code-addition",
			Why:              "A worker has no HTTP surface. Default framework bootstraps that start an Express/Fastify listener fight the platform's empty run.ports — the listener has nothing to bind to and the framework crashes or hangs at boot. Boot the framework's no-HTTP equivalent (e.g. NestJS createApplicationContext, BullMQ Worker, plain process loop) instead.",
			CandidateClass:   "platform-invariant",
			CandidateHeading: "", // agent-filled per §2.7
			CandidateSurface: "CODEBASE_IG",
			EngineEmitted:    true,
		})
	}

	return out
}

// classCUmbrellaFact emits the single own-key-aliases fact that applies
// to every codebase consuming managed services regardless of which
// services they are. The pattern itself is the platform contract; per-
// service alias names are agent-derived from zerops_discover.
func classCUmbrellaFact(cb Codebase) FactRecord {
	return FactRecord{
		Topic:            cb.Hostname + "-own-key-aliases",
		Kind:             FactKindPorterChange,
		Scope:            cb.Hostname + "/zerops.yaml/run.envVariables",
		ChangeKind:       "config-addition",
		Why:              "Cross-service references like ${db_hostname} auto-inject project-wide under platform-side keys. Reading those names directly couples the application to Zerops naming. Declare own-key aliases in zerops.yaml run.envVariables and read those.",
		CandidateClass:   "platform-invariant",
		CandidateHeading: "Read managed-service credentials from own-key aliases",
		CandidateSurface: "CODEBASE_IG",
		CitationGuide:    "env-var-model",
		EngineEmitted:    true,
	}
}

// perServiceShells emits one empty-Why shell per consumed managed
// service. Why is intentionally empty — the agent fills it at codebase-
// content phase by reading the per-service knowledge atom (§7.2 fact-
// shell pattern). Engine pre-seeds shape + citation guide so the agent
// can't forget per-service IG items; atom remains the single source for
// per-service idiom prose.
func perServiceShells(cb Codebase, services []Service) []FactRecord {
	out := make([]FactRecord, 0, len(services))
	for _, svc := range services {
		out = append(out, FactRecord{
			Topic:            cb.Hostname + "-connect-" + svc.Hostname,
			Kind:             FactKindPorterChange,
			Scope:            cb.Hostname + "/code",
			ChangeKind:       "code-addition",
			Why:              "", // agent-filled via fill-fact-slot after consulting zerops_knowledge
			CandidateClass:   "intersection",
			CandidateHeading: "", // agent-filled (framework-specific name)
			CandidateSurface: "CODEBASE_IG",
			CitationGuide:    citationGuideForServiceType(svc.Type),
			EngineEmitted:    true,
		})
	}
	return out
}

// managedServicesConsumedBy returns the managed services this codebase
// consumes. Run-16 minimal version: every managed service in the plan
// is assumed consumed by every codebase. Future extension can filter on
// per-codebase service-consumption metadata.
func managedServicesConsumedBy(plan *Plan, _ Codebase) []Service {
	if plan == nil {
		return nil
	}
	var out []Service
	for _, svc := range plan.Services {
		if svc.Kind == ServiceKindManaged {
			out = append(out, svc)
		}
	}
	return out
}

// citationGuideForServiceType maps a managed-service type (e.g.
// "postgresql@18", "valkey@7", "nats@2.12") to the topic id of its
// per-service knowledge atom in CitationMap. Family-prefix matching;
// version trailers ignored (the connection idiom is family-stable).
//
// Run-16 §7.2 — engine-side curation is JUST this lookup; per-service
// connection prose lives in the atom (single source). Returning a
// topic that maps in CitationMap is enforced by
// TestEmittedFactShell_CitationGuideMatchesCitationMap.
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
