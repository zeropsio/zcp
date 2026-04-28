package recipe

import (
	"strings"
	"testing"
)

// Run-16 §7.1 — Class B universal-for-role facts. Per-role × runtime
// matrix; framework-specific names left to the agent.

func TestEmittedFacts_ClassB_API_Nodejs_HasBindAndSigterm(t *testing.T) {
	t.Parallel()
	plan := &Plan{}
	cb := Codebase{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}
	facts := EmittedFactsForCodebase(plan, cb)

	hasTopic(t, facts, "api-bind-and-trust-proxy")
	hasTopic(t, facts, "api-sigterm-drain")
	for _, f := range facts {
		if f.Topic == "api-bind-and-trust-proxy" && !strings.Contains(f.Why, "L7 balancer") {
			t.Errorf("bind-and-trust Why should name the L7 balancer; got: %s", f.Why)
		}
	}
}

func TestEmittedFacts_ClassB_API_PHPNginx_NoSigterm(t *testing.T) {
	t.Parallel()
	plan := &Plan{}
	cb := Codebase{Hostname: "api", Role: RoleAPI, BaseRuntime: "php-nginx@8.3"}
	facts := EmittedFactsForCodebase(plan, cb)

	hasTopic(t, facts, "api-bind-and-trust-proxy")
	for _, f := range facts {
		if f.Topic == "api-sigterm-drain" {
			t.Errorf("php-nginx role should NOT emit sigterm-drain (PHP-FPM handles it): %+v", f)
		}
	}
}

func TestEmittedFacts_ClassB_Worker_Nodejs_HasNoHTTPSurface_AgentFilledHeading(t *testing.T) {
	t.Parallel()
	plan := &Plan{}
	cb := Codebase{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22"}
	facts := EmittedFactsForCodebase(plan, cb)

	hasTopic(t, facts, "worker-no-http-surface")
	for _, f := range facts {
		if f.Topic == "worker-no-http-surface" {
			if f.CandidateHeading != "" {
				t.Errorf("worker no-http heading should be agent-filled (empty); got %q", f.CandidateHeading)
			}
			if f.Why == "" {
				t.Error("worker no-http Why must be engine-emitted (mechanism-level)")
			}
			if !f.EngineEmitted {
				t.Error("worker no-http fact should be EngineEmitted=true")
			}
		}
	}

	// Worker should NOT emit the bind/trust/sigterm facts — those are
	// HTTP-serving role obligations.
	for _, f := range facts {
		if strings.HasSuffix(f.Topic, "-bind-and-trust-proxy") || strings.HasSuffix(f.Topic, "-sigterm-drain") {
			t.Errorf("worker should not emit %q", f.Topic)
		}
	}
}

func TestEmittedFacts_ClassB_Frontend_Monolith_AlsoEmitBindAndSigterm(t *testing.T) {
	t.Parallel()
	plan := &Plan{}
	for _, role := range []Role{RoleFrontend, RoleMonolith} {
		cb := Codebase{Hostname: "h", Role: role, BaseRuntime: "nodejs@22"}
		facts := EmittedFactsForCodebase(plan, cb)
		hasTopic(t, facts, "h-bind-and-trust-proxy")
		hasTopic(t, facts, "h-sigterm-drain")
	}
}

// Run-16 §7.1 — Class C umbrella + per-managed-service shells.

func TestEmittedFacts_ClassC_Umbrella_OnlyWhenServicesConsumed(t *testing.T) {
	t.Parallel()

	cb := Codebase{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}

	// No managed services → no umbrella.
	emptyPlan := &Plan{}
	for _, f := range EmittedFactsForCodebase(emptyPlan, cb) {
		if strings.HasSuffix(f.Topic, "-own-key-aliases") {
			t.Errorf("no managed services → no own-key-aliases fact; got %+v", f)
		}
	}

	// With managed services → umbrella present.
	plan := &Plan{Services: []Service{
		{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
	}}
	facts := EmittedFactsForCodebase(plan, cb)
	hasTopic(t, facts, "api-own-key-aliases")
}

func TestEmittedFacts_PerManagedService_Shells_EmptyWhy(t *testing.T) {
	t.Parallel()
	plan := &Plan{Services: []Service{
		{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
		{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@7"},
		{Kind: ServiceKindManaged, Hostname: "search", Type: "meilisearch@1.10"},
	}}
	cb := Codebase{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}
	facts := EmittedFactsForCodebase(plan, cb)

	for _, hostname := range []string{"db", "cache", "search"} {
		topic := "api-connect-" + hostname
		var fact *FactRecord
		for i := range facts {
			if facts[i].Topic == topic {
				fact = &facts[i]
				break
			}
		}
		if fact == nil {
			t.Errorf("missing per-service shell for %s: topic=%q", hostname, topic)
			continue
		}
		if fact.Why != "" {
			t.Errorf("per-service shell %s must have empty Why (agent fills via fill-fact-slot); got %q", hostname, fact.Why)
		}
		if !fact.EngineEmitted {
			t.Errorf("per-service shell %s should be EngineEmitted=true", hostname)
		}
		if fact.CitationGuide == "" {
			t.Errorf("per-service shell %s missing CitationGuide", hostname)
		}
	}
}

func TestEmittedFactShells_PerConsumedManagedService(t *testing.T) {
	t.Parallel()
	plan := &Plan{Services: []Service{
		{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
		{Kind: ServiceKindStorage, Hostname: "storage", Type: "object-storage"}, // not managed kind
		{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@7"},
	}}
	cb := Codebase{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}
	facts := EmittedFactsForCodebase(plan, cb)

	// One shell per managed service consumed (storage is not managed kind).
	shellCount := 0
	for _, f := range facts {
		if strings.HasPrefix(f.Topic, "api-connect-") {
			shellCount++
		}
	}
	if shellCount != 2 {
		t.Errorf("expected 2 per-service shells (db, cache); got %d", shellCount)
	}
}

func TestEmittedFactShell_CitationGuideMatchesCitationMap(t *testing.T) {
	t.Parallel()
	for _, family := range []string{
		"postgresql", "valkey", "redis", "nats", "rabbitmq", "kafka",
		"meilisearch", "elasticsearch", "typesense", "qdrant", "clickhouse",
		"mariadb", "keydb",
	} {
		guide := citationGuideForServiceType(family + "@1")
		if guide == "" {
			t.Errorf("citationGuideForServiceType(%s) returned empty — every supported family must have a topic", family)
			continue
		}
		if _, ok := CitationMap[guide]; !ok {
			t.Errorf("citationGuideForServiceType(%s) returned %q which is not in CitationMap", family, guide)
		}
	}
}

func TestCitationGuideForServiceType_UnknownFamily_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := citationGuideForServiceType("madeup-service@1"); got != "" {
		t.Errorf("unknown family should return empty; got %q", got)
	}
}

// Run-16 §5.3 — TierServiceModeDelta + tier_decision pre-emit.

func TestTierServiceModeDelta_HASupporters_FlipFromTier4ToTier5(t *testing.T) {
	t.Parallel()
	plan := &Plan{Services: []Service{
		{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", SupportsHA: true},
	}}
	tiers := Tiers()
	deltas := TierServiceModeDelta(tiers[4], tiers[5], plan)
	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta (postgres NON_HA→HA at tier 4→5); got %d", len(deltas))
	}
	if deltas[0].From != "NON_HA" || deltas[0].To != "HA" {
		t.Errorf("postgres delta: %+v", deltas[0])
	}
}

func TestTierServiceModeDelta_NonHASupporter_NoFlip(t *testing.T) {
	t.Parallel()
	// meilisearch family doesn't support HA on Zerops — stays NON_HA at tier 5.
	plan := &Plan{Services: []Service{
		{Kind: ServiceKindManaged, Hostname: "search", Type: "meilisearch@1.10"},
	}}
	tiers := Tiers()
	deltas := TierServiceModeDelta(tiers[4], tiers[5], plan)
	if len(deltas) != 0 {
		t.Errorf("meilisearch should not flip mode at tier 4→5 (no HA support); got %+v", deltas)
	}
}

func TestTierServiceModeDelta_MixedFamilies_OnlyHASupporters(t *testing.T) {
	t.Parallel()
	plan := &Plan{Services: []Service{
		{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", SupportsHA: true},
		{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@7", SupportsHA: true},
		{Kind: ServiceKindManaged, Hostname: "search", Type: "meilisearch@1.10"},
	}}
	tiers := Tiers()
	deltas := TierServiceModeDelta(tiers[4], tiers[5], plan)
	if len(deltas) != 2 {
		t.Errorf("expected 2 deltas (db, cache); got %d", len(deltas))
	}
}

func TestTierServiceModeDelta_SkipsRuntimeServices(t *testing.T) {
	t.Parallel()
	plan := &Plan{Services: []Service{
		{Kind: ServiceKindStorage, Hostname: "storage", Type: "object-storage"},
		{Kind: ServiceKindUtility, Hostname: "mailpit", Type: "mailpit"},
	}}
	tiers := Tiers()
	deltas := TierServiceModeDelta(tiers[4], tiers[5], plan)
	if len(deltas) != 0 {
		t.Errorf("non-managed services should not contribute deltas; got %+v", deltas)
	}
}

func TestEmittedTierDecisionFacts_CoversCrossTierDeltas(t *testing.T) {
	t.Parallel()
	plan := &Plan{Services: []Service{
		{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", SupportsHA: true},
	}}
	facts := EmittedTierDecisionFacts(plan)

	// Each fact must validate as tier_decision.
	for _, f := range facts {
		if err := f.Validate(); err != nil {
			t.Errorf("emitted tier_decision fact failed Validate: %v %+v", err, f)
		}
		if f.Kind != FactKindTierDecision {
			t.Errorf("expected Kind=tier_decision; got %q", f.Kind)
		}
		if f.CandidateSurface != "ENV_IMPORT_COMMENTS" {
			t.Errorf("tier_decision should route to ENV_IMPORT_COMMENTS; got %q", f.CandidateSurface)
		}
	}

	// Per-service postgres flip at tier 4→5 must be present.
	wantTopic := "tier-5-db-mode"
	found := false
	for _, f := range facts {
		if f.Topic == wantTopic {
			found = true
			if f.Service != "db" || f.ChosenValue != "HA" {
				t.Errorf("tier-5-db-mode wrong shape: %+v", f)
			}
			break
		}
	}
	if !found {
		t.Errorf("tier-5-db-mode fact missing — engine pre-emit didn't cover the postgres mode flip")
	}
}

func TestEmittedTierDecisionFacts_NilPlan_Empty(t *testing.T) {
	t.Parallel()
	if facts := EmittedTierDecisionFacts(nil); facts != nil {
		t.Errorf("nil plan should produce no tier_decision facts; got %+v", facts)
	}
}

// hasTopic asserts a facts slice contains a record with the given Topic.
func hasTopic(t *testing.T, facts []FactRecord, topic string) {
	t.Helper()
	for _, f := range facts {
		if f.Topic == topic {
			return
		}
	}
	t.Errorf("facts missing topic %q; got %v", topic, factTopics(facts))
}

func factTopics(facts []FactRecord) []string {
	out := make([]string, len(facts))
	for i, f := range facts {
		out[i] = f.Topic
	}
	return out
}
