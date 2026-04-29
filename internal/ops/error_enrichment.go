package ops

import (
	"errors"

	"github.com/zeropsio/zcp/internal/platform"
)

// EnrichSetupNotFound annotates a `zeropsYamlSetupNotFound` PlatformError
// with the local-known list of available setup names from the parsed
// zerops.yaml so the agent reads "available setups: dev, prod" without
// the platform's bare error message hiding the choices.
//
// F3 closure (audit-prerelease-internal-testing-2026-04-29): pre-fix the
// platform reclassifier echoed only the server message — the submitter
// HAS the parsed YAML at hand and knows which setups are declared, but
// that locally-known context was being discarded. The new central
// helper appends an APIMeta `availableSetups` entry so every error site
// surfaces the choice list through the same MCP wire shape.
//
// Signature: takes the in-flight error AND the locally-known context
// (attempted setup name, available setups). Returns the SAME error
// instance with APIMeta extended on a match — no-op for any other
// PlatformError shape, non-PlatformError errors, or nil. Idempotent:
// re-enriching an already-enriched error appends a duplicate entry,
// so call once per error.
//
// Layer rule: lives in ops/ (not platform/) because reading and
// validating zerops.yaml semantics is an ops concern, not a raw API
// concern. platform/ remains a thin SDK wrapper.
func EnrichSetupNotFound(err error, attemptedSetup string, availableSetups []string) error {
	if err == nil {
		return nil
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		return err
	}
	if pe.APICode != "zeropsYamlSetupNotFound" {
		return err
	}
	pe.APIMeta = append(pe.APIMeta, platform.APIMetaItem{
		Code:  "availableSetups",
		Error: "setup name not declared in zerops.yaml",
		Metadata: map[string][]string{
			"attemptedSetup":  {attemptedSetup},
			"availableSetups": availableSetups,
		},
	})
	return err
}
