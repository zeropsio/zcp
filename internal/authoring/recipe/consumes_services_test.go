package recipe

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseConsumedServicesFromYaml — Run-21 R2-3.
//
// Parser walks run.envVariables, extracts `${X}` and `${X_*}`
// references, returns sorted unique hostnames that match a managed
// service in the plan.
func TestParseConsumedServicesFromYaml(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
			{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@8"},
			{Kind: ServiceKindManaged, Hostname: "broker", Type: "nats@2.10"},
			{Kind: ServiceKindManaged, Hostname: "search", Type: "meilisearch@1"},
		},
	}

	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{
			name: "two_services_referenced",
			yaml: `zerops:
  - setup: api
    run:
      envVariables:
        DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}
        REDIS_URL: redis://${cache_hostname}:${cache_port}
`,
			want: []string{"cache", "db"},
		},
		{
			name: "no_envvars",
			yaml: `zerops:
  - setup: api
    run:
      start: node index.js
`,
			want: nil,
		},
		{
			name: "ignores_unknown_token",
			yaml: `zerops:
  - setup: spa
    run:
      envVariables:
        VITE_API_URL: ${api_zeropsSubdomain}
        APP_ENV: production
`,
			// `api` is not in plan.Services (it's a runtime codebase).
			// Only managed-service hits count.
			want: nil,
		},
		{
			name: "deduplicates",
			yaml: `zerops:
  - setup: api
    run:
      envVariables:
        DB1: ${db_hostname}
        DB2: ${db_port}
        DB3: ${db_user}
`,
			want: []string{"db"},
		},
		{
			name: "bare_reference_no_underscore",
			yaml: `zerops:
  - setup: api
    run:
      envVariables:
        SEARCH: ${search}
`,
			want: []string{"search"},
		},
		{
			name: "unparseable_yaml_returns_nil",
			yaml: ":::not yaml:::",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseConsumedServicesFromYaml(tt.yaml, plan)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseConsumedServicesFromYaml = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRecipeContext_FiltersServicesByCodebaseConsumption — Run-21 R2-3.
//
// SPA codebase with empty (non-nil) ConsumesServices gets the Managed
// services block omitted from its dispatched prompt context.
func TestRecipeContext_FiltersServicesByCodebaseConsumption(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Slug:      "showcase",
		Framework: "node-svelte",
		Tier:      "showcase",
		Codebases: []Codebase{
			{
				Hostname:         "spa",
				Role:             RoleFrontend,
				BaseRuntime:      "nodejs@22",
				SourceRoot:       "/var/www/spadev",
				ConsumesServices: []string{}, // explicit empty: SPA consumes nothing managed
			},
			{
				Hostname:         "api",
				Role:             RoleAPI,
				BaseRuntime:      "nodejs@22",
				SourceRoot:       "/var/www/apidev",
				ConsumesServices: []string{"db", "cache"},
			},
		},
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
			{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@8"},
			{Kind: ServiceKindManaged, Hostname: "broker", Type: "nats@2.10"},
		},
	}

	// SPA brief — Managed services block should be absent.
	in := RecipeInput{BriefKind: "scaffold", Codebase: "spa"}
	prompt, err := buildSubagentPrompt(plan, in)
	if err != nil {
		t.Fatalf("buildSubagentPrompt spa: %v", err)
	}
	if strings.Contains(prompt, "### Managed services") {
		t.Errorf("spa prompt should omit Managed services block when ConsumesServices is empty;\n%s", prompt)
	}

	// API brief — Managed services block lists db + cache, excludes broker.
	in.Codebase = "api"
	prompt, err = buildSubagentPrompt(plan, in)
	if err != nil {
		t.Fatalf("buildSubagentPrompt api: %v", err)
	}
	if !strings.Contains(prompt, "### Managed services") {
		t.Errorf("api prompt missing Managed services block")
	}
	if !strings.Contains(prompt, "`db`") || !strings.Contains(prompt, "`cache`") {
		t.Errorf("api prompt missing db/cache listings")
	}
	if strings.Contains(prompt, "`broker`") {
		t.Errorf("api prompt leaks broker (not in ConsumesServices)")
	}
}

// TestCrossCodebaseManagedServiceFacts_PropagatesByEnvKey — Run-26 F-31.
//
// Closes the run-26 apidev-NATS factuality drift. Worker-scaffold
// recorded `worker-nats-connection-string` (Scope=workerdev/...) with
// FailureMode `${broker_connectionString}` → TypeError: Invalid URL`;
// apidev codebase consumes broker via `${broker_hostname}` etc. The
// engine must surface the worker fact in apidev's codebase-content
// brief so the apidev sub-agent doesn't fall back to atom-corpus
// generic NATS guidance and endorse `${broker_connectionString}` as a
// viable shape.
//
// Match predicate: fact text fields contain `${<host>_*}` or `${<host>}`
// for any host in cb.ConsumesServices, AND fact.Scope is not this
// codebase. Scope-prefix dedup: facts already in cbFacts are not
// re-propagated.
func TestCrossCodebaseManagedServiceFacts_PropagatesByEnvKey(t *testing.T) {
	t.Parallel()

	apidev := Codebase{
		Hostname:         "apidev",
		ConsumesServices: []string{"broker", "db"},
	}

	facts := []FactRecord{
		// Worker-scoped fact about NATS — should propagate to apidev.
		{
			Topic:      "worker-nats-connection-string",
			Scope:      "workerdev/initCommands",
			Kind:       FactKindPorterChange,
			Why:        "${broker_connectionString} crashes the nats client at boot with TypeError: Invalid URL because the auto-generated password contains `-` and breaks bracketed-host parsing inside the client's hostPort wrapping; switched to four-env Pattern A.",
			ChangeKind: "code-change",
			FixApplied: "Switched to NATS_HOST/NATS_PORT/NATS_USER/NATS_PASS bound to ${broker_hostname}/${broker_port}/${broker_user}/${broker_password}.",
		},
		// Worker-scoped fact about Postgres — should propagate (apidev consumes db).
		{
			Topic:     "db-host-shadow",
			Scope:     "workerdev/zerops.yaml",
			Kind:      FactKindFieldRationale,
			FieldPath: "run.envVariables.DB_HOST",
			Why:       "Re-aliased ${db_hostname} under DB_HOST own-key to avoid the same-key shadow trap.",
		},
		// Worker-scoped fact mentioning a service apidev does NOT consume — skip.
		{
			Topic: "search-master-key",
			Scope: "workerdev/initCommands",
			Kind:  FactKindPorterChange,
			Why:   "Wired ${search_masterKey} as SEARCH_API_KEY for Meilisearch indexing.",
		},
		// Apidev-scoped fact — already in cbFacts, do NOT duplicate.
		{
			Topic: "apidev-startup-trace",
			Scope: "apidev/initCommands",
			Kind:  FactKindPorterChange,
			Why:   "Probed ${broker_hostname} on boot to verify connectivity before subscribing.",
		},
		// Fact with no env-key reference — skip even if scope is sister codebase.
		{
			Topic:     "appdev-vite-port",
			Scope:     "appdev/zerops.yaml",
			Kind:      FactKindFieldRationale,
			FieldPath: "run.ports[0].port",
			Why:       "Vite dev server defaults to 5173.",
		},
	}

	got := CrossCodebaseManagedServiceFacts(facts, apidev)
	if len(got) != 2 {
		t.Fatalf("expected 2 cross-codebase facts (broker + db), got %d:\n%+v", len(got), got)
	}
	wantTopics := map[string]bool{"worker-nats-connection-string": true, "db-host-shadow": true}
	for _, f := range got {
		if !wantTopics[f.Topic] {
			t.Errorf("unexpected topic propagated: %s", f.Topic)
		}
	}
}

// TestCrossCodebaseManagedServiceFacts_TierDecisionAndContractKinds — Run-26 F-31.
//
// Field-set coverage extension after Opus review: tier_decision facts
// often carry `${<host>_*}` references in ChosenValue / TierContext,
// and contract facts carry env refs in Subject / Purpose. Without these
// fields in the scan set, sister-codebase tier_decision facts that
// recorded the env-key shape baked at a given tier would not propagate.
func TestCrossCodebaseManagedServiceFacts_TierDecisionAndContractKinds(t *testing.T) {
	t.Parallel()

	apidev := Codebase{
		Hostname:         "apidev",
		ConsumesServices: []string{"broker", "db"},
	}

	facts := []FactRecord{
		// tier_decision recorded by env-content for the worker; ChosenValue
		// carries the env-key reference. apidev consumes broker, so propagate.
		{
			Topic:       "worker-nats-tier3-binding",
			Scope:       "env-content/workerstage",
			Kind:        FactKindTierDecision,
			Tier:        3,
			Service:     "broker",
			FieldPath:   "run.envVariables.NATS_HOST",
			ChosenValue: "${broker_hostname}",
			TierContext: "Bound NATS_HOST under own-key alias from ${broker_hostname} to avoid the same-key shadow trap.",
		},
		// contract fact recorded by feature pass naming the broker subject.
		{
			Topic:       "jobs-subject-contract",
			Scope:       "feature/jobs",
			Kind:        FactKindContract,
			Subject:     "jobs.echo via ${broker_connectionString}",
			Publishers:  []string{"apidev"},
			Subscribers: []string{"workerdev"},
			Purpose:     "Fire-and-forget echo dispatch; audit shows ${broker_connectionString} crashes the nats client at boot — Pattern A is the supported wiring.",
		},
		// tier_decision for an unrelated service — should NOT propagate.
		{
			Topic:       "search-tier5-mode",
			Scope:       "env-content/search",
			Kind:        FactKindTierDecision,
			Tier:        5,
			Service:     "search",
			ChosenValue: "${search_masterKey}",
			TierContext: "Meilisearch stays NON_HA at every tier; only minRam scales.",
		},
	}

	got := CrossCodebaseManagedServiceFacts(facts, apidev)
	if len(got) != 2 {
		t.Fatalf("expected 2 propagated facts (tier_decision broker + contract jobs), got %d:\n%+v", len(got), got)
	}
	wantTopics := map[string]bool{"worker-nats-tier3-binding": true, "jobs-subject-contract": true}
	for _, f := range got {
		if !wantTopics[f.Topic] {
			t.Errorf("unexpected topic propagated: %s", f.Topic)
		}
	}
}

// TestBuildCodebaseContentBrief_CrossCodebaseFactsCap — Run-26 F-31.
//
// Cap-pinning regression with a POPULATED fact set on the worker
// variant (highest brief load). The legacy cap-pinning test
// (TestCodebaseContentBrief_EmbedsParentMD_WhenParentAbsent_ShowcaseSlug)
// passes nil facts; this test exercises the populated-cross-facts case
// so the cap stays sized for realistic propagation.
func TestBuildCodebaseContentBrief_CrossCodebaseFactsCap(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Slug:      "nestjs-showcase",
		Framework: "nestjs",
		Tier:      "showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22", SourceRoot: "/var/www/apidev",
				ConsumesServices: []string{"db", "cache", "broker", "storage", "search"}},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", SourceRoot: "/var/www/workerdev", IsWorker: true,
				ConsumesServices: []string{"db", "cache", "broker", "storage", "search"}},
		},
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", SupportsHA: true},
			{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@7.2"},
			{Kind: ServiceKindManaged, Hostname: "broker", Type: "nats@2.12"},
			{Kind: ServiceKindManaged, Hostname: "storage", Type: "object-storage"},
			{Kind: ServiceKindManaged, Hostname: "search", Type: "meilisearch@1.20"},
		},
	}

	// Worst-case fact set: api scope records 5 connection-shape facts
	// (one per managed service), so worker's brief propagates all 5.
	apiHost := "api"
	mkFact := func(topic, why, fix string) FactRecord {
		return FactRecord{
			Topic:      topic,
			Scope:      apiHost + "/initCommands",
			Kind:       FactKindPorterChange,
			ChangeKind: "code-change",
			Why:        why,
			FixApplied: fix,
		}
	}
	facts := []FactRecord{
		mkFact("api-db-pool", "TypeORM pool exhausts under burst load when ${db_hostname} routes through the L7 balancer; bypassed via direct ${db_port}.", "Lowered max pool to 8 and added retry on ECONNRESET."),
		mkFact("api-cache-noauth", "${cache_user}/${cache_password} resolve to literal tokens because Valkey runs unauthenticated; ioredis errored with NOAUTH.", "Drop user/pass; wire only ${cache_hostname} + ${cache_port}."),
		mkFact("api-nats-pattern-a", "${broker_connectionString} crashes the nats client at boot with TypeError: Invalid URL because the auto-generated password contains '-'.", "Bound NATS_HOST/PORT/USER/PASS from ${broker_hostname}/${broker_port}/${broker_user}/${broker_password}."),
		mkFact("api-storage-pathstyle", "AWS SDK virtual-hosted style 403s on MinIO behind ${storage_apiUrl}; switched to forcePathStyle.", "forcePathStyle: true with endpoint=${storage_apiUrl} (already https://); region literal us-east-1."),
		mkFact("api-search-masterkey", "${search_masterKey} is the admin key; never ship to browser.", "Mint a defaultSearchKey via Meilisearch admin API for the SPA path."),
	}

	worker := plan.Codebases[1]
	brief, err := BuildCodebaseContentBrief(plan, worker, nil, facts)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	// Confirm propagation actually fired for the worst case.
	if !strings.Contains(brief.Body, "Cross-codebase managed-service facts") {
		t.Errorf("worker brief should carry cross-codebase facts block with 5 api-scoped facts to propagate")
	}
	for _, topic := range []string{"api-db-pool", "api-cache-noauth", "api-nats-pattern-a", "api-storage-pathstyle", "api-search-masterkey"} {
		if !strings.Contains(brief.Body, topic) {
			t.Errorf("worker brief missing propagated fact: %s", topic)
		}
	}
	// Run-31 Fix #1 closure — the legacy single-file
	// CodebaseContentBriefCap byte-cap is gone (multi-file architecture
	// replaced it). Per-part invariants are pinned by
	// `TestBuildCodebaseContentBrief_MultiFile_RealSlug_PartsUnderCap`,
	// which exercises a 142-fact run-shaped corpus; the propagation
	// path here is covered by the topic-substring assertions above and
	// the multi-file per-part token-ceiling test.
}

// TestCrossCodebaseManagedServiceFacts_NilConsumesServices — Run-26 F-31.
//
// Sim/back-compat path: codebase that wasn't analyzed (nil
// ConsumesServices) returns empty cross-codebase facts. Conservative
// vs the fall-back-to-everything pattern used for atoms — propagating
// every cross-codebase fact would leak unrelated infrastructure
// findings, the failure mode worse than under-propagation.
func TestCrossCodebaseManagedServiceFacts_NilConsumesServices(t *testing.T) {
	t.Parallel()

	cb := Codebase{Hostname: "apidev", ConsumesServices: nil}
	facts := []FactRecord{
		{
			Topic: "worker-nats-connection-string",
			Scope: "workerdev/initCommands",
			Kind:  FactKindPorterChange,
			Why:   "${broker_connectionString} crashes.",
		},
	}
	if got := CrossCodebaseManagedServiceFacts(facts, cb); got != nil {
		t.Errorf("nil ConsumesServices should return nil; got %d facts", len(got))
	}
}

// TestCrossCodebaseManagedServiceFacts_EmptyConsumesServices — Run-26 F-31.
//
// Codebase analyzed-but-consumes-nothing (empty non-nil) returns no
// cross-codebase facts. SPA-only frontend that talks to the API via
// `${api_zeropsSubdomain}` (a runtime ref, not a managed-service ref)
// has no managed-service shape concerns to inherit.
func TestCrossCodebaseManagedServiceFacts_EmptyConsumesServices(t *testing.T) {
	t.Parallel()

	cb := Codebase{Hostname: "appdev", ConsumesServices: []string{}}
	facts := []FactRecord{
		{
			Topic: "worker-nats-connection-string",
			Scope: "workerdev/initCommands",
			Kind:  FactKindPorterChange,
			Why:   "${broker_connectionString} crashes.",
		},
	}
	if got := CrossCodebaseManagedServiceFacts(facts, cb); len(got) != 0 {
		t.Errorf("empty ConsumesServices should return no facts; got %d", len(got))
	}
}

// TestBuildCodebaseContentBrief_PropagatesCrossCodebaseFacts — Run-26 F-31.
//
// Integration test on the brief composer: when a worker codebase
// recorded a fact whose text references `${broker_*}` and the
// codebase-content sub-agent dispatches for apidev (which consumes
// broker), the dispatched brief carries a "Cross-codebase managed-
// service facts" block with the worker fact in it.
func TestBuildCodebaseContentBrief_PropagatesCrossCodebaseFacts(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Slug: "nestjs-showcase",
		Codebases: []Codebase{
			{Hostname: "apidev", Role: RoleAPI, BaseRuntime: "nodejs@22",
				ConsumesServices: []string{"broker", "db"}},
			{Hostname: "workerdev", Role: RoleWorker, BaseRuntime: "nodejs@22",
				ConsumesServices: []string{"broker", "db"}},
		},
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "broker", Type: "nats@2.10"},
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
		},
	}
	apidev := plan.Codebases[0]
	facts := []FactRecord{
		{
			Topic:      "worker-nats-connection-string",
			Scope:      "workerdev/initCommands",
			Kind:       FactKindPorterChange,
			ChangeKind: "code-change",
			Why:        "${broker_connectionString} crashes the nats client at boot with TypeError: Invalid URL.",
			FixApplied: "Pattern A — bound NATS_HOST etc. to ${broker_hostname}/${broker_port}/${broker_user}/${broker_password}.",
		},
	}

	brief, err := BuildCodebaseContentBrief(plan, apidev, nil, facts)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "Cross-codebase managed-service facts") {
		t.Errorf("brief should carry the cross-codebase facts heading; brief:\n%s", brief.Body)
	}
	if !strings.Contains(brief.Body, "worker-nats-connection-string") {
		t.Errorf("brief should propagate worker NATS fact topic; brief:\n%s", brief.Body)
	}
	if !strings.Contains(brief.Body, "${broker_connectionString}") {
		t.Errorf("brief should carry the verbatim env-key reference from FixApplied/Why; brief:\n%s", brief.Body)
	}
}

// TestFactMentionsAnyConsumedService_UnbracedHostnamePrefix_Matches —
// run-28 fix #1. Worker scaffold's NATS Pattern-A finding cites
// `broker_connectionString` (alias name) and `broker_hostname` etc.
// without `${...}` wrapping in fact text. The predicate must match
// these unbraced hostname-prefix forms at word boundaries so the
// fact propagates to consumer codebases.
func TestFactMentionsAnyConsumedService_UnbracedHostnamePrefix_Matches(t *testing.T) {
	t.Parallel()
	consumed := map[string]bool{"broker": true}
	cases := []struct {
		name string
		f    FactRecord
	}{
		{
			name: "broker_connectionString_in_why",
			f: FactRecord{
				Why: "broker_connectionString crashes nats client at boot.",
			},
		},
		{
			name: "broker_hostname_in_fixApplied",
			f: FactRecord{
				FixApplied: "Switched to broker_hostname + broker_port wiring.",
			},
		},
		{
			name: "broker_camelCase_alias",
			f: FactRecord{
				Why: "Read broker_connectionString as a single env.",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !factMentionsAnyConsumedService(tc.f, consumed) {
				t.Errorf("expected match on unbraced %q", tc.name)
			}
		})
	}
}

// TestFactMentionsAnyConsumedService_AliasOnly_DoesNotMatch — run-28
// fix #1. Bare alias names without the host prefix (e.g. just
// `connectionString`) must NOT match — too ambiguous, no host anchor.
func TestFactMentionsAnyConsumedService_AliasOnly_DoesNotMatch(t *testing.T) {
	t.Parallel()
	consumed := map[string]bool{"broker": true}
	f := FactRecord{
		Why: "Use connectionString rather than the four split env vars.",
	}
	if factMentionsAnyConsumedService(f, consumed) {
		t.Errorf("alias-only prose should not match without host anchor")
	}
}

// TestFactMentionsAnyConsumedService_BareHostnameWord_DoesNotMatch —
// run-28 fix #1. The bare hostname word "broker" alone must NOT match
// — would over-propagate facts that narrate tier decisions and use
// the hostname conversationally.
func TestFactMentionsAnyConsumedService_BareHostnameWord_DoesNotMatch(t *testing.T) {
	t.Parallel()
	consumed := map[string]bool{"broker": true}
	f := FactRecord{
		Why: "The broker is a NATS server; choose subjects carefully.",
	}
	if factMentionsAnyConsumedService(f, consumed) {
		t.Errorf("bare hostname word should not match without `_<suffix>` anchor")
	}
}

// TestFactMentionsAnyConsumedService_HostnameSubstring_DoesNotMatch —
// run-28 fix #1. Word boundary must be respected: `embroker_hostname`
// (where `embroker` is a different host that happens to contain
// `broker` as a substring) must NOT match consumed=[broker].
func TestFactMentionsAnyConsumedService_HostnameSubstring_DoesNotMatch(t *testing.T) {
	t.Parallel()
	consumed := map[string]bool{"broker": true}
	f := FactRecord{
		Why: "Wired embroker_hostname for the embedded queue service.",
	}
	if factMentionsAnyConsumedService(f, consumed) {
		t.Errorf("hostname substring inside a longer token should not match (word boundary regression)")
	}
}

// TestScaffoldDecisionRecordingSlim_EnvTokenSection_Present — run-28
// fix #1b. Pins that the scaffold decision-recording atom teaches
// porter_change facts about managed-service wiring to cite the
// `${<host>_*}` env-token form so cross-codebase propagation matches.
func TestScaffoldDecisionRecordingSlim_EnvTokenSection_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/scaffold/decision_recording_slim.md")
	if err != nil {
		t.Fatalf("read decision_recording_slim.md: %v", err)
	}
	if !strings.Contains(body, "Env-token references in fact text") {
		t.Errorf("atom missing env-token-references section heading")
	}
	if !strings.Contains(body, "CrossCodebaseManagedServiceFacts") {
		t.Errorf("atom missing reference to cross-codebase propagation predicate")
	}
}

// TestScaffoldDecisionRecordingSlim_EnvTokenWorkedExample_GoodForm —
// run-28 fix #1b. The atom's GOOD example must contain at least one
// `${broker_*}` env-token reference so the porter sees the shape that
// makes propagation match.
func TestScaffoldDecisionRecordingSlim_EnvTokenWorkedExample_GoodForm(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/scaffold/decision_recording_slim.md")
	if err != nil {
		t.Fatalf("read decision_recording_slim.md: %v", err)
	}
	for _, want := range []string{
		"${broker_hostname}",
		"${broker_connectionString}",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("worked example missing env-token reference %q", want)
		}
	}
}

// TestRecipeContext_NilConsumesServices_FallsBackToAll — back-compat.
//
// Codebase with nil (not zero) ConsumesServices preserves pre-R2-3
// behavior of dumping the full Services block — applies to existing
// plans loaded before the field existed and to sim paths that skip
// the parse step.
func TestRecipeContext_NilConsumesServices_FallsBackToAll(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Slug: "x",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22", SourceRoot: "/var/www/apidev"},
		},
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
			{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@8"},
		},
	}
	in := RecipeInput{BriefKind: "scaffold", Codebase: "api"}
	prompt, err := buildSubagentPrompt(plan, in)
	if err != nil {
		t.Fatalf("buildSubagentPrompt: %v", err)
	}
	if !strings.Contains(prompt, "`db`") || !strings.Contains(prompt, "`cache`") {
		t.Errorf("nil-ConsumesServices fallback should list all managed services; prompt:\n%s", prompt)
	}
}
