package recipe

import (
	"testing"
)

// Run-17 §6 — Class B universal-for-role + Class C umbrella + per-managed-
// service shells are retracted. EmittedFactsForCodebase always returns nil;
// the deploy-phase sub-agents record the equivalent porter_change facts
// per the worked examples in scaffold/feature decision_recording atoms.

func TestEmittedFactsForCodebase_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		plan *Plan
		cb   Codebase
	}{
		{"nil plan", nil, Codebase{Hostname: "api"}},
		{"empty plan", &Plan{}, Codebase{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
		{"plan with managed services + api codebase", &Plan{
			Services: []Service{
				{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
				{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@7"},
			},
		}, Codebase{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
		{"plan with managed services + worker codebase", &Plan{
			Services: []Service{
				{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
			},
		}, Codebase{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true}},
		{"plan with managed services + frontend codebase", &Plan{
			Services: []Service{
				{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@7"},
			},
		}, Codebase{Hostname: "frontend", Role: RoleFrontend, BaseRuntime: "nodejs@22"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			facts := EmittedFactsForCodebase(tc.plan, tc.cb)
			if len(facts) != 0 {
				t.Errorf("Run-17 retraction: expected empty/nil; got %d facts: %v",
					len(facts), factTopics(facts))
			}
		})
	}
}

// citationGuideForServiceType + CitationMap contract is retained even
// after per-service shell retraction — agent-side citation lookups in
// IG/KB authoring still use the same family↔guide mapping.

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

// Run-16 §5.3 — TierServiceModeDelta + tier_decision pre-emit. Run-17
// preserves these — the tier-decision fact stream is unaffected by
// engine-emit retraction (Class A in run-17 vocabulary).

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

// factTopics renders a facts slice as topic strings for diagnostic output.
// Helper retained from prior shell tests so error messages stay readable.
func factTopics(facts []FactRecord) []string {
	out := make([]string, len(facts))
	for i, f := range facts {
		out[i] = f.Topic
	}
	return out
}
