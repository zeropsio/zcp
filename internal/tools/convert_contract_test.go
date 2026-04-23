package tools

import (
	"encoding/json"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TA-07: the contract this test pins is the PlatformError → MCP-JSON
// surface invariant. Every non-empty optional field on PlatformError
// must appear as its camelCase JSON key; every empty optional field
// must NOT appear (omitempty behavior, kept stable so consumers can
// rely on absence-means-empty). Previously only apiCode had this
// property; apiMeta joins it in this plan, and future additions go
// through the same contract.
//
// A field that silently disappears on the happy path re-opens F#7
// because the LLM then relies on the stale `suggestion` line instead
// of the structured detail it needs. A field that always appears
// (even when empty) inflates every response and breaks JSON shape
// assertions downstream.
func TestTA07_ConvertError_JSONContractForOptionalFields(t *testing.T) {
	t.Parallel()

	fullyPopulated := func() *platform.PlatformError {
		pe := platform.NewPlatformError(
			platform.ErrAPIError,
			"Invalid parameter provided.",
			"The platform flagged specific fields — see apiMeta.",
		)
		pe.APICode = "projectImportInvalidParameter"
		pe.Diagnostic = "full stderr tail"
		pe.APIMeta = []platform.APIMetaItem{
			{
				Code: "projectImportInvalidParameter",
				Metadata: map[string][]string{
					"{host}.mode": {"mode not supported"},
				},
			},
		}
		return pe
	}

	stripFn := map[string]func(pe *platform.PlatformError){
		"suggestion": func(pe *platform.PlatformError) { pe.Suggestion = "" },
		"apiCode":    func(pe *platform.PlatformError) { pe.APICode = "" },
		"diagnostic": func(pe *platform.PlatformError) { pe.Diagnostic = "" },
		"apiMeta":    func(pe *platform.PlatformError) { pe.APIMeta = nil },
	}

	// Phase 1: fully populated → every optional key is present.
	t.Run("populated_includes_all_keys", func(t *testing.T) {
		t.Parallel()
		parsed := parseConvertedError(t, fullyPopulated())
		for _, key := range []string{"code", "error", "suggestion", "apiCode", "diagnostic", "apiMeta"} {
			if _, ok := parsed[key]; !ok {
				t.Errorf("key %q missing from populated error JSON: %+v", key, parsed)
			}
		}
	})

	// Phase 2: strip one optional key at a time → only that key drops.
	for key, strip := range stripFn {
		t.Run("strip_"+key, func(t *testing.T) {
			t.Parallel()
			pe := fullyPopulated()
			strip(pe)
			parsed := parseConvertedError(t, pe)
			if _, ok := parsed[key]; ok {
				t.Errorf("stripped field %q still present: %v", key, parsed[key])
			}
			// Required fields stay.
			if _, ok := parsed["code"]; !ok {
				t.Errorf("required `code` disappeared when stripping %q", key)
			}
			if _, ok := parsed["error"]; !ok {
				t.Errorf("required `error` disappeared when stripping %q", key)
			}
		})
	}

	// Phase 3: apiMeta empty-slice and nil-slice BOTH produce absence.
	// Previously a len(pe.APIMeta) > 0 check distinguishes nil from
	// an explicitly empty slice; the contract flattens both to absence.
	t.Run("apiMeta_empty_slice_omitted", func(t *testing.T) {
		t.Parallel()
		pe := fullyPopulated()
		pe.APIMeta = []platform.APIMetaItem{}
		parsed := parseConvertedError(t, pe)
		if _, ok := parsed["apiMeta"]; ok {
			t.Errorf("apiMeta empty slice must be omitted from JSON, got %v", parsed["apiMeta"])
		}
	})
}

func parseConvertedError(t *testing.T, pe *platform.PlatformError) map[string]any {
	t.Helper()
	text := getResultText(t, convertError(pe))
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("json unmarshal: %v\n%s", err, text)
	}
	return parsed
}
