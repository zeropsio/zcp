package recipe

import (
	"strings"
	"testing"
)

// Run-17 §7 — KB stem symptom-first record-time refusal. Backstop for
// R-17-C1: prevents author-claim KB stems from landing in fragments
// even if the codebase-content sub-agent's brief teaching slips.

func TestCheckCodebaseKB_SymptomFirst_HTTPCode_OK(t *testing.T) {
	t.Parallel()
	body := "- **403 on every cross-origin request** — browsers reject non-CORS-safelisted headers."
	if msgs := checkSlotShape("codebase/api/knowledge-base", body); len(msgs) > 0 {
		t.Errorf("HTTP-code stem should pass; got refusal: %v", msgs)
	}
}

func TestCheckCodebaseKB_SymptomFirst_QuotedError_OK(t *testing.T) {
	t.Parallel()
	body := "- **`relation already exists` on second container** — Postgres rejects the loser when two containers race the same DDL on first boot."
	if msgs := checkSlotShape("codebase/api/knowledge-base", body); len(msgs) > 0 {
		t.Errorf("backtick-quoted-error stem should pass; got refusal: %v", msgs)
	}
}

func TestCheckCodebaseKB_SymptomFirst_FailureVerb_OK(t *testing.T) {
	t.Parallel()
	body := "- **Subject typo silently stops delivery** — NATS subjects are case-sensitive; one wrong character routes nothing."
	if msgs := checkSlotShape("codebase/api/knowledge-base", body); len(msgs) > 0 {
		t.Errorf("failure-verb stem should pass; got refusal: %v", msgs)
	}
}

func TestCheckCodebaseKB_SymptomFirst_Observable_OK(t *testing.T) {
	t.Parallel()
	body := "- **Empty body on cross-origin custom headers** — browsers strip non-safelisted headers without exposeHeaders."
	if msgs := checkSlotShape("codebase/api/knowledge-base", body); len(msgs) > 0 {
		t.Errorf("observable-wrong-state stem should pass; got refusal: %v", msgs)
	}
}

func TestCheckCodebaseKB_AuthorClaim_TypeORM_PassesViaBacktickConfig_FalsePositiveOK(t *testing.T) {
	t.Parallel()
	// The TypeORM stem `**TypeORM \`synchronize: false\` everywhere**`
	// is semantically author-claim — the porter searching "schema
	// corruption on deploy" or "ALTER TABLE deadlock" doesn't find it.
	// But the backtick-quoted token `synchronize: false` matches
	// kbStemQuotedErrorRE under Option (A) of implementation guide §7.
	// The heuristic accepts the false-positive at record time;
	// refinement (Tranche 4) catches it via the rubric.
	body := "- **TypeORM `synchronize: false` everywhere** — auto-sync mutates the schema on every container start."
	if msgs := checkSlotShape("codebase/api/knowledge-base", body); len(msgs) > 0 {
		t.Errorf("Option (A) accepts the backtick-config false-positive; got refusal: %v", msgs)
	}
}

func TestCheckCodebaseKB_AuthorClaim_DecomposeExecOnce_Refused(t *testing.T) {
	t.Parallel()
	// `**Decompose execOnce keys into migrate + seed**` — pure author-
	// claim directive. No HTTP code, no backtick token (`execOnce` is
	// not backtick-quoted in the stem), no failure verb, no observable.
	// This is the canonical Run-16 R-17-C1 miss; the heuristic refuses.
	body := "- **Decompose execOnce keys into migrate + seed** — a single combined key marks the whole script succeeded."
	msgs := checkSlotShape("codebase/api/knowledge-base", body)
	if len(msgs) == 0 {
		t.Fatal("author-claim stem `Decompose execOnce keys into migrate + seed` should be refused")
	}
	if !strings.Contains(strings.Join(msgs, "\n"), "author-claim") {
		t.Errorf("refusal message should name `author-claim` shape; got: %v", msgs)
	}
	if !strings.Contains(strings.Join(msgs, "\n"), "refinement-references/kb_shapes") {
		t.Errorf("refusal message should redirect to refinement-references/kb_shapes URI; got: %v", msgs)
	}
}

func TestCheckCodebaseKB_DirectiveTightlyMapped_PassesViaBacktickToken(t *testing.T) {
	t.Parallel()
	// `**Cache commands in \`initCommands\`, not \`buildCommands\`**`
	// — directive stem with backtick-quoted config keys. Same regex
	// path as TypeORM Option (A); accepts the directive-mapped form.
	// The body in the showcase reference opens with the observable
	// error string ("directory not found" / "config:cache bakes
	// absolute paths"); the stem-only check doesn't inspect body.
	body := "- **Cache commands in `initCommands`, not `buildCommands`** — config:cache bakes absolute paths into cached files."
	if msgs := checkSlotShape("codebase/api/knowledge-base", body); len(msgs) > 0 {
		t.Errorf("directive-tightly-mapped stem with backtick tokens should pass; got refusal: %v", msgs)
	}
}

func TestCheckCodebaseKB_StemAggregateNoOverflow(t *testing.T) {
	t.Parallel()
	// Five PASS-shaped bullets + one author-claim bullet. The single-
	// pass loop returns the first refusal; aggregation across all
	// offenders is Tranche 5's job.
	body := strings.Join([]string{
		"- **403 on cross-origin** — body 1.",
		"- **`relation already exists` on boot** — body 2.",
		"- **Subject typo silently stops delivery** — body 3.",
		"- **Empty body on missing exposeHeaders** — body 4.",
		"- **Vite manifest 500 on dev deploy** — body 5.",
		"- **Decompose execOnce keys into migrate + seed** — body 6 (offender).",
	}, "\n")
	msgs := checkSlotShape("codebase/api/knowledge-base", body)
	if len(msgs) == 0 {
		t.Fatal("expected at least one refusal naming the author-claim bullet")
	}
	if !strings.Contains(strings.Join(msgs, "\n"), "Decompose execOnce keys into migrate + seed") {
		t.Errorf("refusal should name the offending stem; got: %v", msgs)
	}
}
