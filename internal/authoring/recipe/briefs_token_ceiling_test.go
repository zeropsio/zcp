package recipe

import (
	"strings"
	"testing"
)

// Run-30 Fix #1 PARTIAL — brief writer chunking under the Read-tool
// 25K-token ceiling. With markdown-dense briefs the empirical worst
// case is ~2.51 chars/token (cc-api 35427 tokens / 88844 bytes;
// cc-worker 35014 / 88004; refinement 32297 / 83447). At that rate, a
// 76 KB brief encodes to ~30K real tokens — over the Read-tool 25K
// ceiling. Run-30 sub-agents recovered with offset+limit chunking;
// prompt-cache invalidates and latency adds.
//
// Fix 2 (run-30 follow-up): the estimator was changed from `len/3` to
// `len/2` after empirical 2.51 cpt worst-case proved 3.0 cpt was
// optimistic. The ceiling was re-derived against Read-tool's real 25K
// hard cap: real_tokens = bytes/2.5, want real_tokens ≤ 25000, so
// bytes ≤ 62500, and under len/2 estimator that's ≤ 31250 estimated
// tokens — rounded up to 33000 for ~1.4 KB byte-headroom against
// inputs with slightly different compression.
//
// Run-31 Fix #1 closure (multi-file architecture): multi-file kinds
// (codebase-content / env-content / refinement) have moved off the
// single-file token-ceiling guard — production no longer composes a
// single Body for those kinds; the dispatch path emits index.md + N
// part files and each part's per-part invariant is pinned by
// `TestBuildCodebaseContentBrief_MultiFile_RealSlug_PartsUnderCap` +
// siblings in `briefs_multi_file_test.go`. This test now covers only
// the kinds that REMAIN single-file: scaffold, feature, finalize,
// claudemd-author.
//
// The test uses a real-slug plan (`nestjs-showcase`) so the embedded-
// parent fallback fires (parentSlugFor returns `nestjs-minimal`),
// matching the production load shape that pushed run-30 over.

// realShowcasePlan returns a plan whose slug triggers the embedded
// parent baseline (parentSlugFor("nestjs-showcase") == "nestjs-minimal").
// Mirrors syntheticShowcasePlan otherwise. Shared with
// `briefs_multi_file_test.go`.
func realShowcasePlan() *Plan {
	return &Plan{
		Slug:      "nestjs-showcase",
		Framework: "nestjs",
		Tier:      "showcase",
		Research: ResearchResult{
			CodebaseShape:  "3",
			NeedsAppSecret: true,
			AppSecretKey:   "APP_SECRET",
			Description:    "real-slug showcase plan to exercise embedded-parent fallback",
		},
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "app", Role: RoleFrontend, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
		Services: []Service{
			{Hostname: "db", Type: "postgresql@18", Kind: ServiceKindManaged, Priority: 10},
			{Hostname: "cache", Type: "valkey@7", Kind: ServiceKindManaged, Priority: 10},
			{Hostname: "broker", Type: "nats@2", Kind: ServiceKindManaged, Priority: 10},
			{Hostname: "search", Type: "meilisearch@1.20", Kind: ServiceKindManaged, Priority: 10},
			{Hostname: "storage", Type: "object-storage", Kind: ServiceKindStorage},
		},
	}
}

func TestBrief_StaysUnderReadToolTokenCeiling(t *testing.T) {
	t.Parallel()
	plan := realShowcasePlan()

	// Multi-file kinds (codebase-content / env-content / refinement) are
	// pinned per-part in `briefs_multi_file_test.go`; only single-file
	// kinds remain in scope for this guard.
	cases := []struct {
		name  string
		build func() (Brief, error)
	}{
		{
			name: "scaffold/app",
			build: func() (Brief, error) {
				return BuildScaffoldBrief(plan, plan.Codebases[1], nil)
			},
		},
		{
			name: "feature/frontend",
			build: func() (Brief, error) {
				return BuildFeatureBrief(plan, FeaturePassFrontend)
			},
		},
		{
			name: "feature/backend",
			build: func() (Brief, error) {
				return BuildFeatureBrief(plan, FeaturePassBackend)
			},
		},
		{
			// Fix 3 (run-30 follow-up): cover the remaining brief kinds
			// from the BriefKind enum — finalize is composed once at the
			// end of a recipe build and feeds a sub-agent that validates
			// fragment-shape across the whole tree.
			name: "finalize",
			build: func() (Brief, error) {
				return BuildFinalizeBrief(plan)
			},
		},
		{
			// Fix 3 (run-30 follow-up): claudemd-author runs once per
			// codebase to author the per-codebase CLAUDE.md (Zerops-free
			// brief, §6.7a). Cap is small by construction
			// (ClaudeMDBriefCap = 8 KB) but must still pass the Read-tool
			// token ceiling under the new len/2 estimator.
			name: "claudemd-author/api",
			build: func() (Brief, error) {
				return BuildClaudeMDBrief(plan, plan.Codebases[0])
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			brief, err := tc.build()
			if err != nil {
				t.Fatalf("build %s: %v", tc.name, err)
			}
			tokens := EstimateTokens(brief.Body)
			if tokens >= ReadToolTokenCeiling {
				t.Errorf("%s brief: %d tokens (%d bytes) >= %d-token ceiling; trim composed sections so the agent can Read in one shot",
					tc.name, tokens, brief.Bytes, ReadToolTokenCeiling)
			}
		})
	}
}

// TestEstimateTokens_ConservativeFloor — sanity check that the
// estimator beats the empirical worst-case chars-per-token rate. Run-30
// sub-agents reported 35427 / 35014 / 32297 tokens against on-disk
// briefs of 88844 / 88004 / 83447 bytes — i.e. ~2.51 / 2.51 / 2.58
// chars/token at worst. A `len(s) / 3` estimator would call those
// briefs ~29.6K / 29.3K / 27.8K tokens, undercounting reality by ~17%
// and silently passing the 22K-token cap when real Read-tool would
// blow the 25K ceiling.
//
// Fix 2: estimator uses `len(s) / 2` (assumes 2.0 chars/token —
// pessimistic floor that beats observed worst case 2.51 cpt with
// safety margin). For 30K chars: 15000 tokens. The lower bound (≥14000)
// catches a divisor-flip bug where someone switches /2 → /3 (would
// yield 10000 tokens for 30K chars). The upper bound (≤20000) catches
// an absurdly low divisor (e.g. /1.5 would yield 20000).
func TestEstimateTokens_ConservativeFloor(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("a", 30000) // 30K chars
	tokens := EstimateTokens(body)
	if tokens < 14000 {
		t.Errorf("EstimateTokens(30K chars) = %d; expected ≥14000 (len/2 bound, beats observed 2.51 cpt worst case)", tokens)
	}
	if tokens > 20000 {
		t.Errorf("EstimateTokens(30K chars) = %d; expected ≤20000 (catches absurdly low divisor)", tokens)
	}
}
