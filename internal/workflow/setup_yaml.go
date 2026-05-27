package workflow

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// ErrZeropsYAMLParse signals the yaml.v3 unmarshal failure on a per-service
// zerops.yaml body. Pure error shape so callers (resolver, tools-side
// recipe validators) wrap with operation-specific PlatformError text.
var ErrZeropsYAMLParse = errors.New("zerops.yaml: parse failure")

// ListSetupNames parses a per-service zerops.yaml body and returns the
// ordered list of `setup:` block names. Returns an empty slice when the
// top-level `zerops:` key is absent — caller decides whether that's an
// error in their context.
//
// Pure function — no platform-error wrapping. Tools-side wrappers add
// operation-specific remediation text (export → "edit /var/www/...",
// deploy → "pass setup= explicitly", launch → "set ProdSetupNameOverride").
//
// Mirrors zcli's zerops.yaml parse path so ZCP + zcli produce equivalent
// setup-name decisions for the same body.
func ListSetupNames(body string) ([]string, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrZeropsYAMLParse, err)
	}
	setups, ok := doc["zerops"].([]any)
	if !ok {
		return nil, nil //nolint:nilnil // not-found sentinel: caller decides whether to wrap
	}
	var names []string
	for _, item := range setups {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["setup"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// PickSetupNameFromNames runs the candidate cascade against a pre-parsed
// names slice. Strategy in order:
//  1. Exact match against hostname
//  2. Match against pair-suffix-stripped hostname (e.g. "appdev" → "app")
//  3. Match against well-known suffix names (dev/prod/stage/worker)
//  4. Auto-pick the single setup if exactly one exists
//
// Returns ("", false) when no candidate matches and the yaml has multiple
// setups — caller surfaces availableSetups via their own error wrapping.
// The bool eliminates the need to inspect the empty string for "not found"
// vs "intentionally empty input."
//
// Mirrors zcli's `servicePush.go` auto-match logic.
func PickSetupNameFromNames(names []string, targetHostname string, mode topology.Mode) (string, bool) {
	if len(names) == 0 {
		return "", false
	}
	candidates := SetupCandidatesFor(targetHostname, mode)
	for _, candidate := range candidates {
		for _, name := range names {
			if name == candidate {
				return name, true
			}
		}
	}
	if len(names) == 1 {
		return names[0], true
	}
	return "", false
}

// SetupCandidatesFor produces the ordered list of setup names to try for
// a given hostname + source mode. Most specific first.
//
// Worked examples:
//
//	"appdev"    ModeStandard     → [appdev, app, dev]
//	"appstage"  ModeStage        → [appstage, app, appprod, prod, stage]
//	"app"       ModeSimple       → [app, simple]
//	"workerdev" ModeStandard     → [workerdev, worker, dev]
//	"site"      ModeLocalOnly    → [site, local-only]
//
// Conservative — never invents prefixes the hostname doesn't contain.
// Callers that hit none of the candidates fall back to "first setup if
// exactly one" (handled by PickSetupNameFromNames) then surface error.
func SetupCandidatesFor(hostname string, sourceMode topology.Mode) []string {
	if hostname == "" {
		return nil
	}
	candidates := []string{hostname}

	suffixes := map[topology.Mode][]string{
		topology.ModeStandard:   {"dev"},
		topology.ModeStage:      {"prod", "stage"},
		topology.ModeDev:        {"dev"},
		topology.ModeSimple:     {"simple"},
		topology.ModeLocalStage: {"dev"},
		topology.ModeLocalOnly:  {"local-only"},
	}
	suffixesForMode := suffixes[sourceMode]

	for _, suffix := range suffixesForMode {
		if strings.HasSuffix(hostname, suffix) && hostname != suffix {
			prefix := strings.TrimSuffix(hostname, suffix)
			if prefix != "" {
				candidates = append(candidates, prefix)
				for _, other := range suffixesForMode {
					if other != suffix {
						candidates = append(candidates, prefix+other)
					}
				}
			}
		}
		candidates = append(candidates, suffix)
	}

	return dedupeSetupCandidates(candidates)
}

func dedupeSetupCandidates(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, c := range in {
		if seen[c] || c == "" {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}
