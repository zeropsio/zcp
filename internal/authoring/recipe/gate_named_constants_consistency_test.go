package recipe

import (
	"strings"
	"testing"
)

// Run-40 A1 — gateNamedConstantsConsistency refuses env-content
// close when a tier yaml import-comment cites a named-constant value
// that contradicts plan.NamedConstants. Closes S1-1 (queue-group
// drift) as a defect class.

// TestGateNamedConstantsConsistency_StaleQueueGroupCitation_Refuses
// pins the canonical run-39 failure mode: source code uses
// `'workers'` (recorded as plan.NamedConstants["NATS_QUEUE_GROUP"]),
// but the tier yaml import-comment cites `showcase-workers`. The
// gate must refuse.
func TestGateNamedConstantsConsistency_StaleQueueGroupCitation_Refuses(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		NamedConstants: map[string]string{
			"NATS_QUEUE_GROUP": "workers",
		},
		EnvComments: map[string]EnvComments{
			"4": {
				Service: map[string]string{
					"worker": "Run two worker replicas under queue group `showcase-workers` so cluster work fans out evenly. NATS_QUEUE_GROUP controls the name.",
				},
			},
		},
	}

	v := gateNamedConstantsConsistency(GateContext{Plan: plan})
	if len(v) != 1 {
		t.Fatalf("expected one violation; got %d: %+v", len(v), v)
	}
	if v[0].Code != "named-constant-stale-citation" {
		t.Errorf("code = %q, want named-constant-stale-citation", v[0].Code)
	}
	if v[0].Severity != SeverityBlocking {
		t.Errorf("severity = %v, want Blocking", v[0].Severity)
	}
	if !strings.Contains(v[0].Message, `"showcase-workers"`) {
		t.Errorf("message should cite the stale value; got %q", v[0].Message)
	}
	if !strings.Contains(v[0].Message, `"workers"`) {
		t.Errorf("message should cite the canonical value; got %q", v[0].Message)
	}
}

// TestGateNamedConstantsConsistency_CanonicalCitation_NoViolation —
// when the import-comment already cites the canonical value, the
// gate passes.
func TestGateNamedConstantsConsistency_CanonicalCitation_NoViolation(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		NamedConstants: map[string]string{
			"NATS_QUEUE_GROUP": "workers",
		},
		EnvComments: map[string]EnvComments{
			"4": {
				Service: map[string]string{
					"worker": "Run two worker replicas under queue group `workers` so jobs.demo fans out evenly. NATS_QUEUE_GROUP controls the name.",
				},
			},
		},
	}
	if v := gateNamedConstantsConsistency(GateContext{Plan: plan}); len(v) != 0 {
		t.Errorf("expected no violations on canonical citation; got %+v", v)
	}
}

// TestGateNamedConstantsConsistency_EmptyNamedConstants_NoViolation
// — the gate is a no-op when the plan has no named constants. Pinned
// so a regression that mass-emits violations on every empty plan
// surfaces immediately.
func TestGateNamedConstantsConsistency_EmptyNamedConstants_NoViolation(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "synth-showcase"}
	if v := gateNamedConstantsConsistency(GateContext{Plan: plan}); len(v) != 0 {
		t.Errorf("expected no violations on empty NamedConstants; got %+v", v)
	}
}

// TestGateNamedConstantsConsistency_DistantCitation_NotFlagged —
// prose mention of a backtick-quoted value that's NOT within the
// proximity window of the named-constant KEY doesn't trip the gate.
// Catches over-eager regex regressions.
func TestGateNamedConstantsConsistency_DistantCitation_NotFlagged(t *testing.T) {
	t.Parallel()
	// 200+ chars of prose between the stale citation and the KEY
	// mention; well beyond the 120-char proximity window.
	body := "Historical aside about `showcase-workers` — a name we considered briefly before standardizing. " +
		strings.Repeat("Filler prose that doesn't mention the named constant. ", 5) +
		"In the current recipe the NATS_QUEUE_GROUP variable controls the production value."
	plan := &Plan{
		Slug:           "synth-showcase",
		NamedConstants: map[string]string{"NATS_QUEUE_GROUP": "workers"},
		EnvComments: map[string]EnvComments{
			"0": {Service: map[string]string{"worker": body}},
		},
	}
	if v := gateNamedConstantsConsistency(GateContext{Plan: plan}); len(v) != 0 {
		t.Errorf("citation outside proximity window should not flag; got %+v", v)
	}
}

// TestGateNamedConstantsConsistency_MultipleStaleValuesAcrossTiers —
// the gate produces one violation per (tier, target, stale value).
// Run-39 had stale citations across tier 0 AND tier 4; both must
// surface in one round-trip.
func TestGateNamedConstantsConsistency_MultipleStaleValuesAcrossTiers(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:           "synth-showcase",
		NamedConstants: map[string]string{"NATS_QUEUE_GROUP": "workers"},
		EnvComments: map[string]EnvComments{
			"0": {Service: map[string]string{
				"worker": "Single-replica tier; the queue group `showcase-workers` still applies as NATS_QUEUE_GROUP.",
			}},
			"4": {Service: map[string]string{
				"worker": "Two replicas under queue group `showcase-workers`; NATS_QUEUE_GROUP names the group.",
			}},
		},
	}
	v := gateNamedConstantsConsistency(GateContext{Plan: plan})
	if len(v) != 2 {
		t.Fatalf("expected 2 violations (tier 0 + tier 4); got %d: %+v", len(v), v)
	}
	tiers := map[string]bool{}
	for _, x := range v {
		if strings.Contains(x.Path, "env/0/import-comments/worker") {
			tiers["0"] = true
		}
		if strings.Contains(x.Path, "env/4/import-comments/worker") {
			tiers["4"] = true
		}
	}
	if !tiers["0"] || !tiers["4"] {
		t.Errorf("missing tier coverage in violations; saw %v", tiers)
	}
}

// TestGateNamedConstantsConsistency_ProjectScopeAlsoChecked — the
// gate scans EnvComments[].Project (not just .Service[<host>]) so a
// project-scope comment can't smuggle a stale citation past the
// per-host check.
func TestGateNamedConstantsConsistency_ProjectScopeAlsoChecked(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:           "synth-showcase",
		NamedConstants: map[string]string{"NATS_QUEUE_GROUP": "workers"},
		EnvComments: map[string]EnvComments{
			"0": {Project: "Tier 0 has one worker; NATS_QUEUE_GROUP names the queue group as `showcase-workers`."},
		},
	}
	v := gateNamedConstantsConsistency(GateContext{Plan: plan})
	if len(v) != 1 {
		t.Fatalf("expected 1 project-scope violation; got %d: %+v", len(v), v)
	}
	if !strings.Contains(v[0].Path, "env/0/import-comments/project") {
		t.Errorf("path should name project scope; got %q", v[0].Path)
	}
}

// TestEnvGates_RegistersNamedConstantsConsistency pins the gate
// wiring: env-content complete-phase MUST include the named-constants
// consistency check.
func TestEnvGates_RegistersNamedConstantsConsistency(t *testing.T) {
	t.Parallel()
	gates := EnvGates()
	mustHaveGate(t, gates, "named-constants-consistency")
}

// TestMergePlan_NamedConstants_Compose pins the update-plan merge
// semantics for the new field: entries from incoming overlay onto cur
// without clearing existing keys (matches Fragments behavior).
func TestMergePlan_NamedConstants_Compose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sess := &Session{
		Slug:       "synth-showcase",
		OutputRoot: dir,
		Plan: &Plan{
			Slug:           "synth-showcase",
			NamedConstants: map[string]string{"NATS_QUEUE_GROUP": "workers"},
		},
	}
	err := mergePlan(sess, &Plan{NamedConstants: map[string]string{"CACHE_PREFIX": "ns0"}})
	if err != nil {
		t.Fatalf("mergePlan: %v", err)
	}
	if got := sess.Plan.NamedConstants["NATS_QUEUE_GROUP"]; got != "workers" {
		t.Errorf("existing NATS_QUEUE_GROUP lost on merge; got %q", got)
	}
	if got := sess.Plan.NamedConstants["CACHE_PREFIX"]; got != "ns0" {
		t.Errorf("incoming CACHE_PREFIX missing after merge; got %q", got)
	}
}

// TestMergePlan_NamedConstants_ConflictRefuses pins run-40 fix-up #7:
// when an update-plan call tries to overwrite an existing
// NamedConstants entry with a different value, mergePlan refuses
// with a surface-able error naming the conflict. Pre-fix the merge
// was a plain maps.Copy that silently overwrote — the run-39 queue-
// group rename ("showcase-workers" → "workers") would have been a
// silent-overwrite shape, masking the cross-surface drift the gate
// is meant to catch.
func TestMergePlan_NamedConstants_ConflictRefuses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sess := &Session{
		Slug:       "synth-showcase",
		OutputRoot: dir,
		Plan: &Plan{
			Slug:           "synth-showcase",
			NamedConstants: map[string]string{"NATS_QUEUE_GROUP": "showcase-workers"},
		},
	}
	err := mergePlan(sess, &Plan{NamedConstants: map[string]string{"NATS_QUEUE_GROUP": "workers"}})
	if err == nil {
		t.Fatal("expected merge to refuse on value conflict; got nil")
	}
	if !strings.Contains(err.Error(), "NATS_QUEUE_GROUP") {
		t.Errorf("error should name the conflicting key; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "showcase-workers") || !strings.Contains(err.Error(), `"workers"`) {
		t.Errorf("error should surface both existing and incoming values; got %q", err.Error())
	}
	// In-memory state must NOT have been mutated (refusal protects
	// the prior canonical record).
	if got := sess.Plan.NamedConstants["NATS_QUEUE_GROUP"]; got != "showcase-workers" {
		t.Errorf("conflict merge mutated state despite refusal; got %q", got)
	}
}

// TestMergePlan_NamedConstants_IdempotentSameValuePasses — an
// incoming entry with the same value as the existing record is a
// no-op, not a conflict. Catches over-eager conflict detection.
func TestMergePlan_NamedConstants_IdempotentSameValuePasses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sess := &Session{
		Slug:       "synth-showcase",
		OutputRoot: dir,
		Plan: &Plan{
			Slug:           "synth-showcase",
			NamedConstants: map[string]string{"NATS_QUEUE_GROUP": "workers"},
		},
	}
	if err := mergePlan(sess, &Plan{NamedConstants: map[string]string{"NATS_QUEUE_GROUP": "workers"}}); err != nil {
		t.Errorf("idempotent same-value merge should pass; got %v", err)
	}
}

// TestBuildEnvContentBrief_CarriesNamedConstantsSection pins the
// brief-render integration: when the plan carries NamedConstants the
// env-content brief includes a `## Named constants` section with the
// KEY=value pairings.
func TestBuildEnvContentBrief_CarriesNamedConstantsSection(t *testing.T) {
	t.Parallel()
	plan := showcaseSlugWithChainParent()
	plan.NamedConstants = map[string]string{
		"NATS_QUEUE_GROUP": "workers",
		"CACHE_PREFIX":     "ns0",
	}
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "## Named constants") {
		t.Errorf("brief missing Named constants section")
	}
	if !strings.Contains(brief.Body, "`NATS_QUEUE_GROUP` = `workers`") {
		t.Errorf("brief missing NATS_QUEUE_GROUP=workers entry; got tail:\n%s", briefTail(brief.Body, 600))
	}
	if !strings.Contains(brief.Body, "`CACHE_PREFIX` = `ns0`") {
		t.Errorf("brief missing CACHE_PREFIX=ns0 entry")
	}
}

// TestBuildEnvContentBrief_OmitsNamedConstantsSection_WhenEmpty —
// suppress the section when plan.NamedConstants is empty. Pinned to
// catch a regression that would emit an empty placeholder section.
func TestBuildEnvContentBrief_OmitsNamedConstantsSection_WhenEmpty(t *testing.T) {
	t.Parallel()
	plan := showcaseSlugWithChainParent()
	plan.NamedConstants = nil
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if strings.Contains(brief.Body, "## Named constants") {
		t.Errorf("brief should not include Named constants section when map is empty")
	}
}
