package recipe

import (
	"strings"
	"testing"
)

// Run-45 Pillar C — enrich-findings engine-side determinism tests.
//
// Validates that the audit sub-agent's SLIM findings JSON gets enriched
// at the engine boundary with three deterministic fields:
//
//   - fragmentId           — derived from surface + scope
//   - classification       — looked up from facts.jsonl candidateClass,
//                            falling back to surface defaults
//   - suggestedReplacement — rendered from the Citation Map (form-(b)
//                            markdown link) when the failure mode is
//                            REWRITE AND topic is a citation family

// TestEnrichFindings_FragmentIDForEachSurface — each surface maps to
// the canonical short-form fragment id. Mirrors the routing rule the
// main agent's `record-fragment` handler expects.
func TestEnrichFindings_FragmentIDForEachSurface(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		slim     SlimFinding
		expectFI string
	}{
		{
			name:     "S4 IG without slot",
			slim:     SlimFinding{Surface: "S4", Scope: "api", ItemReference: "bullet stem"},
			expectFI: "codebase/api/integration-guide",
		},
		{
			name:     "S4 IG with explicit slot",
			slim:     SlimFinding{Surface: "S4", Scope: "api", ItemReference: "/run/dir/apidev/README.md IG #3"},
			expectFI: "codebase/api/integration-guide/3",
		},
		{
			name:     "S5 KB short form",
			slim:     SlimFinding{Surface: "S5", Scope: "app", ItemReference: "### Some symptom"},
			expectFI: "codebase/app/knowledge-base",
		},
		{
			name:     "S6 CLAUDE.md",
			slim:     SlimFinding{Surface: "S6", Scope: "worker", ItemReference: "## Dev loop"},
			expectFI: "codebase/worker/claude-md",
		},
		{
			name:     "S7 zerops.yaml",
			slim:     SlimFinding{Surface: "S7", Scope: "api", ItemReference: "comment at line 42"},
			expectFI: "codebase/api/zerops-yaml",
		},
		{
			name:     "S5 SSHFS form rejects empty scope",
			slim:     SlimFinding{Surface: "S5", Scope: "", ItemReference: "x"},
			expectFI: "",
		},
		{
			name:     "S3 project block",
			slim:     SlimFinding{Surface: "S3", Scope: "", ItemReference: "tier 0 project block"},
			expectFI: "env/<N>/import-comments/project",
		},
		{
			name:     "S3 per-host",
			slim:     SlimFinding{Surface: "S3", Scope: "api", ItemReference: "api service block"},
			expectFI: "env/<N>/import-comments/api",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := &FindingsEnvelope{Findings: []SlimFinding{tc.slim}}
			out := EnrichFindings(env, nil)
			if len(out.Findings) != 1 {
				t.Fatalf("expected 1 enriched finding, got %d", len(out.Findings))
			}
			if got := out.Findings[0].FragmentID; got != tc.expectFI {
				t.Errorf("fragmentId: got %q, want %q", got, tc.expectFI)
			}
		})
	}
}

// TestEnrichFindings_SuggestedReplacementForCitations — REWRITE
// findings with a citation-map topic get the form-(b) markdown link.
func TestEnrichFindings_SuggestedReplacementForCitations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		slim        SlimFinding
		wantPiece   string
		wantPresent bool
	}{
		{
			name:        "REWRITE + rolling-deploys topic",
			slim:        SlimFinding{Surface: "S5", Scope: "worker", Topic: "rolling-deploys", SurfaceTestFailureMode: "REWRITE"},
			wantPiece:   "[zero-downtime deploys with multi-container setups](https://docs.zerops.io/features/scaling-ha)",
			wantPresent: true,
		},
		{
			name:        "REWRITE + env-var-model topic",
			slim:        SlimFinding{Surface: "S5", Scope: "api", Topic: "env-var-model", SurfaceTestFailureMode: "REWRITE"},
			wantPiece:   "[per-key env shape and cross-service aliases](https://docs.zerops.io/zerops-yaml/specification#envvariables-)",
			wantPresent: true,
		},
		{
			name:        "REWRITE + http-support topic",
			slim:        SlimFinding{Surface: "S4", Scope: "api", Topic: "http-support", SurfaceTestFailureMode: "REWRITE"},
			wantPiece:   "[Zerops L7 balancer + subdomain access](https://docs.zerops.io/features/access)",
			wantPresent: true,
		},
		{
			name:        "REWRITE + managed-services-nats topic",
			slim:        SlimFinding{Surface: "S5", Scope: "worker", Topic: "managed-services-nats", SurfaceTestFailureMode: "REWRITE"},
			wantPiece:   "[managed NATS broker](https://docs.zerops.io/services/nats)",
			wantPresent: true,
		},
		{
			name:        "DROP — no replacement rendered",
			slim:        SlimFinding{Surface: "S5", Scope: "worker", Topic: "rolling-deploys", SurfaceTestFailureMode: "DROP"},
			wantPiece:   "[",
			wantPresent: false,
		},
		{
			name:        "MOVE-TO — no replacement rendered",
			slim:        SlimFinding{Surface: "S5", Scope: "api", Topic: "rolling-deploys", SurfaceTestFailureMode: "MOVE-TO-S6"},
			wantPiece:   "[",
			wantPresent: false,
		},
		{
			name:        "REWRITE without topic — no replacement",
			slim:        SlimFinding{Surface: "S7", Scope: "api", Topic: "", SurfaceTestFailureMode: "REWRITE"},
			wantPiece:   "[",
			wantPresent: false,
		},
		{
			name:        "REWRITE + unknown topic — no replacement",
			slim:        SlimFinding{Surface: "S5", Scope: "api", Topic: "made-up-topic", SurfaceTestFailureMode: "REWRITE"},
			wantPiece:   "[",
			wantPresent: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := &FindingsEnvelope{Findings: []SlimFinding{tc.slim}}
			out := EnrichFindings(env, nil)
			if len(out.Findings) != 1 {
				t.Fatalf("expected 1 enriched finding, got %d", len(out.Findings))
			}
			got := out.Findings[0].SuggestedReplacement
			if tc.wantPresent {
				if !strings.Contains(got, tc.wantPiece) {
					t.Errorf("suggestedReplacement: got %q, want substring %q", got, tc.wantPiece)
				}
			} else {
				if got != "" {
					t.Errorf("expected no suggestedReplacement, got %q", got)
				}
			}
		})
	}
}

// TestEnrichFindings_ClassificationFromFactRef — Run-45 Issue 3. A
// finding's `factRef` identifies the exact `facts.jsonl` record the
// engine should read for classification. Topic-substring fuzziness
// retires; the factRef shape `<topic>@<recordedAt>` is unambiguous.
func TestEnrichFindings_ClassificationFromFactRef(t *testing.T) {
	t.Parallel()
	facts := []FactRecord{
		{
			Kind:             FactKindPorterChange,
			Topic:            "rolling-deploys",
			Service:          "worker",
			RecordedAt:       "2026-05-11T08:00:00Z",
			CandidateClass:   "intersection",
			Why:              "queue-group mechanics on minContainers >= 2",
			CandidateSurface: "CODEBASE_KB",
		},
		{
			Kind:             FactKindPorterChange,
			Topic:            "init-commands",
			Service:          "api",
			RecordedAt:       "2026-05-11T08:30:00Z",
			CandidateClass:   "platform-invariant",
			Why:              "execOnce + appVersionId model",
			CandidateSurface: "CODEBASE_IG",
		},
		// Two facts sharing an overlapping topic stem — the run-44
		// substring-match ambiguity. With factRef, the engine can
		// pick the right one deterministically.
		{
			Kind:             FactKindPorterChange,
			Topic:            "no-dotenv-file",
			Service:          "api",
			RecordedAt:       "2026-05-11T09:00:00Z",
			CandidateClass:   "platform-invariant",
			Why:              "no dotenv loader needed on Zerops",
			CandidateSurface: "CODEBASE_IG",
		},
		{
			Kind:             FactKindPorterChange,
			Topic:            "worker-env-var-key-alignment-v2",
			Service:          "worker",
			RecordedAt:       "2026-05-11T09:30:00Z",
			CandidateClass:   "scaffold-decision",
			Why:              "worker env-key alignment with project keys",
			CandidateSurface: "CODEBASE_IG",
		},
	}
	cases := []struct {
		name      string
		slim      SlimFinding
		wantClass string
	}{
		{
			name:      "S5 factRef exact match — rolling-deploys",
			slim:      SlimFinding{Surface: "S5", Scope: "worker", Topic: "rolling-deploys", FactRef: "rolling-deploys@2026-05-11T08:00:00Z", SurfaceTestFailureMode: "REWRITE"},
			wantClass: "intersection",
		},
		{
			name:      "S4 factRef exact match — init-commands",
			slim:      SlimFinding{Surface: "S4", Scope: "api", Topic: "init-commands", FactRef: "init-commands@2026-05-11T08:30:00Z", SurfaceTestFailureMode: "REWRITE"},
			wantClass: "platform-invariant",
		},
		{
			name:      "S4 factRef disambiguates overlapping-stem topics — picks no-dotenv-file",
			slim:      SlimFinding{Surface: "S4", Scope: "api", FactRef: "no-dotenv-file@2026-05-11T09:00:00Z", SurfaceTestFailureMode: "REWRITE"},
			wantClass: "platform-invariant",
		},
		{
			name:      "S5 missing factRef — surface default",
			slim:      SlimFinding{Surface: "S5", Scope: "app", Topic: "rolling-deploys", SurfaceTestFailureMode: "REWRITE"},
			wantClass: "intersection", // surface default
		},
		{
			name:      "S5 factRef points at nonexistent fact — surface default",
			slim:      SlimFinding{Surface: "S5", Scope: "api", Topic: "rolling-deploys", FactRef: "rolling-deploys@1999-01-01T00:00:00Z", SurfaceTestFailureMode: "REWRITE"},
			wantClass: "intersection", // surface default
		},
		{
			name:      "S5 empty topic + no factRef — surface default",
			slim:      SlimFinding{Surface: "S5", Scope: "api", SurfaceTestFailureMode: "DROP"},
			wantClass: "intersection", // surface default
		},
		{
			name:      "S5 factRef bare-topic backstop (no @ separator)",
			slim:      SlimFinding{Surface: "S5", Scope: "worker", FactRef: "rolling-deploys", SurfaceTestFailureMode: "REWRITE"},
			wantClass: "intersection", // matches by topic alone
		},
		{
			name:      "S7 finding — no classification (engine doesn't require it on this surface)",
			slim:      SlimFinding{Surface: "S7", Scope: "api", Topic: "rolling-deploys", FactRef: "rolling-deploys@2026-05-11T08:00:00Z", SurfaceTestFailureMode: "REWRITE"},
			wantClass: "",
		},
		{
			name:      "S6 finding — no classification",
			slim:      SlimFinding{Surface: "S6", Scope: "api", SurfaceTestFailureMode: "DROP"},
			wantClass: "",
		},
		{
			name:      "S3 finding — no classification",
			slim:      SlimFinding{Surface: "S3", SurfaceTestFailureMode: "REWRITE"},
			wantClass: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := &FindingsEnvelope{Findings: []SlimFinding{tc.slim}}
			out := EnrichFindings(env, facts)
			if len(out.Findings) != 1 {
				t.Fatalf("expected 1 enriched finding, got %d", len(out.Findings))
			}
			if got := out.Findings[0].Classification; got != tc.wantClass {
				t.Errorf("classification: got %q, want %q", got, tc.wantClass)
			}
		})
	}
}

// TestEnrichFindings_BareTopicBackstop_RefusesOnAmbiguity — Run-45 Issue 3
// follow-up. The bare-topic backstop (factRef carries `<topic>` without
// `@<recordedAt>`) MUST refuse to disambiguate when two or more facts
// share the topic but have DIFFERENT `candidateClass` values. Returning
// the first match was the run-44 ambiguity that the unambiguous factRef
// shape was meant to retire; the backstop carried that same defect.
//
// Live witness: run-42 facts.jsonl lines 4 + 19 both carry
// `topic=db-env-own-key-aliases` on `scope=api` with classes
// `platform-invariant` and `scaffold-decision` respectively — the
// bare-topic backstop would non-deterministically return one or the
// other based on iteration order.
//
// Behavior under ambiguity: return empty so the engine falls to the
// surface default; the audit sub-agent's brief instructs it to ALWAYS
// emit factRef as `<topic>@<recordedAt>` when the topic appears more
// than once. Refuse-on-ambiguity surfaces the under-emit truthfully
// rather than manufacturing a wrong answer.
func TestEnrichFindings_BareTopicBackstop_RefusesOnAmbiguity(t *testing.T) {
	t.Parallel()
	// Strip-down of run-42 facts.jsonl lines 4 + 19 — same topic +
	// same scope, different candidateClass.
	facts := []FactRecord{
		{
			Kind:             FactKindFieldRationale,
			Topic:            "db-env-own-key-aliases",
			Service:          "api",
			RecordedAt:       "2026-05-12T07:35:16Z",
			CandidateClass:   "platform-invariant",
			Why:              "DB_* aliases keep app code portable",
			CandidateSurface: "CODEBASE_ZEROPS_COMMENTS",
		},
		{
			Kind:             FactKindFieldRationale,
			Topic:            "db-env-own-key-aliases",
			Service:          "api",
			RecordedAt:       "2026-05-12T07:35:52Z",
			CandidateClass:   "scaffold-decision",
			Why:              "DB_* aliases as a scaffold convenience",
			CandidateSurface: "CODEBASE_ZEROPS_COMMENTS",
		},
	}
	slim := SlimFinding{
		Surface:                "S5",
		Scope:                  "api",
		FactRef:                "db-env-own-key-aliases", // bare topic — AMBIGUOUS
		SurfaceTestFailureMode: "REWRITE",
		ItemReference:          "x",
	}
	env := &FindingsEnvelope{Findings: []SlimFinding{slim}}
	out := EnrichFindings(env, facts)
	got := out.Findings[0]
	// Surface default fires (S5 → "intersection") because the bare-topic
	// match resolved to ambiguity and the engine refused to pick.
	if got.Classification != string(ClassIntersection) {
		t.Errorf("ambiguous bare-topic backstop should yield surface default %q, got %q (engine MUST refuse to disambiguate; falling back to surface default is the contract)", ClassIntersection, got.Classification)
	}
}

// TestEnrichFindings_BareTopicBackstop_UnambiguousMatchesByTopic — Run-45
// Issue 3 follow-up. The bare-topic backstop's positive case: when ALL
// records sharing the topic also share the same `candidateClass`, the
// backstop returns that class. Refuse-on-ambiguity is gracefully
// failing, not breaking the common-case bare-topic emit.
func TestEnrichFindings_BareTopicBackstop_UnambiguousMatchesByTopic(t *testing.T) {
	t.Parallel()
	facts := []FactRecord{
		{
			Kind:           FactKindPorterChange,
			Topic:          "rolling-deploys",
			Service:        "worker",
			RecordedAt:     "2026-05-11T08:00:00Z",
			CandidateClass: "platform-invariant",
			Why:            "first witness",
		},
		// Same topic + same class on a different fact — bare-topic
		// backstop SHOULD resolve to the shared class (no ambiguity).
		{
			Kind:           FactKindPorterChange,
			Topic:          "rolling-deploys",
			Service:        "api",
			RecordedAt:     "2026-05-11T09:00:00Z",
			CandidateClass: "platform-invariant",
			Why:            "second witness, same class",
		},
	}
	slim := SlimFinding{
		Surface:                "S4",
		Scope:                  "worker",
		FactRef:                "rolling-deploys", // bare topic, all facts agree
		SurfaceTestFailureMode: "REWRITE",
		ItemReference:          "x",
	}
	env := &FindingsEnvelope{Findings: []SlimFinding{slim}}
	out := EnrichFindings(env, facts)
	got := out.Findings[0]
	if got.Classification != "platform-invariant" {
		t.Errorf("unambiguous bare-topic backstop should resolve to the shared class %q, got %q", "platform-invariant", got.Classification)
	}
}

// TestEnrichFindings_BareTopicBackstop_AllEmptyCandidateClass — Run-45
// Issue 3 follow-up coverage gap. When every fact matching the bare
// topic has an empty `candidateClass`, the backstop must return ""
// (no resolution); the surface default then fires upstream. Covers
// the implicit branch in classificationFromFactRef where `resolved`
// never seeds.
func TestEnrichFindings_BareTopicBackstop_AllEmptyCandidateClass(t *testing.T) {
	t.Parallel()
	facts := []FactRecord{
		{
			Kind:           FactKindPorterChange,
			Topic:          "no-class-recorded",
			Service:        "api",
			RecordedAt:     "2026-05-11T08:00:00Z",
			CandidateClass: "", // empty
			Why:            "first witness, no class",
		},
		{
			Kind:           FactKindFieldRationale,
			Topic:          "no-class-recorded",
			Service:        "api",
			RecordedAt:     "2026-05-11T09:00:00Z",
			CandidateClass: "", // empty
			Why:            "second witness, also no class",
		},
	}
	slim := SlimFinding{
		Surface:                "S5",
		Scope:                  "api",
		FactRef:                "no-class-recorded", // bare topic
		SurfaceTestFailureMode: "REWRITE",
		ItemReference:          "x",
	}
	env := &FindingsEnvelope{Findings: []SlimFinding{slim}}
	out := EnrichFindings(env, facts)
	got := out.Findings[0]
	// All matched facts have empty candidateClass → backstop returns "" →
	// engine falls to surface default (S5 → "intersection").
	if got.Classification != string(ClassIntersection) {
		t.Errorf("bare-topic backstop with all-empty candidate class should yield surface default %q, got %q", ClassIntersection, got.Classification)
	}
}

// TestEnrichFindings_DiscardClassTriggersDropOverride — Run-45 Issue 2.
// When the resolved fact's `candidateClass` is a DISCARD class
// (self-inflicted / framework-quirk / library-metadata), the engine
// MUST NOT populate that class on a KB/IG finding (Pillar C's
// copy-verbatim contract would then trigger
// `classificationCompatibleWithSurface` rejection at the main agent's
// `record-fragment` ACT). Instead, override
// `surfaceTestFailureMode` to `DROP` and leave `classification`
// empty — the DISCARD class IS the surface test's failure rationale.
func TestEnrichFindings_DiscardClassTriggersDropOverride(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		factClass string
	}{
		{name: "self-inflicted", factClass: "self-inflicted"},
		{name: "framework-quirk", factClass: "framework-quirk"},
		{name: "library-metadata", factClass: "library-metadata"},
	}
	// Cross every DISCARD-class against every input failure-mode the
	// audit can emit. The DISCARD-class override must dominate the
	// input mode for ALL of REWRITE / DROP / MOVE-TO — the fact's
	// classification is the ground-truth signal that the item should
	// be dropped regardless of what the sub-agent emitted.
	inputModes := []string{"REWRITE", "DROP", "MOVE-TO-S6"}
	for _, tc := range cases {
		for _, surface := range []string{"S4", "S5"} {
			for _, mode := range inputModes {
				t.Run(tc.name+"/"+surface+"/"+mode, func(t *testing.T) {
					t.Parallel()
					facts := []FactRecord{{
						Kind:           FactKindPorterChange,
						Topic:          "some-topic",
						Service:        "api",
						RecordedAt:     "2026-05-11T10:00:00Z",
						CandidateClass: tc.factClass,
						Why:            "test fact",
					}}
					slim := SlimFinding{
						Surface:                surface,
						Scope:                  "api",
						ItemReference:          "x",
						Topic:                  "some-topic",
						FactRef:                "some-topic@2026-05-11T10:00:00Z",
						SurfaceTestFailureMode: mode,
						Rationale:              "DISCARD-class override should dominate input mode",
					}
					env := &FindingsEnvelope{Findings: []SlimFinding{slim}}
					out := EnrichFindings(env, facts)
					got := out.Findings[0]
					if got.Classification != "" {
						t.Errorf("DISCARD-class %q on %s (input=%s): classification should be empty (got %q) — engine MUST NOT propagate incompatible class to the agent", tc.factClass, surface, mode, got.Classification)
					}
					if got.SurfaceTestFailureMode != "DROP" {
						t.Errorf("DISCARD-class %q on %s (input=%s): surfaceTestFailureMode should be overridden to DROP (got %q) — DISCARD trumps input mode", tc.factClass, surface, mode, got.SurfaceTestFailureMode)
					}
					// suggestedReplacement must not be set on an overridden
					// DROP — the action itself is the replacement.
					if got.SuggestedReplacement != "" {
						t.Errorf("DISCARD-class %q on %s (input=%s): suggestedReplacement should be empty on DROP override (got %q)", tc.factClass, surface, mode, got.SuggestedReplacement)
					}
				})
			}
		}
	}
}

// TestEnrichFindings_NeverProducesIncompatibleClassification — table
// sweep against every (class, surface) pair. The post-enrichment
// finding's classification must always pass
// classificationCompatibleWithSurface against its declared surface,
// regardless of what the fact's candidateClass was.
func TestEnrichFindings_NeverProducesIncompatibleClassification(t *testing.T) {
	t.Parallel()
	allClasses := []Classification{
		ClassPlatformInvariant, ClassIntersection,
		ClassScaffoldDecision, ClassOperational,
		ClassFrameworkQuirk, ClassLibraryMetadata, ClassSelfInflicted,
	}
	for _, surfaceAudit := range []string{"S4", "S5"} {
		for _, c := range allClasses {
			t.Run(string(c)+"/"+surfaceAudit, func(t *testing.T) {
				t.Parallel()
				facts := []FactRecord{{
					Kind:           FactKindPorterChange,
					Topic:          "t",
					Service:        "api",
					RecordedAt:     "2026-05-11T10:00:00Z",
					CandidateClass: string(c),
					Why:            "test",
				}}
				slim := SlimFinding{
					Surface:                surfaceAudit,
					Scope:                  "api",
					ItemReference:          "x",
					Topic:                  "t",
					FactRef:                "t@2026-05-11T10:00:00Z",
					SurfaceTestFailureMode: "REWRITE",
					Rationale:              "sweep",
				}
				env := &FindingsEnvelope{Findings: []SlimFinding{slim}}
				out := EnrichFindings(env, facts)
				got := out.Findings[0]
				var engineSurface Surface
				if surfaceAudit == "S4" {
					engineSurface = SurfaceCodebaseIG
				} else {
					engineSurface = SurfaceCodebaseKB
				}
				if got.Classification == "" {
					return // empty class is back-compat-allowed downstream
				}
				if err := classificationCompatibleWithSurface(Classification(got.Classification), engineSurface); err != nil {
					t.Errorf("EnrichFindings produced incompatible (class=%q, surface=%q) pair: %v", got.Classification, engineSurface, err)
				}
			})
		}
	}
}

// TestEnrichFindings_EmptyEnvelopeReturnsEmpty — no findings = no
// enriched findings; empty envelope is a valid pass.
func TestEnrichFindings_EmptyEnvelopeReturnsEmpty(t *testing.T) {
	t.Parallel()
	out := EnrichFindings(&FindingsEnvelope{}, nil)
	if len(out.Findings) != 0 {
		t.Errorf("expected zero enriched findings, got %d", len(out.Findings))
	}
	out2 := EnrichFindings(nil, nil)
	if len(out2.Findings) != 0 {
		t.Errorf("expected zero enriched findings on nil envelope, got %d", len(out2.Findings))
	}
}

// TestEnrichFindings_PreservesSlimFields — engine enrichment is
// strictly additive; agent-authored fields pass through untouched.
func TestEnrichFindings_PreservesSlimFields(t *testing.T) {
	t.Parallel()
	slim := SlimFinding{
		Surface:                "S5",
		Scope:                  "worker",
		ItemReference:          "/run/workerdev/README.md:KB #2",
		SurfaceTestFailureMode: "DROP",
		Topic:                  "rolling-deploys",
		Severity:               "blocker",
		Rationale:              "the bullet teaches a framework quirk; framework docs cover it.",
	}
	env := &FindingsEnvelope{Findings: []SlimFinding{slim}}
	out := EnrichFindings(env, nil)
	got := out.Findings[0]
	if got.Surface != slim.Surface {
		t.Errorf("Surface drift: %q vs %q", got.Surface, slim.Surface)
	}
	if got.Scope != slim.Scope {
		t.Errorf("Scope drift: %q vs %q", got.Scope, slim.Scope)
	}
	if got.ItemReference != slim.ItemReference {
		t.Errorf("ItemReference drift: %q vs %q", got.ItemReference, slim.ItemReference)
	}
	if got.SurfaceTestFailureMode != slim.SurfaceTestFailureMode {
		t.Errorf("SurfaceTestFailureMode drift: %q vs %q", got.SurfaceTestFailureMode, slim.SurfaceTestFailureMode)
	}
	if got.Topic != slim.Topic {
		t.Errorf("Topic drift: %q vs %q", got.Topic, slim.Topic)
	}
	if got.Severity != slim.Severity {
		t.Errorf("Severity drift: %q vs %q", got.Severity, slim.Severity)
	}
	if got.Rationale != slim.Rationale {
		t.Errorf("Rationale drift: %q vs %q", got.Rationale, slim.Rationale)
	}
}

// TestParseFindingsJSON_StripsCodeFence — the audit sub-agent's emit
// is wrapped in ```json ... ```; ParseFindingsJSON tolerates the fence.
func TestParseFindingsJSON_StripsCodeFence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want int // expected number of findings parsed
	}{
		{
			name: "fenced ```json with one finding",
			raw:  "```json\n{\"findings\":[{\"surface\":\"S5\",\"scope\":\"api\",\"itemReference\":\"x\",\"surfaceTestFailureMode\":\"DROP\",\"rationale\":\"y\"}]}\n```",
			want: 1,
		},
		{
			name: "fenced ``` without language tag",
			raw:  "```\n{\"findings\":[]}\n```",
			want: 0,
		},
		{
			name: "unfenced JSON",
			raw:  `{"findings":[{"surface":"S4","scope":"worker","itemReference":"z","surfaceTestFailureMode":"REWRITE","rationale":"q"}]}`,
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, err := ParseFindingsJSON(tc.raw)
			if err != nil {
				t.Fatalf("ParseFindingsJSON: %v", err)
			}
			if len(env.Findings) != tc.want {
				t.Errorf("got %d findings, want %d", len(env.Findings), tc.want)
			}
		})
	}
}

// TestEnrichFindings_HandlerActionEndToEnd — wires the action through
// the public dispatch surface so the MCP path is covered.
func TestEnrichFindings_HandlerActionEndToEnd(t *testing.T) {
	t.Parallel()
	store := NewStore("")
	sess, err := store.OpenOrCreate("enrich-test", t.TempDir())
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	// Append a porter_change fact so the classification lookup has
	// something to find. Pre-stamp RecordedAt so the finding's factRef
	// is deterministic (Append leaves a non-empty RecordedAt as-is per
	// `facts.go::Append`).
	fact := FactRecord{
		Kind:             FactKindPorterChange,
		Topic:            "rolling-deploys",
		Service:          "worker",
		RecordedAt:       "2026-05-13T10:00:00Z",
		CandidateClass:   "intersection",
		Why:              "queue group on minContainers >= 2",
		CandidateSurface: "CODEBASE_KB",
	}
	if err := sess.FactsLog.Append(fact); err != nil {
		t.Fatalf("Append fact: %v", err)
	}

	in := RecipeInput{
		Action: "enrich-findings",
		Slug:   "enrich-test",
		Findings: &FindingsEnvelope{
			Findings: []SlimFinding{
				{
					Surface:                "S5",
					Scope:                  "worker",
					ItemReference:          "### Queue-group exactly-once",
					SurfaceTestFailureMode: "REWRITE",
					Topic:                  "rolling-deploys",
					FactRef:                "rolling-deploys@2026-05-13T10:00:00Z",
					Severity:               "advisory",
					Rationale:              "needs the rolling-deploys cite.",
				},
				{
					Surface:                "S5",
					Scope:                  "api",
					ItemReference:          "### Cache demo: tryGetClient helper",
					SurfaceTestFailureMode: "DROP",
					Rationale:              "self-referential decoration",
				},
			},
		},
	}
	r := dispatch(t.Context(), store, in)
	if !r.OK {
		t.Fatalf("enrich-findings handler failed: %s", r.Error)
	}
	if r.EnrichedFindings == nil {
		t.Fatal("EnrichedFindings is nil on a successful enrich-findings call")
	}
	if len(r.EnrichedFindings.Findings) != 2 {
		t.Fatalf("expected 2 enriched findings, got %d", len(r.EnrichedFindings.Findings))
	}
	// First finding: REWRITE + rolling-deploys → enriched fragmentId,
	// classification from fact, suggestedReplacement from citation map.
	first := r.EnrichedFindings.Findings[0]
	if first.FragmentID != "codebase/worker/knowledge-base" {
		t.Errorf("first.FragmentID = %q, want %q", first.FragmentID, "codebase/worker/knowledge-base")
	}
	if first.Classification != "intersection" {
		t.Errorf("first.Classification = %q, want %q", first.Classification, "intersection")
	}
	if !strings.Contains(first.SuggestedReplacement, "docs.zerops.io/features/scaling-ha") {
		t.Errorf("first.SuggestedReplacement = %q, want substring `docs.zerops.io/features/scaling-ha`", first.SuggestedReplacement)
	}
	// Second finding: DROP → no suggestedReplacement; classification
	// still surface-default.
	second := r.EnrichedFindings.Findings[1]
	if second.FragmentID != "codebase/api/knowledge-base" {
		t.Errorf("second.FragmentID = %q, want %q", second.FragmentID, "codebase/api/knowledge-base")
	}
	if second.Classification != "intersection" {
		t.Errorf("second.Classification = %q, want default %q", second.Classification, "intersection")
	}
	if second.SuggestedReplacement != "" {
		t.Errorf("second.SuggestedReplacement = %q, want empty (DROP has no replacement)", second.SuggestedReplacement)
	}
}

// TestEnrichFindings_HandlerActionAcceptsRawFindingsJSON — pin the
// raw-JSON entry path (main agent pastes the fenced JSON verbatim).
func TestEnrichFindings_HandlerActionAcceptsRawFindingsJSON(t *testing.T) {
	t.Parallel()
	store := NewStore("")
	_, err := store.OpenOrCreate("enrich-raw", t.TempDir())
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	raw := "```json\n" +
		`{"findings":[{"surface":"S4","scope":"api","itemReference":"IG #2","surfaceTestFailureMode":"REWRITE","topic":"http-support","severity":"advisory","rationale":"trust proxy item needs the http-support cite"}]}` +
		"\n```"
	in := RecipeInput{
		Action:       "enrich-findings",
		Slug:         "enrich-raw",
		FindingsJSON: raw,
	}
	r := dispatch(t.Context(), store, in)
	if !r.OK {
		t.Fatalf("enrich-findings (raw) handler failed: %s", r.Error)
	}
	if r.EnrichedFindings == nil || len(r.EnrichedFindings.Findings) != 1 {
		t.Fatalf("expected 1 enriched finding, got %v", r.EnrichedFindings)
	}
	f := r.EnrichedFindings.Findings[0]
	if f.FragmentID != "codebase/api/integration-guide/2" {
		t.Errorf("FragmentID = %q, want codebase/api/integration-guide/2", f.FragmentID)
	}
	if !strings.Contains(f.SuggestedReplacement, "Zerops L7 balancer") {
		t.Errorf("SuggestedReplacement = %q, want substring `Zerops L7 balancer`", f.SuggestedReplacement)
	}
}

// TestEnrichFindings_HandlerActionRejectsEmptyInput — without findings
// (parsed) or findingsJSON, action errors.
func TestEnrichFindings_HandlerActionRejectsEmptyInput(t *testing.T) {
	t.Parallel()
	store := NewStore("")
	_, err := store.OpenOrCreate("enrich-empty", t.TempDir())
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	in := RecipeInput{Action: "enrich-findings", Slug: "enrich-empty"}
	r := dispatch(t.Context(), store, in)
	if r.OK {
		t.Errorf("expected enrich-findings without findings to error")
	}
	if !strings.Contains(r.Error, "at least one of") {
		t.Errorf("expected error message naming required-input rule; got %q", r.Error)
	}
}
