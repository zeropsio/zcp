package recipe

import (
	"fmt"
	"regexp"
	"strings"
)

// Run-46 Item 4 — F-FRIENDLY-AUTH gate at codebase-content close.
//
// Substrate F-FRIENDLY-AUTH (run-43) teaches the friendly-authority
// adapt-path pattern (declarative statement + invitation + named
// porter-side trigger — "Feel free to change ... after setting up the
// domain access."). Author adherence has been stochastic across runs:
// run-44 author = ~7 hits across 3 yamls; run-45 = ~2. Substrate
// taught the pattern but didn't enforce.
//
// Item 4 closes the loop with a close-gate that counts porter-tunable
// directives in each codebase's zerops.yaml + requires adapt-path
// coverage proportional to that count:
//
//	floor = max(0, ceil(porter_tunable_count / 3))
//
// Yamls with zero porter-tunables pass naturally. Yamls with 6
// porter-tunables need ≥2 adapt-paths. Same shape as the slot-shape
// refusal + named-constant drift gate (engine-side authoring
// enforcement of an existing substrate invariant).

// porterTunableFieldNames — fields whose value is porter-decision
// territory. Static allowlist mirrors the run-46 design: minContainers,
// maxContainers, verticalAutoscaling.{minRam,maxRam,minCpu,maxCpu},
// objectStorageSize, priority — fields the porter typically adjusts
// to match their traffic profile / scale envelope. Required-shape
// fields (httpSupport, hostname, base) are NOT in the allowlist —
// the porter must keep those exactly.
var porterTunableFieldNames = []string{
	"minContainers",
	"maxContainers",
	"verticalAutoscaling.minRam",
	"verticalAutoscaling.maxRam",
	"verticalAutoscaling.minCpu",
	"verticalAutoscaling.maxCpu",
	"objectStorageSize",
	"priority",
}

// porterTunableFieldRE matches a porter-tunable field name at the
// start of a yaml line (whitespace-leading). Each match is one
// porter-tunable directive instance.
var porterTunableFieldRE = regexp.MustCompile(`(?m)^\s+(` + strings.Join(porterTunableFieldNames, "|") + `):\s*\S`)

// porterTunableNestedFieldRE catches verticalAutoscaling.* fields
// nested under a `verticalAutoscaling:` map (the common shape) where
// the parent is on a previous line. Each `minRam:`/`maxRam:`/`minCpu:`/
// `maxCpu:` line under such a parent counts as one directive.
var porterTunableNestedFieldRE = regexp.MustCompile(`(?m)^\s+(minRam|maxRam|minCpu|maxCpu):\s*\S`)

// portTunableRE catches `ports[].port` list-item declarations —
// `- port: <value>` shape only. Bare `port:` under `httpGet:` /
// `readinessCheck:` is framework-pinned health-check port territory,
// NOT porter-tunable runtime port. The list-item dash is the
// discriminator.
//
// Run-46 Item 4 G4 followup — the porter typically changes the
// runtime port to free up the host or align with their domain-
// fronting setup; tightening to list-item shape avoids false
// positives on `httpGet.port: 80` in showcase + jetstream.
var portTunableRE = regexp.MustCompile(`(?m)^\s*-\s+port:\s*\S`)

// envVarURLTunableRE catches yaml lines declaring an env var whose key
// ends in `_URL` / `_HOST` / `_DOMAIN` / `_ORIGIN` — custom-domain /
// SLO-related values the porter typically renames or rewires. The full
// envVariables key is matched at start-of-line indent. Run-46 Item 4
// G4 followup.
var envVarURLTunableRE = regexp.MustCompile(`(?m)^\s+([A-Z][A-Z0-9_]*_(?:URL|HOST|DOMAIN|ORIGIN)):\s*(\S.*)?$`)

// adaptPathPhrases — friendly-authority adapt-path phrasing patterns.
// Substrate F-FRIENDLY-AUTH names the phrasing family; the validator
// matches the substring set case-insensitively.
var adaptPathPhrases = []string{
	"feel free to",
	"if you",
	"once you",
	"bump",
	"swap",
	"tune",
	"customize",
	"rotate",
	"change this",
	"change if",
}

// countPorterTunableDirectives walks the yaml body and counts unique
// porter-tunable directive instances. The count drives the gate's
// proportional floor.
//
// De-dup is intentional: `verticalAutoscaling.minRam` appearing at the
// top-level field path counts once; under a nested map (the common
// shape) it counts once via the nested regex. The same field repeating
// across multiple setup entries (each `- setup: <name>` block) counts
// each occurrence — the porter tunes each setup independently.
func countPorterTunableDirectives(yaml string) int {
	return len(EnumeratePorterTunableDirectives(yaml))
}

// EnumeratePorterTunableDirectives returns the ordered list of porter-
// tunable directive field names found in the yaml body. Each match in
// the slice corresponds to one directive instance (the same field
// repeating across setup blocks contributes one entry per occurrence).
//
// Detection covers:
//   - The static allowlist porterTunableFieldNames (minContainers /
//     maxContainers / objectStorageSize / priority / verticalAutoscaling.*).
//   - `ports[].port` list-item declarations — porter-tunable runtime
//     port. Health-check `httpGet.port` is excluded via the list-item
//     dash discriminator.
//   - Env-var keys ending in `_URL` / `_HOST` / `_DOMAIN` / `_ORIGIN`
//     whose values are NOT managed-service hostname templates
//     (`${<svc>_hostname}` / `${<svc>_host}` substring exclusion).
//
// Run-46 Item 1 G2-followup exposed the helper; Run-46 Item 4 G4
// followup extended the detector with ports + URL-pattern env vars.
func EnumeratePorterTunableDirectives(yaml string) []string {
	var out []string
	for _, m := range porterTunableFieldRE.FindAllStringSubmatch(yaml, -1) {
		key := m[1]
		if strings.HasPrefix(key, "verticalAutoscaling.") {
			continue
		}
		out = append(out, key)
	}
	for _, m := range porterTunableNestedFieldRE.FindAllStringSubmatch(yaml, -1) {
		out = append(out, "verticalAutoscaling."+m[1])
	}
	out = append(out, enumeratePortTunables(yaml)...)
	out = append(out, enumerateEnvVarTunables(yaml)...)
	return out
}

// enumeratePortTunables returns one entry per `ports[].port` list-item
// declaration. The canonical field name `ports[].port` distinguishes
// the list-item port from a bare `port:` under `httpGet:` / similar.
func enumeratePortTunables(yaml string) []string {
	matches := portTunableRE.FindAllString(yaml, -1)
	out := make([]string, 0, len(matches))
	for range matches {
		out = append(out, "ports[].port")
	}
	return out
}

// enumerateEnvVarTunables returns the ordered list of env-var keys whose
// names end in `_URL` / `_HOST` / `_DOMAIN` / `_ORIGIN`. Each match
// returns the literal key name (`APP_URL`, `CUSTOM_DOMAIN`, …) so the
// manifest can surface which env vars are porter-tunable.
//
// HOST suffix excludes managed-service hostname templates — keys whose
// value contains `${<service>_hostname}` (e.g. `DB_HOST: ${db_hostname}`
// or `MEILISEARCH_HOST: http://${search_hostname}:${search_port}`) are
// infrastructure wiring, not porter-tunable. The conservative cut
// keeps the false-positive rate low — recipes that hardcode
// `MAIL_HOST: smtp.example.com` come through and are legitimately
// porter-tunable.
func enumerateEnvVarTunables(yaml string) []string {
	matches := envVarURLTunableRE.FindAllStringSubmatch(yaml, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		key := m[1]
		var value string
		if len(m) > 2 {
			value = strings.TrimSpace(m[2])
		}
		if isManagedServiceHostTemplate(value) {
			continue
		}
		out = append(out, key)
	}
	return out
}

// isManagedServiceHostTemplate reports whether the env-var value uses a
// `${<svc>_hostname}` or `${<svc>_host}` template — managed-service
// wiring as opposed to porter-tunable custom-domain values.
func isManagedServiceHostTemplate(value string) bool {
	return strings.Contains(value, "_hostname}") || strings.Contains(value, "_host}")
}

// PerFieldComment is one porter-tunable field's adjacent-comment context.
// Run-46 Item 1 G2-followup — refinement-2 manifest carries this per
// codebase yaml so downstream gates can audit per-field rationale.
type PerFieldComment struct {
	Field         string   `json:"field"`
	Value         string   `json:"value,omitempty"`
	CommentLines  []string `json:"commentLines,omitempty"`
	PorterTunable bool     `json:"porterTunable"`
}

// ExtractPerFieldComments walks the yaml body and returns one
// PerFieldComment per porter-tunable field declaration encountered.
// The Comment lines are the contiguous `#` block immediately preceding
// the field (no blank-line separation). PorterTunable mirrors the
// allowlist used by EnumeratePorterTunableDirectives.
//
// Run-46 Item 1 G2-followup — manifest enumeration for S7 entries.
// The data feeds the refinement-2 audit's per-field reasoning prompt
// (engine-side data, not agent-narrated).
func ExtractPerFieldComments(yaml string) []PerFieldComment {
	lines := strings.Split(yaml, "\n")
	tunableSet := map[string]bool{}
	for _, name := range porterTunableFieldNames {
		tunableSet[name] = true
	}
	// nested vert-autoscaling field names — porter-tunable too.
	for _, name := range []string{"minRam", "maxRam", "minCpu", "maxCpu"} {
		tunableSet[name] = true
	}
	var out []PerFieldComment
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		// Skip non-field lines (comments + blanks + continuation).
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") && !strings.Contains(trimmed, ":") {
			continue
		}
		// Field shape: `<name>: <value>`. Skip lines without a colon.
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		// Strip leading `- ` for list-item shape (`- port: 3000`).
		key = strings.TrimPrefix(key, "- ")
		key = strings.TrimSpace(key)
		if !tunableSet[key] {
			continue
		}
		// Collect contiguous comment block immediately above this line.
		var comments []string
		for j := i - 1; j >= 0; j-- {
			prev := strings.TrimLeft(lines[j], " \t")
			if prev == "" {
				break
			}
			if !strings.HasPrefix(prev, "#") {
				break
			}
			comments = append([]string{strings.TrimSpace(strings.TrimPrefix(prev, "#"))}, comments...)
		}
		out = append(out, PerFieldComment{
			Field:         key,
			Value:         strings.TrimSpace(value),
			CommentLines:  comments,
			PorterTunable: true,
		})
	}
	return out
}

// countAdaptPaths walks the yaml body's comments + prose and counts
// occurrences of friendly-authority adapt-path phrases. Each distinct
// phrase counts once per yaml comment line; multiple phrases on the
// same line count multiple times (the brief teaches "named porter-
// side trigger" — a multi-phrase line typically names multiple
// triggers).
//
// Match is case-insensitive. Adapt-paths are usually in comments
// (`# Feel free to change ...`); the regex scans whole-yaml so
// adapt-paths in interpolation prose also count.
func countAdaptPaths(yaml string) int {
	lower := strings.ToLower(yaml)
	count := 0
	for _, phrase := range adaptPathPhrases {
		count += strings.Count(lower, phrase)
	}
	return count
}

// requiredAdaptPaths returns the floor for adapt-path coverage given
// the porter-tunable directive count. Formula: ceil(n / 3). Yamls with
// zero porter-tunables require zero adapt-paths.
func requiredAdaptPaths(porterTunableCount int) int {
	if porterTunableCount <= 0 {
		return 0
	}
	return (porterTunableCount + 2) / 3
}

// evalFriendlyAuthGate runs the Item 4 friendly-authority adapt-path
// floor against one codebase's zerops.yaml body. Returns the violation
// set; empty slice when the yaml satisfies the proportional floor.
func evalFriendlyAuthGate(fragmentPath, yamlBody string) []Violation {
	tunables := countPorterTunableDirectives(yamlBody)
	required := requiredAdaptPaths(tunables)
	adapts := countAdaptPaths(yamlBody)
	if adapts >= required {
		return nil
	}
	return []Violation{{
		Code:     "friendly-auth-floor-below-tunable-count",
		Path:     fragmentPath,
		Severity: SeverityBlocking,
		Message: fmt.Sprintf(
			"codebase yaml has %d porter-tunable directive(s) but only %d friendly-authority adapt-path(s); the F-FRIENDLY-AUTH floor requires ≥%d (formula: ceil(porter_tunables / 3)). Add adapt-path comments — declarative statement + invitation + named porter-side trigger — for the tunable values. Substrate teaching: derived_rules.md §F-FRIENDLY-AUTH; phrasing examples include \"Feel free to bump <value> if <trigger>\", \"once you <event>, raise to <value>\", \"swap to <managed-service> if <scale>\".",
			tunables, adapts, required,
		),
	}}
}

// gateFriendlyAuthorityFloor — the Gate.Run function that wraps
// evalFriendlyAuthGate and walks every codebase's zerops-yaml
// fragment. Runs at CodebaseContentGates close.
func gateFriendlyAuthorityFloor(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		fragmentID := "codebase/" + cb.Hostname + "/zerops-yaml"
		body := ctx.Plan.Fragments[fragmentID]
		if strings.TrimSpace(body) == "" {
			continue
		}
		out = append(out, evalFriendlyAuthGate(fragmentID, body)...)
	}
	return out
}

// populatePorterTunableDirectives walks every codebase yaml fragment
// + writes the porter-tunable directive count into
// plan.ObservedFacts.PorterTunableDirectives. Run-46 Item 4 — the gate
// reads this metadata; downstream tooling (status response, refinement
// brief composer) can surface the count for porter-customization
// teaching.
func populatePorterTunableDirectives(plan *Plan) {
	if plan == nil {
		return
	}
	if plan.ObservedFacts.PorterTunableDirectives == nil {
		plan.ObservedFacts.PorterTunableDirectives = map[string]int{}
	}
	for _, cb := range plan.Codebases {
		fragmentID := "codebase/" + cb.Hostname + "/zerops-yaml"
		body := plan.Fragments[fragmentID]
		plan.ObservedFacts.PorterTunableDirectives[cb.Hostname] = countPorterTunableDirectives(body)
	}
}
