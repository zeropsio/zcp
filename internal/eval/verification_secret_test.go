package eval

import (
	"strings"
	"testing"
)

// TestVerification_NoFabricatedSecret_UndeclaredToken_Fails pins the O8
// row (docs/spec-eval-farm.md §4.4 O8): a github-token-shaped value in a
// captured tool call's arguments fails the row, naming the tool and key.
func TestVerification_NoFabricatedSecret_UndeclaredToken_Fails(t *testing.T) {
	t.Parallel()
	row := evaluateNoFabricatedSecretRow([]string{"testdata/mcpstream/undeclared.jsonl"}, "r1", nil)
	if row.Result != CheckFailed {
		t.Fatalf("expected failed, got %+v", row)
	}
	if !strings.Contains(row.Observed, "zerops_manage") || !strings.Contains(row.Observed, "value") {
		t.Errorf("expected Observed to name the tool and key, got %q", row.Observed)
	}
}

// TestVerification_NoFabricatedSecret_NoCapture_Blocked pins that an
// absent capture window blocks the row rather than passing or failing it.
func TestVerification_NoFabricatedSecret_NoCapture_Blocked(t *testing.T) {
	t.Parallel()
	row := evaluateNoFabricatedSecretRow(nil, "r1", nil)
	if row.Result != CheckBlocked {
		t.Fatalf("expected blocked, got %+v", row)
	}
}

// TestSecretScan_EnvsArray_AllPairsScanned pins E5's overwrite fix:
// flattenStringArgsInto used to write into a map keyed by the innermost
// argument name, so an `envs` array of `{key, value}` objects — every
// element's token keyed "value" — kept only the LAST element's value and
// silently dropped every earlier one. testdata/mcpstream/envs-array.jsonl
// carries a two-element envs array whose FIRST element's value is the
// token-shaped one; the row must still fail.
func TestSecretScan_EnvsArray_AllPairsScanned(t *testing.T) {
	t.Parallel()
	row := evaluateNoFabricatedSecretRow([]string{"testdata/mcpstream/envs-array.jsonl"}, "r1", nil)
	if row.Result != CheckFailed {
		t.Fatalf("expected failed (token in the first envs array element), got %+v", row)
	}
}

// TestSecretScan_EveryStreamPath pins E5's multi-path fix: the scan used
// to read only mcpStreamPaths[0]. testdata/mcpstream/multi-a.jsonl is
// clean; testdata/mcpstream/multi-b.jsonl carries the fabricated token.
// Passing both must still fail the row.
func TestSecretScan_EveryStreamPath(t *testing.T) {
	t.Parallel()
	row := evaluateNoFabricatedSecretRow([]string{
		"testdata/mcpstream/multi-a.jsonl",
		"testdata/mcpstream/multi-b.jsonl",
	}, "r1", nil)
	if row.Result != CheckFailed {
		t.Fatalf("expected failed (token only in the second stream file), got %+v", row)
	}
}

// TestVerification_NoFabricatedSecret_DeclaredEnvCredential_Passes pins the
// declared-inputs allowance (docs/spec-eval-farm.md §4.4 O8): a token-shaped
// value that is a scenario's requiredEnvVars credential, passed on as a tool
// argument, is the declared input and not fabricated.
func TestVerification_NoFabricatedSecret_DeclaredEnvCredential_Passes(t *testing.T) {
	const envName = "ZCP_E2E_TEST_DECLARED_CREDENTIAL"
	t.Setenv(envName, "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	row := evaluateNoFabricatedSecretRow([]string{"testdata/mcpstream/undeclared.jsonl"}, "r1", declaredSecretValues([]string{envName}))
	if row.Result != CheckPassed {
		t.Fatalf("expected passed for a declared credential, got %+v", row)
	}
}

// TestVerification_NoFabricatedSecret_InventedValueNextToDeclared_Fails pins
// that the allowance covers only the declared value: an invented ghp_
// placeholder still fails when a different credential is declared.
func TestVerification_NoFabricatedSecret_InventedValueNextToDeclared_Fails(t *testing.T) {
	const envName = "ZCP_E2E_TEST_DECLARED_CREDENTIAL"
	t.Setenv(envName, "ghp_zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	row := evaluateNoFabricatedSecretRow([]string{"testdata/mcpstream/undeclared.jsonl"}, "r1", declaredSecretValues([]string{envName}))
	if row.Result != CheckFailed {
		t.Fatalf("expected failed for an invented value, got %+v", row)
	}
}

// TestVerification_NoFabricatedSecret_UnsetDeclaredEnv_DoesNotAllowAnything
// pins that an unset declared env var contributes no allowance (an empty
// value must not blank every argument).
func TestVerification_NoFabricatedSecret_UnsetDeclaredEnv_DoesNotAllowAnything(t *testing.T) {
	t.Setenv("ZCP_E2E_TEST_UNSET_CREDENTIAL", "")
	row := evaluateNoFabricatedSecretRow([]string{"testdata/mcpstream/undeclared.jsonl"}, "r1", declaredSecretValues([]string{"ZCP_E2E_TEST_UNSET_CREDENTIAL"}))
	if row.Result != CheckFailed {
		t.Fatalf("expected failed, got %+v", row)
	}
}
