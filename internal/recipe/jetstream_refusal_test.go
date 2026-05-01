package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRecordFragment_Run20C1_JetStreamFraming pins Run-20 C1: env
// import-comment fragments invoking JetStream framing without an
// attesting `nats-jetstream-*` fact in the FactsLog refuse at record
// time. With an attesting fact, the same body passes. Run-19's tier-0
// + tier-5 import.yaml shipped fabricated JetStream framing on a
// recipe that uses only core pub/sub; the env-content composer didn't
// see the NATS atom that distinguishes the two shapes. This test pins
// the additive layer-2 backstop: the principle teaches the dichotomy
// at brief-compose time; this refusal catches it at record time.
func TestRecordFragment_Run20C1_JetStreamFraming(t *testing.T) {
	t.Parallel()

	// Body containing a JetStream framing token — same shape run-19
	// shipped on tier-0 + tier-5 import.yaml.
	jsBody := "queue — JetStream-backed streams keep messages durable across restarts."

	t.Run("refuse_without_attesting_fact", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		store := NewStore(dir)
		if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
			t.Fatalf("OpenOrCreate: %v", err)
		}
		sess, _ := store.Get("synth-showcase")
		sess.Plan = syntheticShowcasePlan()

		res := dispatch(t.Context(), store, RecipeInput{
			Action:     "record-fragment",
			Slug:       "synth-showcase",
			FragmentID: "env/0/import-comments/broker",
			Fragment:   jsBody,
		})
		if res.OK {
			t.Fatalf("expected JetStream framing without attestation to refuse; got OK")
		}
		if !strings.Contains(res.Error, "JetStream") {
			t.Errorf("refusal must name the offending token; got %q", res.Error)
		}
		if !strings.Contains(res.Error, "nats-jetstream") {
			t.Errorf("refusal must point to the attesting topic; got %q", res.Error)
		}
		if !strings.Contains(res.Error, "principles/nats-shapes.md") {
			t.Errorf("refusal must cite the principle; got %q", res.Error)
		}
	})

	t.Run("pass_with_attesting_fact", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		store := NewStore(dir)
		if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
			t.Fatalf("OpenOrCreate: %v", err)
		}
		sess, _ := store.Get("synth-showcase")
		sess.Plan = syntheticShowcasePlan()
		// Record an attesting fact — recipe genuinely uses JetStream.
		if err := sess.FactsLog.Append(FactRecord{
			Topic:            "nats-jetstream-enabled",
			Kind:             FactKindPorterChange,
			Scope:            "worker/runtime",
			Why:              "Worker uses jetstream(nc) + jsm.streams.add for at-least-once delivery.",
			CandidateClass:   "platform-invariant",
			CandidateSurface: "CODEBASE_KB",
		}); err != nil {
			t.Fatalf("append attesting fact: %v", err)
		}

		res := dispatch(t.Context(), store, RecipeInput{
			Action:     "record-fragment",
			Slug:       "synth-showcase",
			FragmentID: "env/0/import-comments/broker",
			Fragment:   jsBody,
		})
		if !res.OK {
			t.Fatalf("expected JetStream framing WITH attestation to pass; got refusal: %s", res.Error)
		}
	})

	t.Run("clean_body_passes_without_facts", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		store := NewStore(dir)
		if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
			t.Fatalf("OpenOrCreate: %v", err)
		}
		sess, _ := store.Get("synth-showcase")
		sess.Plan = syntheticShowcasePlan()

		res := dispatch(t.Context(), store, RecipeInput{
			Action:     "record-fragment",
			Slug:       "synth-showcase",
			FragmentID: "env/0/import-comments/broker",
			// Core pub/sub framing — no JetStream tokens.
			Fragment: "queue — single NATS instance keeps fan-out + load-balance via queue groups working through restarts.",
		})
		if !res.OK {
			t.Fatalf("clean core-pub/sub framing should pass; got refusal: %s", res.Error)
		}
	})
}

// TestEnvContentBrief_Run20C1_LoadsNATSPrinciple — pins that the
// env-content brief composer pulls in `principles/nats-shapes.md`.
// Run-19 root cause: the principle was only in the codebase-content
// composer.
func TestEnvContentBrief_Run20C1_LoadsNATSPrinciple(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "NATS messaging shapes") {
		t.Errorf("env-content brief missing nats-shapes principle heading")
	}
	if !strings.Contains(brief.Body, "core pub/sub") {
		t.Errorf("env-content brief missing core pub/sub teaching")
	}
	// The principle must explicitly forbid invoking JetStream framing
	// when the recipe doesn't open streams.
	if !strings.Contains(brief.Body, "MUST NOT invoke JetStream framing") {
		t.Errorf("env-content brief missing the don't-invoke-JetStream rule")
	}
}

// TestCodebaseContentBrief_Run20C1_LoadsNATSPrinciple — pins that the
// codebase-content brief composer also pulls in
// `principles/nats-shapes.md` (same source, both phases).
func TestCodebaseContentBrief_Run20C1_LoadsNATSPrinciple(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	cb := plan.Codebases[0]
	brief, err := BuildCodebaseContentBrief(plan, cb, nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "NATS messaging shapes") {
		t.Errorf("codebase-content brief missing nats-shapes principle heading")
	}
}
