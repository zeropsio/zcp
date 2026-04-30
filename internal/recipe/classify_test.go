package recipe

import (
	"strings"
	"testing"
)

// TestClassify_SurfaceHintMapping — per docs/spec-content-surfaces.md
// taxonomy + run-8-readiness §2.C tagging rules. The classifier takes
// a fact's surface hint + mechanism + citation and emits one of the
// seven classifications; DISCARD classes drop before they reach any
// surface body.
func TestClassify_SurfaceHintMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		rec  FactRecord
		want Classification
	}{
		{
			name: "platform-trap with citation in map → platform-invariant",
			rec: FactRecord{
				Topic: "env-var-model", Symptom: "blank DB_HOST",
				Mechanism:   "cross-service auto-inject collision",
				SurfaceHint: "platform-trap", Citation: "env-var-model",
			},
			want: ClassPlatformInvariant,
		},
		{
			name: "platform-trap with citation not in map → intersection",
			rec: FactRecord{
				Topic: "nestjs-execOnce-edgecase", Symptom: "rows missing",
				Mechanism:   "nestjs lifecycle + execOnce per-deploy key",
				SurfaceHint: "platform-trap", Citation: "custom-doc",
			},
			want: ClassIntersection,
		},
		{
			name: "framework-quirk → DROP",
			rec: FactRecord{
				Topic: "setGlobalPrefix-collision", Symptom: "routes 404",
				Mechanism:   "nestjs decorator precedence",
				SurfaceHint: "framework-quirk", Citation: "nestjs-docs",
			},
			want: ClassFrameworkQuirk,
		},
		{
			name: "self-inflicted → DROP",
			rec: FactRecord{
				Topic: "seed-silently-exited", Symptom: "empty db",
				Mechanism:   "our script early-returned",
				SurfaceHint: "self-inflicted", Citation: "none",
			},
			want: ClassSelfInflicted,
		},
		{
			name: "tooling-metadata → library-metadata DROP",
			rec: FactRecord{
				Topic: "vite-plugin-svelte-peer-dep", Symptom: "EPEERINVALID",
				Mechanism:   "npm peer-dep resolver",
				SurfaceHint: "tooling-metadata", Citation: "npm-docs",
			},
			want: ClassLibraryMetadata,
		},
		{
			name: "scaffold-decision → scaffold-decision",
			rec: FactRecord{
				Topic: "chose-predis-over-phpredis", Symptom: "base image lacks ext",
				Mechanism:   "php-nginx base has no phpredis",
				SurfaceHint: "scaffold-decision", Citation: "env-var-model",
			},
			want: ClassScaffoldDecision,
		},
		{
			name: "operational → operational",
			rec: FactRecord{
				Topic: "sshfs-uid-mismatch", Symptom: "EACCES on dev file writes",
				Mechanism:   "SSHFS uid mapping",
				SurfaceHint: "operational", Citation: "n/a",
			},
			want: ClassOperational,
		},
		{
			name: "porter-change → platform-invariant",
			rec: FactRecord{
				Topic: "trust-proxy", Symptom: "wrong client IP",
				Mechanism:   "L7 balancer rewrites X-Forwarded-For",
				SurfaceHint: "porter-change", Citation: "http-support",
			},
			want: ClassPlatformInvariant,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tc.rec)
			if got != tc.want {
				t.Errorf("Classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClassify_DiscardedNotPublished — classifications in the DISCARD
// set never route to a surface. Exposed via IsPublishable.
func TestClassify_DiscardedNotPublished(t *testing.T) {
	t.Parallel()

	publishable := map[Classification]bool{
		ClassPlatformInvariant: true,
		ClassIntersection:      true,
		ClassScaffoldDecision:  true,
		ClassOperational:       true,
		ClassFrameworkQuirk:    false,
		ClassLibraryMetadata:   false,
		ClassSelfInflicted:     false,
	}
	for cls, want := range publishable {
		if got := IsPublishable(cls); got != want {
			t.Errorf("IsPublishable(%q) = %v, want %v", cls, got, want)
		}
	}
}

// TestClassify_AttachesCitationGuide — a platform-invariant or
// intersection fact whose topic appears in the CitationMap has its
// guide id resolved on the classification result. Ensures the
// citation isn't left to the content-author to re-discover.
func TestClassify_AttachesCitationGuide(t *testing.T) {
	t.Parallel()

	rec := FactRecord{
		Topic: "env-var-model", Symptom: "blank", Mechanism: "self-shadow",
		SurfaceHint: "platform-trap", Citation: "env-var-model",
	}
	result := ClassifyDetailed(rec)
	if result.Guide != "env-var-model" {
		t.Errorf("guide = %q, want env-var-model", result.Guide)
	}
	if result.Class != ClassPlatformInvariant {
		t.Errorf("class = %q, want platform-invariant", result.Class)
	}
}

// TestClassify_SelfInflictedFromFixApplied — run-11 gap V-1. Run 10's
// worker bullet ".deployignore filters the build artifact" was a
// self-inflicted incident (recipe author wrote dist into .deployignore,
// deploy bricked, fix was removing dist). The agent labeled it
// platform-trap; spec rule 4 says discard. V-1 detects the shape
// deterministically — fixApplied describes a recipe-source change AND
// failureMode lacks platform-side mechanism vocabulary → override to
// ClassSelfInflicted.
func TestClassify_SelfInflictedFromFixApplied(t *testing.T) {
	t.Parallel()

	rec := FactRecord{
		Topic:       "deployignore-filters-build-artifact",
		Symptom:     "Cannot find module /var/www/dist/main.js looping every 2s",
		Mechanism:   "deployignore filters the deploy bundle",
		SurfaceHint: "platform-trap",
		Citation:    "deploy-files",
		FailureMode: "Cannot find module /var/www/dist/main.js",
		FixApplied:  "removed dist from .deployignore",
	}
	if got := Classify(rec); got != ClassSelfInflicted {
		t.Errorf("Classify() = %q, want %q (V-1 override should fire)", got, ClassSelfInflicted)
	}
	gotClass, notice := ClassifyWithNotice(rec)
	if gotClass != ClassSelfInflicted {
		t.Errorf("ClassifyWithNotice() class = %q, want %q", gotClass, ClassSelfInflicted)
	}
	if notice == "" {
		t.Fatalf("ClassifyWithNotice() should emit notice on override, got empty")
	}
	if !strings.Contains(notice, "rule 4") {
		t.Errorf("notice must name spec rule 4, got: %q", notice)
	}
	if !strings.Contains(notice, "self-inflicted") {
		t.Errorf("notice must name self-inflicted, got: %q", notice)
	}
}

// TestClassify_PlatformInvariantFromGenuineFix — V-1 must NOT
// over-trigger. The trust-proxy / L7 intersection IS platform-side
// teaching despite living in framework code. fixApplied "set
// app.set('trust proxy', true)" doesn't match self-inflicted patterns;
// failureMode "req.ip returned VXLAN peer" mentions VXLAN (platform
// vocabulary). Override stays off; record keeps its surfaceHint
// classification (citation http-support is in CitationMap →
// platform-invariant).
func TestClassify_PlatformInvariantFromGenuineFix(t *testing.T) {
	t.Parallel()

	rec := FactRecord{
		Topic:       "trust-proxy",
		Symptom:     "req.ip returns wrong client IP",
		Mechanism:   "L7 balancer rewrites X-Forwarded-For; Express ignores by default",
		SurfaceHint: "platform-trap",
		Citation:    "http-support",
		FailureMode: "req.ip returned VXLAN peer",
		FixApplied:  "set app.set('trust proxy', true)",
	}
	if got := Classify(rec); got != ClassPlatformInvariant {
		t.Errorf("Classify() = %q, want %q (no override should fire)", got, ClassPlatformInvariant)
	}
	_, notice := ClassifyWithNotice(rec)
	if notice != "" {
		t.Errorf("no notice expected for genuine platform fix, got: %q", notice)
	}
}

// TestClassify_NoOverrideWhenFieldsMissing — V-1's override requires
// both fixApplied + failureMode to be present. Older facts (pre-U-2
// schema enrichment) lack these fields and must not trigger an override.
func TestClassify_NoOverrideWhenFieldsMissing(t *testing.T) {
	t.Parallel()

	rec := FactRecord{
		Topic: "x", Symptom: "y", Mechanism: "z",
		SurfaceHint: "platform-trap", Citation: "env-var-model",
	}
	if got := Classify(rec); got != ClassPlatformInvariant {
		t.Errorf("Classify() = %q, want %q (no override; fields empty)", got, ClassPlatformInvariant)
	}
}

// TestClassifyLog_SplitsPublishableVsDropped — given a mixed facts log,
// ClassifyLog returns publishable facts separated from dropped ones so
// downstream code (finalize gates, status counts) can operate on the
// right set.
func TestClassifyLog_SplitsPublishableVsDropped(t *testing.T) {
	t.Parallel()

	records := []FactRecord{
		{Topic: "a", Symptom: "x", Mechanism: "y", SurfaceHint: "platform-trap", Citation: "env-var-model"},
		{Topic: "b", Symptom: "x", Mechanism: "y", SurfaceHint: "framework-quirk", Citation: "nest"},
		{Topic: "c", Symptom: "x", Mechanism: "y", SurfaceHint: "self-inflicted", Citation: "none"},
		{Topic: "d", Symptom: "x", Mechanism: "y", SurfaceHint: "scaffold-decision", Citation: "http-support"},
	}
	pub, dropped := ClassifyLog(records)
	if len(pub) != 2 {
		t.Errorf("publishable count = %d, want 2", len(pub))
	}
	if len(dropped) != 2 {
		t.Errorf("dropped count = %d, want 2", len(dropped))
	}
}

// TestClassificationCompatibleWithSurface — run-15 F.3 — covers every
// row of docs/spec-content-surfaces.md §"Classification × surface
// compatibility". Compatible pairs return nil; incompatible pairs return
// a redirect-teaching error; DISCARD classes refuse all surfaces.
func TestClassificationCompatibleWithSurface(t *testing.T) {
	t.Parallel()
	cases := []struct {
		class     Classification
		surface   Surface
		wantError bool
	}{
		// platform-invariant → KB or IG
		{ClassPlatformInvariant, SurfaceCodebaseKB, false},
		{ClassPlatformInvariant, SurfaceCodebaseIG, false},
		{ClassPlatformInvariant, SurfaceCodebaseCLAUDE, true},
		{ClassPlatformInvariant, SurfaceCodebaseZeropsComments, true},
		// intersection → KB only
		{ClassIntersection, SurfaceCodebaseKB, false},
		{ClassIntersection, SurfaceCodebaseIG, true},
		{ClassIntersection, SurfaceCodebaseCLAUDE, true},
		// scaffold-decision → IG / yaml comments / env-yaml comments
		{ClassScaffoldDecision, SurfaceCodebaseIG, false},
		{ClassScaffoldDecision, SurfaceCodebaseZeropsComments, false},
		{ClassScaffoldDecision, SurfaceEnvImportComments, false},
		{ClassScaffoldDecision, SurfaceCodebaseKB, true},
		{ClassScaffoldDecision, SurfaceCodebaseCLAUDE, true},
		// operational → CLAUDE.md only
		{ClassOperational, SurfaceCodebaseCLAUDE, false},
		{ClassOperational, SurfaceCodebaseKB, true},
		{ClassOperational, SurfaceCodebaseIG, true},
		// DISCARD classes — refused on every surface
		{ClassFrameworkQuirk, SurfaceCodebaseKB, true},
		{ClassFrameworkQuirk, SurfaceCodebaseIG, true},
		{ClassLibraryMetadata, SurfaceCodebaseKB, true},
		{ClassSelfInflicted, SurfaceCodebaseKB, true},
		{ClassSelfInflicted, SurfaceCodebaseIG, true},
	}
	for _, tc := range cases {
		err := classificationCompatibleWithSurface(tc.class, tc.surface)
		if tc.wantError && err == nil {
			t.Errorf("class=%q surface=%q: want error, got nil", tc.class, tc.surface)
		}
		if !tc.wantError && err != nil {
			t.Errorf("class=%q surface=%q: want nil, got %v", tc.class, tc.surface, err)
		}
	}
	// Empty class is back-compat: no refusal.
	if err := classificationCompatibleWithSurface("", SurfaceCodebaseKB); err != nil {
		t.Errorf("empty classification should be back-compat: got %v", err)
	}
	// Unknown class is rejected with an actionable error.
	if err := classificationCompatibleWithSurface(Classification("bogus"), SurfaceCodebaseKB); err == nil {
		t.Error("unknown classification should be rejected")
	} else if !strings.Contains(err.Error(), "unknown classification") {
		t.Errorf("unknown classification error should name the gap: %v", err)
	}
}

// TestRecordFragment_RefusesIncompatibleClassification pins the engine
// refusal at record-fragment time. The plan is unmodified after a
// refused call — the agent can re-record with a corrected
// classification (or re-classify the fact and discard if no surface fits).
func TestRecordFragment_RefusesIncompatibleClassification(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", dir+"/run"); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	// self-inflicted on KB — must be refused.
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/knowledge-base", Fragment: "scaffold body",
		Classification: "self-inflicted",
	})
	if res.OK {
		t.Fatalf("record-fragment: self-inflicted on KB should be refused; got OK=true")
	}
	if !strings.Contains(res.Error, "self-inflicted") {
		t.Errorf("error should name the classification: %q", res.Error)
	}
	// Plan stays untouched — no fragment was recorded.
	if got := sess.Plan.Fragments["codebase/api/knowledge-base"]; got != "" {
		t.Errorf("refused record must not store fragment; got %q", got)
	}

	// Compatible pair (platform-invariant on KB) — accepts.
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/knowledge-base", Fragment: "scaffold body",
		Classification: "platform-invariant",
	})
	if !res.OK {
		t.Errorf("record-fragment: platform-invariant on KB should accept; got %s", res.Error)
	}

	// Empty classification on multi-class surface — refused.
	//
	// Run-19 prep: surfaces that admit multiple classes (KB and IG)
	// REQUIRE Classification at record-time. The earlier back-compat
	// path silently let agents skip the field, masking the run-18
	// wrong-surface routing. Single-class surfaces still accept empty
	// classification (covered by TestRecordFragment_RequiresClassification_OnKBAndIG).
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "scaffold body",
	})
	if res.OK {
		t.Errorf("record-fragment without classification on IG should refuse (run-19 require-classification rule); got OK")
	}
	if !strings.Contains(res.Error, "classification is required") {
		t.Errorf("refusal message must reference required classification; got %q", res.Error)
	}
}
