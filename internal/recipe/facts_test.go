package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFactRecord_RequiredFields(t *testing.T) {
	t.Parallel()

	// Facts are structured at record time, not classified at consume time
	// (plan §5 P4). Records missing any required field are rejected at
	// Append — the writer never sees a half-typed fact.
	required := []string{"topic", "symptom", "mechanism", "surfaceHint", "citation"}

	for _, missing := range required {
		t.Run("missing="+missing, func(t *testing.T) {
			t.Parallel()
			f := FactRecord{
				Topic:       "cross-service-env-autoinject",
				Symptom:     "503 on deploy",
				Mechanism:   "self-shadow trap",
				SurfaceHint: "platform-trap",
				Citation:    "env-var-model",
			}
			switch missing {
			case "topic":
				f.Topic = ""
			case "symptom":
				f.Symptom = ""
			case "mechanism":
				f.Mechanism = ""
			case "surfaceHint":
				f.SurfaceHint = ""
			case "citation":
				f.Citation = ""
			}
			if err := f.Validate(); err == nil {
				t.Errorf("expected Validate() error for missing %q", missing)
			}
		})
	}
}

func TestFactsLog_AppendRejectsIncomplete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	// Missing citation — must be rejected at Append (not silently written).
	incomplete := FactRecord{
		Topic: "x", Symptom: "y", Mechanism: "z",
		SurfaceHint: "platform-trap",
	}
	if err := log.Append(incomplete); err == nil {
		t.Fatal("Append(incomplete) should have errored")
	}

	records, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records in log after rejected Append, got %d", len(records))
	}
}

func TestFactsLog_AppendAndRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	records := []FactRecord{
		{
			Topic: "cross-service-env-autoinject", Symptom: "503", Mechanism: "shadow",
			SurfaceHint: "platform-trap", Citation: "env-var-model", Author: "scaffold",
		},
		{
			Topic: "rolling-deploys", Symptom: "dropped traffic", Mechanism: "SIGTERM",
			SurfaceHint: "porter-change", Citation: "rolling-deploys", Author: "scaffold",
		},
	}
	for _, r := range records {
		if err := log.Append(r); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(records) {
		t.Fatalf("Read: len = %d, want %d", len(got), len(records))
	}
	for i := range records {
		if got[i].Topic != records[i].Topic {
			t.Errorf("record %d: Topic = %q, want %q", i, got[i].Topic, records[i].Topic)
		}
		if got[i].RecordedAt == "" {
			t.Errorf("record %d: RecordedAt should be auto-filled", i)
		}
	}
}

// TestFactRecord_AcceptsEnrichedFields — run-11 gap U-2. v3 schema now
// carries failureMode/fixApplied/evidence/scope (the v2 shape that ran-10
// agents reached for naturally). Fields round-trip through facts.jsonl
// and surface to V-1's classifier so self-inflicted shape can be
// detected from fixApplied + failureMode.
func TestFactRecord_AcceptsEnrichedFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	rec := FactRecord{
		Topic:       "deployignore-filters-build-artifact",
		Symptom:     "Cannot find module /var/www/dist/main.js looping every 2s",
		Mechanism:   "deployignore filters the deploy bundle, not just the source upload",
		SurfaceHint: "platform-trap",
		Citation:    "deploy-files",
		FailureMode: "Cannot find module /var/www/dist/main.js",
		FixApplied:  "removed dist from .deployignore",
		Evidence:    "deploy log line 12:35; runtime log loop 12:36-12:55",
		Scope:       "content",
	}
	if err := log.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0].FailureMode != rec.FailureMode {
		t.Errorf("FailureMode round-trip: got %q, want %q", got[0].FailureMode, rec.FailureMode)
	}
	if got[0].FixApplied != rec.FixApplied {
		t.Errorf("FixApplied round-trip: got %q, want %q", got[0].FixApplied, rec.FixApplied)
	}
	if got[0].Evidence != rec.Evidence {
		t.Errorf("Evidence round-trip: got %q, want %q", got[0].Evidence, rec.Evidence)
	}
	if got[0].Scope != rec.Scope {
		t.Errorf("Scope round-trip: got %q, want %q", got[0].Scope, rec.Scope)
	}
}

// TestFactRecord_OptionalEnrichedFields — backward-compat: callers that
// don't set the v3 enriched fields still validate + persist correctly.
// No existing call breaks.
func TestFactRecord_OptionalEnrichedFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	rec := FactRecord{
		Topic:       "cross-service-env-autoinject",
		Symptom:     "503 on first deploy",
		Mechanism:   "self-shadow trap",
		SurfaceHint: "platform-trap",
		Citation:    "env-var-model",
	}
	if err := log.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0].FailureMode != "" || got[0].FixApplied != "" || got[0].Evidence != "" {
		t.Errorf("enriched fields should be empty when not set: %+v", got[0])
	}
}

func TestFactsLog_FilterByHint(t *testing.T) {
	t.Parallel()

	all := []FactRecord{
		{Topic: "a", Symptom: "s", Mechanism: "m", SurfaceHint: "platform-trap", Citation: "c"},
		{Topic: "b", Symptom: "s", Mechanism: "m", SurfaceHint: "porter-change", Citation: "c"},
		{Topic: "c", Symptom: "s", Mechanism: "m", SurfaceHint: "platform-trap", Citation: "c"},
	}

	filtered := FilterByHint(all, "platform-trap")
	if len(filtered) != 2 {
		t.Fatalf("FilterByHint: len = %d, want 2", len(filtered))
	}
	for _, r := range filtered {
		if r.SurfaceHint != "platform-trap" {
			t.Errorf("FilterByHint returned record with hint %q", r.SurfaceHint)
		}
	}
}

// Run-16 §5.5 / §5.6 — Kind discriminator + per-Kind validation. The
// legacy Kind="" path (TestFactRecord_RequiredFields) keeps the
// platform-trap shape; new Kind values carry their own required-field
// set.

func TestFactRecord_Validate_PorterChange_RequiresFields(t *testing.T) {
	t.Parallel()

	full := FactRecord{
		Topic:            "apidev-cors-expose-x-cache",
		Kind:             FactKindPorterChange,
		Why:              "Browsers strip non-CORS-safelisted response headers cross-origin.",
		CandidateClass:   "intersection",
		CandidateSurface: "CODEBASE_KB",
	}
	if err := full.Validate(); err != nil {
		t.Fatalf("full porter_change should validate: %v", err)
	}

	cases := []struct {
		field string
		mut   func(*FactRecord)
	}{
		{"topic", func(f *FactRecord) { f.Topic = "" }},
		{"why", func(f *FactRecord) { f.Why = "" }},
		{"candidateClass", func(f *FactRecord) { f.CandidateClass = "" }},
		{"candidateSurface", func(f *FactRecord) { f.CandidateSurface = "" }},
	}
	for _, tc := range cases {
		t.Run("missing="+tc.field, func(t *testing.T) {
			t.Parallel()
			f := full
			tc.mut(&f)
			if err := f.Validate(); err == nil {
				t.Errorf("expected error for missing %q", tc.field)
			}
		})
	}
}

func TestFactRecord_Validate_PorterChange_EngineEmittedShellExempt(t *testing.T) {
	t.Parallel()

	// Engine-emitted shells (§7.2) carry empty Why + Heading; the agent
	// fills them via fill-fact-slot. Validate must NOT reject them.
	shell := FactRecord{
		Topic:            "apidev-connect-db",
		Kind:             FactKindPorterChange,
		EngineEmitted:    true,
		CandidateClass:   "intersection",
		CandidateSurface: "CODEBASE_IG",
		CitationGuide:    "managed-services-postgresql",
	}
	if err := shell.Validate(); err != nil {
		t.Errorf("engine-emitted shell should validate without Why: %v", err)
	}
}

func TestFactRecord_Validate_FieldRationale_RequiresFields(t *testing.T) {
	t.Parallel()

	full := FactRecord{
		Topic:     "apidev-s3-region-us-east-1",
		Kind:      FactKindFieldRationale,
		FieldPath: "run.envVariables.S3_REGION",
		Why:       "us-east-1 is the only region MinIO accepts.",
	}
	if err := full.Validate(); err != nil {
		t.Fatalf("full field_rationale should validate: %v", err)
	}

	cases := []struct {
		field string
		mut   func(*FactRecord)
	}{
		{"topic", func(f *FactRecord) { f.Topic = "" }},
		{"fieldPath", func(f *FactRecord) { f.FieldPath = "" }},
		{"why", func(f *FactRecord) { f.Why = "" }},
	}
	for _, tc := range cases {
		t.Run("missing="+tc.field, func(t *testing.T) {
			t.Parallel()
			f := full
			tc.mut(&f)
			if err := f.Validate(); err == nil {
				t.Errorf("expected error for missing %q", tc.field)
			}
		})
	}
}

func TestFactRecord_Validate_TierDecision_RequiresFields(t *testing.T) {
	t.Parallel()

	full := FactRecord{
		Topic:       "tier-4-db-non-ha",
		Kind:        FactKindTierDecision,
		Tier:        4,
		FieldPath:   "services[name=db].mode",
		ChosenValue: "NON_HA",
	}
	if err := full.Validate(); err != nil {
		t.Fatalf("full tier_decision should validate: %v", err)
	}

	cases := []struct {
		field string
		mut   func(*FactRecord)
	}{
		{"topic", func(f *FactRecord) { f.Topic = "" }},
		// Run-16 reviewer D-1 — Tier 0 (AI Agent) is a real, valid tier;
		// validate against bounds [0..5] instead of treating 0 as unset.
		{"tier-out-of-range-negative", func(f *FactRecord) { f.Tier = -1 }},
		{"tier-out-of-range-too-high", func(f *FactRecord) { f.Tier = 6 }},
		{"fieldPath", func(f *FactRecord) { f.FieldPath = "" }},
		{"chosenValue", func(f *FactRecord) { f.ChosenValue = "" }},
	}
	for _, tc := range cases {
		t.Run("missing="+tc.field, func(t *testing.T) {
			t.Parallel()
			f := full
			tc.mut(&f)
			if err := f.Validate(); err == nil {
				t.Errorf("expected error for case %q", tc.field)
			}
		})
	}
}

func TestFactRecord_Validate_TierDecision_TierZero_Accepted(t *testing.T) {
	t.Parallel()
	// Run-16 reviewer D-1 — Tier 0 is the AI Agent tier; legitimate
	// tier_decision facts can scope to it (research-phase agent records
	// "tier 0 declares NON_HA across the board"). The pre-fix validator
	// rejected this on Go's int zero-value collision with "unset".
	f := FactRecord{
		Topic:       "tier-0-baseline",
		Kind:        FactKindTierDecision,
		Tier:        0,
		FieldPath:   "services[name=db].mode",
		ChosenValue: "NON_HA",
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Tier 0 (AI Agent) should be a valid tier; validator rejected it: %v", err)
	}
}

func TestFactRecord_Validate_Contract_RequiresFields(t *testing.T) {
	t.Parallel()

	full := FactRecord{
		Topic:       "nats-items-created-contract",
		Kind:        FactKindContract,
		Publishers:  []string{"api"},
		Subscribers: []string{"worker"},
		Subject:     "items.created",
		Purpose:     "Worker mirrors items into Meilisearch on create",
	}
	if err := full.Validate(); err != nil {
		t.Fatalf("full contract should validate: %v", err)
	}

	cases := []struct {
		field string
		mut   func(*FactRecord)
	}{
		{"topic", func(f *FactRecord) { f.Topic = "" }},
		{"publishers", func(f *FactRecord) { f.Publishers = nil }},
		{"subscribers", func(f *FactRecord) { f.Subscribers = nil }},
		{"subject", func(f *FactRecord) { f.Subject = "" }},
		{"purpose", func(f *FactRecord) { f.Purpose = "" }},
	}
	for _, tc := range cases {
		t.Run("missing="+tc.field, func(t *testing.T) {
			t.Parallel()
			f := full
			tc.mut(&f)
			if err := f.Validate(); err == nil {
				t.Errorf("expected error for missing %q", tc.field)
			}
		})
	}
}

func TestFactRecord_Validate_PlatformTrap_BackCompat(t *testing.T) {
	t.Parallel()

	// Legacy Kind="" record validates exactly as before — the discriminator
	// addition must not regress existing platform-trap consumers.
	rec := FactRecord{
		Topic:       "cross-service-env-autoinject",
		Symptom:     "503 on first deploy",
		Mechanism:   "self-shadow trap",
		SurfaceHint: "platform-trap",
		Citation:    "env-var-model",
	}
	if err := rec.Validate(); err != nil {
		t.Errorf("legacy Kind=\"\" platform-trap should still validate: %v", err)
	}

	rec.Symptom = ""
	if err := rec.Validate(); err == nil {
		t.Error("legacy Kind=\"\" without Symptom should still error")
	}
}

func TestFactRecord_Validate_UnknownKind_Rejects(t *testing.T) {
	t.Parallel()

	rec := FactRecord{Topic: "x", Kind: "made_up_kind"}
	if err := rec.Validate(); err == nil {
		t.Error("unknown Kind should be rejected")
	}
}

func TestFactsLog_FilterByKind_ReturnsMatchingSubset(t *testing.T) {
	t.Parallel()

	all := []FactRecord{
		{Topic: "a", Kind: FactKindPorterChange, Why: "w", CandidateClass: "platform-invariant", CandidateSurface: "CODEBASE_IG"},
		{Topic: "b", Kind: FactKindFieldRationale, FieldPath: "run", Why: "w"},
		{Topic: "c", Kind: FactKindPorterChange, Why: "w", CandidateClass: "intersection", CandidateSurface: "CODEBASE_KB"},
		{Topic: "d", Symptom: "s", Mechanism: "m", SurfaceHint: "platform-trap", Citation: "c"},
	}

	pcs := FilterByKind(all, FactKindPorterChange)
	if len(pcs) != 2 {
		t.Fatalf("FilterByKind(porter_change): len = %d, want 2", len(pcs))
	}
	for _, r := range pcs {
		if r.Kind != FactKindPorterChange {
			t.Errorf("FilterByKind returned record with kind %q", r.Kind)
		}
	}

	frs := FilterByKind(all, FactKindFieldRationale)
	if len(frs) != 1 || frs[0].Topic != "b" {
		t.Errorf("FilterByKind(field_rationale) wrong subset: %+v", frs)
	}

	legacy := FilterByKind(all, "")
	if len(legacy) != 1 || legacy[0].Topic != "d" {
		t.Errorf("FilterByKind(\"\") should return only the Kind=\"\" record: %+v", legacy)
	}
}

func TestFactsLog_FilterByCodebase_ReturnsMatchingSubset(t *testing.T) {
	t.Parallel()

	all := []FactRecord{
		{Topic: "a", Kind: FactKindPorterChange, Scope: "apidev/code/src/main.ts", Why: "w", CandidateClass: "intersection", CandidateSurface: "CODEBASE_KB"},
		{Topic: "b", Kind: FactKindFieldRationale, Scope: "apidev/zerops.yaml/run.envVariables.S3_REGION", FieldPath: "run.envVariables.S3_REGION", Why: "w"},
		{Topic: "c", Kind: FactKindPorterChange, Scope: "appdev/code/src/proxy.ts", Why: "w", CandidateClass: "intersection", CandidateSurface: "CODEBASE_KB"},
		{Topic: "d", Kind: FactKindFieldRationale, Scope: "apidev", FieldPath: "run", Why: "w"},
	}

	apidev := FilterByCodebase(all, "apidev")
	if len(apidev) != 3 {
		t.Fatalf("FilterByCodebase(apidev): len = %d, want 3", len(apidev))
	}
	for _, r := range apidev {
		if r.Scope != "apidev" && !strings.HasPrefix(r.Scope, "apidev/") {
			t.Errorf("FilterByCodebase returned record with scope %q", r.Scope)
		}
	}

	appdev := FilterByCodebase(all, "appdev")
	if len(appdev) != 1 || appdev[0].Topic != "c" {
		t.Errorf("FilterByCodebase(appdev) wrong subset: %+v", appdev)
	}

	if got := FilterByCodebase(all, ""); got != nil {
		t.Errorf("FilterByCodebase(\"\") should return nil, got %v", got)
	}
}

func TestFactsLog_RoundTrip_NewKindRecords(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	records := []FactRecord{
		{
			Topic:            "apidev-cors-expose-x-cache",
			Kind:             FactKindPorterChange,
			Phase:            "feature",
			ChangeKind:       "code-addition",
			Library:          "@nestjs/common",
			Diff:             "app.enableCors({ exposedHeaders: ['X-Cache'] });",
			Why:              "Browsers strip non-CORS-safelisted response headers cross-origin.",
			CandidateClass:   "intersection",
			CandidateHeading: "Custom response headers across origins",
			CandidateSurface: "CODEBASE_KB",
			CitationGuide:    "http-support",
			Scope:            "apidev/code/src/main.ts",
		},
		{
			Topic:      "apidev-s3-region-us-east-1",
			Kind:       FactKindFieldRationale,
			Phase:      "scaffold",
			FieldPath:  "run.envVariables.S3_REGION",
			FieldValue: "us-east-1",
			Why:        "us-east-1 is the only region MinIO accepts.",
			Scope:      "apidev/zerops.yaml/run.envVariables.S3_REGION",
		},
		{
			Topic:       "tier-4-db-non-ha",
			Kind:        FactKindTierDecision,
			Tier:        4,
			Service:     "db",
			FieldPath:   "services[name=db].mode",
			ChosenValue: "NON_HA",
			TierContext: "Tier 4 audience: small-prod single-region.",
			Scope:       "env/4/services.db",
		},
		{
			Topic:       "nats-items-created-contract",
			Kind:        FactKindContract,
			Publishers:  []string{"api"},
			Subscribers: []string{"worker"},
			Subject:     "items.created",
			Purpose:     "Worker mirrors items into Meilisearch on create",
		},
	}

	for _, r := range records {
		if err := log.Append(r); err != nil {
			t.Fatalf("Append %s: %v", r.Topic, err)
		}
	}

	got, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(records) {
		t.Fatalf("Read: len = %d, want %d", len(got), len(records))
	}
	for i, want := range records {
		if got[i].Topic != want.Topic {
			t.Errorf("record %d: Topic = %q, want %q", i, got[i].Topic, want.Topic)
		}
		if got[i].Kind != want.Kind {
			t.Errorf("record %d: Kind = %q, want %q", i, got[i].Kind, want.Kind)
		}
	}

	if got[0].Diff != records[0].Diff {
		t.Errorf("PorterChange Diff round-trip: got %q, want %q", got[0].Diff, records[0].Diff)
	}
	if got[1].FieldPath != records[1].FieldPath {
		t.Errorf("FieldRationale FieldPath round-trip: got %q, want %q", got[1].FieldPath, records[1].FieldPath)
	}
	if got[2].Tier != records[2].Tier {
		t.Errorf("TierDecision Tier round-trip: got %d, want %d", got[2].Tier, records[2].Tier)
	}
	if len(got[3].Publishers) != 1 || got[3].Publishers[0] != "api" {
		t.Errorf("Contract Publishers round-trip: got %v", got[3].Publishers)
	}
}

func TestFactsLog_ReplaceByTopic_RewritesMatchingRecord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	shell := FactRecord{
		Topic:            "apidev-connect-db",
		Kind:             FactKindPorterChange,
		EngineEmitted:    true,
		CandidateClass:   "intersection",
		CandidateSurface: "CODEBASE_IG",
		CitationGuide:    "managed-services-postgresql",
	}
	other := FactRecord{
		Topic:            "apidev-bind-and-trust-proxy",
		Kind:             FactKindPorterChange,
		EngineEmitted:    true,
		CandidateClass:   "platform-invariant",
		CandidateSurface: "CODEBASE_IG",
		Why:              "Default bind to 127.0.0.1 is unreachable.",
	}
	if err := log.Append(shell); err != nil {
		t.Fatalf("Append shell: %v", err)
	}
	if err := log.Append(other); err != nil {
		t.Fatalf("Append other: %v", err)
	}

	merged := shell
	merged.EngineEmitted = false
	merged.Why = "Postgres credentials live on db_hostname / db_user / db_password aliases via own-key projection."
	merged.CandidateHeading = "Connect to PostgreSQL via own-key aliases"
	merged.Library = "@nestjs/typeorm"
	if err := log.ReplaceByTopic(merged); err != nil {
		t.Fatalf("ReplaceByTopic: %v", err)
	}

	got, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (replace, not append)", len(got))
	}

	var apidevConnect *FactRecord
	for i := range got {
		if got[i].Topic == "apidev-connect-db" {
			apidevConnect = &got[i]
			break
		}
	}
	if apidevConnect == nil {
		t.Fatal("apidev-connect-db record missing after replace")
	}
	if apidevConnect.Why != merged.Why {
		t.Errorf("merged Why not preserved: got %q", apidevConnect.Why)
	}
	if apidevConnect.CandidateHeading != merged.CandidateHeading {
		t.Errorf("merged CandidateHeading not preserved: got %q", apidevConnect.CandidateHeading)
	}
	if apidevConnect.EngineEmitted {
		t.Error("EngineEmitted should flip to false after fill")
	}

	// Other record must be untouched.
	for _, r := range got {
		if r.Topic == "apidev-bind-and-trust-proxy" && r.Why != other.Why {
			t.Errorf("non-target record was clobbered: Why=%q", r.Why)
		}
	}
}

func TestFactsLog_ReplaceByTopic_RejectsMissingTopic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	shell := FactRecord{
		Topic:            "exists",
		Kind:             FactKindPorterChange,
		Why:              "...",
		CandidateClass:   "platform-invariant",
		CandidateSurface: "CODEBASE_IG",
	}
	if err := log.Append(shell); err != nil {
		t.Fatalf("Append: %v", err)
	}

	missing := FactRecord{
		Topic:            "does-not-exist",
		Kind:             FactKindPorterChange,
		Why:              "...",
		CandidateClass:   "platform-invariant",
		CandidateSurface: "CODEBASE_IG",
	}
	if err := log.ReplaceByTopic(missing); err == nil {
		t.Error("ReplaceByTopic with missing topic should error")
	}
}

func TestClassifyWithNotice_NewKind_EarlyReturn(t *testing.T) {
	t.Parallel()

	// A porter_change fact (Kind != "") must NOT be run through V-1's
	// platform-trap classifier — its CandidateClass slot is the
	// authoritative classification, and Symptom/Mechanism/SurfaceHint are
	// empty by design. Reaching the classifier on this shape would crash
	// or auto-discard an otherwise-publishable record.
	rec := FactRecord{
		Topic:            "apidev-cors-expose-x-cache",
		Kind:             FactKindPorterChange,
		Why:              "...",
		CandidateClass:   "intersection",
		CandidateSurface: "CODEBASE_KB",
	}
	class, notice := ClassifyWithNotice(rec)
	if class != "" || notice != "" {
		t.Errorf("ClassifyWithNotice on Kind != \"\" should early-return; got class=%q notice=%q", class, notice)
	}

	// Legacy Kind="" still goes through the classifier as before.
	legacy := FactRecord{
		Topic: "x", Symptom: "s", Mechanism: "m", SurfaceHint: "platform-trap", Citation: "c",
	}
	if _, notice := ClassifyWithNotice(legacy); notice != "" {
		t.Errorf("Kind=\"\" platform-trap should not produce a notice when classifying clean: notice=%q", notice)
	}
}
