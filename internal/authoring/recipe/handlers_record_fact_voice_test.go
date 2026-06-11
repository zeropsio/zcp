package recipe

import (
	"strings"
	"testing"
)

// Run-34 Fix 3 — engine-side runtime gate for forbidden recipe-author
// voice tokens at record-fact. Brief edits in
// scaffold/decision_recording_slim.md and feature/decision_recording.md
// teach the rule but agents ignored at runtime: facts.jsonl carried
// 12.8% strict-token contamination unchanged from run-33 (15/117
// records carry "zerops_dev_server" in `why`; 2 carry "the agent").
// Brief teaches REQUIRED → engine MUST enforce REQUIRED; record-fact
// refuses the record when forbidden tokens appear in the load-bearing
// text fields.
//
// Diagnosed in plans/run-34-validation.md §"Untested-in-sim items".

func TestRecordFact_RejectsForbiddenTokensInWhy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		field       string // for error-message assertion
		factBuilder func(string) FactRecord
	}{
		// Each test case isolates ONE field's voice-token contamination.
		// `topic` is recipe-internal taxonomy and not subject to the
		// porter-voice rule — only the prose text fields are.
		{
			name:  "why",
			field: "why",
			factBuilder: func(token string) FactRecord {
				return FactRecord{
					Topic:            "voice-test-why",
					Kind:             FactKindPorterChange,
					Why:              "NestJS app.listen defaults to 127.0.0.1; " + token + " contaminates this why field.",
					CandidateClass:   "platform-invariant",
					CandidateSurface: "CODEBASE_KB",
				}
			},
		},
		{
			name:  "mechanism",
			field: "mechanism",
			factBuilder: func(token string) FactRecord {
				return FactRecord{
					Topic:       "voice-test-mech",
					Symptom:     "x not running",
					Mechanism:   "Generic mechanism prose with " + token + " contamination",
					SurfaceHint: "CODEBASE_KB",
					Citation:    "guide-x",
				}
			},
		},
		{
			name:  "fixApplied",
			field: "fixApplied",
			factBuilder: func(token string) FactRecord {
				return FactRecord{
					Topic:            "voice-test-fix",
					Kind:             FactKindPorterChange,
					Why:              "Clean why prose.",
					FixApplied:       "Fix prose with " + token + " contamination",
					CandidateClass:   "platform-invariant",
					CandidateSurface: "CODEBASE_KB",
				}
			},
		},
		{
			name:  "surfaceHint",
			field: "surfaceHint",
			factBuilder: func(token string) FactRecord {
				return FactRecord{
					Topic:       "voice-test-surfh",
					Symptom:     "x not running",
					Mechanism:   "Generic mechanism.",
					SurfaceHint: "CODEBASE_KB note: " + token + " contamination",
					Citation:    "guide-x",
				}
			},
		},
		{
			name:  "evidence",
			field: "evidence",
			factBuilder: func(token string) FactRecord {
				return FactRecord{
					Topic:            "voice-test-evidence",
					Kind:             FactKindPorterChange,
					Why:              "Clean why prose.",
					Evidence:         "Evidence text with " + token + " contamination",
					CandidateClass:   "platform-invariant",
					CandidateSurface: "CODEBASE_KB",
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, token := range forbiddenFactVoiceTokens {
				t.Run(token, func(t *testing.T) {
					t.Parallel()
					fact := tc.factBuilder(token)
					if err := validateFactVoiceTokens(fact); err == nil {
						t.Errorf("validateFactVoiceTokens accepted forbidden token %q in field %s; expected refusal",
							token, tc.field)
					} else {
						msg := err.Error()
						if !strings.Contains(msg, token) {
							t.Errorf("error message must name the offending token %q; got %q", token, msg)
						}
						if !strings.Contains(msg, tc.field) {
							t.Errorf("error message must name the offending field %q; got %q", tc.field, msg)
						}
					}
				})
			}
		})
	}
}

func TestRecordFact_AcceptsCleanWhy(t *testing.T) {
	t.Parallel()
	clean := []FactRecord{
		{
			Topic:            "clean-1",
			Kind:             FactKindPorterChange,
			Why:              "NestJS app.listen(port) defaults to 127.0.0.1; the L7 balancer needs 0.0.0.0.",
			CandidateClass:   "platform-invariant",
			CandidateSurface: "CODEBASE_KB",
		},
		{
			Topic:       "clean-2",
			Symptom:     "Naked nc.subscribe causes uneven distribution",
			Mechanism:   "Without queue groups, every subscriber gets every message.",
			SurfaceHint: "CODEBASE_KB",
			Citation:    "nats-queue-groups",
		},
		{
			Topic:            "clean-3",
			Kind:             FactKindPorterChange,
			Why:              "Vite dev server requires --host 0.0.0.0 plus allowed-hosts whitelist.",
			FixApplied:       "Set vite.config.ts: server.host = '0.0.0.0', server.allowedHosts.",
			Evidence:         "Browser at zerops subdomain returns 200 with Vite HMR socket open.",
			CandidateClass:   "platform-invariant",
			CandidateSurface: "CODEBASE_IG",
		},
	}
	for _, f := range clean {
		t.Run(f.Topic, func(t *testing.T) {
			t.Parallel()
			if err := validateFactVoiceTokens(f); err != nil {
				t.Errorf("clean fact rejected by voice gate: %s\nfact: %+v", err, f)
			}
		})
	}
}

// TestRecordFact_DispatchRejectsForbiddenTokens — full dispatch-level
// pin that record-fact path errors out (r.Error != "" + r.OK == false)
// when the fact carries forbidden voice tokens. Without this, the
// engine accepts the contaminated fact and a downstream notice is the
// only signal — agents have empirically ignored notices at runtime
// (run-34 evidence: 12.8% contamination rate UNCHANGED from run-33).
func TestRecordFact_DispatchRejectsForbiddenTokens(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	sess, err := store.OpenOrCreate("recipe-fix3-dispatch", t.TempDir())
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess.Plan = &Plan{Slug: "recipe-fix3-dispatch"}

	in := RecipeInput{
		Action: "record-fact",
		Slug:   "recipe-fix3-dispatch",
		Fact: &FactRecord{
			Topic:            "dispatch-rejection-test",
			Kind:             FactKindPorterChange,
			Why:              "Dev server is owned by zerops_dev_server which then runs nest --watch.",
			CandidateClass:   "platform-invariant",
			CandidateSurface: "CODEBASE_KB",
		},
	}
	r := dispatch(t.Context(), store, in)
	if r.OK {
		t.Errorf("dispatch returned OK on forbidden-token fact; expected refusal")
	}
	if r.Error == "" {
		t.Error("dispatch did not populate Error on forbidden-token fact")
	} else if !strings.Contains(r.Error, "zerops_dev_server") {
		t.Errorf("dispatch error must name the offending token; got %q", r.Error)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}
