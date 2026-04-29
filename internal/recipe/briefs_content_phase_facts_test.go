package recipe

import (
	"strings"
	"testing"
)

// Run-17 §4 Tranche 0 — porter_change + field_rationale Why is bounded by
// recording (typically 5-10 facts/codebase, 250-500 chars each); the brief
// must pass it through verbatim so the codebase-content sub-agent reads
// the full mechanism, not a 120-char truncation marker.

func TestWriteFactSummary_PorterChange_FullWhyVerbatim(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("ab ", 200) // 600 chars
	var b strings.Builder
	writeFactSummary(&b, FactRecord{
		Kind:             FactKindPorterChange,
		Topic:            "api-bind-and-trust-proxy",
		CandidateClass:   "platform-invariant",
		CandidateSurface: "CODEBASE_IG",
		Why:              long,
	})
	out := b.String()
	if !strings.Contains(out, long) {
		t.Errorf("porter_change Why truncated; expected verbatim long string in output\nwant: %q\ngot:  %q", long, out)
	}
	if strings.Contains(out, "…") {
		t.Errorf("porter_change output contains truncation marker '…': %q", out)
	}
}

func TestWriteFactSummary_FieldRationale_FullWhyVerbatim(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("xy ", 200) // 600 chars
	var b strings.Builder
	writeFactSummary(&b, FactRecord{
		Kind:      FactKindFieldRationale,
		Topic:     "api-s3-region",
		FieldPath: "run.envVariables.S3_REGION",
		Why:       long,
	})
	out := b.String()
	if !strings.Contains(out, long) {
		t.Errorf("field_rationale Why truncated; expected verbatim long string in output\nwant: %q\ngot:  %q", long, out)
	}
	if strings.Contains(out, "…") {
		t.Errorf("field_rationale output contains truncation marker '…': %q", out)
	}
}

func TestWriteFactSummary_TierDecision_StillTruncates(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("c", 200)
	var b strings.Builder
	writeFactSummary(&b, FactRecord{
		Kind:        FactKindTierDecision,
		Topic:       "minContainers-tier-4",
		Tier:        4,
		FieldPath:   "minContainers",
		ChosenValue: "2",
		TierContext: long,
	})
	out := b.String()
	if !strings.Contains(out, "…") {
		t.Errorf("tier_decision TierContext expected to truncate at 120 chars (count grows with cross-tier diffs); no '…' marker found: %q", out)
	}
}
