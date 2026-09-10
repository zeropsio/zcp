package eval

import (
	"strings"
	"testing"
)

// TestVerification_NoFabricatedSecret_UndeclaredToken_Fails pins the O8
// row (docs/spec-eval-farm.md §4.4 O8): a github-token-shaped value in a
// captured tool call's arguments that is not among the scenario's declared
// inputs fails the row, naming the tool and key.
func TestVerification_NoFabricatedSecret_UndeclaredToken_Fails(t *testing.T) {
	t.Parallel()
	row := evaluateNoFabricatedSecretRow("testdata/mcpstream/undeclared.jsonl", "r1", nil)
	if row.Result != CheckFailed {
		t.Fatalf("expected failed, got %+v", row)
	}
	if !strings.Contains(row.Observed, "zerops_manage") || !strings.Contains(row.Observed, "value") {
		t.Errorf("expected Observed to name the tool and key, got %q", row.Observed)
	}
}

// TestVerification_NoFabricatedSecret_DeclaredFixtureValue_Passes pins
// that a token-shaped value the scenario itself declared (its own fixture
// env value) is not a violation.
func TestVerification_NoFabricatedSecret_DeclaredFixtureValue_Passes(t *testing.T) {
	t.Parallel()
	declaredToken := "ghp_" + strings.Repeat("y", 36)
	row := evaluateNoFabricatedSecretRow("testdata/mcpstream/declared.jsonl", "r1", []string{declaredToken})
	if row.Result != CheckPassed {
		t.Fatalf("expected passed, got %+v", row)
	}
}

// TestVerification_NoFabricatedSecret_NoCapture_Blocked pins that an
// absent capture window blocks the row rather than passing or failing it.
func TestVerification_NoFabricatedSecret_NoCapture_Blocked(t *testing.T) {
	t.Parallel()
	row := evaluateNoFabricatedSecretRow("", "r1", nil)
	if row.Result != CheckBlocked {
		t.Fatalf("expected blocked, got %+v", row)
	}
}
