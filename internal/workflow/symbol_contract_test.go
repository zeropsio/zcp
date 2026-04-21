package workflow

import (
	"encoding/json"
	"testing"
)

// TestBuildSymbolContract_NilAndEmpty — derivation is total; nil plan and
// plan-with-zero-targets both yield a valid contract with the seeded
// FixRecurrenceRules populated.
func TestBuildSymbolContract_NilAndEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan *RecipePlan
	}{
		{"nil plan", nil},
		{"empty plan", &RecipePlan{}},
		{"no targets", &RecipePlan{Tier: RecipeTierMinimal}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := BuildSymbolContract(tt.plan)
			if len(c.FixRecurrenceRules) != 12 {
				t.Errorf("want 12 seeded fix-recurrence rules, got %d", len(c.FixRecurrenceRules))
			}
			if c.EnvVarsByKind == nil || c.HTTPRoutes == nil ||
				c.NATSSubjects == nil || c.NATSQueues == nil {
				t.Error("maps must be non-nil (JSON marshaling consistency)")
			}
		})
	}
}

// TestBuildSymbolContract_SingleCodebaseMinimal — one runtime + one database.
// Hostnames pair derives from the runtime hostname; DB env vars use
// uppercase-hostname prefix per platform convention.
func TestBuildSymbolContract_SingleCodebaseMinimal(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierMinimal,
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "nodejs@22", Role: "app"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
	c := BuildSymbolContract(plan)

	db, ok := c.EnvVarsByKind["db"]
	if !ok {
		t.Fatalf("db kind missing from EnvVarsByKind: %+v", c.EnvVarsByKind)
	}
	wants := map[string]string{
		"host": "DB_HOST",
		"port": "DB_PORT",
		"user": "DB_USER",
		"pass": "DB_PASSWORD",
		"name": "DB_DBNAME",
	}
	for role, want := range wants {
		if db[role] != want {
			t.Errorf("db[%q]=%q, want %q", role, db[role], want)
		}
	}

	// Runtime hostname pair.
	var appEntry *HostnameEntry
	for i := range c.Hostnames {
		if c.Hostnames[i].Role == "app" && c.Hostnames[i].Dev == "appdev" {
			appEntry = &c.Hostnames[i]
			break
		}
	}
	if appEntry == nil {
		t.Fatalf("runtime hostname pair missing: %+v", c.Hostnames)
	}
	if appEntry.Stage != "appstage" {
		t.Errorf("stage hostname: got %q, want appstage", appEntry.Stage)
	}
}

// TestBuildSymbolContract_DualRuntimeMinimal — two runtimes (apidev+appdev).
// Both get hostname pairs; the DB still has exactly one managed entry.
func TestBuildSymbolContract_DualRuntimeMinimal(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierMinimal,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "app", Type: "nodejs@22", Role: "app"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
	c := BuildSymbolContract(plan)

	roles := map[string]bool{}
	for _, h := range c.Hostnames {
		roles[h.Role] = true
	}
	for _, want := range []string{"api", "app", "db"} {
		if !roles[want] {
			t.Errorf("role %q missing from hostnames: %+v", want, c.Hostnames)
		}
	}
	// Hostnames are sorted deterministically: re-deriving yields the same order.
	c2 := BuildSymbolContract(plan)
	if len(c.Hostnames) != len(c2.Hostnames) {
		t.Fatalf("length mismatch between idempotent derivations")
	}
	for i := range c.Hostnames {
		if c.Hostnames[i] != c2.Hostnames[i] {
			t.Errorf("hostnames[%d] not idempotent: %+v vs %+v", i, c.Hostnames[i], c2.Hostnames[i])
		}
	}
}

// TestBuildSymbolContract_ShowcaseWithWorker — showcase has api + app + worker
// (separate codebase) + all 6 managed kinds. Separate-codebase worker gets
// its own hostname pair; shared-codebase worker would not.
func TestBuildSymbolContract_ShowcaseWithWorker(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "app", Type: "nodejs@22", Role: "app"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, Role: "worker"},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "cache", Type: "valkey@7.2"},
			{Hostname: "queue", Type: "nats@2"},
			{Hostname: "storage", Type: "object-storage"},
			{Hostname: "search", Type: "meilisearch@1"},
		},
	}
	c := BuildSymbolContract(plan)

	for _, kind := range []string{"db", "cache", "queue", "storage", "search"} {
		if _, ok := c.EnvVarsByKind[kind]; !ok {
			t.Errorf("kind %q missing from EnvVarsByKind: keys=%v", kind, keysOf(c.EnvVarsByKind))
		}
	}
	// NATS seeded pair populated when queue present.
	if c.NATSSubjects["job_dispatch"] == "" {
		t.Errorf("NATSSubjects missing seeded job_dispatch: %+v", c.NATSSubjects)
	}
	if c.NATSQueues["workers"] == "" {
		t.Errorf("NATSQueues missing seeded workers entry: %+v", c.NATSQueues)
	}
	// Separate-codebase worker appears in hostnames.
	var workerFound bool
	for _, h := range c.Hostnames {
		if h.Role == "worker" && h.Dev == "workerdev" {
			workerFound = true
			break
		}
	}
	if !workerFound {
		t.Errorf("separate-codebase worker missing hostname pair: %+v", c.Hostnames)
	}
}

// TestBuildSymbolContract_ShowcaseSharedCodebaseWorker — when the worker
// shares a codebase with an app target, the worker does NOT get its own
// hostname pair (no workerdev / workerstage exists).
func TestBuildSymbolContract_ShowcaseSharedCodebaseWorker(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "api-worker", Type: "nodejs@22", IsWorker: true,
				Role: "worker", SharesCodebaseWith: "api"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
	c := BuildSymbolContract(plan)

	// api-worker must NOT appear as a standalone hostname entry — it shares
	// the api target's codebase.
	for _, h := range c.Hostnames {
		if h.Dev == "api-workerdev" || h.Dev == "api-worker" {
			t.Errorf("shared-codebase worker must not have its own hostname pair; found %+v", h)
		}
	}
}

// TestBuildSymbolContract_EmptyManagedServices — plan with no managed
// services yields empty EnvVarsByKind but non-nil map and intact seeded
// rules.
func TestBuildSymbolContract_EmptyManagedServices(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierHelloWorld,
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "nodejs@22", Role: "app"},
		},
	}
	c := BuildSymbolContract(plan)
	if len(c.EnvVarsByKind) != 0 {
		t.Errorf("want zero env-var kinds, got %v", keysOf(c.EnvVarsByKind))
	}
	if len(c.NATSSubjects) != 0 || len(c.NATSQueues) != 0 {
		t.Errorf("want zero NATS entries, got subjects=%v queues=%v",
			c.NATSSubjects, c.NATSQueues)
	}
	if len(c.FixRecurrenceRules) != 12 {
		t.Errorf("fix-recurrence rules always seeded; got %d", len(c.FixRecurrenceRules))
	}
}

// TestBuildSymbolContract_IdempotentJSON — successive calls against the same
// plan yield byte-identical JSON. Critical for P3: parallel scaffold
// dispatches must see the exact same contract JSON fragment.
func TestBuildSymbolContract_IdempotentJSON(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "app", Type: "nodejs@22", Role: "app"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, Role: "worker"},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "queue", Type: "nats@2"},
		},
	}
	a, err := json.Marshal(BuildSymbolContract(plan))
	if err != nil {
		t.Fatalf("marshal 1: %v", err)
	}
	b, err := json.Marshal(BuildSymbolContract(plan))
	if err != nil {
		t.Fatalf("marshal 2: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("contract JSON not idempotent:\nfirst:  %s\nsecond: %s", a, b)
	}
}

// TestSeededFixRecurrenceRules_CoveragePerID — the 12 seeded rule IDs cover
// every recurrence class referenced in atomic-layout.md §4. Any rename
// or removal must update the seed and this list together.
func TestSeededFixRecurrenceRules_CoveragePerID(t *testing.T) {
	t.Parallel()
	want := []string{
		"nats-separate-creds",
		"s3-uses-api-url",
		"s3-force-path-style",
		"routable-bind",
		"trust-proxy",
		"graceful-shutdown",
		"queue-group",
		"env-self-shadow",
		"gitignore-baseline",
		"env-example-preserved",
		"no-scaffold-test-artifacts",
		"skip-git",
	}
	got := SeededFixRecurrenceRules()
	if len(got) != len(want) {
		t.Fatalf("rule count: got %d, want %d", len(got), len(want))
	}
	byID := map[string]FixRule{}
	for _, r := range got {
		byID[r.ID] = r
	}
	for _, id := range want {
		rule, ok := byID[id]
		if !ok {
			t.Errorf("rule %q missing from seed", id)
			continue
		}
		if rule.PositiveForm == "" {
			t.Errorf("rule %q: PositiveForm empty (P8: positive allow-list required)", id)
		}
		if rule.PreAttestCmd == "" {
			t.Errorf("rule %q: PreAttestCmd empty (P1: every check has an author-runnable form)", id)
		}
		if len(rule.AppliesTo) == 0 {
			t.Errorf("rule %q: AppliesTo empty (every rule must scope to ≥1 role or 'any')", id)
		}
	}
}

// TestSeededFixRecurrenceRules_NoNegativeProhibitions — P8 positive allow-list
// invariant: no seeded rule's PositiveForm should be a bare "do not X" /
// "avoid X" / "never X" phrase. Rules express what the scaffold MUST DO,
// not what it must not do.
func TestSeededFixRecurrenceRules_NoNegativeProhibitions(t *testing.T) {
	t.Parallel()
	banned := []string{"do not ", "never ", "avoid ", "must not "}
	for _, r := range SeededFixRecurrenceRules() {
		// The "env-self-shadow" rule's positive form legitimately says
		// "contains no KEY: ${KEY}" — that's a positive-form assertion about
		// the file's shape, not a prohibition. Same for "no-scaffold-test-
		// artifacts" (positive: no such files committed).
		for _, bad := range banned {
			if containsCI(r.PositiveForm, bad) {
				t.Errorf("rule %q PositiveForm uses negative enumeration %q: %q",
					r.ID, bad, r.PositiveForm)
			}
		}
	}
}

// keysOf returns map keys (used only by tests).
func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// containsCI is a lowercase-insensitive substring check, kept inline so
// tests do not pull strings.ToLower from non-test code paths.
func containsCI(haystack, needle string) bool {
	hLen := len(haystack)
	nLen := len(needle)
	if nLen == 0 {
		return true
	}
	if hLen < nLen {
		return false
	}
	for i := 0; i <= hLen-nLen; i++ {
		match := true
		for j := range nLen {
			hc := haystack[i+j]
			nc := needle[j]
			if hc >= 'A' && hc <= 'Z' {
				hc += 'a' - 'A'
			}
			if nc >= 'A' && nc <= 'Z' {
				nc += 'a' - 'A'
			}
			if hc != nc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
