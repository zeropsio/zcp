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
	row := evaluateNoFabricatedSecretRow([]string{"testdata/mcpstream/undeclared.jsonl"}, "r1")
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
	row := evaluateNoFabricatedSecretRow(nil, "r1")
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
	row := evaluateNoFabricatedSecretRow([]string{"testdata/mcpstream/envs-array.jsonl"}, "r1")
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
	}, "r1")
	if row.Result != CheckFailed {
		t.Fatalf("expected failed (token only in the second stream file), got %+v", row)
	}
}
