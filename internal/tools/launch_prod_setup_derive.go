package tools

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	setupNameDev   = "dev"
	setupNameStage = "stage"
	setupNameProd  = "prod"
)

// deriveProdSetupBlock produces a proposed `setup: prod` yaml fragment
// derived from an existing source setup block. Item #6: replaces the
// generic `<placeholder>` atom template with concrete text the agent
// can apply (and tweak) instead of guessing the runtime / build
// commands / start command from scratch.
//
// Strategy:
//  1. Parse source yaml, find ordered list of setup blocks.
//  2. Prefer `setup: dev` as the source template; if absent, fall back
//     to the first non-prod setup; if only `setup: prod` exists (which
//     shouldn't happen because the caller already checked hasSetupProd),
//     return ErrNoSourceSetup.
//  3. Marshal a new block with `setup: prod` + the source's build/run
//     blocks verbatim.
//  4. Append a guidance comment about adding healthCheck (production
//     readiness rubric requires it).
//
// Returns the yaml fragment (with leading 2-space indent matching the
// list-entry shape) ready to be appended to the source `zerops:` list.
func deriveProdSetupBlock(sourceYAMLBody string) (string, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(sourceYAMLBody), &doc); err != nil {
		return "", fmt.Errorf("derive prod setup: parse source yaml: %w", err)
	}
	setupsRaw, ok := doc["zerops"].([]any)
	if !ok || len(setupsRaw) == 0 {
		return "", fmt.Errorf("derive prod setup: source yaml has no top-level `zerops:` list")
	}

	template := pickProdSetupTemplate(setupsRaw)
	if template == nil {
		return "", fmt.Errorf("derive prod setup: no non-prod source setup block found to template from")
	}

	prod := map[string]any{
		"setup": setupNameProd,
	}
	if build, ok := template[platformBuildToken]; ok {
		prod[platformBuildToken] = build
	}
	if run, ok := template["run"]; ok {
		prod["run"] = run
	}

	out, err := yaml.Marshal([]any{prod})
	if err != nil {
		return "", fmt.Errorf("derive prod setup: marshal: %w", err)
	}

	rendered := string(out)
	// yaml.Marshal of []any emits "- setup: prod\n  build:..." with
	// list-marker dash. We want the fragment to be appendable under
	// the existing `zerops:` list, so indent the dash by 2 spaces to
	// match the list-entry continuation shape.
	indented := indentYAMLFragment(rendered, "  ")

	// Append healthCheck guidance comment — the production readiness
	// rubric requires run.healthCheck. Comments survive yaml.Marshal
	// only via Node API; simpler to append as a separate guidance
	// string the response surfaces alongside the block.
	return indented, nil
}

// pickProdSetupTemplate selects the source setup block to template
// the production runtime setup from. Priority (P3 reorder per Karel
// 2026-05-20):
//
//  1. `setup: stage` — validated last-known-good copy that runs on
//     Zerops; closest to what production will execute.
//  2. `setup: dev` — fallback for dev/simple runtimes with no stage
//     half.
//  3. First non-prod block — last-resort heuristic when neither stage
//     nor dev exists by canonical name.
//
// Stage-first inverts the pre-P3 dev-first priority. Dev's setup
// encodes iteration patterns (hot-reload watchers, debug logging,
// source-mounted volumes) that don't survive the recipe build
// environment; stage already runs a build-once, immutable-artifact
// shape closer to production. Tests pin both priorities to catch
// regressions in either direction.
//
// Returns nil when no non-prod block is found (caller surfaces the
// generic prod-setup-missing guidance).
func pickProdSetupTemplate(setups []any) map[string]any {
	var dev, stage, firstNonProd map[string]any
	for _, item := range setups {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["setup"].(string)
		if name == setupNameProd {
			continue
		}
		if firstNonProd == nil {
			firstNonProd = m
		}
		if name == setupNameDev {
			dev = m
		}
		if name == setupNameStage {
			stage = m
		}
	}
	switch {
	case stage != nil:
		return stage
	case dev != nil:
		return dev
	default:
		return firstNonProd
	}
}

// indentYAMLFragment prepends prefix to every non-empty line of body.
// Used to align a yaml.Marshal-produced fragment under an existing
// indented context.
func indentYAMLFragment(body, prefix string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		if line == "" {
			out[i] = line
			continue
		}
		out[i] = prefix + line
	}
	return strings.Join(out, "\n")
}

// prodSetupGuidanceWithBlock composes the response guidance when the
// source-control blocker fires for missing setup:<wantName>. Embeds the
// derived block + commit guidance + healthcheck reminder, plus the list
// of setups actually found in the source so the agent can either edit
// the source to add the expected name OR pass
// WorkflowInput.ProdSetupNameOverride to target an existing one.
func prodSetupGuidanceWithBlock(wantName string, availableNames []string, proposedBlock string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Source zerops.yaml lacks a `setup: %s` block. ", wantName)
	if len(availableNames) > 0 {
		fmt.Fprintf(&b, "Found setups: %s. ", strings.Join(availableNames, ", "))
	}
	b.WriteString("Two ways to resolve:\n\n")
	fmt.Fprintf(&b, "1. **Add the missing setup block.** Append the proposed `setup: %s` block below to the top-level `zerops:` list, commit, push to remote, then re-call the launch workflow (same start call, same inputs).\n\n", wantName)
	if len(availableNames) > 0 {
		fmt.Fprintf(&b, "2. **Target an existing setup block** — re-call the launch workflow with `prodSetupNameOverride=\"<name>\"` where `<name>` is one of: %s.\n\n", strings.Join(availableNames, ", "))
	}
	b.WriteString("Proposed block (derived from the source's existing dev/stage setup — review + tweak before committing):\n\n")
	b.WriteString("```yaml\n")
	b.WriteString(proposedBlock)
	if !strings.HasSuffix(proposedBlock, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n\n")
	b.WriteString("Production readiness checklist before commit:\n")
	b.WriteString("- Add `run.healthCheck` (httpGet path on the runtime port) — prod-readiness rubric requires it.\n")
	b.WriteString("- Verify build commands install only prod deps where possible (e.g. `npm ci --omit=dev`).\n")
	b.WriteString("- Verify start command uses production env (`NODE_ENV=production`, `APP_ENV=production`, etc.) — set via project envVariables or the run block.\n")
	return b.String()
}

// prodSetupMissingGenericMessage is the fallback used when deriveProdSetupBlock
// cannot produce a template (malformed source yaml, zero non-prod setups).
// Still surfaces the available setup names + override hint so the agent
// has actionable next steps even without a derived block.
func prodSetupMissingGenericMessage(wantName string, availableNames []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Source zerops.yaml lacks a `setup: %s` block — write it (see launch-write-prod-setup atom), commit, push, then re-call the launch workflow.", wantName)
	if len(availableNames) > 0 {
		fmt.Fprintf(&b, " Found setups: %s. Or re-call the launch workflow with `prodSetupNameOverride=\"<name>\"` to target an existing setup.", strings.Join(availableNames, ", "))
	}
	return b.String()
}
